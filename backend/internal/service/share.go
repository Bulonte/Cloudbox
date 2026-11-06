package service

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"cloudbox/internal/model"
	"cloudbox/internal/pkg/password"
	"cloudbox/internal/repository"
	"cloudbox/internal/service/permission"
	"cloudbox/internal/storage"
)

// 分享生成常量：16 位 Base62；默认 7 天；上限 30 天；碰撞重试 5 次
const (
	ShareCodeLength    = 16
	base62Alphabet     = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	defaultExpireDays  = 7
	maxExpireDays      = 30
	maxCodeGenAttempts = 5
)

// 分享访问限流：固定窗口 10 次/分钟/IP（S2 冻结；演示误伤只改参不改逻辑）
const (
	shareRateLimit        = 10
	shareRateWindow       = time.Minute
	shareAttemptKeyPrefix = "share:access:"
)

// CreateShareInput 创建分享入参：密码/过期/次数均可选
// S3：Role/Dept 由 handler 透传（JWT role；dept 预留，用户表暂无部门字段时传空即按 fail-closed 收口）
type CreateShareInput struct {
	OwnerID      uint64
	Role         string
	Dept         string
	FileID       uint64
	Password     string
	ExpireAt     *time.Time
	MaxDownloads *int32
}

// ShareService 分享业务：归属 SQL 层判，参数规则放这里
// S3：建链收口到 permission.Checker（需 manage；拒绝一律 ErrFileNotFound 伪装）
type ShareService struct {
	shares    *repository.ShareRepository
	files     *repository.FileRepository
	store     storage.Storage
	limiter   RedisStore
	rateLimit int
	codeGen   func() (string, error)
	checker   *permission.Checker
}

// NewShareService 构造注入分享+文件仓储
func NewShareService(shares *repository.ShareRepository, files *repository.FileRepository) *ShareService {
	return &ShareService{shares: shares, files: files, rateLimit: shareRateLimit, codeGen: generateShareCode}
}

// WithStorage 注入内容存储（S2 公开下载放流用），返回同一实例方便链式调用
func (s *ShareService) WithStorage(st storage.Storage) *ShareService {
	s.store = st
	return s
}

// WithLimiter 注入限流器（生产传 *redis.Client，原生满足 RedisStore；测试传内存 fake；nil=不限流仅单测用）
func (s *ShareService) WithLimiter(rdb RedisStore) *ShareService {
	s.limiter = rdb
	return s
}

// WithChecker 注入集中判定器（S3 建链需 manage；nil=老归属口径，仅单测兼容）
func (s *ShareService) WithChecker(c *permission.Checker) *ShareService {
	s.checker = c
	return s
}

// WithRateLimit 覆盖每分钟阈值（<=0 忽略，默认 shareRateLimit）
func (s *ShareService) WithRateLimit(n int) *ShareService {
	if n > 0 {
		s.rateLimit = n
	}
	return s
}

// generateShareCode 16 位 Base62，仅 crypto/rand，禁 math/rand
func generateShareCode() (string, error) {
	var b [ShareCodeLength]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("生成分享码: %w", err)
	}
	out := make([]byte, ShareCodeLength)
	for i, v := range b {
		out[i] = base62Alphabet[int(v)%len(base62Alphabet)]
	}
	return string(out), nil
}

// normalizeExpiry 有效期归一化：空=默认7天；过去/超30天一律参错
func normalizeExpiry(exp *time.Time, now time.Time) (sql.NullTime, error) {
	if exp == nil {
		return sql.NullTime{Time: now.AddDate(0, 0, defaultExpireDays), Valid: true}, nil
	}
	if exp.Before(now) || exp.After(now.AddDate(0, 0, maxExpireDays)) {
		return sql.NullTime{}, fmt.Errorf("%w: 有效期须在未来30天内", ErrInvalidParams)
	}
	return sql.NullTime{Time: *exp, Valid: true}, nil
}

// normalizeLimit 次数归一化：空=不限；给值须为正数
func normalizeLimit(max *int32) (sql.NullInt32, error) {
	if max == nil {
		return sql.NullInt32{}, nil
	}
	if *max <= 0 {
		return sql.NullInt32{}, fmt.Errorf("%w: 下载次数须为正数", ErrInvalidParams)
	}
	return sql.NullInt32{Int32: *max, Valid: true}, nil
}

// hashSharePassword 密码归一化：空=无密码；非空 bcrypt 落库
func hashSharePassword(pw string) (sql.NullString, error) {
	if strings.TrimSpace(pw) == "" {
		return sql.NullString{}, nil
	}
	h, err := password.Hash(pw)
	if err != nil {
		return sql.NullString{}, fmt.Errorf("%w: 密码格式非法", ErrInvalidParams)
	}
	return sql.NullString{String: h, Valid: true}, nil
}

// CreateShare 创建分享：归属校验 → 参数归一 → 碰撞重试；越权伪装 404
// S3：有 Checker 时需 manage（所有者/超管天然过；public 读不给建链）；拒绝一律 ErrFileNotFound 伪装
func (s *ShareService) CreateShare(in CreateShareInput) (*model.FileShare, error) {
	if in.OwnerID == 0 || in.FileID == 0 {
		return nil, fmt.Errorf("%w: 分享人与文件非法", ErrInvalidParams)
	}
	if s.checker == nil {
		if _, err := s.files.FindByID(in.FileID, in.OwnerID); err != nil {
			return nil, err
		}
	} else {
		f, err := s.files.FindByIDAny(in.FileID)
		if err != nil {
			return nil, err
		}
		if !s.checker.Can(in.OwnerID, in.Role, in.Dept, f, permission.ActionManage) {
			return nil, ErrFileNotFound
		}
		in.OwnerID = f.OwnerID
	}
	now := time.Now()
	exp, err := normalizeExpiry(in.ExpireAt, now)
	if err != nil {
		return nil, err
	}
	limit, err := normalizeLimit(in.MaxDownloads)
	if err != nil {
		return nil, err
	}
	pwhash, err := hashSharePassword(in.Password)
	if err != nil {
		return nil, err
	}
	for i := 0; i < maxCodeGenAttempts; i++ {
		code, err := s.codeGen()
		if err != nil {
			return nil, err
		}
		row := &model.FileShare{
			ShareCode: code, FileID: in.FileID, OwnerID: in.OwnerID,
			PasswordHash: pwhash, ExpireAt: exp, MaxDownloads: limit, Status: 1,
		}
		err = s.shares.CreateShare(row)
		if err == nil {
			return row, nil
		}
		if !isShareConflict(err) {
			return nil, err
		}
	}
	return nil, fmt.Errorf("生成分享码冲突请重试: %w", repository.ErrShareCodeConflict)
}

// isShareConflict 分享码碰撞判定
func isShareConflict(err error) bool {
	return errors.Is(err, repository.ErrShareCodeConflict)
}

// normalizeShareCode 分享码归一化：trim + 16 位 + Base62 字符集，非法一律参错
func normalizeShareCode(code string) (string, error) {
	code = strings.TrimSpace(code)
	if len(code) != ShareCodeLength {
		return "", fmt.Errorf("%w: 分享码非法", ErrInvalidParams)
	}
	for _, ch := range code {
		if !strings.ContainsRune(base62Alphabet, ch) {
			return "", fmt.Errorf("%w: 分享码非法", ErrInvalidParams)
		}
	}
	return code, nil
}

// isExpired 分享是否过期：expire_at 为空=永不过期
func isExpired(s *model.FileShare, now time.Time) bool {
	return s.ExpireAt.Valid && now.After(s.ExpireAt.Time)
}

// isExhausted 下载次数是否用尽：max 为空=不限
func isExhausted(s *model.FileShare) bool {
	return s.MaxDownloads.Valid && s.DownloadCount >= uint32(s.MaxDownloads.Int32)
}

// verifySharePassword 分享密码核验：无密直接过；有密空传/错传一律伪装 404（bcrypt 自带常量时间比较）
func verifySharePassword(s *model.FileShare, password string) error {
	if !s.PasswordHash.Valid {
		return nil
	}
	if !passwordVerify(password, s.PasswordHash.String) {
		return repository.ErrShareNotFound
	}
	return nil
}

// passwordVerify 密码比对薄封装（单测可替换思路预留，生产走 bcrypt）
func passwordVerify(plain, hash string) bool {
	return password.Verify(plain, hash)
}

// checkShareRateLimit IP 限流：固定窗口 shareRateLimit 次/分钟；limiter==nil 时跳过（老单测兼容）；
// Redis 故障直接返回内部错 fail-closed，不降级放行
func (s *ShareService) checkShareRateLimit(ip string) error {
	if s.limiter == nil {
		return nil
	}
	ip = strings.TrimSpace(ip)
	if ip == "" {
		ip = "unknown"
	}
	limit := s.rateLimit
	if limit <= 0 {
		limit = shareRateLimit
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	key := shareAttemptKeyPrefix + ip
	n, err := s.limiter.Incr(ctx, key)
	if err != nil {
		return fmt.Errorf("限流计数失败: %w", err)
	}
	if n == 1 {
		if err := s.limiter.Expire(ctx, key, shareRateWindow); err != nil {
			return fmt.Errorf("限流窗口设置失败: %w", err)
		}
	}
	if n > int64(limit) {
		return ErrTooManyAttempts
	}
	return nil
}

// AccessShare 公开访问校验编排：限流 → 查码 → 过期/超次 → 密码 → 文件归属/文件夹伪装
// 码错/取消/过期/超次/密码错/文件夹一律 repository.ErrShareNotFound（前端只见"链接无效"）
func (s *ShareService) AccessShare(code, pw, ip string) (*model.FileShare, *model.File, error) {
	code, err := normalizeShareCode(code)
	if err != nil {
		return nil, nil, err
	}
	if err := s.checkShareRateLimit(ip); err != nil {
		return nil, nil, err
	}
	share, err := s.shares.FindByCode(code)
	if err != nil {
		return nil, nil, err
	}
	if isExpired(share, time.Now()) {
		return nil, nil, repository.ErrShareNotFound
	}
	if isExhausted(share) {
		return nil, nil, repository.ErrShareNotFound
	}
	if err := verifySharePassword(share, pw); err != nil {
		return nil, nil, err
	}
	f, err := s.files.FindByID(share.FileID, share.OwnerID)
	if err != nil {
		return nil, nil, repository.ErrShareNotFound
	}
	if f.IsFolder == 1 {
		return nil, nil, repository.ErrShareNotFound
	}
	return share, f, nil
}

// DownloadShare 公开下载编排：AccessShare 全过 → 原子 +1（超次竞态转 404）→ Stat/Open 取流；调用方负责 Close
func (s *ShareService) DownloadShare(code, pw, ip string) (*model.File, io.ReadCloser, int64, error) {
	share, f, err := s.AccessShare(code, pw, ip)
	if err != nil {
		return nil, nil, 0, err
	}
	if s.store == nil {
		return nil, nil, 0, fmt.Errorf("存储未注入")
	}
	if err := s.shares.IncrDownloadCount(share.ShareCode); err != nil {
		return nil, nil, 0, err
	}
	size, err := s.store.Stat(f.FilePath)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("读取存储大小: %w", err)
	}
	rc, err := s.store.Open(f.FilePath)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("打开存储对象: %w", err)
	}
	return f, rc, size, nil
}
