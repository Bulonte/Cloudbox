package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"cloudbox/internal/model"
	"cloudbox/internal/repository"
	"cloudbox/internal/storage"
)

func newChunkService(t *testing.T) *ChunkService {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	if err != nil {
		t.Fatalf("open sqlite memory: %v", err)
	}
	if err := db.AutoMigrate(&model.User{}, &model.File{}, &model.UploadChunk{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	db.Create(&model.User{Username: "alice", Email: "alice@c.com", PasswordHash: "h", QuotaBytes: 10737418240})
	db.Create(&model.User{Username: "bob", Email: "bob@c.com", PasswordHash: "h", QuotaBytes: 100})
	tmp := t.TempDir()
	st, err := storage.NewLocalStorage(filepath.Join(tmp, "store"))
	if err != nil {
		t.Fatalf("建测试存储失败: %v", err)
	}
	return NewChunkService(repository.NewFileRepository(db), repository.NewUserRepository(db), repository.NewChunkRepository(db), DefaultMaxFileSize, DefaultMaxChunks).WithTmpDir(filepath.Join(tmp, "chunks")).WithStorage(st)
}

func TestChunkInitOK(t *testing.T) {
	svc := newChunkService(t)
	hash := strings.Repeat("a", 64)
	row, err := svc.InitUpload(1, 0, "a.zip", hash, 5*2097152, 5, 2097152)
	if err != nil {
		t.Fatalf("init 失败: %v", err)
	}
	if len(row.UploadID) != 36 || row.Status != 0 {
		t.Fatalf("会话不对: %+v", row)
	}
}

func TestChunkInitReject(t *testing.T) {
	svc := newChunkService(t)
	good := strings.Repeat("a", 64)
	badHex := strings.Repeat("z", 64)
	cases := []struct {
		name string
		uid  uint64
		fn   string
		hash string
		size uint64
		n    uint32
		cs   uint64
		want error
	}{
		{"零用户", 0, "a.zip", good, 2097152, 1, 2097152, ErrInvalidParams},
		{"空名", 1, "   ", good, 2097152, 1, 2097152, ErrInvalidParams},
		{"256rune名拒", 1, strings.Repeat("测", 256), good, 2097152, 1, 2097152, ErrInvalidParams},
		{"100汉字名放行-转hash错", 1, strings.Repeat("测", 100), badHex, 2097152, 1, 2097152, ErrInvalidParams},
		{"hash短", 1, "a.zip", "abc", 2097152, 1, 2097152, ErrInvalidParams},
		{"hash非hex", 1, "a.zip", badHex, 2097152, 1, 2097152, ErrInvalidParams},
		{"size零", 1, "a.zip", good, 0, 1, 2097152, ErrInvalidParams},
		{"size超5G", 1, "a.zip", good, uint64(DefaultMaxFileSize) + 1, 10000, 8 * 1024 * 1024, ErrInvalidParams},
		{"片零", 1, "a.zip", good, 2097152, 1, 0, ErrInvalidParams},
		{"片超8M", 1, "a.zip", good, 9 * 1024 * 1024, 2, 9 * 1024 * 1024, ErrInvalidParams},
		{"总数零", 1, "a.zip", good, 2097152, 0, 2097152, ErrInvalidParams},
		{"总数对不上", 1, "a.zip", good, 2097152, 2, 2097152, ErrInvalidParams},
		{"配额不足", 2, "a.zip", good, 2097152, 1, 2097152, ErrQuotaExceeded},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.InitUpload(tc.uid, 0, tc.fn, tc.hash, tc.size, tc.n, tc.cs)
			if !errors.Is(err, tc.want) {
				t.Fatalf("应 %v, 得到 %v", tc.want, err)
			}
		})
	}
	// 100 汉字名 + 合法 hash 应放行
	if _, err := svc.InitUpload(1, 0, strings.Repeat("测", 100)+".zip", good, 2097152, 1, 2097152); err != nil {
		t.Fatalf("100 汉字名应放行: %v", err)
	}
	// 配额边界：used+total==quota 放行（bob quota=100，新开 100 字节 ok）
	svc2 := newChunkService(t)
	svc2.users.FindByID(2)
	if _, err := svc2.InitUpload(2, 0, "b.zip", good, 100, 1, 100); err != nil {
		t.Fatalf("配额边界应放行: %v", err)
	}
}

func TestChunkInitParent(t *testing.T) {
	svc := newChunkService(t)
	hash := strings.Repeat("b", 64)
	// 父为文件
	if err := svc.files.CreateFile(&model.File{FileName: "p.txt", FileHash: hash, OwnerID: 1}); err != nil {
		t.Fatalf("预置失败: %v", err)
	}
	plain, _ := svc.files.FindByID(1, 1)
	if _, err := svc.InitUpload(1, plain.ID, "a.zip", strings.Repeat("c", 64), 100, 1, 100); !errors.Is(err, ErrInvalidParent) {
		t.Fatalf("父为文件应 ErrInvalidParent, 得到 %v", err)
	}
	// 挂靠他人夹
	dir, err := svc.InitUpload(1, 0, "d.zip", strings.Repeat("d", 64), 100, 1, 100)
	if err != nil {
		t.Fatalf("init 失败: %v", err)
	}
	_ = dir
	folder := &model.File{FileName: "priv", FileHash: strings.Repeat("e", 64), OwnerID: 1, IsFolder: 1}
	if err := svc.files.CreateFile(folder); err != nil {
		t.Fatalf("预置夹失败: %v", err)
	}
	if _, err := svc.InitUpload(2, folder.ID, "a.zip", strings.Repeat("f", 64), 100, 1, 100); !errors.Is(err, ErrInvalidParent) && !errors.Is(err, ErrFileNotFound) {
		t.Fatalf("挂靠他人夹应 404 系, 得到 %v", err)
	}
}

func TestChunkUploadOKAndIdempotent(t *testing.T) {
	svc := newChunkService(t)
	hash := strings.Repeat("a", 64)
	row, err := svc.InitUpload(1, 0, "a.zip", hash, 2*2097152, 2, 2097152)
	if err != nil {
		t.Fatalf("init 失败: %v", err)
	}
	// 正常片 0
	data0 := bytes.Repeat([]byte("x"), 100)
	res, err := svc.UploadChunk(1, row.UploadID, 0, bytes.NewReader(data0), int64(len(data0)))
	if err != nil {
		t.Fatalf("传片0失败: %v", err)
	}
	if res.Size != uint64(len(data0)) || res.ChunkIndex != 0 {
		t.Fatalf("回显不对: %+v", res)
	}
	abs := filepath.Join(svc.tmpRoot(), row.UploadID, "0.part")
	got, err := os.ReadFile(abs)
	if err != nil || !bytes.Equal(got, data0) {
		t.Fatalf("落盘不对: %v len=%d", err, len(got))
	}
	// 末片小于 cs 放行
	data1 := bytes.Repeat([]byte("y"), 100)
	if _, err := svc.UploadChunk(1, row.UploadID, 1, bytes.NewReader(data1), int64(len(data1))); err != nil {
		t.Fatalf("末片失败: %v", err)
	}
	// 同 index 重传覆盖
	data0b := bytes.Repeat([]byte("z"), 200)
	if _, err := svc.UploadChunk(1, row.UploadID, 0, bytes.NewReader(data0b), int64(len(data0b))); err != nil {
		t.Fatalf("重传失败: %v", err)
	}
	got, _ = os.ReadFile(abs)
	if !bytes.Equal(got, data0b) {
		t.Fatalf("重传未覆盖: len=%d", len(got))
	}
	// DB 仍幂等单行
	idx, err := svc.chunks.ListUploadedIndexes(row.UploadID, 1)
	if err != nil || len(idx) != 2 {
		t.Fatalf("进度应2片: %v %v", idx, err)
	}
}

func TestChunkUploadReject(t *testing.T) {
	svc := newChunkService(t)
	hash := strings.Repeat("b", 64)
	row, err := svc.InitUpload(1, 0, "a.zip", hash, 2097152, 1, 2097152)
	if err != nil {
		t.Fatalf("init 失败: %v", err)
	}
	cases := []struct {
		name string
		uid  uint64
		id   string
		idx  uint32
		data []byte
		size int64
		want error
	}{
		{"零用户", 0, row.UploadID, 0, []byte("x"), 1, ErrInvalidParams},
		{"坏id", 1, "not-uuid", 0, []byte("x"), 1, ErrInvalidParams},
		{"穿越id", 1, "../etc/passwd-12345678901234567890", 0, []byte("x"), 1, ErrInvalidParams},
		{"无会话", 1, "33333333-3333-4333-8333-333333333333", 0, []byte("x"), 1, ErrChunkNotFound},
		{"他人会话", 2, row.UploadID, 0, []byte("x"), 1, ErrChunkNotFound},
		{"序号越界", 1, row.UploadID, 1, []byte("x"), 1, ErrInvalidParams},
		{"序号占位", 1, row.UploadID, repository.ChunkSessionMarker, []byte("x"), 1, ErrInvalidParams},
		{"空片", 1, row.UploadID, 0, []byte{}, 0, ErrInvalidParams},
		{"超cs", 1, row.UploadID, 0, bytes.Repeat([]byte("x"), 100), 2097153, ErrInvalidParams},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var src *bytes.Reader
			if tc.data != nil {
				src = bytes.NewReader(tc.data)
			}
			var r io.Reader
			if src != nil {
				r = src
			}
			_, err := svc.UploadChunk(tc.uid, tc.id, tc.idx, r, tc.size)
			if !errors.Is(err, tc.want) {
				t.Fatalf("应 %v, 得到 %v", tc.want, err)
			}
		})
	}
	// nil 流
	if _, err := svc.UploadChunk(1, row.UploadID, 0, nil, 10); !errors.Is(err, ErrInvalidParams) {
		t.Fatalf("nil流应参错, 得到 %v", err)
	}
}

// buildCompleteSession 按内容切分并上传全部分片，返回会话与原文
func buildCompleteSession(t *testing.T, svc *ChunkService, owner uint64, fileName string, content []byte, chunkSize uint64) (*model.UploadChunk, []byte) {
	t.Helper()
	sum := sha256.Sum256(content)
	hash := hex.EncodeToString(sum[:])
	total := uint64(len(content))
	n := uint32((total + chunkSize - 1) / chunkSize)
	row, err := svc.InitUpload(owner, 0, fileName, hash, total, n, chunkSize)
	if err != nil {
		t.Fatalf("init 失败: %v", err)
	}
	for i := uint32(0); i < n; i++ {
		s, e := uint64(i)*chunkSize, uint64(i+1)*chunkSize
		if e > total {
			e = total
		}
		part := content[s:e]
		if _, err := svc.UploadChunk(owner, row.UploadID, i, bytes.NewReader(part), int64(len(part))); err != nil {
			t.Fatalf("传片 %d 失败: %v", i, err)
		}
	}
	return row, content
}

func TestChunkCompleteOK(t *testing.T) {
	svc := newChunkService(t)
	content := []byte("hello-chunk-merge-world-0123456789-abcdef")
	row, _ := buildCompleteSession(t, svc, 1, "a.txt", content, 10)
	f, err := svc.CompleteUpload(1, 0, row.UploadID, "a.txt")
	if err != nil {
		t.Fatalf("合并失败: %v", err)
	}
	if f.FileSize != uint64(len(content)) || f.MimeType == "" || f.IsFolder != 0 {
		t.Fatalf("落库不对: %+v", f)
	}
	sum := sha256.Sum256(content)
	if f.FileHash != hex.EncodeToString(sum[:]) {
		t.Fatalf("hash不对: %s", f.FileHash)
	}
	// 配额真扣
	u, _ := svc.users.FindByID(1)
	if u.UsedBytes != uint64(len(content)) {
		t.Fatalf("配额未扣: %d", u.UsedBytes)
	}
	// 暂存清盘
	if _, err := os.Stat(filepath.Join(svc.tmpRoot(), row.UploadID)); !os.IsNotExist(err) {
		t.Fatalf("分片目录应清理: %v", err)
	}
	if _, err := os.Stat(filepath.Join(svc.tmpRoot(), row.UploadID+".merged")); !os.IsNotExist(err) {
		t.Fatalf("合并临时应清理: %v", err)
	}
	// 重复 complete 幂等：不二次扣配额
	f2, err := svc.CompleteUpload(1, 0, row.UploadID, "a.txt")
	if err != nil || f2.ID != f.ID {
		t.Fatalf("重复合并应幂等: %+v %v", f2, err)
	}
	u2, _ := svc.users.FindByID(1)
	if u2.UsedBytes != u.UsedBytes {
		t.Fatalf("幂等不应二次扣: %d vs %d", u2.UsedBytes, u.UsedBytes)
	}
}

func TestChunkCompleteReject(t *testing.T) {
	// 缺片
	svc := newChunkService(t)
	content := []byte("0123456789abcdefghij0123456789")
	sum := sha256.Sum256(content)
	hash := hex.EncodeToString(sum[:])
	row, err := svc.InitUpload(1, 0, "a.txt", hash, uint64(len(content)), 3, 10)
	if err != nil {
		t.Fatalf("init 失败: %v", err)
	}
	if _, err := svc.UploadChunk(1, row.UploadID, 0, bytes.NewReader(content[:10]), 10); err != nil {
		t.Fatalf("传片失败: %v", err)
	}
	if _, err := svc.CompleteUpload(1, 0, row.UploadID, "a.txt"); !errors.Is(err, ErrInvalidParams) {
		t.Fatalf("缺片应参错, 得到 %v", err)
	}
	// 会话不存在 / 他人会话
	if _, err := svc.CompleteUpload(1, 0, "33333333-3333-4333-8333-333333333333", "a.txt"); !errors.Is(err, ErrChunkNotFound) {
		t.Fatalf("无会话应 404, 得到 %v", err)
	}
	svc2 := newChunkService(t)
	r2, _ := buildCompleteSession(t, svc2, 1, "a.txt", content, 10)
	_ = r2
	// 他人会话用另一实例同 uploadID 不存在 → 404（跨实例隔离）
	if _, err := svc.CompleteUpload(2, 0, row.UploadID, "a.txt"); !errors.Is(err, ErrChunkNotFound) {
		t.Fatalf("他人会话应 404, 得到 %v", err)
	}
	// 非法文件名 / 非法 parent / 非法 id
	svc3 := newChunkService(t)
	r3, _ := buildCompleteSession(t, svc3, 1, "a.txt", content, 10)
	if _, err := svc3.CompleteUpload(1, 0, r3.UploadID, "bad/name.txt"); !errors.Is(err, ErrInvalidParams) {
		t.Fatalf("坏名应参错, 得到 %v", err)
	}
	if _, err := svc3.CompleteUpload(1, 999999, r3.UploadID, "a.txt"); !errors.Is(err, ErrInvalidParent) && !errors.Is(err, ErrFileNotFound) {
		t.Fatalf("坏parent应 404 系, 得到 %v", err)
	}
	if _, err := svc3.CompleteUpload(1, 0, "not-uuid", "a.txt"); !errors.Is(err, ErrInvalidParams) {
		t.Fatalf("坏id应参错, 得到 %v", err)
	}
	// hash 不一致：声明 A 内容传 B
	svc4 := newChunkService(t)
	hashA := strings.Repeat("a", 64)
	r4, err := svc4.InitUpload(1, 0, "a.txt", hashA, 20, 2, 10)
	if err != nil {
		t.Fatalf("init 失败: %v", err)
	}
	for i := uint32(0); i < 2; i++ {
		if _, err := svc4.UploadChunk(1, r4.UploadID, i, bytes.NewReader([]byte("0123456789")), 10); err != nil {
			t.Fatalf("传片失败: %v", err)
		}
	}
	if _, err := svc4.CompleteUpload(1, 0, r4.UploadID, "a.txt"); !errors.Is(err, ErrInvalidParams) {
		t.Fatalf("hash不一致应参错, 得到 %v", err)
	}
	// 三层校验：exe 扩展名 / 改名攻击（文本内容叫 .pdf 应过，pdf 内容叫 .txt 也过，但 exe 必拒）
	svc5 := newChunkService(t)
	r5, _ := buildCompleteSession(t, svc5, 1, "a.txt", content, 10)
	if _, err := svc5.CompleteUpload(1, 0, r5.UploadID, "evil.exe"); !errors.Is(err, ErrInvalidParams) {
		t.Fatalf("exe应参错, 得到 %v", err)
	}
	// 配额原子扣失败：bob quota=100，先占 80 再合 50 → 30002 系
	svc6 := newChunkService(t)
	c50 := bytes.Repeat([]byte("x"), 50)
	r6, _ := buildCompleteSession(t, svc6, 2, "a.txt", c50, 10)
	if _, err := svc6.users.IncrUsedBytesIfEnough(2, 80); err != nil {
		t.Fatalf("预占失败: %v", err)
	}
	if _, err := svc6.CompleteUpload(2, 0, r6.UploadID, "a.txt"); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("超配额应 ErrQuotaExceeded, 得到 %v", err)
	}
	// 全局 uk_hash 冲突 → ErrFileConflict（他人已存同 hash）
	svc7 := newChunkService(t)
	c7 := []byte("conflict-content-0123456789-abcdefghij")
	sum7 := sha256.Sum256(c7)
	hash7 := hex.EncodeToString(sum7[:])
	if err := svc7.files.CreateFile(&model.File{FileName: "other.txt", FileHash: hash7, OwnerID: 2}); err != nil {
		t.Fatalf("预置冲突行失败: %v", err)
	}
	r7, err := svc7.InitUpload(1, 0, "a.txt", hash7, uint64(len(c7)), 4, 10)
	if err != nil {
		t.Fatalf("init 失败: %v", err)
	}
	for i := uint32(0); i < 4; i++ {
		s, e := uint64(i)*10, uint64(i+1)*10
		if e > uint64(len(c7)) {
			e = uint64(len(c7))
		}
		if _, err := svc7.UploadChunk(1, r7.UploadID, i, bytes.NewReader(c7[s:e]), int64(len(c7[s:e]))); err != nil {
			t.Fatalf("传片失败: %v", err)
		}
	}
	if _, err := svc7.CompleteUpload(1, 0, r7.UploadID, "a.txt"); !errors.Is(err, ErrFileConflict) {
		t.Fatalf("uk冲突应 ErrFileConflict, 得到 %v", err)
	}
	u7, _ := svc7.users.FindByID(1)
	if u7.UsedBytes != 0 {
		t.Fatalf("冲突应回补配额: used=%d", u7.UsedBytes)
	}
}

// stubHeldLocker 模拟锁被他人持有：Acquire 恒返回 ok=false，转 ErrMergeInProgress
type stubHeldLocker struct{}

func (stubHeldLocker) AcquireLock(context.Context, string, time.Duration) (string, bool, error) {
	return "", false, nil
}
func (stubHeldLocker) ReleaseLock(context.Context, string, string) (bool, error) { return false, nil }

// stubErrLocker 模拟 Redis 宕机：Acquire 恒返回 error，应降级放行合并成功
type stubErrLocker struct{ err error }

func (s stubErrLocker) AcquireLock(context.Context, string, time.Duration) (string, bool, error) {
	return "", false, s.err
}
func (stubErrLocker) ReleaseLock(context.Context, string, string) (bool, error) {
	return false, nil
}

func TestGetProgress(t *testing.T) {
	svc := newChunkService(t)
	row, err := svc.InitUpload(1, 0, "a.zip", strings.Repeat("a", 64), 3*10, 3, 10)
	if err != nil {
		t.Fatalf("init 失败: %v", err)
	}
	// 空会话：uploaded=[] 非 nil，missing 全量，complete=false
	p, err := svc.GetProgress(1, row.UploadID)
	if err != nil {
		t.Fatalf("progress 失败: %v", err)
	}
	if p.Uploaded == nil || len(p.Uploaded) != 0 || len(p.Missing) != 3 || p.Complete {
		t.Fatalf("空进度不对: %+v", p)
	}
	// 传 0,2 后：uploaded 升序，missing=[1]
	if _, err := svc.UploadChunk(1, row.UploadID, 2, bytes.NewReader(bytes.Repeat([]byte("y"), 5)), 5); err != nil {
		t.Fatalf("传片2失败: %v", err)
	}
	if _, err := svc.UploadChunk(1, row.UploadID, 0, bytes.NewReader(bytes.Repeat([]byte("x"), 10)), 10); err != nil {
		t.Fatalf("传片0失败: %v", err)
	}
	p, err = svc.GetProgress(1, row.UploadID)
	if err != nil || len(p.Uploaded) != 2 || p.Uploaded[0] != 0 || p.Uploaded[1] != 2 {
		t.Fatalf("进度不对: %+v %v", p, err)
	}
	if len(p.Missing) != 1 || p.Missing[0] != 1 || p.Complete {
		t.Fatalf("缺片不对: %+v", p)
	}
	// 坏 id / 他人 / 零用户
	if _, err := svc.GetProgress(1, "not-uuid"); !errors.Is(err, ErrInvalidParams) {
		t.Fatalf("坏id应参错, 得到 %v", err)
	}
	if _, err := svc.GetProgress(2, row.UploadID); !errors.Is(err, ErrChunkNotFound) {
		t.Fatalf("他人应 404, 得到 %v", err)
	}
	if _, err := svc.GetProgress(0, row.UploadID); !errors.Is(err, ErrInvalidParams) {
		t.Fatalf("零用户应参错, 得到 %v", err)
	}
}

func TestCancelUpload(t *testing.T) {
	svc := newChunkService(t)
	row, err := svc.InitUpload(1, 0, "a.zip", strings.Repeat("a", 64), 2*10, 2, 10)
	if err != nil {
		t.Fatalf("init 失败: %v", err)
	}
	if _, err := svc.UploadChunk(1, row.UploadID, 0, bytes.NewReader(bytes.Repeat([]byte("x"), 10)), 10); err != nil {
		t.Fatalf("传片失败: %v", err)
	}
	if err := svc.CancelUpload(1, row.UploadID); err != nil {
		t.Fatalf("取消失败: %v", err)
	}
	// status=2，占位行仍可查到底层状态
	sess, err := svc.chunks.FindSession(row.UploadID, 1)
	if err != nil || sess.Status != 2 {
		t.Fatalf("取消后状态不对: %+v %v", sess, err)
	}
	// .part 目录清理
	if _, err := os.Stat(filepath.Join(svc.tmpRoot(), row.UploadID)); !os.IsNotExist(err) {
		t.Fatalf("分片目录应清理: %v", err)
	}
	// 进度查询伪装 404
	if _, err := svc.GetProgress(1, row.UploadID); !errors.Is(err, ErrChunkNotFound) {
		t.Fatalf("取消后进度应 404, 得到 %v", err)
	}
	// 重复取消幂等 200
	if err := svc.CancelUpload(1, row.UploadID); err != nil {
		t.Fatalf("重复取消应幂等: %v", err)
	}
	// 他人取消 404
	if err := svc.CancelUpload(2, row.UploadID); !errors.Is(err, ErrChunkNotFound) {
		t.Fatalf("他人取消应 404, 得到 %v", err)
	}
}

func TestCompleteLock(t *testing.T) {
	// 锁被占：complete 立得 ErrMergeInProgress，不碰配额不落库
	svc := newChunkService(t)
	row, _ := buildCompleteSession(t, svc, 1, "a.txt", []byte("0123456789abcdefghij0123456789"), 10)
	svc.WithLocker(stubHeldLocker{})
	if _, err := svc.CompleteUpload(1, 0, row.UploadID, "a.txt"); !errors.Is(err, ErrMergeInProgress) {
		t.Fatalf("抢锁失败应 ErrMergeInProgress, 得到 %v", err)
	}
	u, _ := svc.users.FindByID(1)
	if u.UsedBytes != 0 {
		t.Fatalf("抢锁失败不应扣配额: %d", u.UsedBytes)
	}
	// 锁降级：Acquire 出错仍合并成功
	svc2 := newChunkService(t)
	row2, _ := buildCompleteSession(t, svc2, 1, "a.txt", []byte("0123456789abcdefghij0123456789"), 10)
	svc2.WithLocker(stubErrLocker{err: errors.New("redis down")})
	f, err := svc2.CompleteUpload(1, 0, row2.UploadID, "a.txt")
	if err != nil || f.FileSize != 30 {
		t.Fatalf("降级应合并成功: %+v %v", f, err)
	}
}
