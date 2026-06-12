package msgbot

import "testing"

func BenchmarkApplySendOptions(b *testing.B) {
	opts := []SendOption{
		WithAtAll(),
		WithAtUsers("u1", "u2", "u3"),
	}

	b.ReportAllocs()
	for b.Loop() {
		_ = ApplySendOptions(opts)
	}
}
