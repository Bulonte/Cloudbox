package handler_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"cloudbox/internal/api/handler"
	"cloudbox/internal/api/middleware"
	"cloudbox/internal/model"
	"cloudbox/internal/pkg/jwt"
	"cloudbox/internal/repository"
	"cloudbox/internal/service"
)

const meTestSecret = "me-test-secret-please-32bytes!!!!"

// fakeRevoked 内存吊销表
type fakeRevoked struct{ revoked map[string]bool }

func (f *fakeRevoked) IsRevoked(_ context.Context, jti string) (bool, error) {
	return f.revoked[jti], nil
}

// errRevoked 模拟 Redis 挂了
type errRevoked struct{}

func (e *errRevoked) IsRevoked(_ context.Context, _ string) (bool, error) {
	return false, fmt.Errorf("redis down")
}

func newMeRouter(t *testing.T, checker middleware.RevokedChecker) (*gin.Engine, *jwt.Manager, *service.UserService, *gorm.DB) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	if err != nil {
		t.Fatalf("open sqlite memory: %v", err)
	}
	if err := db.AutoMigrate(&model.User{}); err != nil {
		t.Fatalf("automigrate user: %v", err)
	}
	mgr, err := jwt.NewManager(meTestSecret, "cloudbox", 2*time.Hour, 168*time.Hour)
	if err != nil {
		t.Fatalf("NewManager 失败: %v", err)
	}
	svc := service.NewUserService(repository.NewUserRepository(db))
	if _, err := svc.Register(service.RegisterInput{Username: "me01", Email: "me@example.com", Password: "Abc12345"}); err != nil {
		t.Fatalf("预置用户失败: %v", err)
	}
	h := handler.NewUserHandler(svc)
	r := gin.New()
	r.GET("/api/v1/users/me", middleware.AuthRequired(mgr, checker), h.Me)
	return r, mgr, svc, db
}

func getMe(r *gin.Engine, authHeader string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/me", nil)
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestMeOK(t *testing.T) {
	r, mgr, _, _ := newMeRouter(t, &fakeRevoked{})
	access, _, err := mgr.GenerateAccess(1, "user")
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}
	w := getMe(r, "Bearer "+access)
	if w.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200 body=%s", w.Code, w.Body.String())
	}
	code, _, data := decodeBody(t, w)
	if code != 0 {
		t.Fatalf("code = %d, 期望 0 body=%s", code, w.Body.String())
	}
	if data["username"] != "me01" {
		t.Fatalf("返回用户不对: %v", data)
	}
	if _, exists := data["password_hash"]; exists {
		t.Fatal("返回体不得含 password_hash")
	}
}

func TestMeTable(t *testing.T) {
	mgrSeed, err := jwt.NewManager(meTestSecret, "cloudbox", 2*time.Hour, 168*time.Hour)
	if err != nil {
		t.Fatalf("NewManager 失败: %v", err)
	}
	valid, jti, err := mgrSeed.GenerateAccess(1, "user")
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}
	refreshTok, _, err := mgrSeed.GenerateRefresh(1)
	if err != nil {
		t.Fatalf("签发 refresh 失败: %v", err)
	}
	other, _ := jwt.NewManager("xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx", "cloudbox", 2*time.Hour, 168*time.Hour)
	wrongSigned, _, _ := other.GenerateAccess(1, "user")

	cases := []struct {
		name       string
		header     string
		checker    middleware.RevokedChecker
		wantStatus int
		wantCode   int
	}{
		{"无头401", "", &fakeRevoked{}, 401, 20001},
		{"错scheme401", "Token " + valid, &fakeRevoked{}, 401, 20001},
		{"篡改401", "Bearer " + valid[:len(valid)-2] + "xx", &fakeRevoked{}, 401, 20002},
		{"错密钥401", "Bearer " + wrongSigned, &fakeRevoked{}, 401, 20002},
		{"refresh冒充401", "Bearer " + refreshTok, &fakeRevoked{}, 401, 20002},
		{"已吊销401", "Bearer " + valid, &fakeRevoked{revoked: map[string]bool{jti: true}}, 401, 20002},
		{"Redis挂500", "Bearer " + valid, &errRevoked{}, 500, 90001},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, _, _, _ := newMeRouter(t, tc.checker)
			w := getMe(r, tc.header)
			if w.Code != tc.wantStatus {
				t.Fatalf("status = %d, 期望 %d, body=%s", w.Code, tc.wantStatus, w.Body.String())
			}
			if code, _, _ := decodeBody(t, w); code != tc.wantCode {
				t.Fatalf("code = %d, 期望 %d %s", code, tc.wantCode, w.Body.String())
			}
		})
	}
}

func TestMeUserDeleted(t *testing.T) {
	r, mgr, svc, db := newMeRouter(t, &fakeRevoked{})
	// 注册第二个用户后物理删除，模拟 token 合法但用户已被删：应模糊为 401/20002
	u, err := svc.Register(service.RegisterInput{Username: "gone01", Email: "gone@example.com", Password: "Abc12345"})
	if err != nil {
		t.Fatalf("注册失败: %v", err)
	}
	tok, _, err := mgr.GenerateAccess(u.ID, "user")
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}
	if err := db.Delete(&model.User{}, u.ID).Error; err != nil {
		t.Fatalf("删用户失败: %v", err)
	}
	w := getMe(r, "Bearer "+tok)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("用户已删应 401, 得到 %d %s", w.Code, w.Body.String())
	}
	if code, _, _ := decodeBody(t, w); code != 20002 {
		t.Fatalf("code = %d, 期望 20002 %s", code, w.Body.String())
	}
}
