package main

import (
	"runtime"
	"syscall"
	"unsafe"
)

// ログオフ・シャットダウンの通知（WM_QUERYENDSESSION / WM_ENDSESSION）を受けて、集中モードで変えたものを戻す。
//
// この 2 つは最上位ウィンドウにだけ届く。トレイのウィンドウはメッセージ専用ウィンドウ（HWND_MESSAGE）で、
// メインのウィンドウは go-webview2 のものなので、専用の非表示の最上位ウィンドウを、専用のスレッドで持つ。
// 隠したウィンドウ自体は、ログオフでプロセスごと消えるので問題にならない。戻すのは、レジストリに残る通知スイッチ
// （止めたまま次回のログオンまで残ってしまう）が主な目的。

const (
	wmQueryEndSession = 0x0011
	wmEndSession      = 0x0016
	wsPopup           = 0x80000000
)

var sessionWndProc uintptr // syscall.NewCallback は数に上限があるので、一度だけ作って保持する

func sessionWndProcFunc(hwnd, msg, wParam, lParam uintptr) uintptr {
	switch msg {
	case wmQueryEndSession:
		return 1 // 終了を妨げない
	case wmEndSession:
		if wParam != 0 { // 終了が確定した
			pomodoroShutdown()
		}
		return 0
	}
	ret, _, _ := procDefWindowProc.Call(hwnd, msg, wParam, lParam)
	return ret
}

// startSessionWatcher は、セッション終了の通知を受ける非表示のウィンドウを作る。起動時に 1 回だけ呼ぶ。
// 作れなくても、ほかの機能には影響させない（次の起動時の復元が、これを補う）。
func startSessionWatcher() {
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		className, _ := syscall.UTF16PtrFromString("SystemWidgetSessionClass")
		hInstance, _, _ := procGetModuleHandle.Call(0)
		sessionWndProc = syscall.NewCallback(sessionWndProcFunc)
		wcx := WNDCLASSEXW{LpfnWndProc: sessionWndProc, HInstance: hInstance, LpszClassName: className}
		wcx.CbSize = uint32(unsafe.Sizeof(wcx))
		procRegisterClassEx.Call(uintptr(unsafe.Pointer(&wcx)))
		hwnd, _, _ := procCreateWindowEx.Call(0, uintptr(unsafe.Pointer(className)), 0, wsPopup, 0, 0, 0, 0, 0, 0, hInstance, 0)
		if hwnd == 0 {
			return
		}
		var m MSG
		for {
			ret, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
			if int32(ret) <= 0 {
				return
			}
			procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
			procDispatchMessage.Call(uintptr(unsafe.Pointer(&m)))
		}
	}()
}
