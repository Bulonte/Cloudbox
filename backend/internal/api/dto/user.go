package dto

import (
	"cloudbox/internal/model"
)

// RegisterRequest 注册请求体
type RegisterRequest struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

// UserResponse 脱敏用户返回：绝不含 PasswordHash
type UserResponse struct {
	ID         uint64 `json:"id"`
	Username   string `json:"username"`
	Email      string `json:"email"`
	Role       string `json:"role"`
	QuotaBytes uint64 `json:"quota_bytes"`
	UsedBytes  uint64 `json:"used_bytes"`
}

// ToUserResponse model 转脱敏 DTO
func ToUserResponse(u *model.User) UserResponse {
	return UserResponse{
		ID:         u.ID,
		Username:   u.Username,
		Email:      u.Email,
		Role:       u.Role,
		QuotaBytes: u.QuotaBytes,
		UsedBytes:  u.UsedBytes,
	}
}

// LoginRequest 登录请求体：Login 可为用户名或邮箱
type LoginRequest struct {
	Login    string `json:"login"`
	Password string `json:"password"`
}

// LoginResponse 登录返回：双 token + 类型 + access 有效期秒数
type LoginResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
}

// RefreshRequest 刷新请求体：凭 refresh_token 轮换新双 token
type RefreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// LogoutRequest 登出请求体：refresh_token 可空（前端清不干净也不炸）
type LogoutRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// ChangePasswordRequest 改密请求体：新密码复用注册同规则
type ChangePasswordRequest struct {
	OldPassword string `json:"old_password"`
	NewPassword string `json:"new_password"`
}
