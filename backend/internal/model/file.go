package model

import (
	"database/sql"
	"time"
)

// File 文件表
type File struct {
	ID         uint64          `gorm:"primaryKey;autoIncrement" json:"id"`
	FileName   string          `gorm:"size:255;not null" json:"file_name"`
	FileHash   string          `gorm:"size:64;uniqueIndex:uk_hash;not null" json:"file_hash"`
	FileSize   uint64          `gorm:"default:0;not null" json:"file_size"`
	FilePath   string          `gorm:"size:500;not null" json:"file_path"`
	ParentID   uint64          `gorm:"default:0;not null;index:idx_parent_deleted" json:"parent_id"`
	OwnerID    uint64          `gorm:"not null;index:idx_owner_deleted" json:"owner_id"`
	IsFolder   int8            `gorm:"default:0;not null" json:"is_folder"`
	IsDeleted  int8            `gorm:"default:0;not null;index:idx_parent_deleted,priority:2;index:idx_owner_deleted,priority:2" json:"is_deleted"`
	Category   sql.NullString  `gorm:"size:32;index:idx_category" json:"category,omitempty"`
	Confidence sql.NullFloat64 `gorm:"type:decimal(4,3)" json:"confidence,omitempty"`
	CreatedAt  time.Time       `json:"created_at"`
	UpdatedAt  time.Time       `json:"updated_at"`
}

func (File) TableName() string { return "file" }
