package repository

import (
	"context"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"cloudbox/internal/model"
)

// CategoryKeywordRepository 分类关键词规则仓储：全局规则表，无 owner 概念
type CategoryKeywordRepository struct {
	db *gorm.DB
}

// NewCategoryKeywordRepository 构造注入
func NewCategoryKeywordRepository(db *gorm.DB) *CategoryKeywordRepository {
	return &CategoryKeywordRepository{db: db}
}

// ListByType 按类型取全量规则（type=category 供分类；synonym 归 W9），按类/词排序保证加载确定
func (r *CategoryKeywordRepository) ListByType(ctx context.Context, typ string) ([]model.CategoryKeyword, error) {
	var rows []model.CategoryKeyword
	if err := r.db.WithContext(ctx).Where("type = ?", typ).Order("category, keyword").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("查询关键词规则: %w", err)
	}
	return rows, nil
}

// CountByCategory 按类计数（验收 8 实义类各 ≥25 用）
func (r *CategoryKeywordRepository) CountByCategory(ctx context.Context, typ string) (map[string]int64, error) {
	type cnt struct {
		Category string
		Cnt      int64
	}
	var list []cnt
	if err := r.db.WithContext(ctx).Model(&model.CategoryKeyword{}).
		Select("category, COUNT(*) AS cnt").
		Where("type = ?", typ).Group("category").Scan(&list).Error; err != nil {
		return nil, fmt.Errorf("统计关键词规则: %w", err)
	}
	out := make(map[string]int64, len(list))
	for _, c := range list {
		out[c.Category] = c.Cnt
	}
	return out, nil
}

// UpsertMany 批量幂等写入：按 uk_type_cat_kw 冲突忽略，种子可重跑
func (r *CategoryKeywordRepository) UpsertMany(ctx context.Context, rows []model.CategoryKeyword) error {
	if len(rows) == 0 {
		return nil
	}
	if err := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "type"}, {Name: "category"}, {Name: "keyword"}},
		DoNothing: true,
	}).Create(&rows).Error; err != nil {
		return fmt.Errorf("写入关键词规则: %w", err)
	}
	return nil
}
