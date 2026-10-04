package aiusage

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/jchv/go-webview2"
)

// ログイン画面を出している間は、一定の間隔で取得し直す。画面が閉じられたら止まる。
func TestWatchLoopStopsWhenNoLongerKeptGoing(t *testing.T) {
	var steps atomic.Int32
	var visible atomic.Bool
	visible.Store(true)
	done := make(chan struct{})
	go func() {
		watchLoop(5*time.Millisecond, time.Minute, visible.Load, func() {
			if steps.Add(1) == 3 {
				visible.Store(false) // 3 回目の取得でログイン成功（画面が閉じる）
			}
		})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("画面が閉じられたのに止まらない")
	}
	if got := steps.Load(); got != 3 {
		t.Errorf("取得は 3 回で止まるはず: %d 回", got)
	}
}

// 最長の時間を過ぎたら、画面が出たままでも止まる（取得し続けない）。
func TestWatchLoopStopsAtMax(t *testing.T) {
	var steps atomic.Int32
	start := time.Now()
	watchLoop(10*time.Millisecond, 55*time.Millisecond, func() bool { return true }, func() { steps.Add(1) })
	if el := time.Since(start); el > time.Second {
		t.Errorf("最長の時間で止まらなかった: %v", el)
	}
	if n := steps.Load(); n < 2 || n > 6 {
		t.Errorf("間隔ごとに数回だけ取得するはず: %d 回", n)
	}
}

// 開始してすぐ「続けない」なら、一度も取得しない。
func TestWatchLoopNoStepWhenAlreadyClosed(t *testing.T) {
	var steps atomic.Int32
	watchLoop(5*time.Millisecond, time.Minute, func() bool { return false }, func() { steps.Add(1) })
	if steps.Load() != 0 {
		t.Errorf("取得は 0 回のはず: %d 回", steps.Load())
	}
}

// 取得がタイムアウトしたときの補助ページの再読み込み: ログイン画面を操作中は奪わない。
// 連続して呼ばれても、最小間隔（1 分）までは繰り返さない。
func TestShouldReloadAfterTimeout(t *testing.T) {
	now := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name         string
		last         time.Time
		loginVisible bool
		want         bool
	}{
		{"初回（まだ再読み込みしていない）", time.Time{}, false, true},
		{"最小間隔を過ぎた", now.Add(-reloadMinInterval), false, true},
		{"最小間隔より十分あと", now.Add(-10 * time.Minute), false, true},
		{"直前に再読み込みした", now.Add(-reloadMinInterval + time.Second), false, false},
		{"たった今再読み込みした", now, false, false},
		// ログイン画面を出している間は、ユーザーの操作中のページを奪わない
		{"ログイン画面を出している（初回）", time.Time{}, true, false},
		{"ログイン画面を出している（間隔あり）", now.Add(-10 * time.Minute), true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldReloadAfterTimeout(now, tt.last, tt.loginVisible); got != tt.want {
				t.Errorf("shouldReloadAfterTimeout = %v, want %v", got, tt.want)
			}
		})
	}
}

// Navigate だけを記録する WebView。他のメソッドは埋め込みの nil を呼ぶので、呼ばれたら panic する。
type navRecorder struct {
	webview2.WebView
	navigated []string
}

func (n *navRecorder) Navigate(url string) { n.navigated = append(n.navigated, url) }

// 再読み込みの実行時点で、ログイン画面を出していれば何もしない。
// Dispatch は非同期なので、予約したあとに showLogin がログイン画面へ遷移させていることがあり、
// そのときに使用量ページへ遷移させると、操作中のログイン画面を奪ってしまう。
func TestReloadUsagePageSkipsWhileLoginVisible(t *testing.T) {
	const usageURL = "https://example.test/usage"
	newView := func(inst webview2.WebView) *authView {
		return &authView{p: &provider{usageURL: usageURL}, inst: inst}
	}

	t.Run("ログイン画面を出していない: 使用量ページを開き直す", func(t *testing.T) {
		rec := &navRecorder{}
		v := newView(rec)
		if !v.reloadUsagePage() {
			t.Fatal("開き直すはず")
		}
		if len(rec.navigated) != 1 || rec.navigated[0] != usageURL {
			t.Errorf("遷移先 = %v, want [%s]", rec.navigated, usageURL)
		}
	})

	t.Run("ログイン画面を出している: 何もしない", func(t *testing.T) {
		rec := &navRecorder{}
		v := newView(rec)
		v.visible.Store(true)
		if v.reloadUsagePage() {
			t.Error("開き直してはいけない")
		}
		if len(rec.navigated) != 0 {
			t.Errorf("遷移してはいけない: %v", rec.navigated)
		}
	})

	// 予約した時点では出ていなくても、実行までの間に出たら、実行時の確認で止まる（指摘のケース）。
	t.Run("予約のあとでログイン画面が出た: 何もしない", func(t *testing.T) {
		rec := &navRecorder{}
		v := newView(rec)
		scheduled := func() bool { return v.reloadUsagePage() } // hostDispatch に積まれるコールバック
		v.visible.Store(true)                                   // 実行前に showLogin が表示状態にする
		if scheduled() {
			t.Error("予約後にログイン画面が出たのに開き直した")
		}
		if len(rec.navigated) != 0 {
			t.Errorf("遷移してはいけない: %v", rec.navigated)
		}
	})

	t.Run("補助ページが無い: 何もしない", func(t *testing.T) {
		v := newView(nil)
		if v.reloadUsagePage() {
			t.Error("ページが無いのに開き直した")
		}
	})
}
