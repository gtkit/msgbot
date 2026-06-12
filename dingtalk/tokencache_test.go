package dingtalk

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestGetAccessTokenCachedEmptyCredentials(t *testing.T) {
	cache := NewMemoryTokenCache()
	// 预填缓存：空凭证错误不允许被缓存命中掩盖.
	_ = cache.Set(context.Background(), &AccessToken{AccessToken: "cached"}, time.Minute)

	if _, err := GetAccessTokenCached(context.Background(), "", "", cache); err == nil {
		t.Fatal("want error for empty credentials even with warm cache")
	}
}

func TestGetAccessTokenCached(t *testing.T) {
	var n atomic.Int64
	client := &http.Client{Transport: dingtalkRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		n.Add(1)
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"errcode":0,"errmsg":"ok","access_token":"ding-tok","expires_in":7200}`)),
			Request:    req,
		}, nil
	})}
	cache := NewMemoryTokenCache()

	for i := range 2 {
		tok, err := GetAccessTokenCached(context.Background(), "cached-key", "secret", cache, client)
		if err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
		if tok.AccessToken != "ding-tok" {
			t.Fatalf("call %d: unexpected token %+v", i, tok)
		}
	}
	if n.Load() != 1 {
		t.Fatalf("upstream called %d times, want 1", n.Load())
	}
}
