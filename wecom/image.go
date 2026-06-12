package wecom

import (
	"context"
	"crypto/md5"
	"encoding/base64"
	"fmt"
	"os"

	news "github.com/gtkit/msgbot"
)

// SendImageFromFile 读取本地文件并以 base64+md5 方式发送图片到企业微信群.
// 企业微信 webhook 图片消息要求 base64 编码和 md5 哈希，无需独立上传接口.
func (w *Webhook) SendImageFromFile(ctx context.Context, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("wecom: read image file %s: %w", path, err)
	}

	return w.SendImage(ctx, &news.ImageMessage{
		Base64: base64.StdEncoding.EncodeToString(data),
		MD5:    fmt.Sprintf("%x", md5.Sum(data)),
	})
}

// BuildImageMessage 从原始字节构建企业微信 ImageMessage（base64+md5）.
// 可独立调用获取 ImageMessage 后自行发送.
func BuildImageMessage(data []byte) *news.ImageMessage {
	return &news.ImageMessage{
		Base64: base64.StdEncoding.EncodeToString(data),
		MD5:    fmt.Sprintf("%x", md5.Sum(data)),
	}
}
