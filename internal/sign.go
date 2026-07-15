package internal

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/url"
	"strconv"
	"time"
)

// FeishuSign 为飞书 webhook 生成 HMAC-SHA256 签名。
// 算法：base64(HMAC-SHA256(timestamp + "\n" + secret, ""))。
func FeishuSign(secret string, timestamp int64) (string, error) {
	stringToSign := fmt.Sprintf("%d\n%s", timestamp, secret)
	h := hmac.New(sha256.New, []byte(stringToSign))
	// hmac 密钥已通过 New() 设置；h.Sum 直接计算摘要。
	return base64.StdEncoding.EncodeToString(h.Sum(nil)), nil
}

// DingTalkSignedURL 向 webhook URL 添加 timestamp 和 HMAC-SHA256 的 sign
// query 参数。算法：base64(HMAC-SHA256(secret, timestamp + "\n" + secret))。
// query 处理经由 net/url，因此无论 base URL 是否已带 query 字符串，结果都正确。
func DingTalkSignedURL(webhookURL, secret string) (string, error) {
	parsed, err := url.Parse(webhookURL)
	if err != nil {
		return "", fmt.Errorf("parse webhook url: %w", err)
	}

	ts := time.Now().UnixMilli()
	stringToSign := strconv.FormatInt(ts, 10) + "\n" + secret

	h := hmac.New(sha256.New, []byte(secret))
	if _, err := h.Write([]byte(stringToSign)); err != nil {
		return "", fmt.Errorf("hmac write: %w", err)
	}
	sign := base64.StdEncoding.EncodeToString(h.Sum(nil))

	query := parsed.Query()
	query.Set("timestamp", strconv.FormatInt(ts, 10))
	query.Set("sign", sign)
	parsed.RawQuery = query.Encode()

	return parsed.String(), nil
}
