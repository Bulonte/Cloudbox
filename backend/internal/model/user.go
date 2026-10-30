package model

import "time"

// 角色常量（规范 §6.2）：禁止散落字符串字面量；super_admin 预留 W9 管理端用
const (
	RoleUser       = "user"
	RoleAdmin      = "admin"
	RoleSuperAdmin = "super_admin"
)

// User 用户表
type User struct {
	ID           uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	Username     string    `gorm:"size:64;uniqueIndex:uk_username;not null" json:"username"`
	Email        string    `gorm:"size:128;uniqueIndex:uk_email;not null" json:"email"`
	PasswordHash string    `gorm:"size:255;not null" json:"-"`
	Role         string    `gorm:"size:16;default:user;not null" json:"role"`
	QuotaBytes   uint64    `gorm:"default:10737418240;not null" json:"quota_bytes"`
	UsedBytes    uint64    `gorm:"default:0;not null" json:"used_bytes"`
	Status       int8      `gorm:"default:1;not null" json:"status"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func (User) TableName() string { return "user" }

// IsAdmin 管理员放行：admin 与预留的 super_admin 均可过 AdminOnly
func (u *User) IsAdmin() bool {
	return u.Role == RoleAdmin || u.Role == RoleSuperAdmin
}
