package main

import (
	_ "embed"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"widget-go/aiusage"
	"widget-go/ping"
	"widget-go/sysinfo"

	"github.com/jchv/go-webview2"
)

var (
	user32                    = syscall.NewLazyDLL("user32.dll")
	procFindWindow            = user32.NewProc("FindWindowW")
	procIsWindowVisible       = user32.NewProc("IsWindowVisible")
	procIsIconic              = user32.NewProc("IsIconic")
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

	dwmapi                    = syscall.NewLazyDLL("dwmapi.dll")
	procDwmSetWindowAttribute = dwmapi.NewProc("DwmSetWindowAttribute")

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
	NIF_INFO    = 0x00000010

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

	// AI 使用量のメニュー項目（AI 使用量が ON のときだけ出す）
	ID_TRAY_AI_REFRESH = 1002 // 1003〜1005 は、削除したログインと「AI 通知のテスト」の項目

	// ポモドーロのメニュー項目（区画を表示しているときだけ出す）
	ID_TRAY_POMO_TOGGLE = 1006
	ID_TRAY_POMO_REVEAL = 1007

	TRAY_DISPLAY_ICON  = 0
	TRAY_DISPLAY_CPU   = 1
	TRAY_DISPLAY_USAGE = 2 // AI 使用量（Claude / Codex）。AI 使用量が OFF のときは実際の表示はアイコンのみ

	SPI_GETWORKAREA = 0x0030

	MONITOR_DEFAULTTONEAREST = 0x00000002

	GWL_EXSTYLE   = -20
	WS_EX_LAYERED = 0x00080000
	LWA_ALPHA     = 0x00000002

	ALPHA_OPAQUE      = 255
	ALPHA_TRANSPARENT = 160

	SW_HIDE    = 0
	SW_SHOW    = 5
	SW_RESTORE = 9

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

	// DWM の角丸設定（Windows 11 22000 以降）。それ以前の OS では
	// DwmSetWindowAttribute が E_INVALIDARG を返すだけで、直角のまま動作する。
	DWMWA_WINDOW_CORNER_PREFERENCE = 33
	DWMWCP_ROUNDSMALL              = 3
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
	// Disk は固定ドライブの容量と Disk I/O。区画を非表示にしている間と、取得できないときは null。
	Disk *sysinfo.DiskInfo `json:"Disk"`
	// PowerMode は電源モード（設定アプリの「電源モード」）の状態。取得できない環境では null。
	PowerMode *sysinfo.PowerModeInfo `json:"PowerMode"`
	// WorkTimer は勤務時間タイマーの表示値。区画を非表示にしている間は null。
	WorkTimer *WorkTimerInfo `json:"WorkTimer"`
	// Pomodoro はポモドーロタイマーの表示値。区画を非表示にしている間は null。
	Pomodoro *PomodoroInfo `json:"Pomodoro"`
	// Ping は Ping 監視の表示値。区画を非表示にしている間と、ホスト未登録時は null。
	Ping     *ping.Info `json:"Ping"`
	ViewMode int        `json:"ViewMode"`
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
	cachedPower    *sysinfo.PowerModeInfo
	cachedDrives   []sysinfo.DriveInfo
	cachedDiskIO   *sysinfo.DiskIOInfo
)

// showDisk は Disk 区画を表示しているか（Hidden.Disk の逆）。OFF の間は Disk の収集自体を止める。
// showGPU と同じく、HTTP ハンドラと collector goroutine から並行に読み書きされる。
var showDisk atomic.Bool

// diskKick は Disk 区画を表示に戻したときに、容量の取得を次の周期を待たず即座に走らせる合図。
var diskKick = make(chan struct{}, 1)

// startupGeometrySettled は、起動時のジオメトリ復元（遅延再適用まで）が済んだかどうか。
// 済むまでは persistCurrentWindowGeometry が何も保存しない。
var startupGeometrySettled atomic.Bool

// showGPU は「GPU/NPU を表示」設定の実行時状態。OFF の間は GPU/NPU の収集自体を止める。
// HTTP ハンドラと collector goroutine から並行に読み書きされ、cacheMu の内外どちらからも
// 読むのでロック順序の問題が起きない atomic で持つ。
var showGPU atomic.Bool

func init() {
	showGPU.Store(true)
	showDisk.Store(true)
}

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
	trayDisplayMode = TRAY_DISPLAY_ICON // 0=アイコンのみ, 1=CPU%, 2=AI 使用量

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
	// config のロックを取るので、cacheMu を保持する前に計算する（ロックを入れ子にしない）
	workTimer := currentWorkTimer(workTimerClock())
	pomodoro := currentPomodoro(pomodoroClock())
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
		Disk:     diskSnapshotLocked(),
		// 設定アプリなど外部での変更にも追従するため、2 秒周期の収集で更新する。
		PowerMode: cachedPower,
		WorkTimer: workTimer,
		Pomodoro:  pomodoro,
		Ping:      currentPing(),
		ViewMode:  vm,
	}
}

// diskSnapshotLocked はキャッシュから DiskInfo を組み立てる。cacheMu を保持して呼ぶこと。
// 容量も I/O も無ければ nil（UI は区画ごと隠す）。
func diskSnapshotLocked() *sysinfo.DiskInfo {
	if !showDisk.Load() || (len(cachedDrives) == 0 && cachedDiskIO == nil) {
		return nil
	}
	return &sysinfo.DiskInfo{Drives: cachedDrives, IO: cachedDiskIO}
}

// refreshDiskIO は Disk I/O を取得してキャッシュに反映する。
// 区画が非表示の間は PDH クエリを閉じ、収集しない。
func refreshDiskIO() {
	if !showDisk.Load() {
		sysinfo.CloseDiskIO()
		clearDiskCache()
		return
	}
	io := sysinfo.GetDiskIO()
	cacheMu.Lock()
	// 取得中に OFF にされた場合は、ハンドラがクリアしたキャッシュを上書きしない
	if showDisk.Load() {
		cachedDiskIO = io
	}
	cacheMu.Unlock()
}

// refreshDrives は固定ドライブの容量を取得してキャッシュに反映する。
func refreshDrives() {
	if !showDisk.Load() {
		return
	}
	drives := sysinfo.GetDrives()
	cacheMu.Lock()
	if showDisk.Load() {
		cachedDrives = drives
	}
	cacheMu.Unlock()
}

func clearDiskCache() {
	cacheMu.Lock()
	cachedDrives = nil
	cachedDiskIO = nil
	cacheMu.Unlock()
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

// refreshPowerMode は電源モードの状態を取得してキャッシュに反映し、その値を返す。
// 取得できない環境（API が無い、読み取り失敗）では nil で上書きする。古い状態を見せ続けるより、
// UI の行ごと隠す方が正確なため（電源プランの変更などで前提が変わりうる）。
// 切替機能が OFF の間は、電源モードの API を一切呼ばずに nil にする（Windows の設定・制御のまま）。
func refreshPowerMode() *sysinfo.PowerModeInfo {
	var info *sysinfo.PowerModeInfo
	if snapshotConfig().PowerModeEnabled {
		info = sysinfo.GetPowerMode()
	}
	cacheMu.Lock()
	cachedPower = info
	cacheMu.Unlock()
	return info
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
	refreshPowerMode()
	refreshWiFi()
	refreshEthernet()
	// ネットワーク計測の基準値を仕込む（初回は差分計算できないので 0 を返す）
	refreshNetwork()

	go func() {
		// GPU の初期化はこの goroutine で行う（main スレッドで COM に触れない）
		refreshGPU()
		// Disk I/O の PDH クエリも先に開き、最初のベースラインを取っておく（最初の周期から値を出すため）
		refreshDiskIO()
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			refreshCPUAndMem()
			refreshPowerMode()
			refreshNetwork()
			refreshGPU()
			refreshDiskIO()
		}
	}()

	// ドライブの容量は変化が遅く、応答の遅いドライブ（スピンアップ待ちの外付け HDD など）に
	// 待たされることもあるので、2 秒周期の収集とは別の goroutine で 10 秒ごとに取る。
	go func() {
		refreshDrives()
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
			case <-diskKick:
			}
			refreshDrives()
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

// getWindowSize はウィンドウの大きさをレイアウト px（物理 px ÷ DPI 倍率 ÷ ズーム。フロントの CSS px と
// 同じ）で返す。フロントとやり取りするサイズと、保存する幅・高さに使う。位置の計算には
// getWindowDpiLogicalSize を使うこと（zoom.go の先頭を参照）。
func getWindowSize() (int32, int32) {
	if windowHandle.Load() == 0 {
		return windowWidth, windowHeight
	}
	var rect RECT
	procGetWindowRect.Call(windowHandle.Load(), uintptr(unsafe.Pointer(&rect)))
	width := rect.Right - rect.Left
	height := rect.Bottom - rect.Top
	scale := layoutScale()
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

// getWindowDpiLogicalSize はウィンドウの大きさを DPI 論理 px（物理 px ÷ DPI 倍率。ズームを含まない）で
// 返す。右下の座標（DPI 論理 px）から左上を逆算するなど、位置の計算に使う。
func getWindowDpiLogicalSize() (int32, int32) {
	left, top, right, bottom := getWindowLogicalRect()
	return right - left, bottom - top
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

// resizeWindow はウィンドウを width×height（レイアウト px。ズーム中は物理 px がその分変わる）にリサイズする。
// anchorTop=false は下端固定（コンパクト化・幾何復元で下端に揃える既定）、
// anchorTop=true は上端固定（下端エッジを掴んだ手動リサイズで下方向に伸ばす）。
// minWindowHeight はウィンドウ高さの下限（論理 px）。
const minWindowHeight = 40

// maxWindowHeight はウィンドウ高さの上限（レイアウト px）。
const maxWindowHeight = 3000

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
	// 下限は区画 1 つのコンパクト表示（約 55px）より小さくする。大きいと下に空白が出る。
	// 設定画面は内容が高いので、開いている間はフロント側が必要な高さまで広げる。
	if height < minWindowHeight {
		height = minWindowHeight
	}
	// 上限は目安（レイアウト px）。実際に収まる高さは、下の作業領域への矯正が決める。
	// ズームで縮小しているときは、1000 を超える中身も作業領域に収まるので、大きめにしておく。
	if height > maxWindowHeight {
		height = maxWindowHeight
	}
	scale := layoutScale()
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
	// 下端固定で高くすると上へ伸びて画面外に出る（設定画面を開いたときなど）。
	// 作業領域に収まるよう高さと位置を矯正する。
	if hmon, _, _ := procMonitorFromWindow.Call(windowHandle.Load(), MONITOR_DEFAULTTONEAREST); hmon != 0 {
		var mi MONITORINFO
		mi.CbSize = uint32(unsafe.Sizeof(mi))
		if ok, _, _ := procGetMonitorInfoW.Call(hmon, uintptr(unsafe.Pointer(&mi))); ok != 0 {
			newTop, scaledH = clampVertical(newTop, scaledH, mi.RcWork.Top, mi.RcWork.Bottom)
		}
	}
	procSetWindowPos.Call(windowHandle.Load(), 0, uintptr(newLeft), uintptr(newTop), uintptr(scaledW), uintptr(scaledH), SWP_NOZORDER)
}

// clampVertical は [top, top+height) を [workTop, workBottom) に収める。
// 作業領域より高い場合は高さを作業領域の高さに切り詰める。
func clampVertical(top, height, workTop, workBottom int32) (int32, int32) {
	if height > workBottom-workTop {
		height = workBottom - workTop
	}
	if top < workTop {
		top = workTop
	}
	if top+height > workBottom {
		top = workBottom - height
	}
	return top, height
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

// applyViewModeGeometry はビューモードの切替に合わせてウィンドウの幅と位置を変える。
//
// 幅: フル表示 → コンパクトのときだけ直前の幅を Window.NormalWidth に退避し、
// 戻すときはそれを復元する（prev==mode の再適用では退避しない。起動時に
// 保存済みのコンパクト状態を適用し直すケースで退避値を壊さないため）。
// 高さは adjustWindowHeight がフロント側の実測値で別途合わせるので 0 を渡す。
//
// 位置: 出ていくモードの位置を控えてから、入るモードに記憶があればそこへ移動する。
// 記憶が無い初回はリサイズ時の右端固定の結果をそのまま使う。
func applyViewModeGeometry(mode, prev int) {
	if windowHandle.Load() == 0 {
		return
	}
	if prev != mode {
		rememberModePosition(prev)
	}

	if mode == viewModeCompact {
		if curW, _ := getWindowSize(); prev != viewModeCompact && curW > compactWindowWidth {
			mutateConfig(func(c *Config) { c.Window.NormalWidth = curW })
		}
		resizeWindowAnchored(compactWindowWidth, 0, false, true)
	} else {
		width := snapshotConfig().Window.NormalWidth
		if width <= 0 {
			width = windowWidth
		}
		resizeWindowAnchored(width, 0, false, true)
	}

	// 移動はリサイズの後に行う。先に動かすと右端固定のリサイズで位置がずれる。
	moveToRememberedPosition(mode)
}

// rememberModePosition は現在のウィンドウの右下を指定モードのスロットに保存する。
// トレイ (viewModeTray) など対象外のモードでは何もしない。
func rememberModePosition(mode int) {
	if mode != viewModeNormal && mode != viewModeCompact {
		return
	}
	_, _, right, bottom := getWindowLogicalRect()
	compact := mode == viewModeCompact
	mutateConfig(func(c *Config) { setModePosition(c, compact, right, bottom) })
}

// moveToRememberedPosition は指定モードの記憶された位置へウィンドウを移動する。
// 記憶している右下に現在のサイズを合わせる形で左上を逆算する。移動したら true。
// 記憶が無ければ何もせず false を返す（呼び出し元のリサイズ結果をそのまま残す）。
func moveToRememberedPosition(mode int) bool {
	c := snapshotConfig()
	var right, bottom int32
	switch mode {
	case viewModeCompact:
		if !c.Window.CompactPosSaved {
			return false
		}
		right, bottom = c.Window.CompactRight, c.Window.CompactBottom
	case viewModeNormal:
		if !c.Window.NormalPosSaved {
			return false
		}
		right, bottom = c.Window.NormalRight, c.Window.NormalBottom
	default:
		return false
	}
	// 右下の座標は DPI 論理 px なので、大きさも DPI 論理 px で引く（レイアウト px だとズーム中にずれる）
	wid, hei := getWindowDpiLogicalSize()
	moveWindowLogicalClamped(right-wid, bottom-hei)
	return true
}

// normalizeViewMode は保存されたビューモードを起動時に使える値へ矯正する。
// フル (0) とコンパクト (1) 以外は、手編集や将来の値でもフル表示に倒す。
// トレイ (2) は実行時の状態で、起動時の復元対象ではない。
func normalizeViewMode(m int) int {
	if m == viewModeCompact {
		return viewModeCompact
	}
	return viewModeNormal
}

// startupWidth は起動時に使うウィンドウ幅を返す。コンパクトは専用幅。フル表示は前回
// ユーザーがリサイズした値を尊重する（コンパクト中に終了した場合の width は専用幅なので、
// フル表示の幅を持つ normalWidth を優先して見る）。
func startupWidth(mode int, normalWidth, width int32) int32 {
	if mode == viewModeCompact {
		return compactWindowWidth
	}
	if normalWidth > 0 {
		return normalWidth
	}
	if width > 0 {
		return width
	}
	return windowWidth
}

// applyStartupGeometry は起動時のジオメトリを適用する。
// 前回終了したモードの幅にして、そのモードで最後にいた位置へ戻す。位置の記憶が無い
// 初回（と、記憶の無い旧バージョンの設定）は画面右下から始める。
// 位置は右下で覚えているので、フロントが高さを下端固定で詰めても位置はずれない。
//
// applyHeight=false のときは高さに触れない。起動直後の遅延再適用では、フロントが
// 内容の実寸に合わせて詰めた高さを上書きしないようにするため。
func applyStartupGeometry(applyHeight bool) {
	if windowHandle.Load() == 0 {
		return
	}
	c := snapshotConfig()
	mode := normalizeViewMode(c.ViewMode)
	var height int32 // 0 は「現在の高さを維持」
	if applyHeight {
		height = windowHeight
	}
	resizeWindow(startupWidth(mode, c.Window.NormalWidth, c.Window.Width), height, false)
	if !moveToRememberedPosition(mode) {
		moveToBottomRight()
	}
}

// moveWindowLogicalClamped は論理ピクセル座標でウィンドウを移動する。
// DPI スケール補正をしたうえで、ウィンドウが乗っているモニタの作業領域から
// はみ出さないようにクランプする。
func moveWindowLogicalClamped(logicalX, logicalY int32) {
	if windowHandle.Load() == 0 {
		return
	}
	scale := getWindowDpiScale()
	if scale <= 0 {
		scale = 1.0
	}
	physX := int32(math.Round(float64(logicalX) * scale))
	physY := int32(math.Round(float64(logicalY) * scale))

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
}

// getWindowLogicalRect は現在のウィンドウの四辺を論理ピクセルで返す。
func getWindowLogicalRect() (left, top, right, bottom int32) {
	var rect RECT
	procGetWindowRect.Call(windowHandle.Load(), uintptr(unsafe.Pointer(&rect)))
	scale := getWindowDpiScale()
	if scale <= 0 {
		scale = 1.0
	}
	toLogical := func(v int32) int32 { return int32(math.Round(float64(v) / scale)) }
	return toLogical(rect.Left), toLogical(rect.Top), toLogical(rect.Right), toLogical(rect.Bottom)
}

// viewModeSwitching は、表示モードの切替（フル ⇄ コンパクト）の途中で true になる。
// 切替ボタンの押下は、ドラッグの mouseup で /api/persistwindow を送りつつ /api/setviewmode も送るため、
// 2 つのリクエストが並走する。モードを切り替えた直後・ウィンドウを動かす前に保存が走ると、
// 切替前のウィンドウの右下が新しいモードのスロットに書かれて、「前回の位置に戻る」が戻らなくなる。
// 切替中は保存を見送る（フロントは切替の 300ms 後にもう一度保存を呼ぶ）。ロックにしないのは、
// ズーム適用が UI スレッドで保存を呼ぶため（切替側の SetWindowPos は UI スレッドの応答を待つので、
// ロックの取り合いでデッドロックしうる）。
var viewModeSwitching atomic.Bool

// persistCurrentWindowGeometry は現在のウィンドウ位置・サイズを config に書き込む。
func persistCurrentWindowGeometry() {
	// 起動直後は go-webview2 の初期化が幅と位置をリセットしており、復元が済む前に保存すると
	// 前回の記憶を既定位置で上書きしてしまう。500ms 後の再適用が済むまでは保存しない。
	if windowHandle.Load() == 0 || !startupGeometrySettled.Load() || viewModeSwitching.Load() {
		return
	}
	logicalX, logicalY, right, bottom := getWindowLogicalRect()
	wid, hei := getWindowSize()
	// ドラッグで動かした位置を現在のモードのスロットにも反映する。
	// configMu の内側で viewModeMu を取らないよう、先に読んでおく。
	compact := layoutIsCompact()
	// ウィンドウとモードを読んでいる間に切替が始まっていたら、読んだ組み合わせは食い違いうる
	if viewModeSwitching.Load() {
		return
	}
	mutateConfig(func(c *Config) {
		c.Window.X = logicalX
		c.Window.Y = logicalY
		c.Window.Width = wid
		c.Window.Height = hei
		c.Window.Saved = true
		setModePosition(c, compact, right, bottom)
	})
}

// setModePosition は右下の論理座標をコンパクト / フルいずれかのスロットへ書き込む。
// mutateConfig の中から呼ぶこと。
func setModePosition(c *Config, compact bool, right, bottom int32) {
	if compact {
		c.Window.CompactRight, c.Window.CompactBottom, c.Window.CompactPosSaved = right, bottom, true
		return
	}
	c.Window.NormalRight, c.Window.NormalBottom, c.Window.NormalPosSaved = right, bottom, true
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

	setRoundedCorners(hwnd)
}

// setRoundedCorners はウィンドウの角を丸める。枠なしウィンドウなので HTML 側の
// border-radius では角の外側にコンテナの背景が残ってしまう。DWM にクリップさせると
// アンチエイリアスが効き、リサイズにも自動で追従する。
// 角丸の指定は SetWindowLong によるスタイル変更で解除されるため、フレームを
// 外したあとに呼ぶこと。
func setRoundedCorners(hwnd uintptr) {
	if hwnd == 0 {
		return
	}
	pref := int32(DWMWCP_ROUNDSMALL)
	procDwmSetWindowAttribute.Call(hwnd, DWMWA_WINDOW_CORNER_PREFERENCE,
		uintptr(unsafe.Pointer(&pref)), unsafe.Sizeof(pref))
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
	mutexName, _ := syscall.UTF16PtrFromString(singleInstanceMutexName)
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
	defer reportPanic()

	windowTitle := applicationWindowTitle
	if !ensureSingleInstance(windowTitle) {
		return
	}

	// WebView2 のユーザーデータを固定場所に保存する。
	// 既定の挙動 (%APPDATA%\{exe 名}) に依存すると、ビルド名変更やカレントディレクトリ違いで
	// localStorage が別プロファイル扱いになる。明示指定で設定の記憶を確実にする。
	appRoot := appConfigDirectory()
	appDataRoot = appRoot
	dataPath := filepath.Join(appRoot, "WebView2")
	_ = os.MkdirAll(dataPath, 0755)

	// 設定ファイルを読み込み、永続化された設定を適用する。
	// 以降の状態変更は即ファイルに書き込むので、os.Exit で強制終了されても記憶が残る。
	configPath = filepath.Join(appRoot, "config.json")
	initAppLog(appRoot)
	redirectStdLog()
	initCrashLog(appRoot)
	loadConfig()
	c := snapshotConfig()
	transparentMu.Lock()
	transparent = c.Transparent
	transparentMu.Unlock()
	alwaysOnTopMu.Lock()
	alwaysOnTop = c.Topmost
	alwaysOnTopMu.Unlock()
	// 前回終了したときのモード（フル / コンパクト）で起動する。トレイ格納中に終了しても、
	// 保存されているのは格納前のモード（/api/setviewmode は 0 と 1 しか保存しない）。
	startMode := normalizeViewMode(c.ViewMode)
	viewModeMu.Lock()
	viewMode = startMode
	viewModeBeforeTray = startMode
	viewModeMu.Unlock()
	trayDisplayMu.Lock()
	switch c.TrayDisplay {
	case "cpu":
		trayDisplayMode = TRAY_DISPLAY_CPU
	case "usage":
		trayDisplayMode = TRAY_DISPLAY_USAGE
	default:
		trayDisplayMode = TRAY_DISPLAY_ICON
	}
	trayDisplayMu.Unlock()
	showGPU.Store(c.ShowGPU)
	showDisk.Store(!c.Hidden.Disk)

	apiToken := generateToken()
	pageAccessKey = generateToken() // ページ用のキー。API トークンとは別（HTML を取れる者にだけトークンを渡すため）
	ln, port := listenLocal()
	serverPort = port
	webviewDataPath = dataPath
	startCollector()
	go startServer(ln, apiToken)
	// ポモドーロの周期処理。最初の周期で、前回の異常終了で隠れたままのものを戻す。
	go pomodoroLoop()
	// Ping 監視の周期処理。表示中かつホスト登録時のみ動作。
	go pingLoop()
	// ログオフ・シャットダウンの前に、隠したものと止めた通知を戻す。
	startSessionWatcher()

	// IME Composition Window の左上吸い込み現象を WinEvent Hook で監視・自動復旧する。
	// 内部で goroutine + runtime.LockOSThread + GetMessageW ループを起動するので、
	// ここではブロッキングしない（フックの登録を待つだけ）。設定画面の切替と同じ関数を通す。
	if c.IME.Enabled {
		if err := applyIMEWatch(c.IME); err != nil {
			appLogf("IME 自動復旧の監視を開始できませんでした: %v", err)
		}
	}

	// WebView2 は最初のページを描画するまで、既定の背景（白）を画面に出す。起動直後に白い四角が
	// 一瞬見えるので、既定背景をウィジェットの背景色に近い色（テーマに合わせて暗色／明色）にしておく。
	// WebView2 の環境変数（ARGB の 16 進）で、ブラウザプロセスの起動前に設定する必要がある。
	// 利用者が自分で指定している場合は、それを尊重する。
	if os.Getenv("WEBVIEW2_DEFAULT_BACKGROUND_COLOR") == "" {
		_ = os.Setenv("WEBVIEW2_DEFAULT_BACKGROUND_COLOR",
			widgetStartupBackground(c.Theme, c.Skin, systemUsesLightTheme()).argbHex())
	}

	// ウィンドウは非表示で作る（go-webview2 は既定で作成直後に表示し、位置・中身が決まる前の
	// 白や中央の窓が見えてしまう）。位置とフレームを整え、フロントの準備完了（/api/ready）か
	// フォールバックのタイマーで、完成した状態を 1 回だけ表示する。
	w := webview2.NewWithOptions(webview2.WebViewOptions{
		Debug:     false,
		AutoFocus: true,
		DataPath:  dataPath,
		// 拡大・縮小は自前（zoom.go）で行う。WebView2 既定の Ctrl+ホイール / Ctrl+プラス・マイナスは、
		// ウィンドウを変えずにページだけを拡大縮小して、下に空白ができてしまうので無効にする。
		DisableZoomControl: true,
		WindowOptions: webview2.WindowOptions{
			Title:  windowTitle,
			Width:  windowWidth,
			Height: windowHeight,
			Center: true,
			Hidden: true,
		},
	})
	if w == nil {
		fatalf("画面を作成できませんでした。Microsoft Edge WebView2 Runtime がインストールされているか確認してください。")
	}
	defer w.Destroy()

	// go-webview2 は CreateWindowExW を NewWithOptions 内で同期的に実行済みなので、
	// w.Window() でメインウィンドウの HWND を即座に取得できる。旧実装の FindWindow
	// ポーリング（タイトル文字列依存・最大5秒待ち）は不要。ここでハンドルを確定させ、
	// フレーム除去・位置復元・前面表示を適用する。
	windowHandle.Store(uintptr(w.Window()))
	removeWindowFrame(windowHandle.Load())
	// 保存された拡大率を、最初のサイズ決めと表示より前に適用する（100% の画面が一瞬見えないように）。
	// ここは UI スレッド（Run の前）なので、ZoomFactor をそのまま設定できる。
	widgetZoom.Store(int32(normalizeZoom(c.Zoom)))
	if err := w.SetZoomFactor(zoomFactor()); err != nil {
		aiusage.Logf("拡大率の適用に失敗しました: %v", err)
	}
	applyStartupGeometry(true)
	// 位置が決まったので、完全な透明のまま表示状態にして描画させる（見せるのは /api/ready か
	// フォールバックのタイマー）。
	beginMainWindowStealth()
	// go-webview2 の SetSize(HintFixed) が後段で幅と位置をリセットするため、
	// 初期化が落ち着いたあとに再適用する。このときフロント側は既に実測値で
	// 高さを詰めているので、高さには触れない。
	time.AfterFunc(500*time.Millisecond, func() {
		applyStartupGeometry(false)
		startupGeometrySettled.Store(true)
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
	// 設定ファイルを読めなかったときは、トレイのアイコンが出たあとで 1 回だけ知らせる。
	if notice := configLoadNotice; notice != "" {
		time.AfterFunc(3*time.Second, func() { showBalloon("システムモニター", notice, aiusage.NIIF_WARNING) })
	}
	// Alt+F4 などで w.Run() を抜けた場合に通知領域へアイコンを残さない。
	defer cleanupTray()
	// 同じく、隠したままの Teams / Outlook や、止めたままの通知を残さない。
	defer pomodoroShutdown()

	// AI 使用量（aiusage）を始める。メイン WebView を登録してから呼ぶ（補助 WebView の
	// 生成・Eval は、すべてメイン WebView の Dispatch 経由で UI スレッドに寄せられる）。
	// 設定が OFF（既定）のうちは、認証 WebView2 を作らず、取得もしない。
	setMainWebView(w)
	aiusage.Start(aiHost{})

	// フロントの準備完了が来なくても、一定時間後には必ず表示する。
	time.AfterFunc(startupShowTimeout, func() { showMainWindowOnce("timeout") })

	w.SetSize(windowWidth, windowHeight, webview2.HintFixed)
	w.Navigate(pageURL(port, "/"))

	w.Run()
}
