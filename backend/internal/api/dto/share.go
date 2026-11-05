package dto

// CreateShareRequest 创建分享请求体：文件必传；密码/过期/次数可选
type CreateShareRequest struct {
	FileID       uint64  `json:"file_id"`
	Password     string  `json:"password"`
	ExpireAt     *string `json:"expire_at"`
	MaxDownloads *int32  `json:"max_downloads"`
}

// CreateShareResponse 创建分享回显：不含 password_hash
type CreateShareResponse struct {
	ID           uint64  `json:"id"`
	ShareCode    string  `json:"share_code"`
	ShareURL     string  `json:"share_url"`
	FileID       uint64  `json:"file_id"`
	ExpireAt     *string `json:"expire_at,omitempty"`
	MaxDownloads *int32  `json:"max_downloads,omitempty"`
}

// ShareInfoResponse 公开访问回显：只含脱敏元数据，不含 password_hash/内部路径
type ShareInfoResponse struct {
	ShareCode string  `json:"share_code"`
	FileName  string  `json:"file_name"`
	FileSize  uint64  `json:"file_size"`
	MimeType  string  `json:"mime_type"`
	ExpireAt  *string `json:"expire_at,omitempty"`
}
