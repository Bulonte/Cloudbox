package service

import (
	"context"
	"sort"
	"strings"
	"time"

	"cloudbox/internal/repository"
	"cloudbox/internal/service/smart"
)

// SmartStatsService 仪表盘聚合：只读 5 路聚合 + 内存相似比对，不写盘不动配额
// 归属一律 SQL 层带 owner_id；days 归一 1..30，缺省 14
// S5-fix：service 禁依赖 api/dto（层级倒置），此处自有载体类型，Handler 负责转 DTO
type SmartStatsService struct {
	files *repository.FileRepository
	fps   *repository.FingerprintRepository
	users *repository.UserRepository
}

// NewSmartStatsService 构造注入文件/指纹/用户仓储
func NewSmartStatsService(files *repository.FileRepository, fps *repository.FingerprintRepository, users *repository.UserRepository) *SmartStatsService {
	return &SmartStatsService{files: files, fps: fps, users: users}
}

// StorageTypeItem 按文件类型聚合行：type 冻结 6 项 image/video/audio/document/archive/other
type StorageTypeItem struct {
	Type  string
	Bytes uint64
	Count int64
}

// StorageBlock 存储概览载体（Handler 转 dto.StorageBlock）
type StorageBlock struct {
	UsedBytes  uint64
	QuotaBytes uint64
	FileCount  int64
	ByType     []StorageTypeItem
}

// TimelinePoint 上传时间线点：date 为 YYYY-MM-DD
type TimelinePoint struct {
	Date  string
	Count int64
	Bytes uint64
}

// CategoryItem 分类分布行
type CategoryItem struct {
	Category string
	Count    int64
}

// DuplicateItem 重复报告行：kind=exact/similar
type DuplicateItem struct {
	Kind       string
	FileIDs    []uint64
	FileNames  []string
	Count      int
	SavedBytes uint64
}

// DuplicatesBlock 重复检测报告载体（Handler 转 dto.DuplicatesBlock）
type DuplicatesBlock struct {
	ExactGroups     int
	ExactFiles      int
	ExactSavedBytes uint64
	SimilarGroups   int
	SimilarFiles    int
	Items           []DuplicateItem
}

// SmartStatsResult 内部载体（Handler 转 DTO）
type SmartStatsResult struct {
	Storage    StorageBlock
	Timeline   []TimelinePoint
	Category   []CategoryItem
	Duplicates DuplicatesBlock
}

// statsDays 归一：<=0 缺省 14；>30 截 30
func statsDays(days int) int {
	if days <= 0 {
		return 14
	}
	if days > 30 {
		return 30
	}
	return days
}

// mapMimeToType mime → 展示 6 类（image/video/audio 前缀直判；其余按扩展名前缀兜底）
func mapMimeToType(mime, name string) string {
	m := strings.ToLower(strings.TrimSpace(mime))
	if strings.HasPrefix(m, "image/") {
		return "image"
	}
	if strings.HasPrefix(m, "video/") {
		return "video"
	}
	if strings.HasPrefix(m, "audio/") {
		return "audio"
	}
	if strings.HasPrefix(m, "application/zip") || strings.HasPrefix(m, "application/x-rar") ||
		strings.HasPrefix(m, "application/x-7z") || strings.HasPrefix(m, "application/gzip") ||
		strings.HasPrefix(m, "application/x-tar") {
		return "archive"
	}
	// 文本/文档兜底：pdf/doc/txt/md/csv/xls 均归 document（与分类器 extCategory 文档口径一致）
	if strings.HasPrefix(m, "text/") || strings.Contains(m, "pdf") || strings.Contains(m, "msword") ||
		strings.Contains(m, "officedocument") || strings.Contains(m, "sheet") || strings.Contains(m, "csv") {
		return "document"
	}
	// mime 缺失时按扩展名兜底（与 smart.extCategory 思路一致，image 四类优先）
	lower := strings.ToLower(strings.TrimSpace(name))
	for _, suf := range []string{".jpg", ".jpeg", ".png", ".gif", ".webp", ".bmp"} {
		if strings.HasSuffix(lower, suf) {
			return "image"
		}
	}
	for _, suf := range []string{".mp4", ".avi", ".mov", ".mkv"} {
		if strings.HasSuffix(lower, suf) {
			return "video"
		}
	}
	for _, suf := range []string{".mp3", ".wav", ".flac", ".aac"} {
		if strings.HasSuffix(lower, suf) {
			return "audio"
		}
	}
	for _, suf := range []string{".zip", ".rar", ".7z", ".tar", ".gz"} {
		if strings.HasSuffix(lower, suf) {
			return "archive"
		}
	}
	return "other"
}

// normalizeCategory 空串/未知归"待确认"，不透 confidence（与 FileResponse 口径一致）
func normalizeCategory(c string) string {
	c = strings.TrimSpace(c)
	if c == "" {
		return "待确认"
	}
	if _, ok := smart.ParseCategory(c); ok {
		return c
	}
	return "待确认"
}

// GetStats 仪表盘聚合主流程：配额行 + 4 类聚合 + 重复报告；顺序查 5 次（单测友好，不打爆连接）
func (s *SmartStatsService) GetStats(ownerID uint64, days int) (*SmartStatsResult, error) {
	if ownerID == 0 {
		return nil, ErrInvalidParams
	}
	days = statsDays(days)
	u, err := s.users.FindByID(ownerID)
	if err != nil {
		return nil, err
	}
	// used 取 user 行配额口径（与上传卡配额同一数；软删不释放配额，SUM(file_size) 会偏小故不用）
	_, count, err := s.files.SumSizeByOwner(ownerID)
	if err != nil {
		return nil, err
	}
	byType, err := s.storageByType(ownerID)
	if err != nil {
		return nil, err
	}
	timeline, err := s.buildTimeline(ownerID, days)
	if err != nil {
		return nil, err
	}
	category, err := s.buildCategory(ownerID)
	if err != nil {
		return nil, err
	}
	dups, err := s.buildDuplicates(ownerID)
	if err != nil {
		return nil, err
	}
	tl := timeline
	if tl == nil {
		tl = []TimelinePoint{}
	}
	cat := category
	if cat == nil {
		cat = []CategoryItem{}
	}
	return &SmartStatsResult{
		Storage: StorageBlock{
			UsedBytes:  u.UsedBytes,
			QuotaBytes: u.QuotaBytes,
			FileCount:  count,
			ByType:     byType,
		},
		Timeline:   tl,
		Category:   cat,
		Duplicates: dups,
	}, nil
}

// storageByType mime 聚合 → 6 类合并（固定顺序 image/video/audio/document/archive/other）
func (s *SmartStatsService) storageByType(ownerID uint64) ([]StorageTypeItem, error) {
	rows, err := s.files.GroupSizeByMime(ownerID)
	if err != nil {
		return nil, err
	}
	// mime 行无文件名，按 mime 主类型映射（扩展名兜底缺文件名时归 other/document 大类即可）
	agg := map[string]*StorageTypeItem{
		"image":    {Type: "image"},
		"video":    {Type: "video"},
		"audio":    {Type: "audio"},
		"document": {Type: "document"},
		"archive":  {Type: "archive"},
		"other":    {Type: "other"},
	}
	for _, r := range rows {
		t := mapMimeToType(r.MimeType, "")
		a := agg[t]
		if r.Bytes > 0 {
			a.Bytes += uint64(r.Bytes)
		}
		a.Count += r.Count
	}
	order := []string{"image", "video", "audio", "document", "archive", "other"}
	out := make([]StorageTypeItem, 0, len(order))
	for _, k := range order {
		out = append(out, *agg[k])
	}
	return out, nil
}

// buildTimeline 最近 days 天补零（date ASC，空库全零线）
func (s *SmartStatsService) buildTimeline(ownerID uint64, days int) ([]TimelinePoint, error) {
	now := time.Now()
	day0 := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	since := day0.AddDate(0, 0, -(days - 1))
	rows, err := s.files.GroupDaily(ownerID, since)
	if err != nil {
		return nil, err
	}
	byDate := make(map[string]TimelinePoint, len(rows))
	for _, r := range rows {
		var b uint64
		if r.Bytes > 0 {
			b = uint64(r.Bytes)
		}
		byDate[r.Date] = TimelinePoint{Date: r.Date, Count: r.Count, Bytes: b}
	}
	out := make([]TimelinePoint, 0, days)
	for i := 0; i < days; i++ {
		d := since.AddDate(0, 0, i).Format("2006-01-02")
		if p, ok := byDate[d]; ok {
			out = append(out, p)
			continue
		}
		out = append(out, TimelinePoint{Date: d, Count: 0, Bytes: 0})
	}
	return out, nil
}

// buildCategory 9 大类 + 待确认（count DESC 已由仓储排，归一后保持稳定排序）
func (s *SmartStatsService) buildCategory(ownerID uint64) ([]CategoryItem, error) {
	rows, err := s.files.GroupCountByCategory(ownerID)
	if err != nil {
		return nil, err
	}
	agg := make(map[string]int64, len(rows))
	for _, r := range rows {
		agg[normalizeCategory(r.Category)] += r.Count
	}
	out := make([]CategoryItem, 0, len(agg))
	for k, v := range agg {
		out = append(out, CategoryItem{Category: k, Count: v})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Category < out[j].Category
	})
	if out == nil {
		out = []CategoryItem{}
	}
	return out, nil
}

// similarPair 相似判定：同算法 + 非零 + 汉明距离 <5（阈值冻结，文献依据）
func similarPair(algo string, a, b uint64) bool {
	if a == 0 || b == 0 {
		return false
	}
	if algo != "simhash" && algo != "phash" {
		return false
	}
	return smart.HammingDistance(a, b) < 5
}

// buildDuplicates 精确组（SQL，同 hash；uk_hash 全局唯一故本人内恒为 0，保留口径防回归）
// + 相似组（simhash/phash 指纹 Hamming<5，500 截断内内存两两比对，最多 20 明细）
func (s *SmartStatsService) buildDuplicates(ownerID uint64) (DuplicatesBlock, error) {
	block := DuplicatesBlock{Items: []DuplicateItem{}}
	groups, err := s.files.ExactDuplicateGroups(ownerID)
	if err != nil {
		return block, err
	}
	for _, g := range groups {
		rows, err := s.files.ListFilesByHash(ownerID, g.FileHash, 10)
		if err != nil {
			return block, err
		}
		ids := make([]uint64, 0, len(rows))
		names := make([]string, 0, len(rows))
		for _, r := range rows {
			ids = append(ids, r.ID)
			names = append(names, r.FileName)
		}
		n := int(g.Count)
		if n < 2 {
			continue
		}
		var saved uint64
		if n > 1 && g.Size > 0 {
			saved = uint64(n-1) * uint64(g.Size)
		}
		block.ExactGroups++
		block.ExactFiles += n
		block.ExactSavedBytes += saved
		if len(block.Items) < 20 {
			block.Items = append(block.Items, DuplicateItem{
				Kind:       "exact",
				FileIDs:    ids,
				FileNames:  names,
				Count:      n,
				SavedBytes: saved,
			})
		}
	}
	// 相似组：simhash/phash 分算法粗筛 → 截断 500 → 两两 Hamming<5 组对；明细取 similar 行（saved=0）
	for _, algo := range []string{"simhash", "phash"} {
		sets := s.similarSets(ownerID, algo)
		block.SimilarGroups += len(sets)
		for _, set := range sets {
			block.SimilarFiles += len(set)
			if len(block.Items) >= 20 {
				continue
			}
			ids := make([]uint64, 0, len(set))
			names := make([]string, 0, len(set))
			for _, m := range set {
				ids = append(ids, m.FileID)
				names = append(names, m.FileName)
			}
			block.Items = append(block.Items, DuplicateItem{
				Kind:       "similar",
				FileIDs:    ids,
				FileNames:  names,
				Count:      len(set),
				SavedBytes: 0,
			})
		}
	}
	return block, nil
}

// similarSets 单算法相似集合：候选截断 500，O(n²) 有界；组按并查集收敛，只回成员数>1 的组
func (s *SmartStatsService) similarSets(ownerID uint64, algo string) [][]repository.FingerprintCandidate {
	cands, err := s.fps.ListCandidates(context.Background(), ownerID, algo)
	if err != nil || len(cands) < 2 {
		return nil
	}
	if len(cands) > 500 {
		cands = cands[:500]
	}
	type val struct {
		v uint64
		c repository.FingerprintCandidate
	}
	vals := make([]val, 0, len(cands))
	for _, c := range cands {
		v, err := smart.ParseSimHash(strings.TrimSpace(c.Fingerprint))
		if err != nil || v == 0 {
			continue
		}
		vals = append(vals, val{v: v, c: c})
	}
	if len(vals) < 2 {
		return nil
	}
	parent := make([]int, len(vals))
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(x int) int {
		for parent[x] != x {
			parent[x] = parent[parent[x]]
			x = parent[x]
		}
		return x
	}
	for i := 0; i < len(vals); i++ {
		for j := i + 1; j < len(vals); j++ {
			if similarPair(algo, vals[i].v, vals[j].v) {
				ra, rb := find(i), find(j)
				if ra != rb {
					parent[ra] = rb
				}
			}
		}
	}
	byRoot := make(map[int][]repository.FingerprintCandidate)
	for i, v := range vals {
		r := find(i)
		byRoot[r] = append(byRoot[r], v.c)
	}
	out := make([][]repository.FingerprintCandidate, 0, len(byRoot))
	for _, set := range byRoot {
		if len(set) > 1 {
			out = append(out, set)
		}
	}
	return out
}

// countSimilarGroups 单算法相似组数（similarSets 的计数投影，保留供单测/排障）
func (s *SmartStatsService) countSimilarGroups(ownerID uint64, algo string) (int, int) {
	sets := s.similarSets(ownerID, algo)
	nGroups, nFiles := 0, 0
	for _, set := range sets {
		nGroups++
		nFiles += len(set)
	}
	return nGroups, nFiles
}
