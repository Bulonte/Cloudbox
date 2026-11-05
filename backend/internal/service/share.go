package service

import (
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"cloudbox/internal/model"
	"cloudbox/internal/pkg/password"
	"cloudbox/internal/repository"
)

// 分享生成常量：16 位 Base62；默认 7 天；上限 30 天；碰撞重试 5 次
const (
	ShareCodeLength    = 16
	base62Alphabet     = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	defaultExpireDays  = 7
	maxExpireDays      = 30
	maxCodeGenAttempts = 5
)

// CreateShareInput 创建分享入参：密码/过期/次数均可选
type CreateShareInput struct {
	OwnerID      uint64
	FileID       uint64
	Password     string
	ExpireAt     *time.Time
	MaxDownloads *int32
}

// ShareService 分享业务：归属 SQL 层判，参数规则放这里
type ShareService struct {
	shares  *repository.ShareRepository
	files   *repository.FileRepository
	codeGen func() (string, error)
}

// NewShareService 构造注入分享+文件仓储
func NewShareService(shares *repository.ShareRepository, files *repository.FileRepository) *ShareService {
	return &ShareService{shares: shares, files: files, codeGen: generateShareCode}
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
func (s *ShareService) CreateShare(in CreateShareInput) (*model.FileShare, error) {
	if in.OwnerID == 0 || in.FileID == 0 {
		return nil, fmt.Errorf("%w: 分享人与文件非法", ErrInvalidParams)
	}
	if _, err := s.files.FindByID(in.FileID, in.OwnerID); err != nil {
		return nil, err
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
