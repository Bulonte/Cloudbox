package handler_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"cloudbox/internal/api"
	"cloudbox/internal/api/handler"
	"cloudbox/internal/model"
	"cloudbox/internal/pkg/jwt"
	"cloudbox/internal/repository"
	"cloudbox/internal/service"
)

// lifecycleRedis S5 测试替身：service.RedisStore + middleware.RevokedChecker 二合一
type lifecycleRedis struct {
	counts map[string]int64
	kv     map[string]string
}

func newLifecycleRedis() *lifecycleRedis {
	return &lifecycleRedis{counts: map[string]int64{}, kv: map[string]string{}}
}

func (m *lifecycleRedis) Incr(_ context.Context, key string) (int64, error) {
	m.counts[key]++
	return m.counts[key], nil
}

func (m *lifecycleRedis) Expire(_ context.Context, _ string, _ time.Duration) error { return nil }

func (m *lifecycleRedis) Set(_ context.Context, key string, val any, _ time.Duration) error {
	switch v := val.(type) {
	case string:
		m.kv[key] = v
	case uint64:
		m.kv[key] = uintToStr(v)
	default:
		m.kv[key] = ""
	}
	return nil
}

func (m *lifecycleRedis) Del(_ context.Context, keys ...string) error {
	for _, k := range keys {
		delete(m.counts, k)
		delete(m.kv, k)
	}
	return nil
}

func (m *lifecycleRedis) Get(_ context.Context, key string) (string, error) {
	if v, ok := m.kv[key]; ok {
		return v, nil
	}
	return "", context.DeadlineExceeded
}

// IsRevoked middleware.RevokedChecker：查 jwt:access:<jti>
func (m *lifecycleRedis) IsRevoked(_ context.Context, jti string) (bool, error) {
	_, ok := m.kv["jwt:access:"+jti]
	return ok, nil
}

func newLifecycleRouter(t *testing.T) (*gin.Engine, *lifecycleRedis) {
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
	mgr, err := jwt.NewManager("lifecycle-test-secret-32bytes!!!", "cloudbox", 2*time.Hour, 168*time.Hour)
	if err != nil {
		t.Fatalf("NewManager 失败: %v", err)
	}
	mem := newLifecycleRedis()
	svc := service.NewUserService(repository.NewUserRepository(db)).WithLoginDeps(mgr, mem)
	h := handler.NewUserHandler(svc)
	r := api.SetupRouter()
	api.RegisterAuthRoutes(r, h, mgr, mem)
	api.RegisterUserRoutes(r, h, mgr, mem)
	return r, mem
}

func doJSON(t *testing.T, r *gin.Engine, method, path, body, auth string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	if auth != "" {
		req.Header.Set("Authorization", "Bearer "+auth)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// TestAuthLifecycle 全链路：register→login→refresh→旧refresh复用401→logout→再用401→改密→老密401新密200
func TestAuthLifecycle(t *testing.T) {
	r, _ := newLifecycleRouter(t)

	if w := doJSON(t, r, "POST", "/api/v1/auth/register", `{"username":"lc01","email":"lc@example.com","password":"Abc12345"}`, ""); w.Code != http.StatusOK {
		t.Fatalf("注册应 200, 得到 %d %s", w.Code, w.Body.String())
	}
	w := doJSON(t, r, "POST", "/api/v1/auth/login", `{"login":"lc01","password":"Abc12345"}`, "")
	if w.Code != http.StatusOK {
		t.Fatalf("登录应 200, 得到 %d %s", w.Code, w.Body.String())
	}
	_, _, data := decodeBody(t, w)
	access := data["access_token"].(string)
	refresh := data["refresh_token"].(string)

	// refresh 轮换成功
	w = doJSON(t, r, "POST", "/api/v1/auth/refresh", `{"refresh_token":"`+refresh+`"}`, "")
	if w.Code != http.StatusOK {
		t.Fatalf("刷新应 200, 得到 %d %s", w.Code, w.Body.String())
	}
	_, _, data2 := decodeBody(t, w)
	access2 := data2["access_token"].(string)
	refresh2 := data2["refresh_token"].(string)
	if refresh2 == refresh {
		t.Fatal("刷新应签发新 refresh")
	}

	// 旧 refresh 复用 → 401/20002
	w = doJSON(t, r, "POST", "/api/v1/auth/refresh", `{"refresh_token":"`+refresh+`"}`, "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("旧 refresh 复用应 401, 得到 %d %s", w.Code, w.Body.String())
	}
	if code, _, _ := decodeBody(t, w); code != 20002 {
		t.Fatalf("code = %d, 期望 20002 %s", code, w.Body.String())
	}

	// 拿 access 当 refresh → 401/20002
	w = doJSON(t, r, "POST", "/api/v1/auth/refresh", `{"refresh_token":"`+access2+`"}`, "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("access 冒充应 401, 得到 %d %s", w.Code, w.Body.String())
	}

	// logout（带新 access + 新 refresh）
	w = doJSON(t, r, "POST", "/api/v1/auth/logout", `{"refresh_token":"`+refresh2+`"}`, access2)
	if w.Code != http.StatusOK {
		t.Fatalf("登出应 200, 得到 %d %s", w.Code, w.Body.String())
	}
	// 登出后 access 调 /me → 401
	if w := doJSON(t, r, "GET", "/api/v1/users/me", ``, access2); w.Code != http.StatusUnauthorized {
		t.Fatalf("登出后 access 应 401, 得到 %d %s", w.Code, w.Body.String())
	}
	// 登出后 refresh 再用 → 401
	if w := doJSON(t, r, "POST", "/api/v1/auth/refresh", `{"refresh_token":"`+refresh2+`"}`, ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("登出后刷新应 401, 得到 %d %s", w.Code, w.Body.String())
	}

	// 重新登录后改密
	w = doJSON(t, r, "POST", "/api/v1/auth/login", `{"login":"lc01","password":"Abc12345"}`, "")
	if w.Code != http.StatusOK {
		t.Fatalf("重登录应 200, 得到 %d %s", w.Code, w.Body.String())
	}
	_, _, data3 := decodeBody(t, w)
	access3 := data3["access_token"].(string)

	// 弱密码 → 400/10001
	if w := doJSON(t, r, "PUT", "/api/v1/users/password", `{"old_password":"Abc12345","new_password":"weak"}`, access3); w.Code != http.StatusBadRequest {
		t.Fatalf("弱密码应 400, 得到 %d %s", w.Code, w.Body.String())
	}
	// 旧密码错 → 401/20002
	if w := doJSON(t, r, "PUT", "/api/v1/users/password", `{"old_password":"Wrong123","new_password":"Xyz98765"}`, access3); w.Code != http.StatusUnauthorized {
		t.Fatalf("旧密码错应 401, 得到 %d %s", w.Code, w.Body.String())
	}
	// 改密成功
	if w := doJSON(t, r, "PUT", "/api/v1/users/password", `{"old_password":"Abc12345","new_password":"Xyz98765"}`, access3); w.Code != http.StatusOK {
		t.Fatalf("改密应 200, 得到 %d %s", w.Code, w.Body.String())
	}
	// 改密后本 access 应被吊销
	if w := doJSON(t, r, "GET", "/api/v1/users/me", ``, access3); w.Code != http.StatusUnauthorized {
		t.Fatalf("改密后旧 access 应 401, 得到 %d %s", w.Code, w.Body.String())
	}
	// 老密码登录 → 401
	if w := doJSON(t, r, "POST", "/api/v1/auth/login", `{"login":"lc01","password":"Abc12345"}`, ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("老密码应 401, 得到 %d %s", w.Code, w.Body.String())
	}
	// 新密码登录 → 200
	if w := doJSON(t, r, "POST", "/api/v1/auth/login", `{"login":"lc01","password":"Xyz98765"}`, ""); w.Code != http.StatusOK {
		t.Fatalf("新密码应 200, 得到 %d %s", w.Code, w.Body.String())
	}
	_ = access
}
