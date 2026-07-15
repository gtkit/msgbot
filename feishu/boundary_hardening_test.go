package feishu

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestGetAccessTokenRejectsInvalidHTTPAndEmptyToken(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{name: "http status", status: http.StatusBadGateway, body: `bad gateway`, want: "unexpected status 502"},
		{name: "empty token", status: http.StatusOK, body: `{"code":0,"expire":7200}`, want: "tenant_access_token is empty"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &http.Client{Transport: hardeningRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				return hardeningResponse(req, tt.status, tt.body), nil
			})}
			_, err := GetAccessToken(context.Background(), "app", "secret", client)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("want error containing %q, got %v", tt.want, err)
			}
		})
	}
}

func TestDownloadImageValidatesAndEscapesImageKey(t *testing.T) {
	token := &AccessToken{TenantAccessToken: "tenant"}
	if err := token.DownloadImage(context.Background(), "", t.TempDir()+"/image", nil); err == nil {
		t.Fatal("expected empty image key error")
	}

	client := &http.Client{Transport: hardeningRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		const wantPath = "/open-apis/im/v1/images/folder%2Fkey%3Fx=1"
		if got := req.URL.EscapedPath(); got != wantPath {
			t.Fatalf("want escaped path %q, got %q", wantPath, got)
		}
		return hardeningResponse(req, http.StatusOK, "image"), nil
	})}

	if err := token.DownloadImage(context.Background(), "folder/key?x=1", t.TempDir()+"/image", client); err != nil {
		t.Fatalf("download image: %v", err)
	}
}

func TestUploadImageRejectsInvalidStatusAndEmptyImageKey(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{name: "http status", status: http.StatusBadGateway, body: `bad gateway`, want: "unexpected status 502"},
		{name: "empty image key", status: http.StatusOK, body: `{"code":0,"data":{}}`, want: "image_key is empty"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &http.Client{Transport: hardeningRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				return hardeningResponse(req, tt.status, tt.body), nil
			})}
			_, err := UploadImageFromReader(context.Background(), "tenant", "image.png", bytes.NewReader([]byte("image")), client)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("want error containing %q, got %v", tt.want, err)
			}
		})
	}
}

type hardeningRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn hardeningRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func hardeningResponse(req *http.Request, status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Request: req}
}
