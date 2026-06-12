package tokencache

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestMemoryExpiry(t *testing.T) {
	var m Memory[testToken]
	ctx := context.Background()

	if err := m.Set(ctx, &testToken{ID: "v1"}, 30*time.Millisecond); err != nil {
		t.Fatalf("set: %v", err)
	}
	if tok, _ := m.Get(ctx); tok == nil || tok.ID != "v1" {
		t.Fatalf("want hit before deadline, got %v", tok)
	}

	time.Sleep(50 * time.Millisecond)
	if tok, _ := m.Get(ctx); tok != nil {
		t.Fatalf("want miss after deadline, got %v", tok)
	}
}

func TestMemoryEmptyAndNonPositiveTTL(t *testing.T) {
	var m Memory[testToken]
	ctx := context.Background()

	if tok, err := m.Get(ctx); tok != nil || err != nil {
		t.Fatalf("empty cache: want (nil, nil), got (%v, %v)", tok, err)
	}
	_ = m.Set(ctx, &testToken{ID: "v"}, 0)
	_ = m.Set(ctx, nil, time.Minute)
	if tok, _ := m.Get(ctx); tok != nil {
		t.Fatalf("non-positive ttl or nil value must not be stored, got %v", tok)
	}
}

func TestMemoryCopyIsolation(t *testing.T) {
	var m Memory[testToken]
	ctx := context.Background()

	src := &testToken{ID: "v1"}
	_ = m.Set(ctx, src, time.Minute)
	src.ID = "mutated-after-set"

	got1, _ := m.Get(ctx)
	if got1.ID != "v1" {
		t.Fatalf("Set must copy: got %q", got1.ID)
	}

	got1.ID = "mutated-after-get"
	got2, _ := m.Get(ctx)
	if got2.ID != "v1" {
		t.Fatalf("Get must copy: got %q", got2.ID)
	}
}

func TestMemoryConcurrent(t *testing.T) {
	var m Memory[testToken]
	ctx := context.Background()
	var wg sync.WaitGroup

	for i := range 20 {
		wg.Go(func() {
			for range 100 {
				if i%2 == 0 {
					_ = m.Set(ctx, &testToken{ID: "v"}, time.Minute)
				} else if tok, _ := m.Get(ctx); tok != nil && tok.ID != "v" {
					t.Errorf("unexpected token %q", tok.ID)
				}
			}
		})
	}
	wg.Wait()
}
