package service

import (
	"database/sql"
	"errors"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"cloudbox/internal/model"
	"cloudbox/internal/repository"
	"cloudbox/internal/service/permission"
)

func newPermFileService(t *testing.T) (*FileService, *repository.FileRepository, func()) {
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
	fr := repository.NewFileRepository(db)
	svc := NewFileService(fr).WithChecker(permission.NewChecker())
	return svc, fr, func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	}
}

func seedVisFile(t *testing.T, fr *repository.FileRepository, owner uint64, vis, dept string) *model.File {
	t.Helper()
	f := &model.File{FileName: "v.txt", FileHash: "h-" + vis + dept, OwnerID: owner, Visibility: vis}
	if dept != "" {
		f.DepartmentID = sql.NullString{String: dept, Valid: true}
	}
	if err := fr.CreateFile(f); err != nil {
		t.Fatalf("预置可见性文件失败: %v", err)
	}
	return f
}

func TestCheckedReadMatrix(t *testing.T) {
	svc, fr, cleanup := newPermFileService(t)
	defer cleanup()

	priv := seedVisFile(t, fr, 1, "private", "")
	dept := seedVisFile(t, fr, 1, "department", "rd")
	pub := seedVisFile(t, fr, 1, "public", "")

	cases := []struct {
		name string
		uid  uint64
		role string
		dept string
		id   uint64
		act  permission.Action
		want bool
	}{
		{"所有者读私密", 1, "user", "", priv.ID, permission.ActionRead, true},
		{"异人读私密拒", 2, "user", "rd", priv.ID, permission.ActionRead, false},
		{"同部门读部门放", 2, "user", "rd", dept.ID, permission.ActionRead, true},
		{"异部门读部门拒", 3, "user", "other", dept.ID, permission.ActionRead, false},
		{"异人读公开放", 2, "user", "", pub.ID, permission.ActionRead, true},
		{"异人写公开拒", 2, "user", "", pub.ID, permission.ActionWrite, false},
		{"异人管公开拒", 2, "user", "", pub.ID, permission.ActionManage, false},
		{"超管读私密放", 9, "super_admin", "", priv.ID, permission.ActionRead, true},
		{"超管管私密放", 9, "super_admin", "", priv.ID, permission.ActionManage, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.loadForWrite(tc.uid, tc.role, tc.dept, tc.id, tc.act)
			if tc.act == permission.ActionRead {
				_, rerr := svc.loadForRead(tc.uid, tc.role, tc.dept, tc.id)
				err = rerr
			}
			if tc.want && err != nil {
				t.Fatalf("应放行, 得到 %v", err)
			}
			if !tc.want && !errors.Is(err, ErrFileNotFound) {
				t.Fatalf("应伪装404, 得到 %v", err)
			}
		})
	}
}

func TestCheckedWriteManageEnforce(t *testing.T) {
	svc, fr, cleanup := newPermFileService(t)
	defer cleanup()

	pub := seedVisFile(t, fr, 1, "public", "x")

	if _, err := svc.RenameChecked(2, "user", "", pub.ID, "hacked"); !errors.Is(err, ErrFileNotFound) {
		t.Fatalf("公开读不给改名, 得到 %v", err)
	}
	if _, err := svc.MoveChecked(2, "user", "", pub.ID, 0); !errors.Is(err, ErrFileNotFound) {
		t.Fatalf("公开读不给移动, 得到 %v", err)
	}
	if err := svc.DeleteChecked(2, "user", "", pub.ID); !errors.Is(err, ErrFileNotFound) {
		t.Fatalf("公开读不给删除, 得到 %v", err)
	}
	if _, err := svc.RenameChecked(1, "user", "", pub.ID, "ok"); err != nil {
		t.Fatalf("所有者改名应过: %v", err)
	}
	if err := svc.DeleteChecked(9, "super_admin", "", pub.ID); err != nil {
		t.Fatalf("超管删除应过: %v", err)
	}
}

func TestCreateShareManageOnly(t *testing.T) {
	svc, cleanup := newShareService(t)
	defer cleanup()
	svc.WithChecker(permission.NewChecker())

	row, err := svc.CreateShare(CreateShareInput{OwnerID: 1, FileID: 1})
	if err != nil {
		t.Fatalf("所有者建链应过: %v", err)
	}
	_ = row

	if _, err := svc.CreateShare(CreateShareInput{OwnerID: 2, FileID: 1}); !errors.Is(err, ErrFileNotFound) {
		t.Fatalf("异人私密建链应404, 得到 %v", err)
	}
	if _, err := svc.CreateShare(CreateShareInput{OwnerID: 9, Role: "super_admin", FileID: 1}); err != nil {
		t.Fatalf("超管建链应过: %v", err)
	}
}
