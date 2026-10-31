package handler_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"cloudbox/internal/api/handler"
	"cloudbox/internal/model"
	"cloudbox/internal/repository"
	"cloudbox/internal/service"
)

// newRegisterRouter S2 测试路由：内存库 + 真 service + 真 handler，不依赖 Docker
func newRegisterRouter(t *testing.T) *gin.Engine {
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
	users := repository.NewUserRepository(db)
	h := handler.NewUserHandler(service.NewUserService(users))
	r := gin.New()
	r.POST("/api/v1/auth/register", h.Register)
	return r
}

func postRegister(t *testing.T, r *gin.Engine, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func decodeBody(t *testing.T, w *httptest.ResponseRecorder) (int, string, map[string]any) {
	t.Helper()
	var resp struct {
		Code    int            `json:"code"`
		Message string         `json:"message"`
		Data    map[string]any `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("响应不是合法JSON: %v body=%s", err, w.Body.String())
	}
	return resp.Code, resp.Message, resp.Data
}

func TestRegisterHandlerOK(t *testing.T) {
	r := newRegisterRouter(t)
	w := postRegister(t, r, `{"username":"alice01","email":"alice@example.com","password":"Abc12345"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200 body=%s", w.Code, w.Body.String())
	}
	code, _, data := decodeBody(t, w)
	if code != 0 {
		t.Fatalf("code = %d, 期望 0 body=%s", code, w.Body.String())
	}
	if data["username"] != "alice01" || data["email"] != "alice@example.com" {
		t.Fatalf("返回用户不对: %v", data)
	}
	if _, exists := data["password_hash"]; exists {
		t.Fatal("返回体不得含 password_hash")
	}
	if _, exists := data["passwordHash"]; exists {
		t.Fatal("返回体不得含 passwordHash")
	}
}

func TestRegisterHandlerDuplicate(t *testing.T) {
	r := newRegisterRouter(t)
	body := `{"username":"bob01","email":"bob@example.com","password":"Abc12345"}`
	if w := postRegister(t, r, body); w.Code != http.StatusOK {
		t.Fatalf("首次注册应 200, 得到 %d %s", w.Code, w.Body.String())
	}
	// 用户名重复
	w := postRegister(t, r, `{"username":"bob01","email":"other@example.com","password":"Abc12345"}`)
	if w.Code != http.StatusConflict {
		t.Fatalf("用户名重复应 409, 得到 %d %s", w.Code, w.Body.String())
	}
	if code, _, _ := decodeBody(t, w); code != 20004 {
		t.Fatalf("code = %d, 期望 20004 %s", code, w.Body.String())
	}
	// 邮箱重复
	w = postRegister(t, r, `{"username":"other01","email":"bob@example.com","password":"Abc12345"}`)
	if w.Code != http.StatusConflict {
		t.Fatalf("邮箱重复应 409, 得到 %d %s", w.Code, w.Body.String())
	}
}

func TestRegisterHandlerInvalid(t *testing.T) {
	r := newRegisterRouter(t)
	// 非法 JSON
	w := postRegister(t, r, `{"username":}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("非法 JSON 应 400, 得到 %d %s", w.Code, w.Body.String())
	}
	if code, _, _ := decodeBody(t, w); code != 10001 {
		t.Fatalf("code = %d, 期望 10001 %s", code, w.Body.String())
	}
	// 短密码
	w = postRegister(t, r, `{"username":"valid01","email":"a@example.com","password":"123"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("短密码应 400, 得到 %d %s", w.Code, w.Body.String())
	}
	if code, _, _ := decodeBody(t, w); code != 10001 {
		t.Fatalf("code = %d, 期望 10001 %s", code, w.Body.String())
	}
}
