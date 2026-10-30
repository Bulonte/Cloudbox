package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"cloudbox/internal/api/dto"
	"cloudbox/internal/pkg/response"
	"cloudbox/internal/service"
)

// 用户错误码：10001 参数错误；20004 用户名或邮箱已存在；90001 服务器内部错误
const (
	codeInvalidParams = 10001
	codeUserConflict  = 20004
	codeInternal      = 90001
)

// UserHandler 用户 HTTP 层：只做 Bind/校验/错误码映射，不直接碰仓储
type UserHandler struct {
	svc *service.UserService
}

// NewUserHandler 构造注入 service
func NewUserHandler(svc *service.UserService) *UserHandler {
	return &UserHandler{svc: svc}
}

// Register POST /api/v1/auth/register：注册成功 200 + code 0 + 脱敏用户
func (h *UserHandler) Register(c *gin.Context) {
	var req dto.RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, codeInvalidParams, "请求参数不正确")
		return
	}
	u, err := h.svc.Register(service.RegisterInput{
		Username: req.Username,
		Email:    req.Email,
		Password: req.Password,
	})
	switch {
	case err == nil:
		response.OK(c, dto.ToUserResponse(u))
	case errors.Is(err, service.ErrInvalidParams):
		response.Fail(c, http.StatusBadRequest, codeInvalidParams, err.Error())
	case errors.Is(err, service.ErrUserConflict):
		response.Fail(c, http.StatusConflict, codeUserConflict, "用户名或邮箱已存在")
	default:
		response.Fail(c, http.StatusInternalServerError, codeInternal, "服务器内部错误")
	}
}
