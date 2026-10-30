package handler_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"cloudbox/internal/api"
	"cloudbox/internal/model"
	"cloudbox/internal/pkg/jwt"
)

func newAdminRouter(t *testing.T) (*gin.Engine, *jwt.Manager) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	mgr, err := jwt.NewManager("admin-test-secret-32bytes!!!!!!!", "cloudbox", 2*time.Hour, 168*time.Hour)
	if err != nil {
		t.Fatalf("NewManager 失败: %v", err)
	}
	r := api.SetupRouter()
	api.RegisterAdminRoutes(r, mgr, &fakeRevoked{})
	return r, mgr
}

func getAdminPing(r *gin.Engine, authHeader string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/ping", nil)
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestAdminPingOK(t *testing.T) {
	r, mgr := newAdminRouter(t)
	tok, _, err := mgr.GenerateAccess(2, model.RoleAdmin)
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}
	w := getAdminPing(r, "Bearer "+tok)
	if w.Code != http.StatusOK {
		t.Fatalf("admin 应 200, 得到 %d %s", w.Code, w.Body.String())
	}
	if code, _, data := decodeBody(t, w); code != 0 || data["role"] != model.RoleAdmin {
		t.Fatalf("回显不对: code=%d data=%v body=%s", code, data, w.Body.String())
	}
	// super_admin 预留位同样放行
	sup, _, err := mgr.GenerateAccess(3, model.RoleSuperAdmin)
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}
	if w := getAdminPing(r, "Bearer "+sup); w.Code != http.StatusOK {
		t.Fatalf("super_admin 应 200, 得到 %d %s", w.Code, w.Body.String())
	}
}

func TestAdminPingForbidden(t *testing.T) {
	r, mgr := newAdminRouter(t)
	userTok, _, _ := mgr.GenerateAccess(1, model.RoleUser)
	w := getAdminPing(r, "Bearer "+userTok)
	if w.Code != http.StatusForbidden {
		t.Fatalf("user 应 403, 得到 %d %s", w.Code, w.Body.String())
	}
	if code, _, _ := decodeBody(t, w); code != 20003 {
		t.Fatalf("code = %d, 期望 20003 %s", code, w.Body.String())
	}
	// 大小写敏感：User 不得放行
	upperTok, _, _ := mgr.GenerateAccess(1, "User")
	if w := getAdminPing(r, "Bearer "+upperTok); w.Code != http.StatusForbidden {
		t.Fatalf("role 大小写错应 403, 得到 %d %s", w.Code, w.Body.String())
	}
	// 空 role 不得放行
	emptyTok, _, _ := mgr.GenerateAccess(1, "")
	if w := getAdminPing(r, "Bearer "+emptyTok); w.Code != http.StatusForbidden {
		t.Fatalf("空 role 应 403, 得到 %d %s", w.Code, w.Body.String())
	}
}

func TestAdminPingUnauthorized(t *testing.T) {
	r, mgr := newAdminRouter(t)
	// 无头 → 401/20001
	if w := getAdminPing(r, ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("无头应 401, 得到 %d %s", w.Code, w.Body.String())
	} else if code, _, _ := decodeBody(t, w); code != 20001 {
		t.Fatalf("code = %d, 期望 20001 %s", code, w.Body.String())
	}
	// refresh 冒充 → 401/20002
	refreshTok, _, _ := mgr.GenerateRefresh(1)
	w := getAdminPing(r, "Bearer "+refreshTok)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("refresh 冒充应 401, 得到 %d %s", w.Code, w.Body.String())
	}
	if code, _, _ := decodeBody(t, w); code != 20002 {
		t.Fatalf("code = %d, 期望 20002 %s", code, w.Body.String())
	}
}

// TestRegisterRoleIgnored SEC-09：注册 body 带 role=admin 也必须落为 user
func TestRegisterRoleIgnored(t *testing.T) {
	r := newRegisterRouter(t)
	w := postRegister(t, r, `{"username":"role01","email":"role01@example.com","password":"Abc12345","role":"admin"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("注册应 200（多余字段忽略）, 得到 %d %s", w.Code, w.Body.String())
	}
	_, _, data := decodeBody(t, w)
	if data["role"] != model.RoleUser {
		t.Fatalf("role 应为 user, 得到 %v body=%s", data["role"], w.Body.String())
	}
}
