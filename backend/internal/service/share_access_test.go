package service

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"cloudbox/internal/model"
	"cloudbox/internal/repository"
	"cloudbox/internal/storage"
)

// newShareAccessService S2 专用：内存库 + 真存储 + 可选限流
func newShareAccessService(t *testing.T, limiter RedisStore) (*ShareService, func()) {
	t.Helper()
	svc, cleanup := newShareService(t)
	st, err := storage.NewLocalStorage(t.TempDir())
	if err != nil {
		cleanup()
		t.Fatalf("建测试存储失败: %v", err)
	}
	svc.WithStorage(st).WithLimiter(limiter)
	return svc, cleanup
}

var shareFileSeq int

// seedShareFile 落一个带真实存储对象的文件，返回文件 ID 与内容
func seedShareFile(t *testing.T, svc *ShareService, ownerID uint64, name, content string) (uint64, string) {
	t.Helper()
	shareFileSeq++
	// 64 位 hex：用序号混入保证唯一且合法
	base := fmt.Sprintf("%064x", shareFileSeq*7919+len(content)*131+len(name))
	st := svc.store.(*storage.LocalStorage)
	rel, err := st.Store(base, strings.NewReader(content))
	if err != nil {
		t.Fatalf("Store 失败: %v", err)
	}
	f := &model.File{
		FileName: name,
		FileHash: base,
		FileSize: uint64(len(content)),
		FilePath: rel,
		MimeType: "text/plain",
		OwnerID:  ownerID,
	}
	if err := svc.files.CreateFile(f); err != nil {
		t.Fatalf("建文件失败: %v", err)
	}
	return f.ID, content
}

func TestAccessShareOK(t *testing.T) {
	svc, cleanup := newShareAccessService(t, nil)
	defer cleanup()
	fileID, _ := seedShareFile(t, svc, 1, "hello.txt", "hello world")
	row, err := svc.CreateShare(CreateShareInput{OwnerID: 1, FileID: fileID})
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	share, f, err := svc.AccessShare(row.ShareCode, "", "1.2.3.4")
	if err != nil {
		t.Fatalf("访问失败: %v", err)
	}
	if share.ShareCode != row.ShareCode || f.FileName != "hello.txt" {
		t.Fatalf("回显不对: %+v %+v", share, f)
	}
}

func TestAccessSharePassword(t *testing.T) {
	svc, cleanup := newShareAccessService(t, nil)
	defer cleanup()
	fileID, _ := seedShareFile(t, svc, 1, "secret.txt", "top secret")
	row, err := svc.CreateShare(CreateShareInput{OwnerID: 1, FileID: fileID, Password: "Abc12345"})
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	if _, _, err := svc.AccessShare(row.ShareCode, "Abc12345", "1.2.3.4"); err != nil {
		t.Fatalf("正确密码应过: %v", err)
	}
	if _, _, err := svc.AccessShare(row.ShareCode, "wrongpwd", "1.2.3.4"); !errors.Is(err, repository.ErrShareNotFound) {
		t.Fatalf("错密码应伪装 404, 得到 %v", err)
	}
	if _, _, err := svc.AccessShare(row.ShareCode, "", "1.2.3.4"); !errors.Is(err, repository.ErrShareNotFound) {
		t.Fatalf("空密码应伪装 404, 得到 %v", err)
	}
}

func TestAccessShareCamouflage(t *testing.T) {
	svc, cleanup := newShareAccessService(t, nil)
	defer cleanup()
	fileID, _ := seedShareFile(t, svc, 1, "camo.txt", "camo")

	// 非法格式 → 参错
	if _, _, err := svc.AccessShare("short", "", "9.9.9.9"); !errors.Is(err, ErrInvalidParams) {
		t.Fatalf("短码应参错, 得到 %v", err)
	}
	// 格式合法但不存在 → 伪装 404
	if _, _, err := svc.AccessShare("0000000000000000", "", "9.9.9.9"); !errors.Is(err, repository.ErrShareNotFound) {
		t.Fatalf("错码应伪装 404, 得到 %v", err)
	}

	// 过期伪装：建未来 1 天再回拨 expire_at 到过去（CreateShare 拒绝过去时间，只能先建合法再改）
	row, err := svc.CreateShare(CreateShareInput{OwnerID: 1, FileID: fileID})
	if err != nil {
		t.Fatalf("预置失败: %v", err)
	}
	past := time.Now().Add(-time.Hour)
	if err := svc.shares.SetExpireAt(row.ID, sql.NullTime{Time: past, Valid: true}); err != nil {
		t.Fatalf("改过期失败: %v", err)
	}
	if _, _, err := svc.AccessShare(row.ShareCode, "", "9.9.9.9"); !errors.Is(err, repository.ErrShareNotFound) {
		t.Fatalf("过期应伪装 404, 得到 %v", err)
	}

	// 超次伪装：max=1，先下载一次再访问
	one := int32(1)
	row2, err := svc.CreateShare(CreateShareInput{OwnerID: 1, FileID: fileID, MaxDownloads: &one})
	if err != nil {
		t.Fatalf("预置2失败: %v", err)
	}
	_, rc2, _, err := svc.DownloadShare(row2.ShareCode, "", "9.9.9.9")
	if err != nil {
		t.Fatalf("首次下载应成功: %v", err)
	}
	_ = rc2.Close()
	if _, _, err := svc.AccessShare(row2.ShareCode, "", "9.9.9.9"); !errors.Is(err, repository.ErrShareNotFound) {
		t.Fatalf("超次应伪装 404, 得到 %v", err)
	}

	// 文件夹伪装
	shareFileSeq++
	folderHash := fmt.Sprintf("%064x", 900000+shareFileSeq)
	folder := &model.File{FileName: "dir", FileHash: folderHash, OwnerID: 1, IsFolder: 1}
	if err := svc.files.CreateFile(folder); err != nil {
		t.Fatalf("建夹失败: %v", err)
	}
	row3, err := svc.CreateShare(CreateShareInput{OwnerID: 1, FileID: folder.ID})
	if err != nil {
		t.Fatalf("分享夹失败: %v", err)
	}
	if _, _, err := svc.AccessShare(row3.ShareCode, "", "9.9.9.9"); !errors.Is(err, repository.ErrShareNotFound) {
		t.Fatalf("文件夹应伪装 404, 得到 %v", err)
	}

	// 已删文件伪装：软删后访问旧分享
	fileID2, _ := seedShareFile(t, svc, 1, "gone.txt", "gone")
	row4, err := svc.CreateShare(CreateShareInput{OwnerID: 1, FileID: fileID2})
	if err != nil {
		t.Fatalf("预置4失败: %v", err)
	}
	if err := svc.files.SoftDelete(fileID2, 1); err != nil {
		t.Fatalf("软删失败: %v", err)
	}
	if _, _, err := svc.AccessShare(row4.ShareCode, "", "9.9.9.9"); !errors.Is(err, repository.ErrShareNotFound) {
		t.Fatalf("已删文件应伪装 404, 得到 %v", err)
	}

	// 取消分享伪装
	if err := svc.shares.CancelShare(row4.ID, 1); err != nil {
		t.Fatalf("取消失败: %v", err)
	}
	if _, _, err := svc.AccessShare(row4.ShareCode, "", "9.9.9.9"); !errors.Is(err, repository.ErrShareNotFound) {
		t.Fatalf("取消后应伪装 404, 得到 %v", err)
	}
}

func TestAccessShareRateLimit(t *testing.T) {
	fr := newFakeRedis()
	svc, cleanup := newShareAccessService(t, fr)
	defer cleanup()
	svc.WithRateLimit(2)
	fileID, _ := seedShareFile(t, svc, 1, "rl.txt", "rl")
	row, err := svc.CreateShare(CreateShareInput{OwnerID: 1, FileID: fileID})
	if err != nil {
		t.Fatalf("预置失败: %v", err)
	}
	for i := 0; i < 2; i++ {
		if _, _, err := svc.AccessShare(row.ShareCode, "", "5.6.7.8"); err != nil {
			t.Fatalf("前2次应过: %v", err)
		}
	}
	if _, _, err := svc.AccessShare(row.ShareCode, "", "5.6.7.8"); !errors.Is(err, ErrTooManyAttempts) {
		t.Fatalf("第3次应限流, 得到 %v", err)
	}
}

func TestDownloadShareOKAndExhaust(t *testing.T) {
	svc, cleanup := newShareAccessService(t, nil)
	defer cleanup()
	fileID, content := seedShareFile(t, svc, 1, "dl.txt", "download-bytes-123")
	one := int32(1)
	row, err := svc.CreateShare(CreateShareInput{OwnerID: 1, FileID: fileID, MaxDownloads: &one})
	if err != nil {
		t.Fatalf("预置失败: %v", err)
	}
	f, rc, size, err := svc.DownloadShare(row.ShareCode, "", "7.7.7.7")
	if err != nil {
		t.Fatalf("下载失败: %v", err)
	}
	defer func() { _ = rc.Close() }()
	if f.ID != fileID || size != int64(len(content)) {
		t.Fatalf("下载元数据不对: %+v size=%d", f, size)
	}
	buf := make([]byte, len(content))
	n := 0
	for n < len(buf) {
		m, rerr := rc.Read(buf[n:])
		n += m
		if rerr != nil {
			break
		}
	}
	if string(buf[:n]) != content {
		t.Fatalf("内容不对: %q", buf[:n])
	}
	// 计数应为 1
	got, err := svc.shares.FindByCode(row.ShareCode)
	if err != nil {
		t.Fatalf("回查失败: %v", err)
	}
	if got.DownloadCount != 1 {
		t.Fatalf("计数=%d, 期望1", got.DownloadCount)
	}
	// 第二次：Access 先判超次，直接 404
	if _, _, _, err := svc.DownloadShare(row.ShareCode, "", "7.7.7.7"); !errors.Is(err, repository.ErrShareNotFound) {
		t.Fatalf("超次下载应 404, 得到 %v", err)
	}
}

func TestIncrDownloadCountGuard(t *testing.T) {
	svc, cleanup := newShareAccessService(t, nil)
	defer cleanup()
	fileID, _ := seedShareFile(t, svc, 1, "cnt.txt", "cnt")
	one := int32(1)
	row, err := svc.CreateShare(CreateShareInput{OwnerID: 1, FileID: fileID, MaxDownloads: &one})
	if err != nil {
		t.Fatalf("预置失败: %v", err)
	}
	if err := svc.shares.IncrDownloadCount(row.ShareCode); err != nil {
		t.Fatalf("首次+1失败: %v", err)
	}
	if err := svc.shares.IncrDownloadCount(row.ShareCode); !errors.Is(err, repository.ErrShareNotFound) {
		t.Fatalf("超次+1应伪装 404, 得到 %v", err)
	}
}
