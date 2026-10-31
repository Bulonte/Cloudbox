package smart

import (
	"strings"
	"sync"

	"github.com/yanyiwu/gojieba"
)

// Segmenter 中文分词抽象：SimHash / 搜索只依赖它，不依赖具体库（ADR C10 可互换 gse）
type Segmenter interface {
	Segment(text string) []string
}

// JiebaSegmenter gojieba 实现：Cut 非线程安全假设，全程互斥
type JiebaSegmenter struct {
	mu sync.Mutex
	x  *gojieba.Jieba
}

// NewJiebaSegmenter 新建（调用方负责 Free；生产请走包级 Init/Close 单例）
func NewJiebaSegmenter() *JiebaSegmenter {
	return &JiebaSegmenter{x: gojieba.NewJieba()}
}

// Free 释放 C++ 侧词典；重复 Free 会 double-free，调用方保证只调一次
func (j *JiebaSegmenter) Free() {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.x != nil {
		j.x.Free()
		j.x = nil
	}
}

// Segment 切词：HMM 开启；空输入返回空切片；去空白 token
func (j *JiebaSegmenter) Segment(text string) []string {
	if strings.TrimSpace(text) == "" {
		return []string{}
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.x == nil {
		return []string{}
	}
	words := j.x.Cut(text, true)
	out := make([]string, 0, len(words))
	for _, w := range words {
		if strings.TrimSpace(w) == "" {
			continue
		}
		out = append(out, w)
	}
	return out
}

var (
	once    sync.Once
	current Segmenter
	jiebaX  *JiebaSegmenter
)

// Init 全进程加载一次词典；W6 接线后由 main 启动时调用
func Init() {
	once.Do(func() {
		jiebaX = NewJiebaSegmenter()
		current = Segmenter(jiebaX)
	})
}

// Close 进程退出时释放词典；只调一次
func Close() {
	if jiebaX != nil {
		jiebaX.Free()
		jiebaX = nil
	}
}

// SetSegmenter 仅测试注入 fake（gse 降级/单测隔离用）
func SetSegmenter(s Segmenter) {
	current = s
}

// Segment 包级快捷方式：业务方只调这个；忘调 Init 时懒兜底
func Segment(text string) []string {
	Init()
	return current.Segment(text)
}
