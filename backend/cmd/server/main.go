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
	"cloudbox/internal/service/upload"
	"cloudbox/internal/storage"
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
	checker := &middleware.RedisRevokedChecker{RDB: rdb}
	api.RegisterAuthRoutes(r, h, jwtMgr, checker)
	// S4：/api/v1/users/me 接线 AuthRequired + Redis 吊销检查；S5 追加 PUT /password
	api.RegisterUserRoutes(r, h, jwtMgr, checker)
	// S6：管理演示路由纵向越权回归（W9 真管理接口复用同一条链）
	api.RegisterAdminRoutes(r, jwtMgr, checker)
	// W4-S2：文件夹 CRUD 路由接线（建夹/改名移动/软删）；W4-S7 下载存储注入
	fileRepo := repository.NewFileRepository(db)
	// W4-S6/S7：存储根创建失败直接 panic（上传内容寻址 + 下载流式回写共用）
	st, err := storage.NewLocalStorage(cfg.Storage.LocalDir)
	if err != nil {
		panic(err)
	}
	fh := handler.NewFileHandler(service.NewFileService(fileRepo).WithStorage(st))
	fh.WithUploader(upload.NewUploadService(fileRepo, st, cfg.Upload.MaxSimpleSize))
	api.RegisterFileRoutes(r, fh, jwtMgr, checker)
	// W5-S1：分片会话初始化路由接线（init 只建占位行，不碰 storage/Redis）
	chunkSvc := service.NewChunkService(fileRepo, repository.NewUserRepository(db), repository.NewChunkRepository(db), cfg.Upload.MaxFileSize, cfg.Upload.MaxChunks)
	api.RegisterUploadRoutes(r, handler.NewChunkHandler(chunkSvc), jwtMgr, checker)
	if err := r.Run(fmt.Sprintf(":%d", cfg.App.Port)); err != nil {
		panic(err)
	}
}
