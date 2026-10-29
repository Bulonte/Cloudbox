package config

import (
	"errors"

	"github.com/spf13/viper"
)

// Config 后端全部配置
type Config struct {
	App struct {
		Port int    `mapstructure:"port"`
		Env  string `mapstructure:"env"`
	} `mapstructure:"app"`
	MySQL struct {
		DSN string `mapstructure:"dsn"`
	} `mapstructure:"mysql"`
	Redis struct {
		Addr     string `mapstructure:"addr"`
		Password string `mapstructure:"password"`
		DB       int    `mapstructure:"db"`
	} `mapstructure:"redis"`
	JWT struct {
		Secret             string `mapstructure:"secret"`
		AccessExpireHours  int    `mapstructure:"access_expire_hours"`
		RefreshExpireHours int    `mapstructure:"refresh_expire_hours"`
		Issuer             string `mapstructure:"issuer"`
	} `mapstructure:"jwt"`
	Storage struct {
		Driver   string `mapstructure:"driver"`
		LocalDir string `mapstructure:"local_dir"`
	} `mapstructure:"storage"`
}

// devJWTSecret 仅本地开发默认值；prod 用它直接报错，逼配真密钥
const devJWTSecret = "dev-only-please-override-32bytes-minimum!!"

// Load 加载配置：configs/config.yaml 默认值 < 环境变量覆盖
// JWT 密钥：CLOUDBOX_JWT_SECRET 主用，JWT_SECRET 兼容；prod 不配真密钥直接报错，杜绝默认密钥上线
func Load() (*Config, error) {
	v := viper.New()
	v.SetConfigFile("configs/config.yaml")
	// yaml 不存在也不报错，靠 SetDefault + 环境变量照样能跑
	_ = v.ReadInConfig()

	v.SetDefault("app.port", 8080)
	v.SetDefault("app.env", "dev")
	v.SetDefault("mysql.dsn", "cloudbox:cloudbox123456@tcp(127.0.0.1:3307)/cloudbox?charset=utf8mb4&parseTime=True&loc=Local")
	v.SetDefault("redis.addr", "127.0.0.1:6380")
	v.SetDefault("redis.db", 0)
	v.SetDefault("jwt.secret", devJWTSecret)
	v.SetDefault("jwt.access_expire_hours", 2)
	v.SetDefault("jwt.refresh_expire_hours", 168)
	v.SetDefault("jwt.issuer", "cloudbox")
	v.SetDefault("storage.driver", "local")
	v.SetDefault("storage.local_dir", "./storage/uploads")

	v.AutomaticEnv()
	_ = v.BindEnv("app.port", "APP_PORT")
	_ = v.BindEnv("app.env", "APP_ENV")
	_ = v.BindEnv("mysql.dsn", "MYSQL_DSN")
	_ = v.BindEnv("redis.addr", "REDIS_ADDR")
	_ = v.BindEnv("redis.password", "REDIS_PASSWORD")
	_ = v.BindEnv("redis.db", "REDIS_DB")
	// JWT_SECRET 仅兼容历史用法；规范主变量是 CLOUDBOX_JWT_SECRET，优先级更高
	_ = v.BindEnv("jwt.secret", "CLOUDBOX_JWT_SECRET", "JWT_SECRET")
	_ = v.BindEnv("jwt.access_expire_hours", "JWT_ACCESS_EXPIRE_HOURS")
	_ = v.BindEnv("jwt.refresh_expire_hours", "JWT_REFRESH_EXPIRE_HOURS")
	_ = v.BindEnv("jwt.issuer", "JWT_ISSUER")
	_ = v.BindEnv("storage.driver", "STORAGE_DRIVER")
	_ = v.BindEnv("storage.local_dir", "STORAGE_LOCAL_DIR")

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, err
	}
	// prod 安全兜底：仍用开发默认密钥（或空/过短密钥）直接拒绝启动，杜绝默认密钥上线
	if cfg.App.Env == "prod" && (cfg.JWT.Secret == devJWTSecret || len(cfg.JWT.Secret) < 32) {
		return nil, errors.New("prod 环境必须配置 CLOUDBOX_JWT_SECRET（≥32 字节，禁止使用开发默认密钥）")
	}
	return &cfg, nil
}
