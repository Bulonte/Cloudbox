package smart

import (
	"math"
	"strings"
	"testing"
)

func TestCategoryContract(t *testing.T) {
	want := []string{"合同", "简历", "报表", "图片", "视频", "音频", "文档", "压缩包", "其他"}
	for i, w := range want {
		c := Category(i)
		if !c.IsValid() {
			t.Fatalf("Category(%d) 应有效", i)
		}
		if c.String() != w {
			t.Fatalf("Category(%d).String()=%q want %q", i, c.String(), w)
		}
		got, ok := ParseCategory(w)
		if !ok || got != c {
			t.Fatalf("ParseCategory(%q)=(%v,%v) want (%v,true)", w, got, ok, c)
		}
	}
	if Category(-1).String() != "其他" || Category(99).String() != "其他" {
		t.Fatal("越界 Category.String 应兜底其他")
	}
	if Category(-1).IsValid() || Category(9).IsValid() {
		t.Fatal("越界 Category.IsValid 应为 false")
	}
	if _, ok := ParseCategory("不存在的类"); ok {
		t.Fatal("ParseCategory 未知类应返回 false")
	}
	if ThresholdReview != 0.7 {
		t.Fatalf("ThresholdReview=%v 应冻结 0.7", ThresholdReview)
	}
}

func TestNewResultClampAndReview(t *testing.T) {
	cases := []struct {
		name       string
		cat        Category
		conf       float64
		wantCat    Category
		wantConf   float64
		wantReview bool
	}{
		{"负数钳制", CategoryContract, -0.5, CategoryContract, 0, true},
		{"超上钳制", CategoryImage, 1.5, CategoryImage, 1, false},
		{"0.69待确认", CategoryResume, 0.69, CategoryResume, 0.69, true},
		{"0.70边界不待确认", CategoryResume, 0.70, CategoryResume, 0.70, false},
		{"0.71不待确认", CategoryResume, 0.71, CategoryResume, 0.71, false},
		{"无效类兜底", Category(99), 0.9, CategoryOther, 0.9, false},
	}
	for _, tc := range cases {
		r := NewResult(tc.cat, tc.conf)
		if r.Category != tc.wantCat || math.Abs(r.Confidence-tc.wantConf) > 1e-9 || r.NeedsReview != tc.wantReview {
			t.Fatalf("%s: got %+v", tc.name, r)
		}
	}
}

func TestRuleClassifier(t *testing.T) {
	c := NewRuleClassifier()

	// 1. 扩展名直判：媒体四类 + 文档/报表
	extCases := map[string]Category{
		"a.jpg": CategoryImage, "a.PNG": CategoryImage,
		"a.mp4": CategoryVideo, "a.MKV": CategoryVideo,
		"a.mp3": CategoryAudio, "a.flac": CategoryAudio,
		"a.zip": CategoryArchive, "a.RAR": CategoryArchive,
		"a.pdf": CategoryDocument, "a.docx": CategoryDocument,
		"a.xlsx": CategoryReport, "a.csv": CategoryReport,
	}
	for fn, want := range extCases {
		r := c.Classify(fn, "无关文本")
		if r.Category != want {
			t.Fatalf("Classify(%q)=%v want %v", fn, r.Category, want)
		}
		if r.Confidence != 0.95 || r.NeedsReview {
			t.Fatalf("Classify(%q) 扩展名直判应 0.95 且不待确认，got %+v", fn, r)
		}
	}

	// 2. 合同/简历扩展名与文档重叠 → 不伪智能，用关键词
	r := c.Classify("劳动合同.docx", "无关文本")
	if r.Category != CategoryDocument {
		t.Fatalf("扩展名重叠应听扩展名的，got %v", r.Category)
	}

	// 3. 关键词加权：文件名兜不住时文本顶上
	r = c.Classify("未命名.bin", "本劳动合同约定甲方乙方违约金条款")
	if r.Category != CategoryContract {
		t.Fatalf("合同关键词应判合同，got %v text tokens=%v", r.Category, Segment("本劳动合同约定甲方乙方违约金条款"))
	}
	if r.Confidence < 0.55 || r.Confidence > 0.90 {
		t.Fatalf("关键词置信度应在 [0.55,0.90]，got %v", r.Confidence)
	}

	r = c.Classify("未命名.bin", "求职简历包含工作经历与教育背景")
	if r.Category != CategoryResume {
		t.Fatalf("简历关键词应判简历，got %v", r.Category)
	}

	// 4. 空输入兜底其他 0.50 待确认
	r = c.Classify("", "")
	if r.Category != CategoryOther || r.Confidence != 0.50 || !r.NeedsReview {
		t.Fatalf("空输入应兜底其他 0.50 待确认，got %+v", r)
	}

	// 5. 无命中兜底其他
	r = c.Classify("未命名.bin", "xyz123 !@#")
	if r.Category != CategoryOther {
		t.Fatalf("无命中应兜底其他，got %v (tokens=%q)", r.Category, strings.Join(Segment("xyz123 !@#"), ","))
	}
	if r.Confidence < 0 || r.Confidence > 1 {
		t.Fatalf("置信度必须钳制 [0,1]，got %v", r.Confidence)
	}
}
