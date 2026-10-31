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

// fakeSegmenter 内存替身：按空白切分，验证上游可单测不依赖 CGO
type fakeSegmenter struct{}

func (fakeSegmenter) Segment(text string) []string {
	if strings.TrimSpace(text) == "" {
		return []string{}
	}
	return strings.Fields(text)
}

func TestSegmentEmpty(t *testing.T) {
	Init()
	for _, in := range []string{"", "   ", "\t\n"} {
		if got := Segment(in); len(got) != 0 {
			t.Fatalf("空输入应返回空切片, 得到 %v", got)
		}
	}
}

func TestSegmentDeterministic(t *testing.T) {
	Init()
	s := "私有云盘智能分类测试"
	a := Segment(s)
	b := Segment(s)
	if len(a) == 0 {
		t.Fatal("切词结果为空")
	}
	if strings.Join(a, "/") != strings.Join(b, "/") {
		t.Fatalf("同一句子两次结果不一致: %v vs %v", a, b)
	}
}

func TestSegmentConcurrent(t *testing.T) {
	Init()
	done := make(chan []string, 50)
	for i := 0; i < 50; i++ {
		go func() {
			done <- Segment("私有云盘智能分类测试")
		}()
	}
	first := <-done
	for i := 1; i < 50; i++ {
		got := <-done
		if len(got) == 0 {
			t.Fatal("并发切词结果为空")
		}
		if strings.Join(got, "/") != strings.Join(first, "/") {
			t.Fatalf("并发结果不一致: %v vs %v", got, first)
		}
	}
}

func TestSetSegmenter(t *testing.T) {
	Init()
	SetSegmenter(fakeSegmenter{})
	if got := Segment("hello world"); len(got) != 2 || got[0] != "hello" {
		t.Fatalf("注入 fake 后应走 fake, 得到 %v", got)
	}
}
