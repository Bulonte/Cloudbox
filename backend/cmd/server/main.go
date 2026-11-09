package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"cloudbox/internal/api"
	"cloudbox/internal/api/handler"
	"cloudbox/internal/api/middleware"
	"cloudbox/internal/pkg/config"
	"cloudbox/internal/pkg/jwt"
	"cloudbox/internal/pkg/redis"
	"cloudbox/internal/repository"
	"cloudbox/internal/service"
	"cloudbox/internal/service/permission"
	"cloudbox/internal/service/smart"
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
	// W6-S4：审计旁路（C方案：detail 只记 ID/快照，禁记 share_code 明文/哈希；fail-open 不阻断主流程）
	auditSvc := service.NewAuditService(repository.NewOperationLogRepository(db))
	// S6：管理演示路由纵向越权回归（W9 真管理接口复用同一条链；S4 挂载 /operation-logs 查询）
	api.RegisterAdminRoutes(r, jwtMgr, checker, handler.NewAdminHandler(auditSvc))
	// W4-S2：文件夹 CRUD 路由接线（建夹/改名移动/软删）；W4-S7 下载存储注入
	fileRepo := repository.NewFileRepository(db)
	// W4-S6/S7：存储根创建失败直接 panic（上传内容寻址 + 下载流式回写共用）
	st, err := storage.NewLocalStorage(cfg.Storage.LocalDir)
	if err != nil {
		panic(err)
	}
	fh := handler.NewFileHandler(service.NewFileService(fileRepo).WithStorage(st).WithChecker(permission.NewChecker()).WithAuditor(auditSvc))
	// W6-S8：分类器 + 指纹旁路注入（fail-open：规则表空/加载失败回退内置最小表，不阻断启动）
	smart.Init()
	kws, lerr := smart.NewRuleLoader(repository.NewCategoryKeywordRepository(db)).Load(context.Background())
	if lerr != nil {
		slog.Warn("分类规则加载失败，已回退内置表", "err", lerr)
		kws = nil
	}
	classifier := smart.NewRuleClassifierWithKeywords(kws)
	fpsRepo := repository.NewFingerprintRepository(db)
	fh.WithUploader(upload.NewUploadService(fileRepo, st, cfg.Upload.MaxSimpleSize).WithUsers(repository.NewUserRepository(db)).WithClassifier(classifier).WithFingerprints(fpsRepo))
	api.RegisterFileRoutes(r, fh, jwtMgr, checker)
	// W5-S1：分片会话初始化路由接线（init 只建占位行，不碰 storage/Redis）
	// W5-S2：分片暂存根 .tmp/chunks，启动建目录失败直接 panic（与 storage 同策略）
	chunkTmp := filepath.Join(cfg.Storage.LocalDir, ".tmp", "chunks")
	if err := os.MkdirAll(chunkTmp, 0o755); err != nil {
		panic(err)
	}
	chunkSvc := service.NewChunkService(fileRepo, repository.NewUserRepository(db), repository.NewChunkRepository(db), cfg.Upload.MaxFileSize, cfg.Upload.MaxChunks).WithTmpDir(chunkTmp).WithStorage(st).WithLocker(rdb).WithClassifier(classifier).WithFingerprints(fpsRepo)
	api.RegisterUploadRoutes(r, handler.NewChunkHandler(chunkSvc), jwtMgr, checker)
	// W6-S1：分享生成路由接线（POST /api/v1/shares，需登录；S2 公开链路另接）
	shareSvc := service.NewShareService(repository.NewShareRepository(db), fileRepo).WithStorage(st).WithLimiter(rdb).WithChecker(permission.NewChecker()).WithAuditor(auditSvc)
	api.RegisterShareRoutes(r, handler.NewShareHandler(shareSvc), jwtMgr, checker)
	// W6-S2：分享公开链路（GET /s/:code + GET /api/v1/shares/:code/download，免登录；限流/伪装由 Service 层收口）
	api.RegisterSharePublicRoutes(r, handler.NewShareHandler(shareSvc))
	if err := r.Run(fmt.Sprintf(":%d", cfg.App.Port)); err != nil {
		panic(err)
	}
}
