package main

import (
	"sync/atomic"

	"widget-go/aiusage"

	"github.com/jchv/go-webview2"
)

// mainWebView はメイン UI の WebView。go-webview2 の Run ループは自分の Dispatch キューしか
// 処理しないため、補助 WebView（aiusage）の生成・Eval・Win32 操作は、すべてこちらの
// Dispatch を通して UI スレッドへ寄せる。
var mainWebView atomic.Pointer[webview2.WebView]

func setMainWebView(w webview2.WebView) { mainWebView.Store(&w) }

// dispatchUI は f をメイン WebView の UI スレッドで実行する。メイン WebView の登録前
// （起動の最序盤）は、その場で実行する（このときは呼び出し側が主 goroutine にいる）。
func dispatchUI(f func()) {
	if p := mainWebView.Load(); p != nil {
		(*p).Dispatch(f)
		return
	}
	f()
}

// appDataRoot は %LOCALAPPDATA%\widget-go。認証データ（AuthWebView2 など）と notify.log を置く。
var appDataRoot string

// aiHost は aiusage.Host の実装。
type aiHost struct{}

func (aiHost) Dispatch(f func()) { dispatchUI(f) }

func (aiHost) Balloon(title, msg string, flag uint32) { showBalloon(title, msg, flag) }

// OnSnapshotChanged は、トレイの AI 使用量表示（trayDisplay = "usage"）を実装する T3 で使う。
func (aiHost) OnSnapshotChanged() {}

// OnAuthWindowHidden は、ログイン用の補助ウィンドウを閉じた直後に、メインウィンドウの
// 最前面と透過を再適用する。WebView2 の初期化やナビゲーション、別ウィンドウの表示で
// z-order が外れることがあるため（ClaudeMonitor でも同じ対策をしていた）。
func (aiHost) OnAuthWindowHidden() {
	alwaysOnTopMu.Lock()
	top := alwaysOnTop
	alwaysOnTopMu.Unlock()
	if top {
		setTopmost(true)
	}
}

func (aiHost) Settings() aiusage.Settings { return snapshotConfig().AI }

func (aiHost) MutateSettings(f func(*aiusage.Settings)) {
	mutateConfig(func(c *Config) { f(&c.AI) })
}

func (aiHost) DataRoot() string { return appDataRoot }
