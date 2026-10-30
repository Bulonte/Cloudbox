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
