package internal

import (
	"errors"
	"fmt"
	"net/url"
)

// URLOriginForLog returns only the non-sensitive origin of a URL.
// Webhook paths and query strings commonly contain access tokens and signatures.
func URLOriginForLog(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "[redacted]"
	}
	return parsed.Scheme + "://" + parsed.Host
}

// ValidateHTTPURL validates an absolute HTTP(S) endpoint without echoing the
// potentially sensitive raw URL in the returned error.
func ValidateHTTPURL(rawURL string) error {
	parsed, err := url.ParseRequestURI(rawURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("must be an absolute HTTP(S) URL with a host")
	}
	return nil
}

// SanitizeRequestError removes request URLs from net/url transport errors while
// preserving the underlying cause for errors.Is and errors.As checks.
func SanitizeRequestError(err error) error {
	if err == nil {
		return nil
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return fmt.Errorf("%s request failed: %w", urlErr.Op, SanitizeRequestError(urlErr.Err))
	}
	return err
}
