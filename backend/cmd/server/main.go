package main

import (
	"fmt"

	"cloudbox/internal/api"
	"cloudbox/internal/api/handler"
	"cloudbox/internal/pkg/config"
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
	r := api.SetupRouter()
	api.RegisterAuthRoutes(r, handler.NewUserHandler(
		service.NewUserService(repository.NewUserRepository(db)),
	))
	if err := r.Run(fmt.Sprintf(":%d", cfg.App.Port)); err != nil {
		panic(err)
	}
}
