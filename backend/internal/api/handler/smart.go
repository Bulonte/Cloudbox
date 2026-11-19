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
	tl := make([]dto.TimelinePoint, 0, len(r.Timeline))
	for _, p := range r.Timeline {
		tl = append(tl, dto.TimelinePoint{Date: p.Date, Count: p.Count, Bytes: p.Bytes})
	}
	cat := make([]dto.CategoryItem, 0, len(r.Category))
	for _, c := range r.Category {
		cat = append(cat, dto.CategoryItem{Category: c.Category, Count: c.Count})
	}
	byType := make([]dto.StorageTypeItem, 0, len(r.Storage.ByType))
	for _, t := range r.Storage.ByType {
		byType = append(byType, dto.StorageTypeItem{Type: t.Type, Bytes: t.Bytes, Count: t.Count})
	}
	items := make([]dto.DuplicateItem, 0, len(r.Duplicates.Items))
	for _, it := range r.Duplicates.Items {
		ids := append([]uint64(nil), it.FileIDs...)
		if ids == nil {
			ids = []uint64{}
		}
		names := append([]string(nil), it.FileNames...)
		if names == nil {
			names = []string{}
		}
		items = append(items, dto.DuplicateItem{
			Kind:       it.Kind,
			FileIDs:    ids,
			FileNames:  names,
			Count:      it.Count,
			SavedBytes: it.SavedBytes,
		})
	}
	return dto.SmartStatsResponse{
		Storage: dto.StorageBlock{
			UsedBytes:  r.Storage.UsedBytes,
			QuotaBytes: r.Storage.QuotaBytes,
			FileCount:  r.Storage.FileCount,
			ByType:     byType,
		},
		Timeline: tl,
		Category: cat,
		Duplicates: dto.DuplicatesBlock{
			ExactGroups:     r.Duplicates.ExactGroups,
			ExactFiles:      r.Duplicates.ExactFiles,
			ExactSavedBytes: r.Duplicates.ExactSavedBytes,
			SimilarGroups:   r.Duplicates.SimilarGroups,
			SimilarFiles:    r.Duplicates.SimilarFiles,
			Items:           items,
		},
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
