package repository

import (
	"errors"
	"testing"

	"cloudbox/internal/model"
)

func newUserRepo(t *testing.T) (*UserRepository, func()) {
	t.Helper()
	db := openTestDB(t)
	return NewUserRepository(db), func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	}
}

func TestUserRepoFindByUsernameAndEmail(t *testing.T) {
	repo, cleanup := newUserRepo(t)
	defer cleanup()

	u := &model.User{Username: "alice", Email: "alice@example.com", PasswordHash: "h"}
	if err := repo.CreateUser(u); err != nil {
		t.Fatalf("创建用户失败: %v", err)
	}

	got, err := repo.FindByUsername("alice")
	if err != nil {
		t.Fatalf("按用户名查失败: %v", err)
	}
	if got.ID != u.ID || got.Email != "alice@example.com" {
		t.Fatalf("查到的用户不对: %+v", got)
	}

	got2, err := repo.FindByEmail("alice@example.com")
	if err != nil {
		t.Fatalf("按邮箱查失败: %v", err)
	}
	if got2.ID != u.ID {
		t.Fatalf("按邮箱查到的用户不对: %+v", got2)
	}

	if _, err := repo.FindByUsername("nobody"); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("查不存在用户应返回 ErrUserNotFound, 得到 %v", err)
	}
}

func TestUserRepoFindByLogin(t *testing.T) {
	repo, cleanup := newUserRepo(t)
	defer cleanup()

	u := &model.User{Username: "bob", Email: "bob@example.com", PasswordHash: "h"}
	if err := repo.CreateUser(u); err != nil {
		t.Fatalf("创建用户失败: %v", err)
	}

	// 用户名登录
	if _, err := repo.FindByLogin("bob"); err != nil {
		t.Fatalf("用户名登录查询失败: %v", err)
	}
	// 邮箱登录
	if _, err := repo.FindByLogin("bob@example.com"); err != nil {
		t.Fatalf("邮箱登录查询失败: %v", err)
	}
	if _, err := repo.FindByLogin("nobody"); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("查不存在应返回 ErrUserNotFound, 得到 %v", err)
	}
}

func TestUserRepoExists(t *testing.T) {
	repo, cleanup := newUserRepo(t)
	defer cleanup()

	u := &model.User{Username: "carol", Email: "carol@example.com", PasswordHash: "h"}
	if err := repo.CreateUser(u); err != nil {
		t.Fatalf("创建用户失败: %v", err)
	}

	if ok, err := repo.ExistsByUsername("carol"); err != nil || !ok {
		t.Fatalf("用户名已占用, ok=%v err=%v", ok, err)
	}
	if ok, err := repo.ExistsByEmail("carol@example.com"); err != nil || !ok {
		t.Fatalf("邮箱已占用, ok=%v err=%v", ok, err)
	}
	if ok, err := repo.ExistsByUsername("nobody"); err != nil || ok {
		t.Fatalf("用户名不存在, ok=%v err=%v", ok, err)
	}
}

func TestUserRepoCreateConflict(t *testing.T) {
	repo, cleanup := newUserRepo(t)
	defer cleanup()

	u1 := &model.User{Username: "dave", Email: "dave@example.com", PasswordHash: "h"}
	if err := repo.CreateUser(u1); err != nil {
		t.Fatalf("创建用户失败: %v", err)
	}

	// 用户名重复
	u2 := &model.User{Username: "dave", Email: "other@example.com", PasswordHash: "h"}
	if err := repo.CreateUser(u2); !errors.Is(err, ErrUserConflict) {
		t.Fatalf("用户名重复应返回 ErrUserConflict, 得到 %v", err)
	}

	// 邮箱重复
	u3 := &model.User{Username: "other", Email: "dave@example.com", PasswordHash: "h"}
	if err := repo.CreateUser(u3); !errors.Is(err, ErrUserConflict) {
		t.Fatalf("邮箱重复应返回 ErrUserConflict, 得到 %v", err)
	}
}

func TestUserRepoFindByID(t *testing.T) {
	repo, cleanup := newUserRepo(t)
	defer cleanup()

	u := &model.User{Username: "erin", Email: "erin@example.com", PasswordHash: "h"}
	if err := repo.CreateUser(u); err != nil {
		t.Fatalf("创建用户失败: %v", err)
	}
	got, err := repo.FindByID(u.ID)
	if err != nil {
		t.Fatalf("按 ID 查失败: %v", err)
	}
	if got.Username != "erin" {
		t.Fatalf("查到的用户不对: %+v", got)
	}
	if _, err := repo.FindByID(999999); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("查不存在 ID 应返回 ErrUserNotFound, 得到 %v", err)
	}
}
