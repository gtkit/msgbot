package msgbot

import (
	"context"
	"sync"
	"testing"
)

func TestSwitchStates(t *testing.T) {
	t.Parallel()

	var nilSwitch *Switch
	if !nilSwitch.Enabled() {
		t.Fatal("nil switch must read as enabled")
	}
	if !(&Switch{}).Enabled() {
		t.Fatal("zero-value switch must read as enabled")
	}

	s := NewSwitch()
	if !s.Enabled() {
		t.Fatal("NewSwitch must start enabled")
	}
	s.Disable()
	if s.Enabled() {
		t.Fatal("Disable must take effect")
	}
	s.Disable() // 幂等：重复关闭不应翻回启用。
	if s.Enabled() {
		t.Fatal("repeated Disable must stay disabled")
	}
	s.Enable()
	if !s.Enabled() {
		t.Fatal("Enable must restore sending")
	}
	s.Enable()
	if !s.Enabled() {
		t.Fatal("repeated Enable must stay enabled")
	}
}

func TestConfigMuted(t *testing.T) {
	t.Parallel()

	if (&Config{}).Muted() {
		t.Fatal("config without a switch must never be muted")
	}

	s := NewSwitch()
	cfg := &Config{Switch: s}
	if cfg.Muted() {
		t.Fatal("enabled switch must not mute")
	}
	s.Disable()
	if !cfg.Muted() {
		t.Fatal("disabled switch must mute")
	}
}

// TestSendMutedDropsMessage 是「静音时不发请求、只计 muted」这一契约的反证测试：
// 移除 Config.Send 开头的静音短路，transport 会被调用、sent 会增长；
// 移除其中的 IncMuted，muted 会停在 0。两种情况测试都失败。
func TestSendMutedDropsMessage(t *testing.T) {
	t.Parallel()

	tr := &scriptedTransport{steps: []scriptedStep{{status: 200, body: `{"errcode":0}`}}}
	logger := &discardLogger{}
	cfg := newSendConfig(tr, RetryPolicy{})
	cfg.Logger = logger
	cfg.Switch = NewSwitch()
	cfg.Switch.Disable()

	var stats Stats
	if err := cfg.Send(context.Background(), &stats, PlatformWeCom, "SendText", okBuild); err != nil {
		t.Fatalf("muted send must return nil, got %v", err)
	}
	if tr.calls != 0 {
		t.Fatalf("muted send must not reach the transport, got %d calls", tr.calls)
	}
	if stats.TotalSent() != 0 || stats.TotalError() != 0 {
		t.Fatalf("muted is neither success nor failure, got sent=%d error=%d", stats.TotalSent(), stats.TotalError())
	}
	if stats.TotalMuted() != 1 {
		t.Fatalf("muted send must leave a countable trace, got muted=%d", stats.TotalMuted())
	}
	if logger.debug == 0 {
		t.Fatal("muted send must leave a debug log so the drop is diagnosable")
	}
	if logger.err != 0 {
		t.Fatalf("muted send is not a failure, got %d error logs", logger.err)
	}
}

// TestSendMutedSkipsBuild 锁定静音短路发生在 BuildRequest 之前：
// 构造函数不应被调用，因此签名、marshal 等工作都省掉。
func TestSendMutedSkipsBuild(t *testing.T) {
	t.Parallel()

	tr := &scriptedTransport{steps: []scriptedStep{{status: 200, body: `{"errcode":0}`}}}
	cfg := newSendConfig(tr, RetryPolicy{})
	cfg.Switch = NewSwitch()
	cfg.Switch.Disable()

	built := false
	build := func() (string, any, error) {
		built = true
		return okBuild()
	}
	if err := cfg.Send(context.Background(), new(Stats), PlatformWeCom, "SendText", build); err != nil {
		t.Fatalf("muted send must return nil, got %v", err)
	}
	if built {
		t.Fatal("muted send must not run the payload builder")
	}
}

func TestSendResumesAfterEnable(t *testing.T) {
	t.Parallel()

	tr := &scriptedTransport{steps: []scriptedStep{{status: 200, body: `{"errcode":0}`}}}
	cfg := newSendConfig(tr, RetryPolicy{})
	cfg.Switch = NewSwitch()

	var stats Stats
	ctx := context.Background()
	if err := cfg.Send(ctx, &stats, PlatformWeCom, "SendText", okBuild); err != nil {
		t.Fatalf("send while enabled: %v", err)
	}
	cfg.Switch.Disable()
	if err := cfg.Send(ctx, &stats, PlatformWeCom, "SendText", okBuild); err != nil {
		t.Fatalf("send while muted: %v", err)
	}
	cfg.Switch.Enable()
	if err := cfg.Send(ctx, &stats, PlatformWeCom, "SendText", okBuild); err != nil {
		t.Fatalf("send after enable: %v", err)
	}

	if tr.calls != 2 {
		t.Fatalf("want 2 transport calls (muted one skipped), got %d", tr.calls)
	}
	if stats.TotalSent() != 2 || stats.TotalError() != 0 || stats.TotalMuted() != 1 {
		t.Fatalf("want sent=2 error=0 muted=1, got sent=%d error=%d muted=%d",
			stats.TotalSent(), stats.TotalError(), stats.TotalMuted())
	}
}

// TestSwitchSharedAcrossConfigs 锁定「共享同一实例即被一次 Disable 同时静音」，
// 这是 Switch 的作用域契约。
func TestSwitchSharedAcrossConfigs(t *testing.T) {
	t.Parallel()

	shared := NewSwitch()
	trA := &scriptedTransport{steps: []scriptedStep{{status: 200, body: `{"errcode":0}`}}}
	trB := &scriptedTransport{steps: []scriptedStep{{status: 200, body: `{"errcode":0}`}}}
	cfgA := newSendConfig(trA, RetryPolicy{})
	cfgA.Switch = shared
	cfgB := newSendConfig(trB, RetryPolicy{})
	cfgB.Switch = shared

	shared.Disable()
	ctx := context.Background()
	if err := cfgA.Send(ctx, new(Stats), PlatformFeishu, "SendText", okBuild); err != nil {
		t.Fatalf("cfgA: %v", err)
	}
	if err := cfgB.Send(ctx, new(Stats), PlatformWeCom, "SendText", okBuild); err != nil {
		t.Fatalf("cfgB: %v", err)
	}
	if trA.calls != 0 || trB.calls != 0 {
		t.Fatalf("one Disable must mute every sharing provider, got A=%d B=%d", trA.calls, trB.calls)
	}
}

// TestSwitchConcurrentUse 是「可被多个 goroutine 并发读写」这一契约的反证测试：
// 把 Switch.off 从 atomic.Bool 换成裸 bool，-race 下即报数据竞争。
func TestSwitchConcurrentUse(t *testing.T) {
	t.Parallel()

	s := NewSwitch()
	cfg := &Config{Switch: s}

	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 200 {
				s.Disable()
				s.Enable()
			}
		})
		wg.Go(func() {
			for range 200 {
				_ = s.Enabled()
				_ = cfg.Muted()
			}
		})
	}
	wg.Wait()
}

// TestMutedStatsCounting 覆盖 muted 计数的三条语义：多次累加、与 sent/error
// 互斥、开关翻转后的计数序列。
func TestMutedStatsCounting(t *testing.T) {
	t.Parallel()

	t.Run("accumulates", func(t *testing.T) {
		t.Parallel()

		tr := &scriptedTransport{steps: []scriptedStep{{status: 200, body: `{"errcode":0}`}}}
		cfg := newSendConfig(tr, RetryPolicy{})
		cfg.Switch = NewSwitch()
		cfg.Switch.Disable()

		var stats Stats
		for range 3 {
			if err := cfg.Send(context.Background(), &stats, PlatformWeCom, "SendText", okBuild); err != nil {
				t.Fatalf("muted send: %v", err)
			}
		}
		if stats.TotalMuted() != 3 {
			t.Fatalf("want muted=3, got %d", stats.TotalMuted())
		}
		if stats.TotalSent() != 0 || stats.TotalError() != 0 {
			t.Fatalf("want sent=0 error=0, got sent=%d error=%d", stats.TotalSent(), stats.TotalError())
		}
	})

	t.Run("failure does not count as muted", func(t *testing.T) {
		t.Parallel()

		tr := &scriptedTransport{steps: []scriptedStep{{status: 200, body: `{"errcode":40001,"errmsg":"bad"}`}}}
		cfg := newSendConfig(tr, RetryPolicy{})
		cfg.Switch = NewSwitch()

		var stats Stats
		if err := cfg.Send(context.Background(), &stats, PlatformWeCom, "SendText", okBuild); err == nil {
			t.Fatal("expected a platform error")
		}
		if stats.TotalError() != 1 || stats.TotalMuted() != 0 {
			t.Fatalf("want error=1 muted=0, got error=%d muted=%d", stats.TotalError(), stats.TotalMuted())
		}
	})

	// 静音按发送任务计，与 sent/error 的粒度一致：开启重试也只计一次。
	t.Run("retry does not multiply the count", func(t *testing.T) {
		t.Parallel()

		tr := &scriptedTransport{steps: []scriptedStep{{status: 200, body: `{"errcode":0}`}}}
		cfg := newSendConfig(tr, RetryPolicy{MaxRetries: 3, InitialDelay: fastRetry})
		cfg.Switch = NewSwitch()
		cfg.Switch.Disable()

		var stats Stats
		if err := cfg.Send(context.Background(), &stats, PlatformWeCom, "SendText", okBuild); err != nil {
			t.Fatalf("muted send: %v", err)
		}
		if stats.TotalMuted() != 1 {
			t.Fatalf("want muted=1, got %d", stats.TotalMuted())
		}
	})

	// nil Stats 是扩展 API 的合法用法，静音路径不能成为唯一会 panic 的分支。
	t.Run("nil stats does not panic", func(t *testing.T) {
		t.Parallel()

		tr := &scriptedTransport{steps: []scriptedStep{{status: 200, body: `{"errcode":0}`}}}
		cfg := newSendConfig(tr, RetryPolicy{})
		cfg.Switch = NewSwitch()
		cfg.Switch.Disable()

		if err := cfg.Send(context.Background(), nil, PlatformWeCom, "SendText", okBuild); err != nil {
			t.Fatalf("muted send with nil stats: %v", err)
		}
		if tr.calls != 0 {
			t.Fatalf("want no transport call, got %d", tr.calls)
		}
	})
}
