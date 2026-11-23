package repository

import (
	"fmt"
	"time"
)

// MimeStat 按 mime 聚合：原始 mime + 字节和 + 文件数（type 映射放 Service）
type MimeStat struct {
	MimeType string `gorm:"column:mime_type"`
	Bytes    int64  `gorm:"column:bytes"`
	Count    int64  `gorm:"column:count"`
}

// CategoryStat 按 category 聚合：空串由 Service 归一为"待确认"
type CategoryStat struct {
	Category string `gorm:"column:category"`
	Count    int64  `gorm:"column:count"`
}

// DailyStat 按天聚合：Date 为 YYYY-MM-DD（sqlite DATE() 与 MySQL DATE() 均适用）
type DailyStat struct {
	Date  string `gorm:"column:date"`
	Count int64  `gorm:"column:count"`
	Bytes int64  `gorm:"column:bytes"`
}

// HashGroup 精确重复组：同 hash 多文件（uk_hash 全局唯一约束下本人内通常为空，保留口径防回归）
type HashGroup struct {
	FileHash string `gorm:"column:file_hash"`
	Size     int64  `gorm:"column:size"`
	Count    int64  `gorm:"column:count"`
}

// fileStatsCond 统计通用条件：本人 + 未删 + 非文件夹
const fileStatsCond = "owner_id = ? AND is_deleted = 0 AND is_folder = 0"

// SumSizeByOwner 本人文件总字节 + 总数；空库回 0,0（SUM NULL 兜零）
func (r *FileRepository) SumSizeByOwner(ownerID uint64) (uint64, int64, error) {
	var a struct {
		Bytes int64 `gorm:"column:bytes"`
		Count int64 `gorm:"column:count"`
	}
	if err := r.db.Table("file").Select("COALESCE(SUM(file_size),0) AS bytes, COUNT(*) AS count").
		Where(fileStatsCond, ownerID).Scan(&a).Error; err != nil {
		return 0, 0, fmt.Errorf("统计存储总量: %w", err)
	}
	if a.Bytes < 0 {
		a.Bytes = 0
	}
	return uint64(a.Bytes), a.Count, nil
}

// GroupSizeByMime 按 mime_type 聚合字节与数量
func (r *FileRepository) GroupSizeByMime(ownerID uint64) ([]MimeStat, error) {
	var out []MimeStat
	if err := r.db.Table("file").Select("mime_type AS mime_type, COALESCE(SUM(file_size),0) AS bytes, COUNT(*) AS count").
		Where(fileStatsCond, ownerID).Group("mime_type").Scan(&out).Error; err != nil {
		return nil, fmt.Errorf("按类型统计: %w", err)
	}
	if out == nil {
		out = []MimeStat{}
	}
	return out, nil
}

// GroupCountByCategory 按 category 聚合数量（含空串，归一放 Service）
func (r *FileRepository) GroupCountByCategory(ownerID uint64) ([]CategoryStat, error) {
	var out []CategoryStat
	if err := r.db.Table("file").Select("COALESCE(category,'') AS category, COUNT(*) AS count").
		Where(fileStatsCond, ownerID).Group("category").Order("count DESC").Scan(&out).Error; err != nil {
		return nil, fmt.Errorf("按分类统计: %w", err)
	}
	if out == nil {
		out = []CategoryStat{}
	}
	return out, nil
}

// GroupDaily 按天聚合（since 起含当天，调用方保证 since 为零点）
func (r *FileRepository) GroupDaily(ownerID uint64, since time.Time) ([]DailyStat, error) {
	var out []DailyStat
	if err := r.db.Table("file").Select("DATE(created_at) AS date, COUNT(*) AS count, COALESCE(SUM(file_size),0) AS bytes").
		Where(fileStatsCond+" AND created_at >= ?", ownerID, since).Group("DATE(created_at)").Order("date ASC").Scan(&out).Error; err != nil {
		return nil, fmt.Errorf("按天统计: %w", err)
	}
	if out == nil {
		out = []DailyStat{}
	}
	return out, nil
}

// ExactDuplicateGroups 精确重复组：同 hash 数量>1（uk_hash 全局唯一，本人口径通常为空；保留供回归/口径冻结）
// size 取 MAX，同一内容大小一致
func (r *FileRepository) ExactDuplicateGroups(ownerID uint64) ([]HashGroup, error) {
	var out []HashGroup
	if err := r.db.Table("file").Select("file_hash AS file_hash, MAX(file_size) AS size, COUNT(*) AS count").
		Where(fileStatsCond, ownerID).Group("file_hash").Having("COUNT(*) > 1").Scan(&out).Error; err != nil {
		return nil, fmt.Errorf("统计精确重复: %w", err)
	}
	if out == nil {
		out = []HashGroup{}
	}
	return out, nil
}

// DuplicateFileRow 重复组内文件行（items 明细用，按 id 升序确定性）
type DuplicateFileRow struct {
	ID       uint64 `gorm:"column:id"`
	FileName string `gorm:"column:file_name"`
	FileSize uint64 `gorm:"column:file_size"`
}

// ListFilesByHash 同 hash 文件行（items 明细用，上限由调用方传 limit）
func (r *FileRepository) ListFilesByHash(ownerID uint64, hash string, limit int) ([]DuplicateFileRow, error) {
	if limit < 1 {
		limit = 10
	}
	if limit > 50 {
		limit = 50
	}
	var out []DuplicateFileRow
	if err := r.db.Table("file").Select("id AS id, file_name AS file_name, file_size AS file_size").
		Where(fileStatsCond+" AND file_hash = ?", ownerID, hash).Order("id ASC").Limit(limit).Scan(&out).Error; err != nil {
		return nil, fmt.Errorf("查重复明细: %w", err)
	}
	if out == nil {
		out = []DuplicateFileRow{}
	}
	return out, nil
}
