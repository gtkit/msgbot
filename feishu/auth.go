package feishu

import (
	"context"
	"fmt"
	json "github.com/gtkit/json/v2"
	"io"
	"net/http"
	"os"

	"github.com/gtkit/msgbot/internal"
)

const (
	// AccessTokenAPI 飞书获取自建应用 access token 的 API 地址.
	AccessTokenAPI = "https://open.feishu.cn/open-apis/auth/v3/app_access_token/internal"
)

// AccessToken 飞书自建应用的 access token 信息.
// 通过 GetAccessToken 获取，所有字段在返回后不可变.
type AccessToken struct {
	AppAccessToken    string `json:"app_access_token"`
	TenantAccessToken string `json:"tenant_access_token"`
	Expire            int    `json:"expire"` // 过期时间，单位秒.
}

// accessTokenResp 飞书获取 access token API 的完整响应结构体.
type accessTokenResp struct {
	Code              int    `json:"code"`
	Msg               string `json:"msg"`
	AppAccessToken    string `json:"app_access_token"`
	TenantAccessToken string `json:"tenant_access_token"`
	Expire            int    `json:"expire"`
}

// GetAccessToken 通过 app_id 和 app_secret 获取飞书自建应用的 access token.
// 返回的 AccessToken 包含 AppAccessToken、TenantAccessToken 和 Expire.
// 调用方应缓存返回值，在 Expire 秒后重新获取，避免频繁调用.
func GetAccessToken(ctx context.Context, appID, appSecret string, client ...*http.Client) (*AccessToken, error) {
	if appID == "" || appSecret == "" {
		return nil, fmt.Errorf("feishu: app_id and app_secret are required")
	}

	payload, err := json.Marshal(map[string]string{
		"app_id":     appID,
		"app_secret": appSecret,
	})
	if err != nil {
		return nil, fmt.Errorf("feishu: marshal token request: %w", err)
	}

	httpClient := http.DefaultClient
	if len(client) > 0 && client[0] != nil {
		httpClient = client[0]
	}

	data, err := internal.PostJSON(ctx, httpClient, AccessTokenAPI, payload)
	if err != nil {
		return nil, fmt.Errorf("feishu: get access token: %w", err)
	}

	var resp accessTokenResp
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("feishu: decode token response: %w", err)
	}

	if resp.Code != 0 {
		return nil, fmt.Errorf("feishu: get access token: code=%d, msg=%s", resp.Code, resp.Msg)
	}

	return &AccessToken{
		AppAccessToken:    resp.AppAccessToken,
		TenantAccessToken: resp.TenantAccessToken,
		Expire:            resp.Expire,
	}, nil
}

// TenantToken 返回带 Bearer 前缀的 tenant access token，可直接用于 Authorization header.
func (t *AccessToken) TenantToken() string {
	return "Bearer " + t.TenantAccessToken
}

// AppToken 返回带 Bearer 前缀的 app access token，可直接用于 Authorization header.
func (t *AccessToken) AppToken() string {
	return "Bearer " + t.AppAccessToken
}

// UploadImageWithToken 使用 AccessToken 上传本地图片到飞书，返回 image_key.
// 这是一个便捷方法，等同于 UploadImageFromFile(ctx, t.TenantAccessToken, path).
func (t *AccessToken) UploadImageWithToken(ctx context.Context, path string, client ...*http.Client) (*UploadImageResp, error) {
	return UploadImageFromFile(ctx, t.TenantAccessToken, path, client...)
}

// DownloadImage 使用 AccessToken 从飞书下载图片到本地路径.
func (t *AccessToken) DownloadImage(ctx context.Context, imageKey, savePath string, client ...*http.Client) error {
	if t.TenantAccessToken == "" {
		return fmt.Errorf("feishu: tenant access token is empty")
	}

	api := "https://open.feishu.cn/open-apis/im/v1/images/" + imageKey

	httpClient := http.DefaultClient
	if len(client) > 0 && client[0] != nil {
		httpClient = client[0]
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, api, nil)
	if err != nil {
		return fmt.Errorf("feishu: create download request: %w", err)
	}
	req.Header.Set("Authorization", t.TenantToken())

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("feishu: download request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("feishu: download unexpected status %d", resp.StatusCode)
	}

	const maxImage = 10 << 20
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxImage))
	if err != nil {
		return fmt.Errorf("feishu: read image data: %w", err)
	}

	if err := os.WriteFile(savePath, data, 0o644); err != nil {
		return fmt.Errorf("feishu: write image file: %w", err)
	}

	return nil
}
