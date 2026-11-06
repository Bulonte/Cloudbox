package service

import (
	"errors"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"cloudbox/internal/model"
	"cloudbox/internal/repository"
)

func newShareService(t *testing.T) (*ShareService, func()) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	if err != nil {
		t.Fatalf("open sqlite memory: %v", err)
	}
	if err := db.AutoMigrate(&model.User{}, &model.File{}, &model.FileShare{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	db.Create(&model.User{Username: "alice", Email: "alice@s.com", PasswordHash: "h", QuotaBytes: 10737418240})
	db.Create(&model.User{Username: "bob", Email: "bob@s.com", PasswordHash: "h", QuotaBytes: 10737418240})
	files := repository.NewFileRepository(db)
	shares := repository.NewShareRepository(db)
	// 预置 alice 文件供分享
	if err := files.CreateFile(&model.File{FileName: "a.txt", FileHash: "hash-a-txt-1", OwnerID: 1}); err != nil {
		t.Fatalf("预置文件失败: %v", err)
	}
	svc := NewShareService(shares, files)
	return svc, func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	}
}

func TestGenerateShareCodeShape(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		c, err := generateShareCode()
		if err != nil {
			t.Fatalf("生成失败: %v", err)
		}
		if len(c) != ShareCodeLength {
			t.Fatalf("码长=%d, 期望16: %q", len(c), c)
		}
		for _, ch := range c {
			if !strings.ContainsRune(base62Alphabet, ch) {
				t.Fatalf("非法字符 %q in %q", ch, c)
			}
		}
		if seen[c] {
			t.Fatalf("100个码碰撞: %q", c)
		}
		seen[c] = true
	}
}

func TestCreateShareOKDefault(t *testing.T) {
	svc, cleanup := newShareService(t)
	defer cleanup()

	before := time.Now()
	got, err := svc.CreateShare(CreateShareInput{OwnerID: 1, FileID: 1})
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	if len(got.ShareCode) != 16 || got.Status != 1 || got.DownloadCount != 0 {
		t.Fatalf("回显不对: %+v", got)
	}
	if got.OwnerID != 1 || got.FileID != 1 {
		t.Fatalf("归属不对: %+v", got)
	}
	if got.PasswordHash.Valid {
		t.Fatalf("无密码时 Valid 应为 false")
	}
	if got.ExpireAt.Valid {
		_ = got.ExpireAt
	} else {
		t.Fatalf("默认有效期应有值")
	}
	d := got.ExpireAt.Time.Sub(before)
	if d < 7*24*time.Hour-5*time.Minute || d > 7*24*time.Hour+5*time.Minute {
		t.Fatalf("默认7天不对: %v", got.ExpireAt.Time)
	}
	if got.MaxDownloads.Valid {
		t.Fatalf("默认次数应不限")
	}
}

func TestCreateShareWithPassword(t *testing.T) {
	svc, cleanup := newShareService(t)
	defer cleanup()

	pw := "123456"
	got, err := svc.CreateShare(CreateShareInput{OwnerID: 1, FileID: 1, Password: pw})
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	if !got.PasswordHash.Valid || got.PasswordHash.String == "" {
		t.Fatalf("有密码应落库: %+v", got)
	}
	if got.PasswordHash.String == pw {
		t.Fatalf("密码存了明文")
	}
}

func TestCreateShareReject(t *testing.T) {
	svc, cleanup := newShareService(t)
	defer cleanup()

	past := time.Now().Add(-time.Hour)
	far := time.Now().Add(31 * 24 * time.Hour)
	over := time.Now().Add(30*24*time.Hour + time.Hour)
	zero := int32(0)
	neg := int32(-1)
	okMax := int32(3)
	future := time.Now().Add(24 * time.Hour)

	cases := []struct {
		name string
		in   CreateShareInput
		want error
	}{
		{"零用户", CreateShareInput{OwnerID: 0, FileID: 1}, ErrInvalidParams},
		{"零文件", CreateShareInput{OwnerID: 1, FileID: 0}, ErrInvalidParams},
		{"文件不存在", CreateShareInput{OwnerID: 1, FileID: 999999}, ErrFileNotFound},
		{"他人文件", CreateShareInput{OwnerID: 2, FileID: 1}, ErrFileNotFound},
		{"过期时间过去", CreateShareInput{OwnerID: 1, FileID: 1, ExpireAt: &past}, ErrInvalidParams},
		{"过期超30天", CreateShareInput{OwnerID: 1, FileID: 1, ExpireAt: &far}, ErrInvalidParams},
		{"过期刚超30天", CreateShareInput{OwnerID: 1, FileID: 1, ExpireAt: &over}, ErrInvalidParams},
		{"次数零", CreateShareInput{OwnerID: 1, FileID: 1, MaxDownloads: &zero}, ErrInvalidParams},
		{"次数负", CreateShareInput{OwnerID: 1, FileID: 1, MaxDownloads: &neg}, ErrInvalidParams},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := svc.CreateShare(tc.in); !errors.Is(err, tc.want) {
				t.Fatalf("应 %v, 得到 %v", tc.want, err)
			}
		})
	}
	// 合法边界应放行：未来1天 + 次数3
	if _, err := svc.CreateShare(CreateShareInput{OwnerID: 1, FileID: 1, ExpireAt: &future, MaxDownloads: &okMax}); err != nil {
		t.Fatalf("合法边界应放行: %v", err)
	}
}

func TestCreateShareConflictRetry(t *testing.T) {
	svc, cleanup := newShareService(t)
	defer cleanup()

	// 先占一个码，再让生成器首码碰撞、次码全新，验证重试成功
	first, err := svc.CreateShare(CreateShareInput{OwnerID: 1, FileID: 1})
	if err != nil {
		t.Fatalf("预置失败: %v", err)
	}
	calls := 0
	svc.codeGen = func() (string, error) {
		calls++
		if calls == 1 {
			return first.ShareCode, nil
		}
		return generateShareCode()
	}
	got, err := svc.CreateShare(CreateShareInput{OwnerID: 1, FileID: 1})
	if err != nil {
		t.Fatalf("重试应成功: %v", err)
	}
	if got.ShareCode == first.ShareCode {
		t.Fatalf("重试后码仍碰撞")
	}
	if calls < 2 {
		t.Fatalf("未发生重试 calls=%d", calls)
	}
}
