//go:build windows

package focus

import (
	"os"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

var (
	user32                       = windows.NewLazySystemDLL("user32.dll")
	procEnumWindows              = user32.NewProc("EnumWindows")
	procIsWindow                 = user32.NewProc("IsWindow")
	procIsWindowVisible          = user32.NewProc("IsWindowVisible")
	procIsIconic                 = user32.NewProc("IsIconic")
	procIsZoomed                 = user32.NewProc("IsZoomed")
	procGetWindow                = user32.NewProc("GetWindow")
	procGetWindowThreadProcessID = user32.NewProc("GetWindowThreadProcessId")
	procGetWindowLongW           = user32.NewProc("GetWindowLongW")
	procGetWindowRect            = user32.NewProc("GetWindowRect")
	procGetClassNameW            = user32.NewProc("GetClassNameW")
	procGetWindowTextW           = user32.NewProc("GetWindowTextW")
	procShowWindowAsync          = user32.NewProc("ShowWindowAsync")
)

const (
	gwOwner          = 4
	gwlExStyle       = ^uintptr(19) // GWL_EXSTYLE (-20)
	wsExToolWindow   = 0x00000080
	swHide           = 0
	swShowNoActivate = 4
	swShowMaximized  = 3
	swShowMinNoAct   = 7
)

type rect struct{ Left, Top, Right, Bottom int32 }

// EnumWindows のコールバックは、syscall.NewCallback がプロセスの存続中ずっと解放されず、数に上限がある
// （約 2000 個）ので、周期ごとに作らず、一度だけ作る。列挙の結果は、enumMu で守ったパッケージ変数に積む。
var (
	enumMu       sync.Mutex
	enumHandles  []uintptr
	enumCallback = syscall.NewCallback(enumProc)
)

func enumProc(hwnd, _ uintptr) uintptr {
	enumHandles = append(enumHandles, hwnd)
	return 1 // 続ける
}

// SystemWindows は実際の Win32 の実装。
type SystemWindows struct{}

func (SystemWindows) topLevel() []uintptr {
	enumMu.Lock()
	defer enumMu.Unlock()
	enumHandles = enumHandles[:0]
	procEnumWindows.Call(enumCallback, 0)
	out := make([]uintptr, len(enumHandles))
	copy(out, enumHandles)
	return out
}

// exeCache は 1 回の列挙の間だけ、プロセス ID から実行ファイル名への対応を覚える。
type exeCache map[uint32]string

func (c exeCache) name(pid uint32) string {
	if n, ok := c[pid]; ok {
		return n
	}
	n := processExe(pid)
	c[pid] = n
	return n
}

// processExe はプロセスの実行ファイルのベース名。開けない（管理者権限・別セッション・終了済み）ときは空。
func processExe(pid uint32) string {
	if pid == 0 {
		return ""
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, windows.MAX_PATH*2)
	n := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &n); err != nil {
		return ""
	}
	return NormalizeExe(windows.UTF16ToString(buf[:n]))
}

func isTrue(r uintptr) bool { return r != 0 }

func describe(hwnd uintptr, cache exeCache, withText bool) Window {
	w := Window{HWND: hwnd}
	r, _, _ := procIsWindowVisible.Call(hwnd)
	w.Visible = isTrue(r)
	r, _, _ = procIsIconic.Call(hwnd)
	w.Minimized = isTrue(r)
	r, _, _ = procIsZoomed.Call(hwnd)
	w.Maximized = isTrue(r)
	owner, _, _ := procGetWindow.Call(hwnd, gwOwner)
	w.HasOwner = owner != 0
	var pid uint32
	procGetWindowThreadProcessID.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	w.PID = pid
	w.Exe = cache.name(pid)
	ex, _, _ := procGetWindowLongW.Call(hwnd, gwlExStyle)
	w.ToolWindow = uint32(ex)&wsExToolWindow != 0
	var rc rect
	procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&rc)))
	w.Width, w.Height = rc.Right-rc.Left, rc.Bottom-rc.Top
	if withText {
		buf := make([]uint16, 256)
		n, _, _ := procGetClassNameW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
		w.Class = windows.UTF16ToString(buf[:n])
		n, _, _ = procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
		w.Title = windows.UTF16ToString(buf[:n])
	}
	return w
}

// Enumerate は最上位ウィンドウを列挙する。includeHidden が false のときは、可視で所有者のいないものだけを調べる
// （毎秒呼ぶので、関係ないウィンドウのプロセスを開かない）。
func (s SystemWindows) Enumerate(includeHidden bool) []Window {
	// 隠れたウィンドウも調べるとき（「表示を戻す」の候補選び）は、タイトルで補助ウィンドウを除くので、タイトルも取る
	return s.enumerate(includeHidden, includeHidden)
}

// EnumerateDetailed はクラス名とタイトルも付けて列挙する（調査用ツール cmd/focusprobe が使う）。
func (s SystemWindows) EnumerateDetailed(includeHidden bool) []Window {
	return s.enumerate(includeHidden, true)
}

func (s SystemWindows) enumerate(includeHidden, withText bool) []Window {
	cache := exeCache{}
	var out []Window
	for _, h := range s.topLevel() {
		if !includeHidden {
			if r, _, _ := procIsWindowVisible.Call(h); r == 0 {
				continue
			}
			if o, _, _ := procGetWindow.Call(h, gwOwner); o != 0 {
				continue
			}
		}
		out = append(out, describe(h, cache, withText))
	}
	return out
}

func (SystemWindows) Lookup(hwnd uintptr) (Window, bool) {
	if hwnd == 0 {
		return Window{}, false
	}
	if r, _, _ := procIsWindow.Call(hwnd); r == 0 {
		return Window{}, false
	}
	return describe(hwnd, exeCache{}, false), true
}

// ShowWindow は、相手のウィンドウが応答しないと、こちらも固まる。ShowWindowAsync は相手のスレッドへ
// メッセージを送るだけなので、固まらない。
func (SystemWindows) Hide(hwnd uintptr)     { procShowWindowAsync.Call(hwnd, swHide) }
func (SystemWindows) Minimize(hwnd uintptr) { procShowWindowAsync.Call(hwnd, swShowMinNoAct) }

func (SystemWindows) Show(hwnd uintptr, state string) {
	switch state {
	case StateMin:
		procShowWindowAsync.Call(hwnd, swShowMinNoAct)
	case StateMax:
		procShowWindowAsync.Call(hwnd, swShowMaximized)
	default:
		procShowWindowAsync.Call(hwnd, swShowNoActivate)
	}
}

// ---- 通知スイッチ（レジストリ）----

const notificationSettingsPath = `Software\Microsoft\Windows\CurrentVersion\Notifications\Settings`

// SystemRegistry は HKCU の通知設定。
type SystemRegistry struct{}

func (SystemRegistry) Subkeys() []string {
	k, err := registry.OpenKey(registry.CURRENT_USER, notificationSettingsPath, registry.ENUMERATE_SUB_KEYS)
	if err != nil {
		return nil
	}
	defer k.Close()
	names, _ := k.ReadSubKeyNames(-1)
	return names
}

func (SystemRegistry) Enabled(sub string) (uint32, bool) {
	k, err := registry.OpenKey(registry.CURRENT_USER, notificationSettingsPath+`\`+sub, registry.QUERY_VALUE)
	if err != nil {
		return 0, false
	}
	defer k.Close()
	v, _, err := k.GetIntegerValue("Enabled")
	if err != nil {
		return 0, false
	}
	return uint32(v), true
}

func (SystemRegistry) SetEnabled(sub string, value uint32) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, notificationSettingsPath+`\`+sub, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	return k.SetDWordValue("Enabled", value)
}

func (SystemRegistry) DeleteEnabled(sub string) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, notificationSettingsPath+`\`+sub, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if err := k.DeleteValue("Enabled"); err != nil && err != registry.ErrNotExist {
		return err
	}
	return nil
}

// NewSystemManager は実際のウィンドウとレジストリを操作する Manager（自分のプロセスは除く）。
func NewSystemManager() *Manager {
	return &Manager{Win: SystemWindows{}, Reg: SystemRegistry{}, SelfPID: uint32(os.Getpid())}
}
