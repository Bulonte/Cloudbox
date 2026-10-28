package redis_test

import (
	"context"
	"testing"
	"time"

	"cloudbox/internal/pkg/redis"
)

// 直连真实容器 127.0.0.1:6380；拨不通就 Skip（不引 mock 依赖）
func mustClient(t *testing.T) (*redis.Client, context.Context) {
	t.Helper()
	c := redis.NewClient("127.0.0.1:6380", "", 0)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	t.Cleanup(cancel)
	if err := c.Ping(ctx); err != nil {
		t.Skipf("Redis 未启动（127.0.0.1:6380 Ping 失败）: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c, ctx
}

func TestPing(t *testing.T) {
	c, ctx := mustClient(t)
	if err := c.Ping(ctx); err != nil {
		t.Fatalf("Ping 失败: %v", err)
	}
}

func TestSetGetDel(t *testing.T) {
	c, ctx := mustClient(t)
	key := "cb:test:d:setget"
	_ = c.Del(ctx, key)

	if err := c.Set(ctx, key, "hello", time.Minute); err != nil {
		t.Fatalf("Set 失败: %v", err)
	}
	got, err := c.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get 失败: %v", err)
	}
	if got != "hello" {
		t.Fatalf("Get = %q, 期望 hello", got)
	}
	if err := c.Del(ctx, key); err != nil {
		t.Fatalf("Del 失败: %v", err)
	}
	if _, err := c.Get(ctx, key); !redis.IsNil(err) {
		t.Fatalf("删除后 Get 应返回 Nil, 得到 err=%v", err)
	}
}

func TestLockAcquireRelease(t *testing.T) {
	c, ctx := mustClient(t)
	key := "cb:test:d:lock"
	_ = c.Del(ctx, key)

	token, ok, err := c.AcquireLock(ctx, key, time.Minute)
	if err != nil || !ok {
		t.Fatalf("首次加锁失败: ok=%v err=%v", ok, err)
	}
	if token == "" {
		t.Fatalf("token 为空")
	}
	// 同 key 再次加锁必须失败
	_, ok2, err := c.AcquireLock(ctx, key, time.Minute)
	if err != nil {
		t.Fatalf("二次加锁出错: %v", err)
	}
	if ok2 {
		t.Fatalf("锁已被占，二次加锁应返回 false")
	}
	// 错 token 释放必须失败
	released, err := c.ReleaseLock(ctx, key, "wrong-token")
	if err != nil {
		t.Fatalf("错 token 释放出错: %v", err)
	}
	if released {
		t.Fatalf("错 token 不应释放成功")
	}
	// 对 token 释放成功
	released, err = c.ReleaseLock(ctx, key, token)
	if err != nil {
		t.Fatalf("释放出错: %v", err)
	}
	if !released {
		t.Fatalf("对 token 释放应成功")
	}
	// 释放后可重新加锁
	_, ok3, err := c.AcquireLock(ctx, key, time.Minute)
	if err != nil || !ok3 {
		t.Fatalf("释放后重新加锁失败: ok=%v err=%v", ok3, err)
	}
	_ = c.Del(ctx, key)
}
