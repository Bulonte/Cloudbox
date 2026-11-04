package repository

import (
	"testing"

	"cloudbox/internal/model"
)

func TestChunkRepositorySession(t *testing.T) {
	db := openTestDB(t)
	repo := NewChunkRepository(db)

	row := &model.UploadChunk{UploadID: "11111111-1111-4111-8111-111111111111", FileHash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ChunkIndex: ChunkSessionMarker, TotalChunks: 50, ChunkSize: 2097152, OwnerID: 1}
	if err := repo.CreateInitRow(row); err != nil {
		t.Fatalf("建会话失败: %v", err)
	}
	// 会话占位行可查
	s, err := repo.FindSession(row.UploadID, 1)
	if err != nil || s.TotalChunks != 50 {
		t.Fatalf("查会话失败: %+v %v", s, err)
	}
	// 他人查一律 ErrChunkNotFound（伪装）
	if _, err := repo.FindSession(row.UploadID, 2); err != ErrChunkNotFound {
		t.Fatalf("他人应 ErrChunkNotFound, 得到 %v", err)
	}
	if _, err := repo.FindByUploadID(row.UploadID, 2); err != ErrChunkNotFound {
		t.Fatalf("他人全量应 ErrChunkNotFound, 得到 %v", err)
	}
	// 空会话已传序号返回 [] 非 nil
	idx, err := repo.ListUploadedIndexes(row.UploadID, 1)
	if err != nil || idx == nil || len(idx) != 0 {
		t.Fatalf("空进度应 [] 非 nil: %v %v", idx, err)
	}
}

func TestChunkUpsertDone(t *testing.T) {
	db := openTestDB(t)
	repo := NewChunkRepository(db)
	uid := "22222222-2222-4222-8222-222222222222"
	hash := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	// 幂等插入→重传更新仍单条
	if err := repo.UpsertChunkDone(uid, 1, 0, 2, 2097152, hash, uid+"/0.part"); err != nil {
		t.Fatalf("upsert 失败: %v", err)
	}
	if err := repo.UpsertChunkDone(uid, 1, 0, 2, 2097152, hash, uid+"/0.part"); err != nil {
		t.Fatalf("重传应幂等: %v", err)
	}
	got, err := repo.FindChunk(uid, 1, 0)
	if err != nil || got.Status != 1 || got.ChunkPath != uid+"/0.part" {
		t.Fatalf("查分片不对: %+v %v", got, err)
	}
	// 他人查/覆盖一律 ErrChunkNotFound
	if _, err := repo.FindChunk(uid, 2, 0); err != ErrChunkNotFound {
		t.Fatalf("他人应 ErrChunkNotFound, 得到 %v", err)
	}
	if err := repo.UpsertChunkDone(uid, 2, 0, 2, 2097152, hash, uid+"/0.part"); err != ErrChunkNotFound {
		t.Fatalf("他人覆盖应 ErrChunkNotFound, 得到 %v", err)
	}
	if _, err := repo.FindChunk(uid, 1, 9); err != ErrChunkNotFound {
		t.Fatalf("缺片应 ErrChunkNotFound, 得到 %v", err)
	}
}
