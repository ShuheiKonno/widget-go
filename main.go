package main

import (
	"crypto/rand"
	"crypto/subtle"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
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
	procRegisterWindowMessage = user32.NewProc("RegisterWindowMessageW")
	procCreateWindowEx  = user32.NewProc("CreateWindowExW")
	procDefWindowProc   = user32.NewProc("DefWindowProcW")
	procGetMessage      = user32.NewProc("GetMessageW")
	procTranslateMessage = user32.NewProc("TranslateMessage")
	procDispatchMessage  = user32.NewProc("DispatchMessageW")
	procPostQuitMessage  = user32.NewProc("PostQuitMessage")
	procLoadIcon         = user32.NewProc("LoadIconW")
	procLoadImageW       = user32.NewProc("LoadImageW")
	procGetSystemMetrics = user32.NewProc("GetSystemMetrics")

	shell32             = syscall.NewLazyDLL("shell32.dll")
	procShellNotifyIcon = shell32.NewProc("Shell_NotifyIconW")

	kernel32            = syscall.NewLazyDLL("kernel32.dll")
	procGetModuleHandle = kernel32.NewProc("GetModuleHandleW")
	procCreateMutexW    = kernel32.NewProc("CreateMutexW")

	procSetForegroundWindow = user32.NewProc("SetForegroundWindow")
	procSetLayeredWindowAttributes = user32.NewProc("SetLayeredWindowAttributes")
	procMonitorFromWindow = user32.NewProc("MonitorFromWindow")
	procGetMonitorInfoW   = user32.NewProc("GetMonitorInfoW")

	gdi32                = syscall.NewLazyDLL("gdi32.dll")
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

	MF_STRING        = 0x00000000
	TPM_RIGHTBUTTON  = 0x0002
	TPM_RETURNCMD    = 0x0100

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

	TRANSPARENT = 1
	BI_RGB      = 0
	DIB_RGB_COLORS = 0

	windowWidth  = 210
	windowHeight = 400
)

// windowHandle は WebView2 のメインウィンドウ HWND。
// startServer の HTTP ハンドラ goroutine 群と main の初期化から並行アクセスされるため
// atomic で保護する（-race 検出を含むデータ競合の回避）。
var windowHandle atomic.Uintptr

// Config はプロセスをまたいで永続化される設定。
// WebView2 の localStorage は os.Exit(0) で flush されずに失われる可能性があるため、
// Go 側で JSON ファイルに保存する。
type Config struct {
	Window struct {
		X      int32 `json:"x"`
		Y      int32 `json:"y"`
		Width  int32 `json:"width"`
		Height int32 `json:"height"`
		Saved  bool  `json:"saved"`
	} `json:"window"`
	Topmost     bool   `json:"topmost"`
	ViewMode    int    `json:"viewMode"`
	Transparent bool   `json:"transparent"`
	TrayDisplay string `json:"trayDisplay"` // "icon" | "cpu"
	Theme       string `json:"theme"`       // "dark" | "light" | "system"
	CoreBase    int    `json:"coreBase"`    // 0 | 1
	NetUnit     string `json:"netUnit"`     // "byte" | "bit"
	IME         IMEConfig `json:"ime"`
}

// IMEConfig は IME Composition Window 検知・自動復旧機能の設定。
type IMEConfig struct {
	Enabled     bool  `json:"enabled"`
	CooldownSec int   `json:"cooldownSec"`
	ThresholdPx int32 `json:"thresholdPx"`
	DryRun      bool  `json:"dryRun"`
}

var (
	configMu   sync.Mutex
	configPath string
	config     Config
)

func defaultConfig() Config {
	var c Config
	c.TrayDisplay = "icon"
	c.Theme = "system"
	c.CoreBase = 0
	c.NetUnit = "byte"
	c.IME = IMEConfig{
		Enabled:     true,
		CooldownSec: 2,
		ThresholdPx: 5,
		DryRun:      false,
	}
	return c
}

func loadConfig() {
	configMu.Lock()
	defer configMu.Unlock()
	config = defaultConfig()
	if configPath == "" {
		return
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		return
	}
	// デフォルト値を上書きする形で JSON を読み込む
	tmp := config
	if err := json.Unmarshal(data, &tmp); err != nil {
		return
	}
	config = tmp
}

// writeConfigLocked は config を JSON でアトミック（tmp+rename）に書き出す。
// configMu を保持した状態で呼ぶこと。マーシャルと書き込みをロック内で一体化することで、
// 並行 mutateConfig 呼び出しによる lost update（古いスナップショットの後勝ち）を防ぐ。
func writeConfigLocked() {
	if configPath == "" {
		return
	}
	data, err := json.MarshalIndent(&config, "", "  ")
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(configPath), 0755)
	tmp := configPath + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return
	}
	_ = os.Rename(tmp, configPath)
}

// mutateConfig は config をロックしたうえで変更関数を適用し、ロックを保持したまま保存する。
func mutateConfig(f func(c *Config)) {
	configMu.Lock()
	defer configMu.Unlock()
	f(&config)
	writeConfigLocked()
}

func snapshotConfig() Config {
	configMu.Lock()
	defer configMu.Unlock()
	return config
}

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
	Memory   *sysinfo.MemoryInfo   `json:"Memory"`
	WiFi     *sysinfo.WiFiInfo     `json:"WiFi"`
	Ethernet *sysinfo.EthernetInfo `json:"Ethernet"`
	Network  *sysinfo.NetworkInfo  `json:"Network"`
	ViewMode int                   `json:"ViewMode"`
}

// バックグラウンドで更新されるスナップショット。
// /api/system はこのキャッシュを読むだけで、その場での計測は行わない。
var (
	cacheMu        sync.RWMutex
	cachedCPU      *sysinfo.CPUInfo
	cachedMem      *sysinfo.MemoryInfo
	cachedWiFi     *sysinfo.WiFiInfo
	cachedEthernet *sysinfo.EthernetInfo
	cachedNet      *sysinfo.NetworkInfo
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
		HInstance:      hInstance,
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
		Ethernet: cachedEthernet,
		Network:  cachedNet,
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

	cacheMu.Lock()
	if cpuErr == nil {
		cachedCPU = cpuInfo
	}
	if memErr == nil {
		cachedMem = memInfo
	}
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
// キャッシュを更新する。CPU/メモリは 2 秒ごと、Wi-Fi は 3 秒ごと。
// （フロア移動などで AP が切り替わったときに UI が「未接続」のまま固まるのを
//   防ぐため、Wi-Fi の検出間隔は短めに保つ。netsh は 100ms 程度で完了する）。
func startCollector() {
	sysinfo.PrimeCPU()
	refreshCPUAndMem()
	refreshWiFi()
	refreshEthernet()
	// ネットワーク計測の基準値を仕込む（初回は差分計算できないので 0 を返す）
	refreshNetwork()

	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			refreshCPUAndMem()
			refreshNetwork()
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

	var rect RECT
	procGetWindowRect.Call(windowHandle.Load(), uintptr(unsafe.Pointer(&rect)))
	newLeft := rect.Left
	newTop := rect.Bottom - scaledH // 既定: 下端固定
	if anchorTop {
		newTop = rect.Top // 上端固定（下端エッジのドラッグ時）
	}
	procSetWindowPos.Call(windowHandle.Load(), 0, uintptr(newLeft), uintptr(newTop), uintptr(scaledW), uintptr(scaledH), SWP_NOZORDER)
}

// generateToken は起動毎のランダムなセッショントークンを生成する。
// フロントに埋め込み、全 /api 呼び出しのカスタムヘッダ検証に使う（CSRF 対策）。
func generateToken() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// 生成失敗はほぼ起こらないが、起きた場合は fail-closed（安全側）で停止する。
		log.Fatalf("セキュリティトークンの生成に失敗: %v", err)
	}
	return hex.EncodeToString(b)
}

// securityMiddleware はローカル HTTP API をブラウザ由来の攻撃から守る多層防御を適用する。
//   - Host 検証: 127.0.0.1:<port> 以外を 403（DNS rebinding 対策）
//   - "/"（UI）: トークン不要。CSP と nosniff を付与してここでトークンを配布する
//   - "/api/*": X-Widget-Token カスタムヘッダを検証（クロスサイトの単純リクエストは
//     カスタムヘッダを付けられず、付ければ preflight が発生して失敗するため CSRF を防ぐ）
func securityMiddleware(next http.Handler, token string, port int) http.Handler {
	expectedHost := fmt.Sprintf("127.0.0.1:%d", port)
	scriptCSP := "default-src 'none'; " +
		"script-src 'nonce-" + token + "'; " +
		"style-src 'unsafe-inline'; " +
		"connect-src 'self'; " +
		"img-src 'self' data:"
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != expectedHost {
			http.Error(w, "forbidden host", http.StatusForbidden)
			return
		}
		if r.URL.Path == "/" {
			w.Header().Set("Content-Security-Policy", scriptCSP)
			w.Header().Set("X-Content-Type-Options", "nosniff")
			next.ServeHTTP(w, r)
			return
		}
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Widget-Token")), []byte(token)) != 1 {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func startServer(ln net.Listener, token string) {
	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(getHTML(token)))
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
		mutateConfig(func(c *Config) { c.ViewMode = m })
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
		mutateConfig(func(c *Config) { c.Topmost = current })
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"topmost": current})
	})

	// 常に前面表示を明示設定する (enable=true|false)
	mux.HandleFunc("/api/topmost", func(w http.ResponseWriter, r *http.Request) {
		v := r.URL.Query().Get("enable")
		enable := v == "true" || v == "1"
		alwaysOnTopMu.Lock()
		alwaysOnTop = enable
		alwaysOnTopMu.Unlock()
		setTopmost(enable)
		mutateConfig(func(c *Config) { c.Topmost = enable })
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"topmost": enable})
	})

	// ウィンドウ位置とサイズ (論理ピクセル) をまとめて返す
	mux.HandleFunc("/api/windowgeometry", func(w http.ResponseWriter, r *http.Request) {
		var x, y int32
		if windowHandle.Load() != 0 {
			var rect RECT
			procGetWindowRect.Call(windowHandle.Load(), uintptr(unsafe.Pointer(&rect)))
			scale := getWindowDpiScale()
			if scale <= 0 {
				scale = 1.0
			}
			x = int32(math.Round(float64(rect.Left) / scale))
			y = int32(math.Round(float64(rect.Top) / scale))
		}
		wid, hei := getWindowSize()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]int32{
			"x":      x,
			"y":      y,
			"width":  wid,
			"height": hei,
		})
	})

	// ウィンドウ位置とサイズを明示的に設定する (論理ピクセル)
	mux.HandleFunc("/api/setwindow", func(w http.ResponseWriter, r *http.Request) {
		if windowHandle.Load() == 0 {
			http.Error(w, "window not ready", http.StatusServiceUnavailable)
			return
		}
		q := r.URL.Query()
		xVal, xErr := strconv.Atoi(q.Get("x"))
		yVal, yErr := strconv.Atoi(q.Get("y"))
		wVal, _ := strconv.Atoi(q.Get("w"))
		hVal, _ := strconv.Atoi(q.Get("h"))

		if wVal > 0 && hVal > 0 {
			resizeWindow(int32(wVal), int32(hVal), false)
		}
		if xErr == nil && yErr == nil {
			scale := getWindowDpiScale()
			if scale <= 0 {
				scale = 1.0
			}
			// 画面外に完全に出ないよう、アクティブモニタの作業領域でクランプする
			hmon, _, _ := procMonitorFromWindow.Call(windowHandle.Load(), MONITOR_DEFAULTTONEAREST)
			if hmon != 0 {
				var mi MONITORINFO
				mi.CbSize = uint32(unsafe.Sizeof(mi))
				if ok, _, _ := procGetMonitorInfoW.Call(hmon, uintptr(unsafe.Pointer(&mi))); ok != 0 {
					var rect RECT
					procGetWindowRect.Call(windowHandle.Load(), uintptr(unsafe.Pointer(&rect)))
					ww := rect.Right - rect.Left
					hh := rect.Bottom - rect.Top
					physX := int32(math.Round(float64(xVal) * scale))
					physY := int32(math.Round(float64(yVal) * scale))
					minX := mi.RcWork.Left
					minY := mi.RcWork.Top
					maxX := mi.RcWork.Right - ww
					maxY := mi.RcWork.Bottom - hh
					if physX < minX {
						physX = minX
					}
					if physX > maxX {
						physX = maxX
					}
					if physY < minY {
						physY = minY
					}
					if physY > maxY {
						physY = maxY
					}
					moveWindow(physX, physY)
					persistCurrentWindowGeometry()
					w.WriteHeader(http.StatusOK)
					return
				}
			}
			// モニタ情報が取れない場合はそのまま適用
			physX := int32(math.Round(float64(xVal) * scale))
			physY := int32(math.Round(float64(yVal) * scale))
			moveWindow(physX, physY)
		}
		persistCurrentWindowGeometry()
		w.WriteHeader(http.StatusOK)
	})

	// 透過モードトグル (旧 UI 互換)。現行 UI は /api/transparent?enable= を使う。
	mux.HandleFunc("/api/toggletransparent", func(w http.ResponseWriter, r *http.Request) {
		transparentMu.Lock()
		transparent = !transparent
		current := transparent
		transparentMu.Unlock()
		setTransparent(current)
		mutateConfig(func(c *Config) { c.Transparent = current })
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"transparent": current})
	})

	// 透過モードを明示的に設定する (enable=true|false)
	mux.HandleFunc("/api/transparent", func(w http.ResponseWriter, r *http.Request) {
		v := r.URL.Query().Get("enable")
		enable := v == "true" || v == "1"
		transparentMu.Lock()
		transparent = enable
		transparentMu.Unlock()
		setTransparent(enable)
		mutateConfig(func(c *Config) { c.Transparent = enable })
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"transparent": enable})
	})

	// トレイ表示モード (icon|cpu) を設定する
	mux.HandleFunc("/api/traydisplay", func(w http.ResponseWriter, r *http.Request) {
		modeStr := r.URL.Query().Get("mode")
		var m int
		var label string
		switch modeStr {
		case "icon", "0":
			m = TRAY_DISPLAY_ICON
			label = "icon"
		case "cpu", "1":
			m = TRAY_DISPLAY_CPU
			label = "cpu"
		default:
			http.Error(w, "invalid mode", http.StatusBadRequest)
			return
		}
		trayDisplayMu.Lock()
		trayDisplayMode = m
		trayDisplayMu.Unlock()
		mutateConfig(func(c *Config) { c.TrayDisplay = label })
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]int{"trayDisplay": m})
	})

	// theme と coreBase など Go 側の挙動に影響しないフロント向け設定を保存する
	mux.HandleFunc("/api/setoption", func(w http.ResponseWriter, r *http.Request) {
		key := r.URL.Query().Get("key")
		val := r.URL.Query().Get("value")
		switch key {
		case "theme":
			if val != "dark" && val != "light" && val != "system" {
				http.Error(w, "invalid theme", http.StatusBadRequest)
				return
			}
			mutateConfig(func(c *Config) { c.Theme = val })
		case "coreBase":
			n, err := strconv.Atoi(val)
			if err != nil || (n != 0 && n != 1) {
				http.Error(w, "invalid coreBase", http.StatusBadRequest)
				return
			}
			mutateConfig(func(c *Config) { c.CoreBase = n })
		case "netUnit":
			if val != "byte" && val != "bit" {
				http.Error(w, "invalid netUnit", http.StatusBadRequest)
				return
			}
			mutateConfig(func(c *Config) { c.NetUnit = val })
		default:
			http.Error(w, "unknown key", http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	// 現在のサーバー側設定を一括取得する (フロント側の初期化で使用)
	mux.HandleFunc("/api/settings", func(w http.ResponseWriter, r *http.Request) {
		c := snapshotConfig()
		viewModeMu.RLock()
		vm := viewMode
		viewModeMu.RUnlock()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"transparent": c.Transparent,
			"topmost":     c.Topmost,
			"trayDisplay": c.TrayDisplay,
			"theme":       c.Theme,
			"coreBase":    c.CoreBase,
			"netUnit":     c.NetUnit,
			"viewMode":    vm,
		})
	})

	// リサイズ実行。mousemove 中に連続して呼ばれるので、ここでは永続化しない。
	// 確定タイミング (mouseup) で /api/persistwindow が呼ばれる。
	mux.HandleFunc("/api/resize", func(w http.ResponseWriter, r *http.Request) {
		wStr := r.URL.Query().Get("w")
		hStr := r.URL.Query().Get("h")
		wVal, _ := strconv.Atoi(wStr)
		h, _ := strconv.Atoi(hStr)
		anchorTop := r.URL.Query().Get("anchor") == "top"
		resizeWindow(int32(wVal), int32(h), anchorTop)
		w.WriteHeader(http.StatusOK)
	})

	// フロント側のドラッグ/リサイズ確定時に現在位置を config に記録する
	mux.HandleFunc("/api/persistwindow", func(w http.ResponseWriter, r *http.Request) {
		persistCurrentWindowGeometry()
		w.WriteHeader(http.StatusOK)
	})

	port := ln.Addr().(*net.TCPAddr).Port
	if err := http.Serve(ln, securityMiddleware(mux, token, port)); err != nil {
		// http.Serve は正常時は戻らない。戻った＝致命的（リスナー閉塞など）。
		log.Fatalf("HTTP サーバーが停止しました: %v", err)
	}
}

// listenLocal は 127.0.0.1 の空きポートでリスナーを確保して返す。
// 旧 getAvailablePort のように一度 Close して再 Listen すると、その隙間に別プロセスへ
// ポートを奪われる TOCTOU があった。確保したリスナーをそのまま Serve に渡すことで回避する。
func listenLocal() (net.Listener, int) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatalf("ローカルポートの確保に失敗: %v", err)
	}
	return ln, ln.Addr().(*net.TCPAddr).Port
}

// restoreWindowGeometry は永続化された位置・サイズを現在のウィンドウに適用する。
// 保存値が無い場合や windowHandle 未取得時は false を返し、呼び出し元はフォールバックを選ぶ。
// 論理ピクセル値で保存されているので DPI スケール補正を行う。
func restoreWindowGeometry() bool {
	if windowHandle.Load() == 0 {
		return false
	}
	c := snapshotConfig()
	if !c.Window.Saved || c.Window.Width <= 0 || c.Window.Height <= 0 {
		return false
	}
	resizeWindow(c.Window.Width, c.Window.Height, false)

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
// 主経路では main が w.Window() で直接ハンドルを取得するため、これは enterTray の
// フォールバック（ハンドル未取得時の復旧）専用。成功時 true。
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
	if c.ViewMode == 0 || c.ViewMode == 1 {
		viewMode = c.ViewMode
	}
	viewModeMu.Unlock()
	trayDisplayMu.Lock()
	if c.TrayDisplay == "cpu" {
		trayDisplayMode = TRAY_DISPLAY_CPU
	} else {
		trayDisplayMode = TRAY_DISPLAY_ICON
	}
	trayDisplayMu.Unlock()

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
		if !restoreWindowGeometry() {
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

	w.SetSize(windowWidth, windowHeight, webview2.HintFixed)
	w.Navigate(fmt.Sprintf("http://127.0.0.1:%d", port))

	w.Run()
}

//go:embed ui/index.html
var indexHTML string

// getHTML は埋め込み済みの UI HTML を返す。__WIDGET_TOKEN__ を起動毎の
// セッショントークンに差し替える（<script> の CSP nonce と JS の WIDGET_TOKEN 定数）。
func getHTML(token string) string {
	return strings.ReplaceAll(indexHTML, "__WIDGET_TOKEN__", token)
}
