package main

import (
	"fmt"
	"sync/atomic"
	"time"

	"golang.org/x/sys/windows/registry"

	"widget-go/aiusage"
)

// 起動時のちらつき対策。
//
// go-webview2 は既定でウィンドウを作成直後に表示するため、位置や中身が決まる前の窓（白や暗い四角、
// 画面中央の窓）が見えてしまう。そこで third_party/go-webview2 に WindowOptions.Hidden を足し、
// メインウィンドウは非表示で作る。位置とフレームを整えたあと、完全な透明（alpha 0）で表示状態にして
// WebView2 に実際に描画させ（SW_HIDE のままだと、表示したときに黒い面が数フレーム出る）、
// フロントの準備完了（/api/ready）かフォールバックのタイマーで、不透明に戻して見せる。

// startupShowTimeout は、フロントの準備完了が来なくても、メインウィンドウを見せるまでの上限。
// JS の失敗などで窓が出ないままになるのを防ぐ。
const startupShowTimeout = 3 * time.Second

const swShowNoActivate = 4 // SW_SHOWNOACTIVATE

var (
	mainShown      atomic.Bool
	mainStealth    atomic.Bool // 完全な透明で表示状態にしている間（見せる前）
	procSetFocus   = user32.NewProc("SetFocus")
	processStart   = time.Now()
	// 何で、起動から何 ms 後に見せたかを notify.log に 1 行残す（準備完了が間に合わず
	// フォールバックの timeout になっていないかの切り分け用）。テストで差し替える。
	startupShowLog = func(reason string) {
		aiusage.Logf("main window shown reason=%s after=%dms", reason, time.Since(processStart).Milliseconds())
	}
)

// beginMainWindowStealth は、非表示で作ったメインウィンドウを、完全な透明のまま表示状態にする。
// 位置とフレームを整えたあとに、UI スレッドから呼ぶこと。
func beginMainWindowStealth() {
	hwnd := windowHandle.Load()
	if hwnd == 0 {
		return
	}
	gwlExStyle := int32(GWL_EXSTYLE)
	ex, _, _ := procGetWindowLong.Call(hwnd, uintptr(gwlExStyle))
	procSetWindowLong.Call(hwnd, uintptr(gwlExStyle), ex|WS_EX_LAYERED)
	procSetLayeredWindowAttributes.Call(hwnd, 0, 0, LWA_ALPHA)
	mainStealth.Store(true)
	procShowWindow.Call(hwnd, swShowNoActivate)
}

// endMainWindowStealth は、透明にしていたメインウィンドウを元の状態（設定の透過か、不透明）へ戻す。
func endMainWindowStealth() {
	if !mainStealth.CompareAndSwap(true, false) {
		return
	}
	hwnd := windowHandle.Load()
	if hwnd == 0 {
		return
	}
	transparentMu.Lock()
	tr := transparent
	transparentMu.Unlock()
	if tr {
		setTransparent(true)
		return
	}
	// 設定の透過が OFF なら、起動前と同じく、レイヤードウィンドウでない状態に戻す。
	gwlExStyle := int32(GWL_EXSTYLE)
	ex, _, _ := procGetWindowLong.Call(hwnd, uintptr(gwlExStyle))
	procSetWindowLong.Call(hwnd, uintptr(gwlExStyle), ex&^WS_EX_LAYERED)
	procSetWindowPos.Call(hwnd, 0, 0, 0, 0, 0, SWP_FRAMECHANGED|SWP_NOMOVE|SWP_NOZORDER|SWP_NOSIZE)
}

// showMainWindowOnce は、透明にしておいたメインウィンドウを、初回に 1 回だけ見せる。
// どの goroutine からでも呼べる（2 回目以降は何もしない）。
func showMainWindowOnce(reason string) {
	if !mainShown.CompareAndSwap(false, true) {
		return
	}
	startupShowLog(reason)
	dispatchUI(func() {
		hwnd := windowHandle.Load()
		if hwnd == 0 {
			return
		}
		endMainWindowStealth()
		// 見せる前にトレイへ格納されていた場合は、前面化しない。
		viewModeMu.RLock()
		inTray := viewMode == viewModeTray
		viewModeMu.RUnlock()
		if inTray {
			return
		}
		// go-webview2 は、Hidden のときは作成時の SetFocus も行わないので、ここで行う。
		procSetFocus.Call(hwnd)
		alwaysOnTopMu.Lock()
		top := alwaysOnTop
		alwaysOnTopMu.Unlock()
		if top {
			setTopmost(true)
		}
	})
}

// rgb は起動時の背景色。
type rgb struct{ r, g, b uint8 }

var (
	// startupBgDark / startupBgLight は ui/index.html の既定（ダーク）とライトテーマの背景色に近い色。
	startupBgDark  = rgb{0x0D, 0x0D, 0x12}
	startupBgLight = rgb{0xF2, 0xF4, 0xFB}
)

// startupBackground はテーマ設定（"dark" | "light" | "system"）から WebView2 の既定背景色を決める。
// "system" と未知の値は、Windows のアプリのテーマ（systemLight）に従う。
// フロント側（ui/index.html の applyTheme）と同じ判定。表示前の保険で、通常は見えない。
func startupBackground(theme string, systemLight bool) rgb {
	light := systemLight
	switch theme {
	case "light":
		light = true
	case "dark":
		light = false
	}
	if light {
		return startupBgLight
	}
	return startupBgDark
}

// argbHex は WEBVIEW2_DEFAULT_BACKGROUND_COLOR 用の ARGB 16 進（不透明）。
func (c rgb) argbHex() string { return fmt.Sprintf("FF%02X%02X%02X", c.r, c.g, c.b) }

// systemUsesLightTheme は Windows のアプリのテーマがライトか。読めない場合は false（ダーク）。
func systemUsesLightTheme() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER,
		`Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	v, _, err := k.GetIntegerValue("AppsUseLightTheme")
	return err == nil && v == 1
}
