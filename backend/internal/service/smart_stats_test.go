package service

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"cloudbox/internal/model"
	"cloudbox/internal/repository"
)

func newSmartStatsService(t *testing.T) *SmartStatsService {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	if err != nil {
		t.Fatalf("open sqlite memory: %v", err)
	}
	if err := db.AutoMigrate(&model.User{}, &model.File{}, &model.FileFingerprint{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	db.Create(&model.User{Username: "alice", Email: "alice@s.com", PasswordHash: "h", QuotaBytes: 10737418240})
	db.Create(&model.User{Username: "bob", Email: "bob@s.com", PasswordHash: "h", QuotaBytes: 100})
	files := repository.NewFileRepository(db)
	fps := repository.NewFingerprintRepository(db)
	users := repository.NewUserRepository(db)
	return NewSmartStatsService(files, fps, users)
}

func seedStatsFile(t *testing.T, svc *SmartStatsService, owner uint64, name, hash, mime, category string, size uint64, created time.Time) uint64 {
	t.Helper()
	ns := sql.NullString{}
	if category != "" {
		ns = sql.NullString{String: category, Valid: true}
	}
	f := &model.File{
		FileName: name, FileHash: hash, FileSize: size, FilePath: "p/" + hash,
		MimeType: mime, OwnerID: owner, ParentID: 0, IsFolder: 0,
		Category: ns, CreatedAt: created, UpdatedAt: created,
	}
	if err := svc.files.CreateFile(f); err != nil {
		t.Fatalf("预置文件失败: %v", err)
	}
	return f.ID
}

func TestSmartStatsEmpty(t *testing.T) {
	svc := newSmartStatsService(t)
	res, err := svc.GetStats(1, 0)
	if err != nil {
		t.Fatalf("空库失败: %v", err)
	}
	if res.Storage.UsedBytes != 0 || res.Storage.FileCount != 0 {
		t.Fatalf("空库存储应为零: %+v", res.Storage)
	}
	if res.Storage.QuotaBytes != 10737418240 {
		t.Fatalf("配额应透出用户行: %d", res.Storage.QuotaBytes)
	}
	if len(res.Timeline) != 14 {
		t.Fatalf("days=0 应缺省 14 天: %d", len(res.Timeline))
	}
	if len(res.Category) != 0 {
		t.Fatalf("空库分类应空: %+v", res.Category)
	}
	if res.Duplicates.ExactGroups != 0 || res.Duplicates.SimilarGroups != 0 {
		t.Fatalf("空库重复应为零: %+v", res.Duplicates)
	}
	// days 超限归一 30
	res2, err := svc.GetStats(1, 99)
	if err != nil || len(res2.Timeline) != 30 {
		t.Fatalf("days=99 应归一 30: %v %d", err, len(res2.Timeline))
	}
	// 零用户参错
	if _, err := svc.GetStats(0, 14); err == nil {
		t.Fatal("零用户应参错")
	}
}

func TestSmartStatsFull(t *testing.T) {
	svc := newSmartStatsService(t)
	now := time.Now()
	day0 := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	// uk_hash 全局唯一：本人内精确重复恒为 0，重复报告走指纹相似组
	aID := seedStatsFile(t, svc, 1, "a.jpg", fmt.Sprintf("%064x", 11), "image/jpeg", "图片", 1000, day0)
	seedStatsFile(t, svc, 1, "b.pdf", fmt.Sprintf("%064x", 22), "application/pdf", "", 2000, day0.AddDate(0, 0, -1))
	c1ID := seedStatsFile(t, svc, 1, "c1.txt", fmt.Sprintf("%064x", 33), "text/plain", "文档", 500, day0)
	c2ID := seedStatsFile(t, svc, 1, "c2.txt", fmt.Sprintf("%064x", 34), "text/plain", "文档", 500, day0)
	// 相似指纹：同算法 simhash，低 2 比特不同 → Hamming=2 <5 成组；第三个远指纹不成组
	ctx := context.Background()
	if err := svc.fps.Upsert(ctx, c1ID, "simhash", "fffffffffffffffc"); err != nil {
		t.Fatalf("写指纹失败: %v", err)
	}
	if err := svc.fps.Upsert(ctx, c2ID, "simhash", "fffffffffffffffe"); err != nil {
		t.Fatalf("写指纹失败: %v", err)
	}
	if err := svc.fps.Upsert(ctx, aID, "simhash", "0000000000000000"); err != nil {
		t.Fatalf("写指纹失败: %v", err)
	}
	// 他人文件不计入
	seedStatsFile(t, svc, 2, "other.txt", fmt.Sprintf("%064x", 44), "text/plain", "文档", 9999, day0)

	res, err := svc.GetStats(1, 7)
	if err != nil {
		t.Fatalf("聚合失败: %v", err)
	}
	if res.Storage.FileCount != 4 {
		t.Fatalf("文件数应 4: %d", res.Storage.FileCount)
	}
	if res.Storage.UsedBytes != 4000 {
		t.Fatalf("总字节应 4000: %d", res.Storage.UsedBytes)
	}
	// 分类：文档 3 + 图片 1 + 待确认 1？b.pdf 空类归待确认：文档 3? c1/c2/b? b 空类→待确认 1，文档 2，图片 1
	catMap := map[string]int64{}
	for _, c := range res.Category {
		catMap[c.Category] = c.Count
	}
	if catMap["文档"] != 2 || catMap["图片"] != 1 || catMap["待确认"] != 1 {
		t.Fatalf("分类不对: %+v", res.Category)
	}
	// 时间线 7 天补零不断线
	if len(res.Timeline) != 7 {
		t.Fatalf("时间线应 7 点: %d", len(res.Timeline))
	}
	// 精确重复恒为 0（uk_hash 全局唯一）；相似组 1 组 2 文件
	if res.Duplicates.ExactGroups != 0 || res.Duplicates.ExactFiles != 0 || res.Duplicates.ExactSavedBytes != 0 {
		t.Fatalf("精确重复应为零: %+v", res.Duplicates)
	}
	if res.Duplicates.SimilarGroups != 1 || res.Duplicates.SimilarFiles != 2 {
		t.Fatalf("相似组不对: %+v", res.Duplicates)
	}
	if len(res.Duplicates.Items) != 1 || res.Duplicates.Items[0].Kind != "similar" {
		t.Fatalf("明细应 1 similar 行: %+v", res.Duplicates.Items)
	}
	// 存储 by_type 固定 6 项顺序
	if len(res.Storage.ByType) != 6 || res.Storage.ByType[0].Type != "image" || res.Storage.ByType[5].Type != "other" {
		t.Fatalf("by_type 顺序不对: %+v", res.Storage.ByType)
	}
}
