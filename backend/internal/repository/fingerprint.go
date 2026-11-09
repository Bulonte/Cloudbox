package repository

import (
	"context"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"cloudbox/internal/model"
)

// fingerprintColumns 显列字段，禁 SELECT *（规范 §2.2.4）
const fingerprintColumns = "id,file_id,algorithm,fingerprint,created_at"

// FingerprintRepository 内容指纹仓储：纯存取 + 粗筛，不算距离不判阈值
type FingerprintRepository struct {
	db *gorm.DB
}

// NewFingerprintRepository 新建指纹仓储
func NewFingerprintRepository(db *gorm.DB) *FingerprintRepository {
	return &FingerprintRepository{db: db}
}

// Upsert 幂等写：按 uk_file_algo(file_id, algorithm) 冲突覆盖 fingerprint
// 指纹是幂等覆盖语义，冲突不报 ErrFileConflict
func (r *FingerprintRepository) Upsert(ctx context.Context, fileID uint64, algorithm, fingerprint string) error {
	row := &model.FileFingerprint{FileID: fileID, Algorithm: algorithm, Fingerprint: fingerprint}
	if err := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "file_id"}, {Name: "algorithm"}},
		DoUpdates: clause.AssignmentColumns([]string{"fingerprint"}),
	}).Create(row).Error; err != nil {
		return fmt.Errorf("写入指纹: %w", err)
	}
	return nil
}

// GetByFile 取单文件单算法指纹；无行返回 nil,nil（调用方判 miss，与 FindByHash 口径一致）
func (r *FingerprintRepository) GetByFile(ctx context.Context, fileID uint64, algorithm string) (*model.FileFingerprint, error) {
	var row model.FileFingerprint
	err := r.db.WithContext(ctx).Select(fingerprintColumns).
		Where("file_id = ? AND algorithm = ?", fileID, algorithm).First(&row).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("查询指纹: %w", err)
	}
	return &row, nil
}

// FingerprintCandidate 粗筛行：自带归属 + 文件名，避免 Service N+1 回查
type FingerprintCandidate struct {
	FileID      uint64
	Fingerprint string
	FileName    string
}

// ListCandidates 按 owner+algorithm 粗筛：JOIN file 滤软删；按 file_id 升序保证确定性
// file_fingerprint 表无 owner_id，归属必须经 JOIN，不可单表 WHERE owner_id
func (r *FingerprintRepository) ListCandidates(ctx context.Context, ownerID uint64, algorithm string) ([]FingerprintCandidate, error) {
	var out []FingerprintCandidate
	if err := r.db.WithContext(ctx).Table("file_fingerprint AS fp").
		Select("fp.file_id, fp.fingerprint, f.file_name").
		Joins("JOIN file f ON f.id = fp.file_id").
		Where("f.owner_id = ? AND f.is_deleted = 0 AND fp.algorithm = ?", ownerID, algorithm).
		Order("fp.file_id ASC").
		Scan(&out).Error; err != nil {
		return nil, fmt.Errorf("粗筛指纹候选: %w", err)
	}
	return out, nil
}

// DeleteByFile 删文件时显式清理指纹行（表有 FK CASCADE，此方法供单测/后台任务用）
func (r *FingerprintRepository) DeleteByFile(ctx context.Context, fileID uint64) error {
	if err := r.db.WithContext(ctx).Where("file_id = ?", fileID).Delete(&model.FileFingerprint{}).Error; err != nil {
		return fmt.Errorf("删除指纹: %w", err)
	}
	return nil
}
