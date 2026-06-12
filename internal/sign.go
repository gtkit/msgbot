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

// FeishuSign generates an HMAC-SHA256 signature for Feishu webhook.
// The algorithm: base64(HMAC-SHA256(timestamp + "\n" + secret, "")).
func FeishuSign(secret string, timestamp int64) (string, error) {
	stringToSign := fmt.Sprintf("%d\n%s", timestamp, secret)
	h := hmac.New(sha256.New, []byte(stringToSign))
	// hmac key is set via New(); h.Sum computes the digest directly.
	return base64.StdEncoding.EncodeToString(h.Sum(nil)), nil
}

// DingTalkSignedURL appends timestamp and HMAC-SHA256 sign to the webhook URL.
// The algorithm: base64(HMAC-SHA256(secret, timestamp + "\n" + secret)).
func DingTalkSignedURL(webhookURL, secret string) (string, error) {
	ts := time.Now().UnixMilli()
	stringToSign := strconv.FormatInt(ts, 10) + "\n" + secret

	h := hmac.New(sha256.New, []byte(secret))
	if _, err := h.Write([]byte(stringToSign)); err != nil {
		return "", fmt.Errorf("hmac write: %w", err)
	}
	sign := base64.StdEncoding.EncodeToString(h.Sum(nil))

	return fmt.Sprintf("%s&timestamp=%d&sign=%s", webhookURL, ts, url.QueryEscape(sign)), nil
}
