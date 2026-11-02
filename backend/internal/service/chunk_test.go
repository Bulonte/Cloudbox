package service

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"cloudbox/internal/model"
	"cloudbox/internal/repository"
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
	return NewChunkService(repository.NewFileRepository(db), repository.NewUserRepository(db), repository.NewChunkRepository(db), DefaultMaxFileSize, DefaultMaxChunks).WithTmpDir(t.TempDir())
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
