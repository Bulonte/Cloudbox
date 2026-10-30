package service

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"regexp"
	"strings"
	"time"

	"cloudbox/internal/model"
	"cloudbox/internal/pkg/jwt"
	"cloudbox/internal/pkg/password"
	"cloudbox/internal/repository"
)

// ErrInvalidParams 参数校验失败（映射 HTTP 400 / code 10001）
var ErrInvalidParams = errors.New("参数错误")

// ErrUserConflict 用户名或邮箱已存在（映射 HTTP 409 / code 20004），复用仓储层哨兵
var ErrUserConflict = repository.ErrUserConflict

// ErrInvalidCredentials 账号或密码错误（映射 HTTP 401 / code 20002，故意模糊防枚举）
var ErrInvalidCredentials = errors.New("账号或密码错误")

// ErrTooManyAttempts 登录尝试过于频繁（映射 HTTP 429 / code 20005）
var ErrTooManyAttempts = errors.New("登录尝试过于频繁请稍后再试")

// 登录限流：固定窗口 5 次/分钟（规范 §4.4 冻结）
const (
	loginAttemptLimit  = 5
	loginAttemptWindow = time.Minute
)

// 登录 Redis key 前缀
const (
	loginAttemptKeyPrefix = "login:attempt:"
	refreshTokenKeyPrefix = "refresh:"
)

// access 吊销 key 前缀（登出/改密时写，AuthRequired 查）
const revokedKeyPrefix = "jwt:access:"

// ErrInvalidRefreshToken refresh 无效（映射 HTTP 401 / code 20002，模糊防枚举）
var ErrInvalidRefreshToken = errors.New("登录已过期")

// RegisterInput 注册入参
type RegisterInput struct {
	Username string
	Email    string
	Password string
}

var usernameRe = regexp.MustCompile(`^[A-Za-z0-9_]{3,32}$`)

// RedisStore 登录所需的 Redis 能力子集（*redis.Client 原生满足，测试可用内存 fake）
type RedisStore interface {
	Incr(ctx context.Context, key string) (int64, error)
	Expire(ctx context.Context, key string, ttl time.Duration) error
	Set(ctx context.Context, key string, val any, ttl time.Duration) error
	Del(ctx context.Context, keys ...string) error
	Get(ctx context.Context, key string) (string, error)
}

// UserService 用户业务：只做业务不碰 HTTP
// jwtMgr/rdb 为空表示未接登录依赖（S2 注册单测场景），调 Login 会报配置错
type UserService struct {
	users  *repository.UserRepository
	jwtMgr *jwt.Manager
	rdb    RedisStore
}

// NewUserService 构造注入仓储，测试可换内存库
func NewUserService(users *repository.UserRepository) *UserService {
	return &UserService{users: users}
}

// WithLoginDeps 注入登录依赖（JWT 签发 + Redis 限流/refresh 落库），返回同一实例方便链式调用
func (s *UserService) WithLoginDeps(jwtMgr *jwt.Manager, rdb RedisStore) *UserService {
	s.jwtMgr = jwtMgr
	s.rdb = rdb
	return s
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
		Role:         model.RoleUser,
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

// Profile 按用户 ID 查个人信息（/me 用）：薄封装，查不到透传 ErrUserNotFound
func (s *UserService) Profile(userID uint64) (*model.User, error) {
	if userID == 0 {
		return nil, fmt.Errorf("%w: 用户 ID 非法", ErrInvalidParams)
	}
	return s.users.FindByID(userID)
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

// LoginInput 登录入参：Login 可为用户名或邮箱
type LoginInput struct {
	Login    string
	Password string
}

// TokenPair 登录签发的双 token
type TokenPair struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    int64 // access 有效期秒数，前端倒计时用
}

// Login 登录：查用户 → 限流 → 比对密码 → 签发双 token → refresh 落库
// 账号不存在与密码错误返回同一错误，防用户枚举；Redis 故障直接 500（fail-closed）
func (s *UserService) Login(in LoginInput) (*TokenPair, error) {
	if s.jwtMgr == nil || s.rdb == nil {
		return nil, fmt.Errorf("登录依赖未注入")
	}
	login := strings.TrimSpace(in.Login)
	if login == "" || in.Password == "" {
		return nil, fmt.Errorf("%w: 登录账号与密码不能为空", ErrInvalidParams)
	}
	u, err := s.users.FindByLogin(login)
	if err != nil {
		// 查不到与仓储异常都模糊为凭证错（R3 不吞错前提下防枚举；真故障由日志定位）
		if errors.Is(err, repository.ErrUserNotFound) {
			return nil, ErrInvalidCredentials
		}
		return nil, fmt.Errorf("查询用户失败: %w", err)
	}
	// S5：停用/删除账号拒绝登录（模糊防枚举）
	if u.Status != 1 {
		return nil, ErrInvalidCredentials
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// 限流：固定窗口 5 次/分钟；Redis 故障 fail-closed 直接 500
	attemptKey := loginAttemptKeyPrefix + login
	n, err := s.rdb.Incr(ctx, attemptKey)
	if err != nil {
		return nil, fmt.Errorf("限流计数失败: %w", err)
	}
	if n == 1 {
		if err := s.rdb.Expire(ctx, attemptKey, loginAttemptWindow); err != nil {
			return nil, fmt.Errorf("限流窗口设置失败: %w", err)
		}
	}
	if n > loginAttemptLimit {
		return nil, ErrTooManyAttempts
	}

	if !password.Verify(in.Password, u.PasswordHash) {
		return nil, ErrInvalidCredentials
	}

	access, _, err := s.jwtMgr.GenerateAccess(u.ID, u.Role)
	if err != nil {
		return nil, fmt.Errorf("签发访问 token 失败: %w", err)
	}
	refresh, refreshJTI, err := s.jwtMgr.GenerateRefresh(u.ID)
	if err != nil {
		return nil, fmt.Errorf("签发刷新 token 失败: %w", err)
	}
	// refresh 落库：登出/刷新轮换时 DEL；TTL 与 JWT refresh 一致
	if err := s.rdb.Set(ctx, refreshTokenKeyPrefix+refreshJTI, u.ID, s.jwtMgr.RefreshTTL()); err != nil {
		return nil, fmt.Errorf("刷新 token 落库失败: %w", err)
	}
	// 登录成功清限流计数
	if err := s.rdb.Del(ctx, attemptKey); err != nil {
		return nil, fmt.Errorf("限流计数清理失败: %w", err)
	}
	return &TokenPair{
		AccessToken:  access,
		RefreshToken: refresh,
		ExpiresIn:    int64(s.jwtMgr.AccessTTL() / time.Second),
	}, nil
}

// RefreshInput 刷新入参：凭 refresh_token 轮换新双 token（防重放：旧 jti 用后即删）
type RefreshInput struct {
	RefreshToken string
}

// Refresh 刷新轮换：验 refresh → 查落库一致性 → 验用户可用 → 签新对 → 先 Set 新再 Del 旧
func (s *UserService) Refresh(in RefreshInput) (*TokenPair, error) {
	if s.jwtMgr == nil || s.rdb == nil {
		return nil, fmt.Errorf("登录依赖未注入")
	}
	if strings.TrimSpace(in.RefreshToken) == "" {
		return nil, fmt.Errorf("%w: refresh_token 不能为空", ErrInvalidParams)
	}
	claims, err := s.jwtMgr.Parse(in.RefreshToken)
	if err != nil {
		return nil, ErrInvalidRefreshToken
	}
	// 必须是 refresh（拿 access 来刷直接拒绝）
	if claims.TokenType != jwt.TokenTypeRefresh {
		return nil, ErrInvalidRefreshToken
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	stored, err := s.rdb.Get(ctx, refreshTokenKeyPrefix+claims.JTI)
	if err != nil {
		// key 不存在 = 已轮换/已登出/已过期，一律模糊为过期
		return nil, ErrInvalidRefreshToken
	}
	if stored == "" || stored != uintToString(claims.UserID) {
		return nil, ErrInvalidRefreshToken
	}
	u, err := s.users.FindByID(claims.UserID)
	if err != nil {
		if errors.Is(err, repository.ErrUserNotFound) {
			// 僵尸 refresh 顺手清理
			_ = s.rdb.Del(ctx, refreshTokenKeyPrefix+claims.JTI)
			return nil, ErrInvalidRefreshToken
		}
		return nil, fmt.Errorf("查询用户失败: %w", err)
	}
	if u.Status != 1 {
		_ = s.rdb.Del(ctx, refreshTokenKeyPrefix+claims.JTI)
		return nil, ErrInvalidCredentials
	}
	access, _, err := s.jwtMgr.GenerateAccess(u.ID, u.Role)
	if err != nil {
		return nil, fmt.Errorf("签发访问 token 失败: %w", err)
	}
	refresh, newJTI, err := s.jwtMgr.GenerateRefresh(u.ID)
	if err != nil {
		return nil, fmt.Errorf("签发刷新 token 失败: %w", err)
	}
	// 先 Set 新再 Del 旧：崩溃时最多多一枚有效 refresh，反过来会丢登录态
	if err := s.rdb.Set(ctx, refreshTokenKeyPrefix+newJTI, u.ID, s.jwtMgr.RefreshTTL()); err != nil {
		return nil, fmt.Errorf("刷新 token 落库失败: %w", err)
	}
	if err := s.rdb.Del(ctx, refreshTokenKeyPrefix+claims.JTI); err != nil {
		return nil, fmt.Errorf("旧刷新 token 清理失败: %w", err)
	}
	return &TokenPair{
		AccessToken:  access,
		RefreshToken: refresh,
		ExpiresIn:    int64(s.jwtMgr.AccessTTL() / time.Second),
	}, nil
}

// LogoutInput 登出入参：AccessJTI 由中间件注入；RefreshToken 顺手清理可空
type LogoutInput struct {
	AccessJTI    string
	RefreshToken string
}

// Logout 登出：吊销本 access + 删带来的 refresh；refresh 解析失败不报错（幂等成功）
func (s *UserService) Logout(in LogoutInput) error {
	if s.jwtMgr == nil || s.rdb == nil {
		return fmt.Errorf("登录依赖未注入")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if in.AccessJTI != "" {
		if err := s.rdb.Set(ctx, revokedKeyPrefix+in.AccessJTI, "1", s.jwtMgr.AccessTTL()); err != nil {
			return fmt.Errorf("吊销访问 token 失败: %w", err)
		}
	}
	if strings.TrimSpace(in.RefreshToken) == "" {
		return nil
	}
	claims, err := s.jwtMgr.Parse(in.RefreshToken)
	if err != nil || claims.TokenType != jwt.TokenTypeRefresh {
		return nil
	}
	if err := s.rdb.Del(ctx, refreshTokenKeyPrefix+claims.JTI); err != nil {
		return fmt.Errorf("清理刷新 token 失败: %w", err)
	}
	return nil
}

// ChangePasswordInput 改密入参：UserID/AccessJTI 由中间件注入
type ChangePasswordInput struct {
	UserID       uint64
	AccessJTI    string
	OldPassword  string
	NewPassword  string
	RefreshToken string
}

// ChangePassword 改密：验旧密 → 哈希落库 → 吊销本 access 强制重登；refresh 顺手清
func (s *UserService) ChangePassword(in ChangePasswordInput) error {
	if s.jwtMgr == nil || s.rdb == nil {
		return fmt.Errorf("登录依赖未注入")
	}
	if in.UserID == 0 {
		return fmt.Errorf("%w: 用户 ID 非法", ErrInvalidParams)
	}
	if !validPassword(in.NewPassword) {
		return fmt.Errorf("%w: 密码需8位以上且含字母和数字", ErrInvalidParams)
	}
	u, err := s.users.FindByID(in.UserID)
	if err != nil {
		if errors.Is(err, repository.ErrUserNotFound) {
			return ErrInvalidCredentials
		}
		return fmt.Errorf("查询用户失败: %w", err)
	}
	if u.Status != 1 {
		return ErrInvalidCredentials
	}
	if !password.Verify(in.OldPassword, u.PasswordHash) {
		return fmt.Errorf("%w: 旧密码不正确", ErrInvalidCredentials)
	}
	hash, err := password.Hash(in.NewPassword)
	if err != nil {
		return fmt.Errorf("密码加密失败: %w", err)
	}
	if err := s.users.UpdatePasswordHash(u.ID, hash); err != nil {
		if errors.Is(err, repository.ErrUserNotFound) {
			return ErrInvalidCredentials
		}
		return fmt.Errorf("更新密码失败: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	// 强制本设备下线：吊销当前 access
	if in.AccessJTI != "" {
		if err := s.rdb.Set(ctx, revokedKeyPrefix+in.AccessJTI, "1", s.jwtMgr.AccessTTL()); err != nil {
			return fmt.Errorf("吊销访问 token 失败: %w", err)
		}
	}
	if strings.TrimSpace(in.RefreshToken) == "" {
		return nil
	}
	if claims, err := s.jwtMgr.Parse(in.RefreshToken); err == nil && claims.TokenType == jwt.TokenTypeRefresh {
		if err := s.rdb.Del(ctx, refreshTokenKeyPrefix+claims.JTI); err != nil {
			return fmt.Errorf("清理刷新 token 失败: %w", err)
		}
	}
	return nil
}

// uintToString 用户 ID 与 Redis 落库值比对用（内存 fake 与真库统一走字符串）
func uintToString(n uint64) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
