package aiusage

import (
	"sync"
	"testing"
)

// fakeHost はテスト用の Host。Dispatch はその場で実行し、バルーンは記録する。
type fakeHost struct {
	mu       sync.Mutex
	s        Settings
	root     string
	changed  int
	balloons []balloonCall
}

func (h *fakeHost) Dispatch(f func()) { f() }
func (h *fakeHost) Balloon(title, msg string, flag uint32) {
	h.mu.Lock()
	h.balloons = append(h.balloons, balloonCall{title, msg, flag})
	h.mu.Unlock()
}
func (h *fakeHost) OnSnapshotChanged()               { h.mu.Lock(); h.changed++; h.mu.Unlock() }
func (h *fakeHost) OnAuthWindowHidden()              {}
func (h *fakeHost) DataRoot() string                 { h.mu.Lock(); defer h.mu.Unlock(); return h.root }
func (h *fakeHost) Settings() Settings               { h.mu.Lock(); defer h.mu.Unlock(); return h.s }
func (h *fakeHost) MutateSettings(f func(*Settings)) { h.mu.Lock(); defer h.mu.Unlock(); f(&h.s) }

func init() {
	host = &fakeHost{s: DefaultSettings()}
}

// withConfig は設定を一時的に書き換える（テスト後に復元）。
func withConfig(t *testing.T, modify func(c *Settings)) {
	t.Helper()
	h := host.(*fakeHost)
	h.mu.Lock()
	prev := h.s
	modify(&h.s)
	h.mu.Unlock()
	t.Cleanup(func() {
		h.mu.Lock()
		h.s = prev
		h.mu.Unlock()
	})
}
