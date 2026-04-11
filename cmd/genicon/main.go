// cmd/genicon は system-widget.exe 用のアイコン (icon.ico) を生成する。
// デザイン: ウィジェットのテーマカラーに合わせた暗い背景に 3 本のバーチャート。
//
// 使い方:  go run ./cmd/genicon
// 出力:    icon.ico (16x16, 32x32, 48x48, 256x256)
package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"log"
	"math"
	"os"
)

// ── ICO ファイルフォーマット ──────────────────────────

type iconDir struct {
	Reserved uint16
	Type     uint16
	Count    uint16
}

type iconDirEntry struct {
	Width       byte
	Height      byte
	ColorCount  byte
	Reserved    byte
	Planes      uint16
	BitCount    uint16
	BytesInRes  uint32
	ImageOffset uint32
}

// ── メイン ─────────────────────────────────────────

func main() {
	sizes := []int{16, 32, 48, 256}

	// 各サイズの PNG データを生成
	pngDatas := make([][]byte, len(sizes))
	for i, sz := range sizes {
		img := drawIcon(sz)
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			log.Fatalf("PNG encode failed for size %d: %v", sz, err)
		}
		pngDatas[i] = buf.Bytes()
	}

	// ICO ファイルを組み立て
	var ico bytes.Buffer

	// ICONDIR ヘッダ
	binary.Write(&ico, binary.LittleEndian, iconDir{
		Reserved: 0,
		Type:     1,
		Count:    uint16(len(sizes)),
	})

	// オフセット計算
	dataOffset := 6 + 16*len(sizes) // header + entries
	for i, sz := range sizes {
		w, h := byte(sz), byte(sz)
		if sz >= 256 {
			w, h = 0, 0 // ICO の慣例: 256 は 0 で表す
		}
		binary.Write(&ico, binary.LittleEndian, iconDirEntry{
			Width:       w,
			Height:      h,
			Planes:      1,
			BitCount:    32,
			BytesInRes:  uint32(len(pngDatas[i])),
			ImageOffset: uint32(dataOffset),
		})
		dataOffset += len(pngDatas[i])
	}

	// PNG データ
	for _, d := range pngDatas {
		ico.Write(d)
	}

	if err := os.WriteFile("icon.ico", ico.Bytes(), 0644); err != nil {
		log.Fatalf("write icon.ico: %v", err)
	}
	log.Println("icon.ico を生成しました")
}

// ── 描画 ───────────────────────────────────────────

// ウィジェットのテーマカラー
var (
	bgOuter = color.RGBA{30, 30, 52, 255}       // 外周のわずかに暗い色
	bgInner = color.RGBA{60, 60, 95, 255}       // #3c3c5f — ウィジェット背景
	border  = color.RGBA{80, 80, 130, 255}       // 微妙なボーダー
	barCyan = color.RGBA{123, 255, 224, 255}     // #7bffe0 — CPU
	barPurp = color.RGBA{176, 124, 255, 255}     // #b07cff — コア
	barPink = color.RGBA{255, 163, 255, 255}     // #ffa3ff — メモリ
)

func drawIcon(size int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	s := float64(size)

	// ── 1. 角丸背景 ──
	radius := s / 6.0
	if size <= 16 {
		radius = 2.5
	}
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			d := roundedRectSDF(float64(x)+0.5, float64(y)+0.5, s, s, radius)
			if d < -1.5 {
				img.Set(x, y, bgInner)
			} else if d < 0.5 {
				// ボーダー領域 — SDF に基づいてブレンド
				t := smoothstep(-1.5, 0.5, d)
				img.Set(x, y, lerpColor(bgInner, border, t))
			} else if d < 1.5 {
				// 外側エッジ — アンチエイリアス
				t := smoothstep(0.5, 1.5, d)
				img.Set(x, y, lerpColor(border, color.RGBA{0, 0, 0, 0}, t))
			}
		}
	}

	// ── 2. バーチャート ──
	barColors := []color.RGBA{barCyan, barPurp, barPink}
	barHeights := []float64{0.65, 0.42, 0.82}

	margin := s * 0.22
	innerW := s - 2*margin
	innerH := s - 2*margin
	numBars := float64(len(barColors))
	gapRatio := 0.3 // バー幅に対するギャップの割合
	barW := innerW / (numBars + (numBars-1)*gapRatio)
	gap := barW * gapRatio
	baseY := s - margin
	barR := barW / 4.0 // バー上部の角丸

	if size <= 16 {
		barR = 1.0
	}

	for i, col := range barColors {
		bx := margin + float64(i)*(barW+gap)
		bh := innerH * barHeights[i]
		by := baseY - bh

		// バーの各ピクセルを描画（上部のみ角丸）
		for py := 0; py < size; py++ {
			for px := 0; px < size; px++ {
				fx := float64(px) + 0.5
				fy := float64(py) + 0.5

				// バーの矩形内かチェック
				if fx < bx || fx > bx+barW || fy < by || fy > baseY {
					continue
				}

				// 上部の角丸チェック (SDF)
				d := topRoundedRectSDF(fx, fy, bx, by, barW, bh, barR)
				if d < -0.7 {
					// バー内部
					img.Set(px, py, col)
				} else if d < 0.7 {
					// エッジ — アンチエイリアス
					t := smoothstep(-0.7, 0.7, d)
					bg := img.RGBAAt(px, py)
					img.Set(px, py, lerpColor(col, bg, t))
				}
			}
		}

		// バーにグラデーション（上部を少し明るく）
		for py := int(by); py < int(baseY); py++ {
			for px := int(bx); px < int(bx+barW)+1; px++ {
				if px < 0 || py < 0 || px >= size || py >= size {
					continue
				}
				cur := img.RGBAAt(px, py)
				if cur.A == 0 {
					continue
				}
				// 上部を10%明るく
				progress := (float64(py) - by) / bh
				brightness := 1.0 + 0.12*(1.0-progress)
				cur.R = clampByte(float64(cur.R) * brightness)
				cur.G = clampByte(float64(cur.G) * brightness)
				cur.B = clampByte(float64(cur.B) * brightness)
				img.SetRGBA(px, py, cur)
			}
		}
	}

	return img
}

// ── SDF (Signed Distance Field) ─────────────────────

// 角丸矩形の SDF: 負=内側, 正=外側
func roundedRectSDF(x, y, w, h, r float64) float64 {
	// 中心を原点とする
	cx, cy := w/2, h/2
	dx := math.Abs(x-cx) - (cx - r)
	dy := math.Abs(y-cy) - (cy - r)
	outside := math.Sqrt(math.Max(dx, 0)*math.Max(dx, 0) + math.Max(dy, 0)*math.Max(dy, 0))
	inside := math.Min(math.Max(dx, dy), 0)
	return outside + inside - r
}

// 上部のみ角丸のバー用 SDF
func topRoundedRectSDF(px, py, rx, ry, rw, rh, r float64) float64 {
	// バーの矩形: (rx, ry) から (rx+rw, ry+rh)
	cx := rx + rw/2

	// x 方向の距離
	dx := math.Abs(px-cx) - rw/2

	// y 方向: 上部のみ角丸
	if py < ry+r {
		// 上部角丸領域
		dy := (ry + r) - py
		cornerDx := math.Abs(px-cx) - (rw/2 - r)
		if cornerDx > 0 && dy > 0 {
			return math.Sqrt(cornerDx*cornerDx+dy*dy) - r
		}
		return math.Max(dx, ry-py)
	}

	// 下部（角丸なし）
	dy := py - (ry + rh)
	return math.Max(dx, dy)
}

// ── ユーティリティ ──────────────────────────────────

func smoothstep(edge0, edge1, x float64) float64 {
	t := (x - edge0) / (edge1 - edge0)
	if t < 0 {
		t = 0
	}
	if t > 1 {
		t = 1
	}
	return t * t * (3 - 2*t)
}

func lerpColor(a, b color.RGBA, t float64) color.RGBA {
	return color.RGBA{
		R: clampByte(float64(a.R)*(1-t) + float64(b.R)*t),
		G: clampByte(float64(a.G)*(1-t) + float64(b.G)*t),
		B: clampByte(float64(a.B)*(1-t) + float64(b.B)*t),
		A: clampByte(float64(a.A)*(1-t) + float64(b.A)*t),
	}
}

func clampByte(v float64) byte {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return byte(math.Round(v))
}
