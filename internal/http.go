// Package internal provides shared utilities for msgbot providers.
package internal

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

// PostJSON sends an HTTP POST request with a JSON body and returns the response body.
// It handles context cancellation, request creation, and response reading.
// The response body is limited to 1MB to guard against unexpected large responses.
func PostJSON(ctx context.Context, client *http.Client, url string, body []byte) ([]byte, error) {
	if client == nil {
		client = http.DefaultClient
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", SanitizeRequestError(err))
	}
	defer func() { _ = resp.Body.Close() }()

	return ReadResponse(resp, 1<<20)
}

// HTTPError reports a non-2xx HTTP response. It carries the status code so
// callers can classify the failure (for example, decide whether to retry)
// without parsing the error string. The body is retained for diagnostics, and
// RetryAfter holds the parsed Retry-After header value when the server sent one.
type HTTPError struct {
	StatusCode int
	Body       string
	RetryAfter time.Duration
}

// Error implements error. The wording is kept stable for callers that match it.
func (e *HTTPError) Error() string {
	return fmt.Sprintf("unexpected status %d: %s", e.StatusCode, e.Body)
}

// parseRetryAfter interprets a Retry-After header value, which is either a
// number of seconds or an HTTP date. It returns 0 when the value is absent or
// unparseable.
func parseRetryAfter(v string) time.Duration {
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil {
		if secs < 0 {
			return 0
		}
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}

// ReadResponse reads and validates an HTTP response body.
// The caller remains responsible for closing resp.Body.
// A non-2xx status returns the body together with an *HTTPError.
func ReadResponse(resp *http.Response, maxBody int64) ([]byte, error) {
	if resp == nil {
		return nil, fmt.Errorf("read response: response is nil")
	}
	if resp.Body == nil {
		return nil, fmt.Errorf("read response: body is nil")
	}
	if maxBody <= 0 {
		return nil, fmt.Errorf("read response: max body must be positive")
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if int64(len(data)) > maxBody {
		return nil, fmt.Errorf("read response: body exceeds %d bytes", maxBody)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return data, &HTTPError{
			StatusCode: resp.StatusCode,
			Body:       string(data),
			RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After")),
		}
	}

	return data, nil
}
