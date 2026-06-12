package internal

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestPostJSON(t *testing.T) {
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPost {
			t.Fatalf("want POST, got %s", req.Method)
		}
		if got := req.Header.Get("Content-Type"); got != "application/json; charset=utf-8" {
			t.Fatalf("unexpected content type %q", got)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
			Request:    req,
		}, nil
	})

	data, err := PostJSON(context.Background(), &http.Client{Transport: rt}, "https://example.com", []byte(`{}`))
	if err != nil {
		t.Fatalf("post json: %v", err)
	}
	if string(data) != `{"ok":true}` {
		t.Fatalf("unexpected body %s", data)
	}
}

func TestPostJSONStatusError(t *testing.T) {
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusBadGateway,
			Body:       io.NopCloser(strings.NewReader(`bad gateway`)),
			Request:    req,
		}, nil
	})

	_, err := PostJSON(context.Background(), &http.Client{Transport: rt}, "https://example.com", []byte(`{}`))
	if err == nil || !strings.Contains(err.Error(), "unexpected status 502") {
		t.Fatalf("want status error, got %v", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}
