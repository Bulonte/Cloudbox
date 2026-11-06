package handler_test

import (
	"net/http"
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

// newAuditRouter 管理查询测试路由：内存库 + 真审计链，不过 Redis
func newAuditRouter(t *testing.T) (*gin.Engine, *jwt.Manager, *repository.OperationLogRepository) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	if err != nil {
		t.Fatalf("open sqlite memory: %v", err)
	}
	if err := db.AutoMigrate(&model.OperationLog{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	logs := repository.NewOperationLogRepository(db)
	mgr, err := jwt.NewManager("audit-test-secret-32bytes!!!!!!!", "cloudbox", 2*time.Hour, 168*time.Hour)
	if err != nil {
		t.Fatalf("NewManager 失败: %v", err)
	}
	r := api.SetupRouter()
	api.RegisterAdminRoutes(r, mgr, &fakeRevoked{}, handler.NewAdminHandler(service.NewAuditService(logs)))
	return r, mgr, logs
}

func TestListOperationLogsOK(t *testing.T) {
	r, mgr, logs := newAuditRouter(t)
	svc := service.NewAuditService(logs)
	_ = svc.Record(service.AuditInput{UserID: 1, Action: repository.ActionShareCreate, TargetType: "share", TargetID: 7, Detail: map[string]any{"share_id": uint64(7)}})
	_ = svc.Record(service.AuditInput{UserID: 2, Action: repository.ActionDownload, TargetType: "file", TargetID: 8})
	adminTok, _, _ := mgr.GenerateAccess(9, model.RoleAdmin)
	w := doFile(r, http.MethodGet, "/api/v1/admin/operation-logs?page=1&page_size=20", "", adminTok)
	if w.Code != http.StatusOK {
		t.Fatalf("admin 应 200, 得到 %d %s", w.Code, w.Body.String())
	}
	code, _, data := decodeBody(t, w)
	if code != 0 || data["total"] != float64(2) {
		t.Fatalf("total 不对: code=%d data=%v %s", code, data, w.Body.String())
	}
	// user_id 过滤
	w = doFile(r, http.MethodGet, "/api/v1/admin/operation-logs?user_id=1", "", adminTok)
	if _, _, d := decodeBody(t, w); d["total"] != float64(1) {
		t.Fatalf("过滤不对: %v %s", d, w.Body.String())
	}
	// 非法时间 400/10001
	w = doFile(r, http.MethodGet, "/api/v1/admin/operation-logs?start_time=bad", "", adminTok)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("坏时间应 400, 得到 %d %s", w.Code, w.Body.String())
	}
	// 普通用户 403/20003
	userTok, _, _ := mgr.GenerateAccess(1, model.RoleUser)
	if w := doFile(r, http.MethodGet, "/api/v1/admin/operation-logs", "", userTok); w.Code != http.StatusForbidden {
		t.Fatalf("user 应 403, 得到 %d %s", w.Code, w.Body.String())
	}
}
