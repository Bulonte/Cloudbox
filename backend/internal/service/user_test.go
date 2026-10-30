package service

import (
	"errors"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"cloudbox/internal/model"
	"cloudbox/internal/repository"
)

// openUserTestDB S2 测试库：SQLite 内存 + 仅迁移 user 表
func openUserTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	if err != nil {
		t.Fatalf("open sqlite memory: %v", err)
	}
	if err := db.AutoMigrate(&model.User{}); err != nil {
		t.Fatalf("automigrate user: %v", err)
	}
	return db
}

func newUserService(t *testing.T) *UserService {
	t.Helper()
	return NewUserService(repository.NewUserRepository(openUserTestDB(t)))
}

func TestRegisterOK(t *testing.T) {
	svc := newUserService(t)
	u, err := svc.Register(RegisterInput{Username: "alice01", Email: "alice@example.com", Password: "Abc12345"})
	if err != nil {
		t.Fatalf("注册失败: %v", err)
	}
	if u.ID == 0 {
		t.Fatal("注册后 ID 应非 0")
	}
	if u.Username != "alice01" || u.Email != "alice@example.com" {
		t.Fatalf("返回用户不对: %+v", u)
	}
	if u.Role != "user" {
		t.Fatalf("新用户 Role = %q, 期望 user", u.Role)
	}
	if u.PasswordHash == "" || u.PasswordHash == "Abc12345" {
		t.Fatal("PasswordHash 应为 bcrypt 哈希，不得存明文")
	}
}

func TestRegisterDuplicateUsername(t *testing.T) {
	svc := newUserService(t)
	in := RegisterInput{Username: "bob01", Email: "bob@example.com", Password: "Abc12345"}
	if _, err := svc.Register(in); err != nil {
		t.Fatalf("首次注册失败: %v", err)
	}
	in.Email = "other@example.com"
	if _, err := svc.Register(in); !errors.Is(err, ErrUserConflict) {
		t.Fatalf("用户名重复应返回 ErrUserConflict, 得到 %v", err)
	}
}

func TestRegisterDuplicateEmail(t *testing.T) {
	svc := newUserService(t)
	in := RegisterInput{Username: "carol01", Email: "carol@example.com", Password: "Abc12345"}
	if _, err := svc.Register(in); err != nil {
		t.Fatalf("首次注册失败: %v", err)
	}
	in.Username = "other01"
	if _, err := svc.Register(in); !errors.Is(err, ErrUserConflict) {
		t.Fatalf("邮箱重复应返回 ErrUserConflict, 得到 %v", err)
	}
}

func TestRegisterInvalidParams(t *testing.T) {
	svc := newUserService(t)
	cases := []RegisterInput{
		{Username: "ab", Email: "a@example.com", Password: "Abc12345"},        // 用户名过短
		{Username: "bad name!", Email: "a@example.com", Password: "Abc12345"}, // 非法字符
		{Username: "valid01", Email: "not-an-email", Password: "Abc12345"},    // 邮箱非法
		{Username: "valid01", Email: "a@example.com", Password: "1234567"},    // 密码过短
		{Username: "valid01", Email: "a@example.com", Password: "12345678"},   // 纯数字
		{Username: "valid01", Email: "a@example.com", Password: "abcdefgh"},   // 纯字母
	}
	for i, in := range cases {
		if _, err := svc.Register(in); !errors.Is(err, ErrInvalidParams) {
			t.Fatalf("用例 %d 应返回 ErrInvalidParams, 得到 %v", i, err)
		}
	}
}
