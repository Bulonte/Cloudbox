package upload

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"cloudbox/internal/model"
	"cloudbox/internal/repository"
	"cloudbox/internal/storage"
)

func newUploadService(t *testing.T) *UploadService {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	if err != nil {
		t.Fatalf("open sqlite memory: %v", err)
	}
	if err := db.AutoMigrate(&model.User{}, &model.File{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	st, err := storage.NewLocalStorage(t.TempDir())
	if err != nil {
		t.Fatalf("建存储失败: %v", err)
	}
	ur := repository.NewUserRepository(db)
	db.Create(&model.User{Username: "alice", Email: "alice@f.com", PasswordHash: "h", QuotaBytes: 10737418240})
	db.Create(&model.User{Username: "bob", Email: "bob@f.com", PasswordHash: "h", QuotaBytes: 10737418240})
	s := NewUploadService(repository.NewFileRepository(db), st, MaxSimpleUploadSize)
	return s.WithUsers(ur)
}

// newQuotaService 配额专用：建 quota 指定用户，返回服务与用户 id
func newQuotaService(t *testing.T, quota uint64) (*UploadService, *repository.UserRepository, uint64) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	if err != nil {
		t.Fatalf("open sqlite memory: %v", err)
	}
	if err := db.AutoMigrate(&model.User{}, &model.File{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	st, err := storage.NewLocalStorage(t.TempDir())
	if err != nil {
		t.Fatalf("建存储失败: %v", err)
	}
	ur := repository.NewUserRepository(db)
	db.Create(&model.User{Username: "q", Email: "q@f.com", PasswordHash: "h", QuotaBytes: quota})
	u, err := ur.FindByUsername("q")
	if err != nil {
		t.Fatalf("查配额用户失败: %v", err)
	}
	s := NewUploadService(repository.NewFileRepository(db), st, MaxSimpleUploadSize).WithUsers(ur)
	return s, ur, u.ID
}

func TestUploadOK(t *testing.T) {
	s := newUploadService(t)
	f, err := s.Upload(1, 0, "a.pdf", bytes.NewReader(pdfHead), int64(len(pdfHead)))
	if err != nil {
		t.Fatalf("上传失败: %v", err)
	}
	if f.ID == 0 || f.FileName != "a.pdf" || f.IsFolder != 0 {
		t.Fatalf("回显不对: %+v", f)
	}
	if f.MimeType == "" || f.MimeType == "application/octet-stream" {
		t.Fatalf("MIME 不可判: %q", f.MimeType)
	}
	if f.FileSize != uint64(len(pdfHead)) {
		t.Fatalf("大小不对: %d", f.FileSize)
	}
	if f.FilePath == "" {
		t.Fatal("FilePath 为空")
	}
	// 内容寻址可读回
	rc, err := s.store.Open(f.FilePath)
	if err != nil {
		t.Fatalf("Open 失败: %v", err)
	}
	defer func() { _ = rc.Close() }()
}

func TestUploadReject(t *testing.T) {
	mz := mzHead
	big := make([]byte, MaxSimpleUploadSize+1)
	if _, err := rand.Read(big); err != nil {
		t.Fatalf("随机失败: %v", err)
	}
	cases := []struct {
		name string
		uid  uint64
		file string
		data []byte
		size int64
		want error
	}{
		{"超2MB拒", 1, "a.pdf", big, int64(len(big)), ErrTooLarge},
		{"空文件拒", 1, "a.pdf", nil, 0, ErrTooLarge},
		{"exe拒", 1, "x.exe", mz, int64(len(mz)), ErrUnsupportedExt},
		{"改名攻击拒", 1, "fake.jpg", mz, int64(len(mz)), ErrMimeMismatch},
		{"空名拒", 1, "   ", pdfHead, int64(len(pdfHead)), ErrInvalidName},
		{"斜杠名拒", 1, "a/b.pdf", pdfHead, int64(len(pdfHead)), ErrInvalidName},
		{"零用户拒", 0, "a.pdf", pdfHead, int64(len(pdfHead)), ErrInvalidName},
		{"nil流拒", 1, "a.pdf", nil, -1, nil}, // size 走 ValidateSize 兜底分支另测，此处仅占位跳过
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.name == "nil流拒" {
				if _, err := s_new(t).Upload(tc.uid, 0, tc.file, nil, 10); !errors.Is(err, ErrTooLarge) {
					t.Fatalf("nil 流应 ErrTooLarge, 得到 %v", err)
				}
				return
			}
			s := newUploadService(t)
			if _, err := s.Upload(tc.uid, 0, tc.file, bytes.NewReader(tc.data), tc.size); !errors.Is(err, tc.want) {
				t.Fatalf("应 %v, 得到 %v", tc.want, err)
			}
		})
	}
}

// s_new 每用例独立库，避免 uk_hash 串扰
func s_new(t *testing.T) *UploadService { return newUploadService(t) }

func TestUploadParent(t *testing.T) {
	s := newUploadService(t)
	// 他人夹：先以 uid1 建夹，再以 uid2 挂靠
	// 用仓储直插绕开 service 建夹（upload 包不依赖 service/file.go）
	if err := s.files.CreateFile(&model.File{FileName: "priv", FileHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", OwnerID: 1, IsFolder: 1}); err != nil {
		t.Fatalf("预置夹失败: %v", err)
	}
	priv, err := s.files.FindByID(1, 1)
	if err != nil {
		t.Fatalf("查预置夹失败: %v", err)
	}
	if _, err := s.Upload(2, priv.ID, "a.pdf", bytes.NewReader(pdfHead), int64(len(pdfHead))); !errors.Is(err, ErrFileNotFound) {
		t.Fatalf("挂靠他人夹应 ErrFileNotFound, 得到 %v", err)
	}
	// parent 为文件
	if err := s.files.CreateFile(&model.File{FileName: "p.txt", FileHash: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", OwnerID: 1}); err != nil {
		t.Fatalf("预置文件失败: %v", err)
	}
	plain, err := s.files.FindByID(2, 1)
	if err != nil {
		t.Fatalf("查预置文件失败: %v", err)
	}
	if _, err := s.Upload(1, plain.ID, "a.pdf", bytes.NewReader(pdfHead), int64(len(pdfHead))); !errors.Is(err, ErrInvalidParent) {
		t.Fatalf("parent 为文件应 ErrInvalidParent, 得到 %v", err)
	}
	// 根目录 parent=0 成功
	if _, err := s.Upload(1, 0, "ok.pdf", bytes.NewReader(pdfHead), int64(len(pdfHead))); err != nil {
		t.Fatalf("根上传失败: %v", err)
	}
}

func TestUploadDuplicateHash(t *testing.T) {
	s := newUploadService(t)
	first, err := s.Upload(1, 0, "a.pdf", bytes.NewReader(pdfHead), int64(len(pdfHead)))
	if err != nil {
		t.Fatalf("首次失败: %v", err)
	}
	// S4 被动秒传：本人同内容二次传直接返回既有行，不再报 409
	second, err := s.Upload(1, 0, "b.pdf", bytes.NewReader(pdfHead), int64(len(pdfHead)))
	if err != nil {
		t.Fatalf("二次应被动秒传直返, 得到 %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("被动秒传应同 id: %d vs %d", second.ID, first.ID)
	}
	// 跨用户同 hash：uk_hash 全局唯一仍报冲突（降级口径，前端凭 instant-check exists_global 提示跳过）
	if _, err := s.Upload(2, 0, "c.pdf", bytes.NewReader(pdfHead), int64(len(pdfHead))); !errors.Is(err, ErrFileConflict) {
		t.Fatalf("跨用户同 hash 应 ErrFileConflict, 得到 %v", err)
	}
}

func TestInstantCheck(t *testing.T) {
	s := newUploadService(t)
	sum := sha256.Sum256(pdfHead)
	hash := hex.EncodeToString(sum[:])
	// 全 miss
	res, err := s.InstantCheck(1, 0, "a.pdf", hash)
	if err != nil || res.Hit || res.ExistsGlobal || res.File != nil {
		t.Fatalf("全新应双 false: %+v %v", res, err)
	}
	// 非法 hash/文件名/用户
	if _, err := s.InstantCheck(1, 0, "a.pdf", "zzz"); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("坏hash应 ErrInvalidName, 得到 %v", err)
	}
	if _, err := s.InstantCheck(1, 0, "   ", hash); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("空名应 ErrInvalidName, 得到 %v", err)
	}
	if _, err := s.InstantCheck(0, 0, "a.pdf", hash); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("零用户应 ErrInvalidName, 得到 %v", err)
	}
	// 坏 parent → 404 系
	if _, err := s.InstantCheck(1, 999999, "a.pdf", hash); !errors.Is(err, ErrFileNotFound) {
		t.Fatalf("坏parent应 ErrFileNotFound, 得到 %v", err)
	}
	// 上传后本人命中
	f, err := s.Upload(1, 0, "a.pdf", bytes.NewReader(pdfHead), int64(len(pdfHead)))
	if err != nil {
		t.Fatalf("上传失败: %v", err)
	}
	res, err = s.InstantCheck(1, 0, "a.pdf", hash)
	if err != nil || !res.Hit || res.File == nil || res.File.ID != f.ID {
		t.Fatalf("本人应命中: %+v %v", res, err)
	}
	// 大小写兼容
	res, err = s.InstantCheck(1, 0, "a.pdf", strings.ToUpper(hash))
	if err != nil || !res.Hit {
		t.Fatalf("大写hash应命中: %+v %v", res, err)
	}
	// 他人全局命中（uk 全局唯一，实传仍 409，前端凭此提示跳过）
	res, err = s.InstantCheck(2, 0, "b.pdf", hash)
	if err != nil || res.Hit || !res.ExistsGlobal || res.File != nil {
		t.Fatalf("他人应 exists_global: %+v %v", res, err)
	}
	// 孤儿引用（盘删了）当 miss
	if err := s.store.Delete(f.FilePath); err != nil {
		t.Fatalf("删盘失败: %v", err)
	}
	res, err = s.InstantCheck(2, 0, "b.pdf", hash)
	if err != nil || res.Hit || res.ExistsGlobal {
		t.Fatalf("孤儿应 miss: %+v %v", res, err)
	}
}

func TestUploadLongChineseFileName(t *testing.T) {
	// 86 汉字（≈258 字节）：按 rune 计 86+4（.pdf）≤255 应放行（Bugbot 回归）
	name := strings.Repeat("测", 86) + ".pdf"
	if !validFileName(name) {
		t.Fatalf("86 汉字文件名应放行")
	}
	s := newUploadService(t)
	if _, err := s.Upload(1, 0, name, bytes.NewReader(pdfHead), int64(len(pdfHead))); err != nil {
		t.Fatalf("中文长名上传失败: %v", err)
	}
	// 252 汉字 + .pdf = 256 rune 应拒
	tooLong := strings.Repeat("测", 252) + ".pdf"
	if validFileName(tooLong) {
		t.Fatalf("256 rune 应拒")
	}
}

func TestUploadQuota(t *testing.T) {
	// quota=100：pdfHead 15B 成功扣 15；再传 90B（15+90=105 超额）→ ErrQuotaExceeded 且 used 不变
	s, ur, uid := newQuotaService(t, 100)
	f, err := s.Upload(uid, 0, "a.pdf", bytes.NewReader(pdfHead), int64(len(pdfHead)))
	if err != nil {
		t.Fatalf("首次应成功: %v", err)
	}
	if f.FileSize != uint64(len(pdfHead)) {
		t.Fatalf("大小不对: %d", f.FileSize)
	}
	u, _ := ur.FindByID(uid)
	if u.UsedBytes != uint64(len(pdfHead)) {
		t.Fatalf("配额应扣 %d, 得到 %d", len(pdfHead), u.UsedBytes)
	}
	// 90 字节内容：pdf 头 + 填充至 90，保证三层校验过
	big := append(append([]byte{}, pdfHead...), bytes.Repeat([]byte("x"), 90-len(pdfHead))...)
	if _, err := s.Upload(uid, 0, "b.pdf", bytes.NewReader(big), int64(len(big))); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("超额应 ErrQuotaExceeded, 得到 %v", err)
	}
	u2, _ := ur.FindByID(uid)
	if u2.UsedBytes != u.UsedBytes {
		t.Fatalf("超额不应扣: %d vs %d", u2.UsedBytes, u.UsedBytes)
	}
	// 声明 size 谎报：实际 15 但声明 10，仍按 actual 卡（quota=10 时 15 应拒）
	s2, _, uid2 := newQuotaService(t, 10)
	if _, err := s2.Upload(uid2, 0, "a.pdf", bytes.NewReader(pdfHead), 10); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("actual超额应拒, 得到 %v", err)
	}
}
