package model

import "time"

// FileFingerprint 内容指纹表
type FileFingerprint struct {
	ID          uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	FileID      uint64    `gorm:"not null;uniqueIndex:uk_file_algo,priority:1" json:"file_id"`
	Algorithm   string    `gorm:"size:16;not null;uniqueIndex:uk_file_algo,priority:2;index:idx_algo_fp,priority:1" json:"algorithm"`
	Fingerprint string    `gorm:"size:16;not null;index:idx_algo_fp,priority:2" json:"fingerprint"`
	CreatedAt   time.Time `json:"created_at"`
}

func (FileFingerprint) TableName() string { return "file_fingerprint" }
