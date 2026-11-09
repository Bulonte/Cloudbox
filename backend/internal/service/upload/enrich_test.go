package upload

// S8 上传旁路单测：分类 + 指纹 fail-open，不断言旧链路行为，只验旁路增量

import (
	"bytes"
	"context"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"cloudbox/internal/model"
	"cloudbox/internal/repository"
	"cloudbox/internal/service/smart"
	"cloudbox/internal/storage"
)

// newEnrichService S8 专用：多迁 FileFingerprint 表 + 注入分类器/指纹仓储
func newEnrichService(t *testing.T) (*UploadService, *repository.FingerprintRepository) {
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
	st, err := storage.NewLocalStorage(t.TempDir())
	if err != nil {
		t.Fatalf("建存储失败: %v", err)
	}
	ur := repository.NewUserRepository(db)
	db.Create(&model.User{Username: "alice", Email: "alice@f.com", PasswordHash: "h", QuotaBytes: 10737418240})
	s := NewUploadService(repository.NewFileRepository(db), st, MaxSimpleUploadSize).WithUsers(ur)
	fps := repository.NewFingerprintRepository(db)
	s.WithClassifier(smart.NewRuleClassifier()).WithFingerprints(fps)
	return s, fps
}

func TestUploadEnrichDocument(t *testing.T) {
	s, fps := newEnrichService(t)
	f, err := s.Upload(1, 0, "a.pdf", bytes.NewReader(pdfHead), int64(len(pdfHead)))
	if err != nil {
		t.Fatalf("上传失败: %v", err)
	}
	if !f.Category.Valid || f.Category.String != "文档" {
		t.Fatalf("分类应为文档: %+v", f.Category)
	}
	if !f.Confidence.Valid || f.Confidence.Float64 != 0.95 {
		t.Fatalf("扩展名直判置信度应 0.95: %+v", f.Confidence)
	}
	row, err := fps.GetByFile(context.Background(), f.ID, "simhash")
	if err != nil || row == nil || len(row.Fingerprint) != 16 {
		t.Fatalf("simhash 指纹应落库: %+v %v", row, err)
	}
}

func TestUploadEnrichContractKeyword(t *testing.T) {
	s, fps := newEnrichService(t)
	body := []byte("甲方 乙方 违约金 合同条款正文内容补充说明")
	f, err := s.Upload(1, 0, "合同初稿.ppt", bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("上传失败: %v", err)
	}
	if !f.Category.Valid || f.Category.String != "合同" {
		t.Fatalf("分类应为合同: %+v", f.Category)
	}
	row, err := fps.GetByFile(context.Background(), f.ID, "simhash")
	if err != nil || row == nil {
		t.Fatalf("simhash 指纹应落库: %+v %v", row, err)
	}
}

func TestUploadEnrichNilDepsStillOK(t *testing.T) {
	s := newUploadService(t)
	f, err := s.Upload(1, 0, "a.pdf", bytes.NewReader(pdfHead), int64(len(pdfHead)))
	if err != nil {
		t.Fatalf("无旁路依赖上传应成功: %v", err)
	}
	if f.Category.Valid {
		t.Fatalf("无分类器时不应写分类: %+v", f.Category)
	}
}

func TestUploadPassiveBackfill(t *testing.T) {
	s, _ := newEnrichService(t)
	f1, err := s.Upload(1, 0, "a.pdf", bytes.NewReader(pdfHead), int64(len(pdfHead)))
	if err != nil {
		t.Fatalf("首次失败: %v", err)
	}
	// 模拟历史脏行：清掉分类，再走被动秒传应回填
	if err := s.files.UpdateByID(f1.ID, map[string]any{"category": "", "confidence": 0}); err != nil {
		t.Fatalf("清分类失败: %v", err)
	}
	f2, err := s.Upload(1, 0, "b.pdf", bytes.NewReader(pdfHead), int64(len(pdfHead)))
	if err != nil {
		t.Fatalf("被动秒传失败: %v", err)
	}
	if f2.ID != f1.ID {
		t.Fatalf("秒传应返回同一行: %d vs %d", f2.ID, f1.ID)
	}
	if !f2.Category.Valid || f2.Category.String != "文档" {
		t.Fatalf("被动秒传应回填分类: %+v", f2.Category)
	}
}
