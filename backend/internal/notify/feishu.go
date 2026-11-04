package notify

import (
	"context"
	"fmt"

	"github.com/go-lark/lark/v2"
)

type FeishuNotifier struct {
	webhookURL string
}

func NewFeishuNotifier(webhookURL string) *FeishuNotifier {
	return &FeishuNotifier{webhookURL: webhookURL}
}

func (f *FeishuNotifier) SendText(ctx context.Context, text string) error {
	bot := lark.NewNotificationBot(f.webhookURL)
	resp, err := bot.PostNotification(ctx, lark.NewMsgBuffer(lark.MsgText).Text(text).Build())
	if err != nil {
		return err
	}
	if resp.Code != 0 {
		return fmt.Errorf("飞书通知失败: code=%d msg=%s", resp.Code, resp.Msg)
	}
	return nil
}
