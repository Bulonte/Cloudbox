package model

import "time"

// UploadChunk 分片上传记录表
type UploadChunk struct {
	ID          uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	UploadID    string    `gorm:"size:36;not null;uniqueIndex:uk_upload_chunk,priority:1;index:idx_upload" json:"upload_id"`
	FileHash    string    `gorm:"size:64;not null" json:"file_hash"`
	ChunkIndex  uint32    `gorm:"not null;uniqueIndex:uk_upload_chunk,priority:2" json:"chunk_index"`
	TotalChunks uint32    `gorm:"not null" json:"total_chunks"`
	ChunkSize   uint64    `gorm:"default:0;not null" json:"chunk_size"`
	ChunkPath   string    `gorm:"size:500;not null" json:"-"`
	OwnerID     uint64    `gorm:"not null;index:idx_owner_hash,priority:1" json:"owner_id"`
	Status      int8      `gorm:"default:0;not null" json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (UploadChunk) TableName() string { return "upload_chunk" }
