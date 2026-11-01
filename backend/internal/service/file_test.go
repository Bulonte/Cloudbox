package service

import (
	"errors"
	"strings"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"cloudbox/internal/model"
	"cloudbox/internal/repository"
)

func newFileService(t *testing.T) (*FileService, func()) {
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
	return NewFileService(repository.NewFileRepository(db)), func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	}
}

func TestFileServiceCreateFolder(t *testing.T) {
	svc, cleanup := newFileService(t)
	defer cleanup()

	f, err := svc.CreateFolder(1, "docs", 0)
	if err != nil {
		t.Fatalf("建夹失败: %v", err)
	}
	if f.IsFolder != 1 || f.FileName != "docs" {
		t.Fatalf("建夹不对: %+v", f)
	}

	// 非法名
	for _, name := range []string{"", "   ", "a/b", "a\\b", string(make([]byte, 300))} {
		if _, err := svc.CreateFolder(1, name, 0); !errors.Is(err, ErrInvalidParams) {
			t.Fatalf("名 %q 应 ErrInvalidParams, 得到 %v", name, err)
		}
	}

	// 父为文件
	plain := &model.File{FileName: "p.txt", FileHash: "h-p", OwnerID: 1}
	if err := svc.files.CreateFile(plain); err != nil {
		t.Fatalf("预置失败: %v", err)
	}
	if _, err := svc.CreateFolder(1, "sub", plain.ID); !errors.Is(err, ErrInvalidParent) {
		t.Fatalf("父为文件应 ErrInvalidParent, 得到 %v", err)
	}
	// 父为他人夹
	if _, err := svc.CreateFolder(2, "sub", f.ID); !errors.Is(err, ErrFileNotFound) {
		t.Fatalf("挂靠他人应 ErrFileNotFound, 得到 %v", err)
	}
}

func TestFileServiceRenameMoveDelete(t *testing.T) {
	svc, cleanup := newFileService(t)
	defer cleanup()

	dir, err := svc.CreateFolder(1, "d", 0)
	if err != nil {
		t.Fatalf("建夹失败: %v", err)
	}
	target, err := svc.CreateFolder(1, "t", 0)
	if err != nil {
		t.Fatalf("建夹失败: %v", err)
	}

	got, err := svc.Rename(1, dir.ID, "d2")
	if err != nil || got.FileName != "d2" {
		t.Fatalf("改名失败: %+v %v", got, err)
	}
	if _, err := svc.Rename(1, dir.ID, ""); !errors.Is(err, ErrInvalidParams) {
		t.Fatalf("空名应 ErrInvalidParams, 得到 %v", err)
	}

	moved, err := svc.Move(1, dir.ID, target.ID)
	if err != nil || moved.ParentID != target.ID {
		t.Fatalf("移动失败: %+v %v", moved, err)
	}
	if err := svc.Delete(1, dir.ID); err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	if err := svc.Delete(1, dir.ID); !errors.Is(err, ErrFileNotFound) {
		t.Fatalf("二次删应 ErrFileNotFound, 得到 %v", err)
	}
	if err := svc.Delete(2, target.ID); !errors.Is(err, ErrFileNotFound) {
		t.Fatalf("他人删应 ErrFileNotFound, 得到 %v", err)
	}
}

func TestFileServiceList(t *testing.T) {
	svc, cleanup := newFileService(t)
	defer cleanup()

	if _, err := svc.CreateFolder(1, "a", 0); err != nil {
		t.Fatalf("建夹失败: %v", err)
	}
	if _, err := svc.CreateFolder(1, "b", 0); err != nil {
		t.Fatalf("建夹失败: %v", err)
	}

	files, total, page, size, err := svc.List(1, 0, 0, 0)
	if err != nil || total != 2 || page != 1 || size != 20 || len(files) != 2 {
		t.Fatalf("缺省分页不对: total=%d page=%d size=%d len=%d err=%v", total, page, size, len(files), err)
	}
	_, _, _, size, err = svc.List(1, 0, 1, 5000)
	if err != nil || size != 1000 {
		t.Fatalf("超大size应截1000, size=%d err=%v", size, err)
	}
	files, total, _, _, err = svc.List(1, 0, 99, 20)
	if err != nil || total != 2 || len(files) != 0 {
		t.Fatalf("超大page应空页: total=%d len=%d err=%v", total, len(files), err)
	}
	_, total, _, _, err = svc.List(2, 0, 1, 20)
	if err != nil || total != 0 {
		t.Fatalf("他人应 total=0, total=%d err=%v", total, err)
	}
}

func TestFileNameRuneLimit(t *testing.T) {
	// 86 汉字 ≈258 字节：按 rune 计 86≤255 应放行（Bugbot 回归：旧 len 按字节会误拒）
	longCN := strings.Repeat("测", 100)
	if !validFileName(longCN) {
		t.Fatalf("100 汉字文件名应放行")
	}
	if _, err := newFileServiceForRune(t, longCN); err != nil {
		t.Fatalf("100 汉字建夹失败: %v", err)
	}
	// 256 rune 无论中英文都拒
	if validFileName(strings.Repeat("a", 256)) {
		t.Fatalf("256 ASCII 应拒")
	}
	if validFileName(strings.Repeat("测", 256)) {
		t.Fatalf("256 汉字应拒")
	}
}

func newFileServiceForRune(t *testing.T, name string) (*model.File, error) {
	svc, cleanup := newFileService(t)
	defer cleanup()
	return svc.CreateFolder(1, name, 0)
}
