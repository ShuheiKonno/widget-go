//go:build windows

package main

// 全仮想デスクトップに表示する（設定 allDesktops）。
//
// Windows には、ウィンドウを「全デスクトップに表示」にする公開 API が無い（IVirtualDesktopManager は
// 取得・判定・移動だけ）。非公開インターフェース（IVirtualDesktopPinnedApps など）は Windows のビルドごとに
// IID が変わるので使わない。代わりに、拡張スタイル WS_EX_TOOLWINDOW を使う。ツールウィンドウは仮想デスクトップの
// 管理外になり（IVirtualDesktopManager.GetWindowDesktopId が TYPE_E_ELEMENTNOTFOUND を返す）、どのデスクトップ
// でも表示されたままになる。AI 認証用の補助ウィンドウ（aiusage/win.go）も同じスタイルでタスクバーから外している。
//
// 副作用: ツールウィンドウはタスクバーと Alt+Tab に出ない。設定画面の説明にも書いている。

const (
	wsExToolWindow = 0x00000080
	wsExAppWindow  = 0x00040000
)

// exStyleIndex は GetWindowLong / SetWindowLong の GWL_EXSTYLE（-20）。定数のままだと uintptr に収まらないので変数で持つ。
var exStyleIndex = int32(GWL_EXSTYLE)

// allDesktopsExStyle は、拡張スタイル ex に「全デスクトップに表示」の指定を足す／外した値を返す。
// 他のビット（レイヤード・最前面など）には触れない。
func allDesktopsExStyle(ex uintptr, on bool) uintptr {
	if on {
		return (ex &^ wsExAppWindow) | wsExToolWindow
	}
	return ex &^ wsExToolWindow
}

// applyAllDesktops はウィンドウに設定を反映する。UI スレッドで呼ぶこと。
//
// 表示中のウィンドウは、スタイルを書き換えただけではシェルの管理（タスクバー・デスクトップへの所属）に
// 反映されないことがあるので、いったん隠してから書き換え、元の表示状態に戻す。表示していないとき
// （起動前・トレイ格納中）は、スタイルだけ書き換えて表示は変えない。
func applyAllDesktops(hwnd uintptr, on bool) {
	if hwnd == 0 {
		return
	}
	ex, _, _ := procGetWindowLong.Call(hwnd, uintptr(exStyleIndex))
	want := allDesktopsExStyle(ex, on)
	if want == ex {
		return
	}
	visible, _, _ := procIsWindowVisible.Call(hwnd)
	if visible != 0 {
		procShowWindow.Call(hwnd, SW_HIDE)
	}
	procSetWindowLong.Call(hwnd, uintptr(exStyleIndex), want)
	if visible != 0 {
		// 前面化はしない（設定画面から切り替えたあとに、ウィジェットがフォーカスを奪わないように）。
		procShowWindow.Call(hwnd, swShowNoActivate)
	}
}

// setAllDesktops は設定を保存し、すぐにメインウィンドウへ反映する。
func setAllDesktops(on bool) {
	mutateConfig(func(c *Config) { c.AllDesktops = on })
	dispatchUI(func() { applyAllDesktops(windowHandle.Load(), on) })
}
