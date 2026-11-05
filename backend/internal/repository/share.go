package repository

import (
	"errors"
	"fmt"

	"gorm.io/gorm"

	"cloudbox/internal/model"
)

// ErrShareNotFound 分享不存在（含已取消/过期伪装，不暴露存在性）
var ErrShareNotFound = errors.New("分享不存在")

// ErrShareCodeConflict 分享码已存在（uk_share_code 碰撞，调用方重生成重试）
var ErrShareCodeConflict = errors.New("分享码已存在")

// shareColumns 显列字段，禁 SELECT *（规范 §2.2.4）
const shareColumns = "id,share_code,file_id,owner_id,password_hash,expire_at,max_downloads,download_count,status,created_at,updated_at"

// ShareRepository 分享仓储：创建 + 按码查询，归属一律 SQL 层过滤
type ShareRepository struct {
	db *gorm.DB
}

// NewShareRepository 新建分享仓储
func NewShareRepository(db *gorm.DB) *ShareRepository {
	return &ShareRepository{db: db}
}

// CreateShare 创建分享行；share_code 撞 uk 返回 ErrShareCodeConflict 供重试
func (r *ShareRepository) CreateShare(s *model.FileShare) error {
	if err := r.db.Create(s).Error; err != nil {
		if isUniqueViolation(err) {
			return ErrShareCodeConflict
		}
		return fmt.Errorf("创建分享: %w", err)
	}
	return nil
}

// FindByCode 按分享码查有效行（status=1）；无行/已取消一律 ErrShareNotFound
func (r *ShareRepository) FindByCode(code string) (*model.FileShare, error) {
	var s model.FileShare
	err := r.db.Select(shareColumns).Where("share_code = ? AND status = 1", code).First(&s).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrShareNotFound
		}
		return nil, fmt.Errorf("查询分享: %w", err)
	}
	return &s, nil
}

// FindByID 按主键查（带归属：owner 不一致一律 ErrShareNotFound）
func (r *ShareRepository) FindByID(id, ownerID uint64) (*model.FileShare, error) {
	var s model.FileShare
	err := r.db.Select(shareColumns).Where("id = ? AND owner_id = ? AND status = 1", id, ownerID).First(&s).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrShareNotFound
		}
		return nil, fmt.Errorf("查询分享: %w", err)
	}
	return &s, nil
}

// CancelShare 取消分享（status=1→0，幂等：重复取消仍成功）；他人/不存在一律 ErrShareNotFound
func (r *ShareRepository) CancelShare(id, ownerID uint64) error {
	res := r.db.Model(&model.FileShare{}).Where("id = ? AND owner_id = ? AND status = 1", id, ownerID).Update("status", 0)
	if res.Error != nil {
		return fmt.Errorf("取消分享: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrShareNotFound
	}
	return nil
}
