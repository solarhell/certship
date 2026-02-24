package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/solarhell/certship/internal/apiserver"
	"github.com/solarhell/certship/internal/config"
	"github.com/solarhell/certship/internal/daemon"
	"github.com/solarhell/certship/internal/version"
	"github.com/solarhell/certship/pkg/database"
)

var (
	configPath string
	debug      bool
	addr       string
)

var rootCmd = &cobra.Command{
	Use:   "certship",
	Short: "自动为阿里云 OSS 自定义域名颁发并续期 SSL 证书",
	RunE: func(cmd *cobra.Command, args []string) error {
		logger, err := buildLogger(debug)
		if err != nil {
			return fmt.Errorf("初始化日志失败: %w", err)
		}
		defer func() { _ = logger.Sync() }()

		cfg, err := config.Load(configPath)
		if err != nil {
			return err
		}

		database.Setup(cfg.Database)

		ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer cancel()

		// API server 与 daemon 并行运行
		errCh := make(chan error, 2)

		go func() {
			errCh <- apiserver.Run(ctx, addr, logger)
		}()

		go func() {
			errCh <- daemon.New(logger).Run(ctx)
		}()

		select {
		case err := <-errCh:
			cancel()
			return err
		case <-ctx.Done():
			return nil
		}
	},
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "打印版本信息",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println(version.Get())
	},
}

func init() {
	rootCmd.Flags().StringVar(&configPath, "config", "config.toml", "配置文件路径")
	rootCmd.Flags().BoolVar(&debug, "debug", false, "开启 debug 日志")
	rootCmd.Flags().StringVar(&addr, "addr", "127.0.0.1:8080", "API server 监听地址")
}

func main() {
	rootCmd.AddCommand(versionCmd)
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func buildLogger(debug bool) (*zap.Logger, error) {
	if debug {
		return zap.NewDevelopment()
	}
	cfg := zap.NewProductionConfig()
	cfg.OutputPaths = []string{"stdout"}
	return cfg.Build()
}
