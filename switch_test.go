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

// TestSendMutedDropsMessage 是「静音时不发请求、不计数」这一契约的反证测试：
// 移除 Config.Send 开头的静音短路，transport 会被调用、stats 会增长，测试即失败。
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
		t.Fatalf("muted send must not touch stats, got sent=%d error=%d", stats.TotalSent(), stats.TotalError())
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
	if stats.TotalSent() != 2 || stats.TotalError() != 0 {
		t.Fatalf("want sent=2 error=0, got sent=%d error=%d", stats.TotalSent(), stats.TotalError())
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
