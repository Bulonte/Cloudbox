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
