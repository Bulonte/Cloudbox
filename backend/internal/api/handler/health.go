package handler

import (
	"github.com/gin-gonic/gin"

	"cloudbox/internal/pkg/response"
)

// Health 健康检查
func Health(c *gin.Context) {
	response.OK(c, gin.H{"status": "ok"})
}
