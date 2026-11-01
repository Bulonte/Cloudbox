package upload

import (
	"bytes"
	"crypto/rand"
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
	return NewUploadService(repository.NewFileRepository(db), st, MaxSimpleUploadSize)
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
	if _, err := s.Upload(1, 0, "a.pdf", bytes.NewReader(pdfHead), int64(len(pdfHead))); err != nil {
		t.Fatalf("首次失败: %v", err)
	}
	// 同内容二次传：uk_hash 全局唯一 → ErrFileConflict（W5 转被动秒传前按 409 闭合）
	if _, err := s.Upload(1, 0, "b.pdf", bytes.NewReader(pdfHead), int64(len(pdfHead))); !errors.Is(err, ErrFileConflict) {
		t.Fatalf("二次同 hash 应 ErrFileConflict, 得到 %v", err)
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
