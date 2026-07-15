package wecom

import (
	"context"
	"crypto/md5"
	"encoding/base64"
	"fmt"
	"os"

	"github.com/gtkit/msgbot"
)

// wecomImageMaxBytes 企业微信 webhook 图片消息大小上限（base64 编码前 2MB）。
// 读文件前先校验大小，避免把超大文件整体读入内存再叠加 base64/JSON 副本。
const wecomImageMaxBytes = 2 << 20

// SendImageFromFile 读取本地文件并以 base64+md5 方式发送图片到企业微信群.
// 企业微信 webhook 图片消息要求 base64 编码和 md5 哈希，无需独立上传接口.
func (w *Webhook) SendImageFromFile(ctx context.Context, path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("wecom: stat image file %s: %w", path, err)
	}
	if info.Size() > wecomImageMaxBytes {
		return msgbot.ValidationError(msgbot.PlatformWeCom, "SendImageFromFile", "image exceeds 2MB limit", nil)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("wecom: read image file %s: %w", path, err)
	}

	return w.SendImage(ctx, &msgbot.ImageMessage{
		Base64: base64.StdEncoding.EncodeToString(data),
		MD5:    fmt.Sprintf("%x", md5.Sum(data)),
	})
}

// BuildImageMessage 从原始字节构建企业微信 ImageMessage（base64+md5）.
// 可独立调用获取 ImageMessage 后自行发送.
func BuildImageMessage(data []byte) *msgbot.ImageMessage {
	return &msgbot.ImageMessage{
		Base64: base64.StdEncoding.EncodeToString(data),
		MD5:    fmt.Sprintf("%x", md5.Sum(data)),
	}
}
