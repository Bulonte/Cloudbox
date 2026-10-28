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
	t.Setenv("JWT_SECRET", "test-secret")

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
