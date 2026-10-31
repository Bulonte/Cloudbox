package redis

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// Client Redis 薄封装：只给原语，业务 key 名由 service 层定
type Client struct {
	rdb *goredis.Client
}

// NewClient 新建客户端（不做 Ping，连通性由调用方/Ping 验证）
func NewClient(addr, password string, db int) *Client {
	rdb := goredis.NewClient(&goredis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	})
	return &Client{rdb: rdb}
}

// Close 关闭连接
func (c *Client) Close() error {
	return c.rdb.Close()
}

// Ping 连通性检查
func (c *Client) Ping(ctx context.Context) error {
	return c.rdb.Ping(ctx).Err()
}

// Set 写入（ttl<=0 表示无过期）
func (c *Client) Set(ctx context.Context, key string, val any, ttl time.Duration) error {
	return c.rdb.Set(ctx, key, val, ttl).Err()
}

// Get 读取；key 不存在时返回 goredis.Nil
func (c *Client) Get(ctx context.Context, key string) (string, error) {
	return c.rdb.Get(ctx, key).Result()
}

// Del 删除
func (c *Client) Del(ctx context.Context, keys ...string) error {
	return c.rdb.Del(ctx, keys...).Err()
}

// Incr 计数器+1（登录限流用）；key 不存在时从 1 开始
func (c *Client) Incr(ctx context.Context, key string) (int64, error) {
	return c.rdb.Incr(ctx, key).Result()
}

// Expire 设置过期（登录限流窗口用）
func (c *Client) Expire(ctx context.Context, key string, ttl time.Duration) error {
	return c.rdb.Expire(ctx, key, ttl).Err()
}

// IsNil key 不存在（goredis.Nil），供调用方区分"无值"与"真错误"
func IsNil(err error) bool {
	return err == goredis.Nil
}

// NewToken 生成随机 token（锁 value / jti 用）
func NewToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// AcquireLock 分布式锁：SET NX EX；成功返回 token
// 调用方必须 defer ReleaseLock，否则 ttl 到期才释放
func (c *Client) AcquireLock(ctx context.Context, key string, ttl time.Duration) (string, bool, error) {
	token, err := NewToken()
	if err != nil {
		return "", false, err
	}
	ok, err := c.rdb.SetNX(ctx, key, token, ttl).Result()
	if err != nil {
		return "", false, err
	}
	if !ok {
		return "", false, nil
	}
	return token, true, nil
}

// releaseScript 比对 token 后删除，防误删别人的锁
var releaseScript = goredis.NewScript(`
if redis.call("get", KEYS[1]) == ARGV[1] then
  return redis.call("del", KEYS[1])
else
  return 0
end`)

// ReleaseLock 释放锁：token 对上才删；返回是否删掉
func (c *Client) ReleaseLock(ctx context.Context, key, token string) (bool, error) {
	n, err := releaseScript.Run(ctx, c.rdb, []string{key}, token).Int()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}
