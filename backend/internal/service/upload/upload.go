package upload

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"unicode/utf8"

	"cloudbox/internal/model"
	"cloudbox/internal/repository"
	"cloudbox/internal/service/smart"
	"cloudbox/internal/storage"
)

// ErrInvalidName 文件名非法（空/超长/含路径分隔）
var ErrInvalidName = errors.New("文件名非法")

// ErrFileNotFound 父目录不存在（含越权伪装），复用仓储层哨兵
var ErrFileNotFound = repository.ErrFileNotFound

// ErrInvalidParent 父目录非法，复用仓储层哨兵
var ErrInvalidParent = repository.ErrInvalidParent

// ErrFileConflict 同 hash 已存在（uk_hash 全局唯一，S4 后本人重复走被动秒传直返，跨用户仍按 409 闭合）
var ErrFileConflict = repository.ErrFileConflict

// ErrQuotaExceeded 配额不足（映射 HTTP 400 / code 30002；分片链路复用 service.ErrQuotaExceeded 同语义）
var ErrQuotaExceeded = errors.New("配额不足")

// InstantResult 秒传预检结果：Hit=true 表本人已有（秒传成功，File 为既有行）；
// Hit=false + ExistsGlobal=true 表他人已存（全局提示，前端可跳过上传，实传仍 409）；
// 全 miss 时 File=nil 双 false，前端走正常上传
type InstantResult struct {
	File         *model.File
	Hit          bool
	ExistsGlobal bool
}

// validSHA256 hash 规则：64 位 hex，大小写均放行（与 chunk 会话口径一致）
func validSHA256(h string) bool {
	if len(h) != 64 {
		return false
	}
	_, err := hex.DecodeString(strings.TrimSpace(h))
	return err == nil
}

// InstantCheck 秒传预检（只读+存储存在性双判，不写 DB 不碰配额，<1s）：
// 本人/他人命中均须存储对象仍在才算命中；blob 丢失一律当 miss 让前端重传自愈
func (s *UploadService) InstantCheck(ownerID, parentID uint64, fileName, fileHash string) (*InstantResult, error) {
	if ownerID == 0 {
		return nil, fmt.Errorf("%w: 用户非法", ErrInvalidName)
	}
	if !validFileName(fileName) {
		return nil, ErrInvalidName
	}
	if !validSHA256(strings.TrimSpace(fileHash)) {
		return nil, ErrInvalidName
	}
	if err := s.checkParent(ownerID, parentID); err != nil {
		return nil, err
	}
	canonical := strings.ToLower(strings.TrimSpace(fileHash))
	if f, err := s.files.FindByHash(canonical, ownerID); err != nil {
		return nil, err
	} else if f != nil {
		// 本人命中须二次校验存储对象：blob 丢失时不当命中，落到全局/全 miss 让前端重传自愈
		if _, ok := s.store.Exists(canonical); ok {
			return &InstantResult{File: f, Hit: true}, nil
		}
	}
	any, err := s.files.FindAnyByHash(canonical)
	if err != nil {
		return nil, err
	}
	if any == nil {
		return &InstantResult{Hit: false}, nil
	}
	// 他人已存：存储对象在才算全局命中，盘丢了当 miss 让前端正常上传重建
	if _, ok := s.store.Exists(canonical); ok {
		return &InstantResult{Hit: false, ExistsGlobal: true}, nil
	}
	return &InstantResult{Hit: false}, nil
}

// UploadService 单文件上传编排：S5 校验 → SHA256 → S4 落盘 → S1 落库 + S5 配额闭环
// S8 追加 WithClassifier/WithFingerprints（分类 + 指纹旁路 enrichment，nil 即关闭，老单测兼容）
type UploadService struct {
	files      *repository.FileRepository
	users      *repository.UserRepository
	store      storage.Storage
	maxSimple  int64
	classifier smart.Classifier
	fps        *repository.FingerprintRepository
}

// NewUploadService 构造注入；maxSimple<=0 回退 MaxSimpleUploadSize
func NewUploadService(files *repository.FileRepository, store storage.Storage, maxSimple int64) *UploadService {
	if maxSimple <= 0 {
		maxSimple = MaxSimpleUploadSize
	}
	return &UploadService{files: files, store: store, maxSimple: maxSimple}
}

// WithUsers 注入用户仓储（S5 配额预检+原子扣减用；nil 即无配额模式，老单测兼容）
func (s *UploadService) WithUsers(u *repository.UserRepository) *UploadService {
	s.users = u
	return s
}

// WithClassifier 注入分类器（S8 分类旁路；nil 即关闭，老单测兼容）
func (s *UploadService) WithClassifier(c smart.Classifier) *UploadService {
	s.classifier = c
	return s
}

// WithFingerprints 注入指纹仓储（S8 指纹旁路；nil 即关闭，老单测兼容）
func (s *UploadService) WithFingerprints(fps *repository.FingerprintRepository) *UploadService {
	s.fps = fps
	return s
}

// checkQuota 配额预检（nil 用户仓储时跳过；与 ChunkService 同语义：只读不扣减）
func (s *UploadService) checkQuota(ownerID, size uint64) error {
	if s.users == nil {
		return nil
	}
	u, err := s.users.FindByID(ownerID)
	if err != nil {
		return err
	}
	if size > u.QuotaBytes {
		return ErrQuotaExceeded
	}
	if u.UsedBytes+size < u.UsedBytes {
		return ErrQuotaExceeded
	}
	if u.UsedBytes+size > u.QuotaBytes {
		return ErrQuotaExceeded
	}
	return nil
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
	// S4 被动秒传：本人已有同 hash 且存储对象仍在，直接返回既有行（0 字节净增）。
	// 必须先于配额预检：配额已满时重传已有文件仍应秒传成功，否则永远走不到短路。
	// blob 已丢时不短路，落到正常 Store 流程重建自愈，避免返回不可下载的孤儿引用。
	if existing, err := s.files.FindByHash(hash, ownerID); err != nil {
		return nil, err
	} else if existing != nil {
		if _, ok := s.store.Exists(hash); ok {
			s.enrich(existing, data)
			return existing, nil
		}
	}
	// S5 配额闭环：声明 size 预检已过，此处按实测 actual 再卡一次（client 可能谎报 size）
	if err := s.checkQuota(ownerID, uint64(len(data))); err != nil {
		return nil, err
	}
	rel, err := s.store.Store(hash, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("落盘失败: %w", err)
	}
	// S5 原子真扣：false 即并发超额，转 ErrQuotaExceeded；nil 用户仓储时跳过（老单测兼容）
	if s.users != nil {
		ok, err := s.users.IncrUsedBytesIfEnough(ownerID, uint64(len(data)))
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, ErrQuotaExceeded
		}
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
		// S5 回补：落库失败一律回补配额；uk 冲突的自抢回查转秒传直返，他人仍 409
		if s.users != nil {
			_ = s.users.DecrUsedBytes(ownerID, uint64(len(data)))
		}
		if errors.Is(err, ErrFileConflict) {
			if existing, rerr := s.files.FindByHash(hash, ownerID); rerr == nil && existing != nil {
				s.enrich(existing, data)
				return existing, nil
			}
		}
		return nil, err
	}
	s.enrich(f, data)
	return f, nil
}

// enrich S8 分类 + 指纹旁路：落库成功后才调，全吞错（fail-open），永不阻断上传
func (s *UploadService) enrich(f *model.File, data []byte) {
	if s.classifier == nil && s.fps == nil {
		return
	}
	text := smart.TextSnippet(f.FileName, data, f.MimeType)
	combined := strings.TrimSpace(f.FileName + " " + text)
	cat := smart.ClassifyWithFallback(s.classifier, f.FileName, combined)
	_ = s.files.UpdateByID(f.ID, map[string]any{"category": cat.Category.String(), "confidence": cat.Confidence})
	f.Category = sql.NullString{String: cat.Category.String(), Valid: true}
	f.Confidence = sql.NullFloat64{Float64: cat.Confidence, Valid: true}
	if s.fps == nil {
		return
	}
	algo := smart.PickAlgorithm(f.FileName, f.MimeType)
	var hexStr string
	if algo == "phash" {
		fp, err := smart.PHashFromBytes(data)
		if err != nil || fp == 0 {
			return
		}
		hexStr = smart.EncodePHash(fp)
	} else {
		fp := smart.SimHashWithSegmenter(combined, nil)
		if fp == 0 {
			return
		}
		hexStr = smart.EncodeSimHash(fp)
	}
	if err := s.fps.Upsert(context.Background(), f.ID, algo, hexStr); err != nil {
		slog.Warn("指纹落库失败，已跳过", "file_id", f.ID, "algo", algo)
	}
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
