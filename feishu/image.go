package feishu

import (
	"bytes"
	"context"
	"fmt"
	json "github.com/gtkit/json/v2"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"

	news "github.com/gtkit/msgbot"
)

const (
	// FeishuUploadImageAPI 飞书上传图片 API 地址.
	FeishuUploadImageAPI = "https://open.feishu.cn/open-apis/im/v1/images"
)

// UploadImageResp 飞书上传图片 API 的完整响应结构体.
type UploadImageResp struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		ImageKey string `json:"image_key"`
	} `json:"data"`
	Error struct {
		Message              string `json:"message"`
		LogID                string `json:"log_id"`
		PermissionViolations []struct {
			Type    string `json:"type"`
			Subject string `json:"subject"`
		} `json:"permission_violations"`
		Helps []struct {
			URL         string `json:"url"`
			Description string `json:"description"`
		} `json:"helps"`
	} `json:"error,omitempty"`
}

// ImageKey 返回上传成功后的 image_key.
func (r *UploadImageResp) ImageKey() string {
	return r.Data.ImageKey
}

// UploadImageFromFile 从本地文件路径上传图片到飞书，返回 image_key.
// tenantAccessToken 由调用方传入（如通过 fs.NewInternalApp 获取）.
func UploadImageFromFile(ctx context.Context, tenantAccessToken, path string, client ...*http.Client) (*UploadImageResp, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("feishu: open file %s: %w", path, err)
	}
	defer func() { _ = file.Close() }()

	return UploadImageFromReader(ctx, tenantAccessToken, filepath.Base(path), file, client...)
}

// UploadImageFromReader 从 io.Reader 上传图片到飞书，返回 image_key.
// 支持传入 []byte 包装的 bytes.Reader、文件句柄等任意 Reader.
// tenantAccessToken 由调用方传入.
func UploadImageFromReader(ctx context.Context, tenantAccessToken, filename string, reader io.Reader, client ...*http.Client) (*UploadImageResp, error) {
	if tenantAccessToken == "" {
		return nil, fmt.Errorf("feishu: tenant access token is required for upload")
	}

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	part, err := writer.CreateFormFile("image", filename)
	if err != nil {
		return nil, fmt.Errorf("feishu: create form file: %w", err)
	}

	if _, err = io.Copy(part, reader); err != nil {
		return nil, fmt.Errorf("feishu: copy image data: %w", err)
	}

	_ = writer.WriteField("image_type", "message")
	if err = writer.Close(); err != nil {
		return nil, fmt.Errorf("feishu: close multipart writer: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, FeishuUploadImageAPI, body)
	if err != nil {
		return nil, fmt.Errorf("feishu: create upload request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+tenantAccessToken)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	httpClient := http.DefaultClient
	if len(client) > 0 && client[0] != nil {
		httpClient = client[0]
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("feishu: upload request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	const maxBody = 1 << 20
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, fmt.Errorf("feishu: read upload response: %w", err)
	}

	var uploadResp UploadImageResp
	if err := json.Unmarshal(data, &uploadResp); err != nil {
		return nil, fmt.Errorf("feishu: decode upload response: %w", err)
	}

	if uploadResp.Code != 0 {
		return nil, fmt.Errorf("feishu: upload image: code=%d, msg=%s", uploadResp.Code, uploadResp.Msg)
	}

	return &uploadResp, nil
}

// SendImageFromFile 上传本地图片并直接发送到飞书群（webhook 方式）.
// 内部先调用 UploadImageFromFile 获取 image_key，再通过 webhook 发送.
func (w *Webhook) SendImageFromFile(ctx context.Context, tenantAccessToken, path string) error {
	uploadResp, err := UploadImageFromFile(ctx, tenantAccessToken, path, w.cfg.GetHTTPClient())
	if err != nil {
		return err
	}

	return w.SendImage(ctx, &news.ImageMessage{
		ImageKey: uploadResp.ImageKey(),
	})
}
