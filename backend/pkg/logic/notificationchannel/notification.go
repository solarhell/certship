package notificationchannel

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	"go.uber.org/zap"

	certshipv1 "github.com/solarhell/certship/pkg/api/certship/v1"
	"github.com/solarhell/certship/pkg/api/certship/v1/certshipv1connect"
	"github.com/solarhell/certship/pkg/database"
	"github.com/solarhell/certship/pkg/ent"
)

func NewServer() certshipv1connect.NotificationChannelServiceHandler {
	return &Server{logger: zap.L().Named("notification_channel")}
}

type Server struct {
	logger *zap.Logger
}

func (s *Server) ListNotificationChannels(ctx context.Context, _ *connect.Request[certshipv1.ListNotificationChannelsRequest]) (*connect.Response[certshipv1.ListNotificationChannelsResponse], error) {
	channels, err := database.GetClient().NotificationChannel.Query().All(ctx)
	if err != nil {
		s.logger.Error("查询通知渠道失败", zap.Error(err))
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	items := make([]*certshipv1.NotificationChannelItem, 0, len(channels))
	for _, c := range channels {
		items = append(items, &certshipv1.NotificationChannelItem{
			Id:         c.ID,
			Name:       c.Name,
			Type:       string(c.Type),
			WebhookUrl: c.WebhookURL,
			Enabled:    c.Enabled,
			CreatedAt:  c.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		})
	}

	return connect.NewResponse(&certshipv1.ListNotificationChannelsResponse{Channels: items}), nil
}

func (s *Server) CreateNotificationChannel(ctx context.Context, req *connect.Request[certshipv1.CreateNotificationChannelRequest]) (*connect.Response[certshipv1.CreateNotificationChannelResponse], error) {
	if req.Msg.Name == "" || req.Msg.WebhookUrl == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("name 和 webhook_url 不能为空"))
	}

	c, err := database.GetClient().NotificationChannel.Create().
		SetName(req.Msg.Name).
		SetType("feishu").
		SetWebhookURL(req.Msg.WebhookUrl).
		SetEnabled(req.Msg.Enabled).
		Save(ctx)
	if err != nil {
		s.logger.Error("创建通知渠道失败", zap.Error(err))
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&certshipv1.CreateNotificationChannelResponse{Id: c.ID}), nil
}

func (s *Server) UpdateNotificationChannel(ctx context.Context, req *connect.Request[certshipv1.UpdateNotificationChannelRequest]) (*connect.Response[certshipv1.UpdateNotificationChannelResponse], error) {
	if req.Msg.Id == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("id 不能为空"))
	}

	updater := database.GetClient().NotificationChannel.UpdateOneID(req.Msg.Id)
	if req.Msg.Name != "" {
		updater = updater.SetName(req.Msg.Name)
	}
	if req.Msg.WebhookUrl != "" {
		updater = updater.SetWebhookURL(req.Msg.WebhookUrl)
	}
	updater = updater.SetEnabled(req.Msg.Enabled)

	if err := updater.Exec(ctx); err != nil {
		if ent.IsNotFound(err) {
			return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("通知渠道不存在"))
		}
		s.logger.Error("更新通知渠道失败", zap.Error(err))
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&certshipv1.UpdateNotificationChannelResponse{}), nil
}

func (s *Server) DeleteNotificationChannel(ctx context.Context, req *connect.Request[certshipv1.DeleteNotificationChannelRequest]) (*connect.Response[certshipv1.DeleteNotificationChannelResponse], error) {
	if req.Msg.Id == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("id 不能为空"))
	}

	if err := database.GetClient().NotificationChannel.DeleteOneID(req.Msg.Id).Exec(ctx); err != nil {
		if ent.IsNotFound(err) {
			return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("通知渠道不存在"))
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&certshipv1.DeleteNotificationChannelResponse{}), nil
}
