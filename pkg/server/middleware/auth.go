package middleware

import (
	"context"
	"fmt"
	"strings"
	"time"

	"connectrpc.com/connect"
	"go.uber.org/zap"

	"github.com/solarhell/certship/pkg/ctxadmin"
	"github.com/solarhell/certship/pkg/database"
	"github.com/solarhell/certship/pkg/ent"
	entadmin "github.com/solarhell/certship/pkg/ent/admin"
	entauthtoken "github.com/solarhell/certship/pkg/ent/authtoken"
	jwtmodule "github.com/solarhell/certship/pkg/module/jwt"
)

// anonymousProcedures 允许匿名访问的 ConnectRPC procedure 路径
var anonymousProcedures = map[string]bool{
	"/certship.v1.AuthService/Login": true,
}

type AuthInterceptor struct {
	logger *zap.Logger
}

func NewAuthInterceptor() *AuthInterceptor {
	return &AuthInterceptor{logger: zap.L().Named("auth")}
}

func (i *AuthInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		ctx, err := i.authenticate(ctx, req.Spec().Procedure, req.Header().Get("Authorization"))
		if err != nil {
			return nil, err
		}
		return next(ctx, req)
	}
}

func (i *AuthInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

func (i *AuthInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		ctx, err := i.authenticate(ctx, conn.Spec().Procedure, conn.RequestHeader().Get("Authorization"))
		if err != nil {
			return err
		}
		return next(ctx, conn)
	}
}

func (i *AuthInterceptor) authenticate(ctx context.Context, procedure, authHeader string) (context.Context, error) {
	if anonymousProcedures[procedure] {
		return ctx, nil
	}

	token := strings.TrimPrefix(strings.TrimSpace(authHeader), "Bearer ")
	if token == "" {
		return ctx, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("token 不能为空"))
	}

	settings, err := database.GetAppSettings(ctx)
	if err != nil || settings.JwtSecret == "" {
		return ctx, connect.NewError(connect.CodeInternal, fmt.Errorf("获取 JWT 密钥失败"))
	}

	adminID, err := jwtmodule.ParseToken(token, settings.JwtSecret)
	if err != nil {
		return ctx, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("token 无效"))
	}

	db := database.GetClient()

	tokenRecord, err := db.AuthToken.Query().
		Where(entauthtoken.TokenEQ(token)).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return ctx, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("token 已失效"))
		}
		return ctx, connect.NewError(connect.CodeInternal, err)
	}

	admin, err := db.Admin.Query().
		Where(entadmin.IDEQ(adminID)).
		Only(ctx)
	if err != nil {
		return ctx, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("管理员不存在"))
	}

	// 异步更新最后活跃时间，失败不阻塞请求
	now := time.Now()
	ua := ""
	go func() {
		_ = db.AuthToken.UpdateOne(tokenRecord).
			SetLastActiveTime(now).
			SetLastActiveUserAgent(ua).
			Exec(context.Background())
	}()

	ctx = ctxadmin.SetInContext(ctx, ctxadmin.AuthContext{
		Token:   token,
		AdminID: adminID,
		Admin:   admin,
	})
	return ctx, nil
}
