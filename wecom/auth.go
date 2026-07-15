package wecom

import (
	"context"
	"net/http"
	"net/url"

	json "github.com/gtkit/json/v2"

	"github.com/gtkit/msgbot"
	"github.com/gtkit/msgbot/internal"
)

const (
	// AccessTokenAPI 企业微信获取 access_token 的 API 地址.
	AccessTokenAPI = "https://qyapi.weixin.qq.com/cgi-bin/gettoken"
)

// AccessToken 企业微信的 access_token 信息.
// 通过 GetAccessToken 获取，所有字段在返回后不可变.
type AccessToken struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"` // 过期时间，单位秒，通常为 7200.
}

// accessTokenResp 企业微信获取 access_token API 的完整响应结构体.
type accessTokenResp struct {
	ErrCode     int    `json:"errcode"`
	ErrMsg      string `json:"errmsg"`
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
}

// GetAccessToken 通过 corpid 和 corpsecret 获取企业微信的 access_token.
// 返回的 AccessToken 包含 AccessToken 和 ExpiresIn（通常为 7200 秒）.
// 调用方应缓存返回值，避免频繁调用（否则会被企业微信频率限制）.
func GetAccessToken(ctx context.Context, corpID, corpSecret string, client ...*http.Client) (*AccessToken, error) {
	if corpID == "" || corpSecret == "" {
		return nil, msgbot.ValidationError(msgbot.PlatformWeCom, "GetAccessToken", "corpid and corpsecret are required", nil)
	}

	endpoint, err := url.Parse(AccessTokenAPI)
	if err != nil {
		return nil, msgbot.ValidationError(msgbot.PlatformWeCom, "GetAccessToken", "parse token endpoint", err)
	}
	query := endpoint.Query()
	query.Set("corpid", corpID)
	query.Set("corpsecret", corpSecret)
	endpoint.RawQuery = query.Encode()

	httpClient := internal.PickClient(internal.DefaultClient(), client)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, msgbot.ValidationError(msgbot.PlatformWeCom, "GetAccessToken", "create token request", err)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, msgbot.WrapError(msgbot.PlatformWeCom, "GetAccessToken", internal.SanitizeRequestError(err))
	}
	defer func() { _ = resp.Body.Close() }()

	data, err := internal.ReadResponse(resp, 1<<20)
	if err != nil {
		return nil, msgbot.WrapError(msgbot.PlatformWeCom, "GetAccessToken", err)
	}

	var result accessTokenResp
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, msgbot.DecodeError(msgbot.PlatformWeCom, "GetAccessToken", "decode token response", err)
	}

	if result.ErrCode != 0 {
		return nil, msgbot.PlatformError(msgbot.PlatformWeCom, "GetAccessToken", result.ErrCode, result.ErrMsg)
	}
	if result.AccessToken == "" {
		return nil, msgbot.DecodeError(msgbot.PlatformWeCom, "GetAccessToken", "access_token is empty", nil)
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
