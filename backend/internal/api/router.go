package api

import (
	"github.com/gin-gonic/gin"

	"cloudbox/internal/api/handler"
)

// SetupRouter 注册所有路由（无依赖：/health 健康检查，测试与本地验活用）
func SetupRouter() *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery())
	r.GET("/health", handler.Health)
	return r
}

// RegisterAuthRoutes 注册认证路由组（需外部注入已接好 DB 的 handler）
// S2 注册 POST /register；S3 追加 POST /login；refresh/logout 留给 S5
func RegisterAuthRoutes(r *gin.Engine, h *handler.UserHandler) {
	auth := r.Group("/api/v1/auth")
	auth.POST("/register", h.Register)
	auth.POST("/login", h.Login)
}
