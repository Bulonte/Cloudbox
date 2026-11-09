package service

import (
	"context"
	"errors"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"cloudbox/internal/model"
	"cloudbox/internal/repository"
	"cloudbox/internal/service/smart"
)

func newSimilarService(t *testing.T) (*SimilarService, *gorm.DB) {
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
	fps := repository.NewFingerprintRepository(db)
	files := repository.NewFileRepository(db)
	return NewSimilarService(fps, files), db
}

func mustCreateSimilarFile(t *testing.T, db *gorm.DB, name, hash string, owner uint64) uint64 {
	t.Helper()
	f := &model.File{FileName: name, FileHash: hash, FilePath: "/s/" + hash, OwnerID: owner, Visibility: "private"}
	if err := repository.NewFileRepository(db).CreateFile(f); err != nil {
		t.Fatalf("预置 %s 失败: %v", name, err)
	}
	return f.ID
}

// distHex 以非零基线翻转低 n 位，得到与 distHex(0) 距离恰为 n 的 hex
// （基线不能是 0：目标 0 按 S5 空指纹语义直接回空；不依赖分词/图片，零 CGO）
func distHex(n uint) string {
	const base uint64 = 0xAAAAAAAAAAAAAAAA
	fp := base
	if n > 0 {
		fp = base ^ ((uint64(1) << n) - 1)
	}
	return smart.EncodeSimHash(fp)
}

func TestFindSimilarThresholdBoundary(t *testing.T) {
	svc, db := newSimilarService(t)
	ctx := context.Background()
	fps := repository.NewFingerprintRepository(db)

	// 逐个建不同 hash 的文件
	mk := func(hash string) uint64 {
		return mustCreateSimilarFile(t, db, hash+".txt", hash, 1)
	}
	id0 := mk("th-h0")
	id1 := mk("th-h1")
	id4 := mk("th-h4")
	id5 := mk("th-h5")
	id6 := mk("th-h6")
	for id, n := range map[uint64]int{id0: 0, id1: 1, id4: 4, id5: 5, id6: 6} {
		if err := fps.Upsert(ctx, id, AlgorithmSimHash, distHex(uint(n))); err != nil {
			t.Fatalf("upsert 失败: %v", err)
		}
	}

	got, err := svc.FindSimilarByFingerprint(ctx, 1, AlgorithmSimHash, distHex(0), FindSimilarOptions{Limit: 10})
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("距离 0/1/4 应命中 3 条（5/6 排除），得到 %d: %+v", len(got), got)
	}
	if got[0].Distance != 0 || got[1].Distance != 1 || got[2].Distance != 4 {
		t.Fatalf("应按距离升序 0,1,4: %+v", got)
	}
	if got[0].FileID != id0 || got[1].FileID != id1 || got[2].FileID != id4 {
		t.Fatalf("file_id 对应错误: %+v", got)
	}
}

func TestFindSimilarExcludeSelfAndLimit(t *testing.T) {
	svc, db := newSimilarService(t)
	ctx := context.Background()
	fps := repository.NewFingerprintRepository(db)

	a := mustCreateSimilarFile(t, db, "a.txt", "ex-ha", 1)
	b := mustCreateSimilarFile(t, db, "b.txt", "ex-hb", 1)
	c := mustCreateSimilarFile(t, db, "c.txt", "ex-hc", 1)
	for _, id := range []uint64{a, b, c} {
		if err := fps.Upsert(ctx, id, AlgorithmSimHash, distHex(1)); err != nil {
			t.Fatalf("upsert 失败: %v", err)
		}
	}

	got, err := svc.FindSimilarByFingerprint(ctx, 1, AlgorithmSimHash, distHex(1), FindSimilarOptions{ExcludeFileID: a, Limit: 10})
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("排除自己后应 2 条，得到 %d", len(got))
	}
	for _, it := range got {
		if it.FileID == a {
			t.Fatalf("自排除失效: %+v", got)
		}
	}

	got, err = svc.FindSimilarByFingerprint(ctx, 1, AlgorithmSimHash, distHex(1), FindSimilarOptions{Limit: 1})
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("Limit=1 应截断为 1 条，得到 %d", len(got))
	}
}

func TestFindSimilarInvalidInput(t *testing.T) {
	svc, _ := newSimilarService(t)
	ctx := context.Background()

	if _, err := svc.FindSimilarByFingerprint(ctx, 1, "md5", distHex(0), FindSimilarOptions{}); !errors.Is(err, ErrUnsupportedAlgorithm) {
		t.Fatalf("非法算法应 ErrUnsupportedAlgorithm: %v", err)
	}
	for _, bad := range []string{"", "ABCDEF0123456789", "xyz", "00000000000000000"} {
		if _, err := svc.FindSimilarByFingerprint(ctx, 1, AlgorithmSimHash, bad, FindSimilarOptions{}); !errors.Is(err, ErrInvalidFingerprint) {
			t.Fatalf("非法指纹 %q 应 ErrInvalidFingerprint: %v", bad, err)
		}
	}
	got, err := svc.FindSimilarByFingerprint(ctx, 1, AlgorithmSimHash, distHex(0)[:0]+"0000000000000000", FindSimilarOptions{})
	if err != nil || len(got) != 0 {
		t.Fatalf("目标 0 应回空不报错: %+v err=%v", got, err)
	}
}

func TestFindSimilarDirtyRowSkipped(t *testing.T) {
	svc, db := newSimilarService(t)
	ctx := context.Background()
	fps := repository.NewFingerprintRepository(db)

	good := mustCreateSimilarFile(t, db, "good.txt", "dirty-h1", 1)
	dirty := mustCreateSimilarFile(t, db, "dirty.txt", "dirty-h2", 1)
	if err := fps.Upsert(ctx, good, AlgorithmSimHash, distHex(0)); err != nil {
		t.Fatalf("upsert 失败: %v", err)
	}
	if err := fps.Upsert(ctx, dirty, AlgorithmSimHash, "NOT-HEX-AT-ALL!!!!"); err != nil {
		t.Fatalf("脏行写入失败: %v", err)
	}

	got, err := svc.FindSimilarByFingerprint(ctx, 1, AlgorithmSimHash, distHex(0), FindSimilarOptions{})
	if err != nil {
		t.Fatalf("脏行应跳过不炸整查询: %v", err)
	}
	if len(got) != 1 || got[0].FileID != good {
		t.Fatalf("应仅命中好行: %+v", got)
	}
}

func TestFindSimilarByFile(t *testing.T) {
	svc, db := newSimilarService(t)
	ctx := context.Background()
	fps := repository.NewFingerprintRepository(db)

	src := mustCreateSimilarFile(t, db, "src.txt", "bf-h1", 1)
	near := mustCreateSimilarFile(t, db, "near.txt", "bf-h2", 1)
	if err := fps.Upsert(ctx, src, AlgorithmSimHash, distHex(0)); err != nil {
		t.Fatalf("upsert 失败: %v", err)
	}
	if err := fps.Upsert(ctx, near, AlgorithmSimHash, distHex(2)); err != nil {
		t.Fatalf("upsert 失败: %v", err)
	}

	got, err := svc.FindSimilarByFile(ctx, 1, src, AlgorithmSimHash, 10)
	if err != nil {
		t.Fatalf("按文件查失败: %v", err)
	}
	if len(got) != 1 || got[0].FileID != near || got[0].Distance != 2 {
		t.Fatalf("应命中 near 距离 2: %+v", got)
	}

	// 源无指纹回空
	empty := mustCreateSimilarFile(t, db, "empty.txt", "bf-h3", 1)
	got, err = svc.FindSimilarByFile(ctx, 1, empty, AlgorithmSimHash, 10)
	if err != nil || len(got) != 0 {
		t.Fatalf("源无指纹应回空: %+v err=%v", got, err)
	}

	// 查他人文件 → ErrFileNotFound 伪装
	if _, err := svc.FindSimilarByFile(ctx, 2, src, AlgorithmSimHash, 10); !errors.Is(err, ErrFileNotFound) {
		t.Fatalf("查他人文件应 ErrFileNotFound: %v", err)
	}
	if _, err := svc.FindSimilarByFile(ctx, 1, src, "md5", 10); !errors.Is(err, ErrUnsupportedAlgorithm) {
		t.Fatalf("非法算法应 ErrUnsupportedAlgorithm: %v", err)
	}
}
