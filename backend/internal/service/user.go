package service

import (
	"errors"
	"fmt"
	"net/mail"
	"regexp"
	"strings"

	"cloudbox/internal/model"
	"cloudbox/internal/pkg/password"
	"cloudbox/internal/repository"
)

// ErrInvalidParams 参数校验失败（映射 HTTP 400 / code 10001）
var ErrInvalidParams = errors.New("参数错误")

// ErrUserConflict 用户名或邮箱已存在（映射 HTTP 409 / code 20004），复用仓储层哨兵
var ErrUserConflict = repository.ErrUserConflict

// RegisterInput 注册入参
type RegisterInput struct {
	Username string
	Email    string
	Password string
}

var usernameRe = regexp.MustCompile(`^[A-Za-z0-9_]{3,32}$`)

// UserService 用户业务：只做业务不碰 HTTP
type UserService struct {
	users *repository.UserRepository
}

// NewUserService 构造注入仓储，测试可换内存库
func NewUserService(users *repository.UserRepository) *UserService {
	return &UserService{users: users}
}

// Register 注册：校验 → 用户名查重 → 邮箱查重 → bcrypt 落库
// 冲突错误故意模糊（不区分用户名/邮箱），防用户枚举
func (s *UserService) Register(in RegisterInput) (*model.User, error) {
	username := strings.TrimSpace(in.Username)
	email := strings.TrimSpace(in.Email)
	if !usernameRe.MatchString(username) {
		return nil, fmt.Errorf("%w: 用户名需3-32位字母数字下划线", ErrInvalidParams)
	}
	if _, err := mail.ParseAddress(email); err != nil {
		return nil, fmt.Errorf("%w: 邮箱格式不正确", ErrInvalidParams)
	}
	if !validPassword(in.Password) {
		return nil, fmt.Errorf("%w: 密码需8位以上且含字母和数字", ErrInvalidParams)
	}
	if ok, err := s.users.ExistsByUsername(username); err != nil {
		return nil, fmt.Errorf("查重失败: %w", err)
	} else if ok {
		return nil, ErrUserConflict
	}
	if ok, err := s.users.ExistsByEmail(email); err != nil {
		return nil, fmt.Errorf("查重失败: %w", err)
	} else if ok {
		return nil, ErrUserConflict
	}
	hash, err := password.Hash(in.Password)
	if err != nil {
		return nil, fmt.Errorf("密码加密失败: %w", err)
	}
	u := &model.User{
		Username:     username,
		Email:        email,
		PasswordHash: hash,
		Role:         "user",
		Status:       1,
	}
	if err := s.users.CreateUser(u); err != nil {
		// 并发竞态：两次查重之间被插相同记录，Create 兜底转冲突
		if errors.Is(err, repository.ErrUserConflict) {
			return nil, ErrUserConflict
		}
		return nil, fmt.Errorf("创建用户失败: %w", err)
	}
	return u, nil
}

// validPassword 密码规则：≥8 位且同时含字母与数字
func validPassword(pw string) bool {
	if len(pw) < 8 {
		return false
	}
	var hasLetter, hasDigit bool
	for _, r := range pw {
		switch {
		case r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z':
			hasLetter = true
		case r >= '0' && r <= '9':
			hasDigit = true
		}
		if hasLetter && hasDigit {
			return true
		}
	}
	return false
}
