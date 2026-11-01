package smart

import (
	"path/filepath"
	"strings"
)

// Category 智能分类九大类（规范 §2.5.3 固定，顺序即枚举序）
type Category int

const (
	CategoryContract Category = iota // 合同
	CategoryResume                   // 简历
	CategoryReport                   // 报表
	CategoryImage                    // 图片
	CategoryVideo                    // 视频
	CategoryAudio                    // 音频
	CategoryDocument                 // 文档
	CategoryArchive                  // 压缩包
	CategoryOther                    // 其他
)

// ThresholdReview 置信度低于该值前端标"待确认"（冻结 0.7）
const ThresholdReview = 0.7

// categoryNames 与枚举一一对应，String 直接给前端展示
var categoryNames = []string{"合同", "简历", "报表", "图片", "视频", "音频", "文档", "压缩包", "其他"}

// String 中文名；越界返回"其他"
func (c Category) String() string {
	if c < CategoryContract || c > CategoryOther {
		return "其他"
	}
	return categoryNames[int(c)]
}

// IsValid 枚举有效性
func (c Category) IsValid() bool {
	return c >= CategoryContract && c <= CategoryOther
}

// ParseCategory 中文名反查；失败返回 false（调用方兜底其他）
func ParseCategory(s string) (Category, bool) {
	for i, n := range categoryNames {
		if n == s {
			return Category(i), true
		}
	}
	return CategoryOther, false
}

// Result 单次分类结果：Confidence 恒钳制 [0,1]
type Result struct {
	Category    Category
	Confidence  float64
	NeedsReview bool
}

// NewResult 构造并钳制置信度；NeedsReview = Confidence < 0.7（0.70 本身不待确认）
func NewResult(cat Category, confidence float64) Result {
	if !cat.IsValid() {
		cat = CategoryOther
	}
	if confidence < 0 {
		confidence = 0
	}
	if confidence > 1 {
		confidence = 1
	}
	return Result{
		Category:    cat,
		Confidence:  confidence,
		NeedsReview: confidence < ThresholdReview,
	}
}

// Classifier 分类器抽象：W6 融合版只换实现不换调用方
type Classifier interface {
	Classify(filename string, text string) Result
}

// keywordWeight 关键词权重（生产唯一来源：category_keyword 表经 RuleLoader 加载）
type keywordWeight struct {
	cat    Category
	weight float64
}

// testFallbackKeywords 单测最小回退（仅测试用，生产必须经 RuleLoader 从表加载）
var testFallbackKeywords = map[string]keywordWeight{
	"合同": {CategoryContract, 1.0}, "甲方": {CategoryContract, 1.0}, "乙方": {CategoryContract, 1.0}, "违约金": {CategoryContract, 2.0},
	"简历": {CategoryResume, 1.0}, "求职": {CategoryResume, 1.0}, "工作经历": {CategoryResume, 1.5}, "教育背景": {CategoryResume, 1.5},
	"报表": {CategoryReport, 1.0}, "财务": {CategoryReport, 1.0}, "营收": {CategoryReport, 1.0},
}

// extCategory 扩展名直判：媒体四类 + 文档/报表（合同/简历扩展名与文档重叠，不做伪智能）
// confidence 统一 0.95 不待确认
var extCategory = map[string]Category{
	"jpg": CategoryImage, "jpeg": CategoryImage, "png": CategoryImage, "gif": CategoryImage,
	"webp": CategoryImage, "bmp": CategoryImage, "tif": CategoryImage, "tiff": CategoryImage,
	"mp4": CategoryVideo, "avi": CategoryVideo, "mov": CategoryVideo, "mkv": CategoryVideo,
	"mp3": CategoryAudio, "wav": CategoryAudio, "flac": CategoryAudio, "aac": CategoryAudio,
	"zip": CategoryArchive, "rar": CategoryArchive, "7z": CategoryArchive, "tar": CategoryArchive, "gz": CategoryArchive,
	"pdf": CategoryDocument, "doc": CategoryDocument, "docx": CategoryDocument,
	"txt": CategoryDocument, "md": CategoryDocument,
	"xls": CategoryReport, "xlsx": CategoryReport, "csv": CategoryReport,
}

// RuleClassifier 规则骨架：扩展名优先 + 关键词加权 + 其他兜底
type RuleClassifier struct {
	ext map[string]Category
	kws map[string]keywordWeight
}

// NewRuleClassifier 单测默认构造（最小回退表）；生产必须经 RuleLoader 从表加载后调 NewRuleClassifierWithKeywords
func NewRuleClassifier() *RuleClassifier {
	return &RuleClassifier{ext: extCategory, kws: testFallbackKeywords}
}

// NewRuleClassifierWithKeywords 生产入口：关键词表来自 RuleLoader.Load（category_keyword 表）
func NewRuleClassifierWithKeywords(kws map[string]keywordWeight) *RuleClassifier {
	if len(kws) == 0 {
		kws = testFallbackKeywords
	}
	return &RuleClassifier{ext: extCategory, kws: kws}
}

// Classify 三段式：扩展名直判 → 关键词加权 → 其他兜底
func (r *RuleClassifier) Classify(filename string, text string) Result {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(filename), "."))
	if cat, ok := r.ext[ext]; ok {
		return NewResult(cat, 0.95)
	}
	combined := filename + " " + text
	if strings.TrimSpace(combined) == "" {
		return NewResult(CategoryOther, 0.50)
	}
	// 经 Segment 切词（依赖 S7 单例），命中再按子串复核防"劳动合同/合同"类粘连漏判
	tokens := Segment(combined)
	set := make(map[string]struct{}, len(tokens))
	for _, t := range tokens {
		set[t] = struct{}{}
	}
	scores := make(map[Category]float64)
	for kw, kwc := range r.kws {
		if _, ok := set[kw]; ok {
			scores[kwc.cat] += kwc.weight
			continue
		}
		if strings.Contains(combined, kw) {
			scores[kwc.cat] += kwc.weight
		}
	}
	if len(scores) == 0 {
		return NewResult(CategoryOther, 0.50)
	}
	var best Category
	var bestScore float64
	for cat, s := range scores {
		if s > bestScore || (s == bestScore && cat < best) {
			best, bestScore = cat, s
		}
	}
	// 归一化到 [0.55,0.90]：1 命中≈0.725，随命中增多渐近 0.90
	conf := 0.55 + 0.35*(1-1/(1+bestScore))
	return NewResult(best, conf)
}
