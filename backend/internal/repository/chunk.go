package repository

import (
	"errors"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"cloudbox/internal/model"
)

// ErrChunkNotFound 分片会话不存在（含越权伪装，他人 upload_id 一律 404）
var ErrChunkNotFound = errors.New("分片会话不存在")

// ErrChunkConflict 分片会话冲突（upload_id 重撞，理论上仅 UUID 碰撞）
var ErrChunkConflict = errors.New("分片会话已存在")

// ChunkSessionMarker 会话占位行标记：upload_chunk 表无独立会话表，
// S1 初始化时写一行 ChunkIndex=0xFFFFFFFF 的占位行存会话参数
// （file_hash/total_chunks/chunk_size/owner），S2 真分片行与之区分。
const ChunkSessionMarker = ^uint32(0)

// chunkColumns 显列字段，禁 SELECT *（规范 §2.2.4）
const chunkColumns = "id,upload_id,file_hash,chunk_index,total_chunks,chunk_size,chunk_path,owner_id,status,created_at,updated_at"

// ChunkRepository 分片上传记录仓储：会话占位 + 分片行 CRUD，归属一律 SQL 层过滤
type ChunkRepository struct {
	db *gorm.DB
}

// NewChunkRepository 新建分片仓储
func NewChunkRepository(db *gorm.DB) *ChunkRepository {
	return &ChunkRepository{db: db}
}

// CreateInitRow 建会话占位行；upload_id 撞 uk（UUID 碰撞）返回 ErrChunkConflict 供重试
func (r *ChunkRepository) CreateInitRow(row *model.UploadChunk) error {
	if err := r.db.Create(row).Error; err != nil {
		if isUniqueViolation(err) {
			return ErrChunkConflict
		}
		return fmt.Errorf("创建分片会话: %w", err)
	}
	return nil
}

// FindByUploadID 按会话查全量行（占位行 + 分片行，带归属：他人一律 ErrChunkNotFound）
func (r *ChunkRepository) FindByUploadID(uploadID string, ownerID uint64) ([]*model.UploadChunk, error) {
	var rows []*model.UploadChunk
	if err := r.db.Select(chunkColumns).Where("upload_id = ? AND owner_id = ?", uploadID, ownerID).Order("chunk_index ASC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("查询分片会话: %w", err)
	}
	if len(rows) == 0 {
		return nil, ErrChunkNotFound
	}
	return rows, nil
}

// FindSession 仅查会话占位行（S2/S3 校验会话参数用）；无占位行即 ErrChunkNotFound
func (r *ChunkRepository) FindSession(uploadID string, ownerID uint64) (*model.UploadChunk, error) {
	var s model.UploadChunk
	err := r.db.Select(chunkColumns).Where("upload_id = ? AND owner_id = ? AND chunk_index = ?", uploadID, ownerID, ChunkSessionMarker).First(&s).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrChunkNotFound
		}
		return nil, fmt.Errorf("查询分片会话: %w", err)
	}
	return &s, nil
}

// ListUploadedIndexes 已完成分片序号（status=1 且非占位行，供 S6 progress 复用；空返回 [] 非 nil）
func (r *ChunkRepository) ListUploadedIndexes(uploadID string, ownerID uint64) ([]uint32, error) {
	var idx []uint32
	if err := r.db.Model(&model.UploadChunk{}).Where("upload_id = ? AND owner_id = ? AND status = 1 AND chunk_index != ?", uploadID, ownerID, ChunkSessionMarker).Pluck("chunk_index", &idx).Error; err != nil {
		return nil, fmt.Errorf("查询已传分片: %w", err)
	}
	if idx == nil {
		idx = []uint32{}
	}
	return idx, nil
}

// FindChunk 查单分片行（S2 覆盖前可选读，S3 合并校验复用）；无行返回 ErrChunkNotFound
func (r *ChunkRepository) FindChunk(uploadID string, ownerID uint64, index uint32) (*model.UploadChunk, error) {
	var row model.UploadChunk
	err := r.db.Select(chunkColumns).Where("upload_id = ? AND owner_id = ? AND chunk_index = ?", uploadID, ownerID, index).First(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrChunkNotFound
		}
		return nil, fmt.Errorf("查询分片: %w", err)
	}
	return &row, nil
}

// UpsertChunkDone 分片行幂等置完成；不存在则 INSERT，存在则 UPDATE status=1
// 归属全程带 owner_id：先按三元组 UPDATE，0 行再 INSERT；INSERT 撞 uk 说明行属他人，转 ErrChunkNotFound 防覆盖
func (r *ChunkRepository) UpsertChunkDone(uploadID string, ownerID uint64, index uint32, totalChunks uint32, chunkSize uint64, fileHash, chunkPath string) error {
	res := r.db.Model(&model.UploadChunk{}).Where("upload_id = ? AND owner_id = ? AND chunk_index = ?", uploadID, ownerID, index).Updates(map[string]any{
		"file_hash":    fileHash,
		"total_chunks": totalChunks,
		"chunk_size":   chunkSize,
		"chunk_path":   chunkPath,
		"status":       1,
	})
	if res.Error != nil {
		return fmt.Errorf("更新分片: %w", res.Error)
	}
	if res.RowsAffected == 1 {
		return nil
	}
	row := &model.UploadChunk{
		UploadID:    uploadID,
		FileHash:    fileHash,
		ChunkIndex:  index,
		TotalChunks: totalChunks,
		ChunkSize:   chunkSize,
		ChunkPath:   chunkPath,
		OwnerID:     ownerID,
		Status:      1,
	}
	if err := r.db.Clauses(clause.OnConflict{DoNothing: true}).Create(row).Error; err != nil {
		if isUniqueViolation(err) {
			return ErrChunkNotFound
		}
		return fmt.Errorf("写入分片: %w", err)
	}
	if row.ID == 0 {
		// DoNothing 生效：行已存在（属他人或并发写入），走归属 UPDATE 兜底
		res2 := r.db.Model(&model.UploadChunk{}).Where("upload_id = ? AND owner_id = ? AND chunk_index = ?", uploadID, ownerID, index).Updates(map[string]any{
			"file_hash":    fileHash,
			"total_chunks": totalChunks,
			"chunk_size":   chunkSize,
			"chunk_path":   chunkPath,
			"status":       1,
		})
		if res2.Error != nil {
			return fmt.Errorf("更新分片: %w", res2.Error)
		}
		if res2.RowsAffected == 0 {
			return ErrChunkNotFound
		}
	}
	return nil
}

// MarkDone 分片行置完成（S1 占位）；0 行影响转 ErrChunkNotFound
func (r *ChunkRepository) MarkDone(uploadID string, ownerID uint64, index uint32) error {
	res := r.db.Model(&model.UploadChunk{}).Where("upload_id = ? AND owner_id = ? AND chunk_index = ?", uploadID, ownerID, index).Update("status", 1)
	if res.Error != nil {
		return fmt.Errorf("标记分片完成: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrChunkNotFound
	}
	return nil
}

// Cancel 会话取消（S3 合并后清理/超时清理用，占位行 + 分片行全置 status=2）
func (r *ChunkRepository) Cancel(uploadID string, ownerID uint64) error {
	if err := r.db.Model(&model.UploadChunk{}).Where("upload_id = ? AND owner_id = ?", uploadID, ownerID).Update("status", 2).Error; err != nil {
		return fmt.Errorf("取消分片会话: %w", err)
	}
	return nil
}
