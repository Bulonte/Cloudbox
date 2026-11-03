package dto

// CreateFolderRequest 建文件夹请求体
type CreateFolderRequest struct {
	FileName string `json:"file_name"`
	ParentID uint64 `json:"parent_id"`
}

// UpdateFileRequest 重命名/移动请求体：两字段可选，二合一
type UpdateFileRequest struct {
	FileName *string `json:"file_name"`
	ParentID *uint64 `json:"parent_id"`
}

// FileResponse 文件/文件夹脱敏返回：不含存储内幕，只给展示字段
type FileResponse struct {
	ID        uint64 `json:"id"`
	FileName  string `json:"file_name"`
	ParentID  uint64 `json:"parent_id"`
	IsFolder  int8   `json:"is_folder"`
	FileSize  uint64 `json:"file_size"`
	MimeType  string `json:"mime_type"`
	Category  string `json:"category,omitempty"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// ListFilesQuery 列表查询：parent_id 缺省 0（根）；page/page_size 缺省 1/20
type ListFilesQuery struct {
	ParentID uint64 `form:"parent_id"`
	Page     int    `form:"page"`
	PageSize int    `form:"page_size"`
}

// FileListResponse 分页列表返回：items 恒为数组（空目录 [] 非 null）
type FileListResponse struct {
	Total    int64          `json:"total"`
	Page     int            `json:"page"`
	PageSize int            `json:"page_size"`
	Items    []FileResponse `json:"items"`
}

// InstantCheckRequest 秒传预检请求体（S4：上传前 SHA256 查询，不碰存储字节流）
type InstantCheckRequest struct {
	FileName string `json:"file_name"`
	ParentID uint64 `json:"parent_id"`
	FileHash string `json:"file_hash"`
}

// InstantCheckResponse 秒传预检回显：hit=true 本人已有秒传成功；
// hit=false + exists_global=true 他人已存（uk 全局唯一，实传仍 409，前端可提示跳过）
type InstantCheckResponse struct {
	Hit          bool          `json:"hit"`
	ExistsGlobal bool          `json:"exists_global,omitempty"`
	File         *FileResponse `json:"file,omitempty"`
}
