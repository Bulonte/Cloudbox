package model

import "time"

// CategoryKeyword 分类关键词规则表
// type=category 供智能分类；type=synonym 供W9同义词复用，不建第8张表
type CategoryKeyword struct {
	ID        uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	Type      string    `gorm:"size:16;default:category;not null;uniqueIndex:uk_type_cat_kw,priority:1" json:"type"`
	Category  string    `gorm:"size:32;not null;uniqueIndex:uk_type_cat_kw,priority:2;index:idx_keyword_category" json:"category"`
	Keyword   string    `gorm:"size:64;not null;uniqueIndex:uk_type_cat_kw,priority:3" json:"keyword"`
	Weight    float64   `gorm:"type:decimal(5,2);default:1.00;not null" json:"weight"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (CategoryKeyword) TableName() string { return "category_keyword" }
