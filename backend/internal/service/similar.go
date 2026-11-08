package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"

	"cloudbox/internal/repository"
	"cloudbox/internal/service/smart"
)

// 指纹算法白名单（与 0001 迁移注释 simhash/phash 对齐）
const (
	AlgorithmSimHash = "simhash"
	AlgorithmPHash   = "phash"
)

// SimilarThreshold 汉明距离相似阈值（冻结：<5 判相似，禁调）
const SimilarThreshold = 5

// SimilarDefaultLimit / SimilarMaxLimit 查询条数默认与上限
const (
	SimilarDefaultLimit = 10
	SimilarMaxLimit     = 50
)

// ErrUnsupportedAlgorithm 不支持的指纹算法（调用方包成 10001）
var ErrUnsupportedAlgorithm = errors.New("不支持的指纹算法")

// ErrInvalidFingerprint 指纹格式非法（调用方包成 10001）
var ErrInvalidFingerprint = errors.New("指纹格式非法")

// SimilarItem 单条命中
type SimilarItem struct {
	FileID      uint64 `json:"file_id"`
	FileName    string `json:"file_name"`
	Fingerprint string `json:"fingerprint"`
	Distance    int    `json:"distance"`
}

// FindSimilarOptions 查询选项
type FindSimilarOptions struct {
	ExcludeFileID uint64
	Limit         int
}

// SimilarService 相似查询服务：参数校验 + 精筛排序，不碰图片解码/分词
type SimilarService struct {
	fps   *repository.FingerprintRepository
	files *repository.FileRepository
}

// NewSimilarService 构造注入相似查询服务
func NewSimilarService(fps *repository.FingerprintRepository, files *repository.FileRepository) *SimilarService {
	return &SimilarService{fps: fps, files: files}
}

// FindSimilarByFingerprint 按指纹查（S8 上传后手里已有刚算出的指纹用这个）
// 目标指纹 0（S5 空文本语义）直接回空，不报错；空结果返回空切片非 nil
func (s *SimilarService) FindSimilarByFingerprint(ctx context.Context, ownerID uint64, algorithm, fingerprintHex string, opt FindSimilarOptions) ([]SimilarItem, error) {
	if algorithm != AlgorithmSimHash && algorithm != AlgorithmPHash {
		return nil, ErrUnsupportedAlgorithm
	}
	target, err := smart.ParseSimHash(fingerprintHex)
	if err != nil {
		return nil, ErrInvalidFingerprint
	}
	if target == 0 {
		return []SimilarItem{}, nil
	}
	limit := normalizeSimilarLimit(opt.Limit)
	cands, err := s.fps.ListCandidates(ctx, ownerID, algorithm)
	if err != nil {
		return nil, fmt.Errorf("查询相似文件: %w", err)
	}
	return filterSimilar(target, cands, opt.ExcludeFileID, limit), nil
}

// FindSimilarByFile 按文件查（已入库文件找相似用这个）
// 先经 FindByID 校验归属（他人/已删一律 ErrFileNotFound 伪装，复用 S3 口径）；源无指纹回空不报错
func (s *SimilarService) FindSimilarByFile(ctx context.Context, ownerID uint64, fileID uint64, algorithm string, limit int) ([]SimilarItem, error) {
	if algorithm != AlgorithmSimHash && algorithm != AlgorithmPHash {
		return nil, ErrUnsupportedAlgorithm
	}
	if _, err := s.files.FindByID(fileID, ownerID); err != nil {
		return nil, err
	}
	src, err := s.fps.GetByFile(ctx, fileID, algorithm)
	if err != nil {
		return nil, fmt.Errorf("查询相似文件: %w", err)
	}
	if src == nil {
		return []SimilarItem{}, nil
	}
	return s.FindSimilarByFingerprint(ctx, ownerID, algorithm, src.Fingerprint, FindSimilarOptions{ExcludeFileID: fileID, Limit: limit})
}

// normalizeSimilarLimit 条数归一（<=0→默认，>上限→上限）
func normalizeSimilarLimit(limit int) int {
	if limit <= 0 {
		return SimilarDefaultLimit
	}
	if limit > SimilarMaxLimit {
		return SimilarMaxLimit
	}
	return limit
}

// filterSimilar 精筛：Parse 失败的脏行跳过（记 warn，不炸整查询）；
// 距离 <5 且非自身则收录；按 (距离, file_id) 排序后截断
func filterSimilar(target uint64, cands []repository.FingerprintCandidate, excludeFileID uint64, limit int) []SimilarItem {
	out := make([]SimilarItem, 0, len(cands))
	for _, c := range cands {
		if c.FileID == excludeFileID {
			continue
		}
		fp, err := smart.ParseSimHash(c.Fingerprint)
		if err != nil {
			slog.Warn("指纹脏数据已跳过", "file_id", c.FileID)
			continue
		}
		d := smart.HammingDistance(target, fp)
		if d >= SimilarThreshold {
			continue
		}
		out = append(out, SimilarItem{FileID: c.FileID, FileName: c.FileName, Fingerprint: c.Fingerprint, Distance: d})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Distance != out[j].Distance {
			return out[i].Distance < out[j].Distance
		}
		return out[i].FileID < out[j].FileID
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}
