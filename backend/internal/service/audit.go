package service

import (
	"database/sql"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"cloudbox/internal/model"
	"cloudbox/internal/repository"
)

// 审计明细字段黑名单：命中即整键丢弃（规范 §2.3 禁记密码/Token/分享码明文）
var auditSensitiveKeys = []string{
	"password", "passwd", "pwd", "token", "secret", "authorization",
	"jwt", "cookie", "session", "share_code", "sharecode", "credential",
}

// auditDetailMaxLen 明细 JSON 长度上限，防单条撑爆 TEXT
const auditDetailMaxLen = 4096

// AuditInput 审计埋点入参（原始明细由 Service 层统一脱敏）
type AuditInput struct {
	UserID     uint64
	Action     string
	TargetType string
	TargetID   uint64
	IP         string
	Detail     map[string]any
}

// AuditQuery 审计查询条件（零值=不限）
type AuditQuery struct {
	UserID    uint64
	Action    string
	StartTime time.Time
	EndTime   time.Time
}

// AuditResult 审计查询结果（分页元信息）
type AuditResult struct {
	Items    []*model.OperationLog `json:"items"`
	Total    int64                 `json:"total"`
	Page     int                   `json:"page"`
	PageSize int                   `json:"page_size"`
}

// AuditService 审计服务：统一脱敏后落库 + 管理员查询
type AuditService struct {
	logs *repository.OperationLogRepository
}

// NewAuditService 构造注入审计服务
func NewAuditService(logs *repository.OperationLogRepository) *AuditService {
	return &AuditService{logs: logs}
}

// Record 写一条审计日志：白名单动作校验 → 明细脱敏 → JSON 落库。
// 审计是旁路，写失败只记应用日志，绝不阻断主业务流程。
func (s *AuditService) Record(in AuditInput) error {
	if s == nil || s.logs == nil {
		return nil
	}
	if !isAuditActionAllowed(in.Action) {
		slog.Error("审计动作非法，已丢弃", "action", in.Action)
		return nil
	}
	detail, ok := sanitizeAuditDetail(in.Detail)
	if !ok {
		slog.Error("审计明细含敏感信息，已整条丢弃", "action", in.Action)
		return nil
	}
	row := &model.OperationLog{
		UserID:     in.UserID,
		Action:     in.Action,
		TargetType: nullString(in.TargetType),
		TargetID:   nullInt64(in.TargetID),
		IP:         nullString(in.IP),
		Detail:     nullString(detail),
	}
	if err := s.logs.CreateLog(row); err != nil {
		slog.Error("写审计日志失败（不影响主流程）", "action", in.Action, "err", err)
		return nil
	}
	return nil
}

// List 查询审计日志（供管理员接口），带分页归一
func (s *AuditService) List(q AuditQuery, page, pageSize int) (*AuditResult, error) {
	if s == nil || s.logs == nil {
		return nil, repository.ErrOperationLogUnavailable
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 1000 {
		pageSize = 1000
	}
	offset := (page - 1) * pageSize
	rows, total, err := s.logs.ListLogs(repository.LogFilter{
		UserID:    q.UserID,
		Action:    q.Action,
		StartTime: q.StartTime,
		EndTime:   q.EndTime,
	}, offset, pageSize)
	if err != nil {
		return nil, err
	}
	return &AuditResult{Items: rows, Total: total, Page: page, PageSize: pageSize}, nil
}

// isAuditActionAllowed 校验动作是否在白名单内（fail-closed）
func isAuditActionAllowed(action string) bool {
	switch action {
	case repository.ActionShareCreate, repository.ActionShareCancel,
		repository.ActionShareAccess, repository.ActionDownload,
		repository.ActionPermissionChange, repository.ActionClassify:
		return true
	default:
		return false
	}
}

// sanitizeAuditDetail 明细脱敏：黑名单键整条丢弃；值形似 bcrypt/JWT 则整条丢弃；超长截断。
// 返回 (脱敏后JSON, 是否可写入)；ok=false 表示整条明细不可落库。
func sanitizeAuditDetail(detail map[string]any) (string, bool) {
	if len(detail) == 0 {
		return "", true
	}
	clean := make(map[string]any, len(detail))
	for k, v := range detail {
		if isSensitiveKey(k) {
			continue
		}
		if containsSecretValue(v) {
			return "", false
		}
		clean[k] = v
	}
	raw, err := json.Marshal(clean)
	if err != nil {
		return "", false
	}
	if len(raw) > auditDetailMaxLen {
		raw = raw[:auditDetailMaxLen]
	}
	return string(raw), true
}

// isSensitiveKey 判断键名是否命中敏感黑名单（大小写不敏感）
func isSensitiveKey(key string) bool {
	lower := strings.ToLower(key)
	for _, bad := range auditSensitiveKeys {
		if strings.Contains(lower, bad) {
			return true
		}
	}
	return false
}

// containsSecretValue 判断值是否形似 bcrypt 哈希或 JWT（出现即整条明细丢弃）
func containsSecretValue(v any) bool {
	switch t := v.(type) {
	case string:
		return looksLikeSecret(t)
	case []any:
		for _, item := range t {
			if containsSecretValue(item) {
				return true
			}
		}
	case map[string]any:
		for _, item := range t {
			if containsSecretValue(item) {
				return true
			}
		}
	}
	return false
}

// looksLikeSecret 识别 bcrypt 前缀或 JWT 三段式形态
func looksLikeSecret(s string) bool {
	if strings.HasPrefix(s, "$2a$") || strings.HasPrefix(s, "$2b$") || strings.HasPrefix(s, "$2y$") {
		return true
	}
	parts := strings.Split(s, ".")
	return len(parts) == 3 && parts[0] != "" && parts[1] != "" && parts[2] != ""
}

// nullString 空串转 NULL（审计列全可空，禁存空串占位）
func nullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

// nullInt64 零值转 NULL（target_id=0 表示无目标，禁存 0 占位）
func nullInt64(n uint64) sql.NullInt64 {
	if n == 0 {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: int64(n), Valid: true}
}
