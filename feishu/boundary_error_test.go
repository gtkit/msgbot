package feishu

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/gtkit/msgbot"
)

func TestGetAccessTokenReturnsStructuredHTTPError(t *testing.T) {
	rt := &recordingRoundTripper{responses: []roundTripResponse{
		{status: http.StatusBadGateway, body: `bad gateway`},
	}}
	_, err := GetAccessToken(context.Background(), "cli", "sec", &http.Client{Transport: rt})

	var e *msgbot.Error
	if !errors.As(err, &e) {
		t.Fatalf("want *msgbot.Error, got %T: %v", err, err)
	}
	if e.Kind != msgbot.KindHTTP || e.HTTPStatus != http.StatusBadGateway {
		t.Fatalf("want KindHTTP 502, got kind=%s status=%d", e.Kind, e.HTTPStatus)
	}
	if !e.Retryable {
		t.Fatal("5xx token failure should be retryable")
	}
}

func TestGetAccessTokenRateLimitIsRetryable(t *testing.T) {
	rt := &recordingRoundTripper{responses: []roundTripResponse{
		{status: http.StatusOK, body: `{"code":11232,"msg":"frequency limited"}`},
	}}
	_, err := GetAccessToken(context.Background(), "cli", "sec", &http.Client{Transport: rt})

	var e *msgbot.Error
	if !errors.As(err, &e) || e.Kind != msgbot.KindPlatform || e.Code != 11232 {
		t.Fatalf("want structured platform error 11232, got %v", err)
	}
	if !e.Retryable {
		t.Fatal("Feishu 11232 should be retryable")
	}
}

func TestUploadImageRejectsOversizeStream(t *testing.T) {
	rt := &recordingRoundTripper{}
	// 11MB exceeds the 10MB Feishu limit.
	oversize := strings.NewReader(strings.Repeat("x", 11<<20))
	_, err := UploadImageFromReader(context.Background(), "tenant", "big.png", oversize, &http.Client{Transport: rt})

	var e *msgbot.Error
	if !errors.As(err, &e) || e.Kind != msgbot.KindValidation {
		t.Fatalf("want validation error for oversize image, got %v", err)
	}
	if len(rt.requests) != 0 {
		t.Fatalf("oversize image must be rejected before any request, got %d", len(rt.requests))
	}
}
