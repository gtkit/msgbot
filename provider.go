// Package msgbot provides a unified multi-platform messaging provider
// for Feishu (Lark), WeCom (WeChat Work), and DingTalk webhook robots.
// All Provider implementations are safe for concurrent use.
package msgbot

import (
	"context"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"
)

// Provider defines the unified interface for sending messages
// across different IM platforms via webhook.
type Provider interface {
	// SendText sends a plain text message to the webhook endpoint.
	SendText(ctx context.Context, text string, opts ...SendOption) error
	// SendMarkdown sends a markdown-formatted message to the webhook endpoint.
	SendMarkdown(ctx context.Context, title, content string, opts ...SendOption) error
	// SendRichText sends a rich text (post) message to the webhook endpoint.
	// Feishu supports this natively; other platforms degrade to markdown.
	SendRichText(ctx context.Context, msg *RichTextMessage) error
	// SendImage sends an image message to the webhook endpoint.
	// Feishu uses image_key, WeCom uses base64+md5, DingTalk uses picURL in markdown.
	SendImage(ctx context.Context, img *ImageMessage) error
	// Platform returns the platform identifier of this provider.
	Platform() Platform
}

// Platform represents a supported IM platform.
type Platform string

const (
	PlatformFeishu   Platform = "feishu"
	PlatformWeCom    Platform = "wecom"
	PlatformDingTalk Platform = "dingtalk"
)

// SendOption applies optional parameters to a message.
type SendOption func(*SendOptions)

// SendOptions holds the resolved send options.
type SendOptions struct {
	AtAll     bool     // Whether to mention all members.
	AtUserIDs []string // Platform-specific user identifiers to mention.
}

// WithAtAll enables mentioning all members in the group.
func WithAtAll() SendOption {
	return func(o *SendOptions) {
		o.AtAll = true
	}
}

// WithAtUsers specifies the users to mention.
// Feishu: user_id or open_id list; WeCom: userid list; DingTalk: mobile list.
func WithAtUsers(ids ...string) SendOption {
	return func(o *SendOptions) {
		o.AtUserIDs = append(o.AtUserIDs, ids...)
	}
}

// ApplySendOptions resolves a list of SendOption into a SendOptions struct.
func ApplySendOptions(opts []SendOption) *SendOptions {
	o := &SendOptions{}
	for _, fn := range opts {
		fn(o)
	}
	return o
}

// RichTextMessage represents a rich text (post) message,
// primarily used by Feishu. Other platforms degrade to markdown.
type RichTextMessage struct {
	Title   string          // Message title.
	Content [][]RichTextTag // Lines of rich text elements.
}

// RichTextTag represents a single element in a rich text line.
type RichTextTag struct {
	Tag    string // Element type: "text", "a", "at", "img".
	Text   string // Text content (for "text" and "a" tags).
	Href   string // Hyperlink URL (for "a" tag).
	UserID string // User ID to mention (for "at" tag; "all" = everyone).
	ImgKey string // Image key (for "img" tag, Feishu only).
}

// ImageMessage represents an image to be sent.
// Different platforms require different fields.
type ImageMessage struct {
	ImageKey string // Feishu: image_key obtained after uploading via Feishu API.
	Base64   string // WeCom: base64-encoded image data (no newlines, no prefix).
	MD5      string // WeCom: md5 hash of the raw image bytes.
	PicURL   string // DingTalk: publicly accessible image URL.
}

// Logger defines a minimal logging interface for debugging. It is intentionally
// small so callers can adapt any logging library in a few lines without this
// package depending on one. If nil, no debug logs are emitted.
type Logger interface {
	// DebugContext logs a debug message with context.
	DebugContext(ctx context.Context, msg string, args ...any)
	// ErrorContext logs an error message with context.
	ErrorContext(ctx context.Context, msg string, args ...any)
}

// Config holds the common configuration for a webhook provider.
// Config is designed to be passed by value; modifications after
// passing to New() do not affect the created provider.
type Config struct {
	WebhookURL string        // Required: webhook URL of the robot.
	Secret     string        // Optional: signing secret for request verification.
	HTTPClient *http.Client  // Optional: custom HTTP client; uses default if nil.
	Timeout    time.Duration // Optional: request timeout; defaults to 10s when HTTPClient is nil.
	Logger     Logger        // Optional: logger for debug/error messages; nil disables logging.
	Retry      RetryPolicy   // Optional: retry policy for failed sends; zero value disables retry.

	// frozen is the resolved HTTP client, set once by Freeze().
	// After freezing, GetHTTPClient() always returns this same instance.
	frozen *http.Client
}

// Freeze resolves the HTTP client once and stores it internally.
// This must be called during provider construction (New) so that
// GetHTTPClient() never allocates a new client on the hot path.
func (c *Config) Freeze() {
	if c.HTTPClient != nil {
		c.frozen = c.HTTPClient
		return
	}
	timeout := c.Timeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	c.frozen = &http.Client{Timeout: timeout}
}

// GetHTTPClient returns the frozen HTTP client.
// Freeze() must have been called beforehand (all New functions do this).
func (c *Config) GetHTTPClient() *http.Client {
	return c.frozen
}

// LogDebug logs a debug message if the logger is configured.
func (c *Config) LogDebug(ctx context.Context, msg string, args ...any) {
	if c.Logger != nil {
		c.Logger.DebugContext(ctx, msg, args...)
	}
}

// LogError logs an error message if the logger is configured.
func (c *Config) LogError(ctx context.Context, msg string, args ...any) {
	if c.Logger != nil {
		c.Logger.ErrorContext(ctx, msg, args...)
	}
}

// Response represents a generic API response from IM platforms.
type Response struct {
	Code    int    `json:"code,omitempty"`
	ErrCode int    `json:"errcode,omitempty"`
	ErrMsg  string `json:"errmsg,omitempty"`
	Msg     string `json:"msg,omitempty"`
}

// code returns the effective business code, preferring Code over ErrCode.
func (r *Response) code() int {
	if r.Code != 0 {
		return r.Code
	}
	return r.ErrCode
}

// message returns the effective message, preferring Msg over ErrMsg.
func (r *Response) message() string {
	if r.Msg != "" {
		return r.Msg
	}
	return r.ErrMsg
}

// Err returns a non-nil error if the response indicates failure.
func (r *Response) Err() error {
	if code := r.code(); code != 0 {
		return fmt.Errorf("api error: code=%d, msg=%s", code, r.message())
	}
	return nil
}

// Stats tracks provider-level metrics using atomic operations for lock-free,
// concurrent-safe access without any mutex overhead.
type Stats struct {
	totalSent  atomic.Int64
	totalError atomic.Int64
}

// IncSent increments the successful send counter atomically.
func (s *Stats) IncSent() { s.totalSent.Add(1) }

// IncError increments the error counter atomically.
func (s *Stats) IncError() { s.totalError.Add(1) }

// TotalSent returns the total number of successfully sent messages.
func (s *Stats) TotalSent() int64 { return s.totalSent.Load() }

// TotalError returns the total number of failed message attempts.
func (s *Stats) TotalError() int64 { return s.totalError.Load() }
