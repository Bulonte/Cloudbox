package smart

import (
	"context"
	"errors"
	"math"
	"strings"

	"cloudbox/internal/repository"
)

// ErrEmptyRules 规则表为空：调用方决定降级，Loader 内不兜底硬编码
var ErrEmptyRules = errors.New("分类关键词规则为空")

// RuleLoader 分类规则加载器：category_keyword(type='category') → keywordWeight 表
type RuleLoader struct {
	repo *repository.CategoryKeywordRepository
}

// NewRuleLoader 构造注入
func NewRuleLoader(repo *repository.CategoryKeywordRepository) *RuleLoader {
	return &RuleLoader{repo: repo}
}

// LoadResult 加载结果：规则表 + 跳过统计（脏行可观测，不打死全表）
type LoadResult struct {
	Keywords map[string]keywordWeight
	Skipped  int
	Total    int
}

// Load 只取 type='category'；非法类名整行跳过；同词跨类保留最大权重；非法权重回退 1.0
func (l *RuleLoader) Load(ctx context.Context) (map[string]keywordWeight, error) {
	res, err := l.LoadWithStats(ctx)
	if err != nil {
		return nil, err
	}
	return res.Keywords, nil
}

// LoadWithStats 带跳过计数版本（测试与运维观测用）
func (l *RuleLoader) LoadWithStats(ctx context.Context) (*LoadResult, error) {
	rows, err := l.repo.ListByType(ctx, "category")
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, ErrEmptyRules
	}
	out := make(map[string]keywordWeight, len(rows))
	skipped := 0
	for _, r := range rows {
		cat, ok := ParseCategory(strings.TrimSpace(r.Category))
		if !ok {
			skipped++
			continue
		}
		kw := strings.TrimSpace(r.Keyword)
		if kw == "" {
			skipped++
			continue
		}
		w := r.Weight
		if math.IsNaN(w) || math.IsInf(w, 0) || w <= 0 {
			w = 1.0
		}
		if cur, dup := out[kw]; dup {
			// 同词跨类保留最大权重，保证行为确定
			if w > cur.weight {
				out[kw] = keywordWeight{cat: cat, weight: w}
			}
			continue
		}
		out[kw] = keywordWeight{cat: cat, weight: w}
	}
	if len(out) == 0 {
		return nil, ErrEmptyRules
	}
	return &LoadResult{Keywords: out, Skipped: skipped, Total: len(rows)}, nil
}
