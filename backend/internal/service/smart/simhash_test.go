package smart

import (
	"strings"
	"testing"
)

// fakeSimSegmenter 内存替身：按空白切分，隔离 CGO（segmenter_test.go 另有同名实现，按需复用语义）
type fakeSimSegmenter struct{}

func (fakeSimSegmenter) Segment(text string) []string {
	if strings.TrimSpace(text) == "" {
		return []string{}
	}
	return strings.Fields(text)
}

func withFakeSim(t *testing.T) func() {
	t.Helper()
	old := current
	SetSegmenter(fakeSimSegmenter{})
	return func() { SetSegmenter(old) }
}

func TestSimHashEmpty(t *testing.T) {
	restore := withFakeSim(t)
	defer restore()
	for _, in := range []string{"", "   ", "\t\n "} {
		if got := SimHash(in); got != 0 {
			t.Fatalf("空输入应回 0，得到 %x", got)
		}
		if got := SimHashTokens(nil); got != 0 {
			t.Fatalf("nil tokens 应回 0，得到 %x", got)
		}
	}
}

func TestSimHashDeterministicAndRoundTrip(t *testing.T) {
	restore := withFakeSim(t)
	defer restore()
	a := SimHash("hello world")
	b := SimHash("hello world")
	if a == 0 || a != b {
		t.Fatalf("同一文本应确定非零且相等：%x vs %x", a, b)
	}
	enc := EncodeSimHash(a)
	if len(enc) != 16 || strings.ToLower(enc) != enc {
		t.Fatalf("Encode 应 16 字符小写，得到 %q", enc)
	}
	back, err := ParseSimHash(enc)
	if err != nil || back != a {
		t.Fatalf("往返失败：%q err=%v back=%x want=%x", enc, err, back, a)
	}
}

func TestSimHashKnownVector(t *testing.T) {
	restore := withFakeSim(t)
	defer restore()
	// fake 按空白切分 "hello world" → ["hello","world"]，锁死一次 FNV+加权结果，逻辑被改即炸。
	got := SimHash("hello world")
	const want uint64 = 0x410d84600088803
	if got != want {
		t.Fatalf("已知向量漂移：got %016x want %016x", got, want)
	}
}

func TestSimHashNearDuplicate(t *testing.T) {
	restore := withFakeSim(t)
	defer restore()
	// 短文本改 1 词会翻转大量比特（token 越少单 token 权重越大），
	// 近似用例必须用足够长的文本（此处 20 词改 1 词），否则是算法误用而非实现 bug。
	base := "cloudbox private cloud storage sync backup share document " +
		"contract finance report the quick brown fox jumps over the lazy dog"
	near := "cloudbox private cloud storage sync backup share document " +
		"contract finance report the quick brown fox jumps over the lazy cat"
	far := "contract breach liability compensation agreement clause party payment delivery warranty signature stamp"
	dNear := HammingDistance(SimHash(base), SimHash(near))
	if dNear >= 5 {
		t.Fatalf("近似文本距离应 <5，得到 %d", dNear)
	}
	dFar := HammingDistance(SimHash(base), SimHash(far))
	if dFar <= 10 {
		t.Fatalf("完全不同文本距离应显著大（>10），得到 %d", dFar)
	}
}

func TestSimHashChineseRealSegmenter(t *testing.T) {
	// 不依赖全局 Init/once 状态（跨文件测试可能污染 current）：
	// 显式新建真实分词器，用完即 Free，全程隔离。
	seg := NewJiebaSegmenter()
	defer seg.Free()
	text := "本劳动合同约定甲方乙方违约金条款合同期限三年工作地点北京薪资按月支付"
	reordered := "薪资按月支付工作地点北京合同期限三年本劳动合同约定甲方乙方违约金条款"
	fp := SimHashWithSegmenter(text, seg)
	if fp == 0 {
		t.Fatal("真实分词下合同句指纹不应为 0")
	}
	// SimHash 是词袋模型：调序不改变 TF 指纹必相同（距离 0）；
	// 单 token 替换在短文本下翻转可达 10（已实测），属算法固有敏感性，不在此断言。
	if d := HammingDistance(fp, SimHashWithSegmenter(reordered, seg)); d != 0 {
		t.Fatalf("调序指纹应相同（距离 0），得到 %d", d)
	}
}

func TestSimHashLongText(t *testing.T) {
	restore := withFakeSim(t)
	defer restore()
	big := strings.Repeat("合同 甲方 乙方 违约金 简历 求职 报告 财务 ", 8192)
	if got := SimHash(big); got == 0 {
		t.Fatal("超长文本指纹不应为 0")
	}
	// 必须经 SimHashTokens 覆盖已分词路径
	toks := strings.Fields(big)
	if got := SimHashTokens(toks); got == 0 {
		t.Fatal("SimHashTokens 超长不应为 0")
	}
}

func TestSimHashHexSpec(t *testing.T) {
	if got := EncodeSimHash(0); got != "0000000000000000" {
		t.Fatalf("零值应全零补齐 16 位，得到 %q", got)
	}
	if got := EncodeSimHash(0xabcdef); got != "0000000000abcdef" {
		t.Fatalf("前导零保留失败，得到 %q", got)
	}
	for _, bad := range []string{"", "ABCDEF1234567890", "abc", "000000000000000g", "00000000000000000"} {
		if _, err := ParseSimHash(bad); err == nil {
			t.Fatalf("非法指纹 %q 应判错", bad)
		}
	}
	if _, err := ParseSimHash("0000000000abcdef"); err != nil {
		t.Fatalf("合法小写应通过：%v", err)
	}
}

func TestHammingDistance(t *testing.T) {
	if HammingDistance(0, 0) != 0 {
		t.Fatal("0 vs 0 应为 0")
	}
	if HammingDistance(0, ^uint64(0)) != 64 {
		t.Fatal("0 vs MaxUint64 应为 64")
	}
	if HammingDistance(0, 1) != 1 {
		t.Fatal("单比特翻转应为 1")
	}
}

func TestSimHashWithSegmenter(t *testing.T) {
	restore := withFakeSim(t)
	defer restore()
	a := SimHashWithSegmenter("hello world", nil)
	b := SimHash("hello world")
	if a != b {
		t.Fatalf("nil 分词器应回落包级 Segment：%x vs %x", a, b)
	}
	c := SimHashWithSegmenter("hello world", fakeSimSegmenter{})
	if c != b {
		t.Fatalf("显式 fake 应与包级一致：%x vs %x", c, b)
	}
}

// BenchmarkSimHashTokens_1MB 纯哈希性能门禁（剥离 CGO）：必须 <100ms
func BenchmarkSimHashTokens_1MB(b *testing.B) {
	chunk := strings.Fields(strings.Repeat("合同 甲方 乙方 违约金 简历 求职 报告 财务 ", 4096))
	toks := make([]string, 0, 1024*256)
	for len(toks) < 1024*256 {
		toks = append(toks, chunk...)
	}
	toks = toks[:1024*256]
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if SimHashTokens(toks) == 0 {
			b.Fatal("基准指纹不应为 0")
		}
	}
}

// BenchmarkSimHash_1MB_Real 端到端观测项（含 gojieba）：只记录耗时，不设硬门禁
func BenchmarkSimHash_1MB_Real(b *testing.B) {
	Init()
	text := strings.Repeat("本劳动合同约定甲方乙方违约金条款，财务报表营收增长。", 4096)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = SimHash(text)
	}
}
