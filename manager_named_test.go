package msgbot

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
)

func TestNewNamedManagerLookup(t *testing.T) {
	t.Parallel()

	p0 := &fakeProvider{platform: PlatformFeishu}
	oncall := &fakeProvider{platform: PlatformFeishu}
	archive := &fakeProvider{platform: PlatformDingTalk}

	mgr, err := NewNamedManager(Named("p0", p0), Named("oncall", oncall), Named("archive", archive))
	if err != nil {
		t.Fatalf("new named manager: %v", err)
	}

	if mgr.GetNamed("p0") != Provider(p0) {
		t.Fatal("GetNamed must return the provider registered under that name")
	}
	if mgr.GetNamed("oncall") != Provider(oncall) {
		t.Fatal("same-platform providers must not overwrite each other in the named index")
	}
	if mgr.GetNamed("archive") != Provider(archive) {
		t.Fatal("archive lookup mismatch")
	}
	if mgr.GetNamed("missing") != nil {
		t.Fatal("unregistered name must resolve to nil")
	}
}

func TestNewNamedManagerRejectsBadInput(t *testing.T) {
	t.Parallel()

	valid := &fakeProvider{platform: PlatformFeishu}
	other := &fakeProvider{platform: PlatformWeCom}
	var typedNil *fakeProvider

	tests := []struct {
		name    string
		in      []NamedProvider
		wantErr string
	}{
		{name: "no providers", in: nil, wantErr: "at least one named provider"},
		{name: "empty name", in: []NamedProvider{Named("", valid)}, wantErr: "empty name"},
		{name: "duplicate name", in: []NamedProvider{Named("x", valid), Named("x", other)}, wantErr: `duplicate provider name "x"`},
		{name: "nil provider", in: []NamedProvider{Named("x", nil)}, wantErr: `named provider "x" is nil`},
		{name: "typed nil provider", in: []NamedProvider{Named("x", typedNil)}, wantErr: `named provider "x" is nil`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mgr, err := NewNamedManager(tt.in...)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("want error containing %q, got %v", tt.wantErr, err)
			}
			if mgr != nil {
				t.Fatal("a rejected registration must not yield a usable manager")
			}
		})
	}
}

// TestNamedManagerNamesAreOrderedCopies 锁定 Names 的两条契约：注册顺序、返回副本。
// 去掉 slices.Clone，篡改返回值后 Manager 的内部状态就会跟着变，测试失败。
func TestNamedManagerNamesAreOrderedCopies(t *testing.T) {
	t.Parallel()

	mgr, err := NewNamedManager(
		Named("p0", &fakeProvider{platform: PlatformFeishu}),
		Named("oncall", &fakeProvider{platform: PlatformWeCom}),
		Named("archive", &fakeProvider{platform: PlatformDingTalk}),
	)
	if err != nil {
		t.Fatalf("new named manager: %v", err)
	}

	want := []string{"p0", "oncall", "archive"}
	got := mgr.Names()
	if len(got) != len(want) {
		t.Fatalf("want %d names, got %v", len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("names must follow registration order, want %v got %v", want, got)
		}
	}

	got[0] = "tampered"
	if mgr.Names()[0] != "p0" {
		t.Fatal("Names must return a copy")
	}

	// 由 NewManager 创建的 Manager 没有具名注册。
	if names := NewManager(&fakeProvider{platform: PlatformFeishu}).Names(); len(names) != 0 {
		t.Fatalf("NewManager has no named providers, got %v", names)
	}
}

// TestNamedManagerAllKeepsEveryTarget 锁定 All 的核心价值：同平台的多个目标
// 都要留在结果里。若 All 退回按平台索引取值，飞书只会剩一个，测试失败。
func TestNamedManagerAllKeepsEveryTarget(t *testing.T) {
	t.Parallel()

	p0 := &fakeProvider{platform: PlatformFeishu}
	oncall := &fakeProvider{platform: PlatformFeishu}
	archive := &fakeProvider{platform: PlatformDingTalk}

	mgr, err := NewNamedManager(Named("p0", p0), Named("oncall", oncall), Named("archive", archive))
	if err != nil {
		t.Fatalf("new named manager: %v", err)
	}

	all := mgr.All()
	if len(all) != 3 {
		t.Fatalf("want 3 providers, got %d", len(all))
	}
	if all[0] != Provider(p0) || all[1] != Provider(oncall) || all[2] != Provider(archive) {
		t.Fatal("All must follow registration order")
	}

	all[0] = nil
	if mgr.All()[0] == nil {
		t.Fatal("All must return a copy")
	}
}

// TestManagerAllDropsOverwrittenProvider 锁定 NewManager 的既有语义没被 All 的
// 改写破坏：同平台后者覆盖前者，被覆盖者不出现在 All 中。
func TestManagerAllDropsOverwrittenProvider(t *testing.T) {
	t.Parallel()

	first := &fakeProvider{platform: PlatformFeishu}
	second := &fakeProvider{platform: PlatformFeishu}
	wecom := &fakeProvider{platform: PlatformWeCom}

	mgr := NewManager(first, wecom, second)
	all := mgr.All()
	if len(all) != 2 {
		t.Fatalf("want 2 surviving providers, got %d", len(all))
	}
	if all[0] != Provider(wecom) || all[1] != Provider(second) {
		t.Fatal("All must keep only the platform-index survivors, in registration order")
	}
	if mgr.Get(PlatformFeishu) != Provider(second) {
		t.Fatal("later provider must win the platform index")
	}
}

// countingProvider 统计自己收到的发送次数，用于验证广播的扇出范围。
type countingProvider struct {
	fakeProvider
	sends atomic.Int64
}

func (p *countingProvider) SendText(context.Context, string, ...SendOption) error {
	p.sends.Add(1)
	return p.err
}

// unstablePlatformProvider 每次 Platform() 返回不同的值。它是 NewManager 只调用
// Platform() 一次这一约束的反证：若实现分两遍循环各调一次，两遍拿到的平台不一致，
// 存活者判定就会错，All 的结果随之出错。
type unstablePlatformProvider struct {
	fakeProvider
	calls int
}

func (p *unstablePlatformProvider) Platform() Platform {
	p.calls++
	if p.calls == 1 {
		return PlatformFeishu
	}
	return PlatformDingTalk
}

func TestNewManagerCallsPlatformOncePerProvider(t *testing.T) {
	t.Parallel()

	unstable := &unstablePlatformProvider{}
	mgr := NewManager(unstable)

	if unstable.calls != 1 {
		t.Fatalf("Platform() must be called once per provider, got %d calls", unstable.calls)
	}
	if len(mgr.All()) != 1 {
		t.Fatalf("want 1 provider, got %d", len(mgr.All()))
	}
	if mgr.Get(PlatformFeishu) != Provider(unstable) {
		t.Fatal("the platform index must use the single Platform() result")
	}
}

// TestNamedManagerDefaultResolvesByPlatform 锁定一个容易误解的行为：Default 按平台
// 解析，因此同平台多目标时它返回该平台最后注册的那个，而不是第一个注册项。
// 这条行为写进了 README 与方法注释，必须有测试钉住。
func TestNamedManagerDefaultResolvesByPlatform(t *testing.T) {
	t.Parallel()

	p0 := &fakeProvider{platform: PlatformFeishu}
	oncall := &fakeProvider{platform: PlatformFeishu}

	mgr, err := NewNamedManager(Named("p0", p0), Named("oncall", oncall))
	if err != nil {
		t.Fatalf("new named manager: %v", err)
	}

	if mgr.Default() != Provider(oncall) {
		t.Fatal("Default resolves through the platform index, so it yields the last provider of that platform")
	}
	if mgr.Feishu() != Provider(oncall) {
		t.Fatal("Feishu() resolves through the platform index too")
	}
	if mgr.GetNamed("p0") != Provider(p0) {
		t.Fatal("GetNamed stays the way to reach a specific target")
	}

	// 跨平台时默认平台仍是第一个注册项的平台。
	mixed, err := NewNamedManager(Named("a", p0), Named("b", &fakeProvider{platform: PlatformDingTalk}))
	if err != nil {
		t.Fatalf("new named manager: %v", err)
	}
	if mixed.Default() != Provider(p0) {
		t.Fatal("the default platform must be the first registration's platform")
	}
}

func TestNamedManagerMultiBroadcastsToEveryTarget(t *testing.T) {
	t.Parallel()

	p0 := &countingProvider{fakeProvider: fakeProvider{platform: PlatformFeishu}}
	oncall := &countingProvider{fakeProvider: fakeProvider{platform: PlatformFeishu}}
	archive := &countingProvider{fakeProvider: fakeProvider{platform: PlatformDingTalk}}

	mgr, err := NewNamedManager(Named("p0", p0), Named("oncall", oncall), Named("archive", archive))
	if err != nil {
		t.Fatalf("new named manager: %v", err)
	}
	multi, err := mgr.Multi()
	if err != nil {
		t.Fatalf("multi: %v", err)
	}
	if err := multi.SendText(context.Background(), "broadcast"); err != nil {
		t.Fatalf("send text: %v", err)
	}

	if p0.sends.Load() != 1 || oncall.sends.Load() != 1 || archive.sends.Load() != 1 {
		t.Fatalf("each target must receive exactly one send, got %d/%d/%d",
			p0.sends.Load(), oncall.sends.Load(), archive.sends.Load())
	}
}

func TestNamedManagerSetDefault(t *testing.T) {
	t.Parallel()

	mgr, err := NewNamedManager(
		Named("p0", &fakeProvider{platform: PlatformFeishu}),
		Named("archive", &fakeProvider{platform: PlatformDingTalk}),
	)
	if err != nil {
		t.Fatalf("new named manager: %v", err)
	}

	if err := mgr.SetDefault(PlatformDingTalk); err != nil {
		t.Fatalf("set default: %v", err)
	}
	if mgr.Default() != mgr.Get(PlatformDingTalk) {
		t.Fatal("SetDefault must move Default to the requested platform")
	}
	if err := mgr.SetDefault(PlatformWeCom); err == nil {
		t.Fatal("SetDefault must reject an unregistered platform")
	}
	if mgr.Default() != mgr.Get(PlatformDingTalk) {
		t.Fatal("a rejected SetDefault must leave the current default untouched")
	}
}
