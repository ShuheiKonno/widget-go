//go:build windows

package main

import (
	"encoding/json"
	"runtime"
	"syscall"
	"testing"
	"unsafe"
)

func TestAllDesktopsExStyle(t *testing.T) {
	const other = WS_EX_LAYERED | 0x00000008 // レイヤード + WS_EX_TOPMOST。触れてはいけないビット
	cases := []struct {
		name string
		ex   uintptr
		on   bool
		want uintptr
	}{
		{"ON: ツールウィンドウを足す", other, true, other | wsExToolWindow},
		{"ON: アプリウィンドウ指定は外す", other | wsExAppWindow, true, other | wsExToolWindow},
		{"ON: すでに ON なら変わらない", other | wsExToolWindow, true, other | wsExToolWindow},
		{"OFF: ツールウィンドウを外す", other | wsExToolWindow, false, other},
		{"OFF: すでに OFF なら変わらない", other, false, other},
	}
	for _, c := range cases {
		if got := allDesktopsExStyle(c.ex, c.on); got != c.want {
			t.Errorf("%s: allDesktopsExStyle(%#x, %v) = %#x, want %#x", c.name, c.ex, c.on, got, c.want)
		}
	}
}

// 設定が無い旧バージョンの config.json でも、既定は OFF のまま。設定は JSON のキー allDesktops に保存される。
func TestAllDesktopsConfig(t *testing.T) {
	if defaultConfig().AllDesktops {
		t.Error("既定は OFF のはず")
	}
	c := defaultConfig()
	if err := json.Unmarshal([]byte(`{"topmost": true}`), &c); err != nil || c.AllDesktops {
		t.Errorf("キーが無い旧設定は OFF のまま: err=%v allDesktops=%v", err, c.AllDesktops)
	}
	if err := json.Unmarshal([]byte(`{"allDesktops": true}`), &c); err != nil || !c.AllDesktops {
		t.Errorf("キーがあれば読み込む: err=%v allDesktops=%v", err, c.AllDesktops)
	}
}

// 実際の Win32 ウィンドウで、スタイルの付け外しと、表示状態が変わらないことを確かめる。
// 画面外の小さなウィンドウを作る。作れない環境（デスクトップが無い CI など）ではスキップする。
func TestApplyAllDesktops(t *testing.T) {
	runtime.LockOSThread() // ウィンドウは作った OS スレッドで操作する（traytoggle_test.go と同じ理由）
	class, _ := syscall.UTF16PtrFromString("STATIC")
	title, _ := syscall.UTF16PtrFromString("alldesktops-test")
	const wsPopup = 0x80000000
	off := ^uintptr(31999) // -32000
	hwnd, _, _ := procCreateWindowEx.Call(0, uintptr(unsafe.Pointer(class)), uintptr(unsafe.Pointer(title)),
		wsPopup, off, off, 100, 100, 0, 0, 0, 0)
	if hwnd == 0 {
		runtime.UnlockOSThread()
		t.Skip("テスト用のウィンドウを作れない環境")
	}
	destroy := user32.NewProc("DestroyWindow")
	t.Cleanup(func() {
		destroy.Call(hwnd)
		runtime.UnlockOSThread()
	})

	exStyle := func() uintptr {
		ex, _, _ := procGetWindowLong.Call(hwnd, uintptr(exStyleIndex))
		return ex
	}
	visible := func() bool { v, _, _ := procIsWindowVisible.Call(hwnd); return v != 0 }

	// 表示中: スタイルが付き、表示のまま。ほかのビット（レイヤード）は残る。
	procSetWindowLong.Call(hwnd, uintptr(exStyleIndex), exStyle()|WS_EX_LAYERED)
	procShowWindow.Call(hwnd, swShowNoActivate)
	applyAllDesktops(hwnd, true)
	if exStyle()&wsExToolWindow == 0 || exStyle()&WS_EX_LAYERED == 0 || !visible() {
		t.Errorf("ON(表示中): ex=%#x visible=%v", exStyle(), visible())
	}
	applyAllDesktops(hwnd, false)
	if exStyle()&wsExToolWindow != 0 || exStyle()&WS_EX_LAYERED == 0 || !visible() {
		t.Errorf("OFF(表示中): ex=%#x visible=%v", exStyle(), visible())
	}

	// 非表示（トレイ格納中など）: スタイルだけ変わり、表示はされない。
	procShowWindow.Call(hwnd, SW_HIDE)
	applyAllDesktops(hwnd, true)
	if exStyle()&wsExToolWindow == 0 || visible() {
		t.Errorf("ON(非表示): ex=%#x visible=%v", exStyle(), visible())
	}
}
