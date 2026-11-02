package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"cloudbox/internal/api/dto"
	"cloudbox/internal/api/middleware"
	"cloudbox/internal/pkg/response"
	"cloudbox/internal/repository"
	"cloudbox/internal/service"
)

// 上传错误码：30002 配额不足；其余复用 file.go 的 10001/40004/90001
const codeQuotaExceeded = 30002

// ChunkHandler 分片会话 HTTP 层：只做 Bind/错误码映射，不直调仓储
type ChunkHandler struct {
	svc *service.ChunkService
}

// NewChunkHandler 构造注入分片服务
func NewChunkHandler(svc *service.ChunkService) *ChunkHandler {
	return &ChunkHandler{svc: svc}
}

// ToInitUploadResponse 会话占位行转脱敏 DTO
func ToInitUploadResponse(uploadID, fileHash string, totalChunks uint32, chunkSize uint64, status int8) dto.InitUploadResponse {
	return dto.InitUploadResponse{
		UploadID:    uploadID,
		FileHash:    fileHash,
		TotalChunks: totalChunks,
		ChunkSize:   chunkSize,
		Status:      status,
	}
}

// InitUpload POST /api/v1/uploads/init：签发分片会话（hash 已存在也照常建，秒传归 S4）
func (h *ChunkHandler) InitUpload(c *gin.Context) {
	var req dto.InitUploadRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, codeInvalidParams, "请求参数不正确")
		return
	}
	row, err := h.svc.InitUpload(c.GetUint64(middleware.CtxUserID), req.ParentID, req.FileName, req.FileHash, req.TotalSize, req.TotalChunks, req.ChunkSize)
	if err != nil {
		h.failChunk(c, err)
		return
	}
	response.OK(c, ToInitUploadResponse(row.UploadID, row.FileHash, row.TotalChunks, row.ChunkSize, row.Status))
}

// failChunk 分片错误统一映射：参数 400/10001；越权/父非法 404 伪装；配额 400/30002
func (h *ChunkHandler) failChunk(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrInvalidParams),
		errors.Is(err, repository.ErrChunkConflict):
		response.Fail(c, http.StatusBadRequest, codeInvalidParams, err.Error())
	case errors.Is(err, service.ErrQuotaExceeded):
		response.Fail(c, http.StatusBadRequest, codeQuotaExceeded, err.Error())
	case errors.Is(err, service.ErrFileNotFound),
		errors.Is(err, service.ErrInvalidParent),
		errors.Is(err, service.ErrChunkNotFound),
		errors.Is(err, repository.ErrFileNotFound),
		errors.Is(err, repository.ErrInvalidParent),
		errors.Is(err, repository.ErrChunkNotFound):
		response.Fail(c, http.StatusNotFound, codeFileNotFound, "文件不存在")
	default:
		response.Fail(c, http.StatusInternalServerError, codeInternal, "服务器内部错误")
	}
}
