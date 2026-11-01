package smart

import (
	"context"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"cloudbox/internal/model"
	"cloudbox/internal/repository"
)

// TestClassifierFromDB Loader 产出表驱动分类：合同/简历/营收仍判对；扩展名直判不受表影响
func TestClassifierFromDB(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	if err != nil {
		t.Fatalf("open sqlite memory: %v", err)
	}
	if err := db.AutoMigrate(&model.CategoryKeyword{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	repo := repository.NewCategoryKeywordRepository(db)
	ctx := context.Background()
	if err := repo.UpsertMany(ctx, SeedCategoryKeywords()); err != nil {
		t.Fatalf("种子写入失败: %v", err)
	}
	kws, err := NewRuleLoader(repo).Load(ctx)
	if err != nil {
		t.Fatalf("加载失败: %v", err)
	}
	c := NewRuleClassifierWithKeywords(kws)

	if got := c.Classify("未命名.bin", "本劳动合同约定甲方乙方违约金条款"); got.Category != CategoryContract {
		t.Fatalf("合同关键词应判合同，got %v", got.Category)
	}
	if got := c.Classify("未命名.bin", "求职简历包含工作经历与教育背景"); got.Category != CategoryResume {
		t.Fatalf("简历关键词应判简历，got %v", got.Category)
	}
	if got := c.Classify("未命名.bin", "本季度营收同比增长审计通过"); got.Category != CategoryReport {
		t.Fatalf("报表关键词应判报表，got %v", got.Category)
	}

	// 扩展名直判不受表影响
	if got := c.Classify("a.jpg", "无关文本"); got.Category != CategoryImage || got.Confidence != 0.95 {
		t.Fatalf("扩展名直判应图片 0.95，got %+v", got)
	}
	if got := c.Classify("a.xlsx", "无关文本"); got.Category != CategoryReport {
		t.Fatalf("扩展名直判应报表，got %v", got.Category)
	}
	// 空输入仍兜底其他 0.50
	if got := c.Classify("", ""); got.Category != CategoryOther || got.Confidence != 0.50 || !got.NeedsReview {
		t.Fatalf("空输入应兜底其他 0.50 待确认，got %+v", got)
	}
}

// TestNewRuleClassifierWithKeywordsEmpty 空表回退不断言炸，行为确定
func TestNewRuleClassifierWithKeywordsEmpty(t *testing.T) {
	c := NewRuleClassifierWithKeywords(nil)
	if c == nil || len(c.kws) == 0 {
		t.Fatal("空入参应回退最小表，不断言炸")
	}
}
