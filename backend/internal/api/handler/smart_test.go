package handler_test

import (
	"fmt"
	"net/http"
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

func newSmartRouter(t *testing.T) (*gin.Engine, *jwt.Manager) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	if err != nil {
		t.Fatalf("open sqlite memory: %v", err)
	}
	if err := db.AutoMigrate(&model.User{}, &model.File{}, &model.FileFingerprint{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	db.Create(&model.User{Username: "alice", Email: "alice@s.com", PasswordHash: "h", QuotaBytes: 10737418240})
	db.Create(&model.User{Username: "bob", Email: "bob@c.com", PasswordHash: "h", QuotaBytes: 100})
	files := repository.NewFileRepository(db)
	svc := service.NewSmartStatsService(files, repository.NewFingerprintRepository(db), repository.NewUserRepository(db))
	// 预置 alice 一文件（今天），供聚合非空
	f := &model.File{FileName: "a.txt", FileHash: fmt.Sprintf("%064x", 7), FileSize: 100, FilePath: "p/a", MimeType: "text/plain", OwnerID: 1}
	if err := files.CreateFile(f); err != nil {
		t.Fatalf("预置失败: %v", err)
	}
	mgr, err := jwt.NewManager(fileTestSecret, "cloudbox", 2*time.Hour, 168*time.Hour)
	if err != nil {
		t.Fatalf("NewManager 失败: %v", err)
	}
	sh := handler.NewSmartHandler(svc)
	r := gin.New()
	g := r.Group("/api/v1/smart", middleware.AuthRequired(mgr, &fakeRevoked{}))
	g.GET("/stats", sh.GetStats)
	return r, mgr
}

func TestSmartStatsAPI(t *testing.T) {
	r, mgr := newSmartRouter(t)
	a := tok(t, mgr, 1)
	b := tok(t, mgr, 2)

	// 缺省 days=14：200 + code0 + 四块齐 + timeline 14 点
	w := doFile(r, http.MethodGet, "/api/v1/smart/stats", "", a)
	if w.Code != http.StatusOK {
		t.Fatalf("应 200, 得到 %d %s", w.Code, w.Body.String())
	}
	code, _, data := decodeBody(t, w)
	if code != 0 {
		t.Fatalf("code=%d 期望0 %s", code, w.Body.String())
	}
	for _, k := range []string{"storage", "timeline", "category", "duplicates"} {
		if _, ok := data[k]; !ok {
			t.Fatalf("缺 %s: %v", k, data)
		}
	}
	st, _ := data["storage"].(map[string]any)
	if st["file_count"] != float64(1) || st["used_bytes"] != float64(100) {
		t.Fatalf("存储不对: %v", st)
	}
	if tl, _ := data["timeline"].([]any); len(tl) != 14 {
		t.Fatalf("timeline 应 14: %d", len(tl))
	}
	// 他人隔离：bob 空聚合 file_count=0
	w = doFile(r, http.MethodGet, "/api/v1/smart/stats?days=7", "", b)
	code, _, data = decodeBody(t, w)
	if code != 0 {
		t.Fatalf("bob code=%d %s", code, w.Body.String())
	}
	st, _ = data["storage"].(map[string]any)
	if st["file_count"] != float64(0) {
		t.Fatalf("他人应隔离: %v", st)
	}
	if tl, _ := data["timeline"].([]any); len(tl) != 7 {
		t.Fatalf("days=7 应 7 点: %d", len(tl))
	}
	// days 非法（负数/超限）由 Service 归一不 400
	w = doFile(r, http.MethodGet, "/api/v1/smart/stats?days=999", "", a)
	if w.Code != http.StatusOK {
		t.Fatalf("超限应归一 200: %d %s", w.Code, w.Body.String())
	}
	// 无头 401/20001
	w = doFile(r, http.MethodGet, "/api/v1/smart/stats", "", "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("无头应 401: %d %s", w.Code, w.Body.String())
	}
	if code, _, _ := decodeBody(t, w); code != 20001 {
		t.Fatalf("code=%d 期望20001 %s", code, w.Body.String())
	}
}
