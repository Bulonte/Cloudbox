package middleware_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"cloudbox/internal/api/middleware"
	"cloudbox/internal/pkg/jwt"
	"cloudbox/internal/pkg/redis"
)

const testSecret = "test-secret-please-override-32bytes!!"

func testManager(t *testing.T) *jwt.Manager {
	t.Helper()
	m, err := jwt.NewManager(testSecret, "cloudbox", 2*time.Hour, 168*time.Hour)
	if err != nil {
		t.Fatalf("NewManager 失败: %v", err)
	}
	return m
}

// fakeChecker 内存吊销表，测拒绝/放行不依赖 Redis
type fakeChecker struct{ revoked map[string]bool }

func (f *fakeChecker) IsRevoked(_ context.Context, jti string) (bool, error) {
	return f.revoked[jti], nil
}

// errChecker 模拟 Redis 挂了
type errChecker struct{}

func (e *errChecker) IsRevoked(_ context.Context, _ string) (bool, error) {
	return false, fmt.Errorf("redis down")
}

func setupRouter(mgr *jwt.Manager, checker middleware.RevokedChecker) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/ping", middleware.AuthRequired(mgr, checker), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"userID": c.GetUint64(middleware.CtxUserID),
			"role":   c.GetString(middleware.CtxRole),
		})
	})
	r.GET("/admin", middleware.AuthRequired(mgr, checker), middleware.AdminOnly(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	return r
}

func doReq(r *gin.Engine, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestAuthRequiredTable(t *testing.T) {
	mgr := testManager(t)
	valid, _, err := mgr.GenerateAccess(7, "user")
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}
	tampered := valid[:len(valid)-2] + "xx"

	other, _ := jwt.NewManager("xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx", "cloudbox", 2*time.Hour, 168*time.Hour)
	wrongSigned, _, _ := other.GenerateAccess(7, "user")

	// refresh 只能换 token，不能当访问凭证：中间件必须 401
	refreshTok, _, err := mgr.GenerateRefresh(7)
	if err != nil {
		t.Fatalf("签发 refresh 失败: %v", err)
	}

	cases := []struct {
		name       string
		header     string // ""=不带头；"raw:"前缀=原样 Authorization
		checker    middleware.RevokedChecker
		wantStatus int
		wantCode   int
	}{
		{"合法放行", "Bearer " + valid, &fakeChecker{}, 200, 0},
		{"无头401", "", &fakeChecker{}, 401, 20001},
		{"错scheme401", "raw:Token " + valid, &fakeChecker{}, 401, 20001},
		{"篡改401", "Bearer " + tampered, &fakeChecker{}, 401, 20002},
		{"错密钥401", "Bearer " + wrongSigned, &fakeChecker{}, 401, 20002},
		{"refresh冒充401", "Bearer " + refreshTok, &fakeChecker{}, 401, 20002},
		{"已吊销401", "Bearer " + valid, &fakeChecker{revoked: map[string]bool{}}, 401, 20002}, // jti 稍后填
		{"Redis挂500", "Bearer " + valid, &errChecker{}, 500, 90001},
	}
	// 已吊销用例：解析出 jti 后预置
	claims, _ := mgr.Parse(valid)
	for i := range cases {
		if cases[i].name == "已吊销401" {
			cases[i].checker = &fakeChecker{revoked: map[string]bool{claims.JTI: true}}
		}
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := setupRouter(mgr, tc.checker)
			var w *httptest.ResponseRecorder
			if tc.header == "" {
				w = doReq(r, "")
			} else if len(tc.header) > 4 && tc.header[:4] == "raw:" {
				req := httptest.NewRequest(http.MethodGet, "/ping", nil)
				req.Header.Set("Authorization", tc.header[4:])
				w = httptest.NewRecorder()
				r.ServeHTTP(w, req)
			} else {
				token := tc.header[len("Bearer "):]
				w = doReq(r, token)
			}
			if w.Code != tc.wantStatus {
				t.Fatalf("status = %d, 期望 %d, body=%s", w.Code, tc.wantStatus, w.Body.String())
			}
		})
	}
}

func TestAdminOnly(t *testing.T) {
	mgr := testManager(t)
	userTok, _, _ := mgr.GenerateAccess(1, "user")
	adminTok, _, _ := mgr.GenerateAccess(2, "admin")
	r := setupRouter(mgr, &fakeChecker{})

	get := func(path, tok string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	if w := get("/admin", adminTok); w.Code != 200 {
		t.Fatalf("admin 应放行, 得到 %d %s", w.Code, w.Body.String())
	}
	if w := get("/admin", userTok); w.Code != 403 {
		t.Fatalf("user 应 403, 得到 %d %s", w.Code, w.Body.String())
	}
}

// TestRevokedAgainstRealRedis 真容吊销链：签发→写入吊销→请求 401→删除后 200
// Redis 未启动则 Skip
func TestRevokedAgainstRealRedis(t *testing.T) {
	c := redis.NewClient("127.0.0.1:6380", "", 0)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := c.Ping(ctx); err != nil {
		t.Skipf("Redis 未启动: %v", err)
	}
	defer c.Close()

	mgr := testManager(t)
	token, jti, err := mgr.GenerateAccess(9, "user")
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}
	key := jwt.RevokedKey(jti)
	_ = c.Del(ctx, key)
	r := setupRouter(mgr, &middleware.RedisRevokedChecker{RDB: c})

	if w := doReq(r, token); w.Code != 200 {
		t.Fatalf("未吊销应 200, 得到 %d %s", w.Code, w.Body.String())
	}
	if err := c.Set(ctx, key, "1", time.Minute); err != nil {
		t.Fatalf("写吊销失败: %v", err)
	}
	if w := doReq(r, token); w.Code != 401 {
		t.Fatalf("吊销后应 401, 得到 %d %s", w.Code, w.Body.String())
	}
	_ = c.Del(ctx, key)
	if w := doReq(r, token); w.Code != 200 {
		t.Fatalf("删吊销后应恢复 200, 得到 %d %s", w.Code, w.Body.String())
	}
}
