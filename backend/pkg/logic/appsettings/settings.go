package appsettings

import (
	"context"
	"fmt"
	"time"

	"connectrpc.com/connect/v2"
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

func (s *Server) GetAppSettings(ctx context.Context, _ *certshipv1.GetAppSettingsRequest) (*certshipv1.GetAppSettingsResponse, error) {
	settings, err := database.GetAppSettings(ctx)
	if err != nil {
		s.logger.Error("读取配置失败", zap.Error(err))
		return nil, connect.NewError(connect.CodeInternal, err.Error()).WithCause(err)
	}

	return &certshipv1.GetAppSettingsResponse{
		ScanInterval:      settings.ScanInterval,
		RenewBeforeDays:   int32(settings.RenewBeforeDays),
		MissingGrace:      settings.MissingGrace,
		ArchiveAfter:      settings.ArchiveAfter,
		ArchivedRetention: settings.ArchivedRetention,
		DnsResolvers:      settings.DNSResolvers,
	}, nil
}

func (s *Server) UpdateAppSettings(ctx context.Context, req *certshipv1.UpdateAppSettingsRequest) (*certshipv1.UpdateAppSettingsResponse, error) {
	settings, err := database.GetAppSettings(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err.Error()).WithCause(err)
	}

	updater := settings.Update()
	if req.ScanInterval != "" {
		if _, err := database.ParseScanInterval(req.ScanInterval); err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err.Error()).WithCause(err)
		}
		updater = updater.SetScanInterval(req.ScanInterval)
	}
	if req.RenewBeforeDays > 0 {
		updater = updater.SetRenewBeforeDays(int(req.RenewBeforeDays))
	}
	if req.MissingGrace != "" {
		if _, err := time.ParseDuration(req.MissingGrace); err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("missing_grace 不是合法的 duration: %w", err).Error())
		}
		updater = updater.SetMissingGrace(req.MissingGrace)
	}
	if req.ArchiveAfter != "" {
		if _, err := time.ParseDuration(req.ArchiveAfter); err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("archive_after 不是合法的 duration: %w", err).Error())
		}
		updater = updater.SetArchiveAfter(req.ArchiveAfter)
	}
	if req.DnsResolvers != "" {
		if len(alidns.ParseResolvers(req.DnsResolvers)) == 0 {
			return nil, connect.Errorf(connect.CodeInvalidArgument, "dns_resolvers 至少要有一个合法的 host 或 host:port")
		}
		updater = updater.SetDNSResolvers(req.DnsResolvers)
	}
	if req.ArchivedRetention != "" {
		if _, err := time.ParseDuration(req.ArchivedRetention); err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("archived_retention 不是合法的 duration: %w", err).Error())
		}
		updater = updater.SetArchivedRetention(req.ArchivedRetention)
	}

	if err := updater.Exec(ctx); err != nil {
		s.logger.Error("更新配置失败", zap.Error(err))
		return nil, connect.NewError(connect.CodeInternal, err.Error()).WithCause(err)
	}

	return &certshipv1.UpdateAppSettingsResponse{}, nil
}
