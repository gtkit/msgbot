package internal

import (
	"net/http"
	"time"
)

// Default timeouts for non-webhook HTTP calls that fall back to the shared
// clients below. A caller-supplied *http.Client or a context deadline always
// takes precedence over these values.
const (
	// DefaultTimeout bounds token and application-message requests.
	DefaultTimeout = 10 * time.Second
	// DefaultUploadTimeout bounds image upload and download requests, which
	// have a different latency profile than small JSON calls.
	DefaultUploadTimeout = 30 * time.Second
)

// Package-level shared clients so connections are reused across calls.
// http.DefaultClient has no timeout and must never be used as the fallback.
var (
	defaultClient = &http.Client{Timeout: DefaultTimeout}
	uploadClient  = &http.Client{Timeout: DefaultUploadTimeout}
)

// DefaultClient returns the shared client with the standard timeout, used as
// the fallback for token and message requests when the caller passes none.
func DefaultClient() *http.Client { return defaultClient }

// DefaultUploadClient returns the shared client with the longer timeout, used
// as the fallback for image upload and download when the caller passes none.
func DefaultUploadClient() *http.Client { return uploadClient }

// PickClient returns the first non-nil caller-supplied client, or fallback.
func PickClient(fallback *http.Client, client []*http.Client) *http.Client {
	if len(client) > 0 && client[0] != nil {
		return client[0]
	}
	return fallback
}
