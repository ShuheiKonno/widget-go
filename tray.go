package main

import (
	"fmt"
	"math"
	"os"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"

	"widget-go/aiusage"
	"widget-go/sysinfo"
)

// --- システムトレイ機能 ---

// loadTrayIcon は winres/icon.ico から埋め込まれたアイコンを
// トレイに適したサイズ (SM_CXSMICON) でロードする。
// 返されるハンドルは LR_SHARED 付きなのでシステム管理 — 呼び出し側は破棄しない。
func loadTrayIcon() uintptr {
	hModule, _, _ := procGetModuleHandle.Call(0)
	// winres.json の RT_GROUP_ICON キー "APP" が Windows リソース名になる
	name, _ := syscall.UTF16PtrFromString("APP")

	cx, _, _ := procGetSystemMetrics.Call(SM_CXSMICON)
	cy, _, _ := procGetSystemMetrics.Call(SM_CYSMICON)
	if cx == 0 {
		cx = 16
	}
	if cy == 0 {
		cy = 16
	}

	icon, _, _ := procLoadImageW.Call(hModule, uintptr(unsafe.Pointer(name)), IMAGE_ICON, cx, cy, LR_SHARED)
	if icon != 0 {
		return icon
	}
	// フォールバック 1: リソース名でデフォルトサイズのグループアイコンを取得
	icon, _, _ = procLoadIcon.Call(hModule, uintptr(unsafe.Pointer(name)))
	if icon != 0 {
		return icon
	}
	// フォールバック 2: Windows 既定のアプリアイコン
	icon, _, _ = procLoadIcon.Call(0, uintptr(32512)) // IDI_APPLICATION
	return icon
}

// digitFont5x7 は 0-9 のドットフォント (幅5, 高さ7)。
// 各要素は 1 行 5 ビット (bit4=左端, bit0=右端)。
var digitFont5x7 = [10][7]uint8{
	{0x0E, 0x11, 0x11, 0x11, 0x11, 0x11, 0x0E},
	{0x04, 0x0C, 0x04, 0x04, 0x04, 0x04, 0x0E},
	{0x0E, 0x11, 0x01, 0x02, 0x04, 0x08, 0x1F},
	{0x0E, 0x11, 0x01, 0x06, 0x01, 0x11, 0x0E},
	{0x02, 0x06, 0x0A, 0x12, 0x1F, 0x02, 0x02},
	{0x1F, 0x10, 0x1E, 0x01, 0x01, 0x11, 0x0E},
	{0x06, 0x08, 0x10, 0x1E, 0x11, 0x11, 0x0E},
	{0x1F, 0x01, 0x02, 0x04, 0x08, 0x08, 0x08},
	{0x0E, 0x11, 0x11, 0x0E, 0x11, 0x11, 0x0E},
	{0x0E, 0x11, 0x11, 0x0F, 0x01, 0x02, 0x0C},
}

// drawDigits は pixels (32bpp ARGB, top-down) に数字を中央描画する。
// 最大 2 桁。scale を自動決定し、白 α=255 で描く。
func drawDigits(pixels []uint32, w, h int, value int) {
	if value < 0 {
		value = 0
	}
	if value > 99 {
		value = 99
	}
	var digits []int
	if value < 10 {
		digits = []int{value}
	} else {
		digits = []int{value / 10, value % 10}
	}

	scale := 1
	if h >= 22 {
		scale = 2
	}
	if h >= 34 {
		scale = 3
	}

	charW := 5 * scale
	gap := scale
	totalW := charW*len(digits) + gap*(len(digits)-1)
	totalH := 7 * scale
	xStart := (w - totalW) / 2
	yStart := (h - totalH) / 2

	const fg = uint32(0xFFFFFFFF) // 白 α=255 (プリマルチプライド済み)

	for di, d := range digits {
		baseX := xStart + di*(charW+gap)
		for row := 0; row < 7; row++ {
			rowBits := digitFont5x7[d][row]
			for col := 0; col < 5; col++ {
				if (rowBits>>(4-col))&1 == 0 {
					continue
				}
				for sy := 0; sy < scale; sy++ {
					for sx := 0; sx < scale; sx++ {
						x := baseX + col*scale + sx
						y := yStart + row*scale + sy
						if x >= 0 && x < w && y >= 0 && y < h {
							pixels[y*w+x] = fg
						}
					}
				}
			}
		}
	}
}

// premulARGB は α とストレート RGB からプリマルチプライド ARGB (32bpp) を組み立てる。
// CreateDIBSection で作った 32bpp ビットマップを CreateIconIndirect に渡す場合、
// カラー値は α で乗算済みでなければ縁にハローが出る。
func premulARGB(a, r, g, b uint8) uint32 {
	pr := uint32(r) * uint32(a) / 255
	pg := uint32(g) * uint32(a) / 255
	pb := uint32(b) * uint32(a) / 255
	return uint32(a)<<24 | pr<<16 | pg<<8 | pb
}

// トレイアイコン背景: 半透明ダーク (α=200, RGB=(20,22,40))。
var iconBgColor = premulARGB(200, 20, 22, 40)

// アクセント色は ui/index.html のクロックのバー（.clock-fill 系）に対応させる。CSS 側は 2 色の
// グラデーションだが 16px のアイコンでは表現できないため、それぞれの中間色を代表色として使う。
//
// 色は電源モードではなく、ベースクロックとの差で決める（バーと同じ規則。clockPercentDiff を参照）:
//
//	ベース -11% 以下      : #7be0a0 → #3cc47c の中点 = #5bd28e  (.clock-fill.clk-low)
//	ベース -10% 〜 +20%   : #5ad0ff → #8a9cff の中点 = #72b6ff  (.clock-fill)
//	ベース +21% 以上      : #ffc46a → #ff7a3c の中点 = #ff9f53  (.clock-fill.clk-high)
var (
	clockAccentLow    = premulARGB(255, 0x5b, 0xd2, 0x8e) // .clock-fill.clk-low
	clockAccentNormal = premulARGB(255, 0x72, 0xb6, 0xff) // .clock-fill
	clockAccentHigh   = premulARGB(255, 0xff, 0x9f, 0x53) // .clock-fill.clk-high
)

// クロックの色の境界（ベースクロックとの差を整数 % に丸めた値）。
// ui/index.html の CLOCK_LOW_MAX_PCT / CLOCK_HIGH_MIN_PCT と必ず同じ値にすること。
// ここがずれると、トレイアイコンとクロックのバーの色が境界で食い違う。
const (
	clockLowMaxPct  = -11
	clockHighMinPct = 21
)

// clockPercentDiff はベースクロックとの差（%）を整数に丸めた値。
// ui/index.html の Math.round((cur / base - 1) * 100) と同じく、.5 は +∞ 方向へ丸める（math.Round は 0 から遠い方へ丸めるので使わない）。
func clockPercentDiff(f *sysinfo.CPUFreqInfo) int {
	return int(math.Floor((f.CurrentMHz/f.BaseMHz-1)*100 + 0.5))
}

// trayAccentColor は現在のクロック情報からトレイアイコンの帯の色を決める。
// 取得できていない場合 (PDH 初期化失敗など) は ok=false を返し、
// 呼び出し側は従来どおりアクセント無しで描画する。
func trayAccentColor(f *sysinfo.CPUFreqInfo) (uint32, bool) {
	if f == nil || f.BaseMHz <= 0 {
		return 0, false
	}
	switch pct := clockPercentDiff(f); {
	case pct <= clockLowMaxPct:
		return clockAccentLow, true
	case pct >= clockHighMinPct:
		return clockAccentHigh, true
	}
	return clockAccentNormal, true
}

// accentStripHeight はアイコン下端に敷くアクセント帯の高さ (px)。
const accentStripHeight = 2

// buildCpuIcon は CPU 使用率 (0-100) を示すトレイアイコンを GDI で生成する。
// 背景は半透明の角丸ダーク矩形、文字は白。hasAccent が真なら下端に accent 色の帯を敷く
// (クロックのバーと同じ配色。trayAccentColor を参照)。背景を染めずに帯にするのは、16px で白文字の
// コントラストを落とさないため。
// 返値が 0 でない場合、呼び出し側は不要になったら procDestroyIcon.Call(hIcon) で解放する必要がある。
func buildCpuIcon(percent int, accent uint32, hasAccent bool) uintptr {
	cx, _, _ := procGetSystemMetrics.Call(SM_CXSMICON)
	cy, _, _ := procGetSystemMetrics.Call(SM_CYSMICON)
	if cx == 0 {
		cx = 16
	}
	if cy == 0 {
		cy = 16
	}
	w := int32(cx)
	h := int32(cy)

	hdc, _, _ := procCreateCompatibleDC.Call(0)
	if hdc == 0 {
		return 0
	}
	defer procDeleteDC.Call(hdc)

	var bmi BITMAPINFOHEADER
	bmi.BiSize = uint32(unsafe.Sizeof(bmi))
	bmi.BiWidth = w
	bmi.BiHeight = -h // top-down
	bmi.BiPlanes = 1
	bmi.BiBitCount = 32
	bmi.BiCompression = BI_RGB

	var bitsPtr unsafe.Pointer
	hBmColor, _, _ := procCreateDIBSection.Call(hdc, uintptr(unsafe.Pointer(&bmi)), DIB_RGB_COLORS, uintptr(unsafe.Pointer(&bitsPtr)), 0, 0)
	if hBmColor == 0 {
		return 0
	}
	defer procDeleteObject.Call(hBmColor)

	pixels := unsafe.Slice((*uint32)(bitsPtr), int(w)*int(h))

	// 背景: 半透明ダーク (プリマルチプライド ARGB)
	for i := range pixels {
		pixels[i] = iconBgColor
	}

	iw := int(w)
	ih := int(h)

	// アクセント帯を下端に敷く。角落としより先に塗ること
	// (帯が下段の両端ピクセルを上書きするため、順序を逆にすると丸角が消える)。
	if hasAccent && ih > accentStripHeight {
		for y := ih - accentStripHeight; y < ih; y++ {
			for x := 0; x < iw; x++ {
				pixels[y*iw+x] = accent
			}
		}
	}

	// 角落とし (1px)
	pixels[0] = 0
	pixels[iw-1] = 0
	pixels[(ih-1)*iw] = 0
	pixels[ih*iw-1] = 0

	drawDigits(pixels, iw, ih, percent)

	hBmMask, _, _ := procCreateBitmap.Call(uintptr(w), uintptr(h), 1, 1, 0)
	if hBmMask == 0 {
		return 0
	}
	defer procDeleteObject.Call(hBmMask)

	ii := ICONINFO{FIcon: 1, HbmMask: hBmMask, HbmColor: hBmColor}
	hIcon, _, _ := procCreateIconIndirect.Call(uintptr(unsafe.Pointer(&ii)))
	return hIcon
}

// snapEthernetLinkSpeedMbps は Mbps 値を Ethernet 標準速度 (10/100/1000/2500/5000/10000) に
// ±2% 以内ならスナップして返す。ドライバが 995Mbps のように丸めて報告するケースへの対策。
func snapEthernetLinkSpeedMbps(mbps float64) float64 {
	standards := []float64{10, 100, 1000, 2500, 5000, 10000}
	for _, s := range standards {
		if math.Abs(mbps-s)/s <= 0.02 {
			return s
		}
	}
	return mbps
}

// formatLinkSpeedShort は bit/s を短縮文字列化する。例: 1000000000 -> "1G", 2500000000 -> "2.5G"。
func formatLinkSpeedShort(bps uint64) string {
	if bps == 0 {
		return ""
	}
	mbps := snapEthernetLinkSpeedMbps(float64(bps) / 1e6)
	if mbps >= 1000 {
		gbps := mbps / 1000
		if gbps == math.Trunc(gbps) {
			return fmt.Sprintf("%.0fG", gbps)
		}
		return fmt.Sprintf("%.1fG", gbps)
	}
	return fmt.Sprintf("%.0fM", math.Round(mbps))
}

func formatTrayTooltip() string {
	cacheMu.RLock()
	defer cacheMu.RUnlock()
	return buildTrayTooltip(cachedCPU, cachedMem, cachedWiFi, cachedEthernet, cachedGPU, cachedNPU, showGPU.Load())
}

// buildTrayTooltip はツールチップ文字列を組み立てる純粋関数。
// GPU/NPU は showGPU が true かつデバイスがある場合だけ追記し、値が取れないときは「---」にする。
func buildTrayTooltip(cpu *sysinfo.CPUInfo, mem *sysinfo.MemoryInfo, wifi *sysinfo.WiFiInfo, eth *sysinfo.EthernetInfo, gpu *sysinfo.GPUInfo, npu *sysinfo.NPUInfo, showGPU bool) string {
	cpuStr := "---"
	memStr := "---"
	netStr := "未接続"

	if cpu != nil {
		cpuStr = fmt.Sprintf("%d%%", int(math.Round(cpu.Overall)))
	}
	if mem != nil {
		memStr = fmt.Sprintf("%d%%", int(math.Round(mem.UsedPercent)))
	}
	if wifi != nil && wifi.SSID != "" {
		if wifi.Channel > 0 {
			netStr = fmt.Sprintf("%s(%dch)", wifi.Band, wifi.Channel)
		} else {
			netStr = wifi.Band
		}
	} else if eth != nil && eth.Connected {
		if eth.LinkSpeedBps > 0 {
			netStr = "Ethernet " + formatLinkSpeedShort(eth.LinkSpeedBps)
		} else {
			netStr = "Ethernet"
		}
	}

	tip := fmt.Sprintf("CPU: %s | Mem: %s | %s", cpuStr, memStr, netStr)
	if showGPU {
		if gpu != nil {
			tip += " | GPU: " + formatTrayPercent(gpu.TotalPct)
		}
		if npu != nil {
			tip += " | NPU: " + formatTrayPercent(npu.UsagePct)
		}
	}
	return tip
}

// formatTrayPercent は使用率を 0〜100 にクランプした整数 % で返す。nil は「---」。
// GPU エンジンの生値は 100 を超えることがあるので、表示段階でクランプする。
func formatTrayPercent(p *float64) string {
	if p == nil || math.IsNaN(*p) {
		return "---"
	}
	v := math.Max(0, math.Min(100, *p))
	return fmt.Sprintf("%d%%", int(math.Round(v)))
}

func setTipText(nid *NOTIFYICONDATAW, text string) {
	nid.SzTip = tipUTF16(text)
}

// tipUTF16 は text を NOTIFYICONDATAW.szTip 用の NUL 終端 UTF-16 に変換する。
// 127 単位を超える分は切り詰め、サロゲートペアの途中では切らない。残りはゼロで埋める。
func tipUTF16(text string) [128]uint16 {
	var out [128]uint16
	copyUTF16(out[:], text)
	return out
}

// copyUTF16 は text を NUL 終端の UTF-16 として dst に書く。dst に収まらない分は切り詰め、
// サロゲートペアの途中では切らない。残りはゼロで埋める。
func copyUTF16(dst []uint16, text string) {
	clear(dst)
	u := utf16.Encode([]rune(text))
	for i, c := range u {
		if c == 0 { // 途中の NUL 以降は Win32 側から見えないので捨てる
			u = u[:i]
			break
		}
	}
	n := len(u)
	if n > len(dst)-1 {
		n = len(dst) - 1
		if n > 0 && utf16.IsSurrogate(rune(u[n-1])) && u[n-1] < 0xDC00 {
			n-- // 上位サロゲートだけ残さない
		}
	}
	if n > 0 {
		copy(dst[:n], u[:n])
	}
}

// showBalloon はトレイアイコン経由でバルーン（Windows 10/11 ではトースト）通知を出す。
// flag は aiusage.NIIF_INFO / NIIF_WARNING / NIIF_ERROR。トレイが未登録なら何もしない。
func showBalloon(title, message string, flag uint32) bool {
	trayMu.Lock()
	active, hwnd := trayActive, trayHwnd
	trayMu.Unlock()
	if !active || hwnd == 0 {
		return false
	}
	var nid NOTIFYICONDATAW
	nid.CbSize = uint32(unsafe.Sizeof(nid))
	nid.HWnd = hwnd
	nid.UID = 1
	nid.UFlags = NIF_INFO
	nid.DwInfoFlags = flag
	copyUTF16(nid.SzInfoTitle[:], title)
	copyUTF16(nid.SzInfo[:], message)
	r, _, _ := procShellNotifyIcon.Call(NIM_MODIFY, uintptr(unsafe.Pointer(&nid)))
	return r != 0
}

// vsFixedFileInfo は VerQueryValueW("\\") が返す VS_FIXEDFILEINFO の先頭部分。
// 後続フィールド (FileFlags 以降) は使わないので宣言していない。
type vsFixedFileInfo struct {
	Signature        uint32
	StrucVersion     uint32
	FileVersionMS    uint32
	FileVersionLS    uint32
	ProductVersionMS uint32
	ProductVersionLS uint32
}

// vsFixedFileInfoSignature は VS_FIXEDFILEINFO の dwSignature 固定値。
const vsFixedFileInfoSignature = 0xFEEF04BD

// formatFileVersion は VS_FIXEDFILEINFO の dwFileVersionMS/LS を "vX.Y.Z" 形式にする。
// winres.json のバージョンは 4 桁 (例: 1.8.0.0) だが、git タグや README は 3 桁表記なので
// 末尾のビルド番号 (LOWORD(LS)) は落として揃える。
func formatFileVersion(ms, ls uint32) string {
	return fmt.Sprintf("v%d.%d.%d", ms>>16, ms&0xFFFF, ls>>16)
}

// appVersion は実行ファイルに埋め込まれた RT_VERSION からバージョン文字列を読む。
// winres.json の bump だけでメニュー表示も追従させるため、Go 側に定数を持たない。
// 文字列テーブル (StringFileInfo) は winres.json で 0411 (日本語) にのみ定義されており
// 言語 ID 依存になるため、言語非依存な固定情報 (VS_FIXEDFILEINFO) から取得する。
// 取得できない場合は空文字を返し、呼び出し側はバージョン行を出さない。
var appVersion = sync.OnceValue(func() string {
	buf := make([]uint16, syscall.MAX_PATH)
	n, _, _ := procGetModuleFileName.Call(0, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if n == 0 || int(n) >= len(buf) {
		return ""
	}
	pathPtr := uintptr(unsafe.Pointer(&buf[0]))

	size, _, _ := procGetFileVersionInfoSizeW.Call(pathPtr, 0)
	if size == 0 {
		return ""
	}
	data := make([]byte, size)
	if r, _, _ := procGetFileVersionInfoW.Call(pathPtr, 0, size, uintptr(unsafe.Pointer(&data[0]))); r == 0 {
		return ""
	}

	subBlock, err := syscall.UTF16PtrFromString(`\`)
	if err != nil {
		return ""
	}
	var info unsafe.Pointer
	var infoLen uint32
	r, _, _ := procVerQueryValueW.Call(
		uintptr(unsafe.Pointer(&data[0])),
		uintptr(unsafe.Pointer(subBlock)),
		uintptr(unsafe.Pointer(&info)),
		uintptr(unsafe.Pointer(&infoLen)),
	)
	if r == 0 || info == nil || uintptr(infoLen) < unsafe.Sizeof(vsFixedFileInfo{}) {
		return ""
	}
	ffi := (*vsFixedFileInfo)(info)
	if ffi.Signature != vsFixedFileInfoSignature {
		return ""
	}
	v := formatFileVersion(ffi.FileVersionMS, ffi.FileVersionLS)
	// info は data の内部を指すので、読み終わるまで data を生かしておく
	runtime.KeepAlive(data)
	return v
})

func showTrayMenu(hwnd uintptr) {
	menu, _, _ := procCreatePopupMenu.Call()
	if menu == 0 {
		return
	}
	defer procDestroyMenu.Call(menu)

	// バージョン行は選択不可 (MF_GRAYED) かつ ID 0 なので WM_COMMAND は発生しない。
	// 読み取りに失敗したときは誤った値を出さずに行ごと省く。
	if v := appVersion(); v != "" {
		verText, _ := syscall.UTF16PtrFromString("システムモニター " + v)
		procAppendMenuW.Call(menu, MF_STRING|MF_GRAYED, 0, uintptr(unsafe.Pointer(verText)))
		procAppendMenuW.Call(menu, MF_SEPARATOR, 0, 0)
	}

	appendUsageMenu(menu)
	appendPomodoroMenu(menu)

	exitText, _ := syscall.UTF16PtrFromString("終了")
	procAppendMenuW.Call(menu, MF_STRING, ID_TRAY_EXIT, uintptr(unsafe.Pointer(exitText)))

	var pt POINT
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))

	// ポップアップメニューを閉じるために SetForegroundWindow が必要
	procSetForegroundWindow.Call(hwnd)
	procTrackPopupMenu.Call(menu, TPM_RIGHTBUTTON, uintptr(pt.X), uintptr(pt.Y), 0, hwnd, 0)
	// ポップアップを確実に閉じるためダミーメッセージ
	procPostMessageW.Call(hwnd, 0, 0, 0)
}

// addTrayIcon は現在のツールチップと指定アイコンでトレイアイコンを NIM_ADD する。
// startTray の初回登録と TaskbarCreated による再登録で共有する。
func addTrayIcon(hwnd, icon uintptr) {
	var nid NOTIFYICONDATAW
	nid.CbSize = uint32(unsafe.Sizeof(nid))
	nid.HWnd = hwnd
	nid.UID = 1
	nid.UFlags = NIF_MESSAGE | NIF_ICON | NIF_TIP
	nid.UCallbackMessage = WM_TRAYICON
	nid.HIcon = icon
	setTipText(&nid, formatTrayTooltip())
	procShellNotifyIcon.Call(NIM_ADD, uintptr(unsafe.Pointer(&nid)))
}

func trayWndProcFunc(hwnd, msg, wParam, lParam uintptr) uintptr {
	// Explorer 再起動時にブロードキャストされる。トレイアイコン登録が失われるので
	// 静的アイコンで再登録する（CPU% モードなら次の ticker で再描画される）。
	if wmTaskbarCreated != 0 && msg == uintptr(wmTaskbarCreated) {
		trayMu.Lock()
		active := trayActive
		icon := trayIcon
		trayMu.Unlock()
		if active {
			addTrayIcon(hwnd, icon)
		}
		return 0
	}
	switch msg {
	case WM_TRAYICON:
		switch lParam & 0xFFFF {
		case WM_LBUTTONUP:
			toggleWindowFromTray()
		case WM_RBUTTONUP:
			showTrayMenu(hwnd)
		}
		return 0
	case WM_COMMAND:
		// 取得や通知は時間がかかる／UI スレッドを待つので、トレイのスレッドを塞がないよう go で逃がす。
		switch wParam & 0xFFFF {
		case ID_TRAY_EXIT:
			go func() {
				pomodoroShutdown() // 隠した Teams / Outlook と止めた通知を戻してから終わる（以後は隠さない）
				cleanupTray()
				os.Exit(0)
			}()
			return 0
		case ID_TRAY_POMO_TOGGLE:
			go pomodoroTrayToggle()
			return 0
		case ID_TRAY_POMO_REVEAL:
			go pomodoroRevealAll()
			return 0
		case ID_TRAY_AI_REFRESH:
			go aiusage.RefreshNow()
			return 0
		}
	case WM_DESTROY:
		procPostQuitMessage.Call(0)
		return 0
	}
	ret, _, _ := procDefWindowProc.Call(hwnd, msg, wParam, lParam)
	return ret
}

func initTrayWindow() uintptr {
	className, _ := syscall.UTF16PtrFromString("SystemWidgetTrayClass")
	hInstance, _, _ := procGetModuleHandle.Call(0)

	trayWndProc = syscall.NewCallback(trayWndProcFunc)

	wcx := WNDCLASSEXW{
		LpfnWndProc:   trayWndProc,
		HInstance:     hInstance,
		LpszClassName: className,
	}
	wcx.CbSize = uint32(unsafe.Sizeof(wcx))

	procRegisterClassEx.Call(uintptr(unsafe.Pointer(&wcx)))

	// TaskbarCreated メッセージ ID を登録（セッション内で安定）。Explorer 再起動時の
	// トレイアイコン再登録に使う。返値 0 は失敗（その場合は再登録機能のみ無効）。
	if wmTaskbarCreated == 0 {
		msgName, _ := syscall.UTF16PtrFromString("TaskbarCreated")
		id, _, _ := procRegisterWindowMessage.Call(uintptr(unsafe.Pointer(msgName)))
		wmTaskbarCreated = uint32(id)
	}

	windowName, _ := syscall.UTF16PtrFromString("SystemWidgetTrayWindow")
	hwnd, _, _ := procCreateWindowEx.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(windowName)),
		0,
		0, 0, 0, 0,
		^uintptr(2), // HWND_MESSAGE (-3)
		0,
		hInstance,
		0,
	)
	return hwnd
}

// startTray はトレイアイコンを常駐登録する。プロセス起動時に一度だけ呼ぶ。
// ウィンドウの表示/非表示とは独立したライフサイクルで、アイコンはプロセスが
// 終了するまで通知領域に残り続ける（アイコン削除は cleanupTray のみが行う）。
func startTray() {
	trayMu.Lock()
	if trayActive {
		trayMu.Unlock()
		return
	}
	trayActive = true
	trayStop = make(chan struct{})
	stopCh := trayStop
	trayMu.Unlock()

	// トレイには icon.ico から作られた埋め込みアイコンを表示する
	icon := loadTrayIcon()
	trayMu.Lock()
	trayIcon = icon
	trayMu.Unlock()

	// トレイが CPU% モードなら、初期アイコンとして現在値で合成したアイコンを使う。
	// 起動直後は startCollector がまだ値を埋めていないことがあり、その場合は
	// アクセント無しの 0% アイコンになる（2 秒後の ticker が正しい値に差し替える）。
	// 初期合成で失敗したら fallback で icon.ico 側を表示する。
	trayDisplayMu.RLock()
	tm := trayDisplayMode
	trayDisplayMu.RUnlock()
	var initialCpuIcon uintptr
	startIcon := icon
	if tm == TRAY_DISPLAY_CPU {
		cacheMu.RLock()
		p := 0
		if cachedCPU != nil {
			p = int(math.Round(cachedCPU.Overall))
		}
		accent, hasAccent := trayAccentColor(cachedCPUFreq)
		cacheMu.RUnlock()
		if h := buildCpuIcon(p, accent, hasAccent); h != 0 {
			initialCpuIcon = h
			startIcon = h
		}
	}

	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		hwnd := initTrayWindow()
		if hwnd == 0 {
			// メッセージウィンドウを作れないとアイコン登録もできない。
			// 常駐を諦めるだけで、本体ウィンドウの表示には影響させない。
			trayMu.Lock()
			trayActive = false
			trayMu.Unlock()
			if initialCpuIcon != 0 {
				procDestroyIcon.Call(initialCpuIcon)
			}
			return
		}
		trayMu.Lock()
		trayHwnd = hwnd
		trayMu.Unlock()

		addTrayIcon(hwnd, startIcon)
		go runTrayTicker(stopCh, initialCpuIcon)

		// メッセージループ
		var m MSG
		for {
			ret, _, _ := procGetMessage.Call(
				uintptr(unsafe.Pointer(&m)),
				0, 0, 0,
			)
			// GetMessageW は WM_QUIT で 0、エラー時に -1 を返す。Win64 では BOOL(-1)
			// が下位32bitのみ 1 埋め (0x00000000FFFFFFFF) になり ^uintptr(0) と一致しない。
			// int32 にキャストして符号付きで判定する（ime/watcher.go と同方式）。
			if int32(ret) <= 0 {
				break
			}
			procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
			procDispatchMessage.Call(uintptr(unsafe.Pointer(&m)))
		}
	}()
}

// runTrayTicker はトレイのツールチップと (CPU% モード時の) アイコンを定期的に差し替える。
// currentCpuIcon は手動生成した HICON の所有権を追跡する。
// LR_SHARED な icon.ico は絶対に DestroyIcon しない。
func runTrayTicker(stopCh chan struct{}, currentCpuIcon uintptr) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	// usageKey は、最後に作った AI 使用量のアイコンのキー。同じ間は作り直さない
	// （2 秒ごとの作り直しで GDI ハンドルを無駄に消費しない）。
	usageKey := ""
	for {
		select {
		case <-stopCh:
			if currentCpuIcon != 0 {
				procDestroyIcon.Call(currentCpuIcon)
			}
			return
		case <-ticker.C:
		case <-trayKick:
		}
		{
			trayMu.Lock()
			if !trayActive {
				trayMu.Unlock()
				if currentCpuIcon != 0 {
					procDestroyIcon.Call(currentCpuIcon)
				}
				return
			}
			h := trayHwnd
			staticIcon := trayIcon
			trayMu.Unlock()

			trayDisplayMu.RLock()
			mode := effectiveTrayDisplay(trayDisplayMode, snapshotConfig().AI.Enabled)
			trayDisplayMu.RUnlock()

			var upd NOTIFYICONDATAW
			upd.CbSize = uint32(unsafe.Sizeof(upd))
			upd.HWnd = h
			upd.UID = 1
			upd.UFlags = NIF_TIP
			sysTip := formatTrayTooltip()
			setTipText(&upd, sysTip)

			if mode != TRAY_DISPLAY_USAGE {
				usageKey = ""
			}

			// AI 使用量: 角丸四角（5h の数字 + 7d のペース色）。画像が変わるときだけ作り直す。
			if mode == TRAY_DISPLAY_USAGE {
				info := aiusage.GetTrayInfo()
				setTipText(&upd, buildUsageTooltip(info.Tooltip, sysTip))
				if key := info.IconKey(); key == usageKey && currentCpuIcon != 0 {
					procShellNotifyIcon.Call(NIM_MODIFY, uintptr(unsafe.Pointer(&upd)))
					continue
				} else if newIcon := buildUsageIcon(info); newIcon != 0 {
					upd.UFlags |= NIF_ICON
					upd.HIcon = newIcon
					procShellNotifyIcon.Call(NIM_MODIFY, uintptr(unsafe.Pointer(&upd)))
					if currentCpuIcon != 0 {
						procDestroyIcon.Call(currentCpuIcon)
					}
					currentCpuIcon = newIcon
					usageKey = key
					continue
				}
				// アイコンを作れなかったときは、静的アイコンへ落とす
				usageKey = ""
			}

			if mode == TRAY_DISPLAY_CPU {
				cacheMu.RLock()
				p := 0
				if cachedCPU != nil {
					p = int(math.Round(cachedCPU.Overall))
				}
				accent, hasAccent := trayAccentColor(cachedCPUFreq)
				cacheMu.RUnlock()
				if newIcon := buildCpuIcon(p, accent, hasAccent); newIcon != 0 {
					upd.UFlags |= NIF_ICON
					upd.HIcon = newIcon
					procShellNotifyIcon.Call(NIM_MODIFY, uintptr(unsafe.Pointer(&upd)))
					if currentCpuIcon != 0 {
						procDestroyIcon.Call(currentCpuIcon)
					}
					currentCpuIcon = newIcon
					continue
				}
			}

			// アイコン表示モード (または CPU 合成失敗時) — LR_SHARED のアイコンに戻す
			if currentCpuIcon != 0 {
				upd.UFlags |= NIF_ICON
				upd.HIcon = staticIcon
			}
			procShellNotifyIcon.Call(NIM_MODIFY, uintptr(unsafe.Pointer(&upd)))
			if currentCpuIcon != 0 && (upd.UFlags&NIF_ICON) != 0 {
				procDestroyIcon.Call(currentCpuIcon)
				currentCpuIcon = 0
			}
		}
	}
}

// hideWindowToTray は本体ウィンドウを隠す。トレイアイコンは常駐しているので
// ここでは登録も削除も行わない。
func hideWindowToTray() {
	// トレイアイコンの登録に失敗している場合は隠さない。本体ウィンドウは枠なし
	// (WS_SYSMENU 除去済み) でタスクバーボタンも出ないため、隠すと復帰手段が
	// 完全に無くなる。
	trayMu.Lock()
	active := trayActive
	trayMu.Unlock()
	if !active {
		return
	}

	// ウィンドウハンドル未取得のまま隠すと、本体が隠れないまま viewMode だけ
	// 2 になって ✕ に反応しなくなる。ここで一度リカバリを試みる。
	if windowHandle.Load() == 0 {
		setupMainWindow(applicationWindowTitle)
	}
	if windowHandle.Load() == 0 {
		return
	}

	viewModeMu.Lock()
	if viewMode != viewModeTray {
		viewModeBeforeTray = viewMode
	}
	viewMode = viewModeTray
	viewModeMu.Unlock()

	procShowWindow.Call(windowHandle.Load(), SW_HIDE)
}

// windowShown は、本体ウィンドウが表示中かを返す純粋関数。トレイへ格納中（viewModeTray）、
// 非表示、OS による最小化のどれかなら、表示中ではない。
func windowShown(inTrayMode, visible, iconic bool) bool {
	return !inTrayMode && visible && !iconic
}

// isMainWindowShown は、現在の本体ウィンドウが表示中かを返す。
func isMainWindowShown() bool {
	hwnd := windowHandle.Load()
	if hwnd == 0 {
		return false
	}
	viewModeMu.RLock()
	inTray := viewMode == viewModeTray
	viewModeMu.RUnlock()
	visible, _, _ := procIsWindowVisible.Call(hwnd)
	iconic, _, _ := procIsIconic.Call(hwnd)
	return windowShown(inTray, visible != 0, iconic != 0)
}

// toggleWindowFromTray は、トレイアイコンの左クリックの動作。表示中なら格納（最小化。
// タスクバーのボタンは無いのでトレイへ隠す）し、格納中なら表示して前面に出す。
//
// 以前は、表示中のクリックは前面化だけだった（前面に無いだけのウィンドウがクリックで消えると
// 誤動作に見えるため）。クリックで開閉するトグルに変えた。前面に出したいだけのときは、
// 他のウィンドウの陰になっていても、このクリックでいったん隠れるので、もう一度クリックする。
func toggleWindowFromTray() {
	if isMainWindowShown() {
		hideWindowToTray()
		return
	}
	showWindowFromTray()
}

// showWindowFromTray は本体ウィンドウを表示して前面に出す。OS で最小化されている場合は復元する。
func showWindowFromTray() {
	if windowHandle.Load() == 0 {
		setupMainWindow(applicationWindowTitle)
	}
	hwnd := windowHandle.Load()
	if hwnd == 0 {
		return
	}

	// 格納前がコンパクト表示ならそのまま戻す。ウィンドウ幅は格納中も変えて
	// いないので、モードを戻すだけでレイアウトと幅が一致する。
	viewModeMu.Lock()
	if viewMode == viewModeTray {
		viewMode = viewModeBeforeTray
	}
	viewModeMu.Unlock()

	if iconic, _, _ := procIsIconic.Call(hwnd); iconic != 0 {
		procShowWindow.Call(hwnd, SW_RESTORE)
	} else {
		procShowWindow.Call(hwnd, SW_SHOW)
	}
	alwaysOnTopMu.Lock()
	top := alwaysOnTop
	alwaysOnTopMu.Unlock()
	if top {
		setTopmost(true)
	}
	procSetForegroundWindow.Call(hwnd)
}

func cleanupTray() {
	trayMu.Lock()
	active := trayActive
	hwnd := trayHwnd
	stopCh := trayStop
	trayActive = false
	// 二重 close を防ぐため、閉じる責務を持つのはここだけにし、
	// 取り出したチャネルは即座に手放す（/api/close とトレイの「終了」の
	// 両方からこの関数に到達しうる）。
	trayStop = nil
	trayHwnd = 0
	// trayIcon は LR_SHARED でロードされた共有ハンドルなので破棄しない
	trayIcon = 0
	trayMu.Unlock()

	if !active {
		return
	}

	// ticker ゴルーチンを停止
	if stopCh != nil {
		close(stopCh)
	}

	if hwnd != 0 {
		var nid NOTIFYICONDATAW
		nid.CbSize = uint32(unsafe.Sizeof(nid))
		nid.HWnd = hwnd
		nid.UID = 1
		procShellNotifyIcon.Call(NIM_DELETE, uintptr(unsafe.Pointer(&nid)))

		// メッセージループを終了させる。cleanupTray は os.Exit 直前に別 goroutine から
		// 呼ばれるためトレイ作成スレッドではない。DestroyWindow は失敗するので WM_CLOSE を
		// Post する（プロセスは直後に終了するのでベストエフォート。アイコン削除は上の NIM_DELETE で完了済み）。
		procPostMessageW.Call(hwnd, WM_CLOSE, 0, 0)
	}
}
