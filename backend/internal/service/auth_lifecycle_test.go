package service

import (
	"errors"
	"testing"

	"cloudbox/internal/pkg/jwt"
	"cloudbox/internal/pkg/password"
)

func registerLifecycleUser(t *testing.T, svc *UserService) {
	t.Helper()
	if _, err := svc.Register(RegisterInput{Username: "life01", Email: "life@example.com", Password: "Abc12345"}); err != nil {
		t.Fatalf("注册失败: %v", err)
	}
}

func TestRefreshOKAndOldInvalid(t *testing.T) {
	fr := newFakeRedis()
	svc, _ := newLoginService(t, fr)
	registerLifecycleUser(t, svc)
	pair, err := svc.Login(LoginInput{Login: "life01", Password: "Abc12345"})
	if err != nil {
		t.Fatalf("登录失败: %v", err)
	}
	np, err := svc.Refresh(RefreshInput{RefreshToken: pair.RefreshToken})
	if err != nil {
		t.Fatalf("刷新失败: %v", err)
	}
	if np.AccessToken == "" || np.RefreshToken == pair.RefreshToken {
		t.Fatalf("刷新应签发新双 token: %+v", np)
	}
	if np.ExpiresIn != 7200 {
		t.Fatalf("ExpiresIn = %d, 期望 7200", np.ExpiresIn)
	}
	// 旧 refresh 复用必须失败（防重放）
	if _, err := svc.Refresh(RefreshInput{RefreshToken: pair.RefreshToken}); !errors.Is(err, ErrInvalidRefreshToken) {
		t.Fatalf("旧 refresh 复用应返回 ErrInvalidRefreshToken, 得到 %v", err)
	}
}

func TestRefreshRejectsAccess(t *testing.T) {
	fr := newFakeRedis()
	svc, mgr := newLoginService(t, fr)
	registerLifecycleUser(t, svc)
	pair, err := svc.Login(LoginInput{Login: "life01", Password: "Abc12345"})
	if err != nil {
		t.Fatalf("登录失败: %v", err)
	}
	// 拿 access 当 refresh 用必须拒绝
	if _, err := svc.Refresh(RefreshInput{RefreshToken: pair.AccessToken}); !errors.Is(err, ErrInvalidRefreshToken) {
		t.Fatalf("access 冒充应返回 ErrInvalidRefreshToken, 得到 %v", err)
	}
	if _, err := svc.Refresh(RefreshInput{}); !errors.Is(err, ErrInvalidParams) {
		t.Fatalf("空 token 应返回 ErrInvalidParams, 得到 %v", err)
	}
	_ = mgr
}

func TestRefreshUnknownJTI(t *testing.T) {
	fr := newFakeRedis()
	svc, mgr := newLoginService(t, fr)
	registerLifecycleUser(t, svc)
	// 野生 refresh（签名对但没落库）必须拒绝
	wild, _, err := mgr.GenerateRefresh(1)
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}
	if _, err := svc.Refresh(RefreshInput{RefreshToken: wild}); !errors.Is(err, ErrInvalidRefreshToken) {
		t.Fatalf("未落库 refresh 应返回 ErrInvalidRefreshToken, 得到 %v", err)
	}
}

func TestLogoutRevokesAccess(t *testing.T) {
	fr := newFakeRedis()
	svc, mgr := newLoginService(t, fr)
	registerLifecycleUser(t, svc)
	pair, err := svc.Login(LoginInput{Login: "life01", Password: "Abc12345"})
	if err != nil {
		t.Fatalf("登录失败: %v", err)
	}
	claims, err := mgr.Parse(pair.AccessToken)
	if err != nil {
		t.Fatalf("解析 access 失败: %v", err)
	}
	if err := svc.Logout(LogoutInput{AccessJTI: claims.JTI, RefreshToken: pair.RefreshToken}); err != nil {
		t.Fatalf("登出失败: %v", err)
	}
	// access 吊销 key 应写入
	if _, ok := fr.kv["jwt:access:"+claims.JTI]; !ok {
		t.Fatal("登出应吊销 access")
	}
	// refresh 应被删：再刷必须失败
	if _, err := svc.Refresh(RefreshInput{RefreshToken: pair.RefreshToken}); !errors.Is(err, ErrInvalidRefreshToken) {
		t.Fatalf("登出后刷新应失败, 得到 %v", err)
	}
}

func TestChangePasswordOK(t *testing.T) {
	fr := newFakeRedis()
	svc, _ := newLoginService(t, fr)
	registerLifecycleUser(t, svc)
	pair, err := svc.Login(LoginInput{Login: "life01", Password: "Abc12345"})
	if err != nil {
		t.Fatalf("登录失败: %v", err)
	}
	u, err := svc.users.FindByLogin("life01")
	if err != nil {
		t.Fatalf("查用户失败: %v", err)
	}
	if err := svc.ChangePassword(ChangePasswordInput{UserID: u.ID, OldPassword: "Abc12345", NewPassword: "Xyz98765"}); err != nil {
		t.Fatalf("改密失败: %v", err)
	}
	// 老密码必须失效
	if _, err := svc.Login(LoginInput{Login: "life01", Password: "Abc12345"}); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("老密码应失效, 得到 %v", err)
	}
	// 新密码可登录
	np, err := svc.Login(LoginInput{Login: "life01", Password: "Xyz98765"})
	if err != nil {
		t.Fatalf("新密码登录失败: %v", err)
	}
	if np.AccessToken == "" {
		t.Fatal("新密码登录应签发 token")
	}
	_ = pair
}

func TestChangePasswordWrongOldAndWeakNew(t *testing.T) {
	fr := newFakeRedis()
	svc, _ := newLoginService(t, fr)
	registerLifecycleUser(t, svc)
	u, err := svc.users.FindByLogin("life01")
	if err != nil {
		t.Fatalf("查用户失败: %v", err)
	}
	if err := svc.ChangePassword(ChangePasswordInput{UserID: u.ID, OldPassword: "Wrong123", NewPassword: "Xyz98765"}); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("旧密码错应返回 ErrInvalidCredentials, 得到 %v", err)
	}
	if err := svc.ChangePassword(ChangePasswordInput{UserID: u.ID, OldPassword: "Abc12345", NewPassword: "weak"}); !errors.Is(err, ErrInvalidParams) {
		t.Fatalf("弱密码应返回 ErrInvalidParams, 得到 %v", err)
	}
	_ = jwt.TokenTypeRefresh
	_ = password.Cost
}
