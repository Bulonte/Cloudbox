package service

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"cloudbox/internal/model"
	"cloudbox/internal/storage"
)

// newDownloadService 真存储 + 内存库的 FileService
func newDownloadService(t *testing.T) *FileService {
	t.Helper()
	svc, cleanup := newFileService(t)
	t.Cleanup(cleanup)
	st, err := storage.NewLocalStorage(t.TempDir())
	if err != nil {
		t.Fatalf("建测试存储失败: %v", err)
	}
	return svc.WithStorage(st)
}

func seedDownloadFile(t *testing.T, svc *FileService, ownerID uint64, name, hash, rel string, isFolder int8) *model.File {
	t.Helper()
	f := &model.File{
		FileName: name,
		FileHash: hash,
		FileSize: uint64(len("download-bytes")),
		FilePath: rel,
		MimeType: "application/pdf",
		OwnerID:  ownerID,
		IsFolder: isFolder,
	}
	if err := svc.files.CreateFile(f); err != nil {
		t.Fatalf("预置文件失败: %v", err)
	}
	return f
}

func TestFileServiceDownload(t *testing.T) {
	svc := newDownloadService(t)
	st := svc.store

	// 预置 blob
	content := []byte("download-bytes-12345")
	rel, err := st.Store("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", bytes.NewReader(content))
	if err != nil {
		t.Fatalf("Store 失败: %v", err)
	}
	f := seedDownloadFile(t, svc, 1, "a.pdf", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", rel, 0)

	got, rc, size, err := svc.Download(1, f.ID)
	if err != nil {
		t.Fatalf("下载失败: %v", err)
	}
	defer func() { _ = rc.Close() }()
	if got.ID != f.ID {
		t.Fatalf("元数据不对: %+v", got)
	}
	if size != int64(len(content)) {
		t.Fatalf("size=%d 期望 %d", size, len(content))
	}
	data, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("读流失败: %v", err)
	}
	if !bytes.Equal(data, content) {
		t.Fatal("流内容不一致")
	}
}

func TestFileServiceDownloadReject(t *testing.T) {
	svc := newDownloadService(t)

	// 未注存储
	bare, cleanup := newFileService(t)
	defer cleanup()
	if _, _, _, err := bare.Download(1, 1); err == nil {
		t.Fatal("未注存储应报错")
	}

	// 非法参数
	if _, _, _, err := svc.Download(0, 1); !errors.Is(err, ErrInvalidParams) {
		t.Fatalf("零用户应 ErrInvalidParams, 得到 %v", err)
	}
	if _, _, _, err := svc.Download(1, 0); !errors.Is(err, ErrInvalidParams) {
		t.Fatalf("零 id 应 ErrInvalidParams, 得到 %v", err)
	}

	// 文件夹拒
	dir, err := svc.CreateFolder(1, "d", 0)
	if err != nil {
		t.Fatalf("建夹失败: %v", err)
	}
	if _, _, _, err := svc.Download(1, dir.ID); !errors.Is(err, ErrNotDownloadable) {
		t.Fatalf("文件夹应 ErrNotDownloadable, 得到 %v", err)
	}

	// 他人/不存在 404
	if _, _, _, err := svc.Download(2, dir.ID); !errors.Is(err, ErrFileNotFound) {
		t.Fatalf("他人应 ErrFileNotFound, 得到 %v", err)
	}
	if _, _, _, err := svc.Download(1, 999999); !errors.Is(err, ErrFileNotFound) {
		t.Fatalf("不存在应 ErrFileNotFound, 得到 %v", err)
	}
}

func TestFileServiceDownloadOrphan(t *testing.T) {
	svc := newDownloadService(t)
	// DB 有行但 blob 缺失 → 内部错（非 NotFound/NotDownloadable）
	f := seedDownloadFile(t, svc, 1, "orphan.pdf", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "ff/orphan-missing", 0)
	if _, _, _, err := svc.Download(1, f.ID); err == nil {
		t.Fatal("孤儿应报错")
	} else if errors.Is(err, ErrFileNotFound) || errors.Is(err, ErrNotDownloadable) || errors.Is(err, ErrInvalidParams) {
		t.Fatalf("孤儿不应伪装 404/400, 得到 %v", err)
	}
}
