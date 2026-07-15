package wecom

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestGetAccessTokenEncodesCredentials(t *testing.T) {
	const corpID = "corp&admin=true"
	const corpSecret = "secret+value%=x"
	client := &http.Client{Transport: wecomRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		query := req.URL.Query()
		if query.Get("corpid") != corpID || query.Get("corpsecret") != corpSecret {
			t.Fatalf("credentials were not preserved: %q", req.URL.RawQuery)
		}
		if query.Get("admin") != "" {
			t.Fatalf("credential injected query parameter: %q", req.URL.RawQuery)
		}
		return wecomTokenResponse(req, http.StatusOK, `{"errcode":0,"access_token":"token","expires_in":7200}`), nil
	})}

	if _, err := GetAccessToken(context.Background(), corpID, corpSecret, client); err != nil {
		t.Fatalf("get token: %v", err)
	}
}

func TestGetAccessTokenTransportErrorDoesNotLeakCredentials(t *testing.T) {
	const secret = "secret&private=true"
	client := &http.Client{Transport: wecomRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("dial failed")
	})}
	_, err := GetAccessToken(context.Background(), "corp", secret, client)
	if err == nil || !strings.Contains(err.Error(), "dial failed") {
		t.Fatalf("expected transport error, got %v", err)
	}
	if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "corpsecret") {
		t.Fatalf("transport error leaked credentials: %v", err)
	}
}

func TestGetAccessTokenRejectsInvalidHTTPAndEmptyToken(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{name: "http status", status: http.StatusBadGateway, body: `bad gateway`, want: "unexpected status 502"},
		{name: "empty token", status: http.StatusOK, body: `{"errcode":0,"expires_in":7200}`, want: "access_token is empty"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &http.Client{Transport: wecomRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				return wecomTokenResponse(req, tt.status, tt.body), nil
			})}
			_, err := GetAccessToken(context.Background(), "corp", "secret", client)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("want error containing %q, got %v", tt.want, err)
			}
		})
	}
}

func wecomTokenResponse(req *http.Request, status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Request: req}
}
