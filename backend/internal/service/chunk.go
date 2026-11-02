package service

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"cloudbox/internal/model"
	"cloudbox/internal/repository"
)

// ErrQuotaExceeded 配额不足（映射 HTTP 400 / code 30002，只预检不扣减，真扣减在 S3 原子 UPDATE）
var ErrQuotaExceeded = errors.New("配额不足")

// ErrChunkConflict 分片会话冲突，复用仓储层哨兵（UUID 碰撞时重试一次）
var ErrChunkConflict = repository.ErrChunkConflict

// ErrChunkNotFound 分片会话不存在，复用仓储层哨兵（含越权伪装）
var ErrChunkNotFound = repository.ErrChunkNotFound

// 分片会话参数上限（PRD §2.1 冻结 + 规范 §5.3.3，集中命名常量禁魔法数字）
const (
	// DefaultChunkSize 默认分片大小 2MB（推进计划 5.1 口径，可配项提示用）
	DefaultChunkSize = 2 * 1024 * 1024
	// MaxChunkSize 单分片上限 8MB（防超大片打满内存，MaxBytesReader 按此 +1MB 兜底）
	MaxChunkSize = 8 * 1024 * 1024
	// DefaultMaxFileSize 单文件上限 5GB（PRD §2.1）
	DefaultMaxFileSize = 5 * 1024 * 1024 * 1024
	// DefaultMaxChunks 分片总数上限 10000（PRD §2.1）
	DefaultMaxChunks = 10000
)

// ChunkService 分片会话业务：编排会话签发与分片落盘；S2 落 .part 暂存，事务为单行 UPSERT
type ChunkService struct {
	files       *repository.FileRepository
	users       *repository.UserRepository
	chunks      *repository.ChunkRepository
	maxFileSize uint64
	maxChunks   uint32
	tmpDir      string
}

// ChunkUploadResult 分片上传回显内部载体（Handler 转 DTO，不直接暴露）
type ChunkUploadResult struct {
	UploadID   string
	ChunkIndex uint32
	Size       uint64
}

// NewChunkService 构造注入；maxFileSize<=0 回退 5GB，maxChunks<=0 回退 10000
func NewChunkService(files *repository.FileRepository, users *repository.UserRepository, chunks *repository.ChunkRepository, maxFileSize int64, maxChunks int) *ChunkService {
	var mf uint64 = DefaultMaxFileSize
	if maxFileSize > 0 {
		mf = uint64(maxFileSize)
	}
	var mc uint32 = DefaultMaxChunks
	if maxChunks > 0 {
		mc = uint32(maxChunks)
	}
	return &ChunkService{files: files, users: users, chunks: chunks, maxFileSize: mf, maxChunks: mc}
}

// WithTmpDir 注入分片暂存根（S2 落 .part 用）；空串回退系统临时目录，返回同一实例方便链式调用
func (s *ChunkService) WithTmpDir(dir string) *ChunkService {
	s.tmpDir = dir
	return s
}

// tmpRoot 分片暂存根；未注入回退系统临时目录（测试/兜底）
func (s *ChunkService) tmpRoot() string {
	if strings.TrimSpace(s.tmpDir) != "" {
		return s.tmpDir
	}
	return filepath.Join(os.TempDir(), "cloudbox-chunks")
}

// validUploadID 36 字符 UUID 形校验（8-4-4-4-12，hex+`-`），防 `../` 路径穿越
func validUploadID(id string) bool {
	id = strings.TrimSpace(id)
	if len(id) != 36 {
		return false
	}
	for i := 0; i < 36; i++ {
		c := id[i]
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
			continue
		}
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return false
		}
	}
	return true
}

// partAbsPath 分片落盘绝对路径：tmpRoot/<uploadID>/<index>.part，先 Clean 再前缀双保险
func (s *ChunkService) partAbsPath(uploadID string, index uint32) (abs, rel string, err error) {
	rel = fmt.Sprintf("%s/%d.part", strings.TrimSpace(uploadID), index)
	abs = filepath.Join(s.tmpRoot(), filepath.FromSlash(rel))
	cleaned := filepath.Clean(abs)
	root := filepath.Clean(s.tmpRoot())
	if cleaned != root && !strings.HasPrefix(cleaned, root+string(filepath.Separator)) {
		return "", "", fmt.Errorf("%w: 分片路径非法", ErrInvalidParams)
	}
	return cleaned, rel, nil
}

// savePart 流式落盘：tmp 写完 Rename 原子生效；CopyN 上限 cs+1，超限删 tmp 并报参错
func (s *ChunkService) savePart(abs string, chunkSize uint64, src io.Reader) (uint64, error) {
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return 0, fmt.Errorf("建分片目录: %w", err)
	}
	tmp := fmt.Sprintf("%s.tmp-%d", abs, os.Getpid())
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return 0, fmt.Errorf("建分片临时文件: %w", err)
	}
	// 有界拷贝：最多读 cs+1 字节，多 1 字节即判定超限
	n, err := io.CopyN(f, src, int64(chunkSize)+1)
	cerr := f.Close()
	if err != nil && !errors.Is(err, io.EOF) {
		_ = os.Remove(tmp)
		return 0, fmt.Errorf("写入分片: %w", err)
	}
	if cerr != nil {
		_ = os.Remove(tmp)
		return 0, fmt.Errorf("关闭分片文件: %w", cerr)
	}
	if n == 0 {
		_ = os.Remove(tmp)
		return 0, fmt.Errorf("%w: 分片为空", ErrInvalidParams)
	}
	if uint64(n) > chunkSize {
		_ = os.Remove(tmp)
		return 0, fmt.Errorf("%w: 分片超限", ErrInvalidParams)
	}
	if err := os.Rename(tmp, abs); err != nil {
		_ = os.Remove(tmp)
		return 0, fmt.Errorf("生效分片文件: %w", err)
	}
	return uint64(n), nil
}

// UploadChunk 单分片上传：会话归属 → 序号/大小校验 → 落盘 → 幂等入库；重传覆盖以最后一次为准
func (s *ChunkService) UploadChunk(ownerID uint64, uploadID string, index uint32, src io.Reader, size int64) (*ChunkUploadResult, error) {
	if ownerID == 0 {
		return nil, fmt.Errorf("%w: 用户非法", ErrInvalidParams)
	}
	uploadID = strings.TrimSpace(uploadID)
	if !validUploadID(uploadID) {
		return nil, fmt.Errorf("%w: 会话非法", ErrInvalidParams)
	}
	if src == nil {
		return nil, fmt.Errorf("%w: 分片缺失", ErrInvalidParams)
	}
	sess, err := s.chunks.FindSession(uploadID, ownerID)
	if err != nil {
		return nil, err
	}
	if sess.Status == 2 {
		// 已取消会话伪装成不存在，不泄露存在性
		return nil, ErrChunkNotFound
	}
	if index >= sess.TotalChunks {
		return nil, fmt.Errorf("%w: 分片序号越界", ErrInvalidParams)
	}
	if size <= 0 || uint64(size) > sess.ChunkSize {
		return nil, fmt.Errorf("%w: 分片大小非法", ErrInvalidParams)
	}
	if uint64(size) > MaxChunkSize {
		return nil, fmt.Errorf("%w: 分片超限", ErrInvalidParams)
	}
	abs, rel, err := s.partAbsPath(uploadID, index)
	if err != nil {
		return nil, err
	}
	n, err := s.savePart(abs, sess.ChunkSize, src)
	if err != nil {
		return nil, err
	}
	if err := s.chunks.UpsertChunkDone(uploadID, ownerID, index, sess.TotalChunks, sess.ChunkSize, sess.FileHash, rel); err != nil {
		// DB 失败删刚写 .part，防孤儿
		_ = os.Remove(abs)
		return nil, err
	}
	return &ChunkUploadResult{UploadID: uploadID, ChunkIndex: index, Size: n}, nil
}

// validSHA256 hash 规则：64 位 hex，大小写均放行，存原串
func validSHA256(h string) bool {
	if len(h) != 64 {
		return false
	}
	_, err := hex.DecodeString(h)
	return err == nil
}

// newUploadID 36 字符 UUIDv4（8-4-4-4-12），仅 crypto/rand，不引第三方 uuid 库
func newUploadID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("生成 upload_id: %w", err)
	}
	// RFC4122 v4：版本号 + 变体位
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	h := hex.EncodeToString(b[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32], nil
}

// validateInit V1–V5 纯函数校验：文件名/hash/三元组一致性，不碰 DB 可单测
func (s *ChunkService) validateInit(fileName, fileHash string, totalSize uint64, totalChunks uint32, chunkSize uint64) error {
	if !validFileName(fileName) {
		return fmt.Errorf("%w: 文件名非法", ErrInvalidParams)
	}
	if !validSHA256(strings.TrimSpace(fileHash)) {
		return fmt.Errorf("%w: 文件哈希非法", ErrInvalidParams)
	}
	if totalSize == 0 || totalSize > s.maxFileSize {
		return fmt.Errorf("%w: 文件大小非法", ErrInvalidParams)
	}
	if chunkSize == 0 || chunkSize > MaxChunkSize {
		return fmt.Errorf("%w: 分片大小非法", ErrInvalidParams)
	}
	if totalChunks == 0 || totalChunks > s.maxChunks {
		return fmt.Errorf("%w: 分片总数非法", ErrInvalidParams)
	}
	want := uint32((totalSize + chunkSize - 1) / chunkSize)
	if totalChunks != want {
		return fmt.Errorf("%w: 分片总数与大小不一致", ErrInvalidParams)
	}
	return nil
}

// checkParent 父目录校验：0 根直接过，否则须同人所有且为文件夹（与 FileService 同语义）
func (s *ChunkService) checkParent(ownerID, parentID uint64) error {
	if parentID == 0 {
		return nil
	}
	p, err := s.files.FindByID(parentID, ownerID)
	if err != nil {
		if errors.Is(err, ErrFileNotFound) {
			return ErrInvalidParent
		}
		return err
	}
	if p.IsFolder != 1 {
		return ErrInvalidParent
	}
	return nil
}

// checkQuota 配额预检：used+total<=quota，只读不扣减；先短路 total>quota 防加法溢出
func (s *ChunkService) checkQuota(ownerID, totalSize uint64) error {
	u, err := s.users.FindByID(ownerID)
	if err != nil {
		return err
	}
	if totalSize > u.QuotaBytes {
		return ErrQuotaExceeded
	}
	if u.UsedBytes+totalSize < u.UsedBytes {
		return ErrQuotaExceeded
	}
	if u.UsedBytes+totalSize > u.QuotaBytes {
		return ErrQuotaExceeded
	}
	return nil
}

// InitUpload 会话签发主流程：V1–V5 → parent → quota → UUID 落占位行；hash 已存在也照常建会话
func (s *ChunkService) InitUpload(ownerID, parentID uint64, fileName, fileHash string, totalSize uint64, totalChunks uint32, chunkSize uint64) (*model.UploadChunk, error) {
	if ownerID == 0 {
		return nil, fmt.Errorf("%w: 用户非法", ErrInvalidParams)
	}
	if err := s.validateInit(fileName, fileHash, totalSize, totalChunks, chunkSize); err != nil {
		return nil, err
	}
	if err := s.checkParent(ownerID, parentID); err != nil {
		return nil, err
	}
	if err := s.checkQuota(ownerID, totalSize); err != nil {
		return nil, err
	}
	uploadID, err := newUploadID()
	if err != nil {
		return nil, err
	}
	row := &model.UploadChunk{
		UploadID:    uploadID,
		FileHash:    strings.TrimSpace(fileHash),
		ChunkIndex:  repository.ChunkSessionMarker,
		TotalChunks: totalChunks,
		ChunkSize:   chunkSize,
		ChunkPath:   "",
		OwnerID:     ownerID,
		Status:      0,
	}
	if err := s.chunks.CreateInitRow(row); err != nil {
		if errors.Is(err, repository.ErrChunkConflict) {
			// UUID 碰撞理论重试一次
			uploadID, err = newUploadID()
			if err != nil {
				return nil, err
			}
			row.UploadID = uploadID
			if err := s.chunks.CreateInitRow(row); err != nil {
				return nil, err
			}
			return row, nil
		}
		return nil, err
	}
	return row, nil
}
