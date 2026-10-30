package password

import (
	"errors"
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

// Cost bcrypt 代价因子，规范要求 12（PR 3.1）
const Cost = 12

// Hash 生成 bcrypt 哈希（cost=12，自带随机盐）
func Hash(plain string) (string, error) {
	if plain == "" {
		return "", errors.New("密码不能为空")
	}
	h, err := bcrypt.GenerateFromPassword([]byte(plain), Cost)
	if err != nil {
		return "", fmt.Errorf("生成密码哈希: %w", err)
	}
	return string(h), nil
}

// Verify 校验明文与哈希；哈希非法或密码不匹配一律返回 false
func Verify(plain, hash string) bool {
	if plain == "" || hash == "" {
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)) == nil
}
