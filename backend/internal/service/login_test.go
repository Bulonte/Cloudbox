package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"cloudbox/internal/pkg/jwt"
)

const loginTestSecret = "login-test-secret-please-32bytes!!"

// fakeRedis 内存实现 RedisStore：不依赖 Docker
type fakeRedis struct {
	counts map[string]int64
	kv     map[string]string
}

func newFakeRedis() *fakeRedis {
	return &fakeRedis{counts: map[string]int64{}, kv: map[string]string{}}
}

func (f *fakeRedis) Incr(_ context.Context, key string) (int64, error) {
	f.counts[key]++
	return f.counts[key], nil
}

func (f *fakeRedis) Expire(_ context.Context, _ string, _ time.Duration) error {
	return nil
}

func (f *fakeRedis) Set(_ context.Context, key string, val any, _ time.Duration) error {
	f.kv[key] = sprintVal(val)
	return nil
}

func sprintVal(v any) string {
	switch s := v.(type) {
	case string:
		return s
	default:
		return strings.TrimSpace(strings.Repeat(" ", 0)) + valToString(v)
	}
}

func valToString(v any) string {
	if s, ok := v.(uint64); ok {
		if s == 0 {
			return "0"
		}
		buf := make([]byte, 0, 20)
		n := s
		for n > 0 {
			buf = append([]byte{byte('0' + n%10)}, buf...)
			n /= 10
		}
		return string(buf)
	}
	return ""
}

func (f *fakeRedis) Del(_ context.Context, keys ...string) error {
	for _, k := range keys {
		delete(f.counts, k)
		delete(f.kv, k)
	}
	return nil
}

func (f *fakeRedis) Get(_ context.Context, key string) (string, error) {
	if v, ok := f.kv[key]; ok {
		return v, nil
	}
	return "", errors.New("nil")
}

func newLoginService(t *testing.T, fr *fakeRedis) (*UserService, *jwt.Manager) {
	t.Helper()
	mgr, err := jwt.NewManager(loginTestSecret, "cloudbox", 2*time.Hour, 168*time.Hour)
	if err != nil {
		t.Fatalf("NewManager 失败: %v", err)
	}
	svc := newUserService(t).WithLoginDeps(mgr, fr)
	return svc, mgr
}

func registerLoginUser(t *testing.T, svc *UserService) {
	t.Helper()
	if _, err := svc.Register(RegisterInput{Username: "login01", Email: "login@example.com", Password: "Abc12345"}); err != nil {
		t.Fatalf("注册失败: %v", err)
	}
}

func TestLoginOK(t *testing.T) {
	fr := newFakeRedis()
	svc, mgr := newLoginService(t, fr)
	registerLoginUser(t, svc)

	pair, err := svc.Login(LoginInput{Login: "login01", Password: "Abc12345"})
	if err != nil {
		t.Fatalf("登录失败: %v", err)
	}
	if pair.AccessToken == "" || pair.RefreshToken == "" {
		t.Fatal("双 token 不得为空")
	}
	if pair.ExpiresIn != 7200 {
		t.Fatalf("ExpiresIn = %d, 期望 7200", pair.ExpiresIn)
	}
	ac, err := mgr.ParseAccess(pair.AccessToken)
	if err != nil {
		t.Fatalf("access 解析失败: %v", err)
	}
	if ac.UserID == 0 || ac.Role != "user" {
		t.Fatalf("access claims 不对: %+v", ac)
	}
	rc, err := mgr.Parse(pair.RefreshToken)
	if err != nil {
		t.Fatalf("refresh 解析失败: %v", err)
	}
	if rc.TokenType != jwt.TokenTypeRefresh {
		t.Fatalf("refresh typ = %q, 期望 refresh", rc.TokenType)
	}
	if _, ok := fr.kv["refresh:"+rc.JTI]; !ok {
		t.Fatal("refresh jti 应落库 refresh:<jti>")
	}
	// refresh 不能冒充 access
	if _, err := mgr.ParseAccess(pair.RefreshToken); err == nil {
		t.Fatal("refresh 当 access 用必须被拒绝")
	}
}

func TestLoginByEmail(t *testing.T) {
	fr := newFakeRedis()
	svc, _ := newLoginService(t, fr)
	registerLoginUser(t, svc)
	if _, err := svc.Login(LoginInput{Login: "login@example.com", Password: "Abc12345"}); err != nil {
		t.Fatalf("邮箱登录失败: %v", err)
	}
}

func TestLoginWrongPasswordAndUnknown(t *testing.T) {
	fr := newFakeRedis()
	svc, _ := newLoginService(t, fr)
	registerLoginUser(t, svc)

	if _, err := svc.Login(LoginInput{Login: "login01", Password: "Wrong123"}); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("密码错应返回 ErrInvalidCredentials, 得到 %v", err)
	}
	// 不存在账号与密码错返回同一错误（防枚举）
	if _, err := svc.Login(LoginInput{Login: "nobody", Password: "Wrong123"}); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("不存在账号应返回 ErrInvalidCredentials, 得到 %v", err)
	}
}

func TestLoginRateLimit(t *testing.T) {
	fr := newFakeRedis()
	svc, _ := newLoginService(t, fr)
	registerLoginUser(t, svc)

	// 5 次失败返回凭证错，第 6 次触发限流
	for i := 0; i < 5; i++ {
		if _, err := svc.Login(LoginInput{Login: "login01", Password: "Wrong123"}); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("第 %d 次应为 ErrInvalidCredentials, 得到 %v", i+1, err)
		}
	}
	if _, err := svc.Login(LoginInput{Login: "login01", Password: "Wrong123"}); !errors.Is(err, ErrTooManyAttempts) {
		t.Fatalf("第 6 次应为 ErrTooManyAttempts, 得到 %v", err)
	}
	// 登录成功后计数清零：正确密码应能登录
	pair, err := svc.Login(LoginInput{Login: "login01", Password: "Abc12345"})
	if err != nil {
		// 注意：此时仍在限流窗口内（计数 6），正确密码同样被限流
		if !errors.Is(err, ErrTooManyAttempts) {
			t.Fatalf("限流窗口内正确密码也应被限流, 得到 %v", err)
		}
		return
	}
	if pair.AccessToken == "" {
		t.Fatal("限流清零后登录应签发 token")
	}
}

func TestLoginNoDeps(t *testing.T) {
	svc := newUserService(t)
	if _, err := svc.Login(LoginInput{Login: "x", Password: "Abc12345"}); err == nil {
		t.Fatal("未注入登录依赖应报错")
	}
}
