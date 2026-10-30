package api

import (
	"github.com/gin-gonic/gin"

	"cloudbox/internal/api/handler"
	"cloudbox/internal/api/middleware"
	"cloudbox/internal/pkg/jwt"
)

// SetupRouter 注册所有路由（无依赖：/health 健康检查，测试与本地验活用）
func SetupRouter() *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery())
	r.GET("/health", handler.Health)
	return r
}

// RegisterAuthRoutes 注册认证路由组（需外部注入已接好 DB 的 handler + JWT 鉴权依赖）
// S2 注册 POST /register；S3 追加 POST /login；S5 追加 POST /refresh + POST /logout(需登录)
func RegisterAuthRoutes(r *gin.Engine, h *handler.UserHandler, mgr *jwt.Manager, checker middleware.RevokedChecker) {
	auth := r.Group("/api/v1/auth")
	auth.POST("/register", h.Register)
	auth.POST("/login", h.Login)
	auth.POST("/refresh", h.Refresh)
	auth.POST("/logout", middleware.AuthRequired(mgr, checker), h.Logout)
}

// RegisterUserRoutes 注册需登录的用户路由组（S4：GET /me 接线 AuthRequired；S5：PUT /password）
func RegisterUserRoutes(r *gin.Engine, h *handler.UserHandler, mgr *jwt.Manager, checker middleware.RevokedChecker) {
	users := r.Group("/api/v1/users", middleware.AuthRequired(mgr, checker))
	users.GET("/me", h.Me)
	users.PUT("/password", h.ChangePassword)
}

// RegisterAdminRoutes 注册管理演示路由组（S6：纵向越权回归，W9 真管理接口复用同一条链）
func RegisterAdminRoutes(r *gin.Engine, mgr *jwt.Manager, checker middleware.RevokedChecker) {
	admin := r.Group("/api/v1/admin", middleware.AuthRequired(mgr, checker), middleware.AdminOnly())
	admin.GET("/ping", func(c *gin.Context) {
		c.JSON(200, gin.H{"code": 0, "message": "success", "data": gin.H{"role": c.GetString(middleware.CtxRole)}})
	})
}
