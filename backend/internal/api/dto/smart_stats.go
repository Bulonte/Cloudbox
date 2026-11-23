package dto

// SmartStatsQuery 仪表盘聚合查询：days 缺省 14，上限 30（归一放 Service，Bind 失败才 400）
type SmartStatsQuery struct {
	Days int `form:"days"`
}

// StorageTypeItem 按文件类型聚合行：type 冻结 6 项 image/video/audio/document/archive/other
type StorageTypeItem struct {
	Type  string `json:"type"`
	Bytes uint64 `json:"bytes"`
	Count int64  `json:"count"`
}

// StorageBlock 存储概览：used/quota 取 user 行（配额口径），by_type 只做占比
type StorageBlock struct {
	UsedBytes  uint64            `json:"used_bytes"`
	QuotaBytes uint64            `json:"quota_bytes"`
	FileCount  int64             `json:"file_count"`
	ByType     []StorageTypeItem `json:"by_type"`
}

// TimelinePoint 上传时间线点：date 为 YYYY-MM-DD，空日期后端补零不断线
type TimelinePoint struct {
	Date  string `json:"date"`
	Count int64  `json:"count"`
	Bytes uint64 `json:"bytes"`
}

// CategoryItem 分类分布行：含 9 大类 + "待确认"（空串归一，不透 confidence）
type CategoryItem struct {
	Category string `json:"category"`
	Count    int64  `json:"count"`
}

// DuplicateItem 重复报告行：kind=exact/similar；exact 带 saved_bytes，similar 为 0
type DuplicateItem struct {
	Kind       string   `json:"kind"`
	FileIDs    []uint64 `json:"file_ids"`
	FileNames  []string `json:"file_names"`
	Count      int      `json:"count"`
	SavedBytes uint64   `json:"saved_bytes"`
}

// DuplicatesBlock 重复检测报告：计数 + 明细（items 上限 20，恒为数组）
type DuplicatesBlock struct {
	ExactGroups     int             `json:"exact_groups"`
	ExactFiles      int             `json:"exact_files"`
	ExactSavedBytes uint64          `json:"exact_saved_bytes"`
	SimilarGroups   int             `json:"similar_groups"`
	SimilarFiles    int             `json:"similar_files"`
	Items           []DuplicateItem `json:"items"`
}

// SmartStatsResponse 仪表盘聚合回显：四块恒存在，数组恒为 [] 非 null
type SmartStatsResponse struct {
	Storage    StorageBlock    `json:"storage"`
	Timeline   []TimelinePoint `json:"timeline"`
	Category   []CategoryItem  `json:"category"`
	Duplicates DuplicatesBlock `json:"duplicates"`
}
