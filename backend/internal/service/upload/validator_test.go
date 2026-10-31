package upload

import (
	"errors"
	"testing"
)

var (
	pdfHead = []byte("%PDF-1.7\n%test\n")
	jpgHead = []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x4A, 0x46, 0x49, 0x46, 0x00}
	pngHead = []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52}
	gifHead = []byte("GIF89a\x01\x00\x01\x00\x80\x00\x00")
	zipHead = []byte{0x50, 0x4B, 0x03, 0x04, 0x14, 0x00, 0x00, 0x00}
	mp4Head = []byte{0x00, 0x00, 0x00, 0x18, 0x66, 0x74, 0x79, 0x70, 0x69, 0x73, 0x6F, 0x6D}
	mzHead  = []byte{0x4D, 0x5A, 0x90, 0x00, 0x03, 0x00, 0x00, 0x00}
)

func TestValidateSize(t *testing.T) {
	if err := ValidateSize(100, MaxSimpleUploadSize); err != nil {
		t.Fatalf("正常大小应过: %v", err)
	}
	if err := ValidateSize(2<<20+1, MaxSimpleUploadSize); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("超2MB应 ErrTooLarge, 得到 %v", err)
	}
	if err := ValidateSize(0, MaxSimpleUploadSize); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("空文件应 ErrTooLarge, 得到 %v", err)
	}
	if err := ValidateSize(-1, MaxSimpleUploadSize); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("负数应 ErrTooLarge, 得到 %v", err)
	}
	// limit<=0 回退 MaxFileSize
	if err := ValidateSize(100, 0); err != nil {
		t.Fatalf("回退默认应过: %v", err)
	}
}

func TestValidateExt(t *testing.T) {
	cases := []struct {
		name    string
		file    string
		wantExt string
		wantErr error
	}{
		{"pdf放行", "a.pdf", "pdf", nil},
		{"大写放行", "A.JPG", "jpg", nil},
		{"带空格放行", "report.pdf ", "pdf", nil},
		{"exe直接拒", "x.exe", "", ErrUnsupportedExt},
		{"双扩展名拒", "evil.pdf.exe", "", ErrUnsupportedExt},
		{"无扩展名拒", "noext", "", ErrUnsupportedExt},
		{"未知扩展拒", "a.xyz", "", ErrUnsupportedExt},
		{"路径分隔拒", "a/b.pdf", "", ErrUnsupportedExt},
		{"空名拒", "  ", "", ErrUnsupportedExt},
		{"sh拒", "run.sh", "", ErrUnsupportedExt},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ext, err := ValidateExt(tc.file)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("应 %v, 得到 %v", tc.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("应放行, 得到 %v", err)
			}
			if ext != tc.wantExt {
				t.Fatalf("ext=%q want %q", ext, tc.wantExt)
			}
		})
	}
}

func TestSniffAndCheck(t *testing.T) {
	cases := []struct {
		name string
		file string
		head []byte
		ok   bool
	}{
		{"pdf头正确放行", "a.pdf", pdfHead, true},
		{"jpg头正确放行", "p.jpg", jpgHead, true},
		{"png头正确放行", "p.png", pngHead, true},
		{"gif头正确放行", "p.gif", gifHead, true},
		{"zip头放行", "a.zip", zipHead, true},
		{"mp4头放行", "v.mp4", mp4Head, true},
		{"exe直接拒", "x.exe", mzHead, false},
		{"双扩展名拒", "evil.pdf.exe", mzHead, false},
		{"改名攻击拒", "fake.jpg", mzHead, false},
		{"无扩展名拒", "noext", pdfHead, false},
		{"大写扩展名放行", "A.JPG", jpgHead, true},
		{"空内容拒", "a.pdf", nil, false},
		{"内容错配拒", "a.pdf", jpgHead, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ext, err := ValidateExt(tc.file)
			if err != nil {
				if tc.ok {
					t.Fatalf("扩展名应过: %v", err)
				}
				return
			}
			_, err = SniffAndCheck(tc.head, ext)
			if tc.ok && err != nil {
				t.Fatalf("应放行, 得到 %v", err)
			}
			if !tc.ok && err == nil {
				t.Fatal("应拒绝却放行")
			}
		})
	}
}

func TestSniffMimeValue(t *testing.T) {
	ext, err := ValidateExt("a.pdf")
	if err != nil {
		t.Fatalf("扩展名应过: %v", err)
	}
	mime, err := SniffAndCheck(pdfHead, ext)
	if err != nil {
		t.Fatalf("嗅探应过: %v", err)
	}
	if mime == "" || mime == "application/octet-stream" {
		t.Fatalf("MIME 不可判: %q", mime)
	}
}
