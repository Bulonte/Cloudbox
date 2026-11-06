package service

import (
	"strings"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"cloudbox/internal/model"
	"cloudbox/internal/repository"
)

func newAuditService(t *testing.T) (*AuditService, *repository.OperationLogRepository) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	if err != nil {
		t.Fatalf("open sqlite memory: %v", err)
	}
	if err := db.AutoMigrate(&model.OperationLog{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	logs := repository.NewOperationLogRepository(db)
	return NewAuditService(logs), logs
}

// TestAuditRecordC方案：detail 只记 share_id/file_id/file_name，share_code 明文/哈希一律不落库
func TestAuditRecordCScheme(t *testing.T) {
	svc, logs := newAuditService(t)
	in := AuditInput{
		UserID: 1, Action: repository.ActionShareCreate,
		TargetType: "share", TargetID: 10, IP: "1.2.3.4",
		Detail: map[string]any{"share_id": uint64(10), "file_id": uint64(20), "file_name": "a.txt"},
	}
	if err := svc.Record(in); err != nil {
		t.Fatalf("Record 失败: %v", err)
	}
	rows, total, err := logs.ListLogs(repository.LogFilter{}, 0, 20)
	if err != nil || total != 1 {
		t.Fatalf("回查失败 total=%d err=%v", total, err)
	}
	d := ""
	if rows[0].Detail.Valid {
		d = rows[0].Detail.String
	}
	if strings.Contains(d, "share_code") {
		t.Fatalf("C方案违规：detail 含 share_code: %s", d)
	}
	for _, want := range []string{`"share_id":10`, `"file_id":20`, "a.txt"} {
		if !strings.Contains(d, want) {
			t.Fatalf("detail 缺 %s: %s", want, d)
		}
	}
	if rows[0].TargetType.String != "share" || rows[0].TargetID.Int64 != 10 {
		t.Fatalf("target 不对: %+v", rows[0])
	}
}

// TestAuditSanitizeShareCode：误传 share_code 键只剥键不整条丢；值形似 bcrypt/JWT 则整条丢弃落库失败也不阻断
func TestAuditSanitizeShareCode(t *testing.T) {
	svc, logs := newAuditService(t)
	// 误传 share_code 明文：剥键后仍落库
	_ = svc.Record(AuditInput{
		UserID: 1, Action: repository.ActionShareCreate,
		TargetType: "share", TargetID: 11,
		Detail: map[string]any{"share_id": uint64(11), "share_code": "ABCDEF1234567890"},
	})
	rows, total, _ := logs.ListLogs(repository.LogFilter{}, 0, 20)
	if total != 1 {
		t.Fatalf("剥键后应落库 total=%d", total)
	}
	if strings.Contains(rows[0].Detail.String, "ABCDEF1234567890") {
		t.Fatalf("share_code 明文落库: %s", rows[0].Detail.String)
	}
	// 值形似 bcrypt：整条丢弃
	_ = svc.Record(AuditInput{
		UserID: 1, Action: repository.ActionDownload,
		TargetType: "file", TargetID: 21,
		Detail: map[string]any{"file_id": uint64(21), "sig": "$2a$10$abcdefghijklmnopqrstuu"},
	})
	if _, total2, _ := logs.ListLogs(repository.LogFilter{}, 0, 20); total2 != 1 {
		t.Fatalf("bcrypt 值应整条丢弃 total=%d", total2)
	}
	// fail-open：Record 永不返回错误阻断主流程
	if err := svc.Record(AuditInput{UserID: 1, Action: "illegal_action"}); err != nil {
		t.Fatalf("非法动作应吞错 nil, 得到 %v", err)
	}
	var nilSvc *AuditService
	if err := nilSvc.Record(AuditInput{}); err != nil {
		t.Fatalf("nil 服务应吞错 nil, 得到 %v", err)
	}
}

// TestAuditListPagination：分页归一 page<1→1，pageSize 越界截断
func TestAuditListPagination(t *testing.T) {
	svc, _ := newAuditService(t)
	for i := 0; i < 3; i++ {
		_ = svc.Record(AuditInput{UserID: uint64(i + 1), Action: repository.ActionDownload})
	}
	res, err := svc.List(AuditQuery{}, 0, 0)
	if err != nil || res.Page != 1 || res.PageSize != 20 || res.Total != 3 {
		t.Fatalf("归一不对: %+v err=%v", res, err)
	}
	res, _ = svc.List(AuditQuery{}, 1, 5000)
	if res.PageSize != 1000 {
		t.Fatalf("超限应截断 1000, 得到 %d", res.PageSize)
	}
	if _, err := svc.List(AuditQuery{UserID: 1}, 1, 20); err != nil {
		t.Fatalf("过滤查询失败: %v", err)
	}
}
