package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"cloudbox/internal/api/dto"
	"cloudbox/internal/api/middleware"
	"cloudbox/internal/pkg/response"
	"cloudbox/internal/repository"
	"cloudbox/internal/service"
	"cloudbox/internal/service/upload"
)

// 上传错误码：30002 配额不足；其余复用 file.go 的 10001/40004/90001
const codeQuotaExceeded = 30002

// ChunkBodyLimit 单分片请求体上限：8MB + 1MB 兜底（含 multipart 开销），超限截断后转 400/10001
const ChunkBodyLimit = service.MaxChunkSize + 1*1024*1024

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

// ToUploadChunkResponse 分片上传回显：只含四字段，不含 chunk_path/file_hash
func ToUploadChunkResponse(uploadID string, index uint32, size uint64) dto.UploadChunkResponse {
	return dto.UploadChunkResponse{
		UploadID:   uploadID,
		ChunkIndex: index,
		Size:       size,
		Status:     1,
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

// UploadChunk POST /api/v1/uploads/:id/chunks：接收单个分片，落暂存盘 + 幂等入库
func (h *ChunkHandler) UploadChunk(c *gin.Context) {
	uploadID := c.Param("id")
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, ChunkBodyLimit)
	idx, err := strconv.ParseUint(c.PostForm("chunk_index"), 10, 32)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, codeInvalidParams, "请求参数不正确")
		return
	}
	fh, err := c.FormFile("chunk")
	if err != nil || fh == nil {
		response.Fail(c, http.StatusBadRequest, codeInvalidParams, "请求参数不正确")
		return
	}
	if fh.Size <= 0 {
		response.Fail(c, http.StatusBadRequest, codeInvalidParams, "请求参数不正确")
		return
	}
	f, err := fh.Open()
	if err != nil {
		response.Fail(c, http.StatusBadRequest, codeInvalidParams, "请求参数不正确")
		return
	}
	defer func() { _ = f.Close() }()
	res, err := h.svc.UploadChunk(c.GetUint64(middleware.CtxUserID), uploadID, uint32(idx), f, fh.Size)
	if err != nil {
		h.failChunk(c, err)
		return
	}
	response.OK(c, ToUploadChunkResponse(res.UploadID, res.ChunkIndex, res.Size))
}

// CompleteUpload POST /api/v1/uploads/:id/complete：分片合并收官（响应复用 FileResponse）
func (h *ChunkHandler) CompleteUpload(c *gin.Context) {
	var req dto.CompleteUploadRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, codeInvalidParams, "请求参数不正确")
		return
	}
	f, err := h.svc.CompleteUpload(c.GetUint64(middleware.CtxUserID), req.ParentID, c.Param("id"), req.FileName)
	if err != nil {
		h.failChunk(c, err)
		return
	}
	response.OK(c, ToFileResponse(f))
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
		errors.Is(err, upload.ErrFileNotFound),
		errors.Is(err, upload.ErrInvalidParent),
		errors.Is(err, repository.ErrFileNotFound),
		errors.Is(err, repository.ErrInvalidParent),
		errors.Is(err, repository.ErrChunkNotFound):
		response.Fail(c, http.StatusNotFound, codeFileNotFound, "文件不存在")
	case errors.Is(err, service.ErrFileConflict),
		errors.Is(err, upload.ErrFileConflict),
		errors.Is(err, repository.ErrFileConflict):
		response.Fail(c, http.StatusConflict, codeFileConflict, "同名文件已存在")
	default:
		response.Fail(c, http.StatusInternalServerError, codeInternal, "服务器内部错误")
	}
}
