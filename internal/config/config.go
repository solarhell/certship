package config

import (
	"fmt"

	"github.com/spf13/viper"
)

type Config struct {
	Database DatabaseConfig `mapstructure:"database"`
}

type DatabaseConfig struct {
	Host     string `mapstructure:"host"`
	Port     uint   `mapstructure:"port"`
	Username string `mapstructure:"username"`
	Password string `mapstructure:"password"`
	DB       string `mapstructure:"db"`
}

func Load(path string) (*Config, error) {
	viper.SetConfigFile(path)
	viper.SetConfigType("toml")

	if err := viper.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("读取配置文件失败: %w", err)
	}

	c := &Config{}
	if err := viper.Unmarshal(c); err != nil {
		return nil, fmt.Errorf("解析配置文件失败: %w", err)
	}

	if c.Database.Host == "" {
		return nil, fmt.Errorf("database.host 不能为空")
	}
	return c, nil
}
