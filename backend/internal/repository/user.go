package repository

import (
	"errors"
	"fmt"

	"gorm.io/gorm"

	"cloudbox/internal/model"
)

// ErrUserNotFound 用户不存在
var ErrUserNotFound = errors.New("用户不存在")

// ErrUserConflict 用户名或邮箱已被占用（对应 uk_username / uk_email）
var ErrUserConflict = errors.New("用户名或邮箱已存在")

// UserRepository 用户表专用仓储：登录查询与注册查重
type UserRepository struct {
	*BaseRepository[model.User]
	db *gorm.DB
}

// NewUserRepository 新建用户仓储
func NewUserRepository(db *gorm.DB) *UserRepository {
	return &UserRepository{
		BaseRepository: NewBaseRepository[model.User](db),
		db:             db,
	}
}

// FindByID 按主键查；查不到返回 ErrUserNotFound（/me 用）
func (r *UserRepository) FindByID(id uint64) (*model.User, error) {
	return r.findOne("id = ?", id)
}

// FindByUsername 按用户名查（登录用）；查不到返回 ErrUserNotFound
func (r *UserRepository) FindByUsername(username string) (*model.User, error) {
	return r.findOne("username = ?", username)
}

// FindByEmail 按邮箱查；查不到返回 ErrUserNotFound
func (r *UserRepository) FindByEmail(email string) (*model.User, error) {
	return r.findOne("email = ?", email)
}

// FindByLogin 登录入口：用户名或邮箱均可登录
func (r *UserRepository) FindByLogin(login string) (*model.User, error) {
	return r.findOne("username = ? OR email = ?", login, login)
}

// ExistsByUsername 用户名是否已占用
func (r *UserRepository) ExistsByUsername(username string) (bool, error) {
	return r.exists("username = ?", username)
}

// ExistsByEmail 邮箱是否已占用
func (r *UserRepository) ExistsByEmail(email string) (bool, error) {
	return r.exists("email = ?", email)
}

// CreateUser 创建用户；用户名/邮箱冲突返回 ErrUserConflict
func (r *UserRepository) CreateUser(u *model.User) error {
	if err := r.db.Create(u).Error; err != nil {
		if isUniqueViolation(err) {
			return ErrUserConflict
		}
		return fmt.Errorf("创建用户: %w", err)
	}
	return nil
}

// UpdatePasswordHash 更新密码哈希；记录不存在返回 ErrUserNotFound（改密用）
func (r *UserRepository) UpdatePasswordHash(id uint64, hash string) error {
	res := r.db.Model(&model.User{}).Where("id = ?", id).Update("password_hash", hash)
	if res.Error != nil {
		return fmt.Errorf("更新密码: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrUserNotFound
	}
	return nil
}

// findOne 条件查单条，查不到统一 ErrUserNotFound
func (r *UserRepository) findOne(cond string, args ...any) (*model.User, error) {
	var u model.User
	err := r.db.Where(cond, args...).First(&u).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("查询用户: %w", err)
	}
	return &u, nil
}

// exists 条件查是否存在
func (r *UserRepository) exists(cond string, args ...any) (bool, error) {
	var count int64
	if err := r.db.Model(&model.User{}).Where(cond, args...).Count(&count).Error; err != nil {
		return false, fmt.Errorf("查询用户是否存在: %w", err)
	}
	return count > 0, nil
}
