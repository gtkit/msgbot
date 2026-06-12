package tokencache

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type testToken struct {
	ID string
}

type fakeCache struct {
	getTok *testToken
	getErr error
	setErr error

	mu      sync.Mutex
	setTTLs []time.Duration
	setToks []*testToken
}

func (f *fakeCache) Get(_ context.Context) (*testToken, error) {
	return f.getTok, f.getErr
}

func (f *fakeCache) Set(_ context.Context, t *testToken, ttl time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.setTTLs = append(f.setTTLs, ttl)
	f.setToks = append(f.setToks, t)
	return f.setErr
}

func (f *fakeCache) sets() []time.Duration {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]time.Duration(nil), f.setTTLs...)
}

func countingFetch(n *atomic.Int64, tok *testToken, expire int, err error) func(context.Context) (*testToken, int, error) {
	return func(context.Context) (*testToken, int, error) {
		n.Add(1)
		return tok, expire, err
	}
}

func TestTTLFor(t *testing.T) {
	cases := []struct {
		expire int
		want   time.Duration
	}{
		{-1, 0},
		{0, 0},
		{1, 0}, // 1/2 = 0，兜底不缓存.
		{2, time.Second},
		{600, 300 * time.Second},
		{601, 301 * time.Second},
		{7200, 6900 * time.Second},
	}
	for _, c := range cases {
		if got := ttlFor(c.expire); got != c.want {
			t.Errorf("ttlFor(%d) = %v, want %v", c.expire, got, c.want)
		}
	}
}

func TestFetchCacheHit(t *testing.T) {
	var n atomic.Int64
	cache := &fakeCache{getTok: &testToken{ID: "cached"}}
	got, err := Fetch(context.Background(), cache, NewGroup[testToken](), "k",
		countingFetch(&n, &testToken{ID: "fresh"}, 7200, nil))
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if got.ID != "cached" {
		t.Fatalf("want cached token, got %q", got.ID)
	}
	if n.Load() != 0 {
		t.Fatalf("upstream called %d times, want 0", n.Load())
	}
}

func TestFetchMissFetchesAndWrites(t *testing.T) {
	var n atomic.Int64
	cache := &fakeCache{}
	got, err := Fetch(context.Background(), cache, NewGroup[testToken](), "k",
		countingFetch(&n, &testToken{ID: "fresh"}, 7200, nil))
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if got.ID != "fresh" || n.Load() != 1 {
		t.Fatalf("want fresh token with 1 upstream call, got %q calls=%d", got.ID, n.Load())
	}
	if sets := cache.sets(); len(sets) != 1 || sets[0] != 6900*time.Second {
		t.Fatalf("want one Set with ttl 6900s, got %v", sets)
	}
}

func TestFetchNonPositiveExpireNotCached(t *testing.T) {
	var n atomic.Int64
	cache := &fakeCache{}
	got, err := Fetch(context.Background(), cache, NewGroup[testToken](), "k",
		countingFetch(&n, &testToken{ID: "fresh"}, 0, nil))
	if err != nil || got.ID != "fresh" {
		t.Fatalf("want fresh token, got %v err=%v", got, err)
	}
	if sets := cache.sets(); len(sets) != 0 {
		t.Fatalf("Set should not be called for non-positive expire, got %v", sets)
	}
}

// raceWindowCache 模拟"外层 miss 后、进组回调前缓存已被填充"的竞态窗口：
// 第一次 Get 未命中，之后命中.
type raceWindowCache struct {
	fakeCache
	gets   atomic.Int64
	filled *testToken
}

func (r *raceWindowCache) Get(_ context.Context) (*testToken, error) {
	if r.gets.Add(1) == 1 {
		return nil, nil
	}
	return r.filled, nil
}

func TestFetchDoubleCheckSkipsUpstream(t *testing.T) {
	var n atomic.Int64
	cache := &raceWindowCache{filled: &testToken{ID: "filled-by-previous-initiator"}}
	got, err := Fetch(context.Background(), cache, NewGroup[testToken](), "k",
		countingFetch(&n, &testToken{ID: "fresh"}, 7200, nil))
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if got.ID != "filled-by-previous-initiator" {
		t.Fatalf("double check must return cached token, got %q", got.ID)
	}
	if n.Load() != 0 {
		t.Fatalf("upstream called %d times, want 0 (double check inside group)", n.Load())
	}
	if gets := cache.gets.Load(); gets != 2 {
		t.Fatalf("cache.Get called %d times, want 2 (outer + double check)", gets)
	}
}

func TestFetchGetErrorDegrades(t *testing.T) {
	var n atomic.Int64
	cache := &fakeCache{getErr: errors.New("redis down")}
	got, err := Fetch(context.Background(), cache, NewGroup[testToken](), "k",
		countingFetch(&n, &testToken{ID: "fresh"}, 7200, nil))
	if err != nil || got.ID != "fresh" || n.Load() != 1 {
		t.Fatalf("want degraded direct fetch, got %v err=%v calls=%d", got, err, n.Load())
	}
}

func TestFetchSetErrorIgnored(t *testing.T) {
	var n atomic.Int64
	cache := &fakeCache{setErr: errors.New("redis down")}
	got, err := Fetch(context.Background(), cache, NewGroup[testToken](), "k",
		countingFetch(&n, &testToken{ID: "fresh"}, 7200, nil))
	if err != nil || got.ID != "fresh" {
		t.Fatalf("Set error must not propagate, got %v err=%v", got, err)
	}
}

func TestFetchUpstreamError(t *testing.T) {
	var n atomic.Int64
	wantErr := errors.New("upstream boom")
	_, err := Fetch(context.Background(), &fakeCache{}, NewGroup[testToken](), "k",
		countingFetch(&n, nil, 0, wantErr))
	if !errors.Is(err, wantErr) {
		t.Fatalf("want upstream error, got %v", err)
	}
}

// concurrentFetch 以同一 group 并发执行 fn 指定次数，返回各调用结果.
func concurrentFetch(t *testing.T, goroutines int, fn func() (*testToken, error)) []*testToken {
	t.Helper()
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		results []*testToken
	)
	start := make(chan struct{})
	for range goroutines {
		wg.Go(func() {
			<-start
			tok, err := fn()
			if err != nil {
				t.Errorf("concurrent fetch: %v", err)
				return
			}
			mu.Lock()
			results = append(results, tok)
			mu.Unlock()
		})
	}
	close(start)
	wg.Wait()
	return results
}

func TestFetchSameKeyConcurrentSingleUpstream(t *testing.T) {
	var n atomic.Int64
	g := NewGroup[testToken]()
	cache := &fakeCache{}
	slowFetch := func(context.Context) (*testToken, int, error) {
		n.Add(1)
		time.Sleep(20 * time.Millisecond)
		return &testToken{ID: "fresh"}, 7200, nil
	}
	results := concurrentFetch(t, 50, func() (*testToken, error) {
		return Fetch(context.Background(), cache, g, "k", slowFetch)
	})
	if n.Load() != 1 {
		t.Fatalf("upstream called %d times, want 1", n.Load())
	}
	if len(results) != 50 {
		t.Fatalf("want 50 results, got %d", len(results))
	}
	if sets := cache.sets(); len(sets) != 1 {
		t.Fatalf("Set called %d times, want 1", len(sets))
	}
}

func TestFetchDifferentCredentialsNotMerged(t *testing.T) {
	var nA, nB atomic.Int64
	g := NewGroup[testToken]()
	slow := func(n *atomic.Int64, id string) func(context.Context) (*testToken, int, error) {
		return func(context.Context) (*testToken, int, error) {
			n.Add(1)
			time.Sleep(20 * time.Millisecond)
			return &testToken{ID: id}, 7200, nil
		}
	}
	keyA, keyB := Key("p", "app", "secret-a"), Key("p", "app", "secret-b")
	var wg sync.WaitGroup
	check := func(key, want string, fetch func(context.Context) (*testToken, int, error)) {
		for range 25 {
			wg.Go(func() {
				tok, err := Fetch(context.Background(), nil, g, key, fetch)
				if err != nil || tok.ID != want {
					t.Errorf("key %q: got %v err=%v, want %q", key, tok, err, want)
				}
			})
		}
	}
	check(keyA, "a", slow(&nA, "a"))
	check(keyB, "b", slow(&nB, "b"))
	wg.Wait()
	if nA.Load() != 1 || nB.Load() != 1 {
		t.Fatalf("upstream calls a=%d b=%d, want 1 each", nA.Load(), nB.Load())
	}
}

func TestFetchNilCacheStillDeduped(t *testing.T) {
	var n atomic.Int64
	g := NewGroup[testToken]()
	slowFetch := func(context.Context) (*testToken, int, error) {
		n.Add(1)
		time.Sleep(20 * time.Millisecond)
		return &testToken{ID: "fresh"}, 7200, nil
	}
	concurrentFetch(t, 50, func() (*testToken, error) {
		return Fetch(context.Background(), nil, g, "k", slowFetch)
	})
	if n.Load() != 1 {
		t.Fatalf("upstream called %d times with nil cache, want 1", n.Load())
	}
}

func TestKeyExcludesPlainSecret(t *testing.T) {
	const secret = "very-secret-value"
	key := Key("feishu", "app-id", secret)
	if key == "" || key == secret {
		t.Fatalf("unexpected key %q", key)
	}
	if containsSubstring(key, secret) {
		t.Fatalf("key %q must not contain plain secret", key)
	}
	if Key("feishu", "app-id", "other") == key {
		t.Fatal("different secrets must produce different keys")
	}
	if Key("dingtalk", "app-id", secret) == key {
		t.Fatal("different platforms must produce different keys")
	}
}

func containsSubstring(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
