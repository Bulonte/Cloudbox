package handler

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"cloudbox/internal/api/dto"
	"cloudbox/internal/api/middleware"
	"cloudbox/internal/model"
	"cloudbox/internal/pkg/response"
	"cloudbox/internal/repository"
	"cloudbox/internal/service"
	"cloudbox/internal/service/upload"
)

// 文件错误码：10001 参数错；20002 认证失败；40004 文件不存在（含越权伪装）；40006 同名已存在；90001 服务器内部错误
// （30002 配额不足定义在 chunk.go，同一 handler 包共用）
const (
	codeFileNotFound = 40004
	codeFileConflict = 40006
)

// FileHandler 文件 HTTP 层：只做 Bind/校验/错误码映射，不直调仓储
type FileHandler struct {
	svc      *service.FileService
	uploader *upload.UploadService
}

// NewFileHandler 构造注入 service
func NewFileHandler(svc *service.FileService) *FileHandler {
	return &FileHandler{svc: svc}
}

// WithUploader 注入上传服务（S6 单文件上传用），返回同一实例方便链式调用
func (h *FileHandler) WithUploader(u *upload.UploadService) *FileHandler {
	h.uploader = u
	return h
}

// ToFileResponse model 转脱敏 DTO
func ToFileResponse(f *model.File) dto.FileResponse {
	return dto.FileResponse{
		ID:        f.ID,
		FileName:  f.FileName,
		ParentID:  f.ParentID,
		IsFolder:  f.IsFolder,
		FileSize:  f.FileSize,
		MimeType:  f.MimeType,
		Category:  f.Category.String,
		CreatedAt: f.CreatedAt.Format("2006-01-02 15:04:05"),
		UpdatedAt: f.UpdatedAt.Format("2006-01-02 15:04:05"),
	}
}

// CreateFolder POST /api/v1/files/folders：建文件夹
func (h *FileHandler) CreateFolder(c *gin.Context) {
	var req dto.CreateFolderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, codeInvalidParams, "请求参数不正确")
		return
	}
	f, err := h.svc.CreateFolder(c.GetUint64(middleware.CtxUserID), req.FileName, req.ParentID)
	switch {
	case err == nil:
		response.OK(c, ToFileResponse(f))
	case errors.Is(err, service.ErrInvalidParams):
		response.Fail(c, http.StatusBadRequest, codeInvalidParams, err.Error())
	case errors.Is(err, service.ErrFileNotFound),
		errors.Is(err, service.ErrInvalidParent),
		errors.Is(err, repository.ErrFileNotFound),
		errors.Is(err, repository.ErrInvalidParent):
		response.Fail(c, http.StatusNotFound, codeFileNotFound, "文件不存在")
	default:
		response.Fail(c, http.StatusInternalServerError, codeInternal, "服务器内部错误")
	}
}

// UpdateFile PUT /api/v1/files/:id：改名/移动二合一
func (h *FileHandler) UpdateFile(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		response.Fail(c, http.StatusBadRequest, codeInvalidParams, "请求参数不正确")
		return
	}
	var req dto.UpdateFileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, codeInvalidParams, "请求参数不正确")
		return
	}
	if req.FileName == nil && req.ParentID == nil {
		response.Fail(c, http.StatusBadRequest, codeInvalidParams, "请求参数不正确")
		return
	}
	owner := c.GetUint64(middleware.CtxUserID)
	role := c.GetString(middleware.CtxRole)
	var f *model.File
	if req.FileName != nil {
		f, err = h.svc.RenameChecked(owner, role, "", id, *req.FileName)
		if err != nil {
			h.failFile(c, err)
			return
		}
	}
	if req.ParentID != nil {
		f, err = h.svc.MoveChecked(owner, role, "", id, *req.ParentID)
		if err != nil {
			h.failFile(c, err)
			return
		}
	}
	response.OK(c, ToFileResponse(f))
}

// DeleteFile DELETE /api/v1/files/:id：软删（S3 集中判：需 manage；越权伪装 404）
func (h *FileHandler) DeleteFile(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		response.Fail(c, http.StatusBadRequest, codeInvalidParams, "请求参数不正确")
		return
	}
	if err := h.svc.DeleteChecked(c.GetUint64(middleware.CtxUserID), c.GetString(middleware.CtxRole), "", id); err != nil {
		h.failFile(c, err)
		return
	}
	response.OK(c, nil)
}

// UploadFile POST /api/v1/files：单文件上传（≤2MB 简单上传；S5 三层校验 + S4 内容寻址 + S1 落库）
func (h *FileHandler) UploadFile(c *gin.Context) {
	if h.uploader == nil {
		response.Fail(c, http.StatusInternalServerError, codeInternal, "服务器内部错误")
		return
	}
	// 请求体上限：单文件上限 + 1MB multipart 开销；超限由框架先断，统一转文件过大
	limit := h.uploader.MaxSimpleSize()
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit+1<<20)
	parentID, err := parseParentID(c.PostForm("parent_id"))
	if err != nil {
		response.Fail(c, http.StatusBadRequest, codeInvalidParams, "请求参数不正确")
		return
	}
	fh, err := c.FormFile("file")
	if err != nil || fh == nil {
		response.Fail(c, http.StatusBadRequest, codeInvalidParams, "请求参数不正确")
		return
	}
	f, err := fh.Open()
	if err != nil {
		response.Fail(c, http.StatusBadRequest, codeInvalidParams, "请求参数不正确")
		return
	}
	defer func() { _ = f.Close() }()
	got, err := h.uploader.Upload(c.GetUint64(middleware.CtxUserID), parentID, fh.Filename, f, fh.Size)
	if err != nil {
		h.failFile(c, err)
		return
	}
	response.OK(c, ToFileResponse(got))
}

// parseParentID 表单 parent_id 解析：缺省/空串=0 根目录；非数字报错
func parseParentID(s string) (uint64, error) {
	if s == "" {
		return 0, nil
	}
	return strconv.ParseUint(s, 10, 64)
}

// DownloadFile GET /api/v1/files/:id/download：鉴权 → 集中判(read) → 流式回写
func (h *FileHandler) DownloadFile(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		response.Fail(c, http.StatusBadRequest, codeInvalidParams, "请求参数不正确")
		return
	}
	f, rc, size, err := h.svc.DownloadChecked(c.GetUint64(middleware.CtxUserID), c.GetString(middleware.CtxRole), "", id)
	if err != nil {
		h.failFile(c, err)
		return
	}
	defer func() { _ = rc.Close() }()
	ctype := contentTypeOf(f.MimeType)
	c.Header("Content-Type", ctype)
	c.Header("Content-Disposition", attachmentOf(f.FileName))
	c.Header("X-Content-Type-Options", "nosniff")
	c.DataFromReader(http.StatusOK, size, ctype, rc, nil)
}

// contentTypeOf 下载 Content-Type：只信服务端入库值，空则回退 octet-stream
func contentTypeOf(mime string) string {
	m := strings.TrimSpace(mime)
	if m == "" {
		return "application/octet-stream"
	}
	return m
}

// attachmentOf 强制附件下载：双写 filename + filename*（中文/空格可下）
func attachmentOf(name string) string {
	escaped := strings.ReplaceAll(name, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)
	return `attachment; filename="` + escaped + `"; filename*=UTF-8''` + url.PathEscape(name)
}

// InstantCheck POST /api/v1/files/instant-check：秒传预检（只读+存储双判，不写盘）
// hit=true 本人已有直接秒传；hit=false+exists_global=true 他人已存（uk全局唯一实传仍409）；全miss走正常上传
func (h *FileHandler) InstantCheck(c *gin.Context) {
	if h.uploader == nil {
		response.Fail(c, http.StatusInternalServerError, codeInternal, "服务器内部错误")
		return
	}
	var req dto.InstantCheckRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, codeInvalidParams, "请求参数不正确")
		return
	}
	res, err := h.uploader.InstantCheck(c.GetUint64(middleware.CtxUserID), req.ParentID, req.FileName, req.FileHash)
	if err != nil {
		h.failFile(c, err)
		return
	}
	resp := dto.InstantCheckResponse{Hit: res.Hit, ExistsGlobal: res.ExistsGlobal}
	if res.File != nil {
		f := ToFileResponse(res.File)
		resp.File = &f
	}
	response.OK(c, resp)
}

// ListFiles GET /api/v1/files：按 parent 分页（夹置顶）；空目录 items=[] 非 null
func (h *FileHandler) ListFiles(c *gin.Context) {
	var q dto.ListFilesQuery
	if err := c.ShouldBindQuery(&q); err != nil {
		response.Fail(c, http.StatusBadRequest, codeInvalidParams, "请求参数不正确")
		return
	}
	files, total, page, pageSize, err := h.svc.List(c.GetUint64(middleware.CtxUserID), q.ParentID, q.Page, q.PageSize)
	if err != nil {
		h.failFile(c, err)
		return
	}
	items := make([]dto.FileResponse, 0, len(files))
	for _, f := range files {
		items = append(items, ToFileResponse(f))
	}
	response.OK(c, dto.FileListResponse{Total: total, Page: page, PageSize: pageSize, Items: items})
}

// failFile 文件错误统一映射：越权/已删/父夹非法一律 404 伪装；上传三层校验一律 400/10001；配额 400/30002
func (h *FileHandler) failFile(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrQuotaExceeded),
		errors.Is(err, upload.ErrQuotaExceeded):
		response.Fail(c, http.StatusBadRequest, codeQuotaExceeded, err.Error())
	case errors.Is(err, service.ErrInvalidParams),
		errors.Is(err, service.ErrNotDownloadable),
		errors.Is(err, upload.ErrTooLarge),
		errors.Is(err, upload.ErrUnsupportedExt),
		errors.Is(err, upload.ErrMimeMismatch),
		errors.Is(err, upload.ErrInvalidName):
		response.Fail(c, http.StatusBadRequest, codeInvalidParams, err.Error())
	case errors.Is(err, service.ErrFileNotFound),
		errors.Is(err, service.ErrInvalidParent),
		errors.Is(err, upload.ErrFileNotFound),
		errors.Is(err, upload.ErrInvalidParent),
		errors.Is(err, repository.ErrFileNotFound),
		errors.Is(err, repository.ErrInvalidParent):
		response.Fail(c, http.StatusNotFound, codeFileNotFound, "文件不存在")
	case errors.Is(err, service.ErrFileConflict),
		errors.Is(err, upload.ErrFileConflict),
		errors.Is(err, repository.ErrFileConflict):
		response.Fail(c, http.StatusConflict, codeFileConflict, "同名文件已存在")
	default:
		response.Fail(c, http.StatusInternalServerError, codeInternal, "服务器内部错误")
	}
}
