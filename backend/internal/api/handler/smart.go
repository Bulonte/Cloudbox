package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"cloudbox/internal/api/dto"
	"cloudbox/internal/api/middleware"
	"cloudbox/internal/pkg/response"
	"cloudbox/internal/service"
)

// SmartHandler 仪表盘 HTTP 层：只做 Bind/错误码映射，不直调仓储
type SmartHandler struct {
	svc *service.SmartStatsService
}

// NewSmartHandler 构造注入统计服务
func NewSmartHandler(svc *service.SmartStatsService) *SmartHandler {
	return &SmartHandler{svc: svc}
}

// ToSmartStatsResponse 内部载体转脱敏 DTO（数组恒为 [] 非 null）
func ToSmartStatsResponse(r *service.SmartStatsResult) dto.SmartStatsResponse {
	tl := r.Timeline
	if tl == nil {
		tl = []dto.TimelinePoint{}
	}
	cat := r.Category
	if cat == nil {
		cat = []dto.CategoryItem{}
	}
	byType := r.Storage.ByType
	if byType == nil {
		byType = []dto.StorageTypeItem{}
	}
	items := r.Duplicates.Items
	if items == nil {
		items = []dto.DuplicateItem{}
	}
	r.Duplicates.Items = items
	r.Storage.ByType = byType
	return dto.SmartStatsResponse{
		Storage:    r.Storage,
		Timeline:   tl,
		Category:   cat,
		Duplicates: r.Duplicates,
	}
}

// GetStats GET /api/v1/smart/stats：需登录，只回本人聚合；days 非法由 Service 归一不 400
func (h *SmartHandler) GetStats(c *gin.Context) {
	var q dto.SmartStatsQuery
	if err := c.ShouldBindQuery(&q); err != nil {
		response.Fail(c, http.StatusBadRequest, codeInvalidParams, "请求参数不正确")
		return
	}
	if h.svc == nil {
		response.Fail(c, http.StatusInternalServerError, codeInternal, "服务器内部错误")
		return
	}
	res, err := h.svc.GetStats(c.GetUint64(middleware.CtxUserID), q.Days)
	if err != nil {
		if errors.Is(err, service.ErrInvalidParams) {
			response.Fail(c, http.StatusBadRequest, codeInvalidParams, err.Error())
			return
		}
		response.Fail(c, http.StatusInternalServerError, codeInternal, "服务器内部错误")
		return
	}
	response.OK(c, ToSmartStatsResponse(res))
}
