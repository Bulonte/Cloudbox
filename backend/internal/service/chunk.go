package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"cloudbox/internal/model"
	"cloudbox/internal/repository"
	"cloudbox/internal/service/smart"
	"cloudbox/internal/service/upload"
	"cloudbox/internal/storage"
)

// ErrQuotaExceeded 配额不足（映射 HTTP 400 / code 30002，只预检不扣减，真扣减在 S3 原子 UPDATE）
var ErrQuotaExceeded = errors.New("配额不足")

// ErrChunkConflict 分片会话冲突，复用仓储层哨兵（UUID 碰撞时重试一次）
var ErrChunkConflict = repository.ErrChunkConflict

// ErrChunkNotFound 分片会话不存在，复用仓储层哨兵（含越权伪装）
var ErrChunkNotFound = repository.ErrChunkNotFound

// ErrMergeInProgress 合并进行中（同会话并发 complete 抢锁失败，前端轮询 progress 重试）
var ErrMergeInProgress = errors.New("合并进行中，请稍后重试")

// MergeLocker 合并互斥抽象：生产传 *redis.Client（同名同参直接满足），测试传 stub；
// nil 表无锁模式（单测默认），Acquire 出错时降级放行仍靠 DB 原子兜底
type MergeLocker interface {
	AcquireLock(ctx context.Context, key string, ttl time.Duration) (string, bool, error)
	ReleaseLock(ctx context.Context, key, token string) (bool, error)
}

// mergeLockTTL 同会话合并锁过期：10min 覆盖 5GB 大文件合并（读盘+SHA 常超 30s），
// 防锁先过期致双 complete 交错写坏同一 .merged；合并仍以 O_EXCL 互斥兜底
const mergeLockTTL = 10 * time.Minute

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
// S3 合并需 WithStorage + WithTmpDir 双注入（测试/生产 main.go 均显式注入）
// S5 追加 WithLocker（合并互斥，nil 即无锁模式）
// S8 追加 WithClassifier/WithFingerprints（分类 + 指纹旁路 enrichment，nil 即关闭）
type ChunkService struct {
	files       *repository.FileRepository
	users       *repository.UserRepository
	chunks      *repository.ChunkRepository
	store       storage.Storage
	locker      MergeLocker
	classifier  smart.Classifier
	fps         *repository.FingerprintRepository
	maxFileSize uint64
	maxChunks   uint32
	tmpDir      string
}

// ChunkProgressResult 断点续传进度内部载体（Handler 转 DTO）
type ChunkProgressResult struct {
	UploadID    string
	FileHash    string
	TotalChunks uint32
	ChunkSize   uint64
	Status      int8
	Uploaded    []uint32
	Missing     []uint32
	Complete    bool
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

// WithStorage 注入内容寻址存储（S3 合并/秒传复用 Store）；返回同一实例方便链式调用
func (s *ChunkService) WithStorage(st storage.Storage) *ChunkService {
	s.store = st
	return s
}

// WithLocker 注入合并互斥锁（S5 生产传 *redis.Client；nil 即无锁模式，单测默认）
func (s *ChunkService) WithLocker(l MergeLocker) *ChunkService {
	s.locker = l
	return s
}

// WithClassifier 注入分类器（S8 分类旁路；nil 即关闭，老单测兼容）
func (s *ChunkService) WithClassifier(c smart.Classifier) *ChunkService {
	s.classifier = c
	return s
}

// WithFingerprints 注入指纹仓储（S8 指纹旁路；nil 即关闭，老单测兼容）
func (s *ChunkService) WithFingerprints(fps *repository.FingerprintRepository) *ChunkService {
	s.fps = fps
	return s
}

// mergeLockKey 同会话合并锁键；uploadID 已归一化 trim，key 限定单会话互斥
func mergeLockKey(uploadID string) string {
	return "chunk:merge:" + strings.TrimSpace(uploadID)
}

// acquireMerge 抢合并锁：nil 锁直接放行（空释放）；Acquire 出错（Redis 宕）降级放行，
// 仍靠 IncrIfEnough + uk_hash 兜底正确性；拿锁失败（他人持有）转 ErrMergeInProgress
func (s *ChunkService) acquireMerge(uploadID string) (release func(), err error) {
	noop := func() {}
	if s.locker == nil {
		return noop, nil
	}
	key := mergeLockKey(uploadID)
	token, ok, lerr := s.locker.AcquireLock(context.Background(), key, mergeLockTTL)
	if lerr != nil {
		return noop, nil
	}
	if !ok {
		return nil, ErrMergeInProgress
	}
	return func() { _, _ = s.locker.ReleaseLock(context.Background(), key, token) }, nil
}

// GetProgress 断点续传进度查询（只读：两次索引查询，不碰存储/配额）：
// 已取消会话伪装 404；uploaded 升序，空会话 [] 非 nil；missing 为 [0,n) 差集
func (s *ChunkService) GetProgress(ownerID uint64, uploadID string) (*ChunkProgressResult, error) {
	if ownerID == 0 {
		return nil, fmt.Errorf("%w: 用户非法", ErrInvalidParams)
	}
	uploadID = strings.TrimSpace(uploadID)
	if !validUploadID(uploadID) {
		return nil, fmt.Errorf("%w: 会话非法", ErrInvalidParams)
	}
	sess, err := s.chunks.FindSession(uploadID, ownerID)
	if err != nil {
		return nil, err
	}
	if sess.Status == 2 {
		return nil, ErrChunkNotFound
	}
	idx, err := s.chunks.ListUploadedIndexes(uploadID, ownerID)
	if err != nil {
		return nil, err
	}
	sort.Slice(idx, func(i, j int) bool { return idx[i] < idx[j] })
	n := sess.TotalChunks
	in := make(map[uint32]bool, len(idx))
	for _, v := range idx {
		in[v] = true
	}
	missing := make([]uint32, 0, n)
	for i := uint32(0); i < n; i++ {
		if !in[i] {
			missing = append(missing, i)
		}
	}
	if idx == nil {
		idx = []uint32{}
	}
	return &ChunkProgressResult{
		UploadID:    uploadID,
		FileHash:    sess.FileHash,
		TotalChunks: n,
		ChunkSize:   sess.ChunkSize,
		Status:      sess.Status,
		Uploaded:    idx,
		Missing:     missing,
		Complete:    len(missing) == 0 && n > 0,
	}, nil
}

// CancelUpload 会话取消（幂等：重复取消仍 200）：Cancel 置 status=2 + 清 .part 目录和 .merged；
// 不删 storage、不动配额（配额真扣只在 complete 成功时发生）；他人/不存在 404 伪装
func (s *ChunkService) CancelUpload(ownerID uint64, uploadID string) error {
	if ownerID == 0 {
		return fmt.Errorf("%w: 用户非法", ErrInvalidParams)
	}
	uploadID = strings.TrimSpace(uploadID)
	if !validUploadID(uploadID) {
		return fmt.Errorf("%w: 会话非法", ErrInvalidParams)
	}
	if _, err := s.chunks.FindSession(uploadID, ownerID); err != nil {
		return err
	}
	if err := s.chunks.Cancel(uploadID, ownerID); err != nil {
		return err
	}
	s.cleanupSession(uploadID)
	if merged, err := s.mergedTmpPath(uploadID); err == nil {
		_ = os.Remove(merged)
	}
	return nil
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
// tmp 文件名带进程内唯一随机后缀：同 uploadID+index 并发重传时各写各的 tmp，
// Rename 串行生效保证最后一次为准，避免共用 `<abs>.tmp-<pid>` 被 O_TRUNC 互相截断
func (s *ChunkService) savePart(abs string, chunkSize uint64, src io.Reader) (uint64, error) {
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return 0, fmt.Errorf("建分片目录: %w", err)
	}
	var rnd [8]byte
	if _, err := rand.Read(rnd[:]); err != nil {
		return 0, fmt.Errorf("生成临时文件名: %w", err)
	}
	tmp := fmt.Sprintf("%s.tmp-%d-%s", abs, os.Getpid(), hex.EncodeToString(rnd[:]))
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

// mergedTmpPath 合并临时文件路径：tmpRoot/<uploadID>.merged，前缀双保险同 S2
func (s *ChunkService) mergedTmpPath(uploadID string) (string, error) {
	uploadID = strings.TrimSpace(uploadID)
	abs := filepath.Join(s.tmpRoot(), uploadID+".merged")
	cleaned := filepath.Clean(abs)
	root := filepath.Clean(s.tmpRoot())
	if cleaned != root && !strings.HasPrefix(cleaned, root+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: 合并路径非法", ErrInvalidParams)
	}
	return cleaned, nil
}

// cleanupSession 清 .part 暂存目录（成功/Cancel/冲突幂等后调用，失败忽略）
func (s *ChunkService) cleanupSession(uploadID string) {
	_ = os.RemoveAll(filepath.Join(s.tmpRoot(), filepath.FromSlash(strings.TrimSpace(uploadID))))
}

// CompleteUpload 分片合并编排（S8 瘦身为 30 行编排；拿锁失败转 ErrMergeInProgress）
func (s *ChunkService) CompleteUpload(ownerID, parentID uint64, uploadID, fileName string) (*model.File, error) {
	if ownerID == 0 {
		return nil, fmt.Errorf("%w: 用户非法", ErrInvalidParams)
	}
	uploadID = strings.TrimSpace(uploadID)
	if !validUploadID(uploadID) {
		return nil, fmt.Errorf("%w: 会话非法", ErrInvalidParams)
	}
	if !validFileName(fileName) {
		return nil, fmt.Errorf("%w: 文件名非法", ErrInvalidParams)
	}
	if s.store == nil {
		return nil, fmt.Errorf("存储未注入")
	}
	release, err := s.acquireMerge(uploadID)
	if err != nil {
		return nil, err
	}
	defer release()
	sess, canonicalHash, existing, err := s.checkReady(ownerID, parentID, uploadID)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		s.enrichFile(existing, "")
		return existing, nil
	}
	sum, err := s.verifyParts(ownerID, uploadID, sess)
	if err != nil {
		return nil, err
	}
	merged, err := s.mergeFile(uploadID, sess)
	if err != nil {
		return nil, err
	}
	ext, mime, _, err := s.sniffMerged(merged, fileName)
	if err != nil {
		return nil, err
	}
	f, err := s.verifyAndStore(ownerID, parentID, uploadID, fileName, merged, sum, ext, mime, canonicalHash)
	if err != nil {
		return nil, err
	}
	s.enrichFile(f, merged)
	_ = s.chunks.Cancel(uploadID, ownerID)
	s.cleanupSession(uploadID)
	_ = os.Remove(merged)
	return f, nil
}

// checkReady 会话归属 + parent + 幂等短路（命中既有行时 Cancel + 清盘并回 existing）
func (s *ChunkService) checkReady(ownerID, parentID uint64, uploadID string) (*model.UploadChunk, string, *model.File, error) {
	sess, err := s.chunks.FindSession(uploadID, ownerID)
	if err != nil {
		return nil, "", nil, err
	}
	canonicalHash := strings.ToLower(strings.TrimSpace(sess.FileHash))
	if err := s.checkParent(ownerID, parentID); err != nil {
		return nil, "", nil, err
	}
	if f, err := s.files.FindByHash(canonicalHash, ownerID); err != nil {
		return nil, "", nil, err
	} else if f != nil {
		if _, ok := s.store.Exists(canonicalHash); ok {
			_ = s.chunks.Cancel(uploadID, ownerID)
			s.cleanupSession(uploadID)
			return sess, canonicalHash, f, nil
		}
	}
	if sess.Status == 2 {
		return nil, "", nil, ErrChunkNotFound
	}
	return sess, canonicalHash, nil, nil
}

// verifyParts 齐套 + 逐片 Stat（中间片等大/末片合法）+ sum 区间闭环
func (s *ChunkService) verifyParts(ownerID uint64, uploadID string, sess *model.UploadChunk) (uint64, error) {
	n := sess.TotalChunks
	cs := sess.ChunkSize
	rows, err := s.chunks.FindByUploadID(uploadID, ownerID)
	if err != nil {
		return 0, err
	}
	var cnt uint32
	for _, r := range rows {
		if r.ChunkIndex == repository.ChunkSessionMarker {
			continue
		}
		cnt++
	}
	if cnt != n || n == 0 {
		return 0, fmt.Errorf("%w: 分片缺失", ErrInvalidParams)
	}
	var sum uint64
	for i := uint32(0); i < n; i++ {
		if _, err := s.chunks.FindChunk(uploadID, ownerID, i); err != nil {
			return 0, fmt.Errorf("%w: 分片缺失", ErrInvalidParams)
		}
		sz, err := s.statPart(uploadID, i, n, cs)
		if err != nil {
			return 0, err
		}
		sum += sz
	}
	if n > 0 {
		lo := uint64(n-1) * cs
		if sum <= lo || sum > uint64(n)*cs {
			return 0, fmt.Errorf("%w: 分片总和非法", ErrInvalidParams)
		}
	}
	if sum == 0 || sum > s.maxFileSize {
		return 0, fmt.Errorf("%w: 文件大小非法", ErrInvalidParams)
	}
	return sum, nil
}

// statPart 单片 Stat + 大小合法性（中间片须等大，末片 (0,cs]）
func (s *ChunkService) statPart(uploadID string, i, n uint32, cs uint64) (uint64, error) {
	abs, _, err := s.partAbsPath(uploadID, i)
	if err != nil {
		return 0, err
	}
	fi, err := os.Stat(abs)
	if err != nil {
		return 0, fmt.Errorf("%w: 分片缺失", ErrInvalidParams)
	}
	sz := uint64(fi.Size())
	if i < n-1 {
		if sz != cs {
			return 0, fmt.Errorf("%w: 分片大小不一致", ErrInvalidParams)
		}
	} else {
		if sz == 0 || sz > cs {
			return 0, fmt.Errorf("%w: 末片大小非法", ErrInvalidParams)
		}
	}
	return sz, nil
}

// mergeFile 按序合并：边读边 hash，全程流式禁 ReadAll；O_EXCL 防交错写坏 .merged
func (s *ChunkService) mergeFile(uploadID string, sess *model.UploadChunk) (string, error) {
	n := sess.TotalChunks
	merged, err := s.mergedTmpPath(uploadID)
	if err != nil {
		return "", err
	}
	out, err := os.OpenFile(merged, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return "", ErrMergeInProgress
		}
		return "", fmt.Errorf("建合并文件: %w", err)
	}
	h := sha256.New()
	mw := io.MultiWriter(out, h)
	for i := uint32(0); i < n; i++ {
		abs, _, err := s.partAbsPath(uploadID, i)
		if err != nil {
			_ = out.Close()
			_ = os.Remove(merged)
			return "", err
		}
		in, err := os.Open(abs)
		if err != nil {
			_ = out.Close()
			_ = os.Remove(merged)
			return "", fmt.Errorf("%w: 分片缺失", ErrInvalidParams)
		}
		if _, err := io.CopyBuffer(mw, in, make([]byte, 32*1024)); err != nil {
			_ = in.Close()
			_ = out.Close()
			_ = os.Remove(merged)
			return "", fmt.Errorf("合并分片: %w", err)
		}
		_ = in.Close()
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(merged)
		return "", fmt.Errorf("关闭合并文件: %w", err)
	}
	if got := hex.EncodeToString(h.Sum(nil)); !strings.EqualFold(got, sess.FileHash) {
		_ = os.Remove(merged)
		return "", fmt.Errorf("%w: 文件哈希不一致", ErrInvalidParams)
	}
	return merged, nil
}

// sniffMerged 三层校验：扩展名 + MIME/魔数（复用 upload 包），返回 ext/mime/head
func (s *ChunkService) sniffMerged(merged, fileName string) (string, string, []byte, error) {
	ext, err := upload.ValidateExt(fileName)
	if err != nil {
		_ = os.Remove(merged)
		return "", "", nil, fmt.Errorf("%w: %v", ErrInvalidParams, err)
	}
	head := make([]byte, upload.SniffLen)
	mf, err := os.Open(merged)
	if err != nil {
		_ = os.Remove(merged)
		return "", "", nil, fmt.Errorf("打开合并文件: %w", err)
	}
	hn, _ := io.ReadFull(mf, head)
	_ = mf.Close()
	head = head[:hn]
	mime, err := upload.SniffAndCheck(head, ext)
	if err != nil {
		_ = os.Remove(merged)
		return "", "", nil, fmt.Errorf("%w: %v", ErrInvalidParams, err)
	}
	return ext, mime, head, nil
}

// verifyAndStore 配额预检 + Store 复用 + 原子扣 + file 落库（冲突回补）
func (s *ChunkService) verifyAndStore(ownerID, parentID uint64, uploadID, fileName, merged string, sum uint64, ext, mime, canonicalHash string) (*model.File, error) {
	_ = ext
	if err := s.checkQuota(ownerID, sum); err != nil {
		_ = os.Remove(merged)
		return nil, err
	}
	mr, err := os.Open(merged)
	if err != nil {
		_ = os.Remove(merged)
		return nil, fmt.Errorf("打开合并文件: %w", err)
	}
	rel, err := s.store.Store(canonicalHash, mr)
	_ = mr.Close()
	if err != nil {
		_ = os.Remove(merged)
		return nil, fmt.Errorf("落盘失败: %w", err)
	}
	ok, err := s.users.IncrUsedBytesIfEnough(ownerID, sum)
	if err != nil {
		_ = os.Remove(merged)
		return nil, err
	}
	if !ok {
		_ = os.Remove(merged)
		return nil, ErrQuotaExceeded
	}
	f := &model.File{
		FileName: strings.TrimSpace(fileName),
		FileHash: canonicalHash,
		FileSize: sum,
		FilePath: rel,
		MimeType: mime,
		ParentID: parentID,
		OwnerID:  ownerID,
		IsFolder: 0,
	}
	if err := s.files.CreateFile(f); err != nil {
		_ = s.users.DecrUsedBytes(ownerID, sum)
		_ = os.Remove(merged)
		if errors.Is(err, repository.ErrFileConflict) {
			_ = s.chunks.Cancel(uploadID, ownerID)
			s.cleanupSession(uploadID)
			return nil, ErrFileConflict
		}
		return nil, err
	}
	return f, nil
}

// enrichHeadLen 分片 enrichment 头采样（64KB，二进制截断足够，5GB 安全）
const enrichHeadLen = 64 << 10

// enrichFile S8 分类 + 指纹旁路：全吞错（fail-open），merged=="" 时从存储重读
// 不删 merged / 不 Cancel：清盘归 CompleteUpload 编排统一收尾
func (s *ChunkService) enrichFile(f *model.File, merged string) {
	if s.classifier == nil && s.fps == nil {
		return
	}
	head := s.enrichHead(f, merged)
	text := smart.TextSnippet(f.FileName, head, f.MimeType)
	combined := strings.TrimSpace(f.FileName + " " + text)
	cat := smart.ClassifyWithFallback(s.classifier, f.FileName, combined)
	_ = s.files.UpdateByID(f.ID, map[string]any{"category": cat.Category.String(), "confidence": cat.Confidence})
	f.Category = sql.NullString{String: cat.Category.String(), Valid: true}
	f.Confidence = sql.NullFloat64{Float64: cat.Confidence, Valid: true}
	if s.fps != nil {
		s.enrichFingerprint(f, merged, combined)
	}
}

// enrichHead 取 head：merged 在盘读盘，否则从存储重读，失败回空
func (s *ChunkService) enrichHead(f *model.File, merged string) []byte {
	if merged != "" {
		if b := headOf(merged, enrichHeadLen); len(b) > 0 {
			return b
		}
	}
	rc, err := s.store.Open(f.FilePath)
	if err != nil {
		return nil
	}
	defer func() { _ = rc.Close() }()
	buf := make([]byte, enrichHeadLen)
	n, _ := io.ReadFull(rc, buf)
	return buf[:n]
}

// enrichFingerprint 指纹旁路：phash 读流解码，simhash 用 combined；0 指纹跳过
func (s *ChunkService) enrichFingerprint(f *model.File, merged, combined string) {
	algo := smart.PickAlgorithm(f.FileName, f.MimeType)
	var hexStr string
	if algo == "phash" {
		var fp uint64
		var err error
		if merged != "" {
			fp, err = s.phashOfMerged(merged)
		} else {
			fp, err = s.phashOfStored(f)
		}
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

// phashOfMerged 读 merged 盘文件算 pHash
func (s *ChunkService) phashOfMerged(merged string) (uint64, error) {
	in, err := os.Open(merged)
	if err != nil {
		return 0, err
	}
	defer func() { _ = in.Close() }()
	return smart.PHashFromReader(in)
}

// phashOfStored 从存储重读算 pHash（幂等短路行无 merged 时用）
func (s *ChunkService) phashOfStored(f *model.File) (uint64, error) {
	rc, err := s.store.Open(f.FilePath)
	if err != nil {
		return 0, err
	}
	defer func() { _ = rc.Close() }()
	return smart.PHashFromReader(rc)
}

// headOf 读文件前 limit 字节（fail-open：失败回 nil）
func headOf(path string, limit int) []byte {
	in, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer func() { _ = in.Close() }()
	buf := make([]byte, limit)
	n, _ := io.ReadFull(in, buf)
	return buf[:n]
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
