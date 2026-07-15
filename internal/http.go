// Package internal provides shared utilities for msgbot providers.
package internal

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
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

// ReadResponse reads and validates an HTTP response body.
// The caller remains responsible for closing resp.Body.
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
		return data, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(data))
	}

	return data, nil
}
