package aiusage

import (
	"image"
	"image/color"
	"math"
	"strconv"
	"time"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

// トレイアイコンの描画とペース判定。ClaudeMonitor（cc10dda）の tray.go から、Win32 に
// 依存しない純粋関数だけを、内容を変えずに移植している（HICON への変換は本体側）。

// trayIconSize はアイコン画像の一辺（px）。
const trayIconSize = 16

// trayBand はトレイアイコンの配色バンド（7 日ウィンドウのペース判定結果）。
type trayBand int

const (
	trayBandGreen trayBand = iota
	trayBandAmber
	trayBandRed
)

// trayPaceThresholds は 7 日ウィンドウの経過割合に応じてスケールした配色閾値（黄, 赤）を返す。
// 使用率が「経過時間ぶんの許容ペース」を超えているかで警告するため、60%/80% を経過割合で線形に縮める。
// splitDays はペース基準日数（7/5）。0 の場合はスケーリングせず固定 60/80 を返す（「分割なし」設定）。
func trayPaceThresholds(resetsAt *time.Time, now time.Time, splitDays int) (yellow, red float64) {
	const windowTotal = 7 * 24 * time.Hour // 実際のリセットウィンドウ長（常に7日固定）
	const minElapsed = 12 * time.Hour      // リセット直後は閾値がほぼ0になり色がちらつくため床を設ける
	if resetsAt == nil || splitDays <= 0 {
		return 60, 80 // リセット時刻不明 or 「分割なし」設定時は固定閾値
	}
	paceTotal := time.Duration(splitDays) * 24 * time.Hour
	elapsed := now.Sub(resetsAt.Add(-windowTotal))
	elapsed = min(max(elapsed, minElapsed), paceTotal) // ペース基準日数超・時計ずれによる負値も吸収
	frac := float64(elapsed) / float64(paceTotal)
	return 60 * frac, 80 * frac
}

// trayBandFor は 7 日使用率とペース閾値から配色バンドを決める。
func trayBandFor(utilization, yellow, red float64) trayBand {
	switch {
	case utilization > red:
		return trayBandRed
	case utilization > yellow:
		return trayBandAmber
	default:
		return trayBandGreen
	}
}

// drawTrayIconImage は 16x16 のトレイアイコン画像を RGBA で組み立てる純関数。
// 角丸四角 + 中央数字の構成で、7 日ウィンドウのペース判定を背景色（緑/黄/赤）で、
// 5 時間使用率を中央の数字で表現する。円形時代より数字の左右余白が広く取れるため
// 5/6 など紛らわしい数字の判読性を優先した意匠。数字は basicfont 7x13。
func drawTrayIconImage(pct5h int, band trayBand) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, trayIconSize, trayIconSize))

	// ペース判定バンドで配色決定。Claude 系の暖色系で統一。
	var diskColor, rimColor, fgColor color.RGBA
	switch band {
	case trayBandGreen:
		diskColor = color.RGBA{R: 123, G: 176, B: 123, A: 255} // sage green #7BB07B
		rimColor = color.RGBA{R: 77, G: 128, B: 77, A: 255}
		fgColor = color.RGBA{R: 20, G: 28, B: 20, A: 255}
	case trayBandAmber:
		diskColor = color.RGBA{R: 232, G: 168, B: 83, A: 255} // amber #E8A853
		rimColor = color.RGBA{R: 172, G: 116, B: 45, A: 255}
		fgColor = color.RGBA{R: 40, G: 26, B: 10, A: 255}
	default:
		diskColor = color.RGBA{R: 208, G: 107, B: 91, A: 255} // terracotta #D06B5B
		rimColor = color.RGBA{R: 150, G: 64, B: 52, A: 255}
		fgColor = color.RGBA{R: 248, G: 238, B: 232, A: 255}
	}

	drawTraySquareBase(img, diskColor, rimColor)

	val := pct5h
	if val < 0 {
		val = 0
	}
	if val > 100 {
		val = 100
	}
	if val >= 100 {
		// 7x13 では 3 桁が 16px に収まらないため、100% 専用に 3x5 ビットマップで "100" を描画する。
		drawTrayCompact100(img, fgColor)
	} else {
		text := strconv.Itoa(val)
		drawer := &font.Drawer{
			Dst:  img,
			Src:  image.NewUniform(fgColor),
			Face: basicfont.Face7x13,
		}
		w := drawer.MeasureString(text).Round()
		x := (trayIconSize - w) / 2
		if x < 0 {
			x = 0
		}
		drawer.Dot = fixed.P(x, 12)
		drawer.DrawString(text)
	}
	return img
}

// drawTrayCompact100 は 16px トレイで "100" を読める形に詰めるための 3x5 専用描画。
// 各桁 3x5 + 1px gap で計 11px、トレイ中央に配置する。
func drawTrayCompact100(img *image.RGBA, c color.RGBA) {
	const digitW, digitH, gap = 3, 5, 1
	one := "" +
		".X." +
		"XX." +
		".X." +
		".X." +
		"XXX"
	zero := "" +
		"XXX" +
		"X.X" +
		"X.X" +
		"X.X" +
		"XXX"
	glyphs := []string{one, zero, zero}
	totalW := digitW*len(glyphs) + gap*(len(glyphs)-1)
	startX := (trayIconSize - totalW) / 2
	startY := (trayIconSize - digitH) / 2
	for gi, g := range glyphs {
		ox := startX + gi*(digitW+gap)
		for row := 0; row < digitH; row++ {
			for col := 0; col < digitW; col++ {
				if g[row*digitW+col] == 'X' {
					blendTray(img, ox+col, startY+row, c, 1)
				}
			}
		}
	}
}

// drawTraySquareBase は角丸四角の下地（外枠 + 内側フィル）を描く。
// 0.5px マージンで 16px をいっぱいに使い、リム約 1.2px は円形時代の意匠を踏襲。
func drawTraySquareBase(img *image.RGBA, diskColor, rimColor color.RGBA) {
	const margin = 0.5
	const rim = 1.2
	size := float64(trayIconSize)
	fillTrayRoundedRect(img, margin, margin, size-margin, size-margin, 3.5, rimColor)
	fillTrayRoundedRect(img, margin+rim, margin+rim, size-margin-rim, size-margin-rim, 3.5-rim, diskColor)
}

func drawTrayIconErrorImage() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, trayIconSize, trayIconSize))
	disk := color.RGBA{R: 110, G: 114, B: 126, A: 255}
	rim := color.RGBA{R: 70, G: 72, B: 82, A: 255}
	fg := color.RGBA{R: 250, G: 250, B: 255, A: 255}
	drawTraySquareBase(img, disk, rim)

	drawer := &font.Drawer{
		Dst:  img,
		Src:  image.NewUniform(fg),
		Face: basicfont.Face7x13,
	}
	text := "?"
	w := drawer.MeasureString(text).Round()
	x := (trayIconSize - w) / 2
	if x < 0 {
		x = 0
	}
	drawer.Dot = fixed.P(x, 12)
	drawer.DrawString(text)
	return img
}

// fillTrayRoundedRect: アンチエイリアス付きで角丸四角を塗る（アルファは既存ピクセルにオーバー合成）。
// [x0,x1]×[y0,y1] はピクセル座標での外形。角丸の符号付き距離場からピクセル中心の被覆率を近似する。
func fillTrayRoundedRect(img *image.RGBA, x0, y0, x1, y1, radius float64, c color.RGBA) {
	if x1 <= x0 || y1 <= y0 {
		return
	}
	radius = min(radius, (x1-x0)/2, (y1-y0)/2)
	radius = max(radius, 0)
	cx, cy := (x0+x1)/2, (y0+y1)/2
	halfW, halfH := (x1-x0)/2-radius, (y1-y0)/2-radius
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			dx := math.Abs(float64(x)+0.5-cx) - halfW
			dy := math.Abs(float64(y)+0.5-cy) - halfH
			qx, qy := max(dx, 0), max(dy, 0)
			d := math.Sqrt(qx*qx+qy*qy) + min(max(dx, dy), 0) - radius
			blendTray(img, x, y, c, 0.5-d)
		}
	}
}

func blendTray(img *image.RGBA, x, y int, c color.RGBA, alpha float64) {
	if alpha <= 0 {
		return
	}
	if alpha > 1 {
		alpha = 1
	}
	i := img.PixOffset(x, y)
	sa := float64(c.A) / 255 * alpha
	sr := float64(c.R) / 255 * sa
	sg := float64(c.G) / 255 * sa
	sb := float64(c.B) / 255 * sa
	dr := float64(img.Pix[i+0]) / 255
	dg := float64(img.Pix[i+1]) / 255
	db := float64(img.Pix[i+2]) / 255
	da := float64(img.Pix[i+3]) / 255
	outA := sa + da*(1-sa)
	var outR, outG, outB float64
	if outA > 0 {
		outR = (sr + dr*da*(1-sa)) / outA
		outG = (sg + dg*da*(1-sa)) / outA
		outB = (sb + db*da*(1-sa)) / outA
	}
	img.Pix[i+0] = clamp255(outR * 255)
	img.Pix[i+1] = clamp255(outG * 255)
	img.Pix[i+2] = clamp255(outB * 255)
	img.Pix[i+3] = clamp255(outA * 255)
}

func clamp255(v float64) uint8 {
	if v <= 0 {
		return 0
	}
	if v >= 255 {
		return 255
	}
	return uint8(math.Round(v))
}
