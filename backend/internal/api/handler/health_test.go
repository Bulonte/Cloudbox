package handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"cloudbox/internal/api"
)

func TestHealth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := api.SetupRouter()

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 %d, body=%s", w.Code, http.StatusOK, w.Body.String())
	}

	var body struct {
		Code    int            `json:"code"`
		Message string         `json:"message"`
		Data    map[string]any `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("响应不是合法JSON: %v, body=%s", err, w.Body.String())
	}
	if body.Code != 0 {
		t.Fatalf("code = %d, 期望 0, body=%s", body.Code, w.Body.String())
	}
	if body.Data["status"] != "ok" {
		t.Fatalf("data.status = %v, 期望 ok, body=%s", body.Data["status"], w.Body.String())
	}
}
