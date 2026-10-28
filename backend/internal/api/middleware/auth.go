package middleware

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"cloudbox/internal/pkg/jwt"
	"cloudbox/internal/pkg/redis"
	"cloudbox/internal/pkg/response"
)

// 上下文 key：userID / role / jti
const (
	CtxUserID = "userID"
	CtxRole   = "role"
	CtxJTI    = "jti"
)

// RevokedChecker 吊销查询：命中（err==nil）表示已吊销/登出
// auth 中间件只依赖这个小接口，测试时可 fake，不必真连 Redis
type RevokedChecker interface {
	IsRevoked(ctx context.Context, jti string) (bool, error)
}

// RedisRevokedChecker 生产实现：查 jwt:access:<jti>
type RedisRevokedChecker struct {
	RDB *redis.Client
}

// IsRevoked key 存在=已吊销；不存在=正常；Redis 挂了返回 error 走 9xxxx
func (c *RedisRevokedChecker) IsRevoked(ctx context.Context, jti string) (bool, error) {
	_, err := c.RDB.Get(ctx, jwt.RevokedKey(jti))
	if err == nil {
		return true, nil
	}
	if redis.IsNil(err) {
		return false, nil
	}
	return false, err
}

// AuthRequired 鉴权中间件：粗粒度只做三件事
// 1. Authorization: Bearer <token> 格式；2. Parse 校验；3. Redis 查吊销
// 通过后注入 userID/role/jti；失败一律 Fail，不泄具体原因给前端细节
func AuthRequired(mgr *jwt.Manager, checker RevokedChecker) gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.GetHeader("Authorization")
		parts := strings.SplitN(h, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") || parts[1] == "" {
			response.Fail(c, http.StatusUnauthorized, 20001, "未登录")
			c.Abort()
			return
		}
		claims, err := mgr.Parse(parts[1])
		if err != nil {
			response.Fail(c, http.StatusUnauthorized, 20002, "登录已过期")
			c.Abort()
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		revoked, err := checker.IsRevoked(ctx, claims.JTI)
		cancel()
		if err != nil {
			response.Fail(c, http.StatusInternalServerError, 90001, "服务器内部错误")
			c.Abort()
			return
		}
		if revoked {
			response.Fail(c, http.StatusUnauthorized, 20002, "登录已过期")
			c.Abort()
			return
		}
		c.Set(CtxUserID, claims.UserID)
		c.Set(CtxRole, claims.Role)
		c.Set(CtxJTI, claims.JTI)
		c.Next()
	}
}

// AdminOnly 粗检：role==admin 才放行，W3-3.5 RBAC 用
func AdminOnly() gin.HandlerFunc {
	return func(c *gin.Context) {
		role, _ := c.Get(CtxRole)
		if r, ok := role.(string); !ok || r != "admin" {
			response.Fail(c, http.StatusForbidden, 20003, "无权限")
			c.Abort()
			return
		}
		c.Next()
	}
}
