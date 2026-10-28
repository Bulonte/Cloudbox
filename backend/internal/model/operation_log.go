package model

import (
	"database/sql"
	"time"
)

// OperationLog 操作日志表（只增不改）
type OperationLog struct {
	ID         uint64         `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID     uint64         `gorm:"not null;index:idx_user_time,priority:1" json:"user_id"`
	Action     string         `gorm:"size:64;not null" json:"action"`
	TargetType sql.NullString `gorm:"size:32" json:"target_type,omitempty"`
	TargetID   sql.NullInt64  `json:"target_id,omitempty"`
	IP         sql.NullString `gorm:"size:64" json:"ip,omitempty"`
	Detail     sql.NullString `gorm:"type:text" json:"detail,omitempty"`
	CreatedAt  time.Time      `gorm:"index:idx_created;index:idx_user_time,priority:2" json:"created_at"`
}

func (OperationLog) TableName() string { return "operation_log" }
