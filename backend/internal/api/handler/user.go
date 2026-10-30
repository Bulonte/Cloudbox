package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"cloudbox/internal/api/dto"
	"cloudbox/internal/api/middleware"
	"cloudbox/internal/pkg/response"
	"cloudbox/internal/repository"
	"cloudbox/internal/service"
)

// 用户错误码：10001 参数错误；20002 认证失败；20004 用户名或邮箱已存在；20005 登录频繁；90001 服务器内部错误
const (
	codeInvalidParams = 10001
	codeUnauthorized  = 20002
	codeUserConflict  = 20004
	codeTooMany       = 20005
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

// Login POST /api/v1/auth/login：成功 200 + code 0 + 双 token
func (h *UserHandler) Login(c *gin.Context) {
	var req dto.LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, codeInvalidParams, "请求参数不正确")
		return
	}
	pair, err := h.svc.Login(service.LoginInput{
		Login:    req.Login,
		Password: req.Password,
	})
	switch {
	case err == nil:
		response.OK(c, dto.LoginResponse{
			AccessToken:  pair.AccessToken,
			RefreshToken: pair.RefreshToken,
			TokenType:    "Bearer",
			ExpiresIn:    pair.ExpiresIn,
		})
	case errors.Is(err, service.ErrInvalidParams):
		response.Fail(c, http.StatusBadRequest, codeInvalidParams, err.Error())
	case errors.Is(err, service.ErrInvalidCredentials):
		response.Fail(c, http.StatusUnauthorized, codeUnauthorized, "账号或密码错误")
	case errors.Is(err, service.ErrTooManyAttempts):
		response.Fail(c, http.StatusTooManyRequests, codeTooMany, "登录尝试过于频繁请稍后再试")
	default:
		response.Fail(c, http.StatusInternalServerError, codeInternal, "服务器内部错误")
	}
}

// Me GET /api/v1/users/me：需 AuthRequired；token 合法但用户已被删时模糊为 20002（防枚举）
func (h *UserHandler) Me(c *gin.Context) {
	u, err := h.svc.Profile(c.GetUint64(middleware.CtxUserID))
	switch {
	case err == nil:
		response.OK(c, dto.ToUserResponse(u))
	case errors.Is(err, service.ErrInvalidParams):
		response.Fail(c, http.StatusUnauthorized, codeUnauthorized, "登录已过期")
	case errors.Is(err, repository.ErrUserNotFound):
		// 故意不暴露 404，调用方只知凭证无效
		response.Fail(c, http.StatusUnauthorized, codeUnauthorized, "登录已过期")
	default:
		response.Fail(c, http.StatusInternalServerError, codeInternal, "服务器内部错误")
	}
}
