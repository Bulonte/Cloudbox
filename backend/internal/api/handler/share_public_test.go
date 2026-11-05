package handler_test

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strings"
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
	"cloudbox/internal/storage"
)

// fakeShareLimiter S2 公开链路限流 fake：内存计数，不依赖 Docker
type fakeShareLimiter struct {
	counts map[string]int64
}

func newFakeShareLimiter() *fakeShareLimiter {
	return &fakeShareLimiter{counts: map[string]int64{}}
}

func (f *fakeShareLimiter) Incr(_ context.Context, key string) (int64, error) {
	f.counts[key]++
	return f.counts[key], nil
}

func (f *fakeShareLimiter) Expire(_ context.Context, _ string, _ time.Duration) error {
	return nil
}

func (f *fakeShareLimiter) Set(_ context.Context, _ string, _ any, _ time.Duration) error {
	return nil
}

func (f *fakeShareLimiter) Del(_ context.Context, _ ...string) error { return nil }

func (f *fakeShareLimiter) Get(_ context.Context, _ string) (string, error) {
	return "", fmt.Errorf("nil")
}

// sharePublicFixture 公开链路测试夹具：同库 db + 真存储 + 真分享服务
type sharePublicFixture struct {
	r   *gin.Engine
	mgr *jwt.Manager
	svc *service.ShareService
	db  *gorm.DB
	st  *storage.LocalStorage
	seq int
}

func newSharePublicFixture(t *testing.T, limiter service.RedisStore, rate int) *sharePublicFixture {
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
	st, err := storage.NewLocalStorage(t.TempDir())
	if err != nil {
		t.Fatalf("建测试存储失败: %v", err)
	}
	svc := service.NewShareService(repository.NewShareRepository(db), repository.NewFileRepository(db)).WithStorage(st).WithLimiter(limiter)
	if rate > 0 {
		svc.WithRateLimit(rate)
	}
	mgr, err := jwt.NewManager(fileTestSecret, "cloudbox", 2*time.Hour, 168*time.Hour)
	if err != nil {
		t.Fatalf("NewManager 失败: %v", err)
	}
	sh := handler.NewShareHandler(svc)
	r := gin.New()
	r.GET("/s/:code", sh.GetShareInfo)
	r.GET("/api/v1/shares/:code/download", sh.DownloadShare)
	auth := r.Group("/api/v1/shares", middleware.AuthRequired(mgr, &fakeRevoked{}))
	auth.POST("", sh.CreateShare)
	return &sharePublicFixture{r: r, mgr: mgr, svc: svc, db: db, st: st}
}

// seedPublicFile 落真实存储对象 + 文件行，返回文件 ID
func (fx *sharePublicFixture) seedPublicFile(t *testing.T, name, content string) uint64 {
	t.Helper()
	fx.seq++
	hash := fmt.Sprintf("%064x", 500000+fx.seq*7919+len(content))
	rel, err := fx.st.Store(hash, strings.NewReader(content))
	if err != nil {
		t.Fatalf("Store 失败: %v", err)
	}
	f := &model.File{FileName: name, FileHash: hash, FileSize: uint64(len(content)), FilePath: rel, MimeType: "text/plain", OwnerID: 1}
	if err := repository.NewFileRepository(fx.db).CreateFile(f); err != nil {
		t.Fatalf("建文件失败: %v", err)
	}
	return f.ID
}

func nullTime(t time.Time) sql.NullTime {
	return sql.NullTime{Time: t, Valid: true}
}

func TestSharePublicInfoOK(t *testing.T) {
	fx := newSharePublicFixture(t, nil, 0)
	fileID := fx.seedPublicFile(t, "hello.txt", "hello world")
	row, err := fx.svc.CreateShare(service.CreateShareInput{OwnerID: 1, FileID: fileID})
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	w := doFile(fx.r, http.MethodGet, "/s/"+row.ShareCode, "", "")
	if w.Code != http.StatusOK {
		t.Fatalf("访问应 200, 得到 %d %s", w.Code, w.Body.String())
	}
	code, _, data := decodeBody(t, w)
	if code != 0 {
		t.Fatalf("code=%d, 期望0 %s", code, w.Body.String())
	}
	if data["file_name"] != "hello.txt" || data["share_code"] != row.ShareCode {
		t.Fatalf("元数据不对: %v", data)
	}
	if _, has := data["password_hash"]; has {
		t.Fatalf("泄漏 password_hash: %v", data)
	}
	if _, ok := data["expire_at"]; !ok {
		t.Fatalf("过期时间缺失: %v", data)
	}
}

func TestSharePublicCamouflage(t *testing.T) {
	fx := newSharePublicFixture(t, nil, 0)
	fileID := fx.seedPublicFile(t, "c.txt", "camo")
	row, err := fx.svc.CreateShare(service.CreateShareInput{OwnerID: 1, FileID: fileID, Password: "Abc12345"})
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}

	// 错码（格式合法但不存在）→ 404/40004「链接无效」
	w := doFile(fx.r, http.MethodGet, "/s/0000000000000000", "", "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("错码应 404, 得到 %d %s", w.Code, w.Body.String())
	}
	if code, msg, _ := decodeBody(t, w); code != 40004 || msg != "链接无效" {
		t.Fatalf("code=%d msg=%q, 期望40004/链接无效 %s", code, msg, w.Body.String())
	}

	// 非法格式 → 400/10001
	w = doFile(fx.r, http.MethodGet, "/s/short", "", "")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("短码应 400, 得到 %d %s", w.Code, w.Body.String())
	}
	if code, _, _ := decodeBody(t, w); code != 10001 {
		t.Fatalf("code=%d, 期望10001 %s", code, w.Body.String())
	}

	// 密码错 → 404/40004
	w = doFile(fx.r, http.MethodGet, "/s/"+row.ShareCode+"?password=wrongpwd", "", "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("错密码应 404, 得到 %d %s", w.Code, w.Body.String())
	}
	if code, _, _ := decodeBody(t, w); code != 40004 {
		t.Fatalf("code=%d, 期望40004 %s", code, w.Body.String())
	}

	// 过期 → 404/40004
	past := time.Now().Add(-time.Hour)
	if err := repository.NewShareRepository(fx.db).SetExpireAt(row.ID, nullTime(past)); err != nil {
		t.Fatalf("改过期失败: %v", err)
	}
	w = doFile(fx.r, http.MethodGet, "/s/"+row.ShareCode+"?password=Abc12345", "", "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("过期应 404, 得到 %d %s", w.Code, w.Body.String())
	}
}

func TestSharePublicDownloadOK(t *testing.T) {
	fx := newSharePublicFixture(t, nil, 0)
	content := "download-bytes-123"
	fileID := fx.seedPublicFile(t, "dl.txt", content)
	row, err := fx.svc.CreateShare(service.CreateShareInput{OwnerID: 1, FileID: fileID})
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	w := doFile(fx.r, http.MethodGet, "/api/v1/shares/"+row.ShareCode+"/download", "", "")
	if w.Code != http.StatusOK {
		t.Fatalf("下载应 200, 得到 %d %s", w.Code, w.Body.String())
	}
	if cd := w.Header().Get("Content-Disposition"); !strings.Contains(cd, "attachment") {
		t.Fatalf("Content-Disposition 不对: %q", cd)
	}
	if v := w.Header().Get("X-Content-Type-Options"); v != "nosniff" {
		t.Fatalf("nosniff 缺失: %q", v)
	}
	if w.Body.String() != content {
		t.Fatalf("内容不对: %q", w.Body.String())
	}
	// 计数应为 1
	got, err := repository.NewShareRepository(fx.db).FindByCode(row.ShareCode)
	if err != nil {
		t.Fatalf("回查失败: %v", err)
	}
	if got.DownloadCount != 1 {
		t.Fatalf("计数=%d, 期望1", got.DownloadCount)
	}
}

func TestSharePublicRateLimit(t *testing.T) {
	fx := newSharePublicFixture(t, newFakeShareLimiter(), 2)
	fileID := fx.seedPublicFile(t, "rl.txt", "rl")
	row, err := fx.svc.CreateShare(service.CreateShareInput{OwnerID: 1, FileID: fileID})
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	for i := 0; i < 2; i++ {
		w := doFile(fx.r, http.MethodGet, "/s/"+row.ShareCode, "", "")
		if w.Code != http.StatusOK {
			t.Fatalf("前2次应 200, 得到 %d %s", w.Code, w.Body.String())
		}
	}
	w := doFile(fx.r, http.MethodGet, "/s/"+row.ShareCode, "", "")
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("第3次应 429, 得到 %d %s", w.Code, w.Body.String())
	}
	if code, _, _ := decodeBody(t, w); code != 20005 {
		t.Fatalf("code=%d, 期望20005 %s", code, w.Body.String())
	}
}
