package repository

import (
	"context"
	"testing"

	"cloudbox/internal/model"
)

func newFingerprintFixture(t *testing.T) *FingerprintRepository {
	t.Helper()
	return NewFingerprintRepository(openTestDB(t))
}

func TestFingerprintUpsertIdempotent(t *testing.T) {
	r := newFingerprintFixture(t)
	ctx := context.Background()

	// 直插文件行（仓储单测只验指纹语义，文件行仅作 JOIN 存在性）
	db := r.db
	f := &model.File{FileName: "a.txt", FileHash: "fp-hash-1", FilePath: "/s/a", OwnerID: 1, Visibility: "private"}
	if err := NewFileRepository(db).CreateFile(f); err != nil {
		t.Fatalf("预置文件失败: %v", err)
	}

	if err := r.Upsert(ctx, f.ID, "simhash", "0000000000000001"); err != nil {
		t.Fatalf("首次写入失败: %v", err)
	}
	if err := r.Upsert(ctx, f.ID, "simhash", "0000000000000002"); err != nil {
		t.Fatalf("幂等覆盖失败: %v", err)
	}
	got, err := r.GetByFile(ctx, f.ID, "simhash")
	if err != nil || got == nil {
		t.Fatalf("回查失败: %+v err=%v", got, err)
	}
	if got.Fingerprint != "0000000000000002" {
		t.Fatalf("应被覆盖为新值，得到 %q", got.Fingerprint)
	}
	var cnt int64
	if err := db.Model(&model.FileFingerprint{}).Where("file_id = ?", f.ID).Count(&cnt).Error; err != nil || cnt != 1 {
		t.Fatalf("同(file_id,algorithm)应仅一行: cnt=%d err=%v", cnt, err)
	}
}

func TestFingerprintGetByFileMiss(t *testing.T) {
	r := newFingerprintFixture(t)
	got, err := r.GetByFile(context.Background(), 9999, "simhash")
	if err != nil || got != nil {
		t.Fatalf("miss 应回 nil,nil: %+v err=%v", got, err)
	}
}

func TestFingerprintListCandidatesFilter(t *testing.T) {
	r := newFingerprintFixture(t)
	ctx := context.Background()
	fr := NewFileRepository(r.db)

	mk := func(name, hash string, owner uint64) uint64 {
		t.Helper()
		f := &model.File{FileName: name, FileHash: hash, FilePath: "/s/" + hash, OwnerID: owner, Visibility: "private"}
		if err := fr.CreateFile(f); err != nil {
			t.Fatalf("预置 %s 失败: %v", name, err)
		}
		return f.ID
	}

	keep1 := mk("k1.txt", "cand-h1", 1)
	keep2 := mk("k2.txt", "cand-h2", 1)
	otherAlgo := mk("p.png", "cand-h3", 1)
	otherOwner := mk("o.txt", "cand-h4", 2)
	deleted := mk("d.txt", "cand-h5", 1)

	mustUpsert := func(id uint64, algo, fp string) {
		t.Helper()
		if err := r.Upsert(ctx, id, algo, fp); err != nil {
			t.Fatalf("upsert %d 失败: %v", id, err)
		}
	}
	mustUpsert(keep1, "simhash", "0000000000000001")
	mustUpsert(keep2, "simhash", "0000000000000002")
	mustUpsert(otherAlgo, "phash", "0000000000000001")
	mustUpsert(otherOwner, "simhash", "0000000000000001")
	mustUpsert(deleted, "simhash", "0000000000000001")
	if err := r.db.Model(&model.File{}).Where("id = ?", deleted).Update("is_deleted", 1).Error; err != nil {
		t.Fatalf("软删预置失败: %v", err)
	}

	rows, err := r.ListCandidates(ctx, 1, "simhash")
	if err != nil {
		t.Fatalf("粗筛失败: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("应仅回同人同算法未删 2 行，得到 %d", len(rows))
	}
	if rows[0].FileID != keep1 || rows[1].FileID != keep2 {
		t.Fatalf("应按 file_id 升序: %+v", rows)
	}
	if rows[0].FileName != "k1.txt" || rows[1].FileName != "k2.txt" {
		t.Fatalf("文件名顺带错误: %+v", rows)
	}
}

func TestFingerprintDeleteByFile(t *testing.T) {
	r := newFingerprintFixture(t)
	ctx := context.Background()
	f := &model.File{FileName: "x.txt", FileHash: "del-h1", FilePath: "/s/del", OwnerID: 1, Visibility: "private"}
	if err := NewFileRepository(r.db).CreateFile(f); err != nil {
		t.Fatalf("预置失败: %v", err)
	}
	if err := r.Upsert(ctx, f.ID, "simhash", "0000000000000001"); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	if err := r.Upsert(ctx, f.ID, "phash", "0000000000000001"); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	if err := r.DeleteByFile(ctx, f.ID); err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	if got, err := r.GetByFile(ctx, f.ID, "simhash"); err != nil || got != nil {
		t.Fatalf("删后应 miss: %+v err=%v", got, err)
	}
}
