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
)

const fileTestSecret = "file-test-secret-please-32bytes!!!"

// newFileRouter S2 测试路由：内存库 + FileService 真链 + AuthRequired，不依赖 Docker
func newFileRouter(t *testing.T) (*gin.Engine, *jwt.Manager) {
	t.Helper()
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
	// 预置两用户：uid1=alice, uid2=bob
	db.Create(&model.User{Username: "alice", Email: "alice@f.com", PasswordHash: "h"})
	db.Create(&model.User{Username: "bob", Email: "bob@f.com", PasswordHash: "h"})
	mgr, err := jwt.NewManager(fileTestSecret, "cloudbox", 2*time.Hour, 168*time.Hour)
	if err != nil {
		t.Fatalf("NewManager 失败: %v", err)
	}
	fh := handler.NewFileHandler(service.NewFileService(repository.NewFileRepository(db)))
	r := gin.New()
	files := r.Group("/api/v1/files", middleware.AuthRequired(mgr, &fakeRevoked{}))
	files.POST("/folders", fh.CreateFolder)
	files.PUT("/:id", fh.UpdateFile)
	files.DELETE("/:id", fh.DeleteFile)
	return r, mgr
}

func doFile(r *gin.Engine, method, path, body, token string) *httptest.ResponseRecorder {
	var reader *bytes.Buffer
	if body == "" {
		reader = bytes.NewBuffer(nil)
	} else {
		reader = bytes.NewBufferString(body)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func tok(t *testing.T, mgr *jwt.Manager, uid uint64) string {
	t.Helper()
	s, _, err := mgr.GenerateAccess(uid, "user")
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}
	return s
}

func TestFolderCRUD(t *testing.T) {
	r, mgr := newFileRouter(t)
	a := tok(t, mgr, 1)

	// 建根夹
	w := doFile(r, http.MethodPost, "/api/v1/files/folders", `{"file_name":"docs"}`, a)
	if w.Code != http.StatusOK {
		t.Fatalf("建夹应 200, 得到 %d %s", w.Code, w.Body.String())
	}
	code, _, data := decodeBody(t, w)
	if code != 0 || data["file_name"] != "docs" || data["is_folder"] != float64(1) {
		t.Fatalf("建夹回显不对: code=%d data=%v body=%s", code, data, w.Body.String())
	}
	id := uint64(data["id"].(float64))

	// 建子夹
	w = doFile(r, http.MethodPost, "/api/v1/files/folders", fmt.Sprintf(`{"file_name":"sub","parent_id":%d}`, id), a)
	if w.Code != http.StatusOK {
		t.Fatalf("建子夹应 200, 得到 %d %s", w.Code, w.Body.String())
	}
	_, _, sub := decodeBody(t, w)
	subID := uint64(sub["id"].(float64))

	// 改名
	w = doFile(r, http.MethodPut, fmt.Sprintf("/api/v1/files/%d", subID), `{"file_name":"sub2"}`, a)
	if w.Code != http.StatusOK {
		t.Fatalf("改名应 200, 得到 %d %s", w.Code, w.Body.String())
	}
	if _, _, d := decodeBody(t, w); d["file_name"] != "sub2" {
		t.Fatalf("改名未生效: %v", d)
	}

	// 搬回根
	w = doFile(r, http.MethodPut, fmt.Sprintf("/api/v1/files/%d", subID), `{"parent_id":0}`, a)
	if w.Code != http.StatusOK {
		t.Fatalf("移动应 200, 得到 %d %s", w.Code, w.Body.String())
	}

	// 软删
	w = doFile(r, http.MethodDelete, fmt.Sprintf("/api/v1/files/%d", subID), "", a)
	if w.Code != http.StatusOK {
		t.Fatalf("删除应 200, 得到 %d %s", w.Code, w.Body.String())
	}
	// 二次删 404/40004
	w = doFile(r, http.MethodDelete, fmt.Sprintf("/api/v1/files/%d", subID), "", a)
	if w.Code != http.StatusNotFound {
		t.Fatalf("二次删应 404, 得到 %d %s", w.Code, w.Body.String())
	}
	if code, _, _ := decodeBody(t, w); code != 40004 {
		t.Fatalf("code = %d, 期望 40004 %s", code, w.Body.String())
	}
}

func TestFolderInvalidParams(t *testing.T) {
	r, mgr := newFileRouter(t)
	a := tok(t, mgr, 1)

	cases := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{"空名", http.MethodPost, "/api/v1/files/folders", `{"file_name":""}`},
		{"空格名", http.MethodPost, "/api/v1/files/folders", `{"file_name":"   "}`},
		{"斜杠名", http.MethodPost, "/api/v1/files/folders", `{"file_name":"a/b"}`},
		{"反斜杠名", http.MethodPost, "/api/v1/files/folders", `{"file_name":"a\\b"}`},
		{"非法id", http.MethodPut, "/api/v1/files/abc", `{"file_name":"x"}`},
		{"零id", http.MethodPut, "/api/v1/files/0", `{"file_name":"x"}`},
		{"空体", http.MethodPut, "/api/v1/files/1", `{}`},
		{"非法del", http.MethodDelete, "/api/v1/files/abc", ``},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := doFile(r, tc.method, tc.path, tc.body, a)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("应 400, 得到 %d %s", w.Code, w.Body.String())
			}
			if code, _, _ := decodeBody(t, w); code != 10001 {
				t.Fatalf("code = %d, 期望 10001 %s", code, w.Body.String())
			}
		})
	}
}

// TestFolderOwnership SEC-01 横向越权：B 对 A 的夹一切操作 404/40004；SEC-08 伪造 owner 无入口
func TestFolderOwnership(t *testing.T) {
	r, mgr := newFileRouter(t)
	a := tok(t, mgr, 1)
	b := tok(t, mgr, 2)

	w := doFile(r, http.MethodPost, "/api/v1/files/folders", `{"file_name":"priv"}`, a)
	if w.Code != http.StatusOK {
		t.Fatalf("建夹失败: %d %s", w.Code, w.Body.String())
	}
	_, _, data := decodeBody(t, w)
	id := uint64(data["id"].(float64))

	ops := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{"改名", http.MethodPut, fmt.Sprintf("/api/v1/files/%d", id), `{"file_name":"hacked"}`},
		{"移动", http.MethodPut, fmt.Sprintf("/api/v1/files/%d", id), `{"parent_id":0}`},
		{"删除", http.MethodDelete, fmt.Sprintf("/api/v1/files/%d", id), ``},
	}
	for _, op := range ops {
		w := doFile(r, op.method, op.path, op.body, b)
		if w.Code != http.StatusNotFound {
			t.Fatalf("%s 越权应 404, 得到 %d %s", op.name, w.Code, w.Body.String())
		}
		if code, _, _ := decodeBody(t, w); code != 40004 {
			t.Fatalf("%s code = %d, 期望 40004 %s", op.name, code, w.Body.String())
		}
	}

	// B 建夹 parent 指向 A 的夹 → 404/40004（仓储归属过滤）
	w = doFile(r, http.MethodPost, "/api/v1/files/folders", fmt.Sprintf(`{"file_name":"evil","parent_id":%d}`, id), b)
	if w.Code != http.StatusNotFound {
		t.Fatalf("挂靠他人夹应 404, 得到 %d %s", w.Code, w.Body.String())
	}
}

// TestFolderCycle 防环：夹搬进自己/子孙 → 404/40004
func TestFolderCycle(t *testing.T) {
	r, mgr := newFileRouter(t)
	a := tok(t, mgr, 1)

	w := doFile(r, http.MethodPost, "/api/v1/files/folders", `{"file_name":"p"}`, a)
	_, _, p := decodeBody(t, w)
	pid := uint64(p["id"].(float64))
	w = doFile(r, http.MethodPost, "/api/v1/files/folders", fmt.Sprintf(`{"file_name":"c","parent_id":%d}`, pid), a)
	_, _, c := decodeBody(t, w)
	cid := uint64(c["id"].(float64))

	// 自己搬进自己
	w = doFile(r, http.MethodPut, fmt.Sprintf("/api/v1/files/%d", pid), fmt.Sprintf(`{"parent_id":%d}`, pid), a)
	if w.Code != http.StatusNotFound {
		t.Fatalf("自环应 404, 得到 %d %s", w.Code, w.Body.String())
	}
	// 父搬进子
	w = doFile(r, http.MethodPut, fmt.Sprintf("/api/v1/files/%d", pid), fmt.Sprintf(`{"parent_id":%d}`, cid), a)
	if w.Code != http.StatusNotFound {
		t.Fatalf("父进子应 404, 得到 %d %s", w.Code, w.Body.String())
	}
}

func TestFolderUnauthorized(t *testing.T) {
	r, _ := newFileRouter(t)
	w := doFile(r, http.MethodPost, "/api/v1/files/folders", `{"file_name":"x"}`, "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("无头应 401, 得到 %d %s", w.Code, w.Body.String())
	}
	if code, _, _ := decodeBody(t, w); code != 20001 {
		t.Fatalf("code = %d, 期望 20001 %s", code, w.Body.String())
	}
}
