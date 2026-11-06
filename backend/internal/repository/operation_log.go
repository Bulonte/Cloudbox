package repository

import (
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"cloudbox/internal/model"
)

// ErrOperationLogUnavailable 审计仓储未注入（供 AuditService.List 复用，防 nil 解引用）
var ErrOperationLogUnavailable = errors.New("操作日志服务不可用")

// 操作日志动作常量（白名单，与规范 §8.2.1 对齐；禁散落字符串字面量）
const (
	ActionShareCreate      = "share_create"
	ActionShareCancel      = "share_cancel"
	ActionShareAccess      = "share_access"
	ActionDownload         = "download"
	ActionPermissionChange = "permission_change"
	ActionClassify         = "classify"
)

// operationLogColumns 显列字段，禁 SELECT *（规范 §2.2.4）
const operationLogColumns = "id,user_id,action,target_type,target_id,ip,detail,created_at"

// OperationLogRepository 操作日志仓储：只增不改，审计查询
type OperationLogRepository struct {
	db *gorm.DB
}

// NewOperationLogRepository 新建操作日志仓储
func NewOperationLogRepository(db *gorm.DB) *OperationLogRepository {
	return &OperationLogRepository{db: db}
}

// CreateLog 写一条审计日志；只增不改
func (r *OperationLogRepository) CreateLog(l *model.OperationLog) error {
	if err := r.db.Create(l).Error; err != nil {
		return fmt.Errorf("写操作日志: %w", err)
	}
	return nil
}

// LogFilter 操作日志查询过滤器（零值=不限）
type LogFilter struct {
	UserID    uint64    // 0=不限
	Action    string    // 空=不限
	StartTime time.Time // 零值=不限
	EndTime   time.Time // 零值=不限
}

// ListLogs 分页查操作日志：按用户/动作/时间区间过滤，created_at DESC, id DESC 倒序
func (r *OperationLogRepository) ListLogs(f LogFilter, offset, limit int) ([]*model.OperationLog, int64, error) {
	if offset < 0 {
		offset = 0
	}
	if limit < 1 {
		limit = 20
	}
	if limit > 1000 {
		limit = 1000
	}
	db := r.db.Model(&model.OperationLog{}).Select(operationLogColumns)
	if f.UserID > 0 {
		db = db.Where("user_id = ?", f.UserID)
	}
	if f.Action != "" {
		db = db.Where("action = ?", f.Action)
	}
	if !f.StartTime.IsZero() {
		db = db.Where("created_at >= ?", f.StartTime)
	}
	if !f.EndTime.IsZero() {
		db = db.Where("created_at <= ?", f.EndTime)
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("统计操作日志: %w", err)
	}
	var rows []*model.OperationLog
	err := db.Offset(offset).Limit(limit).Order("created_at DESC, id DESC").Find(&rows).Error
	if err != nil {
		return nil, 0, fmt.Errorf("查操作日志: %w", err)
	}
	return rows, total, nil
}
