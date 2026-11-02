package handler_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
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

const chunkTestSecret = "chunk-test-secret-please-32bytes!!"

// newChunkRouter S1 测试路由：内存库 + ChunkService 真链 + AuthRequired
func newChunkRouter(t *testing.T) (*gin.Engine, *jwt.Manager) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	if err != nil {
		t.Fatalf("open sqlite memory: %v", err)
	}
	if err := db.AutoMigrate(&model.User{}, &model.File{}, &model.UploadChunk{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	db.Create(&model.User{Username: "alice", Email: "alice@c.com", PasswordHash: "h", QuotaBytes: 10737418240})
	db.Create(&model.User{Username: "bob", Email: "bob@c.com", PasswordHash: "h", QuotaBytes: 100})
	mgr, err := jwt.NewManager(chunkTestSecret, "cloudbox", 2*time.Hour, 168*time.Hour)
	if err != nil {
		t.Fatalf("NewManager 失败: %v", err)
	}
	svc := service.NewChunkService(repository.NewFileRepository(db), repository.NewUserRepository(db), repository.NewChunkRepository(db), service.DefaultMaxFileSize, service.DefaultMaxChunks).WithTmpDir(t.TempDir()).WithStorage(mustTestStorage(t))
	ch := handler.NewChunkHandler(svc)
	r := gin.New()
	up := r.Group("/api/v1/uploads", middleware.AuthRequired(mgr, &fakeRevoked{}))
	up.POST("/init", ch.InitUpload)
	up.POST("/:id/chunks", ch.UploadChunk)
	up.POST("/:id/complete", ch.CompleteUpload)
	return r, mgr
}

func mustTestStorage(t *testing.T) *storage.LocalStorage {
	t.Helper()
	st, err := storage.NewLocalStorage(filepath.Join(t.TempDir(), "store"))
	if err != nil {
		t.Fatalf("建测试存储失败: %v", err)
	}
	return st
}

func doChunk(r *gin.Engine, method, path, body, token string) *httptest.ResponseRecorder {
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

func TestChunkInitAPI(t *testing.T) {
	r, mgr := newChunkRouter(t)
	a := tok(t, mgr, 1)

	hash := strings.Repeat("a", 64)
	body := fmt.Sprintf(`{"file_name":"a.zip","parent_id":0,"file_hash":%q,"total_size":4194304,"total_chunks":2,"chunk_size":2097152}`, hash)
	w := doChunk(r, http.MethodPost, "/api/v1/uploads/init", body, a)
	if w.Code != http.StatusOK {
		t.Fatalf("init 应 200, 得到 %d %s", w.Code, w.Body.String())
	}
	code, _, data := decodeBody(t, w)
	if code != 0 || len(data["upload_id"].(string)) != 36 {
		t.Fatalf("回显不对: code=%d data=%v %s", code, data, w.Body.String())
	}

	// 非法 hash → 400/10001
	bad := fmt.Sprintf(`{"file_name":"a.zip","file_hash":%q,"total_size":100,"total_chunks":1,"chunk_size":100}`, "zzz")
	w = doChunk(r, http.MethodPost, "/api/v1/uploads/init", bad, a)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("坏hash应 400, 得到 %d %s", w.Code, w.Body.String())
	}
	if code, _, _ := decodeBody(t, w); code != 10001 {
		t.Fatalf("code = %d, 期望 10001 %s", code, w.Body.String())
	}

	// 配额不足 → 400/30002（bob quota=100）
	b := tok(t, mgr, 2)
	big := fmt.Sprintf(`{"file_name":"a.zip","file_hash":%q,"total_size":2097152,"total_chunks":1,"chunk_size":2097152}`, hash)
	w = doChunk(r, http.MethodPost, "/api/v1/uploads/init", big, b)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("超配额应 400, 得到 %d %s", w.Code, w.Body.String())
	}
	if code, _, _ := decodeBody(t, w); code != 30002 {
		t.Fatalf("code = %d, 期望 30002 %s", code, w.Body.String())
	}

	// 无头 → 401
	w = doChunk(r, http.MethodPost, "/api/v1/uploads/init", body, "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("无头应 401, 得到 %d %s", w.Code, w.Body.String())
	}
}

// postChunk multipart 单分片上传 helper（与 W4 UploadFile 同写法，chunk_index 走 PostForm）
func postChunk(r *gin.Engine, uploadID, idx string, data []byte, token string) *httptest.ResponseRecorder {
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	if idx != "" {
		_ = w.WriteField("chunk_index", idx)
	}
	if data != nil {
		fw, err := w.CreateFormFile("chunk", "part.bin")
		if err != nil {
			panic(err)
		}
		if _, err := fw.Write(data); err != nil {
			panic(err)
		}
	}
	_ = w.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/uploads/"+uploadID+"/chunks", &b)
	req.Header.Set("Content-Type", w.FormDataContentType())
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// openUpload 建会话 helper：返回 uploadID（total 2 片，cs 1MB）
func openUpload(t *testing.T, r *gin.Engine, token string) string {
	t.Helper()
	hash := strings.Repeat("c", 64)
	body := fmt.Sprintf(`{"file_name":"a.zip","parent_id":0,"file_hash":%q,"total_size":2097152,"total_chunks":2,"chunk_size":1048576}`, hash)
	w := doChunk(r, http.MethodPost, "/api/v1/uploads/init", body, token)
	if w.Code != http.StatusOK {
		t.Fatalf("init 应 200, 得到 %d %s", w.Code, w.Body.String())
	}
	_, _, data := decodeBody(t, w)
	id, _ := data["upload_id"].(string)
	if len(id) != 36 {
		t.Fatalf("upload_id 非 36: %v", data)
	}
	return id
}

func TestChunkUploadAPI(t *testing.T) {
	r, mgr := newChunkRouter(t)
	a := tok(t, mgr, 1)
	id := openUpload(t, r, a)

	// 正常片 0
	w := postChunk(r, id, "0", []byte("hello-chunk-0"), a)
	if w.Code != http.StatusOK {
		t.Fatalf("传片应 200, 得到 %d %s", w.Code, w.Body.String())
	}
	code, _, data := decodeBody(t, w)
	if code != 0 || data["chunk_index"] != float64(0) || data["status"] != float64(1) {
		t.Fatalf("回显不对: code=%d data=%v %s", code, data, w.Body.String())
	}

	// 重传同 index 覆盖仍 200
	w = postChunk(r, id, "0", []byte("hello-again"), a)
	if w.Code != http.StatusOK {
		t.Fatalf("重传应 200, 得到 %d %s", w.Code, w.Body.String())
	}

	// 越界 index → 400/10001
	w = postChunk(r, id, "2", []byte("x"), a)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("越界应 400, 得到 %d %s", w.Code, w.Body.String())
	}
	if code, _, _ := decodeBody(t, w); code != 10001 {
		t.Fatalf("code = %d, 期望 10001 %s", code, w.Body.String())
	}

	// 缺 chunk part → 400/10001
	w = postChunk(r, id, "0", nil, a)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("缺片应 400, 得到 %d %s", w.Code, w.Body.String())
	}

	// 冒用他人会话 → 404/40004
	b := tok(t, mgr, 2)
	w = postChunk(r, id, "1", []byte("evil"), b)
	if w.Code != http.StatusNotFound {
		t.Fatalf("冒用应 404, 得到 %d %s", w.Code, w.Body.String())
	}
	if code, _, _ := decodeBody(t, w); code != 40004 {
		t.Fatalf("code = %d, 期望 40004 %s", code, w.Body.String())
	}

	// 非法 id → 400/10001
	w = postChunk(r, "not-uuid", "0", []byte("x"), a)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("坏id应 400, 得到 %d %s", w.Code, w.Body.String())
	}

	// 无头 → 401
	w = postChunk(r, id, "1", []byte("x"), "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("无头应 401, 得到 %d %s", w.Code, w.Body.String())
	}
}

func TestChunkCompleteAPI(t *testing.T) {
	r, mgr := newChunkRouter(t)
	a := tok(t, mgr, 1)

	content := []byte("hello-chunk-merge-api-0123456789-abcdef")
	sum := sha256.Sum256(content)
	hash := hex.EncodeToString(sum[:])
	cs := uint64(10)
	total := uint64(len(content))
	n := uint32((total + cs - 1) / cs)
	initBody := fmt.Sprintf(`{"file_name":"a.txt","parent_id":0,"file_hash":%q,"total_size":%d,"total_chunks":%d,"chunk_size":%d}`, hash, total, n, cs)
	w := doChunk(r, http.MethodPost, "/api/v1/uploads/init", initBody, a)
	if w.Code != http.StatusOK {
		t.Fatalf("init 应 200, 得到 %d %s", w.Code, w.Body.String())
	}
	_, _, initData := decodeBody(t, w)
	id, _ := initData["upload_id"].(string)

	// 缺片直接 complete → 400/10001
	bad := doChunk(r, http.MethodPost, "/api/v1/uploads/"+id+"/complete", `{"file_name":"a.txt","parent_id":0}`, a)
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("缺片合并应 400, 得到 %d %s", bad.Code, bad.Body.String())
	}
	if code, _, _ := decodeBody(t, bad); code != 10001 {
		t.Fatalf("code = %d, 期望 10001 %s", code, bad.Body.String())
	}

	// 传全部分片
	for i := uint32(0); i < n; i++ {
		s, e := uint64(i)*cs, uint64(i+1)*cs
		if e > total {
			e = total
		}
		if w := postChunk(r, id, fmt.Sprint(i), content[s:e], a); w.Code != http.StatusOK {
			t.Fatalf("传片 %d 应 200, 得到 %d %s", i, w.Code, w.Body.String())
		}
	}

	// 正常合并 → 200，file_name/size 回显
	w = doChunk(r, http.MethodPost, "/api/v1/uploads/"+id+"/complete", `{"file_name":"a.txt","parent_id":0}`, a)
	if w.Code != http.StatusOK {
		t.Fatalf("合并应 200, 得到 %d %s", w.Code, w.Body.String())
	}
	code, _, data := decodeBody(t, w)
	if code != 0 || data["file_name"] != "a.txt" {
		t.Fatalf("合并回显不对: code=%d data=%v %s", code, data, w.Body.String())
	}
	if sz, _ := data["file_size"].(float64); uint64(sz) != total {
		t.Fatalf("大小不对: %v 期望 %d %s", data["file_size"], total, w.Body.String())
	}

	// 重复合并幂等 → 200 同一 id
	w2 := doChunk(r, http.MethodPost, "/api/v1/uploads/"+id+"/complete", `{"file_name":"a.txt","parent_id":0}`, a)
	if w2.Code != http.StatusOK {
		t.Fatalf("重复合并应 200, 得到 %d %s", w2.Code, w2.Body.String())
	}
	_, _, data2 := decodeBody(t, w2)
	if data2["id"] != data["id"] {
		t.Fatalf("幂等应同 id: %v vs %v", data2["id"], data["id"])
	}

	// 冒用他人会话 → 404/40004
	b := tok(t, mgr, 2)
	w = doChunk(r, http.MethodPost, "/api/v1/uploads/"+id+"/complete", `{"file_name":"a.txt","parent_id":0}`, b)
	if w.Code != http.StatusNotFound {
		t.Fatalf("冒用应 404, 得到 %d %s", w.Code, w.Body.String())
	}
	if code, _, _ := decodeBody(t, w); code != 40004 {
		t.Fatalf("code = %d, 期望 40004 %s", code, w.Body.String())
	}

	// 非法 id → 400/10001
	w = doChunk(r, http.MethodPost, "/api/v1/uploads/not-uuid/complete", `{"file_name":"a.txt","parent_id":0}`, a)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("坏id应 400, 得到 %d %s", w.Code, w.Body.String())
	}
	if code, _, _ := decodeBody(t, w); code != 10001 {
		t.Fatalf("code = %d, 期望 10001 %s", code, w.Body.String())
	}
}
