package smart

import (
	"context"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"cloudbox/internal/model"
	"cloudbox/internal/repository"
)

func newLoaderRepo(t *testing.T) *repository.CategoryKeywordRepository {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	if err != nil {
		t.Fatalf("open sqlite memory: %v", err)
	}
	if err := db.AutoMigrate(&model.CategoryKeyword{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	return repository.NewCategoryKeywordRepository(db)
}

func TestRuleLoaderEmpty(t *testing.T) {
	repo := newLoaderRepo(t)
	loader := NewRuleLoader(repo)
	if _, err := loader.Load(context.Background()); err != ErrEmptyRules {
		t.Fatalf("空表应 ErrEmptyRules，得到 %v", err)
	}
}

func TestRuleLoaderSkipsDirty(t *testing.T) {
	repo := newLoaderRepo(t)
	ctx := context.Background()
	rows := []model.CategoryKeyword{
		{Type: "category", Category: "合同", Keyword: "违约金", Weight: 2.0},
		{Type: "category", Category: "不存在的类", Keyword: "脏行", Weight: 1.0},
		{Type: "category", Category: "简历", Keyword: "   ", Weight: 1.0},
		{Type: "category", Category: "报表", Keyword: "营收", Weight: 0},
		{Type: "category", Category: "报表", Keyword: "审计", Weight: -3},
	}
	if err := repo.UpsertMany(ctx, rows); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	loader := NewRuleLoader(repo)
	res, err := loader.LoadWithStats(ctx)
	if err != nil {
		t.Fatalf("加载失败: %v", err)
	}
	if res.Skipped != 2 {
		t.Fatalf("应跳过 2 行脏数据，得到 skipped=%d total=%d", res.Skipped, res.Total)
	}
	if res.Keywords["违约金"].weight != 2.0 {
		t.Fatalf("违约金权重不对: %+v", res.Keywords["违约金"])
	}
	if res.Keywords["营收"].weight != 1.0 || res.Keywords["审计"].weight != 1.0 {
		t.Fatalf("非法权重应回退 1.0: %+v", res.Keywords)
	}
}

func TestRuleLoaderDupKeepsMax(t *testing.T) {
	repo := newLoaderRepo(t)
	ctx := context.Background()
	rows := []model.CategoryKeyword{
		{Type: "category", Category: "报表", Keyword: "报告", Weight: 1.0},
		{Type: "category", Category: "文档", Keyword: "报告", Weight: 1.5},
	}
	if err := repo.UpsertMany(ctx, rows); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	loader := NewRuleLoader(repo)
	kws, err := loader.Load(ctx)
	if err != nil {
		t.Fatalf("加载失败: %v", err)
	}
	if kws["报告"].cat != CategoryDocument || kws["报告"].weight != 1.5 {
		t.Fatalf("同词跨类应保留最大权重: %+v", kws["报告"])
	}
}

func TestRuleLoaderSeedCounts(t *testing.T) {
	repo := newLoaderRepo(t)
	ctx := context.Background()
	if err := repo.UpsertMany(ctx, SeedCategoryKeywords()); err != nil {
		t.Fatalf("种子写入失败: %v", err)
	}
	counts, err := repo.CountByCategory(ctx, "category")
	if err != nil {
		t.Fatalf("计数失败: %v", err)
	}
	want := []string{"合同", "简历", "报表", "图片", "视频", "音频", "文档", "压缩包"}
	for _, c := range want {
		if counts[c] < 25 {
			t.Fatalf("类 %s 应 ≥25 条，得到 %d", c, counts[c])
		}
	}
	if _, hasOther := counts["其他"]; hasOther {
		t.Fatalf("其他类应 0 条符合设计，得到 %d", counts["其他"])
	}
	loader := NewRuleLoader(repo)
	kws, err := loader.Load(ctx)
	if err != nil {
		t.Fatalf("加载失败: %v", err)
	}
	if len(kws) < 200 {
		t.Fatalf("规则应 ≥200 条，得到 %d", len(kws))
	}
}
