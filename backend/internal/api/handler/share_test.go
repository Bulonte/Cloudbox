package handler_test

import (
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

func newShareRouter(t *testing.T) (*gin.Engine, *jwt.Manager) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	if err != nil {
		t.Fatalf("open sqlite memory: %v", err)
	}
	if err := db.AutoMigrate(&model.User{}, &model.File{}, &model.FileShare{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	db.Create(&model.User{Username: "alice", Email: "alice@sh.com", PasswordHash: "h", QuotaBytes: 10737418240})
	db.Create(&model.File{FileName: "a.txt", FileHash: "hash-a-txt-share1", OwnerID: 1})
	mgr, err := jwt.NewManager(fileTestSecret, "cloudbox", 2*time.Hour, 168*time.Hour)
	if err != nil {
		t.Fatalf("NewManager 失败: %v", err)
	}
	sh := handler.NewShareHandler(service.NewShareService(repository.NewShareRepository(db), repository.NewFileRepository(db)))
	r := gin.New()
	shares := r.Group("/api/v1/shares", middleware.AuthRequired(mgr, &fakeRevoked{}))
	shares.POST("", sh.CreateShare)
	return r, mgr
}

func TestCreateShareAPI(t *testing.T) {
	r, mgr := newShareRouter(t)
	a := tok(t, mgr, 1)
	b := tok(t, mgr, 2)

	// 默认创建：200 + code0 + 16位码 + /s/ 前缀 + 默认有效期
	w := doFile(r, http.MethodPost, "/api/v1/shares", `{"file_id":1}`, a)
	if w.Code != http.StatusOK {
		t.Fatalf("创建应 200, 得到 %d %s", w.Code, w.Body.String())
	}
	code, _, data := decodeBody(t, w)
	if code != 0 {
		t.Fatalf("code=%d, 期望0 %s", code, w.Body.String())
	}
	sc, _ := data["share_code"].(string)
	if len(sc) != 16 {
		t.Fatalf("码长不对: %v %s", data, w.Body.String())
	}
	if su, _ := data["share_url"].(string); su != "/s/"+sc {
		t.Fatalf("url不对: %v", data)
	}
	if _, ok := data["expire_at"]; !ok {
		t.Fatalf("默认有效期缺失: %v", data)
	}
	if _, has := data["password_hash"]; has {
		t.Fatalf("回显泄漏 password_hash: %v", data)
	}

	// 他人文件 → 404/40004
	w = doFile(r, http.MethodPost, "/api/v1/shares", `{"file_id":1}`, b)
	if w.Code != http.StatusNotFound {
		t.Fatalf("他人应 404, 得到 %d %s", w.Code, w.Body.String())
	}
	if code, _, _ := decodeBody(t, w); code != 40004 {
		t.Fatalf("code=%d, 期望40004 %s", code, w.Body.String())
	}

	// 非法有效期格式 → 400/10001
	w = doFile(r, http.MethodPost, "/api/v1/shares", `{"file_id":1,"expire_at":"not-a-time"}`, a)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("坏时间应 400, 得到 %d %s", w.Code, w.Body.String())
	}
	if code, _, _ := decodeBody(t, w); code != 10001 {
		t.Fatalf("code=%d, 期望10001 %s", code, w.Body.String())
	}

	// 无头 → 401/20001
	w = doFile(r, http.MethodPost, "/api/v1/shares", `{"file_id":1}`, "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("无头应 401, 得到 %d %s", w.Code, w.Body.String())
	}
	if code, _, _ := decodeBody(t, w); code != 20001 {
		t.Fatalf("code=%d, 期望20001 %s", code, w.Body.String())
	}
}
