package aiusage

import (
	"runtime"
	"syscall"
	"testing"
	"unsafe"
)

var (
	procCreateWindowEx  = user32.NewProc("CreateWindowExW")
	procDestroyWindow   = user32.NewProc("DestroyWindow")
	procGetWindowRect   = user32.NewProc("GetWindowRect")
	procIsWindowVisible = user32.NewProc("IsWindowVisible")
)

type winRect struct{ left, top, right, bottom int32 }

func windowState(hwnd uintptr) (left, top int32, ex uint32) {
	var r winRect
	procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
	e, _, _ := procGetWindowLong.Call(hwnd, uintptr(gwlExStyle))
	return r.left, r.top, uint32(e)
}

// 補助ウィンドウの流れ（非表示で作る → 画面外へ置く → 画面外のまま表示 → ログインで画面内へ）が、
// 画面内に出ることなく進み、ログイン画面の表示（moveOnscreen）が効くことを確認する。
// WebView2 は使わず、Hidden で作った go-webview2 のウィンドウと同じ形（WS_VISIBLE なし）の窓で試す。
func TestAuthWindowFlowStaysOffscreenUntilLogin(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	class, _ := syscall.UTF16PtrFromString("STATIC")
	title, _ := syscall.UTF16PtrFromString("aiusage-offscreen-test")
	cwUseDefault := uint32(0x80000000)
	hwnd, _, err := procCreateWindowEx.Call(0, uintptr(unsafe.Pointer(class)), uintptr(unsafe.Pointer(title)),
		0xCF0000, uintptr(cwUseDefault), uintptr(cwUseDefault), 500, 700, 0, 0, 0, 0)
	if hwnd == 0 {
		t.Fatalf("CreateWindowEx: %v", err)
	}
	t.Cleanup(func() { procDestroyWindow.Call(hwnd) })

	if v, _, _ := procIsWindowVisible.Call(hwnd); v != 0 {
		t.Fatal("Hidden で作った窓は、作成直後は非表示のはず")
	}

	// create() と同じ順序: 画面外へ置いてから、画面外のまま表示状態にする。
	moveOffscreen(hwnd)
	procShowWindow.Call(hwnd, swShowNoActivate)
	left, top, ex := windowState(hwnd)
	if left != offscreenPos || top != offscreenPos {
		t.Errorf("表示した時点で画面外にない: (%d,%d)", left, top)
	}
	if ex&wsExToolWindow == 0 || ex&wsExAppWindow != 0 {
		t.Errorf("タスクバーに出ない形になっていない: ex=%#x", ex)
	}
	if v, _, _ := procIsWindowVisible.Call(hwnd); v == 0 {
		t.Error("画面外のまま表示状態にする（SW_HIDE にしない）")
	}

	// ログインが必要になったときの経路（showLogin → moveOnscreen）。
	moveOnscreen(hwnd, 100, 120)
	left, top, ex = windowState(hwnd)
	if left != 100 || top != 120 {
		t.Errorf("moveOnscreen で画面内に戻らない: (%d,%d)", left, top)
	}
	if ex&wsExAppWindow == 0 || ex&wsExToolWindow != 0 {
		t.Errorf("moveOnscreen でタスクバーに出る形に戻らない: ex=%#x", ex)
	}

	// ログイン後・× で閉じたとき（moveOffscreen）に、また画面外へ戻る。
	moveOffscreen(hwnd)
	if left, top, _ = windowState(hwnd); left != offscreenPos || top != offscreenPos {
		t.Errorf("moveOffscreen で画面外へ戻らない: (%d,%d)", left, top)
	}
}
