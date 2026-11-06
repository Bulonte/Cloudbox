package handler

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"cloudbox/internal/api/dto"
	"cloudbox/internal/model"
	"cloudbox/internal/pkg/response"
	"cloudbox/internal/repository"
	"cloudbox/internal/service"
)

// errInvalidAuditTime 审计时间格式非法（仅内部用，不对外泄格式细节）
var errInvalidAuditTime = errors.New("审计时间格式非法")

// AdminHandler 管理查询 HTTP 层：只做 Bind/分页归一/错误码映射，不直调仓储
type AdminHandler struct {
	audit *service.AuditService
}

// NewAdminHandler 构造注入审计服务（nil=审计不可用，统一 500/90001）
func NewAdminHandler(audit *service.AuditService) *AdminHandler {
	return &AdminHandler{audit: audit}
}

// ListOperationLogs GET /api/v1/admin/operation-logs：仅 admin/super_admin（路由层 AdminOnly 已拦）
// 查询全可选：user_id/action/起止时间（RFC3339 或 "2006-01-02 15:04:05"）+ 分页归一
func (h *AdminHandler) ListOperationLogs(c *gin.Context) {
	var q dto.OperationLogQuery
	if err := c.ShouldBindQuery(&q); err != nil {
		response.Fail(c, http.StatusBadRequest, codeInvalidParams, "请求参数不正确")
		return
	}
	start, err := parseAuditTime(q.StartTime)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, codeInvalidParams, "请求参数不正确")
		return
	}
	end, err := parseAuditTime(q.EndTime)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, codeInvalidParams, "请求参数不正确")
		return
	}
	if h.audit == nil {
		response.Fail(c, http.StatusInternalServerError, codeInternal, "服务器内部错误")
		return
	}
	res, err := h.audit.List(service.AuditQuery{
		UserID:    q.UserID,
		Action:    q.Action,
		StartTime: start,
		EndTime:   end,
	}, q.Page, q.PageSize)
	if err != nil {
		if errors.Is(err, repository.ErrOperationLogUnavailable) {
			response.Fail(c, http.StatusInternalServerError, codeInternal, "服务器内部错误")
			return
		}
		response.Fail(c, http.StatusInternalServerError, codeInternal, "服务器内部错误")
		return
	}
	items := make([]dto.OperationLogItem, 0, len(res.Items))
	for _, row := range res.Items {
		items = append(items, toOperationLogItem(row))
	}
	response.OK(c, dto.OperationLogListResponse{
		Total:    res.Total,
		Page:     res.Page,
		PageSize: res.PageSize,
		Items:    items,
	})
}

// parseAuditTime 审计时间解析：空=零值（不限）；兼容 RFC3339 与本地 "2006-01-02 15:04:05"
func parseAuditTime(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	if t, err := time.ParseInLocation("2006-01-02 15:04:05", s, time.Local); err == nil {
		return t, nil
	}
	return time.Time{}, errInvalidAuditTime
}

// toOperationLogItem model 转脱敏 DTO：Null 转省略，时间统一本地格式
func toOperationLogItem(row *model.OperationLog) dto.OperationLogItem {
	if row == nil {
		return dto.OperationLogItem{}
	}
	out := dto.OperationLogItem{
		ID:        row.ID,
		UserID:    row.UserID,
		Action:    row.Action,
		CreatedAt: row.CreatedAt.Format("2006-01-02 15:04:05"),
	}
	if row.TargetType.Valid {
		v := row.TargetType.String
		out.TargetType = &v
	}
	if row.TargetID.Valid {
		v := uint64(row.TargetID.Int64)
		out.TargetID = &v
	}
	if row.IP.Valid {
		v := row.IP.String
		out.IP = &v
	}
	if row.Detail.Valid {
		v := row.Detail.String
		out.Detail = &v
	}
	return out
}
