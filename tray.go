package main

import (
	"fmt"
	"math"
	"os"
	"runtime"
	"syscall"
	"time"
	"unsafe"
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

// buildCpuIcon は CPU 使用率 (0-100) を示すトレイアイコンを GDI で生成する。
// 背景は半透明の角丸ダーク矩形、文字は白。
// 返値が 0 でない場合、呼び出し側は不要になったら procDestroyIcon.Call(hIcon) で解放する必要がある。
func buildCpuIcon(percent int) uintptr {
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
	// α=200, RGB=(20,22,40) → premul RGB=(15,17,31)
	const bgColor = uint32(200)<<24 | uint32(15)<<16 | uint32(17)<<8 | uint32(31)
	for i := range pixels {
		pixels[i] = bgColor
	}
	// 角落とし (1px)
	iw := int(w)
	ih := int(h)
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

	cpuStr := "---"
	memStr := "---"
	netStr := "未接続"

	if cachedCPU != nil {
		cpuStr = fmt.Sprintf("%d%%", int(math.Round(cachedCPU.Overall)))
	}
	if cachedMem != nil {
		memStr = fmt.Sprintf("%d%%", int(math.Round(cachedMem.UsedPercent)))
	}
	if cachedWiFi != nil && cachedWiFi.SSID != "" {
		if cachedWiFi.Channel > 0 {
			netStr = fmt.Sprintf("%s(%dch)", cachedWiFi.Band, cachedWiFi.Channel)
		} else {
			netStr = cachedWiFi.Band
		}
	} else if cachedEthernet != nil && cachedEthernet.Connected {
		if cachedEthernet.LinkSpeedBps > 0 {
			netStr = "Ethernet " + formatLinkSpeedShort(cachedEthernet.LinkSpeedBps)
		} else {
			netStr = "Ethernet"
		}
	}

	return fmt.Sprintf("CPU: %s | Mem: %s | %s", cpuStr, memStr, netStr)
}

func setTipText(nid *NOTIFYICONDATAW, text string) {
	tip, _ := syscall.UTF16FromString(text)
	copy(nid.SzTip[:], tip)
}

func showTrayMenu(hwnd uintptr) {
	menu, _, _ := procCreatePopupMenu.Call()
	if menu == 0 {
		return
	}
	defer procDestroyMenu.Call(menu)

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
// enterTray の初回登録と TaskbarCreated による再登録で共有する。
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
			restoreFromTray()
		case WM_RBUTTONUP:
			showTrayMenu(hwnd)
		}
		return 0
	case WM_COMMAND:
		if (wParam & 0xFFFF) == ID_TRAY_EXIT {
			go func() {
				cleanupTray()
				os.Exit(0)
			}()
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

func enterTray() {
	// ウィンドウハンドル未取得でトレイ化に入ると、本体が隠れないまま
	// trayActive=true が残り続けて二度と ✕ に反応しなくなる。
	// ここで一度リカバリを試み、それでも取れないならトレイ化を諦める。
	if windowHandle.Load() == 0 {
		setupMainWindow("システムモニター")
	}
	if windowHandle.Load() == 0 {
		return
	}

	trayMu.Lock()
	if trayActive {
		trayMu.Unlock()
		return
	}
	trayActive = true
	trayStop = make(chan struct{})
	trayMu.Unlock()

	viewModeMu.Lock()
	viewMode = 2
	viewModeMu.Unlock()

	procShowWindow.Call(windowHandle.Load(), SW_HIDE)

	// トレイには icon.ico から作られた埋め込みアイコンを表示する
	icon := loadTrayIcon()
	trayMu.Lock()
	trayIcon = icon
	trayMu.Unlock()

	// トレイが CPU% モードなら、初期アイコンとして現在値で合成したアイコンを使う。
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
		cacheMu.RUnlock()
		if h := buildCpuIcon(p); h != 0 {
			initialCpuIcon = h
			startIcon = h
		}
	}

	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		hwnd := initTrayWindow()
		if hwnd == 0 {
			// ウィンドウ作成失敗時はトレイモードを解除して復帰
			trayMu.Lock()
			trayActive = false
			trayMu.Unlock()
			viewModeMu.Lock()
			viewMode = 0
			viewModeMu.Unlock()
			if windowHandle.Load() != 0 {
				procShowWindow.Call(windowHandle.Load(), SW_SHOW)
			}
			return
		}
		trayMu.Lock()
		trayHwnd = hwnd
		trayMu.Unlock()

		addTrayIcon(hwnd, startIcon)

		// トレイ定期更新: ツールチップと (CPU% モード時の) アイコンを差し替える。
		// currentCpuIcon は手動生成した HICON 所有権を追跡する。
		// LR_SHARED な icon.ico は絶対に DestroyIcon しない。
		stopCh := trayStop
		currentCpuIcon := initialCpuIcon
		go func() {
			runtime.LockOSThread()
			defer runtime.UnlockOSThread()
			ticker := time.NewTicker(2 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-stopCh:
					if currentCpuIcon != 0 {
						procDestroyIcon.Call(currentCpuIcon)
						currentCpuIcon = 0
					}
					return
				case <-ticker.C:
					trayMu.Lock()
					if !trayActive {
						trayMu.Unlock()
						if currentCpuIcon != 0 {
							procDestroyIcon.Call(currentCpuIcon)
							currentCpuIcon = 0
						}
						return
					}
					h := trayHwnd
					staticIcon := trayIcon
					trayMu.Unlock()

					trayDisplayMu.RLock()
					mode := trayDisplayMode
					trayDisplayMu.RUnlock()

					var upd NOTIFYICONDATAW
					upd.CbSize = uint32(unsafe.Sizeof(upd))
					upd.HWnd = h
					upd.UID = 1
					upd.UFlags = NIF_TIP
					setTipText(&upd, formatTrayTooltip())

					if mode == TRAY_DISPLAY_CPU {
						cacheMu.RLock()
						p := 0
						if cachedCPU != nil {
							p = int(math.Round(cachedCPU.Overall))
						}
						cacheMu.RUnlock()
						if newIcon := buildCpuIcon(p); newIcon != 0 {
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
		}()

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

func restoreFromTray() {
	trayMu.Lock()
	if !trayActive {
		trayMu.Unlock()
		return
	}
	trayActive = false
	hwnd := trayHwnd
	close(trayStop)
	trayMu.Unlock()

	// トレイアイコン削除
	var nid NOTIFYICONDATAW
	nid.CbSize = uint32(unsafe.Sizeof(nid))
	nid.HWnd = hwnd
	nid.UID = 1
	procShellNotifyIcon.Call(NIM_DELETE, uintptr(unsafe.Pointer(&nid)))

	viewModeMu.Lock()
	viewMode = 0
	viewModeMu.Unlock()

	// ウィンドウ復元
	if windowHandle.Load() != 0 {
		procShowWindow.Call(windowHandle.Load(), SW_SHOW)
		alwaysOnTopMu.Lock()
		top := alwaysOnTop
		alwaysOnTopMu.Unlock()
		if top {
			setTopmost(true)
		}
	}

	// メッセージループ終了のため非表示ウィンドウを破棄する。
	// DestroyWindow は作成スレッド以外から呼ぶと失敗するため、直接呼ばず
	// WM_CLOSE を Post して作成スレッド（トレイのメッセージループ）側で
	// DefWindowProc→DestroyWindow→WM_DESTROY→PostQuitMessage を走らせる。
	if hwnd != 0 {
		procPostMessageW.Call(hwnd, WM_CLOSE, 0, 0)
	}

	trayMu.Lock()
	trayHwnd = 0
	// trayIcon は LR_SHARED でロードされた共有ハンドルなので破棄しない
	trayIcon = 0
	trayMu.Unlock()
}

func cleanupTray() {
	trayMu.Lock()
	active := trayActive
	hwnd := trayHwnd
	stopCh := trayStop
	trayActive = false
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

	trayMu.Lock()
	trayHwnd = 0
	trayIcon = 0

	trayMu.Unlock()
}
