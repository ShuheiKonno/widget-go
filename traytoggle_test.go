package main

import (
	"syscall"
	"testing"
	"unsafe"
)

// 表示中と見なすのは「トレイへ格納中でなく、可視で、最小化されていない」ときだけ。
func TestWindowShown(t *testing.T) {
	cases := []struct {
		name                        string
		inTrayMode, visible, iconic bool
		want                        bool
	}{
		{"通常に表示中", false, true, false, true},
		{"トレイへ格納中", true, false, false, false},
		{"格納中の印だけ残って可視（不整合）でも、格納中として扱う", true, true, false, false},
		{"非表示", false, false, false, false},
		{"OS で最小化", false, true, true, false},
		{"非表示かつ最小化", false, false, true, false},
	}
	for _, c := range cases {
		if got := windowShown(c.inTrayMode, c.visible, c.iconic); got != c.want {
			t.Errorf("%s: windowShown(%v,%v,%v) = %v, want %v", c.name, c.inTrayMode, c.visible, c.iconic, got, c.want)
		}
	}
}

// 実際の Win32 ウィンドウで、トレイアイコンの左クリックの動作（トグル）を確かめる。
// 画面外の小さなウィンドウを作る。作れない環境（デスクトップが無い CI など）ではスキップする。
func TestToggleWindowFromTray(t *testing.T) {
	class, _ := syscall.UTF16PtrFromString("STATIC")
	title, _ := syscall.UTF16PtrFromString("toggle-test")
	const wsPopup = 0x80000000
	off := ^uintptr(31999) // -32000
	hwnd, _, _ := procCreateWindowEx.Call(0, uintptr(unsafe.Pointer(class)), uintptr(unsafe.Pointer(title)),
		wsPopup, off, off, 100, 100, 0, 0, 0, 0)
	if hwnd == 0 {
		t.Skip("テスト用のウィンドウを作れない環境")
	}
	destroy := user32.NewProc("DestroyWindow")
	t.Cleanup(func() { destroy.Call(hwnd) })

	// グローバルな状態を差し替え、終了時に戻す。
	prevHandle := windowHandle.Load()
	trayMu.Lock()
	prevActive := trayActive
	trayActive = true // hideWindowToTray は、トレイが登録されているときだけ隠す
	trayMu.Unlock()
	viewModeMu.Lock()
	prevMode, prevBefore := viewMode, viewModeBeforeTray
	viewMode, viewModeBeforeTray = viewModeNormal, viewModeNormal
	viewModeMu.Unlock()
	windowHandle.Store(hwnd)
	t.Cleanup(func() {
		windowHandle.Store(prevHandle)
		trayMu.Lock()
		trayActive = prevActive
		trayMu.Unlock()
		viewModeMu.Lock()
		viewMode, viewModeBeforeTray = prevMode, prevBefore
		viewModeMu.Unlock()
	})

	visible := func() bool { v, _, _ := procIsWindowVisible.Call(hwnd); return v != 0 }
	mode := func() int { viewModeMu.RLock(); defer viewModeMu.RUnlock(); return viewMode }

	procShowWindow.Call(hwnd, SW_SHOW)
	if !isMainWindowShown() || !visible() {
		t.Fatal("前提: 表示中のはず")
	}

	// 表示中のクリック → 格納（最小化）
	toggleWindowFromTray()
	if visible() || isMainWindowShown() || mode() != viewModeTray {
		t.Errorf("表示中のクリックで格納されるはず: visible=%v shown=%v mode=%d", visible(), isMainWindowShown(), mode())
	}

	// 格納中のクリック → 表示
	toggleWindowFromTray()
	if !visible() || !isMainWindowShown() || mode() != viewModeNormal {
		t.Errorf("格納中のクリックで表示されるはず: visible=%v shown=%v mode=%d", visible(), isMainWindowShown(), mode())
	}

	// コンパクト表示で格納しても、戻すと元のモードに戻る
	viewModeMu.Lock()
	viewMode = viewModeCompact
	viewModeMu.Unlock()
	toggleWindowFromTray()
	toggleWindowFromTray()
	if mode() != viewModeCompact || !isMainWindowShown() {
		t.Errorf("コンパクトのまま戻るはず: mode=%d shown=%v", mode(), isMainWindowShown())
	}
	viewModeMu.Lock()
	viewMode = viewModeNormal
	viewModeMu.Unlock()

	// OS による最小化（Win+D など）→ 表示中ではないので、クリックで復元する
	procShowWindow.Call(hwnd, 6) // SW_MINIMIZE
	if iconic, _, _ := procIsIconic.Call(hwnd); iconic == 0 {
		t.Skip("このウィンドウは最小化できない（環境依存）")
	}
	if isMainWindowShown() {
		t.Error("最小化中は、表示中ではないはず")
	}
	toggleWindowFromTray()
	if iconic, _, _ := procIsIconic.Call(hwnd); iconic != 0 || !isMainWindowShown() {
		t.Errorf("最小化からクリックで復元されるはず: iconic=%v shown=%v", iconic != 0, isMainWindowShown())
	}

	// トレイが登録されていないときは隠さない（復帰手段が無くなるため）
	trayMu.Lock()
	trayActive = false
	trayMu.Unlock()
	toggleWindowFromTray()
	if !visible() {
		t.Error("トレイが未登録のときは、隠れてはいけない")
	}
}
