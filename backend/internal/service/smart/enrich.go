package smart

// S8 分类接入上传共享 helper（纯函数，无 DB，不碰 image.Decode/分词器单例之外的 IO）
//
// 放 smart 包而非 service 顶层：service/chunk.go 已 import upload 包，
// helper 若放 service 会让 upload 反向依赖成环；smart 只依赖 repository，
// upload→smart、service→smart 均无环。

import (
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
)

// imagePhashExt pHash 可解集合（与 phash.go 魔数校验 JPEG/PNG/GIF/WebP 对齐；
// classifier 的 bmp/tif 不在此列，走 simhash，解码失败 fail-open 跳过）。
var imagePhashExt = map[string]bool{
	"jpg": true, "jpeg": true, "png": true, "gif": true, "webp": true,
}

// PickAlgorithm 按扩展名/ MIME 选指纹算法：图片 ext → phash，其余 → simhash
func PickAlgorithm(fileName, mime string) string {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(strings.TrimSpace(fileName)), "."))
	if imagePhashExt[ext] {
		return "phash"
	}
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(mime)), "image/") {
		return "phash"
	}
	return "simhash"
}

// textSnippetLen 非文本二进制截断长度（4KB 可打印前缀 + 文件名，足够关键词/ SimHash 信号）
const textSnippetLen = 4096

// TextSnippet 取分类/指纹用文本片段：
// text/* 或 txt/md/csv 直接转 string；其余截前 4KB 取可打印 runes；
// 空输入回 ""（Classifier 自会兜底 其他/0.50）。
func TextSnippet(fileName string, head []byte, mime string) string {
	if len(head) == 0 {
		return ""
	}
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(strings.TrimSpace(fileName)), "."))
	lowerMime := strings.ToLower(strings.TrimSpace(mime))
	if strings.HasPrefix(lowerMime, "text/") || ext == "txt" || ext == "md" || ext == "csv" {
		return string(head)
	}
	n := len(head)
	if n > textSnippetLen {
		n = textSnippetLen
	}
	var b strings.Builder
	b.Grow(n)
	for len(head) > 0 {
		r, size := utf8.DecodeRune(head)
		head = head[size:]
		if r == utf8.RuneError {
			continue
		}
		if unicode.IsPrint(r) || r == '\n' || r == '\t' {
			b.WriteRune(r)
		}
		if b.Len() >= textSnippetLen {
			break
		}
	}
	return b.String()
}

// ClassifyWithFallback 统一兜底：classifier==nil / panic 恢复 → 其他/0.50
func ClassifyWithFallback(c Classifier, fileName, text string) (r Result) {
	defer func() {
		if recover() != nil {
			r = NewResult(CategoryOther, 0.50)
		}
	}()
	if c == nil {
		return NewResult(CategoryOther, 0.50)
	}
	return c.Classify(fileName, text)
}
