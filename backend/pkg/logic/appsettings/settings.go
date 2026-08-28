package appsettings

import (
	"context"
	"fmt"
	"time"

	"connectrpc.com/connect"
	"go.uber.org/zap"

	"github.com/solarhell/certship/internal/alidns"
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
		ScanInterval:      settings.ScanInterval,
		RenewBeforeDays:   int32(settings.RenewBeforeDays),
		MissingGrace:      settings.MissingGrace,
		ArchiveAfter:      settings.ArchiveAfter,
		ArchivedRetention: settings.ArchivedRetention,
		DnsResolvers:      settings.DNSResolvers,
	}), nil
}

func (s *Server) UpdateAppSettings(ctx context.Context, req *connect.Request[certshipv1.UpdateAppSettingsRequest]) (*connect.Response[certshipv1.UpdateAppSettingsResponse], error) {
	settings, err := database.GetAppSettings(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	updater := settings.Update()
	if req.Msg.ScanInterval != "" {
		if _, err := database.ParseScanInterval(req.Msg.ScanInterval); err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
		updater = updater.SetScanInterval(req.Msg.ScanInterval)
	}
	if req.Msg.RenewBeforeDays > 0 {
		updater = updater.SetRenewBeforeDays(int(req.Msg.RenewBeforeDays))
	}
	if req.Msg.MissingGrace != "" {
		if _, err := time.ParseDuration(req.Msg.MissingGrace); err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("missing_grace 不是合法的 duration: %w", err))
		}
		updater = updater.SetMissingGrace(req.Msg.MissingGrace)
	}
	if req.Msg.ArchiveAfter != "" {
		if _, err := time.ParseDuration(req.Msg.ArchiveAfter); err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("archive_after 不是合法的 duration: %w", err))
		}
		updater = updater.SetArchiveAfter(req.Msg.ArchiveAfter)
	}
	if req.Msg.DnsResolvers != "" {
		if len(alidns.ParseResolvers(req.Msg.DnsResolvers)) == 0 {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("dns_resolvers 至少要有一个合法的 host 或 host:port"))
		}
		updater = updater.SetDNSResolvers(req.Msg.DnsResolvers)
	}
	if req.Msg.ArchivedRetention != "" {
		if _, err := time.ParseDuration(req.Msg.ArchivedRetention); err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("archived_retention 不是合法的 duration: %w", err))
		}
		updater = updater.SetArchivedRetention(req.Msg.ArchivedRetention)
	}

	if err := updater.Exec(ctx); err != nil {
		s.logger.Error("更新配置失败", zap.Error(err))
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&certshipv1.UpdateAppSettingsResponse{}), nil
}
