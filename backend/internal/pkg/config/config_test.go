package config_test

import (
	"testing"

	"cloudbox/internal/pkg/config"
)

// 默认值：不设环境变量时读到 yaml/默认值
func TestLoadDefaults(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load 失败: %v", err)
	}
	if cfg.App.Port != 8080 {
		t.Fatalf("App.Port = %d, 期望 8080", cfg.App.Port)
	}
	if cfg.Redis.Addr != "127.0.0.1:6380" {
		t.Fatalf("Redis.Addr = %q, 期望 127.0.0.1:6380", cfg.Redis.Addr)
	}
}

// 环境变量覆盖：APP_PORT 能盖掉 yaml 默认值
func TestLoadEnvOverride(t *testing.T) {
	t.Setenv("APP_PORT", "9090")
	t.Setenv("CLOUDBOX_JWT_SECRET", "test-secret")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load 失败: %v", err)
	}
	if cfg.App.Port != 9090 {
		t.Fatalf("App.Port = %d, 期望 9090 (环境变量覆盖)", cfg.App.Port)
	}
	if cfg.JWT.Secret != "test-secret" {
		t.Fatalf("JWT.Secret = %q, 期望 test-secret (环境变量覆盖)", cfg.JWT.Secret)
	}
}

// 主变量优先：CLOUDBOX_JWT_SECRET 盖掉兼容变量 JWT_SECRET
func TestLoadJWTSecretPriority(t *testing.T) {
	t.Setenv("CLOUDBOX_JWT_SECRET", "primary-secret")
	t.Setenv("JWT_SECRET", "legacy-secret")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load 失败: %v", err)
	}
	if cfg.JWT.Secret != "primary-secret" {
		t.Fatalf("JWT.Secret = %q, 期望 primary-secret (主变量优先)", cfg.JWT.Secret)
	}
}

// prod 兜底：仍用开发默认密钥直接拒绝启动
func TestLoadProdRejectsDevSecret(t *testing.T) {
	t.Setenv("APP_ENV", "prod")
	t.Setenv("CLOUDBOX_JWT_SECRET", "")
	t.Setenv("JWT_SECRET", "")
	if _, err := config.Load(); err == nil {
		t.Fatalf("prod 用默认密钥应报错")
	}
}
