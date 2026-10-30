package main

import (
	"fmt"
	"time"

	"cloudbox/internal/api"
	"cloudbox/internal/api/handler"
	"cloudbox/internal/api/middleware"
	"cloudbox/internal/pkg/config"
	"cloudbox/internal/pkg/jwt"
	"cloudbox/internal/pkg/redis"
	"cloudbox/internal/repository"
	"cloudbox/internal/service"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		panic(err)
	}
	// S2：接 MySQL 真库；失败直接 panic（容器未起时日志即提示）
	db, err := repository.OpenMySQL(cfg.MySQL.DSN)
	if err != nil {
		panic(err)
	}
	// S3：JWT Manager + Redis（登录签发与限流用）；密钥不足 32 字节直接 panic
	jwtMgr, err := jwt.NewManager(
		cfg.JWT.Secret,
		cfg.JWT.Issuer,
		time.Duration(cfg.JWT.AccessExpireHours)*time.Hour,
		time.Duration(cfg.JWT.RefreshExpireHours)*time.Hour,
	)
	if err != nil {
		panic(err)
	}
	rdb := redis.NewClient(cfg.Redis.Addr, cfg.Redis.Password, cfg.Redis.DB)
	r := api.SetupRouter()
	h := handler.NewUserHandler(
		service.NewUserService(repository.NewUserRepository(db)).WithLoginDeps(jwtMgr, rdb),
	)
	api.RegisterAuthRoutes(r, h)
	// S4：/api/v1/users/me 接线 AuthRequired + Redis 吊销检查
	api.RegisterUserRoutes(r, h, jwtMgr, &middleware.RedisRevokedChecker{RDB: rdb})
	if err := r.Run(fmt.Sprintf(":%d", cfg.App.Port)); err != nil {
		panic(err)
	}
}
