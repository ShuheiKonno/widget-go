package main

import (
	_ "embed"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"widget-go/ime"
	"widget-go/sysinfo"

	"github.com/jchv/go-webview2"
)

var (
	user32                    = syscall.NewLazyDLL("user32.dll")
	procFindWindow            = user32.NewProc("FindWindowW")
	procGetWindowLong         = user32.NewProc("GetWindowLongW")
	procSetWindowLong         = user32.NewProc("SetWindowLongW")
	procSetWindowPos          = user32.NewProc("SetWindowPos")
	procGetWindowRect         = user32.NewProc("GetWindowRect")
	procGetCursorPos          = user32.NewProc("GetCursorPos")
	procGetDpiForWindow       = user32.NewProc("GetDpiForWindow")
	procShowWindow            = user32.NewProc("ShowWindow")
	procRegisterClassEx       = user32.NewProc("RegisterClassExW")
	procRegisterWindowMessage = user32.NewProc("RegisterWindowMessageW")
	procCreateWindowEx        = user32.NewProc("CreateWindowExW")
	procDefWindowProc         = user32.NewProc("DefWindowProcW")
	procGetMessage            = user32.NewProc("GetMessageW")
	procTranslateMessage      = user32.NewProc("TranslateMessage")
	procDispatchMessage       = user32.NewProc("DispatchMessageW")
	procPostQuitMessage       = user32.NewProc("PostQuitMessage")
	procLoadIcon              = user32.NewProc("LoadIconW")
	procLoadImageW            = user32.NewProc("LoadImageW")
	procGetSystemMetrics      = user32.NewProc("GetSystemMetrics")

	shell32             = syscall.NewLazyDLL("shell32.dll")
	procShellNotifyIcon = shell32.NewProc("Shell_NotifyIconW")

	kernel32              = syscall.NewLazyDLL("kernel32.dll")
	procGetModuleHandle   = kernel32.NewProc("GetModuleHandleW")
	procCreateMutexW      = kernel32.NewProc("CreateMutexW")
	procGetModuleFileName = kernel32.NewProc("GetModuleFileNameW")

	versionDLL                  = syscall.NewLazyDLL("version.dll")
	procGetFileVersionInfoSizeW = versionDLL.NewProc("GetFileVersionInfoSizeW")
	procGetFileVersionInfoW     = versionDLL.NewProc("GetFileVersionInfoW")
	procVerQueryValueW          = versionDLL.NewProc("VerQueryValueW")

	procSetForegroundWindow        = user32.NewProc("SetForegroundWindow")
	procSetLayeredWindowAttributes = user32.NewProc("SetLayeredWindowAttributes")
	procMonitorFromWindow          = user32.NewProc("MonitorFromWindow")
	procGetMonitorInfoW            = user32.NewProc("GetMonitorInfoW")

	gdi32                  = syscall.NewLazyDLL("gdi32.dll")
	procCreateCompatibleDC = gdi32.NewProc("CreateCompatibleDC")
	procCreateDIBSection   = gdi32.NewProc("CreateDIBSection")
	procDeleteDC           = gdi32.NewProc("DeleteDC")
	procDeleteObject       = gdi32.NewProc("DeleteObject")
	procCreateBitmap       = gdi32.NewProc("CreateBitmap")

	procCreateIconIndirect = user32.NewProc("CreateIconIndirect")
	procDestroyIcon        = user32.NewProc("DestroyIcon")

	procCreatePopupMenu = user32.NewProc("CreatePopupMenu")
	procAppendMenuW     = user32.NewProc("AppendMenuW")
	procTrackPopupMenu  = user32.NewProc("TrackPopupMenu")
	procDestroyMenu     = user32.NewProc("DestroyMenu")
	procPostMessageW    = user32.NewProc("PostMessageW")
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
	WM_CLOSE     = 0x0010
	WM_LBUTTONUP = 0x0202
	WM_RBUTTONUP = 0x0205
	WM_COMMAND   = 0x0111
	WM_DESTROY   = 0x0002

	MF_STRING       = 0x00000000
	MF_GRAYED       = 0x00000001
	MF_SEPARATOR    = 0x00000800
	TPM_RIGHTBUTTON = 0x0002
	TPM_RETURNCMD   = 0x0100

	ID_TRAY_EXIT = 1001

	TRAY_DISPLAY_ICON = 0
	TRAY_DISPLAY_CPU  = 1

	SPI_GETWORKAREA = 0x0030

	MONITOR_DEFAULTTONEAREST = 0x00000002

	GWL_EXSTYLE   = -20
	WS_EX_LAYERED = 0x00080000
	LWA_ALPHA     = 0x00000002

	ALPHA_OPAQUE      = 255
	ALPHA_TRANSPARENT = 160

	SW_HIDE = 0
	SW_SHOW = 5

	IMAGE_ICON     = 1
	LR_DEFAULTSIZE = 0x00000040
	LR_SHARED      = 0x00008000
	SM_CXSMICON    = 49
	SM_CYSMICON    = 50

	TRANSPARENT    = 1
	BI_RGB         = 0
	DIB_RGB_COLORS = 0

	windowWidth  = 230
	windowHeight = 426

	// compactWindowWidth はコンパクト表示時のウィンドウ幅。
	// Windows サイドバーガジェットのドッキング時の幅に合わせてある。
	compactWindowWidth = 130

	// リサイズの下限幅。フル表示はクロック行が折り返さない幅を、
	// コンパクト表示は compactWindowWidth を下回らない範囲を確保する。
	minNormalWidth  = 200
	minCompactWidth = 120

	viewModeNormal  = 0
	viewModeCompact = 1
	viewModeTray    = 2
)

// windowHandle は WebView2 のメインウィンドウ HWND。
// startServer の HTTP ハンドラ goroutine 群と main の初期化から並行アクセスされるため
// atomic で保護する（-race 検出を含むデータ競合の回避）。
var windowHandle atomic.Uintptr

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

type MONITORINFO struct {
	CbSize    uint32
	RcMonitor RECT
	RcWork    RECT
	DwFlags   uint32
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
	CPU      *sysinfo.CPUInfo      `json:"CPU"`
	CPUFreq  *sysinfo.CPUFreqInfo  `json:"CPUFreq"`
	Memory   *sysinfo.MemoryInfo   `json:"Memory"`
	WiFi     *sysinfo.WiFiInfo     `json:"WiFi"`
	Ethernet *sysinfo.EthernetInfo `json:"Ethernet"`
	Network  *sysinfo.NetworkInfo  `json:"Network"`
	GPU      *sysinfo.GPUInfo      `json:"GPU"`
	NPU      *sysinfo.NPUInfo      `json:"NPU"`
	ViewMode int                   `json:"ViewMode"`
}

// バックグラウンドで更新されるスナップショット。
// /api/system はこのキャッシュを読むだけで、その場での計測は行わない。
var (
	cacheMu        sync.RWMutex
	cachedCPU      *sysinfo.CPUInfo
	cachedCPUFreq  *sysinfo.CPUFreqInfo
	cachedMem      *sysinfo.MemoryInfo
	cachedWiFi     *sysinfo.WiFiInfo
	cachedEthernet *sysinfo.EthernetInfo
	cachedNet      *sysinfo.NetworkInfo
	cachedGPU      *sysinfo.GPUInfo
	cachedNPU      *sysinfo.NPUInfo
)

// showGPU は「GPU/NPU を表示」設定の実行時状態。OFF の間は GPU/NPU の収集自体を止める。
// HTTP ハンドラと collector goroutine から並行に読み書きされ、cacheMu の内外どちらからも
// 読むのでロック順序の問題が起きない atomic で持つ。
var showGPU atomic.Bool

func init() { showGPU.Store(true) }

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

	transparentMu sync.Mutex
	transparent   bool
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
	// viewModeBeforeTray はトレイ格納前のモード。格納中は viewMode が 2 で
	// 潰れてしまうので、復帰時にコンパクト表示へ戻せるよう控えておく。
	// 格納中もウィンドウ幅の下限判定に使う（viewModeMu で保護）。
	viewModeBeforeTray int

	trayDisplayMu   sync.RWMutex
	trayDisplayMode = TRAY_DISPLAY_ICON // 0=アイコンのみ, 1=CPU%

	// wmTaskbarCreated は Explorer が起動/再起動時にブロードキャストする
	// "TaskbarCreated" メッセージの ID。initTrayWindow で登録する。
	// Explorer クラッシュ→再起動でトレイアイコンが消えるため、これを受けて再登録する。
	wmTaskbarCreated uint32
)

func setTransparent(enabled bool) {
	if windowHandle.Load() == 0 {
		return
	}
	gwlExStyle := int32(GWL_EXSTYLE)
	exStyle, _, _ := procGetWindowLong.Call(windowHandle.Load(), uintptr(gwlExStyle))
	newStyle := exStyle | WS_EX_LAYERED
	if newStyle != exStyle {
		procSetWindowLong.Call(windowHandle.Load(), uintptr(gwlExStyle), newStyle)
	}
	var alpha uintptr = ALPHA_OPAQUE
	if enabled {
		alpha = ALPHA_TRANSPARENT
	}
	procSetLayeredWindowAttributes.Call(windowHandle.Load(), 0, alpha, LWA_ALPHA)
}

func setTopmost(topmost bool) {
	if windowHandle.Load() == 0 {
		return
	}
	var insertAfter uintptr
	if topmost {
		insertAfter = ^uintptr(0) // HWND_TOPMOST (-1)
	} else {
		insertAfter = ^uintptr(1) // HWND_NOTOPMOST (-2)
	}
	procSetWindowPos.Call(windowHandle.Load(), insertAfter, 0, 0, 0, 0, SWP_NOMOVE|SWP_NOSIZE)
}

func getSystemData() SystemData {
	cacheMu.RLock()
	defer cacheMu.RUnlock()
	viewModeMu.RLock()
	vm := viewMode
	viewModeMu.RUnlock()
	return SystemData{
		CPU:      cachedCPU,
		CPUFreq:  cachedCPUFreq,
		Memory:   cachedMem,
		WiFi:     cachedWiFi,
		Ethernet: cachedEthernet,
		Network:  cachedNet,
		GPU:      cachedGPU,
		NPU:      cachedNPU,
		ViewMode: vm,
	}
}

func refreshNetwork() {
	netInfo, err := sysinfo.GetNetworkInfo()
	if err != nil {
		return
	}
	cacheMu.Lock()
	cachedNet = netInfo
	cacheMu.Unlock()
}

func refreshCPUAndMem() {
	cpuInfo, cpuErr := sysinfo.GetCPUInfo()
	memInfo, memErr := sysinfo.GetMemoryInfo()
	// 取得できない環境（PDH 初期化失敗など）では nil のまま。UI 側は「---」を表示する。
	freqInfo := sysinfo.GetCPUFreq()

	cacheMu.Lock()
	if cpuErr == nil {
		cachedCPU = cpuInfo
	}
	if freqInfo != nil {
		cachedCPUFreq = freqInfo
	}
	if memErr == nil {
		cachedMem = memInfo
	}
	cacheMu.Unlock()
}

// gpuActive は直前の refreshGPU で収集が有効だったか。OFF→ON のたびに InitGPU で
// 再列挙し、OFF 期間全体の平均が 1 周期だけ出るのを防ぐ。
// refreshGPU 専用に使うので goroutine 1 本からしか触らない前提。
var gpuActive bool

// refreshGPU は GPU/NPU の使用率を取得してキャッシュに反映する。
// ShowGPU が OFF の間は収集せずキャッシュを nil にする。
// InitGPU は COM を扱う DXCore を使うため、main スレッド（go-webview2 が STA 初期化済み）
// ではなく collector の goroutine から呼ぶこと。
func refreshGPU() {
	if !showGPU.Load() {
		gpuActive = false
		clearGPUCache()
		return
	}
	if !gpuActive {
		sysinfo.InitGPU()
		gpuActive = true
	}
	gpuInfo, npuInfo := sysinfo.GetGPUInfo()

	cacheMu.Lock()
	// 取得中に OFF にされた場合は、ハンドラがクリアしたキャッシュを上書きしない
	if showGPU.Load() {
		cachedGPU = gpuInfo
		cachedNPU = npuInfo
	}
	cacheMu.Unlock()
}

func clearGPUCache() {
	cacheMu.Lock()
	cachedGPU = nil
	cachedNPU = nil
	cacheMu.Unlock()
}

// wifiFailureStreak は netsh 連続失敗回数。
// AP ハンドオフ中の瞬間的な失敗でキャッシュをクリアしないよう猶予を設ける。
// refreshWiFi 専用に使うので goroutine 1 本からしか触らない前提。
var wifiFailureStreak int

// wifiFailureTolerance 回以上連続で失敗したらキャッシュを捨てて「未接続」扱いにする。
// ポーリング間隔 3 秒 × 2 回 = 6 秒以内に実状態を反映する。
const wifiFailureTolerance = 2

func refreshEthernet() {
	eth, err := sysinfo.GetEthernetInfo()
	if err != nil {
		cacheMu.Lock()
		cachedEthernet = nil
		cacheMu.Unlock()
		return
	}
	cacheMu.Lock()
	cachedEthernet = eth
	cacheMu.Unlock()
}

func refreshWiFi() {
	wifiInfo, err := sysinfo.GetWiFiInfo()
	if err != nil {
		// netsh の一時失敗は許容するが、続けて失敗したら古いキャッシュを捨てる。
		// （AP 切替中に netsh が一時的にエラーを返しても、スタレなデータを見せ続けない）
		wifiFailureStreak++
		if wifiFailureStreak >= wifiFailureTolerance {
			cacheMu.Lock()
			cachedWiFi = nil
			cacheMu.Unlock()
		}
		return
	}
	wifiFailureStreak = 0
	// 未接続（wifiInfo == nil）の場合もそのまま反映する。
	// AP 切替後は BSSID/SSID の違いがここで上書きされ、UI に即時反映される。
	cacheMu.Lock()
	cachedWiFi = wifiInfo
	cacheMu.Unlock()
}

// startCollector はバックグラウンドで周期的にシステム情報を取得し、
// キャッシュを更新する。CPU/メモリ/GPU/NPU は 2 秒ごと、Wi-Fi は 3 秒ごと。
// （フロア移動などで AP が切り替わったときに UI が「未接続」のまま固まるのを
//
//	防ぐため、Wi-Fi の検出間隔は短めに保つ。netsh は 100ms 程度で完了する）。
func startCollector() {
	sysinfo.PrimeCPU()
	sysinfo.InitCPUFreq()
	refreshCPUAndMem()
	refreshWiFi()
	refreshEthernet()
	// ネットワーク計測の基準値を仕込む（初回は差分計算できないので 0 を返す）
	refreshNetwork()

	go func() {
		// GPU の初期化はこの goroutine で行う（main スレッドで COM に触れない）
		refreshGPU()
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			refreshCPUAndMem()
			refreshNetwork()
			refreshGPU()
		}
	}()

	go func() {
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			refreshWiFi()
			refreshEthernet()
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
	if windowHandle.Load() != 0 {
		procGetWindowRect.Call(windowHandle.Load(), uintptr(unsafe.Pointer(&rect)))
	}
	return rect.Left, rect.Top
}

func moveWindow(x, y int32) {
	if windowHandle.Load() != 0 {
		procSetWindowPos.Call(windowHandle.Load(), 0, uintptr(x), uintptr(y), 0, 0, SWP_NOSIZE|SWP_NOZORDER)
	}
}

func getWindowSize() (int32, int32) {
	if windowHandle.Load() == 0 {
		return windowWidth, windowHeight
	}
	var rect RECT
	procGetWindowRect.Call(windowHandle.Load(), uintptr(unsafe.Pointer(&rect)))
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
	if windowHandle.Load() == 0 {
		return 1.0
	}
	dpi, _, _ := procGetDpiForWindow.Call(windowHandle.Load())
	if dpi == 0 {
		return 1.0
	}
	return float64(dpi) / 96.0
}

// resizeWindow はウィンドウを width×height（論理px）にリサイズする。
// anchorTop=false は下端固定（コンパクト化・幾何復元で下端に揃える既定）、
// anchorTop=true は上端固定（下端エッジを掴んだ手動リサイズで下方向に伸ばす）。
func resizeWindow(width, height int32, anchorTop bool) {
	resizeWindowAnchored(width, height, anchorTop, false)
}

// resizeWindowAnchored は resizeWindow に横方向のアンカー指定を加えたもの。
// anchorRight=true は右端固定。既定の配置が画面右下なので、コンパクト幅への
// 切り替えで左端を固定すると右側に隙間ができてしまうのを避ける。
func resizeWindowAnchored(width, height int32, anchorTop, anchorRight bool) {
	if windowHandle.Load() == 0 {
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
	// 下限はビューモードで変わる。コンパクト表示はガジェット相当まで縮められる
	// 必要があり、フル表示は内容が崩れない幅を保つ。
	minWidth := int32(minNormalWidth)
	if layoutIsCompact() {
		minWidth = minCompactWidth
	}
	if width < minWidth {
		width = minWidth
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

	var rect RECT
	procGetWindowRect.Call(windowHandle.Load(), uintptr(unsafe.Pointer(&rect)))
	newLeft := rect.Left
	if anchorRight {
		newLeft = rect.Right - scaledW // 右端固定（コンパクト幅への切替時）
	}
	newTop := rect.Bottom - scaledH // 既定: 下端固定
	if anchorTop {
		newTop = rect.Top // 上端固定（下端エッジのドラッグ時）
	}
	procSetWindowPos.Call(windowHandle.Load(), 0, uintptr(newLeft), uintptr(newTop), uintptr(scaledW), uintptr(scaledH), SWP_NOZORDER)
}

// layoutIsCompact はコンパクト表示のレイアウトが当たっているかを返す。
// トレイ格納中は viewMode が 2 になるため、格納前のモードで判断する
// （格納中に走るリサイズでコンパクト幅がフル表示の下限まで広げられないように）。
func layoutIsCompact() bool {
	viewModeMu.RLock()
	defer viewModeMu.RUnlock()
	if viewMode == viewModeTray {
		return viewModeBeforeTray == viewModeCompact
	}
	return viewMode == viewModeCompact
}

// applyViewModeWidth はビューモードの切替に合わせてウィンドウ幅を変える。
// フル表示 → コンパクトのときだけ直前の幅を Window.NormalWidth に退避し、
// 戻すときはそれを復元する（prev==mode の再適用では退避しない。起動時に
// 保存済みのコンパクト状態を適用し直すケースで退避値を壊さないため）。
// 高さは adjustWindowHeight がフロント側の実測値で別途合わせるので 0 を渡す。
func applyViewModeWidth(mode, prev int) {
	if windowHandle.Load() == 0 {
		return
	}
	if mode == viewModeCompact {
		if curW, _ := getWindowSize(); prev != viewModeCompact && curW > compactWindowWidth {
			mutateConfig(func(c *Config) { c.Window.NormalWidth = curW })
		}
		resizeWindowAnchored(compactWindowWidth, 0, false, true)
		return
	}
	width := snapshotConfig().Window.NormalWidth
	if width <= 0 {
		width = windowWidth
	}
	resizeWindowAnchored(width, 0, false, true)
}

// restoreWindowGeometry は永続化された位置・サイズを現在のウィンドウに適用する。
// 保存値が無い場合や windowHandle 未取得時は false を返し、呼び出し元はフォールバックを選ぶ。
// 論理ピクセル値で保存されているので DPI スケール補正を行う。
func restoreWindowGeometry() bool {
	return restoreWindow(true)
}

// restoreWindowPosition は保存された位置だけを再適用し、サイズには触れない。
// 起動直後の遅延再適用に使う。フロント側が内容の実寸に合わせてリサイズした直後に
// これがサイズまで復元すると、旧バージョンの保存値でウィンドウが縮んで内容が切れるため。
func restoreWindowPosition() bool {
	return restoreWindow(false)
}

func restoreWindow(applySize bool) bool {
	if windowHandle.Load() == 0 {
		return false
	}
	c := snapshotConfig()
	if !c.Window.Saved || c.Window.Width <= 0 || c.Window.Height <= 0 {
		return false
	}
	if applySize {
		resizeWindow(c.Window.Width, c.Window.Height, false)
	}

	scale := getWindowDpiScale()
	if scale <= 0 {
		scale = 1.0
	}
	physX := int32(math.Round(float64(c.Window.X) * scale))
	physY := int32(math.Round(float64(c.Window.Y) * scale))

	// 画面外に出ないように作業領域でクランプする
	hmon, _, _ := procMonitorFromWindow.Call(windowHandle.Load(), MONITOR_DEFAULTTONEAREST)
	if hmon != 0 {
		var mi MONITORINFO
		mi.CbSize = uint32(unsafe.Sizeof(mi))
		if ok, _, _ := procGetMonitorInfoW.Call(hmon, uintptr(unsafe.Pointer(&mi))); ok != 0 {
			var rect RECT
			procGetWindowRect.Call(windowHandle.Load(), uintptr(unsafe.Pointer(&rect)))
			ww := rect.Right - rect.Left
			hh := rect.Bottom - rect.Top
			if physX < mi.RcWork.Left {
				physX = mi.RcWork.Left
			}
			if physX > mi.RcWork.Right-ww {
				physX = mi.RcWork.Right - ww
			}
			if physY < mi.RcWork.Top {
				physY = mi.RcWork.Top
			}
			if physY > mi.RcWork.Bottom-hh {
				physY = mi.RcWork.Bottom - hh
			}
		}
	}
	moveWindow(physX, physY)
	return true
}

// persistCurrentWindowGeometry は現在のウィンドウ位置・サイズを config に書き込む。
func persistCurrentWindowGeometry() {
	if windowHandle.Load() == 0 {
		return
	}
	var rect RECT
	procGetWindowRect.Call(windowHandle.Load(), uintptr(unsafe.Pointer(&rect)))
	scale := getWindowDpiScale()
	if scale <= 0 {
		scale = 1.0
	}
	logicalX := int32(math.Round(float64(rect.Left) / scale))
	logicalY := int32(math.Round(float64(rect.Top) / scale))
	wid, hei := getWindowSize()
	mutateConfig(func(c *Config) {
		c.Window.X = logicalX
		c.Window.Y = logicalY
		c.Window.Width = wid
		c.Window.Height = hei
		c.Window.Saved = true
	})
}

// 作業領域 (タスクバーを除いた画面領域) の右下にウィンドウを移動する。
// サイズは GetWindowRect の実測値を使う。go-webview2 は DPI スケーリングせず
// 生のピクセルで CreateWindowExW を呼ぶため、論理サイズ×DPIスケールで計算すると
// 過大見積もりになって左に寄りすぎる。
// 作業領域は MonitorFromWindow で実際にウィンドウが乗っているモニタの値を使う
// (SPI_GETWORKAREA だとプライマリ固定で、マルチモニタ環境でズレる)。
func moveToBottomRight() {
	if windowHandle.Load() == 0 {
		return
	}
	hmon, _, _ := procMonitorFromWindow.Call(windowHandle.Load(), MONITOR_DEFAULTTONEAREST)
	if hmon == 0 {
		return
	}
	var mi MONITORINFO
	mi.CbSize = uint32(unsafe.Sizeof(mi))
	ret, _, _ := procGetMonitorInfoW.Call(hmon, uintptr(unsafe.Pointer(&mi)))
	if ret == 0 {
		return
	}
	var winRect RECT
	procGetWindowRect.Call(windowHandle.Load(), uintptr(unsafe.Pointer(&winRect)))
	w := winRect.Right - winRect.Left
	h := winRect.Bottom - winRect.Top
	// 画面端にぴったり貼るとタスクバーや他アプリとの境界が見えにくいので、
	// DPI に比例した 8px 相当のマージンを取る。
	scale := getWindowDpiScale()
	if scale <= 0 {
		scale = 1.0
	}
	margin := int32(math.Round(8 * scale))
	newX := mi.RcWork.Right - w - margin
	newY := mi.RcWork.Bottom - h - margin
	procSetWindowPos.Call(windowHandle.Load(), 0, uintptr(newX), uintptr(newY), 0, 0, SWP_NOSIZE|SWP_NOZORDER)
}

// findMainWindow は WebView2 ウィンドウのハンドルを FindWindow で取得する。
// WebView2 の初期化タイミング次第でウィンドウタイトルがすぐには反映されないため、
// 呼び出し側でリトライすることを前提にしている。
func findMainWindow(title string) uintptr {
	titlePtr, _ := syscall.UTF16PtrFromString(title)
	hwnd, _, _ := procFindWindow.Call(0, uintptr(unsafe.Pointer(titlePtr)))
	return hwnd
}

// removeWindowFrame はタイトルバー・枠を除去し、透過設定を（保持されていれば）再適用する。
// hwnd 取得後に一度だけ呼ぶ。
func removeWindowFrame(hwnd uintptr) {
	gwlStyle := int32(-16)
	style, _, _ := procGetWindowLong.Call(hwnd, uintptr(gwlStyle))
	newStyle := style &^ (WS_CAPTION | WS_THICKFRAME | WS_SYSMENU)
	procSetWindowLong.Call(hwnd, uintptr(gwlStyle), newStyle)
	procSetWindowPos.Call(hwnd, 0, 0, 0, 0, 0, SWP_FRAMECHANGED|SWP_NOMOVE|SWP_NOZORDER|SWP_NOSIZE)

	// ウィンドウ確保前に /api/transparent が届いていた場合、状態だけ保持されて
	// 実適用がされていない。ハンドル取得後にもう一度反映する。
	transparentMu.Lock()
	tr := transparent
	transparentMu.Unlock()
	if tr {
		setTransparent(true)
	}
}

// setupMainWindow はウィンドウを FindWindow で見つけ、フレームを外して windowHandle にセットする。
// 主経路では main が w.Window() で直接ハンドルを取得するため、これは
// hideWindowToTray / showWindowFromTray のフォールバック（ハンドル未取得時の復旧）専用。成功時 true。
func setupMainWindow(title string) bool {
	hwnd := findMainWindow(title)
	if hwnd == 0 {
		return false
	}
	windowHandle.Store(hwnd)
	removeWindowFrame(hwnd)
	return true
}

// 多重起動を防ぐ。既に起動していれば既存ウィンドウを前面化して false を返す
func ensureSingleInstance(windowTitle string) bool {
	const errorAlreadyExists = 183
	mutexName, _ := syscall.UTF16PtrFromString("Global\\widget-go-single-instance-mutex")
	_, _, err := procCreateMutexW.Call(0, 0, uintptr(unsafe.Pointer(mutexName)))
	if errno, ok := err.(syscall.Errno); ok && errno == errorAlreadyExists {
		titlePtr, _ := syscall.UTF16PtrFromString(windowTitle)
		if hwnd, _, _ := procFindWindow.Call(0, uintptr(unsafe.Pointer(titlePtr))); hwnd != 0 {
			procShowWindow.Call(hwnd, SW_SHOW)
			procSetForegroundWindow.Call(hwnd)
		}
		return false
	}
	return true
}

func main() {
	runtime.LockOSThread()

	windowTitle := "システムモニター"
	if !ensureSingleInstance(windowTitle) {
		return
	}

	// WebView2 のユーザーデータを固定場所に保存する。
	// 既定の挙動 (%APPDATA%\{exe 名}) に依存すると、ビルド名変更やカレントディレクトリ違いで
	// localStorage が別プロファイル扱いになる。明示指定で設定の記憶を確実にする。
	localAppData := os.Getenv("LOCALAPPDATA")
	if localAppData == "" {
		localAppData = os.Getenv("APPDATA")
	}
	appRoot := filepath.Join(localAppData, "widget-go")
	dataPath := filepath.Join(appRoot, "WebView2")
	_ = os.MkdirAll(dataPath, 0755)

	// 設定ファイルを読み込み、永続化された設定を適用する。
	// 以降の状態変更は即ファイルに書き込むので、os.Exit で強制終了されても記憶が残る。
	configPath = filepath.Join(appRoot, "config.json")
	loadConfig()
	c := snapshotConfig()
	transparentMu.Lock()
	transparent = c.Transparent
	transparentMu.Unlock()
	alwaysOnTopMu.Lock()
	alwaysOnTop = c.Topmost
	alwaysOnTopMu.Unlock()
	viewModeMu.Lock()
	if c.ViewMode == viewModeNormal || c.ViewMode == viewModeCompact {
		viewMode = c.ViewMode
	}
	viewModeBeforeTray = viewMode
	viewModeMu.Unlock()
	trayDisplayMu.Lock()
	if c.TrayDisplay == "cpu" {
		trayDisplayMode = TRAY_DISPLAY_CPU
	} else {
		trayDisplayMode = TRAY_DISPLAY_ICON
	}
	trayDisplayMu.Unlock()
	showGPU.Store(c.ShowGPU)

	apiToken := generateToken()
	ln, port := listenLocal()
	startCollector()
	go startServer(ln, apiToken)

	// IME Composition Window の左上吸い込み現象を WinEvent Hook で監視・自動復旧する。
	// 内部で goroutine + runtime.LockOSThread + GetMessageW ループを起動するので、
	// ここではブロッキングしない。
	if c.IME.Enabled {
		ime.StartWatcher(ime.Config{
			Enabled:     c.IME.Enabled,
			CooldownSec: c.IME.CooldownSec,
			ThresholdPx: c.IME.ThresholdPx,
			DryRun:      c.IME.DryRun,
		})
	}

	w := webview2.NewWithOptions(webview2.WebViewOptions{
		Debug:     false,
		AutoFocus: true,
		DataPath:  dataPath,
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

	// go-webview2 は CreateWindowExW を NewWithOptions 内で同期的に実行済みなので、
	// w.Window() でメインウィンドウの HWND を即座に取得できる。旧実装の FindWindow
	// ポーリング（タイトル文字列依存・最大5秒待ち）は不要。ここでハンドルを確定させ、
	// フレーム除去・位置復元・前面表示を適用する。
	windowHandle.Store(uintptr(w.Window()))
	removeWindowFrame(windowHandle.Load())
	if !restoreWindowGeometry() {
		moveToBottomRight()
	}
	// go-webview2 の SetSize(HintFixed) が後段で位置をリセットすることがあるため、
	// 初期化が落ち着いたあとに再適用する。
	time.AfterFunc(500*time.Millisecond, func() {
		if !restoreWindowPosition() {
			moveToBottomRight()
		}
	})
	// 常に前面表示の復元
	alwaysOnTopMu.Lock()
	top := alwaysOnTop
	alwaysOnTopMu.Unlock()
	if top {
		setTopmost(true)
	}

	// トレイアイコンは起動時から常駐させる（ウィンドウの表示/非表示とは独立）。
	// windowHandle 確定後・w.Run() の前に呼ぶ必要がある。startTray は
	// メッセージウィンドウ用の goroutine を起動して即座に戻るのでブロックしない。
	startTray()
	// Alt+F4 などで w.Run() を抜けた場合に通知領域へアイコンを残さない。
	defer cleanupTray()

	w.SetSize(windowWidth, windowHeight, webview2.HintFixed)
	w.Navigate(fmt.Sprintf("http://127.0.0.1:%d", port))

	w.Run()
}
