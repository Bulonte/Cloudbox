package upload

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"cloudbox/internal/model"
	"cloudbox/internal/repository"
	"cloudbox/internal/storage"
)

// ErrInvalidName 文件名非法（空/超长/含路径分隔）
var ErrInvalidName = errors.New("文件名非法")

// ErrFileNotFound 父目录不存在（含越权伪装），复用仓储层哨兵
var ErrFileNotFound = repository.ErrFileNotFound

// ErrInvalidParent 父目录非法，复用仓储层哨兵
var ErrInvalidParent = repository.ErrInvalidParent

// ErrFileConflict 同 hash 已存在（uk_hash），复用仓储层哨兵（W5 转被动秒传前按 409 闭合）
var ErrFileConflict = repository.ErrFileConflict

// UploadService 单文件上传编排：S5 校验 → SHA256 → S4 落盘 → S1 落库
type UploadService struct {
	files     *repository.FileRepository
	store     storage.Storage
	maxSimple int64
}

// NewUploadService 构造注入；maxSimple<=0 回退 MaxSimpleUploadSize
func NewUploadService(files *repository.FileRepository, store storage.Storage, maxSimple int64) *UploadService {
	if maxSimple <= 0 {
		maxSimple = MaxSimpleUploadSize
	}
	return &UploadService{files: files, store: store, maxSimple: maxSimple}
}

// MaxSimpleSize 服务端单文件上限（handler 设 MaxBytesReader 用）
func (s *UploadService) MaxSimpleSize() int64 {
	if s.maxSimple <= 0 {
		return MaxSimpleUploadSize
	}
	return s.maxSimple
}

// validFileName 文件名规则：与 service/file.go 同口径（trim 后非空、≤255 rune、禁 / \）
func validFileName(name string) bool {
	n := strings.TrimSpace(name)
	if n == "" || utf8.RuneCountInString(n) > 255 {
		return false
	}
	return !strings.ContainsAny(n, "/\\\x00")
}

// Upload 上传主流程：src 只读一次，CopyN 有界缓冲后复用
func (s *UploadService) Upload(ownerID, parentID uint64, fileName string, src io.Reader, size int64) (*model.File, error) {
	if ownerID == 0 {
		return nil, fmt.Errorf("%w: 用户非法", ErrInvalidName)
	}
	if src == nil {
		return nil, ErrTooLarge
	}
	if !validFileName(fileName) {
		return nil, ErrInvalidName
	}
	ext, err := ValidateExt(fileName)
	if err != nil {
		return nil, err
	}
	if err := ValidateSize(size, s.MaxSimpleSize()); err != nil {
		return nil, err
	}
	if err := s.checkParent(ownerID, parentID); err != nil {
		return nil, err
	}
	limit := s.MaxSimpleSize()
	var buf bytes.Buffer
	n, err := io.CopyN(&buf, src, limit+1)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("读取上传流: %w", err)
	}
	if n > limit || int64(buf.Len()) > limit {
		return nil, ErrTooLarge
	}
	data := buf.Bytes()
	if err := ValidateSize(int64(len(data)), limit); err != nil {
		return nil, err
	}
	headLen := len(data)
	if headLen > SniffLen {
		headLen = SniffLen
	}
	mime, err := SniffAndCheck(data[:headLen], ext)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	rel, err := s.store.Store(hash, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("落盘失败: %w", err)
	}
	f := &model.File{
		FileName: strings.TrimSpace(fileName),
		FileHash: hash,
		FileSize: uint64(len(data)),
		FilePath: rel,
		MimeType: mime,
		ParentID: parentID,
		OwnerID:  ownerID,
		IsFolder: 0,
	}
	if err := s.files.CreateFile(f); err != nil {
		return nil, err
	}
	return f, nil
}

// checkParent 父目录校验：0=根直接过；否则须同人所有且为文件夹
func (s *UploadService) checkParent(ownerID, parentID uint64) error {
	if parentID == 0 {
		return nil
	}
	p, err := s.files.FindByID(parentID, ownerID)
	if err != nil {
		return err
	}
	if p.IsFolder != 1 {
		return ErrInvalidParent
	}
	return nil
}
