package auth

import (
	"context"
	"fmt"
	"time"

	"connectrpc.com/connect"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"

	certshipv1 "github.com/solarhell/certship/pkg/api/certship/v1"
	"github.com/solarhell/certship/pkg/api/certship/v1/certshipv1connect"
	"github.com/solarhell/certship/pkg/ctxadmin"
	"github.com/solarhell/certship/pkg/database"
	"github.com/solarhell/certship/pkg/ent"
	entuser "github.com/solarhell/certship/pkg/ent/user"
	jwtmodule "github.com/solarhell/certship/pkg/module/jwt"
)

func NewServer() certshipv1connect.AuthServiceHandler {
	return &Server{logger: zap.L().Named("auth")}
}

type Server struct {
	logger *zap.Logger
}

func (s *Server) Login(ctx context.Context, req *connect.Request[certshipv1.LoginRequest]) (*connect.Response[certshipv1.LoginResponse], error) {
	if req.Msg.Username == "" || req.Msg.Password == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("用户名和密码不能为空"))
	}

	db := database.GetClient()
	u, err := db.User.Query().
		Where(entuser.UsernameEQ(req.Msg.Username)).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("用户名或密码错误"))
		}
		s.logger.Error("查询用户失败", zap.Error(err))
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("查询用户失败"))
	}

	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(req.Msg.Password)); err != nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("用户名或密码错误"))
	}

	settings, err := database.GetAppSettings(ctx)
	if err != nil || settings.JwtSecret == "" {
		s.logger.Error("获取 JWT 密钥失败", zap.Error(err))
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("获取 JWT 密钥失败"))
	}

	token, err := jwtmodule.GenerateToken(u.ID, settings.JwtSecret)
	if err != nil {
		s.logger.Error("生成 token 失败", zap.Error(err))
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("生成 token 失败"))
	}

	now := time.Now()
	ua := req.Header().Get("User-Agent")
	ip := req.Header().Get("X-Real-IP")
	if ip == "" {
		ip = req.Header().Get("X-Forwarded-For")
	}

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
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("创建 token 记录失败"))
	}

	return connect.NewResponse(&certshipv1.LoginResponse{Token: token}), nil
}

func (s *Server) ChangePassword(ctx context.Context, req *connect.Request[certshipv1.ChangePasswordRequest]) (*connect.Response[certshipv1.ChangePasswordResponse], error) {
	if req.Msg.NewPassword == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("新密码不能为空"))
	}

	authCtx := ctxadmin.MustExtract(ctx)

	if err := bcrypt.CompareHashAndPassword([]byte(authCtx.User.PasswordHash), []byte(req.Msg.OldPassword)); err != nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("旧密码错误"))
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Msg.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("密码加密失败"))
	}

	if err := database.GetClient().User.UpdateOneID(authCtx.UserID).
		SetPasswordHash(string(hash)).
		Exec(ctx); err != nil {
		s.logger.Error("更新密码失败", zap.Error(err))
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("更新密码失败"))
	}

	return connect.NewResponse(&certshipv1.ChangePasswordResponse{}), nil
}
