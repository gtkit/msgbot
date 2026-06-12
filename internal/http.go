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
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Webhook responses are tiny JSON; 1MB is more than enough.
	const maxBody = 1 << 20
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return data, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(data))
	}

	return data, nil
}
