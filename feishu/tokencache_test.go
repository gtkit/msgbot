package feishu

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestGetAccessTokenCached(t *testing.T) {
	rt := &recordingRoundTripper{responses: []roundTripResponse{{
		status: http.StatusOK,
		body:   `{"code":0,"msg":"ok","app_access_token":"app-tok","tenant_access_token":"tenant-tok","expire":7200}`,
	}}}
	client := &http.Client{Transport: rt}
	cache := NewMemoryTokenCache()

	for i := range 2 {
		tok, err := GetAccessTokenCached(context.Background(), "cached-app", "secret", cache, client)
		if err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
		if tok.TenantAccessToken != "tenant-tok" {
			t.Fatalf("call %d: unexpected token %+v", i, tok)
		}
	}
	if len(rt.requests) != 1 {
		t.Fatalf("upstream called %d times, want 1", len(rt.requests))
	}
}

func TestGetAccessTokenCachedEmptyCredentials(t *testing.T) {
	cache := NewMemoryTokenCache()
	// 预填缓存：空凭证错误不允许被缓存命中掩盖.
	_ = cache.Set(context.Background(), &AccessToken{TenantAccessToken: "cached"}, time.Minute)

	if _, err := GetAccessTokenCached(context.Background(), "", "", cache); err == nil {
		t.Fatal("want error for empty credentials even with warm cache")
	}
}

func TestGetAccessTokenCachedNilCache(t *testing.T) {
	rt := &recordingRoundTripper{responses: []roundTripResponse{
		{status: http.StatusOK, body: `{"code":0,"msg":"ok","app_access_token":"a","tenant_access_token":"t1","expire":7200}`},
		{status: http.StatusOK, body: `{"code":0,"msg":"ok","app_access_token":"a","tenant_access_token":"t2","expire":7200}`},
	}}
	client := &http.Client{Transport: rt}

	for i := range 2 {
		if _, err := GetAccessTokenCached(context.Background(), "nilcache-app", "secret", nil, client); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	if len(rt.requests) != 2 {
		t.Fatalf("nil cache must not persist: upstream called %d times, want 2", len(rt.requests))
	}
}
