package dto

// OperationLogQuery 管理员审计查询参数（全可选，零值=不限）
type OperationLogQuery struct {
	UserID    uint64 `form:"user_id"`
	Action    string `form:"action"`
	StartTime string `form:"start_time"`
	EndTime   string `form:"end_time"`
	Page      int    `form:"page"`
	PageSize  int    `form:"page_size"`
}

// OperationLogItem 管理员审计回显（脱敏：只读快照，不含敏感明文）
type OperationLogItem struct {
	ID         uint64  `json:"id"`
	UserID     uint64  `json:"user_id"`
	Action     string  `json:"action"`
	TargetType *string `json:"target_type,omitempty"`
	TargetID   *uint64 `json:"target_id,omitempty"`
	IP         *string `json:"ip,omitempty"`
	Detail     *string `json:"detail,omitempty"`
	CreatedAt  string  `json:"created_at"`
}

// OperationLogListResponse 审计分页回显（items 恒为数组）
type OperationLogListResponse struct {
	Total    int64              `json:"total"`
	Page     int                `json:"page"`
	PageSize int                `json:"page_size"`
	Items    []OperationLogItem `json:"items"`
}
