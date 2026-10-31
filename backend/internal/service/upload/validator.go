package upload

import (
	"bytes"
	"errors"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/gabriel-vasile/mimetype"
)

// 上传体积与嗅探常量（PRD §2.1 冻结口径：简单上传 ≤2MB；单文件上限 5GB；分片数上限 10000）
const (
	MaxSimpleUploadSize = 2 * 1024 * 1024
	MaxFileSize         = 5 * 1024 * 1024 * 1024
	MaxChunkCount       = 10000
	// SniffLen 嗅探窗口，与 http.DetectContentType 对齐
	SniffLen = 512
)

// Validate* 哨兵错误：handler 只映射 10001，不回显内部细节
var (
	ErrTooLarge       = errors.New("文件过大")
	ErrUnsupportedExt = errors.New("不支持的文件类型")
	ErrMimeMismatch   = errors.New("文件内容与扩展名不符")
)

// allowedExt 白名单（规范 §5.3.1，一字对齐）
var allowedExt = map[string]bool{
	"pdf": true, "doc": true, "docx": true, "xls": true, "xlsx": true,
	"ppt": true, "pptx": true, "txt": true, "md": true,
	"jpg": true, "jpeg": true, "png": true, "gif": true, "webp": true, "bmp": true,
	"mp4": true, "avi": true, "mov": true, "mkv": true, "webm": true,
	"mp3": true, "wav": true, "flac": true, "aac": true, "m4a": true,
	"zip": true, "rar": true, "7z": true, "tar": true, "gz": true,
}

// deniedExt 黑名单：命中任一直接拒（不看白名单）
var deniedExt = map[string]bool{
	"exe": true, "bat": true, "cmd": true, "sh": true, "dll": true,
	"so": true, "jar": true, "msi": true, "scr": true, "com": true,
	"pif": true, "ps1": true, "psm1": true, "vbs": true, "js": true,
	"jse": true, "ws": true, "wsf": true, "msc": true, "reg": true, "lnk": true,
}

// ValidateSize 体积校验：size<=0 视为非法空文件；limit<=0 回退 MaxFileSize
func ValidateSize(size, limit int64) error {
	if limit <= 0 {
		limit = MaxFileSize
	}
	if size <= 0 || size > limit {
		return ErrTooLarge
	}
	return nil
}

// ValidateExt 扩展名校验：TrimSpace + 拒路径分隔 + 黑名单优先 + 白名单收敛；返回小写 ext
func ValidateExt(fileName string) (string, error) {
	trimmed := strings.TrimSpace(fileName)
	if trimmed == "" || strings.ContainsAny(trimmed, "/\\\x00") {
		return "", ErrUnsupportedExt
	}
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(trimmed), "."))
	if ext == "" {
		return "", ErrUnsupportedExt
	}
	if deniedExt[ext] {
		return "", ErrUnsupportedExt
	}
	if !allowedExt[ext] {
		return "", ErrUnsupportedExt
	}
	return ext, nil
}

// SniffAndCheck 内容嗅探：只信服务端字节头，不信客户端 Content-Type
// ext 须为 ValidateExt 产出；head 为文件前 SniffLen 字节；返回服务端判定的 MIME
func SniffAndCheck(head []byte, ext string) (string, error) {
	ext = strings.ToLower(strings.TrimSpace(ext))
	if ext == "" || deniedExt[ext] || !allowedExt[ext] {
		return "", ErrUnsupportedExt
	}
	if len(head) == 0 {
		return "", ErrMimeMismatch
	}
	detected := mimetype.Detect(head)
	mime := strings.SplitN(detected.String(), ";", 2)[0]
	mime = strings.TrimSpace(strings.ToLower(mime))
	detExt := strings.ToLower(strings.TrimPrefix(detected.Extension(), "."))
	// 可执行/脚本回判直接拒（改名攻击兜底：exe 改 .jpg 在此被拦）
	if deniedExt[detExt] || isExecMime(mime) {
		return "", ErrMimeMismatch
	}
	if !mimeAllowed(ext, mime, detExt) {
		// http.DetectContentType 交叉印证：双判都不可判（octet-stream）即不通过
		fallback := strings.ToLower(http.DetectContentType(head))
		if !mimeAllowed(ext, strings.SplitN(fallback, ";", 2)[0], detExt) {
			return "", ErrMimeMismatch
		}
		mime = strings.SplitN(fallback, ";", 2)[0]
	}
	// 图片加一道 DecodeConfig：unknown format 即造假；截断（EOF）放行，全量校验留给后续
	if isImageExt(ext) {
		if err := checkImageConfig(head); err != nil {
			return "", err
		}
	}
	return mime, nil
}

// isExecMime 可执行 MIME 特征
func isExecMime(mime string) bool {
	switch {
	case strings.Contains(mime, "x-msdownload"),
		strings.Contains(mime, "x-msdos"),
		strings.Contains(mime, "x-elf"),
		strings.Contains(mime, "x-executable"),
		strings.Contains(mime, "x-shellscript"),
		strings.Contains(mime, "x-shar"),
		mime == "application/x-dosexec":
		return true
	}
	return false
}

// isImageExt 需要 DecodeConfig 双判的图片类型（stdlib 可解部分）
func isImageExt(ext string) bool {
	switch ext {
	case "jpg", "jpeg", "png", "gif":
		return true
	}
	return false
}

// checkImageConfig 图片头双判：unknown format 即造假拒；截断 EOF 放行（512 窗口语义）
func checkImageConfig(head []byte) error {
	_, _, err := image.DecodeConfig(bytes.NewReader(head))
	if err == nil {
		return nil
	}
	if strings.Contains(strings.ToLower(err.Error()), "unknown format") {
		return ErrMimeMismatch
	}
	return nil
}

// mimeAllowed 扩展名→可接受 MIME 矩阵（任一命中即过）
func mimeAllowed(ext, mime, detExt string) bool {
	switch ext {
	case "pdf":
		return mime == "application/pdf"
	case "txt", "md":
		return strings.HasPrefix(mime, "text/")
	case "jpg", "jpeg":
		return mime == "image/jpeg"
	case "png":
		return mime == "image/png"
	case "gif":
		return mime == "image/gif"
	case "webp":
		return mime == "image/webp"
	case "bmp":
		return mime == "image/bmp" || mime == "image/x-ms-bmp"
	case "mp4":
		return mime == "video/mp4" || mime == "application/mp4"
	case "avi":
		return mime == "video/x-msvideo" || mime == "video/avi"
	case "mov":
		return mime == "video/quicktime"
	case "mkv":
		return mime == "video/x-matroska"
	case "webm":
		return mime == "video/webm"
	case "mp3":
		return mime == "audio/mpeg" || mime == "audio/mp3"
	case "wav":
		return mime == "audio/wav" || mime == "audio/x-wav" || mime == "audio/vnd.wave"
	case "flac":
		return mime == "audio/flac" || mime == "audio/x-flac"
	case "aac":
		return mime == "audio/aac" || mime == "audio/x-aac"
	case "m4a":
		return mime == "audio/mp4" || mime == "audio/x-m4a" || mime == "audio/aac"
	case "zip":
		return mime == "application/zip"
	case "rar":
		return strings.Contains(mime, "rar")
	case "7z":
		return strings.Contains(mime, "7z") || strings.Contains(mime, "7-zip")
	case "tar":
		return strings.Contains(mime, "tar")
	case "gz":
		return mime == "application/gzip" || mime == "application/x-gzip"
	case "doc", "xls", "ppt", "docx", "xlsx", "pptx":
		// OLE/OOXML 容器误报多，放宽到 application/*（可执行已在上游拒掉）
		return strings.HasPrefix(mime, "application/") || strings.HasPrefix(mime, "text/")
	}
	return false
}
