package middleware

import (
	"context"
	"strings"
	"time"

	"connectrpc.com/connect/v2"

	"github.com/solarhell/certship/pkg/ctxadmin"
	"github.com/solarhell/certship/pkg/database"
	"github.com/solarhell/certship/pkg/ent"
	entauthtoken "github.com/solarhell/certship/pkg/ent/authtoken"
	entuser "github.com/solarhell/certship/pkg/ent/user"
	jwtmodule "github.com/solarhell/certship/pkg/module/jwt"
)

var anonymousProcedures = map[string]bool{
	"/certship.v1.AuthService/Login": true,
}

// v2 的统一拦截器同时覆盖 unary 和 streaming RPC。
func NewAuthInterceptor() connect.ServerInterceptor {
	return func(next connect.ServerFunc) connect.ServerFunc {
		return func(ctx context.Context, spec connect.Spec, stream connect.ServerStream) error {
			var authHeader string
			if info, ok := connect.CallInfoForServerContext(ctx); ok {
				authHeader = info.RequestHeader().Get("Authorization")
			}
			ctx, err := authenticate(ctx, spec.Procedure, authHeader)
			if err != nil {
				return err
			}
			return next(ctx, spec, stream)
		}
	}
}

func authenticate(ctx context.Context, procedure, authHeader string) (context.Context, error) {
	if anonymousProcedures[procedure] {
		return ctx, nil
	}

	token := strings.TrimPrefix(strings.TrimSpace(authHeader), "Bearer ")
	if token == "" {
		return ctx, connect.Errorf(connect.CodeUnauthenticated, "token 不能为空")
	}

	settings, err := database.GetAppSettings(ctx)
	if err != nil || settings.JwtSecret == "" {
		return ctx, connect.Errorf(connect.CodeInternal, "获取 JWT 密钥失败")
	}

	userID, err := jwtmodule.ParseToken(token, settings.JwtSecret)
	if err != nil {
		return ctx, connect.Errorf(connect.CodeUnauthenticated, "token 无效")
	}

	db := database.GetClient()

	tokenRecord, err := db.AuthToken.Query().
		Where(entauthtoken.TokenEQ(token)).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return ctx, connect.Errorf(connect.CodeUnauthenticated, "token 已失效")
		}
		return ctx, connect.NewError(connect.CodeInternal, err.Error()).WithCause(err)
	}

	u, err := db.User.Query().
		Where(entuser.IDEQ(userID)).
		Only(ctx)
	if err != nil {
		return ctx, connect.Errorf(connect.CodeUnauthenticated, "用户不存在")
	}

	now := time.Now()
	go func() {
		_ = db.AuthToken.UpdateOne(tokenRecord).
			SetLastActiveTime(now).
			Exec(context.Background())
	}()

	ctx = ctxadmin.SetInContext(ctx, ctxadmin.AuthContext{
		Token:  token,
		UserID: userID,
		User:   u,
	})
	return ctx, nil
}
