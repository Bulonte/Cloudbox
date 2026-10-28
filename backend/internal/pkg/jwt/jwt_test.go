package jwt_test

import (
	"strings"
	"testing"
	"time"

	gjwt "github.com/golang-jwt/jwt/v5"

	"cloudbox/internal/pkg/jwt"
)

const testSecret = "test-secret-please-override-32bytes!!" // 37 字节，仅测试用

func mustManager(t *testing.T) *jwt.Manager {
	t.Helper()
	m, err := jwt.NewManager(testSecret, "cloudbox", 2*time.Hour, 168*time.Hour)
	if err != nil {
		t.Fatalf("NewManager 失败: %v", err)
	}
	return m
}

func TestNewManagerSecretTooShort(t *testing.T) {
	if _, err := jwt.NewManager("short", "cloudbox", time.Hour, time.Hour); err == nil {
		t.Fatalf("短 secret 应报错")
	}
}

func TestGenerateParseAccess(t *testing.T) {
	m := mustManager(t)
	token, jti, err := m.GenerateAccess(1001, "user")
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}
	if jti == "" {
		t.Fatalf("jti 为空")
	}
	c, err := m.Parse(token)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if c.UserID != 1001 || c.Role != "user" || c.JTI != jti {
		t.Fatalf("claims 不对: %+v", c)
	}
	if c.Issuer != "cloudbox" {
		t.Fatalf("Issuer = %q, 期望 cloudbox", c.Issuer)
	}
}

func TestGenerateRefreshHasNoRole(t *testing.T) {
	m := mustManager(t)
	token, _, err := m.GenerateRefresh(1001)
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}
	c, err := m.Parse(token)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if c.UserID != 1001 {
		t.Fatalf("UserID = %d, 期望 1001", c.UserID)
	}
	if c.Role != "" {
		t.Fatalf("refresh 不应带 role, 得到 %q", c.Role)
	}
}

func TestParseExpiredRejected(t *testing.T) {
	m, err := jwt.NewManager(testSecret, "cloudbox", -time.Hour, -time.Hour)
	if err == nil {
		t.Fatalf("负 TTL 应报错")
	}
	// 构造已过期 token：直接签一个 1 秒有效期的等它过期太慢，用负偏移 claims 手签
	_ = m
	now := time.Now().UTC().Add(-2 * time.Hour)
	claims := gjwt.MapClaims{
		"uid": 1001,
		"jti": "expired-jti",
		"exp": gjwt.NewNumericDate(now.Add(-time.Hour)).Unix(),
		"iat": gjwt.NewNumericDate(now).Unix(),
		"nbf": gjwt.NewNumericDate(now).Unix(),
		"iss": "cloudbox",
	}
	tk := gjwt.NewWithClaims(gjwt.SigningMethodHS256, claims)
	s, err := tk.SignedString([]byte(testSecret))
	if err != nil {
		t.Fatalf("手签失败: %v", err)
	}
	m2 := mustManager(t)
	if _, err := m2.Parse(s); err == nil {
		t.Fatalf("过期 token 应被拒绝")
	}
}

func TestParseAlgNoneRejected(t *testing.T) {
	// alg:none 手工拼一个无签名 token
	header := "eyJhbGciOiJub25lIiwidHlwIjoiSldUIn0"
	payload := "eyJ1aWQiOjEwMDEsImp0aSI6IngiLCJpc3MiOiJjbG91ZGJveCJ9"
	noneToken := header + "." + payload + "."
	m := mustManager(t)
	if _, err := m.Parse(noneToken); err == nil {
		t.Fatalf("alg:none 必须被拒绝")
	}
}

func TestParseWrongSecretRejected(t *testing.T) {
	m := mustManager(t)
	token, _, err := m.GenerateAccess(1, "user")
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}
	other, err := jwt.NewManager(strings.Repeat("x", 32), "cloudbox", 2*time.Hour, 168*time.Hour)
	if err != nil {
		t.Fatalf("NewManager 失败: %v", err)
	}
	if _, err := other.Parse(token); err == nil {
		t.Fatalf("错密钥签名必须被拒绝")
	}
}

func TestRevokedKey(t *testing.T) {
	if got := jwt.RevokedKey("abc"); got != "jwt:access:abc" {
		t.Fatalf("RevokedKey = %q, 期望 jwt:access:abc", got)
	}
}
