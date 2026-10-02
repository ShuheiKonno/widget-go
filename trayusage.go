package main

import (
	"image"
	"syscall"
	"unicode/utf16"
	"unsafe"

	"widget-go/aiusage"
)

// --- トレイの AI 使用量表示（trayDisplay = "usage"）---

// effectiveTrayDisplay は、保存されている設定値と、AI 使用量の ON/OFF から、実際に出す
// 表示を決める。"usage" を選んだまま AI 使用量を OFF にしたときは、保存値は変えず
// 実際の表示だけアイコンのみに落とす（再び ON にすると "usage" が戻る。D3）。
func effectiveTrayDisplay(mode int, aiEnabled bool) int {
	if mode == TRAY_DISPLAY_USAGE && !aiEnabled {
		return TRAY_DISPLAY_ICON
	}
	return mode
}

// tipMaxUnits は NOTIFYICONDATAW.szTip に入る最大の長さ（NUL を除く UTF-16 単位）。
const tipMaxUnits = 127

func utf16Len(s string) int { return len(utf16.Encode([]rune(s))) }

// buildUsageTooltip は、AI 使用量のツールチップを先頭に置き、収まる範囲で widget-go 側の
// 項目（"CPU: 12% | Mem: 45% | ..."）を後ろに足す。収まらない項目は、途中で切らず、
// 末尾の項目から落とす（szTip は 127 文字までなので、そのまま繋ぐと中途半端に切れる）。
func buildUsageTooltip(aiTip, sysTip string) string {
	out := aiTip
	sep := "\n"
	for _, part := range splitTipParts(sysTip) {
		cand := out + sep + part
		if utf16Len(cand) > tipMaxUnits {
			break
		}
		out = cand
		sep = " | "
	}
	return out
}

func splitTipParts(s string) []string {
	var parts []string
	start := 0
	for i := 0; i+3 <= len(s); i++ {
		if s[i:i+3] == " | " {
			parts = append(parts, s[start:i])
			start = i + 3
			i += 2
		}
	}
	if start < len(s) {
		parts = append(parts, s[start:])
	}
	return parts
}

// trayKick は、トレイの更新を次の周期を待たずに促す合図。AI 使用量のスナップショットの
// 更新・設定の変更・表示の切替で送る。バッファ 1 のノンブロッキング送信でよい。
var trayKick = make(chan struct{}, 1)

func kickTray() {
	select {
	case trayKick <- struct{}{}:
	default:
	}
}

// buildUsageIcon は AI 使用量のアイコン（角丸四角 + 5h の数字 + 7d のペース色）を HICON にする。
// 返値が 0 でない場合、呼び出し側は不要になったら procDestroyIcon で解放する。
func buildUsageIcon(info aiusage.TrayInfo) uintptr {
	return imageToHICON(info.Image())
}

// imageToHICON は RGBA 画像を、32bpp BGRA + 1bpp の AND マスクの HICON にする。
// ClaudeMonitor の同名の関数を、widget-go 側の宣言に合わせて移植したもの。
func imageToHICON(img *image.RGBA) uintptr {
	size := img.Bounds().Dx()
	if size <= 0 || img.Bounds().Dy() != size {
		return 0
	}
	bgra := make([]byte, len(img.Pix))
	// 1bpp のマスクは、1 行を 16 ビット（2 バイト）境界に揃える。全て 0 ＝ すべて表示。
	mask := make([]byte, ((size+15)/16)*2*size)
	for i := 0; i < len(img.Pix); i += 4 {
		bgra[i+0] = img.Pix[i+2]
		bgra[i+1] = img.Pix[i+1]
		bgra[i+2] = img.Pix[i+0]
		bgra[i+3] = img.Pix[i+3]
	}

	hbmColor, _, _ := procCreateBitmap.Call(uintptr(size), uintptr(size), 1, 32, uintptr(unsafe.Pointer(&bgra[0])))
	if hbmColor == 0 {
		return 0
	}
	defer procDeleteObject.Call(hbmColor)
	hbmMask, _, _ := procCreateBitmap.Call(uintptr(size), uintptr(size), 1, 1, uintptr(unsafe.Pointer(&mask[0])))
	if hbmMask == 0 {
		return 0
	}
	defer procDeleteObject.Call(hbmMask)

	info := ICONINFO{FIcon: 1, HbmMask: hbmMask, HbmColor: hbmColor}
	hIcon, _, _ := procCreateIconIndirect.Call(uintptr(unsafe.Pointer(&info)))
	return hIcon
}

// appendUsageMenu は、AI 使用量が ON のときのトレイの右クリックメニューの項目を足す。
func appendUsageMenu(menu uintptr) {
	if !snapshotConfig().AI.Enabled {
		return
	}
	add := func(id uintptr, text string) {
		p, _ := syscall.UTF16PtrFromString(text)
		procAppendMenuW.Call(menu, MF_STRING, id, uintptr(unsafe.Pointer(p)))
	}
	add(ID_TRAY_AI_REFRESH, "AI 使用量を更新")
	for _, prov := range aiusage.LoginNeeded() {
		if prov == "codex" {
			add(ID_TRAY_AI_LOGIN_CODEX, aiusage.ProviderName(prov)+" にログイン…")
		} else {
			add(ID_TRAY_AI_LOGIN_CLAUDE, aiusage.ProviderName(prov)+" にログイン…")
		}
	}
	add(ID_TRAY_AI_TEST, "AI 通知のテスト")
	procAppendMenuW.Call(menu, MF_SEPARATOR, 0, 0)
}
