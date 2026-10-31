package main

import (
	"math"
	"unsafe"
)

var procGetClientRect = user32.NewProc("GetClientRect")

// 勤務時間の入力ウィンドウ。コンパクト表示（幅 130px）では、勤務時間の区画に入力欄を置けないため、
// 区画をクリックすると、ウィジェットの横に小さな別ウィンドウを開く。作りは設定ウィンドウと同じ
// （settingswin.go の settingsWindow。非表示で作り、ページの準備完了で見せ、閉じると破棄する）。
// 中身は ui/worktimer.html で、本体の区画と同じ断片（ui/work/）を使う。

const (
	workWindowTitle     = "勤務時間 - システムモニター" // メインの「システムモニター」と完全一致させない（多重起動の判定が FindWindow のため）
	workWindowWidth     = 340               // 論理 px。タイトルバーと枠を含む外枠の大きさ。タイトルが切れない幅
	workWindowHeight    = 350               // 開くときの高さ（上限）。ページが内容を測って、これ以下に縮める（/api/resizeworktimer）
	workWindowMinHeight = 160               // 縮めるときの下限（論理 px）
	swpNoActivate       = 0x0010
	workWindowGap       = 8 // ウィジェットとの間隔（論理 px）
)

var workWin = settingsWindow{title: workWindowTitle, path: "/worktimer", rect: workWindowRect}

// openWorkTimerWindow は勤務時間の入力ウィンドウを開く（開いていれば前面に出す）。どの goroutine からでも呼べる。
func openWorkTimerWindow() { dispatchUI(workWin.open) }

// workWindowRect は、ウィジェットの横に置く入力ウィンドウの位置と大きさ（物理 px）。
func workWindowRect(scale float64) (x, y, w, h int32) {
	w = int32(math.Round(workWindowWidth * scale))
	h = int32(math.Round(workWindowHeight * scale))
	work := RECT{Left: 0, Top: 0, Right: int32(math.Round(1920 * scale)), Bottom: int32(math.Round(1080 * scale))}
	var main RECT
	if hwnd := windowHandle.Load(); hwnd != 0 {
		procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&main)))
		if hmon, _, _ := procMonitorFromWindow.Call(hwnd, MONITOR_DEFAULTTONEAREST); hmon != 0 {
			var mi MONITORINFO
			mi.CbSize = uint32(unsafe.Sizeof(mi))
			if ok, _, _ := procGetMonitorInfoW.Call(hmon, uintptr(unsafe.Pointer(&mi))); ok != 0 {
				work = mi.RcWork
			}
		}
	} else {
		main = RECT{Left: work.Right - w, Top: work.Top, Right: work.Right, Bottom: work.Top + h}
	}
	return placeBeside(work, main, w, h, int32(math.Round(workWindowGap*scale)))
}

// placeBeside は、anchor（ウィジェット）の左に w×h を置く。左に入らなければ右に置く。どちらにも入らなければ、
// 作業領域の端に寄せる（ウィジェットに少し重なる）。縦は anchor の上端にそろえ、作業領域に収める。
// 作業領域より大きいときは縮める。
func placeBeside(work, anchor RECT, w, h, gap int32) (x, y, rw, rh int32) {
	ww, wh := work.Right-work.Left, work.Bottom-work.Top
	rw, rh = min(w, ww), min(h, wh)
	switch {
	case anchor.Left-gap-rw >= work.Left:
		x = anchor.Left - gap - rw
	case anchor.Right+gap+rw <= work.Right:
		x = anchor.Right + gap
	default:
		x = max(work.Left, work.Right-rw)
	}
	y = min(max(anchor.Top, work.Top), work.Bottom-rh)
	return
}

// fitWindowHeight は、内容の高さ（cssH。CSS px）に合わせたウィンドウの外枠の高さ（物理 px）。
// ncH はタイトルバーと枠の分。min〜max に収める。
func fitWindowHeight(cssH int32, scale float64, ncH, minH, maxH int32) int32 {
	h := int32(math.Round(float64(cssH)*scale)) + ncH
	return min(max(h, minH), maxH)
}

// resizeWorkWindow は、入力ウィンドウの高さをページの内容に合わせる（幅と位置は変えない）。余白が残らないようにするため。
// 開くときの高さ（workWindowHeight）が上限で、これを超えて下へ伸びることはない。どの goroutine からでも呼べる。
func resizeWorkWindow(cssH int32) {
	dispatchUI(func() {
		hwnd := workWin.currentHwnd()
		if hwnd == 0 || cssH <= 0 {
			return
		}
		var win, client RECT
		procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&win)))
		procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&client)))
		ncH := (win.Bottom - win.Top) - (client.Bottom - client.Top)
		scale := getWindowDpiScale()
		h := fitWindowHeight(cssH, scale, ncH,
			int32(math.Round(workWindowMinHeight*scale)), int32(math.Round(workWindowHeight*scale)))
		procSetWindowPos.Call(hwnd, 0, 0, 0, uintptr(win.Right-win.Left), uintptr(h), SWP_NOMOVE|SWP_NOZORDER|swpNoActivate)
	})
}

// notifyWorkTimerChanged は、勤務時間の状態が変わったことを、本体と入力ウィンドウの両方に知らせる。
// 各ページは /api/system を読み直して勤務時間の部分だけを描き直す（updateData は呼ばない。
// Network / Disk のグラフ履歴に同じサンプルが二重に積まれるため）。UI のない環境（テスト）では何もしない。
func notifyWorkTimerChanged() {
	dispatchUI(func() {
		const js = "window.__workChanged&&window.__workChanged()"
		if p := mainWebView.Load(); p != nil {
			(*p).Eval(js)
		}
		workWin.eval(js)
	})
}
