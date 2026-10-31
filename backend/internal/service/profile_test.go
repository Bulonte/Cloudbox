package service

import (
	"errors"
	"testing"

	"cloudbox/internal/repository"
)

func TestProfileOK(t *testing.T) {
	svc := newUserService(t)
	u, err := svc.Register(RegisterInput{Username: "me01", Email: "me@example.com", Password: "Abc12345"})
	if err != nil {
		t.Fatalf("注册失败: %v", err)
	}
	got, err := svc.Profile(u.ID)
	if err != nil {
		t.Fatalf("Profile 失败: %v", err)
	}
	if got.Username != "me01" || got.Email != "me@example.com" {
		t.Fatalf("Profile 返回不对: %+v", got)
	}
}

func TestProfileNotFound(t *testing.T) {
	svc := newUserService(t)
	if _, err := svc.Profile(999999); !errors.Is(err, repository.ErrUserNotFound) {
		t.Fatalf("查不存在应透传 ErrUserNotFound, 得到 %v", err)
	}
	if _, err := svc.Profile(0); !errors.Is(err, ErrInvalidParams) {
		t.Fatalf("ID 0 应返回 ErrInvalidParams, 得到 %v", err)
	}
}
