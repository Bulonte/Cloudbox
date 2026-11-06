package handler

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"cloudbox/internal/api/dto"
	"cloudbox/internal/api/middleware"
	"cloudbox/internal/pkg/response"
	"cloudbox/internal/repository"
	"cloudbox/internal/service"
)

// ShareHandler 分享 HTTP 层：只做 Bind/校验/错误码映射，不直调仓储
type ShareHandler struct {
	svc *service.ShareService
}

// NewShareHandler 构造注入分享服务
func NewShareHandler(svc *service.ShareService) *ShareHandler {
	return &ShareHandler{svc: svc}
}

// parseShareExpire 分享有效期解析：空=默认（service 层兜底）；支持 RFC3339 与 "2006-01-02 15:04:05"
func parseShareExpire(s *string) (*time.Time, error) {
	if s == nil || *s == "" {
		return nil, nil
	}
	if t, err := time.Parse(time.RFC3339, *s); err == nil {
		return &t, nil
	}
	if t, err := time.ParseInLocation("2006-01-02 15:04:05", *s, time.Local); err == nil {
		return &t, nil
	}
	return nil, errors.New("有效期格式非法")
}

// CreateShare POST /api/v1/shares：需登录；归属/参数错按文件链路口径映射
func (h *ShareHandler) CreateShare(c *gin.Context) {
	var req dto.CreateShareRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, codeInvalidParams, "请求参数不正确")
		return
	}
	exp, err := parseShareExpire(req.ExpireAt)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, codeInvalidParams, err.Error())
		return
	}
	row, err := h.svc.CreateShare(service.CreateShareInput{
		OwnerID:      c.GetUint64(middleware.CtxUserID),
		Role:         c.GetString(middleware.CtxRole),
		FileID:       req.FileID,
		Password:     req.Password,
		ExpireAt:     exp,
		MaxDownloads: req.MaxDownloads,
		IP:           c.ClientIP(),
	})
	if err != nil {
		h.failShare(c, err)
		return
	}
	var expStr *string
	if row.ExpireAt.Valid {
		s := row.ExpireAt.Time.Format("2006-01-02 15:04:05")
		expStr = &s
	}
	var maxPtr *int32
	if row.MaxDownloads.Valid {
		v := row.MaxDownloads.Int32
		maxPtr = &v
	}
	response.OK(c, dto.CreateShareResponse{
		ID:           row.ID,
		ShareCode:    row.ShareCode,
		ShareURL:     "/s/" + row.ShareCode,
		FileID:       row.FileID,
		ExpireAt:     expStr,
		MaxDownloads: maxPtr,
	})
}

// parseShareID 分享 ID 解析：十进制正整数，非法一律参错
func parseShareID(s string) (uint64, error) {
	id, err := strconv.ParseUint(s, 10, 64)
	if err != nil || id == 0 {
		return 0, errors.New("分享ID非法")
	}
	return id, nil
}

// CancelShare DELETE /api/v1/shares/:id：需登录，仅 owner；幂等由仓储保证，他人/不存在一律 404/40004「分享不存在」防枚举
// S4：成功后审计 share_cancel（C方案 detail 只记 share_id/file_id/file_name，禁记 share_code 明文/哈希）
func (h *ShareHandler) CancelShare(c *gin.Context) {
	id, err := parseShareID(c.Param("id"))
	if err != nil {
		response.Fail(c, http.StatusBadRequest, codeInvalidParams, "请求参数不正确")
		return
	}
	if err := h.svc.CancelShare(c.GetUint64(middleware.CtxUserID), id, c.ClientIP()); err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidParams):
			response.Fail(c, http.StatusBadRequest, codeInvalidParams, err.Error())
		case errors.Is(err, repository.ErrShareNotFound),
			errors.Is(err, service.ErrFileNotFound),
			errors.Is(err, repository.ErrFileNotFound):
			response.Fail(c, http.StatusNotFound, codeFileNotFound, "分享不存在")
		default:
			response.Fail(c, http.StatusInternalServerError, codeInternal, "服务器内部错误")
		}
		return
	}
	response.OK(c, nil)
}

// failShare 分享错误统一映射：参数 400/10001；限流 429/20005；
// S1 创建链路文件归属错 → 404/40004「文件不存在」；S2 公开链路一律 → 404/40004「链接无效」防枚举
// （Access/Download 已把过期/超次/密码错/文件夹/删文件收口为 repository.ErrShareNotFound）
func (h *ShareHandler) failShare(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrInvalidParams):
		response.Fail(c, http.StatusBadRequest, codeInvalidParams, err.Error())
	case errors.Is(err, service.ErrTooManyAttempts):
		response.Fail(c, http.StatusTooManyRequests, codeTooMany, err.Error())
	case errors.Is(err, repository.ErrShareNotFound):
		response.Fail(c, http.StatusNotFound, codeFileNotFound, "链接无效")
	case errors.Is(err, service.ErrFileNotFound),
		errors.Is(err, repository.ErrFileNotFound):
		response.Fail(c, http.StatusNotFound, codeFileNotFound, "文件不存在")
	default:
		response.Fail(c, http.StatusInternalServerError, codeInternal, "服务器内部错误")
	}
}

// GetShareInfo GET /s/:code?password=：公开访问元数据，不鉴权；失败一律按 failShare 伪装
func (h *ShareHandler) GetShareInfo(c *gin.Context) {
	share, f, err := h.svc.AccessShare(c.Param("code"), c.Query("password"), c.ClientIP())
	if err != nil {
		h.failShare(c, err)
		return
	}
	var expStr *string
	if share.ExpireAt.Valid {
		s := share.ExpireAt.Time.Format("2006-01-02 15:04:05")
		expStr = &s
	}
	response.OK(c, dto.ShareInfoResponse{
		ShareCode: share.ShareCode,
		FileName:  f.FileName,
		FileSize:  f.FileSize,
		MimeType:  f.MimeType,
		ExpireAt:  expStr,
	})
}

// DownloadShare GET /api/v1/shares/:code/download?password=：公开流式下载，不鉴权；附件强制下载 + nosniff
func (h *ShareHandler) DownloadShare(c *gin.Context) {
	f, rc, size, err := h.svc.DownloadShare(c.Param("code"), c.Query("password"), c.ClientIP())
	if err != nil {
		h.failShare(c, err)
		return
	}
	defer func() { _ = rc.Close() }()
	ctype := contentTypeOf(f.MimeType)
	c.Header("Content-Type", ctype)
	c.Header("Content-Disposition", attachmentOf(f.FileName))
	c.Header("X-Content-Type-Options", "nosniff")
	c.DataFromReader(http.StatusOK, size, ctype, rc, nil)
}
