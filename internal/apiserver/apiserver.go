package apiserver

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"time"

	"golang.org/x/crypto/bcrypt"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
	"go.uber.org/zap"

	"github.com/solarhell/certship/pkg/database"
	entadmin "github.com/solarhell/certship/pkg/ent/admin"
	"github.com/solarhell/certship/pkg/server"
	"github.com/solarhell/certship/pkg/server/middleware"
)

// Run 启动 API server，阻塞直到 ctx 被取消
func Run(ctx context.Context, addr string, logger *zap.Logger) error {
	if err := ensureJWTSecret(ctx); err != nil {
		return fmt.Errorf("初始化 JWT 密钥失败: %w", err)
	}
	if err := ensureDefaultAdmin(ctx); err != nil {
		return fmt.Errorf("初始化默认管理员失败: %w", err)
	}

	srv := &http.Server{
		Addr: addr,
		Handler: h2c.NewHandler(
			middleware.CORS().Handler(server.Setup()),
			&http2.Server{},
		),
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
	secret := hex.EncodeToString(b)
	return settings.Update().SetJwtSecret(secret).Exec(ctx)
}

// ensureDefaultAdmin 若 t_admin 为空则创建默认管理员 admin/certship
func ensureDefaultAdmin(ctx context.Context) error {
	db := database.GetClient()
	count, err := db.Admin.Query().Where(entadmin.UsernameEQ("admin")).Count(ctx)
	if err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	hash, err := bcrypt.GenerateFromPassword([]byte("certship"), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	return db.Admin.Create().
		SetUsername("admin").
		SetPasswordHash(string(hash)).
		Exec(ctx)
}
