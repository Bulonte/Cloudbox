package repository

import (
	"context"
	"testing"

	"cloudbox/internal/model"
)

func TestCategoryKeywordUpsertManyIdempotent(t *testing.T) {
	db := openTestDB(t)
	repo := NewCategoryKeywordRepository(db)
	ctx := context.Background()

	seeds := []model.CategoryKeyword{
		{Type: "category", Category: "合同", Keyword: "甲方", Weight: 1.5},
		{Type: "category", Category: "合同", Keyword: "乙方", Weight: 1.5},
		{Type: "category", Category: "合同", Keyword: "违约金", Weight: 2.0},
	}
	if err := repo.UpsertMany(ctx, seeds); err != nil {
		t.Fatalf("首次写入失败: %v", err)
	}
	if err := repo.UpsertMany(ctx, seeds); err != nil {
		t.Fatalf("重跑应幂等，得到 %v", err)
	}
	counts, err := repo.CountByCategory(ctx, "category")
	if err != nil {
		t.Fatalf("计数失败: %v", err)
	}
	if counts["合同"] != 3 {
		t.Fatalf("合同应 3 条，得到 %d", counts["合同"])
	}
	if err := repo.UpsertMany(ctx, nil); err != nil {
		t.Fatalf("空写入应成功，得到 %v", err)
	}
}

func TestCategoryKeywordListByType(t *testing.T) {
	db := openTestDB(t)
	repo := NewCategoryKeywordRepository(db)
	ctx := context.Background()

	rows := []model.CategoryKeyword{
		{Type: "category", Category: "简历", Keyword: "求职", Weight: 1.0},
		{Type: "synonym", Category: "简历", Keyword: "求职", Weight: 1.0},
	}
	if err := repo.UpsertMany(ctx, rows); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	got, err := repo.ListByType(ctx, "category")
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if len(got) != 1 || got[0].Keyword != "求职" {
		t.Fatalf("类型隔离不对: %+v", got)
	}
}
