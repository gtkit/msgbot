package dingtalk

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/gtkit/json/v2"

	"github.com/gtkit/msgbot/internal"
)

const (
	// AccessTokenAPI 钉钉获取企业内部应用 access_token 的 API 地址.
	AccessTokenAPI = "https://oapi.dingtalk.com/gettoken"
)

// AccessToken 钉钉的 access_token 信息.
// 通过 GetAccessToken 获取，所有字段在返回后不可变.
type AccessToken struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"` // 过期时间，单位秒，通常为 7200.
}

// accessTokenResp 钉钉获取 access_token API 的完整响应结构体.
type accessTokenResp struct {
	ErrCode     int    `json:"errcode"`
	ErrMsg      string `json:"errmsg"`
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
}

// GetAccessToken 通过 appKey 和 appSecret 获取钉钉企业内部应用的 access_token.
// 返回的 AccessToken 包含 AccessToken 和 ExpiresIn（通常为 7200 秒）.
// 调用方应缓存返回值，有效期内重复获取会返回相同结果并自动续期.
func GetAccessToken(ctx context.Context, appKey, appSecret string, client ...*http.Client) (*AccessToken, error) {
	if appKey == "" || appSecret == "" {
		return nil, fmt.Errorf("dingtalk: appkey and appsecret are required")
	}

	endpoint, err := url.Parse(AccessTokenAPI)
	if err != nil {
		return nil, fmt.Errorf("dingtalk: parse token endpoint: %w", err)
	}
	query := endpoint.Query()
	query.Set("appkey", appKey)
	query.Set("appsecret", appSecret)
	endpoint.RawQuery = query.Encode()

	httpClient := internal.PickClient(internal.DefaultClient(), client)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("dingtalk: create token request: %w", err)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("dingtalk: send token request: %w", internal.SanitizeRequestError(err))
	}
	defer func() { _ = resp.Body.Close() }()

	data, err := internal.ReadResponse(resp, 1<<20)
	if err != nil {
		return nil, fmt.Errorf("dingtalk: token response: %w", err)
	}

	var result accessTokenResp
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("dingtalk: decode token response: %w", err)
	}

	if result.ErrCode != 0 {
		return nil, fmt.Errorf("dingtalk: get access token: errcode=%d, errmsg=%s", result.ErrCode, result.ErrMsg)
	}
	if result.AccessToken == "" {
		return nil, fmt.Errorf("dingtalk: get access token: access_token is empty")
	}

	return &AccessToken{
		AccessToken: result.AccessToken,
		ExpiresIn:   result.ExpiresIn,
	}, nil
}

// Token 返回 access_token 字符串.
func (t *AccessToken) Token() string {
	return t.AccessToken
}
