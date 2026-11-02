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
