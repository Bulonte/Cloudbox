package handler_test

import (
	"bytes"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

var testPDF = []byte("%PDF-1.7\n%test upload content\n")
var testMZ = []byte{0x4D, 0x5A, 0x90, 0x00, 0x03, 0x00, 0x00, 0x00}

// doUpload 构造 multipart 上传请求
func doUpload(r *gin.Engine, token, fieldName, fileName string, content []byte, parentID string) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	if fieldName != "" {
		fw, _ := w.CreateFormFile(fieldName, fileName)
		if content != nil {
			_, _ = fw.Write(content)
		}
	}
	if parentID != "" {
		_ = w.WriteField("parent_id", parentID)
	}
	_ = w.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/files", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestUploadFileOK(t *testing.T) {
	r, mgr := newFileRouter(t)
	a := tok(t, mgr, 1)

	w := doUpload(r, a, "file", "a.pdf", testPDF, "")
	if w.Code != http.StatusOK {
		t.Fatalf("上传应 200, 得到 %d %s", w.Code, w.Body.String())
	}
	code, _, data := decodeBody(t, w)
	if code != 0 || data["file_name"] != "a.pdf" {
		t.Fatalf("上传回显不对: code=%d data=%v body=%s", code, data, w.Body.String())
	}
	if data["id"] == nil || data["id"].(float64) == 0 {
		t.Fatalf("无 id: %v", data)
	}
	if _, hasHash := data["file_hash"]; hasHash {
		t.Fatal("回显不得含 file_hash")
	}
	if _, hasPath := data["file_path"]; hasPath {
		t.Fatal("回显不得含 file_path")
	}
}

func TestUploadFileInvalid(t *testing.T) {
	r, mgr := newFileRouter(t)
	a := tok(t, mgr, 1)

	// 缺 file 字段
	w := doUpload(r, a, "", "a.pdf", testPDF, "")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("缺文件应 400, 得到 %d %s", w.Code, w.Body.String())
	}
	if code, _, _ := decodeBody(t, w); code != 10001 {
		t.Fatalf("code = %d, 期望 10001 %s", code, w.Body.String())
	}

	// exe 直接拒
	w = doUpload(r, a, "file", "x.exe", testMZ, "")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("exe 应 400, 得到 %d %s", w.Code, w.Body.String())
	}

	// 改名攻击：mz 头配 .jpg
	w = doUpload(r, a, "file", "fake.jpg", testMZ, "")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("改名攻击应 400, 得到 %d %s", w.Code, w.Body.String())
	}

	// parent 非数字
	w = doUpload(r, a, "file", "a.pdf", testPDF, "abc")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("parent 非法应 400, 得到 %d %s", w.Code, w.Body.String())
	}
	if code, _, _ := decodeBody(t, w); code != 10001 {
		t.Fatalf("code = %d, 期望 10001 %s", code, w.Body.String())
	}
}

func TestUploadFileParentOwnership(t *testing.T) {
	r, mgr := newFileRouter(t)
	a := tok(t, mgr, 1)
	b := tok(t, mgr, 2)

	// alice 建夹
	w := doFile(r, http.MethodPost, "/api/v1/files/folders", `{"file_name":"priv"}`, a)
	_, _, data := decodeBody(t, w)
	pid := uint64(data["id"].(float64))

	// bob 挂靠 → 404/40004
	w = doUpload(r, b, "file", "a.pdf", testPDF, fmt.Sprintf("%d", pid))
	if w.Code != http.StatusNotFound {
		t.Fatalf("挂靠他人夹应 404, 得到 %d %s", w.Code, w.Body.String())
	}
	if code, _, _ := decodeBody(t, w); code != 40004 {
		t.Fatalf("code = %d, 期望 40004 %s", code, w.Body.String())
	}

	// alice 自己挂靠成功
	w = doUpload(r, a, "file", "b.pdf", testPDF, fmt.Sprintf("%d", pid))
	if w.Code != http.StatusOK {
		t.Fatalf("挂靠自己夹应 200, 得到 %d %s", w.Code, w.Body.String())
	}
}

func TestUploadFileDuplicate(t *testing.T) {
	r, mgr := newFileRouter(t)
	a := tok(t, mgr, 1)

	w := doUpload(r, a, "file", "a.pdf", testPDF, "")
	if w.Code != http.StatusOK {
		t.Fatalf("首次应 200, 得到 %d %s", w.Code, w.Body.String())
	}
	// 同内容二次传 → 409/40006（W5 转被动秒传前契约）
	w = doUpload(r, a, "file", "b.pdf", testPDF, "")
	if w.Code != http.StatusConflict {
		t.Fatalf("二次同 hash 应 409, 得到 %d %s", w.Code, w.Body.String())
	}
	if code, _, _ := decodeBody(t, w); code != 40006 {
		t.Fatalf("code = %d, 期望 40006 %s", code, w.Body.String())
	}
}

func TestUploadFileUnauthorized(t *testing.T) {
	r, _ := newFileRouter(t)
	w := doUpload(r, "", "file", "a.pdf", testPDF, "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("无头应 401, 得到 %d %s", w.Code, w.Body.String())
	}
	if code, _, _ := decodeBody(t, w); code != 20001 {
		t.Fatalf("code = %d, 期望 20001 %s", code, w.Body.String())
	}
}
