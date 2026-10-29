package response

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// 统一响应: {code, message, data}
// code=0 成功; 1xxxx 参数; 2xxxx 认证; 9xxxx 系统错误
type Body struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

func OK(c *gin.Context, data interface{}) {
	c.JSON(http.StatusOK, Body{Code: 0, Message: "success", Data: data})
}

func Fail(c *gin.Context, httpStatus, code int, msg string) {
	c.JSON(httpStatus, Body{Code: code, Message: msg})
}
