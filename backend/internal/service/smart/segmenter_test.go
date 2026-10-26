package smart

import (
	"strings"
	"testing"

	"github.com/yanyiwu/gojieba"
)

// W1-1.6 gojieba CGO 编译验证：能加载词典、能切词、能 Free。
func TestJiebaCut(t *testing.T) {
	x := gojieba.NewJieba()
	defer x.Free()

	sentence := "私有云盘智能分类测试"
	words := x.Cut(sentence, true)
	t.Logf("cut %q => %v", sentence, words)

	if len(words) == 0 {
		t.Fatal("切词结果为空，gojieba 未正常工作")
	}
	joined := strings.Join(words, "/")
	for _, want := range []string{"私有", "云", "智能", "分类", "测试"} {
		// 宽松断言：核心词至少命中一个即算分词器工作
		if strings.Contains(joined, want) {
			t.Logf("命中期望词: %s", want)
			return
		}
	}
	t.Fatalf("未命中任何期望词，切词结果: %v", words)
}
