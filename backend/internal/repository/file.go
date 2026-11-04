package repository

import (
	"errors"
	"fmt"

	"gorm.io/gorm"

	"cloudbox/internal/model"
)

// ErrFileNotFound 文件不存在（含越权/已删/父夹非法统一伪装，不暴露存在性）
var ErrFileNotFound = errors.New("文件不存在")

// ErrFileConflict 同一目录下同名文件已存在
var ErrFileConflict = errors.New("同名文件已存在")

// ErrInvalidParent 父目录非法（不存在/非文件夹/他人所有）
var ErrInvalidParent = errors.New("父目录非法")

// fileColumns 显列字段，禁 SELECT *（规范 §2.2.4）
const fileColumns = "id,file_name,file_hash,file_size,file_path,mime_type,parent_id,owner_id,is_folder,is_deleted,category,confidence,created_at,updated_at"

// FileRepository 文件表专用仓储：CRUD + 归属过滤（SQL 层带 owner_id，不查出再判）
type FileRepository struct {
	*BaseRepository[model.File]
	db *gorm.DB
}

// NewFileRepository 新建文件仓储
func NewFileRepository(db *gorm.DB) *FileRepository {
	return &FileRepository{
		BaseRepository: NewBaseRepository[model.File](db),
		db:             db,
	}
}

// CreateFile 创建文件/文件夹行；同 hash 冲突返回 ErrFileConflict（uk_hash）
func (r *FileRepository) CreateFile(f *model.File) error {
	if err := r.db.Create(f).Error; err != nil {
		if isUniqueViolation(err) {
			return ErrFileConflict
		}
		return fmt.Errorf("创建文件: %w", err)
	}
	return nil
}

// FindByID 按主键查（带归属：owner 不一致/已删一律 ErrFileNotFound）
func (r *FileRepository) FindByID(id, ownerID uint64) (*model.File, error) {
	var f model.File
	err := r.db.Select(fileColumns).Where("id = ? AND owner_id = ? AND is_deleted = 0", id, ownerID).First(&f).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrFileNotFound
		}
		return nil, fmt.Errorf("查询文件: %w", err)
	}
	return &f, nil
}

// FindByHash 按 hash 查同人正常行；无行返回 nil,nil（幂等短路用），仅 DB 错才返回 error
func (r *FileRepository) FindByHash(hash string, ownerID uint64) (*model.File, error) {
	var f model.File
	err := r.db.Select(fileColumns).Where("file_hash = ? AND owner_id = ? AND is_deleted = 0", hash, ownerID).First(&f).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("按哈希查文件: %w", err)
	}
	return &f, nil
}

// FindAnyByHash 全局按 hash 查一正常行（S4 秒传跨用户提示用，不带 owner 归属）；
// 无行返回 nil,nil（调用方判 miss），仅 DB 错才返回 error；软删行不可见
func (r *FileRepository) FindAnyByHash(hash string) (*model.File, error) {
	var f model.File
	err := r.db.Select(fileColumns).Where("file_hash = ? AND is_deleted = 0", hash).First(&f).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("按哈希全局查文件: %w", err)
	}
	return &f, nil
}

// ListByParent 按父目录分页（文件夹置顶）；total 供 S3 分页用
func (r *FileRepository) ListByParent(ownerID, parentID uint64, offset, limit int) ([]*model.File, int64, error) {
	if limit < 1 {
		limit = 20
	}
	if limit > 1000 {
		limit = 1000
	}
	if offset < 0 {
		offset = 0
	}
	cond := "owner_id = ? AND parent_id = ? AND is_deleted = 0"
	var total int64
	if err := r.db.Model(&model.File{}).Where(cond, ownerID, parentID).Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("统计文件数: %w", err)
	}
	var files []*model.File
	if err := r.db.Select(fileColumns).Where(cond, ownerID, parentID).
		Order("is_folder DESC, id DESC").Offset(offset).Limit(limit).Find(&files).Error; err != nil {
		return nil, 0, fmt.Errorf("列出文件: %w", err)
	}
	return files, total, nil
}

// Rename 重命名；0 行影响转 ErrFileNotFound
func (r *FileRepository) Rename(id, ownerID uint64, name string) error {
	res := r.db.Model(&model.File{}).Where("id = ? AND owner_id = ? AND is_deleted = 0", id, ownerID).Update("file_name", name)
	if res.Error != nil {
		return fmt.Errorf("重命名文件: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrFileNotFound
	}
	return nil
}

// Move 移动；目标夹须同人所有且为文件夹，否则 ErrInvalidParent
func (r *FileRepository) Move(id, ownerID, parentID uint64) error {
	if parentID != 0 {
		var parent model.File
		err := r.db.Select("id").Where("id = ? AND owner_id = ? AND is_folder = 1 AND is_deleted = 0", parentID, ownerID).First(&parent).Error
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrInvalidParent
			}
			return fmt.Errorf("校验父目录: %w", err)
		}
	}
	res := r.db.Model(&model.File{}).Where("id = ? AND owner_id = ? AND is_deleted = 0", id, ownerID).Update("parent_id", parentID)
	if res.Error != nil {
		return fmt.Errorf("移动文件: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrFileNotFound
	}
	return nil
}

// SoftDelete 软删（is_deleted=1）；不释放配额（规范 §6.7，彻底删除时释放）
func (r *FileRepository) SoftDelete(id, ownerID uint64) error {
	res := r.db.Model(&model.File{}).Where("id = ? AND owner_id = ? AND is_deleted = 0", id, ownerID).Update("is_deleted", 1)
	if res.Error != nil {
		return fmt.Errorf("删除文件: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrFileNotFound
	}
	return nil
}
