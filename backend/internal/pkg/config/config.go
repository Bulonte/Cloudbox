package config

import (
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

// Load 加载配置：configs/config.yaml 默认值 < 环境变量覆盖
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
	v.SetDefault("jwt.secret", "dev-only-please-override-32bytes-minimum!!")
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
	_ = v.BindEnv("jwt.secret", "JWT_SECRET")
	_ = v.BindEnv("jwt.access_expire_hours", "JWT_ACCESS_EXPIRE_HOURS")
	_ = v.BindEnv("jwt.refresh_expire_hours", "JWT_REFRESH_EXPIRE_HOURS")
	_ = v.BindEnv("jwt.issuer", "JWT_ISSUER")
	_ = v.BindEnv("storage.driver", "STORAGE_DRIVER")
	_ = v.BindEnv("storage.local_dir", "STORAGE_LOCAL_DIR")

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
