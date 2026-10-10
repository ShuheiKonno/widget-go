package aiusage

import "syscall"

// 補助 WebView のウィンドウ操作に必要な Win32 API。メインの宣言（main パッケージ）とは
// 独立に持つ（名前の衝突を避けるため）。
var (
	user32                  = syscall.NewLazyDLL("user32.dll")
	procSetWindowPos        = user32.NewProc("SetWindowPos")
	procGetWindowLong       = user32.NewProc("GetWindowLongW")
	procSetWindowLong       = user32.NewProc("SetWindowLongW")
	procSetWindowLongPtr    = user32.NewProc("SetWindowLongPtrW")
	procCallWindowProc      = user32.NewProc("CallWindowProcW")
	procShowWindow          = user32.NewProc("ShowWindow")
	procSetForegroundWindow = user32.NewProc("SetForegroundWindow")
)

const (
	wmClose          = 0x0010
	wmDestroy        = 0x0002
	wmNCDestroy      = 0x0082
	swShow           = 5
	swShowNoActivate = 4
	swpNoZOrder      = 0x0004
	swpFrameChgd     = 0x0020

	wsExAppWindow  = 0x00040000
	wsExToolWindow = 0x00000080
)

// 定数で書くと const 評価で uintptr のオーバーフローになるため var で保持する。
var (
	gwlExStyle   = int32(-20)
	gwlpWndProc  = int32(-4)
	offscreenPos = int32(-32000)
)

// moveOffscreen はウィンドウをタスクバーから外し、画面外 (-32000,-32000) へ置く。
// SW_HIDE にしないのは、隠し窓だと WebView2 が setInterval / requestAnimationFrame を
// スロットルする可能性があるため（ClaudeMonitor と同じ設計）。UI スレッドで呼ぶこと。
func moveOffscreen(hwnd uintptr) {
	if hwnd == 0 {
		return
	}
	ex, _, _ := procGetWindowLong.Call(hwnd, uintptr(gwlExStyle))
	procSetWindowLong.Call(hwnd, uintptr(gwlExStyle), (ex&^wsExAppWindow)|wsExToolWindow)
	procSetWindowPos.Call(hwnd, 0, uintptr(uint32(offscreenPos)), uintptr(uint32(offscreenPos)),
		500, 700, swpNoZOrder|swpFrameChgd)
}

// moveOnscreen はログイン用にウィンドウをタスクバーへ出し、画面内へ置いて前面にする。
// UI スレッドで呼ぶこと。
func moveOnscreen(hwnd uintptr, x, y int32) {
	if hwnd == 0 {
		return
	}
	ex, _, _ := procGetWindowLong.Call(hwnd, uintptr(gwlExStyle))
	procSetWindowLong.Call(hwnd, uintptr(gwlExStyle), (ex&^wsExToolWindow)|wsExAppWindow)
	procSetWindowPos.Call(hwnd, 0, uintptr(uint32(x)), uintptr(uint32(y)), 800, 800, swpNoZOrder|swpFrameChgd)
	procShowWindow.Call(hwnd, swShow)
	procSetForegroundWindow.Call(hwnd)
}
