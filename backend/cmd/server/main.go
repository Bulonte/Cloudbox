package main

import (
	"cloudbox/internal/api"
)

func main() {
	r := api.SetupRouter()
	// 端口暂硬编码，切片B接入Viper后再改为从配置读取
	if err := r.Run(":8080"); err != nil {
		panic(err)
	}
}
