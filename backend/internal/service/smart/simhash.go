package smart

import (
	"fmt"
	"hash/fnv"
	"math"
	"math/bits"
	"strconv"
	"strings"
	"unicode/utf8"
)

// SimHash 文本指纹（S5 / 6.5 纯函数，不碰 DB/路由/上传链路）
//
// 算法：Segment 切词 → TF 统计 → FNV-1a64 词哈希 → TF×长度增强加权累加 64 维向量 → 收敛。
// 权重 w(t) = tf(t) * (1 + log(1 + runeLen(t)))，IDF 恒为 1（有意简化：S5 无全库语料源，
// 强上 IDF 会引入 DB 依赖破坏纯函数约束；IDF 留待后续优化再议，需 ADR，不在本轮）。
// 返回 0 定义为“无指纹”（空/全空白输入），S7 遇到 0 跳过不报错。

// SimHash 文本 → 64 位指纹（生产入口，走包级 Segment 全局单例）
func SimHash(text string) uint64 {
	return SimHashTokens(Segment(text))
}

// SimHashWithSegmenter 文本 → 64 位指纹（可注入分词器；seg==nil 时回落包级 Segment）
// 单测用 fake 隔离 CGO，中文真测显式传 NewJiebaSegmenter()，不污染全局。
func SimHashWithSegmenter(text string, seg Segmenter) uint64 {
	if seg == nil {
		return SimHash(text)
	}
	return SimHashTokens(seg.Segment(text))
}

// SimHashTokens 已·分词路径 → 64 位指纹（基准测试剥离 CGO 用；空/全空白回 0）
func SimHashTokens(tokens []string) uint64 {
	tf, rlen := countTokens(tokens)
	if len(tf) == 0 {
		return 0
	}
	var vec [64]float64
	accumulate(tf, rlen, &vec)
	return converge(vec)
}

// countTokens 统计词频 + 缓存词 rune 长度（同一 token 只算一次长度，空白跳过）
func countTokens(tokens []string) (map[string]int, map[string]int) {
	tf := make(map[string]int, len(tokens)/4+1)
	rlen := make(map[string]int, len(tokens)/4+1)
	for _, tok := range tokens {
		if strings.TrimSpace(tok) == "" {
			continue
		}
		tf[tok]++
		if _, ok := rlen[tok]; !ok {
			rlen[tok] = utf8.RuneCountInString(tok)
		}
	}
	return tf, rlen
}

// accumulate 加权累加 64 维向量：bit=1 加 w，否则减 w（向量加法可交换，map 迭代随机不影响结果）
func accumulate(tf map[string]int, rlen map[string]int, vec *[64]float64) {
	for tok, c := range tf {
		h := fnv64(tok)
		w := float64(c) * (1 + math.Log1p(float64(rlen[tok])))
		for i := 0; i < 64; i++ {
			if h>>i&1 == 1 {
				vec[i] += w
			} else {
				vec[i] -= w
			}
		}
	}
}

// converge 向量收敛：分量 >0 置 1，否则置 0（==0 时置 0，保证空向量回 0 语义一致）
func converge(vec [64]float64) uint64 {
	var fp uint64
	for i := 0; i < 64; i++ {
		if vec[i] > 0 {
			fp |= 1 << i
		}
	}
	return fp
}

// fnv64 词哈希：FNV-1a64，标准库零依赖跨平台确定
func fnv64(s string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(s))
	return h.Sum64()
}

// EncodeSimHash 指纹 → 存储/展示格式：统一小写 16 字符，前导补零（对应 CHAR(16)）
func EncodeSimHash(h uint64) string {
	return fmt.Sprintf("%016x", h)
}

// ParseSimHash 存储格式 → 指纹：只接受小写 16 字符（大写/非法长度/非法字符判错）
func ParseSimHash(s string) (uint64, error) {
	if len(s) != 16 {
		return 0, fmt.Errorf("指纹格式非法")
	}
	for _, c := range s {
		isDigit := c >= '0' && c <= '9'
		isLower := c >= 'a' && c <= 'f'
		if !isDigit && !isLower {
			return 0, fmt.Errorf("指纹格式非法")
		}
	}
	n, err := strconv.ParseUint(s, 16, 64)
	if err != nil {
		return 0, fmt.Errorf("指纹格式非法")
	}
	return n, nil
}

// HammingDistance 汉明距离：S7 复用；阈值 <5 冻结但不在 S5 硬编码阈值判定
func HammingDistance(a, b uint64) int {
	return bits.OnesCount64(a ^ b)
}
