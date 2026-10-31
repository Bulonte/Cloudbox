package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"cloudbox/internal/api/dto"
	"cloudbox/internal/api/middleware"
	"cloudbox/internal/model"
	"cloudbox/internal/pkg/response"
	"cloudbox/internal/repository"
	"cloudbox/internal/service"
)

// 文件错误码：10001 参数错；20002 认证失败；40004 文件不存在（含越权伪装）；40006 同名已存在；90001 服务器内部错误
const (
	codeFileNotFound = 40004
	codeFileConflict = 40006
)

// FileHandler 文件 HTTP 层：只做 Bind/校验/错误码映射，不直调仓储
type FileHandler struct {
	svc *service.FileService
}

// NewFileHandler 构造注入 service
func NewFileHandler(svc *service.FileService) *FileHandler {
	return &FileHandler{svc: svc}
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
	var f *model.File
	if req.FileName != nil {
		f, err = h.svc.Rename(owner, id, *req.FileName)
		if err != nil {
			h.failFile(c, err)
			return
		}
	}
	if req.ParentID != nil {
		f, err = h.svc.Move(owner, id, *req.ParentID)
		if err != nil {
			h.failFile(c, err)
			return
		}
	}
	response.OK(c, ToFileResponse(f))
}

// DeleteFile DELETE /api/v1/files/:id：软删
func (h *FileHandler) DeleteFile(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		response.Fail(c, http.StatusBadRequest, codeInvalidParams, "请求参数不正确")
		return
	}
	if err := h.svc.Delete(c.GetUint64(middleware.CtxUserID), id); err != nil {
		h.failFile(c, err)
		return
	}
	response.OK(c, nil)
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

// failFile 文件错误统一映射：越权/已删/父夹非法一律 404 伪装
func (h *FileHandler) failFile(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrInvalidParams):
		response.Fail(c, http.StatusBadRequest, codeInvalidParams, err.Error())
	case errors.Is(err, service.ErrFileNotFound),
		errors.Is(err, service.ErrInvalidParent),
		errors.Is(err, repository.ErrFileNotFound),
		errors.Is(err, repository.ErrInvalidParent):
		response.Fail(c, http.StatusNotFound, codeFileNotFound, "文件不存在")
	case errors.Is(err, service.ErrFileConflict),
		errors.Is(err, repository.ErrFileConflict):
		response.Fail(c, http.StatusConflict, codeFileConflict, "同名文件已存在")
	default:
		response.Fail(c, http.StatusInternalServerError, codeInternal, "服务器内部错误")
	}
}
