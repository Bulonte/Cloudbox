package password_test

import (
	"strings"
	"testing"

	"cloudbox/internal/pkg/password"
)

const plain = "Abc123456!"

func TestHashAndVerify(t *testing.T) {
	hash, err := password.Hash(plain)
	if err != nil {
		t.Fatalf("Hash 失败: %v", err)
	}
	if hash == "" || hash == plain {
		t.Fatalf("哈希结果不合法: %q", hash)
	}
	if !strings.HasPrefix(hash, "$2a$12$") && !strings.HasPrefix(hash, "$2b$12$") {
		t.Fatalf("hash 未使用 cost=12: %q", hash)
	}
	if !password.Verify(plain, hash) {
		t.Fatal("正确密码应校验通过")
	}
	if password.Verify("wrong-password", hash) {
		t.Fatal("错误密码应校验失败")
	}
}

func TestHashIsSalted(t *testing.T) {
	h1, err := password.Hash(plain)
	if err != nil {
		t.Fatalf("Hash 失败: %v", err)
	}
	h2, err := password.Hash(plain)
	if err != nil {
		t.Fatalf("Hash 失败: %v", err)
	}
	if h1 == h2 {
		t.Fatal("同一密码两次哈希应不同（含随机盐）")
	}
	if !password.Verify(plain, h1) || !password.Verify(plain, h2) {
		t.Fatal("两个哈希都应能校验通过")
	}
}

func TestVerifyMalformedHash(t *testing.T) {
	if password.Verify(plain, "not-a-hash") {
		t.Fatal("非法哈希应校验失败")
	}
	if password.Verify(plain, "") {
		t.Fatal("空哈希应校验失败")
	}
}

func TestCost(t *testing.T) {
	if password.Cost != 12 {
		t.Fatalf("Cost = %d, 期望 12（规范 §5.1）", password.Cost)
	}
}
