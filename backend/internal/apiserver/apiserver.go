package apiserver

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"time"

	"go.uber.org/zap"

	"github.com/solarhell/certship/internal/daemon"
	"github.com/solarhell/certship/pkg/database"
	entuser "github.com/solarhell/certship/pkg/ent/user"
	"github.com/solarhell/certship/pkg/module/password"
	"github.com/solarhell/certship/pkg/server"
	"github.com/solarhell/certship/pkg/server/middleware"
)

// Run 启动 API server，阻塞直到 ctx 被取消
func Run(ctx context.Context, addr string, logger *zap.Logger, d *daemon.Daemon) error {
	if err := ensureJWTSecret(ctx); err != nil {
		return fmt.Errorf("初始化 JWT 密钥失败: %w", err)
	}
	if err := ensureDefaultUser(ctx, logger); err != nil {
		return fmt.Errorf("初始化默认用户失败: %w", err)
	}

	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	protocols.SetUnencryptedHTTP2(true)

	srv := &http.Server{
		Addr:              addr,
		Handler:           middleware.CORS().Handler(server.Setup(d)),
		Protocols:         protocols,
		ReadHeaderTimeout: time.Second,
		ReadTimeout:       5 * time.Minute,
		WriteTimeout:      5 * time.Minute,
		MaxHeaderBytes:    8 * 1024,
	}

	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
	}()

	logger.Info("API server 启动", zap.String("addr", addr))
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// ensureJWTSecret 若 jwt_secret 为空则自动生成并保存
func ensureJWTSecret(ctx context.Context) error {
	settings, err := database.GetAppSettings(ctx)
	if err != nil {
		return err
	}
	if settings.JwtSecret != "" {
		return nil
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return err
	}
	return settings.Update().SetJwtSecret(hex.EncodeToString(b)).Exec(ctx)
}

// ensureDefaultUser 若 admin 用户不存在则创建，密码随机生成并打印一次
func ensureDefaultUser(ctx context.Context, logger *zap.Logger) error {
	db := database.GetClient()
	count, err := db.User.Query().Where(entuser.UsernameEQ("admin")).Count(ctx)
	if err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	b := make([]byte, 18)
	if _, err := rand.Read(b); err != nil {
		return err
	}
	plain := base64.RawURLEncoding.EncodeToString(b)
	hash, err := password.Hash(plain)
	if err != nil {
		return err
	}
	if err := db.User.Create().
		SetUsername("admin").
		SetPasswordHash(hash).
		SetNickname("Admin").
		Exec(ctx); err != nil {
		return err
	}
	logger.Warn("已创建默认管理员账号，请立即登录并修改密码。此密码仅显示一次。",
		zap.String("username", "admin"),
		zap.String("password", plain),
	)
	return nil
}
