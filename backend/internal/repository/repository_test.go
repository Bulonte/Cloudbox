package repository

import (
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"cloudbox/internal/model"
)

// openTestDB SQLite内存库：每次全新，不依赖Docker；生产建表只认migrations/*.sql
func openTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	if err != nil {
		t.Fatalf("open sqlite memory: %v", err)
	}
	if err := db.AutoMigrate(
		&model.User{},
		&model.File{},
		&model.FileShare{},
		&model.FileFingerprint{},
		&model.UploadChunk{},
		&model.OperationLog{},
		&model.CategoryKeyword{},
	); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	return db
}

func TestUserRepositoryCRUD(t *testing.T) {
	db := openTestDB(t)
	repo := NewBaseRepository[model.User](db)

	u := &model.User{Username: "alice", Email: "alice@example.com", PasswordHash: "bcrypt-hash", QuotaBytes: 10737418240}
	if err := repo.Create(u); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if u.ID == 0 {
		t.Fatal("want auto-increment ID, got 0")
	}

	var got model.User
	if err := db.Where("username = ?", "alice").First(&got).Error; err != nil {
		t.Fatalf("query user: %v", err)
	}
	if got.Email != "alice@example.com" {
		t.Fatalf("want email alice@example.com, got %s", got.Email)
	}
	if got.QuotaBytes != 10737418240 {
		t.Fatalf("want default quota 10737418240, got %d", got.QuotaBytes)
	}
}

func TestFileRepositoryUniqueHash(t *testing.T) {
	db := openTestDB(t)
	repo := NewBaseRepository[model.File](db)

	f1 := &model.File{FileName: "a.txt", FileHash: "abc123", FilePath: "/store/ab/c123", OwnerID: 1}
	if err := repo.Create(f1); err != nil {
		t.Fatalf("create file: %v", err)
	}
	// 同hash第二次插入必须报唯一冲突（uk_hash语义）
	f2 := &model.File{FileName: "b.txt", FileHash: "abc123", FilePath: "/store/ab/c123", OwnerID: 2}
	if err := repo.Create(f2); err == nil {
		t.Fatal("want unique conflict on duplicate file_hash, got nil")
	}
}

func TestCategoryKeywordQueryByCategory(t *testing.T) {
	db := openTestDB(t)
	repo := NewBaseRepository[model.CategoryKeyword](db)

	seeds := []model.CategoryKeyword{
		{Type: "category", Category: "合同", Keyword: "甲方", Weight: 1.5},
		{Type: "category", Category: "合同", Keyword: "乙方", Weight: 1.5},
		{Type: "category", Category: "合同", Keyword: "违约金", Weight: 2.0},
	}
	for i := range seeds {
		if err := repo.Create(&seeds[i]); err != nil {
			t.Fatalf("create keyword: %v", err)
		}
	}

	var got []model.CategoryKeyword
	if err := db.Where("type = ? AND category = ?", "category", "合同").Find(&got).Error; err != nil {
		t.Fatalf("query keywords: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 keywords, got %d", len(got))
	}
}
