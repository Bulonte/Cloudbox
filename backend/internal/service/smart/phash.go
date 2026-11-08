package smart

// pHash 图片感知哈希（S6 / 6.6 纯函数，不碰 DB/路由/上传链路）
//
// 流水线：限流 32MB → 魔数校验 → DecodeConfig 验尺寸（25MP 炸弹图门禁）→
// image.Decode → 透明铺白底 → imaging.Grayscale → Lanczos 缩放 32×32 →
// DCT-II 取左上 8×8 → 均值比较（排除直流分量）→ uint64。
//
// 存储复用 S5 的 EncodeSimHash（小写 %016x / CHAR(16)），距离复用
// HammingDistance，阈值 <5 冻结但不在 S6 硬编码判定。非图片 / 解码失败 /
// 炸弹图统一返回 ErrInvalidImage（S8 再包成 10001，S6 不引入 5xxxx）。

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"math"

	"github.com/disintegration/imaging"
	_ "golang.org/x/image/webp"
)

// ErrInvalidImage 非图片 / 解码失败 / 炸弹图统一哨兵错误。
var ErrInvalidImage = errors.New("无效的图片数据")

const (
	phashEdge      = 32              // 归一化边长
	phashBlock     = 8               // 左上低频块边长
	maxImageBytes  = int64(32 << 20) // 32MB 字节闸
	maxImagePixels = int64(25000000) // 25MP 像素闸
	phashFPBits    = 64              // 输出位数
)

// PHashFromReader 从流计算 pHash（生产入口：上传链路 S8 调用）。
func PHashFromReader(r io.Reader) (uint64, error) {
	raw, err := io.ReadAll(io.LimitReader(r, maxImageBytes+1))
	if err != nil || int64(len(raw)) > maxImageBytes {
		return 0, fmt.Errorf("读取图片失败: %w", ErrInvalidImage)
	}
	return PHashFromBytes(raw)
}

// PHashFromBytes 从字节计算 pHash（测试 / 复用入口）。
func PHashFromBytes(raw []byte) (uint64, error) {
	if !isSupportedImage(raw) {
		return 0, fmt.Errorf("不支持的图片格式: %w", ErrInvalidImage)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return 0, fmt.Errorf("图片头解析失败: %w", ErrInvalidImage)
	}
	if !validDimensions(cfg.Width, cfg.Height) {
		return 0, fmt.Errorf("图片尺寸非法: %w", ErrInvalidImage)
	}
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return 0, fmt.Errorf("图片解码失败: %w", ErrInvalidImage)
	}
	return PHashImage(img), nil
}

// PHashImage 已解码图片 → 64 位指纹（退化空图回 0，不 panic）。
func PHashImage(img image.Image) uint64 {
	b := img.Bounds()
	if b.Dx() <= 0 || b.Dy() <= 0 {
		return 0
	}
	small := imaging.Resize(imaging.Grayscale(flatten(img)), phashEdge, phashEdge, imaging.Lanczos)
	luma := lumaMatrix(small)
	dct := dctTopLeft(luma)
	return bitsFromDCT(dct)
}

// flatten 透明图铺白底（GIF 首帧语义由 image.Decode 保证）。
func flatten(img image.Image) image.Image {
	if _, ok := img.(*image.Gray); ok {
		return img
	}
	b := img.Bounds()
	dst := image.NewRGBA(b)
	draw.Draw(dst, b, &image.Uniform{C: color.White}, image.Point{}, draw.Src)
	draw.Draw(dst, b, img, b.Min, draw.Over)
	return dst
}

// lumaMatrix 取 32×32 灰度归一化矩阵（imaging.Grayscale 后 R=G=B）。
func lumaMatrix(small image.Image) [phashEdge][phashEdge]float64 {
	var m [phashEdge][phashEdge]float64
	for y := 0; y < phashEdge; y++ {
		for x := 0; x < phashEdge; x++ {
			r, _, _, _ := small.At(x, y).RGBA()
			m[y][x] = float64(r>>8) / 255.0
		}
	}
	return m
}

// dctTopLeft 32×32 DCT-II，仅算输出所需的左上 8×8 低频块。
func dctTopLeft(m [phashEdge][phashEdge]float64) [phashBlock][phashBlock]float64 {
	var d [phashBlock][phashBlock]float64
	for v := 0; v < phashBlock; v++ {
		cv := 1.0
		if v == 0 {
			cv = 1 / math.Sqrt2
		}
		for u := 0; u < phashBlock; u++ {
			cu := 1.0
			if u == 0 {
				cu = 1 / math.Sqrt2
			}
			var sum float64
			for y := 0; y < phashEdge; y++ {
				cosy := math.Cos(float64(2*y+1) * float64(v) * math.Pi / 64)
				for x := 0; x < phashEdge; x++ {
					sum += m[y][x] * math.Cos(float64(2*x+1)*float64(u)*math.Pi/64) * cosy
				}
			}
			d[v][u] = 0.25 * cu * cv * sum
		}
	}
	return d
}

// bitsFromDCT 均值比较（排除直流 d[0][0]），行主序 bit = y*8+x。
func bitsFromDCT(d [phashBlock][phashBlock]float64) uint64 {
	var sum float64
	for y := 0; y < phashBlock; y++ {
		for x := 0; x < phashBlock; x++ {
			if x == 0 && y == 0 {
				continue
			}
			sum += d[y][x]
		}
	}
	mean := sum / (phashBlock*phashBlock - 1)
	var fp uint64
	for y := 0; y < phashBlock; y++ {
		for x := 0; x < phashBlock; x++ {
			if d[y][x] > mean {
				fp |= 1 << (y*phashBlock + x)
			}
		}
	}
	return fp
}

// validDimensions 像素闸：正尺寸且 int64 相乘防溢出，总像素 ≤25MP。
func validDimensions(w, h int) bool {
	if w <= 0 || h <= 0 {
		return false
	}
	return int64(w)*int64(h) <= maxImagePixels
}

// isSupportedImage 魔数校验：仅 JPEG / PNG / GIF / WebP。
func isSupportedImage(raw []byte) bool {
	switch {
	case len(raw) >= 3 && raw[0] == 0xFF && raw[1] == 0xD8 && raw[2] == 0xFF:
		return true // JPEG
	case len(raw) >= 8 && raw[0] == 0x89 && raw[1] == 0x50 && raw[2] == 0x4E &&
		raw[3] == 0x47 && raw[4] == 0x0D && raw[5] == 0x0A && raw[6] == 0x1A && raw[7] == 0x0A:
		return true // PNG
	case len(raw) >= 6 && raw[0] == 'G' && raw[1] == 'I' && raw[2] == 'F' &&
		raw[3] == '8' && (raw[4] == '7' || raw[4] == '9') && raw[5] == 'a':
		return true // GIF87a / GIF89a
	case len(raw) >= 12 && raw[0] == 'R' && raw[1] == 'I' && raw[2] == 'F' && raw[3] == 'F' &&
		raw[8] == 'W' && raw[9] == 'E' && raw[10] == 'B' && raw[11] == 'P':
		return true // WebP
	default:
		return false
	}
}

// EncodePHash 指纹 → 存储格式（复用 S5 小写 %016x 口径）。
func EncodePHash(h uint64) string {
	return EncodeSimHash(h)
}

// ParsePHash 存储格式 → 指纹（复用 S5 严格小写 16 字符校验）。
func ParsePHash(s string) (uint64, error) {
	return ParseSimHash(s)
}

// PHashDistance 图片汉明距离（复用 S5 bits.OnesCount64，阈值 <5 由调用方判定）。
func PHashDistance(a, b uint64) int {
	return HammingDistance(a, b)
}
