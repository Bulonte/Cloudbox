package handler_test

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
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
	"cloudbox/internal/service/upload"
	"cloudbox/internal/storage"
)

func doDownload(r *gin.Engine, token, id string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/files/"+id+"/download", nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// TestDownloadFileOK 上传 → 下载字节一致 + 强制附件头 + nosniff
func TestDownloadFileOK(t *testing.T) {
	r, mgr := newFileRouter(t)
	a := tok(t, mgr, 1)

	w := doUpload(r, a, "file", "a.pdf", testPDF, "")
	if w.Code != http.StatusOK {
		t.Fatalf("上传应 200, 得到 %d %s", w.Code, w.Body.String())
	}
	_, _, data := decodeBody(t, w)
	id := uint64(data["id"].(float64))

	d := doDownload(r, a, fmt.Sprintf("%d", id))
	if d.Code != http.StatusOK {
		t.Fatalf("下载应 200, 得到 %d %s", d.Code, d.Body.String())
	}
	if !bytes.Equal(d.Body.Bytes(), testPDF) {
		t.Fatal("下载字节不一致")
	}
	if ct := d.Header().Get("Content-Type"); ct == "" {
		t.Fatal("缺 Content-Type")
	}
	if cd := d.Header().Get("Content-Disposition"); cd == "" {
		t.Fatal("缺 Content-Disposition")
	} else if !bytes.Contains([]byte(cd), []byte("attachment")) || !bytes.Contains([]byte(cd), []byte("a.pdf")) {
		t.Fatalf("附件头不对: %s", cd)
	}
	if v := d.Header().Get("X-Content-Type-Options"); v != "nosniff" {
		t.Fatalf("缺 nosniff, 得到 %q", v)
	}
}

// TestDownloadFileReject 非法 id 400；文件夹 400；他人/不存在 404；无头 401
func TestDownloadFileReject(t *testing.T) {
	r, mgr := newFileRouter(t)
	a := tok(t, mgr, 1)
	b := tok(t, mgr, 2)

	// 非法 id
	for _, id := range []string{"abc", "0"} {
		w := doDownload(r, a, id)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("id=%s 应 400, 得到 %d %s", id, w.Code, w.Body.String())
		}
		if code, _, _ := decodeBody(t, w); code != 10001 {
			t.Fatalf("code = %d, 期望 10001 %s", code, w.Body.String())
		}
	}

	// 文件夹不可下
	w := doFile(r, http.MethodPost, "/api/v1/files/folders", `{"file_name":"d"}`, a)
	_, _, data := decodeBody(t, w)
	dirID := uint64(data["id"].(float64))
	w = doDownload(r, a, fmt.Sprintf("%d", dirID))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("文件夹应 400, 得到 %d %s", w.Code, w.Body.String())
	}
	if code, _, _ := decodeBody(t, w); code != 10001 {
		t.Fatalf("code = %d, 期望 10001 %s", code, w.Body.String())
	}

	// 他人/不存在 404 伪装
	w = doDownload(r, b, fmt.Sprintf("%d", dirID))
	if w.Code != http.StatusNotFound {
		t.Fatalf("他人应 404, 得到 %d %s", w.Code, w.Body.String())
	}
	if code, _, _ := decodeBody(t, w); code != 40004 {
		t.Fatalf("code = %d, 期望 40004 %s", code, w.Body.String())
	}
	w = doDownload(r, a, "999999")
	if w.Code != http.StatusNotFound {
		t.Fatalf("不存在应 404, 得到 %d %s", w.Code, w.Body.String())
	}

	// 无头 401
	w = doDownload(r, "", "1")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("无头应 401, 得到 %d %s", w.Code, w.Body.String())
	}
	if code, _, _ := decodeBody(t, w); code != 20001 {
		t.Fatalf("code = %d, 期望 20001 %s", code, w.Body.String())
	}
}

// TestDownloadFileOrphan DB 有行 blob 缺失 → 500（非 404 伪装）
func TestDownloadFileOrphan(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	if err != nil {
		t.Fatalf("open sqlite memory: %v", err)
	}
	if err := db.AutoMigrate(&model.User{}, &model.File{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	db.Create(&model.User{Username: "alice", Email: "alice@f.com", PasswordHash: "h"})
	mgr, err := jwt.NewManager(fileTestSecret, "cloudbox", 2*time.Hour, 168*time.Hour)
	if err != nil {
		t.Fatalf("NewManager 失败: %v", err)
	}
	fileRepo := repository.NewFileRepository(db)
	st, err := storage.NewLocalStorage(t.TempDir())
	if err != nil {
		t.Fatalf("建测试存储失败: %v", err)
	}
	fh := handler.NewFileHandler(service.NewFileService(fileRepo).WithStorage(st))
	fh.WithUploader(upload.NewUploadService(fileRepo, st, upload.MaxSimpleUploadSize))
	r := gin.New()
	files := r.Group("/api/v1/files", middleware.AuthRequired(mgr, &fakeRevoked{}))
	files.GET("/:id/download", fh.DownloadFile)

	orphan := &model.File{
		FileName: "orphan.pdf",
		FileHash: "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		FileSize: 3,
		FilePath: "cc/orphan-missing",
		MimeType: "application/pdf",
		OwnerID:  1,
	}
	if err := fileRepo.CreateFile(orphan); err != nil {
		t.Fatalf("预置孤儿失败: %v", err)
	}
	a := tok(t, mgr, 1)
	w := doDownload(r, a, fmt.Sprintf("%d", orphan.ID))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("孤儿应 500, 得到 %d %s", w.Code, w.Body.String())
	}
	if code, _, _ := decodeBody(t, w); code != 90001 {
		t.Fatalf("code = %d, 期望 90001 %s", code, w.Body.String())
	}
}
