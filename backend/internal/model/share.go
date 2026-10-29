package model

import (
	"database/sql"
	"time"
)

// FileShare 分享表
type FileShare struct {
	ID            uint64         `gorm:"primaryKey;autoIncrement" json:"id"`
	ShareCode     string         `gorm:"size:16;uniqueIndex:uk_share_code;not null" json:"share_code"`
	FileID        uint64         `gorm:"not null;index:idx_file" json:"file_id"`
	OwnerID       uint64         `gorm:"not null;index:idx_owner" json:"owner_id"`
	PasswordHash  sql.NullString `gorm:"size:255" json:"-"`
	ExpireAt      sql.NullTime   `json:"expire_at,omitempty"`
	MaxDownloads  sql.NullInt32  `json:"max_downloads,omitempty"`
	DownloadCount uint32         `gorm:"default:0;not null" json:"download_count"`
	Status        int8           `gorm:"default:1;not null" json:"status"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
}

func (FileShare) TableName() string { return "file_share" }
