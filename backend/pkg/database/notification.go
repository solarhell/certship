package database

import (
	"context"

	"go.uber.org/zap"

	"github.com/solarhell/certship/internal/notify"
	"github.com/solarhell/certship/pkg/ent"
	entnotification "github.com/solarhell/certship/pkg/ent/notificationchannel"
)

// SeedNotificationChannel 写入初始飞书通知渠道（幂等）
func SeedNotificationChannel(ctx context.Context, name, webhookURL string) error {
	db := GetClient()
	existing, err := db.NotificationChannel.
		Query().
		Where(
			entnotification.TypeEQ(entnotification.TypeFeishu),
			entnotification.NameEQ(name),
		).
		Only(ctx)

	if ent.IsNotFound(err) {
		return db.NotificationChannel.Create().
			SetName(name).
			SetType(entnotification.TypeFeishu).
			SetWebhookURL(webhookURL).
			SetEnabled(true).
			Exec(ctx)
	} else if err == nil {
		return existing.Update().
			SetWebhookURL(webhookURL).
			Exec(ctx)
	}
	return err
}

// SendToAllFeishuChannels 查询所有启用的飞书渠道并发送文本通知
func SendToAllFeishuChannels(ctx context.Context, text string, logger *zap.Logger) {
	channels, err := GetClient().NotificationChannel.
		Query().
		Where(
			entnotification.TypeEQ(entnotification.TypeFeishu),
			entnotification.EnabledEQ(true),
		).
		All(ctx)
	if err != nil {
		logger.Warn("查询通知渠道失败", zap.Error(err))
		return
	}
	for _, ch := range channels {
		if err := notify.NewFeishuNotifier(ch.WebhookURL).SendText(text); err != nil {
			logger.Warn("飞书通知发送失败",
				zap.String("channel", ch.Name),
				zap.Error(err),
			)
		}
	}
}
