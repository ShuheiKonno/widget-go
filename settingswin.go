package main

import (
	"math"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"widget-go/aiusage"

	"github.com/jchv/go-webview2"
)

// 設定は別ウィンドウ（ui/settings.html、/settings）で開く。同じ作りのウィンドウを、勤務時間の入力用にも使う
// （worktimerwin.go の workWin。タイトル・URL・位置だけが違う）。メインのウィジェットは狭いので、
// 広い画面で設定できるようにする。ウィンドウはメインと同じ UI スレッド・同じ Run ループの上に
// 作る（aiusage の補助ウィンドウと同じ作り）。閉じると破棄してメモリを解放し、開くたびに作り直す。
//
// 作成直後の表示でちらつかないよう（docs/startup-flicker.md）、非表示で作り、位置を決めて完全な透明で
// 表示状態にし、ページの準備完了（/api/settingsready）かタイマーで見せる。

const (
	settingsTitle     = "設定 - システムモニター"
	settingsWidth     = 920 // 論理 px
	settingsHeight    = 780
	settingsRevealMax = 3 * time.Second // ページの準備完了が来なくても、見せるまでの上限

	wmNCDestroy  = 0x0082
	dwmwaDarkMod = 20 // DWMWA_USE_IMMERSIVE_DARK_MODE
)

var (
	// GWLP_WNDPROC。定数にすると uintptr への変換でオーバーフローになるため var で持つ。
	gwlpWndProcIdx = int32(-4)

	// serverPort と webviewDataPath は main() が決める。設定ウィンドウの生成に使う。
	serverPort      int
	webviewDataPath string

	// 設定ウィンドウ。勤務時間のウィンドウ（workWin）も同じ型で、タイトル・URL・位置の決め方だけが違う。
	settingsWin = settingsWindow{title: settingsTitle, path: "/settings", rect: settingsWindowRect}

	procSetWindowLongPtr = user32.NewProc("SetWindowLongPtrW")
	procCallWindowProc   = user32.NewProc("CallWindowProcW")
)

// settingsWindow は、メインとは別の WebView2 ウィンドウ 1 つ分（設定、勤務時間）。
type settingsWindow struct {
	title string                                 // ウィンドウのタイトル。メインの「システムモニター」と完全一致させない（多重起動の判定が FindWindow のため）
	path  string                                 // 開くページのパス
	rect  func(scale float64) (x, y, w, h int32) // 開くときの位置と大きさ（物理 px）

	mu       sync.Mutex
	inst     webview2.WebView
	hwnd     uintptr
	revealed bool
	orig     atomic.Uintptr // サブクラス化前のウィンドウプロシージャ
	cb       uintptr        // このウィンドウのサブクラス化プロシージャ（初回の open で作る）
}

// openSettingsWindow は設定ウィンドウを開く（開いていれば前面に出す）。どの goroutine からでも呼べる。
func openSettingsWindow() { dispatchUI(settingsWin.open) }

// closeSettingsWindow は設定ウィンドウを閉じる。どの goroutine からでも呼べる。
func closeSettingsWindow() { settingsWin.closeWindow() }

// closeWindow はウィンドウを閉じる。どの goroutine からでも呼べる。
func (s *settingsWindow) closeWindow() {
	dispatchUI(func() {
		s.mu.Lock()
		hwnd := s.hwnd
		s.mu.Unlock()
		if hwnd != 0 {
			procPostMessageW.Call(hwnd, WM_CLOSE, 0, 0)
		}
	})
}

// open は UI スレッドで呼ぶこと。
func (s *settingsWindow) open() {
	s.mu.Lock()
	hwnd, has := s.hwnd, s.inst != nil
	revealed := s.revealed
	s.mu.Unlock()
	if has {
		// 見せる前（準備中）に押し直されても、何もしない。見せた後なら前面に出す。
		if revealed {
			raiseWindow(hwnd)
		}
		return
	}

	scale := getWindowDpiScale()
	x, y, w, h := s.rect(scale)
	inst := webview2.NewWithOptions(webview2.WebViewOptions{
		Debug:    false,
		DataPath: webviewDataPath,
		WindowOptions: webview2.WindowOptions{
			Title:  s.title,
			Width:  uint(w),
			Height: uint(h),
			Hidden: true,
		},
	})
	if inst == nil {
		aiusage.Logf("settings window: WebView の生成に失敗しました")
		return
	}
	hwnd = uintptr(inst.Window())

	s.mu.Lock()
	s.inst, s.hwnd, s.revealed = inst, hwnd, false
	s.mu.Unlock()

	// go-webview2 は WM_DESTROY で PostQuitMessage を呼び、メインを含むアプリ全体を終了させる。
	// サブクラス化して握りつぶす（aiusage の補助ウィンドウと同じ）。
	if s.cb == 0 {
		s.cb = syscall.NewCallback(s.wndProc)
	}
	orig, _, _ := procSetWindowLongPtr.Call(hwnd, uintptr(gwlpWndProcIdx), s.cb)
	s.orig.Store(orig)

	applySettingsWindowTheme(hwnd)
	procSetWindowPos.Call(hwnd, 0, uintptr(uint32(x)), uintptr(uint32(y)), uintptr(w), uintptr(h), SWP_NOZORDER)
	showTransparent(hwnd)
	inst.Navigate(pageURL(serverPort, s.path))
	// このウィンドウ（hwnd）にだけ効くタイマー。閉じてすぐ開き直したとき、前のタイマーが
	// 読み込み前の新しいウィンドウを見せてしまわないようにする。
	time.AfterFunc(settingsRevealMax, func() { s.revealWindow(hwnd) })

	// 生成中（Embed の入れ子メッセージループの間）に積まれた Dispatch は、次に誰かが
	// Dispatch を積むまで止まる。空の Dispatch を 1 回積んで掃き出す。
	dispatchUI(func() {})
}

// reveal は、透明にしておいた現在の設定ウィンドウを見せて前面に出す。1 回だけ。どの goroutine からでも呼べる。
// ページの準備完了（/api/settingsready）から呼ぶ。
func (s *settingsWindow) reveal() { s.revealWindow(0) }

// revealWindow は reveal の本体。want が 0 以外なら、現在のウィンドウがそのウィンドウのときだけ見せる
// （タイマーが、閉じて開き直した別のウィンドウを見せないため）。
func (s *settingsWindow) revealWindow(want uintptr) {
	dispatchUI(func() {
		s.mu.Lock()
		hwnd, revealed := s.hwnd, s.revealed
		if hwnd != 0 && (want == 0 || want == hwnd) {
			s.revealed = true
		}
		s.mu.Unlock()
		if hwnd == 0 || revealed || (want != 0 && want != hwnd) {
			return
		}
		clearLayered(hwnd)
		raiseWindow(hwnd)
	})
}

// onDestroyed はウィンドウが破棄された後（WM_NCDESTROY）に、状態を片づける。
func (s *settingsWindow) onDestroyed() {
	s.mu.Lock()
	s.inst, s.hwnd, s.revealed = nil, 0, false
	s.mu.Unlock()
}

// eval は、開いているウィンドウのページで JS を実行する（開いていなければ何もしない）。UI スレッドで呼ぶこと。
func (s *settingsWindow) eval(js string) {
	s.mu.Lock()
	inst := s.inst
	s.mu.Unlock()
	if inst != nil {
		inst.Eval(js)
	}
}

func (s *settingsWindow) currentHwnd() uintptr {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hwnd
}

// wndProc はこのウィンドウのサブクラス化プロシージャ。
//   - WM_DESTROY: 握りつぶす（go-webview2 が PostQuitMessage を呼び、アプリ全体が終了するため）。
//   - WM_NCDESTROY: 破棄が済んだので、状態を片づけて元のプロシージャへ進む。
//
// × で閉じたとき（WM_CLOSE）は、go-webview2 の本来の処理（DestroyWindow）に任せる。
func (s *settingsWindow) wndProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	switch msg {
	case WM_DESTROY:
		return 0
	case wmNCDestroy:
		s.onDestroyed()
	}
	r, _, _ := procCallWindowProc.Call(s.orig.Load(), hwnd, msg, wParam, lParam)
	return r
}

func raiseWindow(hwnd uintptr) {
	if hwnd == 0 {
		return
	}
	if iconic, _, _ := procIsIconic.Call(hwnd); iconic != 0 {
		procShowWindow.Call(hwnd, SW_RESTORE)
	}
	procSetForegroundWindow.Call(hwnd)
}

// settingsWindowRect は、ウィジェットがあるモニタの作業領域の中央に置く設定ウィンドウの位置と大きさ
// （物理 px）。作業領域より大きくならないようにする。
func settingsWindowRect(scale float64) (x, y, w, h int32) {
	w = int32(math.Round(settingsWidth * scale))
	h = int32(math.Round(settingsHeight * scale))
	work := RECT{Left: 0, Top: 0, Right: int32(math.Round(1920 * scale)), Bottom: int32(math.Round(1080 * scale))}
	if main := windowHandle.Load(); main != 0 {
		if hmon, _, _ := procMonitorFromWindow.Call(main, MONITOR_DEFAULTTONEAREST); hmon != 0 {
			var mi MONITORINFO
			mi.CbSize = uint32(unsafe.Sizeof(mi))
			if ok, _, _ := procGetMonitorInfoW.Call(hmon, uintptr(unsafe.Pointer(&mi))); ok != 0 {
				work = mi.RcWork
			}
		}
	}
	return centerInWork(work, w, h, int32(math.Round(16*scale)))
}

// centerInWork は work の中央に w×h を置く。work からはみ出す場合は、margin を残して縮める。
func centerInWork(work RECT, w, h, margin int32) (x, y, rw, rh int32) {
	ww, wh := work.Right-work.Left, work.Bottom-work.Top
	rw, rh = w, h
	if rw > ww-2*margin {
		rw = ww - 2*margin
	}
	if rh > wh-2*margin {
		rh = wh - 2*margin
	}
	x = work.Left + (ww-rw)/2
	y = work.Top + (wh-rh)/2
	return
}

// settingsDarkTitleBar は、設定ウィンドウのタイトルバーをダークにするか。
// テーマ（"dark" | "light" | "system"）に従い、"system" は Windows のアプリのテーマに合わせる。
func settingsDarkTitleBar(theme string, systemLight bool) bool {
	return startupBackground(theme, systemLight) == startupBgDark
}

// applySettingsWindowTheme はタイトルバーをダーク / ライトにする。UI スレッドで呼ぶこと。
func applySettingsWindowTheme(hwnd uintptr) {
	if hwnd == 0 {
		return
	}
	dark := int32(0)
	if settingsDarkTitleBar(snapshotConfig().Theme, systemUsesLightTheme()) {
		dark = 1
	}
	procDwmSetWindowAttribute.Call(hwnd, dwmwaDarkMod, uintptr(unsafe.Pointer(&dark)), unsafe.Sizeof(dark))
}

// notifyMainSettingsChanged は、設定ウィンドウでの変更をメインのウィジェットへ知らせる。
// メイン側は /api/settings を読み直して反映する（window.__settingsChanged）。
// あわせて、テーマが変わっているかもしれないので、設定ウィンドウのタイトルバーも合わせる。
func notifyMainSettingsChanged() {
	dispatchUI(func() {
		const js = "window.__settingsChanged&&window.__settingsChanged()"
		if p := mainWebView.Load(); p != nil {
			(*p).Eval(js)
		}
		// 勤務時間の入力ウィンドウも、開いていれば、テーマなどを取り直す
		workWin.eval(js)
		applySettingsWindowTheme(settingsWin.currentHwnd())
		applySettingsWindowTheme(workWin.currentHwnd())
	})
}
