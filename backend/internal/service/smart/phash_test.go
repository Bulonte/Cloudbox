package smart

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"testing"
)

// solidImage 纯色图（程序化生成，不落地夹具）
func solidImage(w, h int, c color.Color) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	return img
}

// gradientImage 水平梯度图（黑→白），pHash 缩放不变性的标准探针
func gradientImage(w, h int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			v := uint8(float64(x) / float64(w-1) * 255)
			img.Set(x, y, color.RGBA{R: v, G: v, B: v, A: 255})
		}
	}
	return img
}

// checkerImage 棋盘格（高频探针，与梯度应显著不同）
func checkerImage(w, h, cell int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if (x/cell+y/cell)%2 == 0 {
				img.Set(x, y, color.White)
			} else {
				img.Set(x, y, color.Black)
			}
		}
	}
	return img
}

func encodePNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("PNG 编码失败：%v", err)
	}
	return buf.Bytes()
}

func TestPHashDeterministic(t *testing.T) {
	a := PHashImage(gradientImage(128, 128))
	b := PHashImage(gradientImage(128, 128))
	if a == 0 || a != b {
		t.Fatalf("同图指纹应确定非零且相等：%016x vs %016x", a, b)
	}
}

func TestPHashScalingInvariance(t *testing.T) {
	small := PHashImage(gradientImage(64, 64))
	large := PHashImage(gradientImage(256, 256))
	if d := PHashDistance(small, large); d >= 5 {
		t.Fatalf("同图不同尺寸距离应 <5，得到 %d（%016x vs %016x）", d, small, large)
	}
}

func TestPHashSolidImage(t *testing.T) {
	// 纯色图 AC 系数理论为零，但浮点 cos 累加留下 epsilon 级噪声，
	// 指纹非零但必须确定；不同纯色噪声模式一致，距离应小（DC 已排除）。
	a := PHashImage(solidImage(100, 100, color.RGBA{200, 200, 200, 255}))
	b := PHashImage(solidImage(100, 100, color.RGBA{200, 200, 200, 255}))
	if a != b {
		t.Fatalf("同纯色指纹应确定相等：%016x vs %016x", a, b)
	}
	c := PHashImage(solidImage(100, 100, color.RGBA{50, 50, 50, 255}))
	if d := PHashDistance(a, c); d >= 5 {
		t.Fatalf("不同纯色指纹距离应 <5，得到 %d（%016x vs %016x）", d, a, c)
	}
}

func TestPHashDistinctImages(t *testing.T) {
	g := PHashImage(gradientImage(128, 128))
	c := PHashImage(checkerImage(128, 128, 8))
	if d := PHashDistance(g, c); d <= 5 {
		t.Fatalf("梯度 vs 棋盘应显著不同（>5），得到 %d", d)
	}
}

func TestPHashFormatMatrix(t *testing.T) {
	base := gradientImage(128, 128)
	ref := encodePNG(t, base)

	var jpgBuf bytes.Buffer
	if err := jpeg.Encode(&jpgBuf, base, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatalf("JPEG 编码失败：%v", err)
	}
	var gifBuf bytes.Buffer
	if err := gif.Encode(&gifBuf, base, nil); err != nil {
		t.Fatalf("GIF 编码失败：%v", err)
	}

	refFP, err := PHashFromBytes(ref)
	if err != nil {
		t.Fatalf("PNG 解码失败：%v", err)
	}
	for name, raw := range map[string][]byte{"jpeg": jpgBuf.Bytes(), "gif": gifBuf.Bytes()} {
		fp, err := PHashFromBytes(raw)
		if err != nil {
			t.Fatalf("%s 解码失败：%v", name, err)
		}
		if d := PHashDistance(refFP, fp); d >= 5 {
			t.Fatalf("%s 与 PNG 同图距离应 <5，得到 %d", name, d)
		}
	}
}

func TestPHashFromReader(t *testing.T) {
	raw := encodePNG(t, gradientImage(64, 64))
	a, err := PHashFromReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("Reader 路径失败：%v", err)
	}
	b, err := PHashFromBytes(raw)
	if err != nil || a != b {
		t.Fatalf("Reader 与 Bytes 应一致：%016x vs %016x err=%v", a, b, err)
	}
}

func TestPHashTransparentFlatten(t *testing.T) {
	// 全透明 NRGBA：铺白底后为纯白（AC 理论零 + 浮点 epsilon 噪声），
	// 不 panic 且与纯白图指纹一致即可。
	img := image.NewNRGBA(image.Rect(0, 0, 64, 64))
	got := PHashImage(img)
	want := PHashImage(solidImage(64, 64, color.White))
	if got != want {
		t.Fatalf("全透明铺白应与纯白一致：%016x vs %016x", got, want)
	}
}

func TestPHashEmptyImage(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 0, 0))
	if got := PHashImage(img); got != 0 {
		t.Fatalf("空图应回 0，得到 %016x", got)
	}
}

func TestPHashRejectNonImage(t *testing.T) {
	for name, raw := range map[string][]byte{
		"空":     {},
		"文本":    []byte("hello world, not an image"),
		"BMP魔数": {0x42, 0x4D, 0x00, 0x01, 0x02, 0x03},
	} {
		if _, err := PHashFromBytes(raw); !errors.Is(err, ErrInvalidImage) {
			t.Fatalf("%s 应返回 ErrInvalidImage，得到 %v", name, err)
		}
		if _, err := PHashFromReader(bytes.NewReader(raw)); !errors.Is(err, ErrInvalidImage) {
			t.Fatalf("%s Reader 应返回 ErrInvalidImage，得到 %v", name, err)
		}
	}
}

func TestPHashRejectTruncated(t *testing.T) {
	raw := encodePNG(t, gradientImage(32, 32))
	cut := raw[:len(raw)/2]
	if _, err := PHashFromBytes(cut); !errors.Is(err, ErrInvalidImage) {
		t.Fatalf("截断 PNG 应返回 ErrInvalidImage，得到 %v", err)
	}
}

// minimalPNGWithIHDR 仅含 IHDR 的最小 PNG，DecodeConfig 只读头，
// 可在不分配大内存的情况下验证 25MP 像素闸。
func minimalPNGWithIHDR(w, h uint32) []byte {
	var buf bytes.Buffer
	buf.Write([]byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A})
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:4], w)
	binary.BigEndian.PutUint32(ihdr[4:8], h)
	ihdr[8] = 8 // 位深
	ihdr[9] = 2 // 真彩
	chunk := make([]byte, 8)
	binary.BigEndian.PutUint32(chunk[0:4], 13)
	copy(chunk[4:8], []byte("IHDR"))
	crc := crc32.ChecksumIEEE(append([]byte("IHDR"), ihdr...))
	var crcBytes [4]byte
	binary.BigEndian.PutUint32(crcBytes[:], crc)
	buf.Write(chunk)
	buf.Write(ihdr)
	buf.Write(crcBytes[:])
	return buf.Bytes()
}

func TestPHashRejectBomb(t *testing.T) {
	// 10000×10000 = 1 亿像素 > 25MP，必须在 DecodeConfig 阶段拒绝。
	raw := minimalPNGWithIHDR(10000, 10000)
	if _, err := PHashFromBytes(raw); !errors.Is(err, ErrInvalidImage) {
		t.Fatalf("炸弹图应返回 ErrInvalidImage，得到 %v", err)
	}
	// 正常小尺寸头不应被像素闸误杀（后续缺 IDAT 解码失败同样转 ErrInvalidImage，但路径不同）。
	small := minimalPNGWithIHDR(64, 64)
	if _, err := PHashFromBytes(small); !errors.Is(err, ErrInvalidImage) {
		t.Fatalf("小头 PNG 缺数据应解码失败转 ErrInvalidImage，得到 %v", err)
	}
}

func TestPHashEncodeParse(t *testing.T) {
	const fp uint64 = 0xabcdef0123456789
	if got := EncodePHash(fp); got != "abcdef0123456789" {
		t.Fatalf("Encode 口径应与 SimHash 一致，得到 %q", got)
	}
	back, err := ParsePHash("abcdef0123456789")
	if err != nil || back != fp {
		t.Fatalf("往返失败：back=%016x err=%v", back, err)
	}
	if _, err := ParsePHash("ABCDEF0123456789"); err == nil {
		t.Fatal("大写应判错")
	}
}

func TestPHashDistance(t *testing.T) {
	if PHashDistance(0, 0) != 0 {
		t.Fatal("0 vs 0 应为 0")
	}
	if PHashDistance(0, 1) != 1 {
		t.Fatal("单比特应为 1")
	}
}
