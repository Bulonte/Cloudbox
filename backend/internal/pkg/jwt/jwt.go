package jwt

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	gjwt "github.com/golang-jwt/jwt/v5"
)

// 吊销 key 约定：jwt:access:<jti>，value 任意，存在即已吊销/登出
const RevokedKeyPrefix = "jwt:access:"

// Token 类型：access 才能过 AuthRequired；refresh 只能换 token，不能当访问凭证
const (
	TokenTypeAccess  = "access"
	TokenTypeRefresh = "refresh"
)

// Claims CloudBox JWT claims
// Access 携带 UserID+Role+JTI+typ=access；Refresh 仅 UserID+JTI+typ=refresh（Role 为空）
type Claims struct {
	UserID    uint64 `json:"uid"`
	Role      string `json:"role,omitempty"`
	JTI       string `json:"jti"`
	TokenType string `json:"typ"`
	gjwt.RegisteredClaims
}

// Manager token 签发与解析
type Manager struct {
	secret     []byte
	issuer     string
	accessTTL  time.Duration
	refreshTTL time.Duration
}

// NewManager 新建；secret 不足 32 字节直接报错，逼生产配真密钥
func NewManager(secret, issuer string, accessTTL, refreshTTL time.Duration) (*Manager, error) {
	if len([]byte(secret)) < 32 {
		return nil, errors.New("jwt secret 长度不足 32 字节")
	}
	if accessTTL <= 0 || refreshTTL <= 0 {
		return nil, errors.New("jwt 过期时间必须为正数")
	}
	return &Manager{secret: []byte(secret), issuer: issuer, accessTTL: accessTTL, refreshTTL: refreshTTL}, nil
}

// nowUTC 统一用 UTC，避免时区坑
func nowUTC() time.Time { return time.Now().UTC() }

// newJTI 每次签发随机生成，吊销就靠它
func newJTI() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("生成 jti: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// GenerateAccess 签发访问 token，返回 token 与 jti
func (m *Manager) GenerateAccess(userID uint64, role string) (token, jti string, err error) {
	jti, err = newJTI()
	if err != nil {
		return "", "", err
	}
	now := nowUTC()
	c := Claims{
		UserID:    userID,
		Role:      role,
		JTI:       jti,
		TokenType: TokenTypeAccess,
		RegisteredClaims: gjwt.RegisteredClaims{
			ExpiresAt: gjwt.NewNumericDate(now.Add(m.accessTTL)),
			IssuedAt:  gjwt.NewNumericDate(now),
			NotBefore: gjwt.NewNumericDate(now),
			Issuer:    m.issuer,
		},
	}
	t := gjwt.NewWithClaims(gjwt.SigningMethodHS256, c)
	s, err := t.SignedString(m.secret)
	if err != nil {
		return "", "", fmt.Errorf("签发 access token: %w", err)
	}
	return s, jti, nil
}

// GenerateRefresh 签发刷新 token，仅存 sub(jti)，返回 token 与 jti
func (m *Manager) GenerateRefresh(userID uint64) (token, jti string, err error) {
	jti, err = newJTI()
	if err != nil {
		return "", "", err
	}
	now := nowUTC()
	c := Claims{
		UserID:    userID,
		JTI:       jti,
		TokenType: TokenTypeRefresh,
		RegisteredClaims: gjwt.RegisteredClaims{
			ExpiresAt: gjwt.NewNumericDate(now.Add(m.refreshTTL)),
			IssuedAt:  gjwt.NewNumericDate(now),
			NotBefore: gjwt.NewNumericDate(now),
			Issuer:    m.issuer,
		},
	}
	t := gjwt.NewWithClaims(gjwt.SigningMethodHS256, c)
	s, err := t.SignedString(m.secret)
	if err != nil {
		return "", "", fmt.Errorf("签发 refresh token: %w", err)
	}
	return s, jti, nil
}

// Parse 解析并校验：算法必须 HS256，exp/iat/nbf/iss 全检；不碰 Redis
// 注意：Parse 不鉴别 typ，仅做签名与有效期校验；鉴权请用 ParseAccess
func (m *Manager) Parse(tokenString string) (*Claims, error) {
	c := &Claims{}
	t, err := gjwt.ParseWithClaims(tokenString, c, func(t *gjwt.Token) (any, error) {
		if t.Method.Alg() != gjwt.SigningMethodHS256.Alg() {
			return nil, fmt.Errorf("不支持的签名算法: %s", t.Method.Alg())
		}
		return m.secret, nil
	}, gjwt.WithIssuer(m.issuer), gjwt.WithExpirationRequired(), gjwt.WithIssuedAt())
	if err != nil {
		return nil, fmt.Errorf("解析 token: %w", err)
	}
	if !t.Valid {
		return nil, errors.New("token 无效")
	}
	return c, nil
}

// ParseAccess 解析访问 token：Parse 基础上强制 typ=access
// refresh（typ=refresh）或缺 typ 的历史 token 一律拒绝，避免 7 天 refresh 冒充 2 小时 access
func (m *Manager) ParseAccess(tokenString string) (*Claims, error) {
	c, err := m.Parse(tokenString)
	if err != nil {
		return nil, err
	}
	if c.TokenType != TokenTypeAccess {
		return nil, errors.New("非访问 token")
	}
	return c, nil
}

// RevokedKey 吊销 key：jwt:access:<jti>
func RevokedKey(jti string) string {
	return RevokedKeyPrefix + jti
}

// AccessTTL 访问 token 有效期（登录返回 expires_in 用）
func (m *Manager) AccessTTL() time.Duration {
	return m.accessTTL
}

// RefreshTTL 刷新 token 有效期（refresh 落库 TTL 用）
func (m *Manager) RefreshTTL() time.Duration {
	return m.refreshTTL
}
