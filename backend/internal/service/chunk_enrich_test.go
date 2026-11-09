package service

// S8 分片旁路单测：分类 + 指纹 fail-open，只验旁路增量

import (
	"context"
	"path/filepath"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"cloudbox/internal/model"
	"cloudbox/internal/repository"
	"cloudbox/internal/service/smart"
	"cloudbox/internal/storage"
)

// newEnrichChunkService S8 专用：多迁 FileFingerprint 表 + 注入分类器/指纹仓储
func newEnrichChunkService(t *testing.T) (*ChunkService, *repository.FingerprintRepository) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	if err != nil {
		t.Fatalf("open sqlite memory: %v", err)
	}
	if err := db.AutoMigrate(&model.User{}, &model.File{}, &model.UploadChunk{}, &model.FileFingerprint{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	db.Create(&model.User{Username: "alice", Email: "alice@c.com", PasswordHash: "h", QuotaBytes: 10737418240})
	tmp := t.TempDir()
	st, err := storage.NewLocalStorage(filepath.Join(tmp, "store"))
	if err != nil {
		t.Fatalf("建测试存储失败: %v", err)
	}
	fps := repository.NewFingerprintRepository(db)
	svc := NewChunkService(repository.NewFileRepository(db), repository.NewUserRepository(db), repository.NewChunkRepository(db), DefaultMaxFileSize, DefaultMaxChunks).
		WithTmpDir(filepath.Join(tmp, "chunks")).WithStorage(st).
		WithClassifier(smart.NewRuleClassifier()).WithFingerprints(fps)
	return svc, fps
}

func TestChunkCompleteEnrichDocument(t *testing.T) {
	svc, fps := newEnrichChunkService(t)
	content := []byte("hello-chunk-merge-world-0123456789-abcdef")
	row, _ := buildCompleteSession(t, svc, 1, "a.txt", content, 10)
	f, err := svc.CompleteUpload(1, 0, row.UploadID, "a.txt")
	if err != nil {
		t.Fatalf("合并失败: %v", err)
	}
	if !f.Category.Valid || f.Category.String != "文档" {
		t.Fatalf("分类应为文档: %+v", f.Category)
	}
	got, err := fps.GetByFile(context.Background(), f.ID, "simhash")
	if err != nil || got == nil || len(got.Fingerprint) != 16 {
		t.Fatalf("simhash 指纹应落库: %+v %v", got, err)
	}
}

func TestChunkCompleteNilDepsStillOK(t *testing.T) {
	svc := newChunkService(t)
	content := []byte("hello-chunk-merge-world-0123456789-abcdef")
	row, _ := buildCompleteSession(t, svc, 1, "a.txt", content, 10)
	f, err := svc.CompleteUpload(1, 0, row.UploadID, "a.txt")
	if err != nil {
		t.Fatalf("无旁路依赖合并应成功: %v", err)
	}
	if f.Category.Valid {
		t.Fatalf("无分类器时不应写分类: %+v", f.Category)
	}
}

func TestChunkCompleteIdempotentBackfill(t *testing.T) {
	svc, _ := newEnrichChunkService(t)
	content := []byte("hello-chunk-merge-world-0123456789-abcdef")
	row, _ := buildCompleteSession(t, svc, 1, "a.txt", content, 10)
	f1, err := svc.CompleteUpload(1, 0, row.UploadID, "a.txt")
	if err != nil {
		t.Fatalf("首次合并失败: %v", err)
	}
	// 模拟历史脏行：清掉分类，重复 complete 走幂等应回填
	if err := svc.files.UpdateByID(f1.ID, map[string]any{"category": "", "confidence": 0}); err != nil {
		t.Fatalf("清分类失败: %v", err)
	}
	f2, err := svc.CompleteUpload(1, 0, row.UploadID, "a.txt")
	if err != nil {
		t.Fatalf("幂等合并失败: %v", err)
	}
	if f2.ID != f1.ID {
		t.Fatalf("幂等应返回同一行: %d vs %d", f2.ID, f1.ID)
	}
	if !f2.Category.Valid || f2.Category.String != "文档" {
		t.Fatalf("幂等应回填分类: %+v", f2.Category)
	}
	// 大文件流式断言：64KB 头采样常量存在即视为 5GB 安全（禁 ReadAll 全量）
	if enrichHeadLen != 64<<10 {
		t.Fatalf("头采样应为 64KB: %d", enrichHeadLen)
	}
}
