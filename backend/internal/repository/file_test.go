package repository

import (
	"errors"
	"testing"

	"cloudbox/internal/model"
)

func newFileRepo(t *testing.T) (*FileRepository, func()) {
	t.Helper()
	db := openTestDB(t)
	return NewFileRepository(db), func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	}
}

func seedFile(t *testing.T, repo *FileRepository, owner uint64, name string, parent uint64, folder int8) *model.File {
	t.Helper()
	f := &model.File{FileName: name, FileHash: "hash-" + name, FilePath: "/store/ha/sh", ParentID: parent, OwnerID: owner, IsFolder: folder}
	if err := repo.CreateFile(f); err != nil {
		t.Fatalf("创建文件失败: %v", err)
	}
	return f
}

func TestFileRepoCreateFind(t *testing.T) {
	repo, cleanup := newFileRepo(t)
	defer cleanup()

	f := seedFile(t, repo, 1, "a.txt", 0, 0)
	got, err := repo.FindByID(f.ID, 1)
	if err != nil {
		t.Fatalf("回查失败: %v", err)
	}
	if got.FileName != "a.txt" || got.MimeType != "" {
		t.Fatalf("回查字段不对: %+v", got)
	}

	// 同 hash 第二次插入报冲突（uk_hash 语义）
	dup := &model.File{FileName: "b.txt", FileHash: "hash-a.txt", FilePath: "/x", OwnerID: 2}
	if err := repo.CreateFile(dup); !errors.Is(err, ErrFileConflict) {
		t.Fatalf("同 hash 应 ErrFileConflict, 得到 %v", err)
	}

	// 查不存在的 ID
	if _, err := repo.FindByID(999999, 1); !errors.Is(err, ErrFileNotFound) {
		t.Fatalf("查不存在应 ErrFileNotFound, 得到 %v", err)
	}
}

func TestFileRepoOwnership(t *testing.T) {
	repo, cleanup := newFileRepo(t)
	defer cleanup()

	f := seedFile(t, repo, 1, "secret.txt", 0, 0)

	// SEC-01 前置：B 查 A 的文件一律 ErrFileNotFound（不暴露存在性）
	for _, op := range []struct {
		name string
		call func() error
	}{
		{"find", func() error { _, err := repo.FindByID(f.ID, 2); return err }},
		{"rename", func() error { return repo.Rename(f.ID, 2, "x") }},
		{"move", func() error { return repo.Move(f.ID, 2, 0) }},
		{"delete", func() error { return repo.SoftDelete(f.ID, 2) }},
	} {
		if err := op.call(); !errors.Is(err, ErrFileNotFound) {
			t.Fatalf("%s 越权应 ErrFileNotFound, 得到 %v", op.name, err)
		}
	}
}

func TestFileRepoSoftDelete(t *testing.T) {
	repo, cleanup := newFileRepo(t)
	defer cleanup()

	f := seedFile(t, repo, 1, "gone.txt", 0, 0)
	if err := repo.SoftDelete(f.ID, 1); err != nil {
		t.Fatalf("软删失败: %v", err)
	}
	// 删后不可见
	if _, err := repo.FindByID(f.ID, 1); !errors.Is(err, ErrFileNotFound) {
		t.Fatalf("删后应不可见, 得到 %v", err)
	}
	// 二次删转 ErrFileNotFound（S2 统一转 404）
	if err := repo.SoftDelete(f.ID, 1); !errors.Is(err, ErrFileNotFound) {
		t.Fatalf("二次删应 ErrFileNotFound, 得到 %v", err)
	}
	// 列表也不见
	list, total, err := repo.ListByParent(1, 0, 0, 20)
	if err != nil || total != 0 || len(list) != 0 {
		t.Fatalf("删后列表应空, total=%d list=%v err=%v", total, list, err)
	}
}

func TestFileRepoRenameMove(t *testing.T) {
	repo, cleanup := newFileRepo(t)
	defer cleanup()

	dir := seedFile(t, repo, 1, "docs", 0, 1)
	otherDir := seedFile(t, repo, 2, "other", 0, 1)
	f := seedFile(t, repo, 1, "old.txt", 0, 0)

	if err := repo.Rename(f.ID, 1, "new.txt"); err != nil {
		t.Fatalf("改名失败: %v", err)
	}
	got, _ := repo.FindByID(f.ID, 1)
	if got.FileName != "new.txt" {
		t.Fatalf("改名未生效: %+v", got)
	}
	if err := repo.Rename(999999, 1, "x"); !errors.Is(err, ErrFileNotFound) {
		t.Fatalf("改不存在应 ErrFileNotFound, 得到 %v", err)
	}

	// 正常移动
	if err := repo.Move(f.ID, 1, dir.ID); err != nil {
		t.Fatalf("移动失败: %v", err)
	}
	got, _ = repo.FindByID(f.ID, 1)
	if got.ParentID != dir.ID {
		t.Fatalf("移动未生效: %+v", got)
	}

	// 移到别人的夹 → ErrInvalidParent
	if err := repo.Move(f.ID, 1, otherDir.ID); !errors.Is(err, ErrInvalidParent) {
		t.Fatalf("跨户挂靠应 ErrInvalidParent, 得到 %v", err)
	}
	// 移到不存在的夹 → ErrInvalidParent
	if err := repo.Move(f.ID, 1, 999999); !errors.Is(err, ErrInvalidParent) {
		t.Fatalf("目标不存在应 ErrInvalidParent, 得到 %v", err)
	}
	// 移到文件（非夹）→ ErrInvalidParent
	plain := seedFile(t, repo, 1, "plain.txt", 0, 0)
	if err := repo.Move(f.ID, 1, plain.ID); !errors.Is(err, ErrInvalidParent) {
		t.Fatalf("移到非夹应 ErrInvalidParent, 得到 %v", err)
	}
}

func TestFileRepoList(t *testing.T) {
	repo, cleanup := newFileRepo(t)
	defer cleanup()

	// 文件夹置顶 + id 倒序：先建文件后建夹，列表夹应在前
	seedFile(t, repo, 1, "z.txt", 0, 0)
	seedFile(t, repo, 1, "a.txt", 0, 0)
	dir := seedFile(t, repo, 1, "d", 0, 1)
	seedFile(t, repo, 1, "inner.txt", dir.ID, 0)

	list, total, err := repo.ListByParent(1, 0, 0, 20)
	if err != nil {
		t.Fatalf("列表失败: %v", err)
	}
	if total != 3 || len(list) != 3 {
		t.Fatalf("根目录应 3 项, total=%d list=%d", total, len(list))
	}
	if list[0].IsFolder != 1 {
		t.Fatalf("文件夹应置顶, 首项=%+v", list[0])
	}

	// 分页切片
	page1, total1, err := repo.ListByParent(1, 0, 0, 2)
	if err != nil || total1 != 3 || len(page1) != 2 {
		t.Fatalf("分页1不对: total=%d len=%d err=%v", total1, len(page1), err)
	}
	page2, _, err := repo.ListByParent(1, 0, 2, 2)
	if err != nil || len(page2) != 1 {
		t.Fatalf("分页2不对: len=%d err=%v", len(page2), err)
	}

	// 子目录只见自己的
	sub, subTotal, err := repo.ListByParent(1, dir.ID, 0, 20)
	if err != nil || subTotal != 1 || sub[0].FileName != "inner.txt" {
		t.Fatalf("子目录不对: total=%d sub=%v err=%v", subTotal, sub, err)
	}

	// 他人视角为空
	_, otherTotal, err := repo.ListByParent(2, 0, 0, 20)
	if err != nil || otherTotal != 0 {
		t.Fatalf("他人应看不见: total=%d err=%v", otherTotal, err)
	}

	// limit 上限截断不断言行数，只断言不炸
	if _, _, err := repo.ListByParent(1, 0, -5, 5000); err != nil {
		t.Fatalf("非法分页应兜底不报错: %v", err)
	}
}

func TestFileRepoFindByHash(t *testing.T) {
	repo, cleanup := newFileRepo(t)
	defer cleanup()

	f := seedFile(t, repo, 1, "h.txt", 0, 0)
	got, err := repo.FindByHash(f.FileHash, 1)
	if err != nil || got == nil || got.ID != f.ID {
		t.Fatalf("按hash应命中: %+v %v", got, err)
	}
	// 他人不可见 → nil,nil（调用方判幂等，不判错）
	got, err = repo.FindByHash(f.FileHash, 2)
	if err != nil || got != nil {
		t.Fatalf("他人应 miss-nil: %+v %v", got, err)
	}
	// 软删后不可见 → nil,nil（uk 冲突留给 CreateFile 报 409）
	if err := repo.SoftDelete(f.ID, 1); err != nil {
		t.Fatalf("软删失败: %v", err)
	}
	got, err = repo.FindByHash(f.FileHash, 1)
	if err != nil || got != nil {
		t.Fatalf("软删后应 miss-nil: %+v %v", got, err)
	}
}
