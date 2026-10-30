//go:build windows

package main

import (
	"sync/atomic"
)

// 拡大・縮小（ズーム）。ウィジェット全体を WebView2 のズーム率で拡大縮小し、ウィンドウの大きさも
// それに合わせる。
//
// サイズには 2 種類の「論理 px」がある。
//   - レイアウト px = 物理 px ÷ (DPI 倍率 × ズーム)。フロントの CSS px と同じで、フロントとやり取りする
//     サイズと、config に保存する幅・高さ（Window.Width / Height / NormalWidth）に使う。ズームが変わっても
//     同じ中身は同じレイアウト px のまま。
//   - DPI 論理 px = 物理 px ÷ DPI 倍率。位置（Window.X / Y、右下の座標、作業領域との比較）に使う。
//     ズームで大きさが変わっても、右下の座標は変わらない。

// zoomSteps は選べる拡大率（%）。キー操作・ホイールは、この並びを 1 段ずつ動く。
var zoomSteps = []int{50, 67, 75, 80, 90, 100, 110, 125, 150, 175, 200}

const defaultZoomPercent = 100

// widgetZoom は今の拡大率（%）。HTTP ハンドラの goroutine と UI スレッドから読むので atomic。
var widgetZoom atomic.Int32

func init() { widgetZoom.Store(defaultZoomPercent) }

// isZoomStep は percent が選べる拡大率かを返す。
func isZoomStep(percent int) bool {
	for _, s := range zoomSteps {
		if s == percent {
			return true
		}
	}
	return false
}

// normalizeZoom は保存された値を使える拡大率へ矯正する。一覧に無い値（手編集や将来の値）は 100%。
func normalizeZoom(percent int) int {
	if isZoomStep(percent) {
		return percent
	}
	return defaultZoomPercent
}

// nextZoomStep は percent から 1 段（dir>0 は拡大、dir<0 は縮小）動かした拡大率を返す。
// 両端では動かない。一覧に無い値からは、その値に最も近い段を起点にする。
func nextZoomStep(percent, dir int) int {
	idx := 0
	for i, s := range zoomSteps {
		if abs(s-percent) < abs(zoomSteps[idx]-percent) {
			idx = i
		}
	}
	switch {
	case dir > 0 && idx < len(zoomSteps)-1:
		idx++
	case dir < 0 && idx > 0:
		idx--
	}
	return zoomSteps[idx]
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// zoomFactor は WebView2 に渡すズーム率（1.0 = 100%）。
func zoomFactor() float64 { return float64(widgetZoom.Load()) / 100 }

// layoutScale はレイアウト px から物理 px への倍率（DPI 倍率 × ズーム）。
func layoutScale() float64 {
	scale := getWindowDpiScale()
	if scale <= 0 {
		scale = 1.0
	}
	return scale * zoomFactor()
}

// setZoom は拡大率を保存して、メインウィンドウに反映する。どの goroutine からでも呼べる。
func setZoom(percent int) {
	percent = normalizeZoom(percent)
	mutateConfig(func(c *Config) { c.Zoom = percent })
	dispatchUI(func() { applyZoom(percent) })
}

// applyZoom は拡大率を WebView とウィンドウに反映する。UI スレッドで呼ぶこと。
// 中身のレイアウト（幅・高さ。レイアウト px）は変えず、右下を固定したままウィンドウを拡縮する。
// 高さはフロントが測り直して合わせる。
func applyZoom(percent int) {
	if int32(percent) == widgetZoom.Load() {
		return
	}
	// 今のズームでのレイアウト幅・高さを先に読む。コンパクト中の幅は、読み戻しの丸めで
	// 切り替えのたびにずれていかないよう、定数を使う。
	curW, curH := getWindowSize()
	if layoutIsCompact() {
		curW = compactWindowWidth
	}
	widgetZoom.Store(int32(percent))
	if p := mainWebView.Load(); p != nil {
		_ = (*p).SetZoomFactor(zoomFactor())
	}
	resizeWindowAnchored(curW, curH, false, true)
	persistCurrentWindowGeometry()
}
