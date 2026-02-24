package appsettings

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	"go.uber.org/zap"

	certshipv1 "github.com/solarhell/certship/pkg/api/certship/v1"
	"github.com/solarhell/certship/pkg/api/certship/v1/certshipv1connect"
	"github.com/solarhell/certship/pkg/database"
)

func NewServer() certshipv1connect.AppSettingsServiceHandler {
	return &Server{logger: zap.L().Named("app_settings")}
}

type Server struct {
	logger *zap.Logger
}

func (s *Server) GetAppSettings(ctx context.Context, _ *connect.Request[certshipv1.GetAppSettingsRequest]) (*connect.Response[certshipv1.GetAppSettingsResponse], error) {
	settings, err := database.GetAppSettings(ctx)
	if err != nil {
		s.logger.Error("读取配置失败", zap.Error(err))
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&certshipv1.GetAppSettingsResponse{
		AcmeEmail:       settings.AcmeEmail,
		AcmeDirectoryUrl: settings.AcmeDirectoryURL,
		ScanInterval:    settings.ScanInterval,
		RenewBeforeDays: int32(settings.RenewBeforeDays),
	}), nil
}

func (s *Server) UpdateAppSettings(ctx context.Context, req *connect.Request[certshipv1.UpdateAppSettingsRequest]) (*connect.Response[certshipv1.UpdateAppSettingsResponse], error) {
	if req.Msg.AcmeEmail == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("acme_email 不能为空"))
	}

	settings, err := database.GetAppSettings(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	updater := settings.Update()
	if req.Msg.AcmeEmail != "" {
		updater = updater.SetAcmeEmail(req.Msg.AcmeEmail)
	}
	if req.Msg.AcmeDirectoryUrl != "" {
		updater = updater.SetAcmeDirectoryURL(req.Msg.AcmeDirectoryUrl)
	}
	if req.Msg.ScanInterval != "" {
		if _, err := database.ParseScanInterval(req.Msg.ScanInterval); err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
		updater = updater.SetScanInterval(req.Msg.ScanInterval)
	}
	if req.Msg.RenewBeforeDays > 0 {
		updater = updater.SetRenewBeforeDays(int(req.Msg.RenewBeforeDays))
	}

	if err := updater.Exec(ctx); err != nil {
		s.logger.Error("更新配置失败", zap.Error(err))
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&certshipv1.UpdateAppSettingsResponse{}), nil
}
