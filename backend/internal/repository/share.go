package repository

import (
	"database/sql"
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

// ListByOwner 按属主分页列出有效分享（status=1，供 S3 分享管理页用）；
// 归属在 SQL 层过滤（owner_id = ?）；按 id DESC 倒序；total 供分页用
func (r *ShareRepository) ListByOwner(ownerID uint64, offset, limit int) ([]*model.FileShare, int64, error) {
	if limit < 1 {
		limit = 20
	}
	if limit > 1000 {
		limit = 1000
	}
	if offset < 0 {
		offset = 0
	}
	cond := "owner_id = ? AND status = 1"
	var total int64
	if err := r.db.Model(&model.FileShare{}).Where(cond, ownerID).Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("统计分享数: %w", err)
	}
	var rows []*model.FileShare
	if err := r.db.Select(shareColumns).Where(cond, ownerID).
		Order("id DESC").Offset(offset).Limit(limit).Find(&rows).Error; err != nil {
		return nil, 0, fmt.Errorf("列出分享: %w", err)
	}
	return rows, total, nil
}

// SetExpireAt 调整过期时间（单测/管理校准用；生产创建后一般不变）
func (r *ShareRepository) SetExpireAt(id uint64, exp sql.NullTime) error {
	if err := r.db.Model(&model.FileShare{}).Where("id = ?", id).Update("expire_at", exp).Error; err != nil {
		return fmt.Errorf("调整过期时间: %w", err)
	}
	return nil
}

// IncrDownloadCount 下载计数原子 +1（S2 公开下载用）：单条 UPDATE 防超发；
// max_downloads 为空=不限，否则仅 download_count < max 时才 +1；
// 0 行 affect（码错/取消/刚好被抢完）一律 ErrShareNotFound，前端只见"链接无效"
func (r *ShareRepository) IncrDownloadCount(code string) error {
	res := r.db.Model(&model.FileShare{}).
		Where("share_code = ? AND status = 1 AND (max_downloads IS NULL OR download_count < max_downloads)", code).
		Update("download_count", gorm.Expr("download_count + 1"))
	if res.Error != nil {
		return fmt.Errorf("累计下载次数: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrShareNotFound
	}
	return nil
}
