package aiusage

import (
	"sync/atomic"
	"testing"
	"time"
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
