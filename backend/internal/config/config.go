package config

import (
	"fmt"

	toml "github.com/knadh/koanf/parsers/toml/v2"
	"github.com/knadh/koanf/providers/file"
	koanf "github.com/knadh/koanf/v2"
)

type Config struct {
	Database DatabaseConfig `koanf:"database"`
}

type DatabaseConfig struct {
	Host     string `koanf:"host"`
	Port     uint   `koanf:"port"`
	Username string `koanf:"username"`
	Password string `koanf:"password"`
	DB       string `koanf:"db"`
}

func Load(path string) (*Config, error) {
	k := koanf.New(".")

	if err := k.Load(file.Provider(path), toml.Parser()); err != nil {
		return nil, fmt.Errorf("读取配置文件失败: %w", err)
	}

	c := &Config{}
	if err := k.Unmarshal("", c); err != nil {
		return nil, fmt.Errorf("解析配置文件失败: %w", err)
	}

	if c.Database.Host == "" {
		return nil, fmt.Errorf("database.host 不能为空")
	}
	return c, nil
}
