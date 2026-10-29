package main

import (
	"fmt"

	"cloudbox/internal/api"
	"cloudbox/internal/pkg/config"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		panic(err)
	}
	r := api.SetupRouter()
	if err := r.Run(fmt.Sprintf(":%d", cfg.App.Port)); err != nil {
		panic(err)
	}
}
