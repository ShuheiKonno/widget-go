package main

import (
	"encoding/json"
	"fmt"
	"math"
	"net"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"widget-go/sysinfo"

	"github.com/jchv/go-webview2"
)

var (
	user32              = syscall.NewLazyDLL("user32.dll")
	procFindWindow      = user32.NewProc("FindWindowW")
	procGetWindowLong   = user32.NewProc("GetWindowLongW")
	procSetWindowLong   = user32.NewProc("SetWindowLongW")
	procSetWindowPos    = user32.NewProc("SetWindowPos")
	procGetWindowRect   = user32.NewProc("GetWindowRect")
	procGetCursorPos    = user32.NewProc("GetCursorPos")
	procGetDpiForWindow = user32.NewProc("GetDpiForWindow")
	procShowWindow      = user32.NewProc("ShowWindow")
	procRegisterClassEx = user32.NewProc("RegisterClassExW")
	procCreateWindowEx  = user32.NewProc("CreateWindowExW")
	procDefWindowProc   = user32.NewProc("DefWindowProcW")
	procGetMessage      = user32.NewProc("GetMessageW")
	procTranslateMessage = user32.NewProc("TranslateMessage")
	procDispatchMessage  = user32.NewProc("DispatchMessageW")
	procPostQuitMessage  = user32.NewProc("PostQuitMessage")
	procDestroyWindow    = user32.NewProc("DestroyWindow")
	procLoadIcon         = user32.NewProc("LoadIconW")

	shell32             = syscall.NewLazyDLL("shell32.dll")
	procShellNotifyIcon = shell32.NewProc("Shell_NotifyIconW")

	kernel32            = syscall.NewLazyDLL("kernel32.dll")
	procGetModuleHandle = kernel32.NewProc("GetModuleHandleW")

	gdi32                = syscall.NewLazyDLL("gdi32.dll")
	procCreateCompatibleDC = gdi32.NewProc("CreateCompatibleDC")
	procCreateDIBSection   = gdi32.NewProc("CreateDIBSection")
	procSelectObject       = gdi32.NewProc("SelectObject")
	procCreateFontW        = gdi32.NewProc("CreateFontW")
	procSetTextColor       = gdi32.NewProc("SetTextColor")
	procSetBkMode          = gdi32.NewProc("SetBkMode")
	procTextOutW           = gdi32.NewProc("TextOutW")
	procDeleteDC           = gdi32.NewProc("DeleteDC")
	procDeleteObject       = gdi32.NewProc("DeleteObject")
	procCreateBitmap       = gdi32.NewProc("CreateBitmap")
	procGetTextExtentPoint32 = gdi32.NewProc("GetTextExtentPoint32W")

	procCreateIconIndirect = user32.NewProc("CreateIconIndirect")
	procDestroyIcon        = user32.NewProc("DestroyIcon")
)

const (
	WS_CAPTION       = 0x00C00000
	WS_THICKFRAME    = 0x00040000
	WS_SYSMENU       = 0x00080000
	SWP_FRAMECHANGED = 0x0020
	SWP_NOMOVE       = 0x0002
	SWP_NOSIZE       = 0x0001
	SWP_NOZORDER     = 0x0004

	NIM_ADD    = 0x00000000
	NIM_MODIFY = 0x00000001
	NIM_DELETE = 0x00000002

	NIF_MESSAGE = 0x00000001
	NIF_ICON    = 0x00000002
	NIF_TIP     = 0x00000004

	WM_APP       = 0x8000
	WM_TRAYICON  = WM_APP + 1
	WM_LBUTTONUP = 0x0202
	WM_DESTROY   = 0x0002

	SW_HIDE = 0
	SW_SHOW = 5

	TRANSPARENT = 1
	BI_RGB      = 0
	DIB_RGB_COLORS = 0

	windowWidth  = 210
	windowHeight = 400
)

var windowHandle uintptr

type POINT struct {
	X, Y int32
}

type RECT struct {
	Left, Top, Right, Bottom int32
}

type NOTIFYICONDATAW struct {
	CbSize           uint32
	HWnd             uintptr
	UID              uint32
	UFlags           uint32
	UCallbackMessage uint32
	HIcon            uintptr
	SzTip            [128]uint16
	DwState          uint32
	DwStateMask      uint32
	SzInfo           [256]uint16
	UVersion         uint32
	SzInfoTitle      [64]uint16
	DwInfoFlags      uint32
	GuidItem         [16]byte
	HBalloonIcon     uintptr
}

type BITMAPINFOHEADER struct {
	BiSize          uint32
	BiWidth         int32
	BiHeight        int32
	BiPlanes        uint16
	BiBitCount      uint16
	BiCompression   uint32
	BiSizeImage     uint32
	BiXPelsPerMeter int32
	BiYPelsPerMeter int32
	BiClrUsed       uint32
	BiClrImportant  uint32
}

type ICONINFO struct {
	FIcon    uint32
	XHotspot uint32
	YHotspot uint32
	HbmMask  uintptr
	HbmColor uintptr
}

type SIZE struct {
	Cx, Cy int32
}

type WNDCLASSEXW struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     uintptr
	HIcon         uintptr
	HCursor       uintptr
	HbrBackground uintptr
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       uintptr
}

type MSG struct {
	HWnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      POINT
}

type SystemData struct {
	CPU      *sysinfo.CPUInfo    `json:"CPU"`
	Memory   *sysinfo.MemoryInfo `json:"Memory"`
	WiFi     *sysinfo.WiFiInfo   `json:"WiFi"`
	ViewMode int                 `json:"ViewMode"`
}

// バックグラウンドで更新されるスナップショット。
// /api/system はこのキャッシュを読むだけで、その場での計測は行わない。
var (
	cacheMu    sync.RWMutex
	cachedCPU  *sysinfo.CPUInfo
	cachedMem  *sysinfo.MemoryInfo
	cachedWiFi *sysinfo.WiFiInfo
)

// ドラッグ開始時の状態（物理ピクセル）。
// ドラッグ中の差分計算をすべてサーバー側で行い、
// ブラウザの論理ピクセルと Win32 の物理ピクセルの不一致を回避する。
var (
	dragMu        sync.Mutex
	dragStartCurX int32
	dragStartCurY int32
	dragStartWinX int32
	dragStartWinY int32
)

var (
	alwaysOnTopMu sync.Mutex
	alwaysOnTop   bool
)

// トレイモード管理
var (
	trayMu      sync.Mutex
	trayHwnd    uintptr
	trayActive  bool
	trayIcon    uintptr
	trayStop    chan struct{}
	trayWndProc uintptr // GC防止のためパッケージレベルに保持

	viewModeMu sync.RWMutex
	viewMode   int // 0=normal, 1=compact, 2=tray
)

func setTopmost(topmost bool) {
	if windowHandle == 0 {
		return
	}
	var insertAfter uintptr
	if topmost {
		insertAfter = ^uintptr(0) // HWND_TOPMOST (-1)
	} else {
		insertAfter = ^uintptr(1) // HWND_NOTOPMOST (-2)
	}
	procSetWindowPos.Call(windowHandle, insertAfter, 0, 0, 0, 0, SWP_NOMOVE|SWP_NOSIZE)
}

// --- システムトレイ機能 ---

func loadAppIcon() uintptr {
	hModule, _, _ := procGetModuleHandle.Call(0)
	icon, _, _ := procLoadIcon.Call(hModule, uintptr(1))
	if icon == 0 {
		icon, _, _ = procLoadIcon.Call(0, uintptr(32512)) // IDI_APPLICATION
	}
	return icon
}

var prevTrayIcon uintptr

// createCPUIcon はCPU使用率の数字をアイコンとして描画して返す (16x16)
func createCPUIcon(percent int) uintptr {
	if percent > 99 {
		percent = 99
	}
	if percent < 0 {
		percent = 0
	}
	text := fmt.Sprintf("%d", percent)

	const size = 16

	// メモリDC作成
	hdc, _, _ := procCreateCompatibleDC.Call(0)
	if hdc == 0 {
		return loadAppIcon()
	}

	// 32bit DIBセクション作成 (カラービットマップ)
	bmi := BITMAPINFOHEADER{
		BiWidth:    size,
		BiHeight:   -size, // トップダウン
		BiPlanes:   1,
		BiBitCount: 32,
	}
	bmi.BiSize = uint32(unsafe.Sizeof(bmi))
	var bits uintptr
	hBitmap, _, _ := procCreateDIBSection.Call(hdc, uintptr(unsafe.Pointer(&bmi)), DIB_RGB_COLORS, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if hBitmap == 0 {
		procDeleteDC.Call(hdc)
		return loadAppIcon()
	}
	oldBmp, _, _ := procSelectObject.Call(hdc, hBitmap)

	// 背景を白、外枠を黒で描画
	pixelSlice := unsafe.Slice((*uint32)(unsafe.Pointer(bits)), size*size)
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			if x == 0 || x == size-1 || y == 0 || y == size-1 {
				pixelSlice[y*size+x] = 0xFF000000 // 黒枠
			} else {
				pixelSlice[y*size+x] = 0xFFFFFFFF // 白背景
			}
		}
	}

	// フォント作成
	fontHeight := int32(-13)
	if len(text) == 1 {
		fontHeight = -14
	}
	fontName, _ := syscall.UTF16PtrFromString("Segoe UI")
	hFont, _, _ := procCreateFontW.Call(
		uintptr(fontHeight), 0, 0, 0,
		700, // FW_BOLD
		0, 0, 0,
		0, 0, 0, 0, 0,
		uintptr(unsafe.Pointer(fontName)),
	)
	oldFont, _, _ := procSelectObject.Call(hdc, hFont)

	// テキスト描画
	procSetBkMode.Call(hdc, TRANSPARENT)
	procSetTextColor.Call(hdc, 0x00000000) // 黒 (BGR: 000000)

	// テキストの中央配置
	textUTF16, _ := syscall.UTF16FromString(text)
	var sz SIZE
	procGetTextExtentPoint32.Call(hdc, uintptr(unsafe.Pointer(&textUTF16[0])), uintptr(len(textUTF16)-1), uintptr(unsafe.Pointer(&sz)))
	x := (int32(size) - sz.Cx) / 2
	y := (int32(size) - sz.Cy) / 2
	procTextOutW.Call(hdc, uintptr(x), uintptr(y), uintptr(unsafe.Pointer(&textUTF16[0])), uintptr(len(textUTF16)-1))

	// 後片付け
	procSelectObject.Call(hdc, oldFont)
	procSelectObject.Call(hdc, oldBmp)
	procDeleteObject.Call(hFont)
	procDeleteDC.Call(hdc)

	// マスクビットマップ (全不透明)
	hMask, _, _ := procCreateBitmap.Call(size, size, 1, 1, 0)

	// アイコン作成
	ii := ICONINFO{
		FIcon:    1,
		HbmMask:  hMask,
		HbmColor: hBitmap,
	}
	icon, _, _ := procCreateIconIndirect.Call(uintptr(unsafe.Pointer(&ii)))

	procDeleteObject.Call(hBitmap)
	procDeleteObject.Call(hMask)

	return icon
}

func formatTrayTooltip() string {
	cacheMu.RLock()
	defer cacheMu.RUnlock()

	cpuStr := "---"
	memStr := "---"
	wifiStr := "未接続"

	if cachedCPU != nil {
		cpuStr = fmt.Sprintf("%d%%", int(math.Round(cachedCPU.Overall)))
	}
	if cachedMem != nil {
		memStr = fmt.Sprintf("%d%%", int(math.Round(cachedMem.UsedPercent)))
	}
	if cachedWiFi != nil && cachedWiFi.SSID != "" {
		if cachedWiFi.Channel > 0 {
			wifiStr = fmt.Sprintf("%s(%dch)", cachedWiFi.Band, cachedWiFi.Channel)
		} else {
			wifiStr = cachedWiFi.Band
		}
	}

	return fmt.Sprintf("CPU: %s | Mem: %s | %s", cpuStr, memStr, wifiStr)
}

func setTipText(nid *NOTIFYICONDATAW, text string) {
	tip, _ := syscall.UTF16FromString(text)
	copy(nid.SzTip[:], tip)
}

func trayWndProcFunc(hwnd, msg, wParam, lParam uintptr) uintptr {
	switch msg {
	case WM_TRAYICON:
		event := lParam & 0xFFFF
		if event == WM_LBUTTONUP {
			restoreFromTray()
		}
		return 0
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
		HInstance:      hInstance,
		LpszClassName: className,
	}
	wcx.CbSize = uint32(unsafe.Sizeof(wcx))

	procRegisterClassEx.Call(uintptr(unsafe.Pointer(&wcx)))

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

	if windowHandle != 0 {
		procShowWindow.Call(windowHandle, SW_HIDE)
	}

	// 初期CPU%アイコンを生成
	cpuPercent := 0
	cacheMu.RLock()
	if cachedCPU != nil {
		cpuPercent = int(math.Round(cachedCPU.Overall))
	}
	cacheMu.RUnlock()
	icon := createCPUIcon(cpuPercent)
	trayMu.Lock()
	trayIcon = icon
	trayMu.Unlock()

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
			if windowHandle != 0 {
				procShowWindow.Call(windowHandle, SW_SHOW)
			}
			return
		}
		trayMu.Lock()
		trayHwnd = hwnd
		trayMu.Unlock()

		var nid NOTIFYICONDATAW
		nid.CbSize = uint32(unsafe.Sizeof(nid))
		nid.HWnd = hwnd
		nid.UID = 1
		nid.UFlags = NIF_MESSAGE | NIF_ICON | NIF_TIP
		nid.UCallbackMessage = WM_TRAYICON
		nid.HIcon = trayIcon
		setTipText(&nid, formatTrayTooltip())
		procShellNotifyIcon.Call(NIM_ADD, uintptr(unsafe.Pointer(&nid)))

		// ツールチップ定期更新
		stopCh := trayStop
		go func() {
			runtime.LockOSThread()
			defer runtime.UnlockOSThread()
			ticker := time.NewTicker(2 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-stopCh:
					return
				case <-ticker.C:
					trayMu.Lock()
					if !trayActive {
						trayMu.Unlock()
						return
					}
					h := trayHwnd
					trayMu.Unlock()

					// CPU%アイコンを更新
					cp := 0
					cacheMu.RLock()
					if cachedCPU != nil {
						cp = int(math.Round(cachedCPU.Overall))
					}
					cacheMu.RUnlock()
					newIcon := createCPUIcon(cp)

					var upd NOTIFYICONDATAW
					upd.CbSize = uint32(unsafe.Sizeof(upd))
					upd.HWnd = h
					upd.UID = 1
					upd.UFlags = NIF_TIP | NIF_ICON
					upd.HIcon = newIcon
					setTipText(&upd, formatTrayTooltip())
					procShellNotifyIcon.Call(NIM_MODIFY, uintptr(unsafe.Pointer(&upd)))

					// 前回のアイコンを破棄してリーク防止
					trayMu.Lock()
					if prevTrayIcon != 0 {
						procDestroyIcon.Call(prevTrayIcon)
					}
					prevTrayIcon = newIcon
					trayMu.Unlock()
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
			if ret == 0 || ret == ^uintptr(0) {
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
	if windowHandle != 0 {
		procShowWindow.Call(windowHandle, SW_SHOW)
		alwaysOnTopMu.Lock()
		top := alwaysOnTop
		alwaysOnTopMu.Unlock()
		if top {
			setTopmost(true)
		}
	}

	// メッセージループ終了のため非表示ウィンドウを破棄
	procDestroyWindow.Call(hwnd)

	trayMu.Lock()
	trayHwnd = 0
	// アイコンハンドルを破棄してリーク防止
	if prevTrayIcon != 0 {
		procDestroyIcon.Call(prevTrayIcon)
		prevTrayIcon = 0
	}
	if trayIcon != 0 {
		procDestroyIcon.Call(trayIcon)
		trayIcon = 0
	}
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

		// メッセージループを終了させる
		procDestroyWindow.Call(hwnd)
	}

	trayMu.Lock()
	trayHwnd = 0
	if prevTrayIcon != 0 {
		procDestroyIcon.Call(prevTrayIcon)
		prevTrayIcon = 0
	}
	if trayIcon != 0 {
		procDestroyIcon.Call(trayIcon)
		trayIcon = 0
	}
	trayMu.Unlock()
}

func getSystemData() SystemData {
	cacheMu.RLock()
	defer cacheMu.RUnlock()
	viewModeMu.RLock()
	vm := viewMode
	viewModeMu.RUnlock()
	return SystemData{
		CPU:      cachedCPU,
		Memory:   cachedMem,
		WiFi:     cachedWiFi,
		ViewMode: vm,
	}
}

func refreshCPUAndMem() {
	cpuInfo, cpuErr := sysinfo.GetCPUInfo()
	memInfo, memErr := sysinfo.GetMemoryInfo()

	cacheMu.Lock()
	if cpuErr == nil {
		cachedCPU = cpuInfo
	}
	if memErr == nil {
		cachedMem = memInfo
	}
	cacheMu.Unlock()
}

func refreshWiFi() {
	wifiInfo, err := sysinfo.GetWiFiInfo()
	if err != nil {
		// netsh が失敗した場合は前回のキャッシュを保持
		return
	}
	// 未接続（wifiInfo == nil）の場合もそのまま反映する
	cacheMu.Lock()
	cachedWiFi = wifiInfo
	cacheMu.Unlock()
}

// startCollector はバックグラウンドで周期的にシステム情報を取得し、
// キャッシュを更新する。CPU/メモリは 2 秒ごと、Wi-Fi は 15 秒ごと
// （netsh の呼び出しコストが高いため）。
func startCollector() {
	sysinfo.PrimeCPU()
	refreshCPUAndMem()
	refreshWiFi()

	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			refreshCPUAndMem()
		}
	}()

	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			refreshWiFi()
		}
	}()
}

func getCursorPos() (int32, int32) {
	var pt POINT
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	return pt.X, pt.Y
}

func getWindowPos() (int32, int32) {
	var rect RECT
	if windowHandle != 0 {
		procGetWindowRect.Call(windowHandle, uintptr(unsafe.Pointer(&rect)))
	}
	return rect.Left, rect.Top
}

func moveWindow(x, y int32) {
	if windowHandle != 0 {
		procSetWindowPos.Call(windowHandle, 0, uintptr(x), uintptr(y), 0, 0, SWP_NOSIZE|SWP_NOZORDER)
	}
}

func getWindowSize() (int32, int32) {
	if windowHandle == 0 {
		return windowWidth, windowHeight
	}
	var rect RECT
	procGetWindowRect.Call(windowHandle, uintptr(unsafe.Pointer(&rect)))
	width := rect.Right - rect.Left
	height := rect.Bottom - rect.Top
	scale := getWindowDpiScale()
	if scale <= 0 {
		scale = 1.0
	}
	logicalW := int32(math.Round(float64(width) / scale))
	logicalH := int32(math.Round(float64(height) / scale))
	if logicalW <= 0 {
		logicalW = windowWidth
	}
	if logicalH <= 0 {
		logicalH = windowHeight
	}
	return logicalW, logicalH
}

func getWindowDpiScale() float64 {
	if windowHandle == 0 {
		return 1.0
	}
	dpi, _, _ := procGetDpiForWindow.Call(windowHandle)
	if dpi == 0 {
		return 1.0
	}
	return float64(dpi) / 96.0
}

func resizeWindow(width, height int32) {
	if windowHandle == 0 {
		return
	}
	if width <= 0 || height <= 0 {
		curW, curH := getWindowSize()
		if width <= 0 {
			width = curW
		}
		if height <= 0 {
			height = curH
		}
	}
	if width < 200 {
		width = 200
	}
	if width > 600 {
		width = 600
	}
	if height < 100 {
		height = 100
	}
	if height > 1000 {
		height = 1000
	}
	scale := getWindowDpiScale()
	scaledW := int32(math.Round(float64(width) * scale))
	scaledH := int32(math.Round(float64(height) * scale))
	procSetWindowPos.Call(windowHandle, 0, 0, 0, uintptr(scaledW), uintptr(scaledH), SWP_NOMOVE|SWP_NOZORDER)
}

func startServer(port int) {
	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(getHTML()))
	})

	mux.HandleFunc("/api/system", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(getSystemData())
	})

	mux.HandleFunc("/api/close", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		go func() {
			cleanupTray()
			os.Exit(0)
		}()
	})

	mux.HandleFunc("/api/entertray", func(w http.ResponseWriter, r *http.Request) {
		enterTray()
		w.WriteHeader(http.StatusOK)
	})

	mux.HandleFunc("/api/setviewmode", func(w http.ResponseWriter, r *http.Request) {
		modeStr := r.URL.Query().Get("mode")
		m, err := strconv.Atoi(modeStr)
		if err != nil || m < 0 || m > 1 {
			http.Error(w, "invalid mode", http.StatusBadRequest)
			return
		}
		viewModeMu.Lock()
		viewMode = m
		viewModeMu.Unlock()
		w.WriteHeader(http.StatusOK)
	})

	// ドラッグ開始時に現在のカーソル位置とウィンドウ位置を返す
	// ドラッグ開始 — カーソルとウィンドウの位置を物理ピクセルで保存
	mux.HandleFunc("/api/dragstart", func(w http.ResponseWriter, r *http.Request) {
		dragMu.Lock()
		dragStartCurX, dragStartCurY = getCursorPos()
		dragStartWinX, dragStartWinY = getWindowPos()
		dragMu.Unlock()
		w.WriteHeader(http.StatusOK)
	})

	// ドラッグ中 — 現在のカーソル位置を Win32 API で取得し、
	// 開始時との差分でウィンドウを移動（すべて物理ピクセル）
	mux.HandleFunc("/api/dragmove", func(w http.ResponseWriter, r *http.Request) {
		cx, cy := getCursorPos()
		dragMu.Lock()
		newX := dragStartWinX + (cx - dragStartCurX)
		newY := dragStartWinY + (cy - dragStartCurY)
		dragMu.Unlock()
		moveWindow(newX, newY)
		w.WriteHeader(http.StatusOK)
	})

	mux.HandleFunc("/api/windowsize", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		wid, hei := getWindowSize()
		json.NewEncoder(w).Encode(map[string]int32{
			"width":  wid,
			"height": hei,
		})
	})

	mux.HandleFunc("/api/toggletopmost", func(w http.ResponseWriter, r *http.Request) {
		alwaysOnTopMu.Lock()
		alwaysOnTop = !alwaysOnTop
		current := alwaysOnTop
		alwaysOnTopMu.Unlock()
		setTopmost(current)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"topmost": current})
	})

	mux.HandleFunc("/api/resize", func(w http.ResponseWriter, r *http.Request) {
		wStr := r.URL.Query().Get("w")
		hStr := r.URL.Query().Get("h")
		wVal, _ := strconv.Atoi(wStr)
		h, _ := strconv.Atoi(hStr)
		resizeWindow(int32(wVal), int32(h))
		w.WriteHeader(http.StatusOK)
	})

	http.ListenAndServe(fmt.Sprintf("127.0.0.1:%d", port), mux)
}

func getAvailablePort() int {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 18080
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	return port
}

func removeWindowFrame(title string) {
	titlePtr, _ := syscall.UTF16PtrFromString(title)
	hwnd, _, _ := procFindWindow.Call(0, uintptr(unsafe.Pointer(titlePtr)))
	if hwnd == 0 {
		return
	}
	windowHandle = hwnd

	gwlStyle := int32(-16)
	style, _, _ := procGetWindowLong.Call(hwnd, uintptr(gwlStyle))
	newStyle := style &^ (WS_CAPTION | WS_THICKFRAME | WS_SYSMENU)
	procSetWindowLong.Call(hwnd, uintptr(gwlStyle), newStyle)
	procSetWindowPos.Call(hwnd, 0, 0, 0, 0, 0, SWP_FRAMECHANGED|SWP_NOMOVE|SWP_NOZORDER|SWP_NOSIZE)
}

func main() {
	runtime.LockOSThread()

	port := getAvailablePort()
	startCollector()
	go startServer(port)

	windowTitle := "システムモニター"

	w := webview2.NewWithOptions(webview2.WebViewOptions{
		Debug:     false,
		AutoFocus: true,
		WindowOptions: webview2.WindowOptions{
			Title:  windowTitle,
			Width:  windowWidth,
			Height: windowHeight,
			Center: true,
		},
	})
	if w == nil {
		panic("Failed to create webview")
	}
	defer w.Destroy()

	go func() {
		time.Sleep(200 * time.Millisecond)
		removeWindowFrame(windowTitle)
	}()

	w.SetSize(windowWidth, windowHeight, webview2.HintFixed)
	w.Navigate(fmt.Sprintf("http://127.0.0.1:%d", port))

	w.Run()
}

func getHTML() string {
	return `<!DOCTYPE html>
<html lang="ja">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>システムモニター</title>
    <style>
        * { margin: 0; padding: 0; box-sizing: border-box; }
        html, body { width: 100%; height: 100%; overflow: hidden; }
        body { font-family: 'Segoe UI', 'Yu Gothic UI', sans-serif; background: linear-gradient(135deg, #3b3b6d 0%, #2e4a7a 100%); color: #fff; font-size: 13px; user-select: none; }
        .container { width: 100%; height: 100%; background: rgba(60,60,95,0.96); padding: 30px 10px 10px 10px; position: relative; cursor: grab; }
        .container.dragging { cursor: grabbing; }
        .top-btns { position: absolute; top: 8px; right: 8px; display: flex; gap: 4px; z-index: 100; }
        .top-btn { width: 18px; height: 18px; border-radius: 50%; border: none; cursor: pointer; display: flex; align-items: center; justify-content: center; font-size: 10px; transition: all 0.2s; }
        .close-btn { background: #ff5f57; color: transparent; }
        .close-btn:hover { background: #ff3b30; color: rgba(0,0,0,0.6); }
        .pin-btn { background: rgba(255,255,255,0.2); color: #aaa; font-size: 9px; }
        .pin-btn:hover { background: rgba(255,255,255,0.35); }
        .pin-btn.active { background: #4CAF50; color: #fff; }
        .compact-btn { background: rgba(255,255,255,0.2); color: #ccc; font-size: 10px; line-height: 1; }
        .compact-btn:hover { background: rgba(255,255,255,0.35); }
        .compact .section { padding: 5px 10px; margin-bottom: 4px; }
        .compact .section-header { margin-bottom: 0; }
        .compact .cores-container { display: none; }
        .compact .cpu-progress { display: none; }
        .compact .mem-progress { display: none; }
        .compact .memory-details { display: none; }
        .compact .wifi-info { display: none; }
        .compact .wifi-compact-value { display: inline; }
        .section { background: rgba(255,255,255,0.09); border-radius: 8px; padding: 8px 10px; margin-bottom: 8px; border: 1px solid rgba(255,255,255,0.16); }
        .section:last-child { margin-bottom: 0; }
        .section-header { display: flex; align-items: center; margin-bottom: 6px; }
        .section-icon { font-size: 14px; margin-right: 6px; }
        .section-title { font-size: 12px; font-weight: 600; flex-grow: 1; color: #e8e8ff; }
        .section-value { color: #7bffe0; font-size: 13px; font-weight: 700; font-family: 'Consolas', monospace; }
        .progress-bar { height: 5px; background: rgba(255,255,255,0.18); border-radius: 3px; overflow: hidden; margin-bottom: 6px; }
        .progress-fill { height: 100%; border-radius: 3px; transition: width 0.3s ease; }
        .cpu-fill { background: linear-gradient(90deg, #8a9cff, #b07cff); }
        .memory-fill { background: linear-gradient(90deg, #ffa3ff, #ff7a8a); }
        .cores-container { display: grid; grid-template-columns: repeat(4,1fr); gap: 3px; margin-top: 5px; }
        .core-item { text-align: center; }
        .core-label { color:#c5c5e0; font-size:8px; display:block; margin-bottom:2px; }
        .core-bar-container { height:24px; background:rgba(255,255,255,0.16); border-radius:3px; position:relative; overflow:hidden; }
        .core-bar { position:absolute; bottom:0; width:100%; background:linear-gradient(180deg,#8a9cff,#b07cff); border-radius:3px; transition:height 0.3s ease; }
        .core-value { display:block; text-align:center; color:#fff; font-size:9px; font-weight:600; line-height:24px; height:24px; margin-top:-24px; position:relative; text-shadow:0 1px 2px rgba(0,0,0,0.6); pointer-events:none; }
        .memory-details { text-align:right; font-size:11px; color:#c5c5e0; font-family:'Consolas', monospace; }
        .memory-details .used { color:#ffa3ff; font-weight:600; }
        .wifi-info { display:flex; flex-direction:column; gap:4px; }
        .wifi-row { display:flex; justify-content:space-between; align-items:center; }
        .wifi-label { color:#c5c5e0; font-size:11px; }
        .wifi-value { color:#fff; font-size:11px; font-weight:600; font-family:'Consolas', monospace; }
        .wifi-connected { color:#7fffaa; }
        .wifi-disconnected { color:#ff8a8a; }
        .wifi-compact-value { display:none; }
    </style>
</head>
<body>
    <div class="container" id="dragArea">
        <div class="top-btns">
            <button class="top-btn pin-btn" id="pinBtn" title="常に前面に表示">📌</button>
            <button class="top-btn compact-btn" id="compactBtn" title="コンパクト表示">▬</button>
            <button class="top-btn close-btn" id="closeBtn" title="閉じる">✕</button>
        </div>
        <div class="section">
            <div class="section-header"><span class="section-icon">💻</span><span class="section-title">CPU</span><span class="section-value" id="cpu-overall">0%</span></div>
            <div class="progress-bar cpu-progress"><div class="progress-fill cpu-fill" id="cpu-bar" style="width:0%"></div></div>
            <div class="cores-container" id="cores-container"></div>
        </div>
        <div class="section">
            <div class="section-header"><span class="section-icon">🧠</span><span class="section-title">メモリ</span><span class="section-value" id="memory-percent">0%</span></div>
            <div class="progress-bar mem-progress"><div class="progress-fill memory-fill" id="memory-bar" style="width:0%"></div></div>
            <div class="memory-details"><span class="used" id="memory-used">0 GB</span><span id="memory-total">/ 0 GB</span></div>
        </div>
        <div class="section">
            <div class="section-header"><span class="section-icon">📶</span><span class="section-title">Wi-Fi</span><span class="section-value wifi-compact-value" id="wifi-compact">---</span></div>
            <div class="wifi-info">
                <div class="wifi-row wifi-detail"><span class="wifi-label">SSID</span><span class="wifi-value" id="wifi-ssid">---</span></div>
                <div class="wifi-row"><span class="wifi-label">帯域</span><span class="wifi-value" id="wifi-band">---</span></div>
                <div class="wifi-row wifi-detail"><span class="wifi-label">規格</span><span class="wifi-value" id="wifi-type">---</span></div>
                <div class="wifi-row wifi-detail"><span class="wifi-label">信号強度</span><span class="wifi-value" id="wifi-signal">---</span></div>
            </div>
        </div>
    </div>
    <script>
        const wifiStandards = { '802.11a':'802.11a','802.11b':'802.11b','802.11g':'802.11g','802.11n':'802.11n (Wi-Fi 4)','802.11ac':'802.11ac (Wi-Fi 5)','802.11ax':'802.11ax (Wi-Fi 6)','802.11be':'802.11be (Wi-Fi 7)' };
        function formatBytes(b){return (b/(1024*1024*1024)).toFixed(1)+' GB';}

        // Close button
        document.getElementById('closeBtn').addEventListener('click', function(e) {
            e.stopPropagation();
            fetch('/api/close').catch(()=>{});
        });

        // Pin button (always-on-top)
        document.getElementById('pinBtn').addEventListener('click', async function(e) {
            e.stopPropagation();
            try {
                const res = await fetch('/api/toggletopmost');
                const data = await res.json();
                this.classList.toggle('active', data.topmost);
            } catch(err) {}
        });

        // 3段階表示切替: Normal → Compact → Tray
        let currentViewMode = 0;
        document.getElementById('compactBtn').addEventListener('click', function(e) {
            e.stopPropagation();
            if (currentViewMode === 0) {
                currentViewMode = 1;
                document.getElementById('dragArea').classList.add('compact');
                fetch('/api/setviewmode?mode=1').catch(()=>{});
                setTimeout(adjustWindowHeight, 50);
            } else if (currentViewMode === 1) {
                currentViewMode = 2;
                fetch('/api/entertray').catch(()=>{});
            }
        });

        // Drag functionality
        let isDragging = false;
        let isResizing = false;
        let resizeDir = '';
        let startCursorX, startCursorY;
        let startWinW = 0, startWinH = 0;
        const dragArea = document.getElementById('dragArea');
        const resizeMargin = 6;

        dragArea.addEventListener('mousemove', function(e) {
            if (isDragging || isResizing) return;
            const rect = dragArea.getBoundingClientRect();
            const onRight = (rect.right - e.clientX) <= resizeMargin;
            const onBottom = (rect.bottom - e.clientY) <= resizeMargin;
            if (onRight && onBottom) {
                dragArea.style.cursor = 'nwse-resize';
            } else if (onRight) {
                dragArea.style.cursor = 'ew-resize';
            } else if (onBottom) {
                dragArea.style.cursor = 'ns-resize';
            } else {
                dragArea.style.cursor = 'grab';
            }
        });

        dragArea.addEventListener('mouseleave', function() {
            if (!isDragging && !isResizing) {
                dragArea.style.cursor = 'grab';
            }
        });

        dragArea.addEventListener('mousedown', async function(e) {
            if (e.target.id === 'closeBtn') return;
            const rect = dragArea.getBoundingClientRect();
            const onRight = (rect.right - e.clientX) <= resizeMargin;
            const onBottom = (rect.bottom - e.clientY) <= resizeMargin;
            if (onRight || onBottom) {
                isResizing = true;
                resizeDir = (onRight ? 'r' : '') + (onBottom ? 'b' : '');
                dragArea.classList.add('resizing');
                startCursorX = e.screenX;
                startCursorY = e.screenY;
                try {
                    const res = await fetch('/api/windowsize');
                    const data = await res.json();
                    startWinW = data.width;
                    startWinH = data.height;
                } catch(err) {
                    isResizing = false;
                    dragArea.classList.remove('resizing');
                }
                e.preventDefault();
                return;
            }
            isDragging = true;
            dragArea.classList.add('dragging');
            fetch('/api/dragstart').catch(()=>{ isDragging = false; dragArea.classList.remove('dragging'); });
        });

        document.addEventListener('mousemove', function(e) {
            if (isResizing) {
                const dx = e.screenX - startCursorX;
                const dy = e.screenY - startCursorY;
                let newW = startWinW;
                let newH = startWinH;
                if (resizeDir.indexOf('r') !== -1) {
                    newW = startWinW + dx;
                }
                if (resizeDir.indexOf('b') !== -1) {
                    newH = startWinH + dy;
                }
                fetch('/api/resize?w=' + Math.round(newW) + '&h=' + Math.round(newH)).catch(()=>{});
                return;
            }
            if (!isDragging) return;
            fetch('/api/dragmove').catch(()=>{});
        });

        document.addEventListener('mouseup', function() {
            if (isResizing) {
                isResizing = false;
                dragArea.classList.remove('resizing');
            }
            isDragging = false;
            dragArea.classList.remove('dragging');
        });

        function adjustWindowHeight() {
            // height:100% / overflow:hidden の影響を避けるため
            // 一時的に auto にしてコンテンツの自然な高さを測定する
            const el = document.documentElement;
            const bd = document.body;
            const ct = document.getElementById('dragArea');
            el.style.height = 'auto'; bd.style.height = 'auto'; ct.style.height = 'auto';
            const h = ct.scrollHeight;
            el.style.height = '100%'; bd.style.height = '100%'; ct.style.height = '100%';
            fetch('/api/resize?h=' + h).catch(()=>{});
        }

        async function updateData(){
            try{ const res=await fetch('/api/system'); const data=await res.json();
                if(data.CPU){ const o=Math.round(data.CPU.Overall); document.getElementById('cpu-overall').textContent=o+'%'; document.getElementById('cpu-bar').style.width=o+'%'; const c=document.getElementById('cores-container'); if(c.children.length===0 && data.CPU.Cores){ data.CPU.Cores.forEach((_,i)=>{ const d=document.createElement('div'); d.className='core-item'; d.innerHTML='<span class="core-label">Core '+i+'</span><div class="core-bar-container"><div class="core-bar" id="core-bar-'+i+'"></div></div><span class="core-value" id="core-value-'+i+'">0%</span>'; c.appendChild(d);}); }
                    if(data.CPU.Cores){ data.CPU.Cores.forEach((load,i)=>{ const bar=document.getElementById('core-bar-'+i); const val=document.getElementById('core-value-'+i); if(bar&&val){ const r=Math.round(load); bar.style.height=r+'%'; val.textContent=r+'%'; } }); }}
                if(data.Memory){ const p=Math.round(data.Memory.UsedPercent); document.getElementById('memory-percent').textContent=p+'%'; document.getElementById('memory-bar').style.width=p+'%'; document.getElementById('memory-used').textContent=formatBytes(data.Memory.Used); document.getElementById('memory-total').textContent='/ '+formatBytes(data.Memory.Total); }
                const ssid=document.getElementById('wifi-ssid'); if(data.WiFi && data.WiFi.SSID){ ssid.textContent=data.WiFi.SSID; ssid.className='wifi-value wifi-connected'; let band=data.WiFi.Band; if(data.WiFi.Channel>0){ band+=' (Ch '+data.WiFi.Channel+')'; } document.getElementById('wifi-band').textContent=band; document.getElementById('wifi-type').textContent=wifiStandards[data.WiFi.Type]||data.WiFi.Type; document.getElementById('wifi-signal').textContent=data.WiFi.Signal+'%'; let compact=data.WiFi.Band; if(data.WiFi.Channel>0){ compact+='('+data.WiFi.Channel+'ch)'; } document.getElementById('wifi-compact').textContent=compact; } else { ssid.textContent='未接続'; ssid.className='wifi-value wifi-disconnected'; document.getElementById('wifi-band').textContent='---'; document.getElementById('wifi-type').textContent='---'; document.getElementById('wifi-signal').textContent='---'; document.getElementById('wifi-compact').textContent='未接続'; }
                // サーバーとの viewMode 同期
                if (data.ViewMode !== undefined && data.ViewMode !== currentViewMode) {
                    currentViewMode = data.ViewMode;
                    if (currentViewMode === 0) {
                        document.getElementById('dragArea').classList.remove('compact');
                        setTimeout(adjustWindowHeight, 50);
                    } else if (currentViewMode === 1) {
                        document.getElementById('dragArea').classList.add('compact');
                        setTimeout(adjustWindowHeight, 50);
                    }
                }
            }catch(e){ console.error('Update error:',e); }
        }
        let heightAdjusted = false;
        updateData().then(()=>{ adjustWindowHeight(); setTimeout(adjustWindowHeight, 300); heightAdjusted = true; });
        setInterval(updateData,2000);
        window.addEventListener('load', function() {
            if(!heightAdjusted){ setTimeout(adjustWindowHeight, 350); setTimeout(adjustWindowHeight, 1000); }
        });
    </script>
</body>
</html>`
}
