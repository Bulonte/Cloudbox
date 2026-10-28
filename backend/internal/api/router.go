package api

import (
	"github.com/gin-gonic/gin"

	"cloudbox/internal/api/handler"
)

// SetupRouter 注册所有路由
func SetupRouter() *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery())
	r.GET("/health", handler.Health)
	return r
}
