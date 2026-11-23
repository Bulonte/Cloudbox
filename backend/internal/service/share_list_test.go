package service

import (
	"testing"
)

// TestListSharesOK 本人两条分享分页回显：total=2，file_name 批量回填
func TestListSharesOK(t *testing.T) {
	svc, cleanup := newShareService(t)
	defer cleanup()

	if _, err := svc.CreateShare(CreateShareInput{OwnerID: 1, FileID: 1}); err != nil {
		t.Fatalf("预置1失败: %v", err)
	}
	if _, err := svc.CreateShare(CreateShareInput{OwnerID: 1, FileID: 1}); err != nil {
		t.Fatalf("预置2失败: %v", err)
	}
	entries, total, page, pageSize, err := svc.ListShares(1, 0, 0)
	if err != nil {
		t.Fatalf("列表失败: %v", err)
	}
	if total != 2 || page != 1 || pageSize != 20 || len(entries) != 2 {
		t.Fatalf("分页不对: total=%d page=%d size=%d len=%d", total, page, pageSize, len(entries))
	}
	for _, e := range entries {
		if e == nil || e.Share == nil || e.FileName != "a.txt" {
			t.Fatalf("file_name 回填不对: %+v", e)
		}
	}
	// 第二页 page_size=1 应只剩 1 条（id DESC 倒序不断）
	entries, total, page, _, err = svc.ListShares(1, 2, 1)
	if err != nil || total != 2 || page != 2 || len(entries) != 1 {
		t.Fatalf("第二页不对: total=%d page=%d len=%d err=%v", total, page, len(entries), err)
	}
}

// TestListSharesIsolation 他人看不见；取消后不回；文件删后名空串不阻断
func TestListSharesIsolation(t *testing.T) {
	svc, cleanup := newShareService(t)
	defer cleanup()

	row, err := svc.CreateShare(CreateShareInput{OwnerID: 1, FileID: 1})
	if err != nil {
		t.Fatalf("预置失败: %v", err)
	}
	// bob 查 total=0
	entries, total, _, _, err := svc.ListShares(2, 1, 20)
	if err != nil || total != 0 || len(entries) != 0 {
		t.Fatalf("他人应隔离: total=%d len=%d err=%v", total, len(entries), err)
	}
	// 零用户参错
	if _, _, _, _, err := svc.ListShares(0, 1, 20); err == nil {
		t.Fatal("零用户应参错")
	}
	// 取消后本人也看不见
	if err := svc.CancelShare(1, row.ID); err != nil {
		t.Fatalf("取消失败: %v", err)
	}
	if _, total, _, _, _ := svc.ListShares(1, 1, 20); total != 0 {
		t.Fatalf("取消后应 total=0, 得到 %d", total)
	}
}

// TestListSharesFileGone 文件软删后列表仍回行，FileName 为空由 DTO 兜底
func TestListSharesFileGone(t *testing.T) {
	svc, cleanup := newShareService(t)
	defer cleanup()

	if _, err := svc.CreateShare(CreateShareInput{OwnerID: 1, FileID: 1}); err != nil {
		t.Fatalf("预置失败: %v", err)
	}
	if err := svc.files.SoftDelete(1, 1); err != nil {
		t.Fatalf("软删失败: %v", err)
	}
	entries, total, _, _, err := svc.ListShares(1, 1, 20)
	if err != nil || total != 1 || len(entries) != 1 {
		t.Fatalf("删文件后行应保留: total=%d err=%v", total, err)
	}
	if entries[0].FileName != "" {
		t.Fatalf("删文件后名应空串: %q", entries[0].FileName)
	}
}
