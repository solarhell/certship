package auth

import (
	"context"
	"errors"
	"time"

	"connectrpc.com/connect/v2"
	"go.uber.org/zap"

	certshipv1 "github.com/solarhell/certship/pkg/api/certship/v1"
	"github.com/solarhell/certship/pkg/api/certship/v1/certshipv1connect"
	"github.com/solarhell/certship/pkg/ctxadmin"
	"github.com/solarhell/certship/pkg/database"
	"github.com/solarhell/certship/pkg/ent"
	entuser "github.com/solarhell/certship/pkg/ent/user"
	jwtmodule "github.com/solarhell/certship/pkg/module/jwt"
	"github.com/solarhell/certship/pkg/module/password"
)

func NewServer() certshipv1connect.AuthServiceHandler {
	return &Server{logger: zap.L().Named("auth")}
}

type Server struct {
	logger *zap.Logger
}

func (s *Server) Login(ctx context.Context, req *certshipv1.LoginRequest) (*certshipv1.LoginResponse, error) {
	if req.Username == "" || req.Password == "" {
		return nil, connect.Errorf(connect.CodeInvalidArgument, "用户名和密码不能为空")
	}

	db := database.GetClient()
	u, err := db.User.Query().
		Where(entuser.UsernameEQ(req.Username)).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, connect.Errorf(connect.CodeUnauthenticated, "用户名或密码错误")
		}
		s.logger.Error("查询用户失败", zap.Error(err))
		return nil, connect.Errorf(connect.CodeInternal, "查询用户失败")
	}

	if err := password.Verify(u.PasswordHash, req.Password); err != nil {
		if errors.Is(err, password.ErrMismatched) {
			return nil, connect.Errorf(connect.CodeUnauthenticated, "用户名或密码错误")
		}
		s.logger.Error("校验密码失败", zap.Error(err))
		return nil, connect.Errorf(connect.CodeInternal, "校验密码失败")
	}

	settings, err := database.GetAppSettings(ctx)
	if err != nil || settings.JwtSecret == "" {
		s.logger.Error("获取 JWT 密钥失败", zap.Error(err))
		return nil, connect.Errorf(connect.CodeInternal, "获取 JWT 密钥失败")
	}

	token, err := jwtmodule.GenerateToken(u.ID, settings.JwtSecret)
	if err != nil {
		s.logger.Error("生成 token 失败", zap.Error(err))
		return nil, connect.Errorf(connect.CodeInternal, "生成 token 失败")
	}

	now := time.Now()
	ua, ip := loginSource(ctx)

	if err := db.AuthToken.Create().
		SetUserID(u.ID).
		SetToken(token).
		SetLoginUserAgent(ua).
		SetLoginIP(ip).
		SetLoginTime(now).
		SetLastActiveUserAgent(ua).
		SetLastActiveIP(ip).
		SetLastActiveTime(now).
		Exec(ctx); err != nil {
		s.logger.Error("创建 token 记录失败", zap.Error(err))
		return nil, connect.Errorf(connect.CodeInternal, "创建 token 记录失败")
	}

	return &certshipv1.LoginResponse{Token: token}, nil
}

func loginSource(ctx context.Context) (userAgent, ip string) {
	info, ok := connect.CallInfoForServerContext(ctx)
	if !ok {
		return "", ""
	}
	headers := info.RequestHeader()
	userAgent = headers.Get("User-Agent")
	ip = headers.Get("X-Real-IP")
	if ip == "" {
		ip = headers.Get("X-Forwarded-For")
	}
	return userAgent, ip
}

func (s *Server) ChangePassword(ctx context.Context, req *certshipv1.ChangePasswordRequest) (*certshipv1.ChangePasswordResponse, error) {
	if req.NewPassword == "" {
		return nil, connect.Errorf(connect.CodeInvalidArgument, "新密码不能为空")
	}

	authCtx := ctxadmin.MustExtract(ctx)

	if err := password.Verify(authCtx.User.PasswordHash, req.OldPassword); err != nil {
		if errors.Is(err, password.ErrMismatched) {
			return nil, connect.Errorf(connect.CodeUnauthenticated, "旧密码错误")
		}
		s.logger.Error("校验旧密码失败", zap.Error(err))
		return nil, connect.Errorf(connect.CodeInternal, "校验旧密码失败")
	}

	hash, err := password.Hash(req.NewPassword)
	if err != nil {
		return nil, connect.Errorf(connect.CodeInternal, "密码加密失败")
	}

	if err := database.GetClient().User.UpdateOneID(authCtx.UserID).
		SetPasswordHash(hash).
		Exec(ctx); err != nil {
		s.logger.Error("更新密码失败", zap.Error(err))
		return nil, connect.Errorf(connect.CodeInternal, "更新密码失败")
	}

	return &certshipv1.ChangePasswordResponse{}, nil
}
