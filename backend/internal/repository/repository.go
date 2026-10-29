package repository

import (
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// BaseRepository 泛型通用 CRUD，T 为 model 指针类型
type BaseRepository[T any] struct {
	db *gorm.DB
}

func NewBaseRepository[T any](db *gorm.DB) *BaseRepository[T] {
	return &BaseRepository[T]{db: db}
}

func (r *BaseRepository[T]) Create(entity *T) error {
	return r.db.Create(entity).Error
}

func (r *BaseRepository[T]) GetByID(id uint64, out *T) error {
	return r.db.First(out, id).Error
}

// ListPage 分页查：page从1开始，pageSize上限1000（规范§2.4.3）
func (r *BaseRepository[T]) ListPage(page, pageSize int, out *[]T, scopes ...func(*gorm.DB) *gorm.DB) (int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 1000 {
		pageSize = 1000
	}
	db := r.db.Model(out)
	for _, s := range scopes {
		db = s(db)
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return 0, err
	}
	err := db.Offset((page - 1) * pageSize).Limit(pageSize).Find(out).Error
	return total, err
}

func (r *BaseRepository[T]) UpdateByID(id uint64, updates map[string]any) error {
	var entity T
	return r.db.Model(&entity).Where("id = ?", id).Updates(updates).Error
}

func (r *BaseRepository[T]) DeleteByID(id uint64) error {
	var entity T
	return r.db.Delete(&entity, id).Error
}

// Upsert 存在则更新、不存在则插入（按唯一约束冲突列判定）
func (r *BaseRepository[T]) Upsert(entity *T, conflictCols []clause.Column, updateCols []string) error {
	return r.db.Clauses(clause.OnConflict{
		Columns:   conflictCols,
		DoUpdates: clause.AssignmentColumns(updateCols),
	}).Create(entity).Error
}
