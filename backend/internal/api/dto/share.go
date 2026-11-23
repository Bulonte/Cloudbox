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

// ShareListItem 分享管理行：file_name 由 Service 批量回填；禁透 password_hash
// S3：供 GET /api/v1/shares 列表用，只回本人数据（归属 SQL 层已过滤）
type ShareListItem struct {
	ID            uint64  `json:"id"`
	ShareCode     string  `json:"share_code"`
	ShareURL      string  `json:"share_url"`
	FileID        uint64  `json:"file_id"`
	FileName      string  `json:"file_name"`
	ExpireAt      *string `json:"expire_at,omitempty"`
	MaxDownloads  *int32  `json:"max_downloads,omitempty"`
	DownloadCount uint32  `json:"download_count"`
	CreatedAt     string  `json:"created_at"`
}

// ShareListResponse 分享分页列表：items 恒为数组（空列表 [] 非 null）
type ShareListResponse struct {
	Total    int64           `json:"total"`
	Page     int             `json:"page"`
	PageSize int             `json:"page_size"`
	Items    []ShareListItem `json:"items"`
}

// ListSharesQuery 分享列表查询：page/page_size 缺省 1/20
type ListSharesQuery struct {
	Page     int `form:"page"`
	PageSize int `form:"page_size"`
}
