//go:build windows

package ime

import (
	"testing"
	"time"
)

// TestStartStopWatcher は、本物の WinEvent Hook を張って起動・停止を繰り返し、
// 固まらないこと（WM_QUIT でループを抜け、フック用スレッドが終わること）と、
// 二重起動・二重停止で状態が崩れないことを確かめる。
//
// DryRun にしているので、左上の IME ウィンドウを検知しても復旧処理（他のウィンドウへの操作）は行わない。
// フックを張れない環境（対話デスクトップのないセッションなど）では Skip する。
func TestStartStopWatcher(t *testing.T) {
	cfg := Config{Enabled: true, CooldownSec: 2, ThresholdPx: 5, DryRun: true}

	// within は f が時間内に終わらなければテストを失敗させる（デッドロックの検出）。
	within := func(name string, f func() error) {
		t.Helper()
		errc := make(chan error, 1)
		go func() { errc <- f() }()
		select {
		case err := <-errc:
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("%s が 5 秒以内に戻らない", name)
		}
	}

	if err := StartWatcher(cfg); err != nil {
		t.Skipf("WinEvent Hook を張れない環境: %v", err)
	}
	t.Cleanup(func() { _ = StopWatcher() })
	if !Running() {
		t.Fatal("起動後に Running() が false")
	}

	for i := 0; i < 3; i++ {
		within("二重の StartWatcher", func() error { return StartWatcher(cfg) })
		if !Running() {
			t.Fatalf("%d 回目: 二重起動で止まった", i)
		}
		within("StopWatcher", StopWatcher)
		if Running() {
			t.Fatalf("%d 回目: 停止後も Running() が true", i)
		}
		if detectChan != nil {
			t.Fatalf("%d 回目: 停止後も detectChan が残っている", i)
		}
		within("二重の StopWatcher", StopWatcher)
		within("再起動の StartWatcher", func() error { return StartWatcher(cfg) })
		if !Running() {
			t.Fatalf("%d 回目: 再起動後に Running() が false", i)
		}
	}
	within("最後の StopWatcher", StopWatcher)
}

// TestStartWatcherDisabled は Enabled=false なら何も起動しないことを確かめる。
func TestStartWatcherDisabled(t *testing.T) {
	if err := StartWatcher(Config{Enabled: false}); err != nil {
		t.Fatal(err)
	}
	if Running() {
		_ = StopWatcher()
		t.Fatal("Enabled=false なのに起動した")
	}
}
