package tokencache

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestGroupWaiterCancelExitsEarly(t *testing.T) {
	g := NewGroup[testToken]()
	release := make(chan struct{})
	started := make(chan struct{})

	go func() {
		_, _ = g.Do(context.Background(), "k", func() (*testToken, error) {
			close(started)
			<-release
			return &testToken{ID: "slow"}, nil
		})
	}()
	<-started

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := g.Do(ctx, "k", func() (*testToken, error) {
		t.Error("waiter must not invoke fn")
		return nil, nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}

	close(release) // 发起者正常收尾，不受等待者退出影响.
}

func TestGroupFailureNotSticky(t *testing.T) {
	g := NewGroup[testToken]()
	var n atomic.Int64

	_, err := g.Do(context.Background(), "k", func() (*testToken, error) {
		n.Add(1)
		return nil, errors.New("boom")
	})
	if err == nil {
		t.Fatal("want first call to fail")
	}

	tok, err := g.Do(context.Background(), "k", func() (*testToken, error) {
		n.Add(1)
		return &testToken{ID: "retry"}, nil
	})
	if err != nil || tok.ID != "retry" {
		t.Fatalf("retry must start fresh, got %v err=%v", tok, err)
	}
	if n.Load() != 2 {
		t.Fatalf("fn called %d times, want 2", n.Load())
	}
}

func TestGroupWaiterSharesInitiatorFailure(t *testing.T) {
	g := NewGroup[testToken]()
	release := make(chan struct{})
	started := make(chan struct{})
	wantErr := errors.New("initiator boom")

	done := make(chan error, 1)
	go func() {
		_, err := g.Do(context.Background(), "k", func() (*testToken, error) {
			close(started)
			<-release
			return nil, wantErr
		})
		done <- err
	}()
	<-started

	waiterDone := make(chan error, 1)
	go func() {
		_, err := g.Do(context.Background(), "k", func() (*testToken, error) {
			t.Error("waiter must not invoke fn")
			return nil, nil
		})
		waiterDone <- err
	}()

	time.Sleep(10 * time.Millisecond) // 让等待者进入等待.
	close(release)

	if err := <-done; !errors.Is(err, wantErr) {
		t.Fatalf("initiator: want %v, got %v", wantErr, err)
	}
	if err := <-waiterDone; !errors.Is(err, wantErr) {
		t.Fatalf("waiter must share initiator failure, got %v", err)
	}
}
