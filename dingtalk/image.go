package dingtalk

import (
	"context"
	"fmt"

	news "github.com/gtkit/msgbot"
)

// SendImageFromURL 通过公网图片 URL 发送图片消息到钉钉群.
// 钉钉自定义 webhook 机器人不支持上传图片，只能通过 markdown 嵌入公网可访问的图片 URL.
func (w *Webhook) SendImageFromURL(ctx context.Context, picURL string) error {
	if picURL == "" {
		return fmt.Errorf("dingtalk: picURL is required")
	}
	return w.SendImage(ctx, &news.ImageMessage{PicURL: picURL})
}
