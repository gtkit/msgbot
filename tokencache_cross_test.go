package msgbot_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gtkit/msgbot/dingtalk"
	"github.com/gtkit/msgbot/feishu"
)

type countingTransport struct {
	n    atomic.Int64
	body string
}

func (c *countingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	c.n.Add(1)
	time.Sleep(20 * time.Millisecond) // 制造并发窗口，让同平台请求有机会合并.
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(c.body)),
		Request:    req,
	}, nil
}

// TestCrossPlatformTokenGroupsIsolated 验证去重按包隔离：
// 飞书与钉钉使用相同 id/secret 并发取 token 时不互相合并，各发各的请求。
func TestCrossPlatformTokenGroupsIsolated(t *testing.T) {
	const id, secret = "same-id", "same-secret"

	fsRT := &countingTransport{body: `{"code":0,"msg":"ok","app_access_token":"a","tenant_access_token":"t","expire":7200}`}
	dtRT := &countingTransport{body: `{"errcode":0,"errmsg":"ok","access_token":"d","expires_in":7200}`}
	fsClient := &http.Client{Transport: fsRT}
	dtClient := &http.Client{Transport: dtRT}

	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() {
			if _, err := feishu.GetAccessTokenCached(context.Background(), id, secret, nil, fsClient); err != nil {
				t.Errorf("feishu: %v", err)
			}
		})
		wg.Go(func() {
			if _, err := dingtalk.GetAccessTokenCached(context.Background(), id, secret, nil, dtClient); err != nil {
				t.Errorf("dingtalk: %v", err)
			}
		})
	}
	wg.Wait()

	if fsRT.n.Load() != 1 || dtRT.n.Load() != 1 {
		t.Fatalf("upstream calls feishu=%d dingtalk=%d, want 1 each (groups must be isolated per package)",
			fsRT.n.Load(), dtRT.n.Load())
	}
}
