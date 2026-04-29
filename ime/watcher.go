//go:build windows

// Package ime は MS IME の Composition Window（合成ウィンドウ）が画面左上に
// 出現する不具合を WinEvent Hook で検知し、自動復旧するためのパッケージである。
package ime

import (
	"context"
	"log"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

// IME 関連ウィンドウのクラス名フィルタ。
var imeClassNames = map[string]bool{
	"MSCTFIME UI": true,
	"IME":         true,
	"IMEUI":       true,
}

// WinEvent 定数（Windows SDK の WinUser.h より）。
const (
	eventObjectCreate         = 0x8000
	eventObjectShow           = 0x8002
	eventObjectLocationChange = 0x800B
	wineventOutOfContext      = 0x0000
)

// RECT は Win32 API の RECT。main.go の同名型と衝突しないよう ime パッケージ内で再定義する。
type RECT struct {
	Left, Top, Right, Bottom int32
}

// DetectEvent は左上 IME ウィンドウ検知通知。WinEvent コールバックは重い処理を
// 行ってはいけないため、この値だけ非ブロッキングで Recoverer に送る。
type DetectEvent struct {
	HWND       uintptr
	ClassName  string
	Rect       RECT
	DetectedAt time.Time
}

// Config は IME 監視の動作パラメータ。main.go 側の Config から渡される。
type Config struct {
	Enabled     bool
	CooldownSec int
	ThresholdPx int32
	DryRun      bool
}

// DefaultConfig は config.json に未設定の場合に使う既定値。
func DefaultConfig() Config {
	return Config{
		Enabled:     true,
		CooldownSec: 2,
		ThresholdPx: 5,
		DryRun:      false,
	}
}

// パッケージ専用の DLL ハンドル。main.go の user32 と分離してパッケージ独立性を確保する。
var (
	imeUser32 = syscall.NewLazyDLL("user32.dll")

	procSetWinEventHook = imeUser32.NewProc("SetWinEventHook")
	procUnhookWinEvent  = imeUser32.NewProc("UnhookWinEvent")
	procGetMessageW     = imeUser32.NewProc("GetMessageW")
	procTranslateMsg    = imeUser32.NewProc("TranslateMessage")
	procDispatchMsgW    = imeUser32.NewProc("DispatchMessageW")
	procGetClassNameW   = imeUser32.NewProc("GetClassNameW")
	procGetWindowRect   = imeUser32.NewProc("GetWindowRect")
	procIsWindowVisible = imeUser32.NewProc("IsWindowVisible")
	procEnumWindows     = imeUser32.NewProc("EnumWindows")
)

// winEventCallback は SetWinEventHook に渡したコールバック関数の uintptr。
// syscall.NewCallback の戻り値は GC 対象から外すためにパッケージレベルで保持する
// （main.go:368 trayWndProc と同パターン）。
var winEventCallback uintptr

// enumIMEProcCallback も同様の理由でパッケージレベルに保持する。
var enumIMEProcCallback uintptr

// detectChan はコールバック → Recoverer の通知用チャネル。バッファ付きにして
// コールバック内で送信が詰まらないようにする。
var detectChan chan DetectEvent

// activeThreshold は WinEvent コールバックが参照する閾値。コールバック内で
// 動的に Config を渡せないためパッケージレベルに置く。
var (
	thresholdMu     sync.RWMutex
	activeThreshold int32 = 5
)

// msg は GetMessageW が返す MSG 構造体。
type msg struct {
	HWnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      struct{ X, Y int32 }
}

// StartWatcher は IME 監視を開始する。内部で goroutine 2本（WinEvent ループと
// Recoverer ループ）を起動し、ブロッキングしない。失敗時はログ出力して return する。
//
// cfg.Enabled が false の場合は何もせず return する（main.go 側で防御するが二重の安全策）。
func StartWatcher(cfg Config) {
	if !cfg.Enabled {
		return
	}
	if cfg.CooldownSec <= 0 {
		cfg.CooldownSec = 2
	}
	if cfg.ThresholdPx <= 0 {
		cfg.ThresholdPx = 5
	}

	thresholdMu.Lock()
	activeThreshold = cfg.ThresholdPx
	thresholdMu.Unlock()

	detectChan = make(chan DetectEvent, 16)

	// Recoverer を別 goroutine で先に起動。ctx は本プロセスが落ちるまで使い続けるので
	// 親プロセス終了時に goroutine ごと回収される前提で context.Background() を渡す。
	rec := &Recoverer{
		Cooldown:      time.Duration(cfg.CooldownSec) * time.Second,
		Threshold:     cfg.ThresholdPx,
		DryRun:        cfg.DryRun,
		InputProvider: WinLastInputInfo{},
	}
	go rec.Run(context.Background(), detectChan)

	// WinEvent Hook は SetWinEventHook を呼んだスレッドのキューにイベントが配送される
	// （WINEVENT_OUTOFCONTEXT モード）。Go ランタイムにスレッドを切り替えられると
	// イベントを受け取れなくなるので runtime.LockOSThread で固定する
	// （main.go:767-804 のトレイメッセージループと同パターン）。
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		winEventCallback = syscall.NewCallback(winEventProc)

		// 1度に2つのイベント範囲をフックする。CREATE/SHOW と LOCATIONCHANGE は
		// 連続値ではないので2回に分けて登録する。
		hook1, _, _ := procSetWinEventHook.Call(
			uintptr(eventObjectCreate),
			uintptr(eventObjectShow),
			0,
			winEventCallback,
			0, 0,
			uintptr(wineventOutOfContext),
		)
		hook2, _, _ := procSetWinEventHook.Call(
			uintptr(eventObjectLocationChange),
			uintptr(eventObjectLocationChange),
			0,
			winEventCallback,
			0, 0,
			uintptr(wineventOutOfContext),
		)
		if hook1 == 0 && hook2 == 0 {
			log.Printf("[%s] IME watcher: SetWinEventHook が両方失敗。監視を停止", time.Now().Format("15:04:05"))
			return
		}
		defer func() {
			if hook1 != 0 {
				procUnhookWinEvent.Call(hook1)
			}
			if hook2 != 0 {
				procUnhookWinEvent.Call(hook2)
			}
		}()

		log.Printf("[%s] IME watcher 起動: threshold=%dpx cooldown=%ds dryRun=%v",
			time.Now().Format("15:04:05"), cfg.ThresholdPx, cfg.CooldownSec, cfg.DryRun)

		// メッセージループ。WinEvent が配送されると DispatchMessageW を経由して
		// winEventCallback が呼ばれる。
		var m msg
		for {
			ret, _, _ := procGetMessageW.Call(
				uintptr(unsafe.Pointer(&m)),
				0, 0, 0,
			)
			// GetMessageW は WM_QUIT で 0、エラー時は -1 を返す。
			// uintptr で受けると -1 は最大値になるので int32 へキャストして判定する。
			if int32(ret) <= 0 {
				return
			}
			procTranslateMsg.Call(uintptr(unsafe.Pointer(&m)))
			procDispatchMsgW.Call(uintptr(unsafe.Pointer(&m)))
		}
	}()
}

// winEventProc は SetWinEventHook のコールバック。
// 仕様 L98-114 に従い、判定とチャネル送信のみ行う（重い処理を行ってはいけない）。
func winEventProc(hook uintptr, event uint32, hwnd uintptr, idObject, idChild int32, threadID, eventTime uint32) uintptr {
	if hwnd == 0 {
		return 0
	}
	visibleRet, _, _ := procIsWindowVisible.Call(hwnd)
	if visibleRet == 0 {
		return 0
	}
	className := getClassName(hwnd)
	if !imeClassNames[className] {
		return 0
	}
	rect, ok := getWindowRect(hwnd)
	if !ok {
		return 0
	}

	thresholdMu.RLock()
	th := activeThreshold
	thresholdMu.RUnlock()

	if !isIMETarget(className, true, rect, th) {
		return 0
	}

	ev := DetectEvent{
		HWND:       hwnd,
		ClassName:  className,
		Rect:       rect,
		DetectedAt: time.Now(),
	}
	// 非ブロッキング送信。チャネルが詰まっていればイベントは捨てる
	// （Recoverer のクールダウン中は古い検知を取りこぼしても問題ない）。
	select {
	case detectChan <- ev:
	default:
	}
	return 0
}

// isIMETarget は IME 左上ボックスの判定を純粋関数として行う（テスト容易性のため）。
// 仕様 L87-94 の4条件を判定する。
func isIMETarget(className string, visible bool, rect RECT, threshold int32) bool {
	if !visible {
		return false
	}
	if !imeClassNames[className] {
		return false
	}
	if rect.Left > threshold || rect.Top > threshold {
		return false
	}
	if rect.Right-rect.Left <= 0 || rect.Bottom-rect.Top <= 0 {
		return false
	}
	return true
}

// getClassName は GetClassNameW で hwnd のクラス名を取得する。
func getClassName(hwnd uintptr) string {
	var buf [256]uint16
	n, _, _ := procGetClassNameW.Call(
		hwnd,
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(len(buf)),
	)
	if n == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf[:n])
}

// getWindowRect は GetWindowRect で hwnd の画面座標を取得する。
func getWindowRect(hwnd uintptr) (RECT, bool) {
	var r RECT
	ret, _, _ := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
	if ret == 0 {
		return r, false
	}
	return r, true
}

// findIMETopLeft は EnumWindows で IME クラス名のトップレベルウィンドウを列挙し、
// 左上判定該当を1件返す。Recoverer の解消確認用。
func findIMETopLeft(threshold int32) (RECT, uintptr, string, bool) {
	var (
		foundRect      RECT
		foundHWND      uintptr
		foundClass     string
		foundDetection bool
	)

	// EnumWindows のコールバックはここでローカル定義してクロージャに見せかけたいが、
	// syscall.NewCallback はトップレベル関数しか安全に登録できない（GC との関係）。
	// そのためパッケージレベルにバッファを置いて、コールバックから書き込む。
	enumMu.Lock()
	defer enumMu.Unlock()
	enumThreshold = threshold
	enumFound = false

	if enumIMEProcCallback == 0 {
		enumIMEProcCallback = syscall.NewCallback(enumIMEProc)
	}
	procEnumWindows.Call(enumIMEProcCallback, 0)

	if !enumFound {
		return foundRect, 0, "", false
	}
	foundRect = enumFoundRect
	foundHWND = enumFoundHWND
	foundClass = enumFoundClass
	foundDetection = true
	return foundRect, foundHWND, foundClass, foundDetection
}

// EnumWindows コールバック共有用パッケージ変数（enumMu で保護）。
var (
	enumMu         sync.Mutex
	enumThreshold  int32
	enumFound      bool
	enumFoundRect  RECT
	enumFoundHWND  uintptr
	enumFoundClass string
)

// enumIMEProc は EnumWindows のコールバック。1件見つけたら以降は false を返して打ち切る。
func enumIMEProc(hwnd uintptr, lparam uintptr) uintptr {
	if enumFound {
		return 0 // 列挙打ち切り
	}
	visibleRet, _, _ := procIsWindowVisible.Call(hwnd)
	if visibleRet == 0 {
		return 1
	}
	className := getClassName(hwnd)
	if !imeClassNames[className] {
		return 1
	}
	rect, ok := getWindowRect(hwnd)
	if !ok {
		return 1
	}
	if !isIMETarget(className, true, rect, enumThreshold) {
		return 1
	}
	enumFound = true
	enumFoundRect = rect
	enumFoundHWND = hwnd
	enumFoundClass = className
	return 0 // 打ち切り
}
