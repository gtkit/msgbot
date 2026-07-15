package msgbot

import (
	"context"
	"errors"

	json "github.com/gtkit/json/v2"

	"github.com/gtkit/msgbot/internal"
)

// BuildRequest produces one send attempt: the target URL and the payload to
// marshal as JSON. It runs once per attempt so time-sensitive signing (such as
// the DingTalk timestamp or the Feishu sign) is regenerated on every retry.
// Returning an error fails the send immediately without retrying.
type BuildRequest func() (url string, payload any, err error)

// Send posts a JSON payload using the config's HTTP client and retry policy,
// then decodes a generic platform Response and classifies any failure as a
// *Error. It records success/error counts on stats. platform and op label the
// resulting error and debug logs.
//
// This is the shared send path for the platform webhook providers; end users
// normally call the provider Send* methods rather than Send directly.
func (c *Config) Send(ctx context.Context, stats *Stats, platform Platform, op string, build BuildRequest) error {
	// Stats are recorded once per send task (not per retry attempt); per-attempt
	// failures are still logged for diagnostics.
	err := c.Retry.do(ctx, func(ctx context.Context) error {
		url, payload, err := build()
		if err != nil {
			if e, ok := errors.AsType[*Error](err); ok {
				return e
			}
			return &Error{Platform: platform, Operation: op, Kind: KindValidation, Message: err.Error(), Err: err}
		}

		body, err := json.Marshal(payload)
		if err != nil {
			return &Error{Platform: platform, Operation: op, Kind: KindValidation, Message: "marshal payload", Err: err}
		}

		c.LogDebug(ctx, string(platform)+": sending message", "endpoint", internal.URLOriginForLog(url))

		data, err := internal.PostJSON(ctx, c.GetHTTPClient(), url, body)
		if err != nil {
			e := classifySendError(platform, op, err)
			c.LogError(ctx, string(platform)+": send failed", "error", e)
			return e
		}

		var resp Response
		if err := json.Unmarshal(data, &resp); err != nil {
			c.LogError(ctx, string(platform)+": decode response failed", "error", err)
			return &Error{Platform: platform, Operation: op, Kind: KindDecode, Message: "decode response", Err: err}
		}
		if code := resp.code(); code != 0 {
			e := &Error{
				Platform:  platform,
				Operation: op,
				Kind:      KindPlatform,
				Code:      code,
				Message:   resp.message(),
				Retryable: platformRetryable(platform, code),
			}
			c.LogError(ctx, string(platform)+": api error", "error", e)
			return e
		}
		return nil
	})

	if err != nil {
		stats.IncError()
		return err
	}
	stats.IncSent()
	return nil
}
