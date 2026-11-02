package dto

// InitUploadRequest 分片会话初始化请求体
type InitUploadRequest struct {
	FileName    string `json:"file_name"`
	ParentID    uint64 `json:"parent_id"`
	FileHash    string `json:"file_hash"`
	TotalSize   uint64 `json:"total_size"`
	TotalChunks uint32 `json:"total_chunks"`
	ChunkSize   uint64 `json:"chunk_size"`
}

// InitUploadResponse 会话签发回显：不含 chunk_path 等内部字段
type InitUploadResponse struct {
	UploadID    string `json:"upload_id"`
	FileHash    string `json:"file_hash"`
	TotalChunks uint32 `json:"total_chunks"`
	ChunkSize   uint64 `json:"chunk_size"`
	Status      int8   `json:"status"`
}

// UploadChunkResponse 单分片上传回显：不含 chunk_path/file_hash 内部信息
type UploadChunkResponse struct {
	UploadID   string `json:"upload_id"`
	ChunkIndex uint32 `json:"chunk_index"`
	Size       uint64 `json:"size"`
	Status     int8   `json:"status"`
}
