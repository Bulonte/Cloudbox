package handler_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"cloudbox/internal/api/handler"
	"cloudbox/internal/model"
	"cloudbox/internal/pkg/jwt"
	"cloudbox/internal/repository"
	"cloudbox/internal/service"
)

// memRedis S3 handler 测试替身：实现 service.RedisStore，不依赖 Docker
type memRedis struct {
	counts map[string]int64
	kv     map[string]string
}

func newMemRedis() *memRedis {
	return &memRedis{counts: map[string]int64{}, kv: map[string]string{}}
}

func (m *memRedis) Incr(_ context.Context, key string) (int64, error) {
	m.counts[key]++
	return m.counts[key], nil
}

func (m *memRedis) Expire(_ context.Context, _ string, _ time.Duration) error {
	return nil
}

func (m *memRedis) Set(_ context.Context, key string, val any, _ time.Duration) error {
	if s, ok := val.(string); ok {
		m.kv[key] = s
		return nil
	}
	if u, ok := val.(uint64); ok {
		m.kv[key] = strings.TrimSpace(strings.Repeat(" ", 0) + uintToStr(u))
		return nil
	}
	m.kv[key] = ""
	return nil
}

func uintToStr(n uint64) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

func (m *memRedis) Del(_ context.Context, keys ...string) error {
	for _, k := range keys {
		delete(m.counts, k)
		delete(m.kv, k)
	}
	return nil
}

func (m *memRedis) Get(_ context.Context, key string) (string, error) {
	if v, ok := m.kv[key]; ok {
		return v, nil
	}
	return "", context.DeadlineExceeded
}

// newLoginRouter S3 测试路由：内存库 + fake Redis + 真 JWT，不依赖 Docker
func newLoginRouter(t *testing.T) *gin.Engine {
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
	mgr, err := jwt.NewManager("login-handler-test-secret-32bytes!!", "cloudbox", 2*time.Hour, 168*time.Hour)
	if err != nil {
		t.Fatalf("NewManager 失败: %v", err)
	}
	svc := service.NewUserService(repository.NewUserRepository(db)).WithLoginDeps(mgr, newMemRedis())
	if _, err := svc.Register(service.RegisterInput{Username: "login01", Email: "login@example.com", Password: "Abc12345"}); err != nil {
		t.Fatalf("预置用户失败: %v", err)
	}
	h := handler.NewUserHandler(svc)
	r := gin.New()
	r.POST("/api/v1/auth/login", h.Login)
	return r
}

func postLogin(t *testing.T, r *gin.Engine, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestLoginHandlerOK(t *testing.T) {
	r := newLoginRouter(t)
	w := postLogin(t, r, `{"login":"login01","password":"Abc12345"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200 body=%s", w.Code, w.Body.String())
	}
	code, _, data := decodeBody(t, w)
	if code != 0 {
		t.Fatalf("code = %d, 期望 0 body=%s", code, w.Body.String())
	}
	if data["access_token"] == nil || data["access_token"] == "" {
		t.Fatalf("缺 access_token: %v", data)
	}
	if data["refresh_token"] == nil || data["refresh_token"] == "" {
		t.Fatalf("缺 refresh_token: %v", data)
	}
	if data["token_type"] != "Bearer" {
		t.Fatalf("token_type = %v, 期望 Bearer", data["token_type"])
	}
	if data["expires_in"] != float64(7200) {
		t.Fatalf("expires_in = %v, 期望 7200", data["expires_in"])
	}
}

func TestLoginHandlerWrongPassword(t *testing.T) {
	r := newLoginRouter(t)
	w := postLogin(t, r, `{"login":"login01","password":"Wrong123"}`)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("密码错应 401, 得到 %d %s", w.Code, w.Body.String())
	}
	if code, _, _ := decodeBody(t, w); code != 20002 {
		t.Fatalf("code = %d, 期望 20002 %s", code, w.Body.String())
	}
}

func TestLoginHandlerRateLimit(t *testing.T) {
	r := newLoginRouter(t)
	// 5 次失败 + 第 6 次触发 429
	for i := 0; i < 5; i++ {
		if w := postLogin(t, r, `{"login":"login01","password":"Wrong123"}`); w.Code != http.StatusUnauthorized {
			t.Fatalf("第 %d 次应 401, 得到 %d %s", i+1, w.Code, w.Body.String())
		}
	}
	w := postLogin(t, r, `{"login":"login01","password":"Wrong123"}`)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("第 6 次应 429, 得到 %d %s", w.Code, w.Body.String())
	}
	if code, _, _ := decodeBody(t, w); code != 20005 {
		t.Fatalf("code = %d, 期望 20005 %s", code, w.Body.String())
	}
}
