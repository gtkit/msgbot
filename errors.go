package msgbot

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gtkit/msgbot/internal"
)

// ErrorKind classifies a failure so callers can branch on the failure type
// without matching error strings.
type ErrorKind string

const (
	// KindValidation means local input was rejected before any request was sent.
	KindValidation ErrorKind = "validation"
	// KindTransport means a network/transport failure (dial, reset, timeout, cancellation).
	KindTransport ErrorKind = "transport"
	// KindHTTP means a non-2xx HTTP response was received.
	KindHTTP ErrorKind = "http"
	// KindPlatform means the platform returned HTTP 200 with a business error code.
	KindPlatform ErrorKind = "platform"
	// KindDecode means the response body could not be decoded.
	KindDecode ErrorKind = "decode"
)

// Error is the structured error returned by outbound operations. Inspect it with
// errors.As(err, &e) where e is a *Error, then branch on Kind, HTTPStatus, Code,
// and Retryable. The wrapped cause is preserved for errors.Is / errors.As, so
// errors.Is(err, context.Canceled) and similar checks work through it.
//
// Error never includes the webhook URL, credentials, or signatures in its message.
type Error struct {
	Platform   Platform      // Originating platform, empty if not platform-specific.
	Operation  string        // Logical operation, e.g. "SendText".
	Kind       ErrorKind     // Failure classification.
	HTTPStatus int           // HTTP status for KindHTTP, otherwise 0.
	Code       int           // Platform business code for KindPlatform, otherwise 0.
	Message    string        // Human-readable detail without secrets.
	RetryAfter time.Duration // Server-advised backoff, 0 if none.
	Retryable  bool          // Whether retrying the operation may succeed.
	Err        error         // Wrapped cause, may be nil.
}

// Error implements the error interface.
func (e *Error) Error() string {
	var b strings.Builder
	b.WriteString("msgbot")
	if e.Platform != "" {
		b.WriteString("/")
		b.WriteString(string(e.Platform))
	}
	if e.Operation != "" {
		b.WriteString(" ")
		b.WriteString(e.Operation)
	}
	b.WriteString(": ")
	b.WriteString(string(e.Kind))
	if e.HTTPStatus != 0 {
		b.WriteString(" (status ")
		b.WriteString(strconv.Itoa(e.HTTPStatus))
		b.WriteString(")")
	}
	if e.Code != 0 {
		b.WriteString(" (code ")
		b.WriteString(strconv.Itoa(e.Code))
		b.WriteString(")")
	}
	if e.Message != "" {
		b.WriteString(": ")
		b.WriteString(e.Message)
	}
	if e.Err != nil {
		b.WriteString(": ")
		b.WriteString(e.Err.Error())
	}
	return b.String()
}

// Unwrap returns the wrapped cause for errors.Is / errors.As.
func (e *Error) Unwrap() error { return e.Err }

// ValidationError returns a *Error of kind validation for locally rejected
// input. Provider packages use it so validation failures are classifiable
// (Kind == KindValidation) and never retried. cause may be nil.
func ValidationError(platform Platform, op, msg string, cause error) *Error {
	return &Error{Platform: platform, Operation: op, Kind: KindValidation, Message: msg, Err: cause}
}

// isRetryable reports whether err is a structured *Error marked retryable.
func isRetryable(err error) bool {
	if e, ok := errors.AsType[*Error](err); ok {
		return e.Retryable
	}
	return false
}

// classifySendError converts a raw error from an internal HTTP helper into a
// structured *Error, preserving the cause. Context cancellation and deadline
// are transport failures that are never retried; a recognized non-2xx status is
// KindHTTP with a status-based retry decision; anything else is treated as a
// retryable transport failure.
func classifySendError(platform Platform, op string, err error) *Error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return &Error{Platform: platform, Operation: op, Kind: KindTransport, Retryable: false, Err: err}
	}
	if he, ok := errors.AsType[*internal.HTTPError](err); ok {
		return &Error{
			Platform:   platform,
			Operation:  op,
			Kind:       KindHTTP,
			HTTPStatus: he.StatusCode,
			RetryAfter: he.RetryAfter,
			Retryable:  httpRetryable(he.StatusCode),
			Err:        err,
		}
	}
	return &Error{Platform: platform, Operation: op, Kind: KindTransport, Retryable: true, Err: err}
}

// httpRetryable reports whether an HTTP status is worth retrying: 408, 425, 429,
// or any 5xx.
func httpRetryable(status int) bool {
	switch status {
	case http.StatusRequestTimeout, http.StatusTooEarly, http.StatusTooManyRequests:
		return true
	}
	return status >= 500 && status <= 599
}

// platformRetryable reports whether a platform business error code returned over
// HTTP 200 is a rate-limit/transient code worth retrying. Codes are transcribed
// from each platform's official documentation:
//   - Feishu 11232: message creation hit the system frequency limit.
//   - WeCom 45009: api call frequency limit; 45033: concurrent call limit.
//   - DingTalk 130101: send too fast (exceeds 20 messages/minute per robot).
func platformRetryable(platform Platform, code int) bool {
	switch platform {
	case PlatformFeishu:
		return code == 11232
	case PlatformWeCom:
		return code == 45009 || code == 45033
	case PlatformDingTalk:
		return code == 130101
	}
	return false
}
