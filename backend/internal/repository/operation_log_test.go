package repository

import (
	"database/sql"
	"testing"
	"time"

	"cloudbox/internal/model"
)

func nullStrForLog(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

func nullI64ForLog(n uint64) sql.NullInt64 {
	if n == 0 {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: int64(n), Valid: true}
}

// TestOperationLogCreateAndList S4：写后可查，倒序；过滤按用户/动作/时间
func TestOperationLogCreateAndList(t *testing.T) {
	db := openTestDB(t)
	r := NewOperationLogRepository(db)

	rows := []*model.OperationLog{
		{UserID: 1, Action: ActionShareCreate, TargetType: nullStrForLog("share"), TargetID: nullI64ForLog(10), IP: nullStrForLog("1.2.3.4"), Detail: nullStrForLog(`{"share_id":10}`)},
		{UserID: 2, Action: ActionDownload, TargetType: nullStrForLog("file"), TargetID: nullI64ForLog(20)},
		{UserID: 1, Action: ActionShareAccess},
	}
	for _, row := range rows {
		if err := r.CreateLog(row); err != nil {
			t.Fatalf("CreateLog 失败: %v", err)
		}
		if row.ID == 0 {
			t.Fatal("ID 未回填")
		}
	}

	got, total, err := r.ListLogs(LogFilter{}, 0, 20)
	if err != nil {
		t.Fatalf("List 失败: %v", err)
	}
	if total != 3 || len(got) != 3 {
		t.Fatalf("total=%d len=%d, 期望3", total, len(got))
	}
	if !(got[0].ID > got[1].ID && got[1].ID > got[2].ID) {
		t.Fatalf("非倒序: %d %d %d", got[0].ID, got[1].ID, got[2].ID)
	}

	// 用户过滤
	_, total, _ = r.ListLogs(LogFilter{UserID: 1}, 0, 20)
	if total != 2 {
		t.Fatalf("user=1 total=%d, 期望2", total)
	}
	// 动作过滤
	got, total, _ = r.ListLogs(LogFilter{Action: ActionDownload}, 0, 20)
	if total != 1 || got[0].UserID != 2 {
		t.Fatalf("action 过滤不对: %+v", got)
	}
	// 时间过滤：未来区间应为 0
	future := time.Now().Add(time.Hour)
	got, total, _ = r.ListLogs(LogFilter{StartTime: future}, 0, 20)
	if total != 0 || len(got) != 0 {
		t.Fatalf("未来区间应空: total=%d", total)
	}
	// 分页归一：limit<=0 按 20；超限截断
	_, _, err = r.ListLogs(LogFilter{}, -5, 0)
	if err != nil {
		t.Fatalf("分页归一失败: %v", err)
	}
}
