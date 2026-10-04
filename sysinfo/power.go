package sysinfo

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// 電源プラン（scheme）の GUID。
var (
	// PowerSchemeBalanced は電源プラン「バランス」。電源モード（overlay）が効くのはこのプランが有効なときだけ。
	PowerSchemeBalanced = windows.GUID{Data1: 0x381b4222, Data2: 0xf694, Data3: 0x41f0, Data4: [8]byte{0x96, 0x85, 0xff, 0x5b, 0xb2, 0x60, 0xdf, 0x2e}}
)

// 電源モード（overlay）の GUID。設定アプリの「電源モード」の 3 段階に対応する。
var (
	// OverlayBetterBattery は「最適な電力効率」。
	OverlayBetterBattery = windows.GUID{Data1: 0x961cc777, Data2: 0x2547, Data3: 0x4f9d, Data4: [8]byte{0x81, 0x74, 0x7d, 0x86, 0x18, 0x1b, 0x8a, 0x7a}}
	// OverlayBalanced は「バランス」。overlay なし（GUID_NULL）がバランスを表す。
	OverlayBalanced = windows.GUID{}
	// OverlayBestPerformance は「最適なパフォーマンス」。
	OverlayBestPerformance = windows.GUID{Data1: 0xded574b5, Data2: 0x45a0, Data3: 0x4f42, Data4: [8]byte{0x87, 0x37, 0x46, 0x34, 0x5c, 0x09, 0xc2, 0x38}}
)

// 電源モードの名前。UI と /api/powermode の mode パラメータで使う。
const (
	PowerModeSaver    = "saver"    // 最適な電力効率
	PowerModeBalanced = "balanced" // バランス
	PowerModeBest     = "best"     // 最適なパフォーマンス
	// PowerModeOther は 3 つのどれでもない状態（未知の overlay、電源プランがバランス以外）。
	// 状態の表示専用で、設定はできない。
	PowerModeOther = "other"
)

// OverlayForMode は mode に対応する overlay の GUID を返す。設定してよい mode の許可リストを兼ねる。
// 3 つ以外（PowerModeOther や未知の文字列を含む）は ok=false。大文字小文字は区別する。
func OverlayForMode(mode string) (g windows.GUID, ok bool) {
	switch mode {
	case PowerModeSaver:
		return OverlayBetterBattery, true
	case PowerModeBalanced:
		return OverlayBalanced, true
	case PowerModeBest:
		return OverlayBestPerformance, true
	}
	return windows.GUID{}, false
}

// ModeForOverlay は overlay の GUID に対応する mode を返す。3 つ以外は PowerModeOther。
// 電源プランは考慮しない。状態の判定には ResolvePowerMode を使うこと。
func ModeForOverlay(g windows.GUID) string {
	switch g {
	case OverlayBetterBattery:
		return PowerModeSaver
	case OverlayBalanced:
		return PowerModeBalanced
	case OverlayBestPerformance:
		return PowerModeBest
	}
	return PowerModeOther
}

// ResolvePowerMode は電源プランと実効 overlay から、UI で強調する mode と、切替できるかを返す。
//
// overlay が効くのは電源プランがバランスのときだけで、それ以外のプランでは実効 overlay が
// 常に GUID_NULL（= バランスと同じ値）になる。そのため overlay より先に電源プランを見て、
// バランス以外なら planOK=false・active=PowerModeOther とする。
func ResolvePowerMode(scheme, overlay windows.GUID) (active string, planOK bool) {
	if scheme != PowerSchemeBalanced {
		return PowerModeOther, false
	}
	return ModeForOverlay(overlay), true
}

// ErrPowerModeUnsupported は電源モードの取得・設定に必要な API が使えないことを表す。
var ErrPowerModeUnsupported = errors.New("電源モードの API が利用できません")

// powrprof の LazyDLL は cpufreq.go のものを共有する。
//
// overlay の取得・設定（PowerGetEffectiveOverlayScheme / PowerSetActiveOverlayScheme）は
// Microsoft Learn に記載のない非公開 API。PowerGetActiveScheme は公式（powersetting.h）。
var (
	procPowerGetActiveScheme           = powrprof.NewProc("PowerGetActiveScheme")
	procPowerGetEffectiveOverlayScheme = powrprof.NewProc("PowerGetEffectiveOverlayScheme")
	procPowerSetActiveOverlayScheme    = powrprof.NewProc("PowerSetActiveOverlayScheme")
)

// PowerModeSupported は電源モードの取得・設定を使えるかを返す。
//
// PowerSetActiveOverlayScheme は GUID を値渡しで受け取る。x64 の呼び出し規約では 16 バイトの
// 構造体は呼び出し側が作ったコピーへのポインタとして渡るので、&guid で呼べる（実機で確認済み）。
// 32bit や ARM64 では渡し方が異なるため、amd64 以外では使えないものとして扱う。
// 3 つの関数のどれかが無い場合も使えない（古い OS など。呼ぶと panic するので先に確認する）。
var PowerModeSupported = sync.OnceValue(func() bool {
	if runtime.GOARCH != "amd64" {
		return false
	}
	for _, p := range []*syscall.LazyProc{
		procPowerGetActiveScheme,
		procPowerGetEffectiveOverlayScheme,
		procPowerSetActiveOverlayScheme,
	} {
		if p.Find() != nil {
			return false
		}
	}
	return true
})

// powerError は powrprof の戻り値（Win32 エラーコード）をエラーにする。
// 呼び出し側は errors.As で syscall.Errno を取り出してコードを参照できる。
func powerError(name string, code uintptr) error {
	return fmt.Errorf("%s: %w", name, syscall.Errno(code))
}

// ActivePowerScheme は現在有効な電源プランの GUID を返す。
func ActivePowerScheme() (windows.GUID, error) {
	if !PowerModeSupported() {
		return windows.GUID{}, ErrPowerModeUnsupported
	}
	var p *windows.GUID
	if r, _, _ := procPowerGetActiveScheme.Call(0, uintptr(unsafe.Pointer(&p))); r != 0 {
		return windows.GUID{}, powerError("PowerGetActiveScheme", r)
	}
	if p == nil {
		return windows.GUID{}, errors.New("PowerGetActiveScheme が GUID を返しませんでした")
	}
	g := *p
	// 返された GUID は LocalAlloc で確保されているので解放する。
	windows.LocalFree(windows.Handle(unsafe.Pointer(p)))
	return g, nil
}

// EffectiveOverlay は現在の電源側（電源接続 / バッテリー駆動）で実効の電源モード（overlay）を返す。
// 電源プランがバランス以外のときは常に OverlayBalanced（GUID_NULL）が返るので、
// 呼び出し側は先に ActivePowerScheme を確認すること。
func EffectiveOverlay() (windows.GUID, error) {
	if !PowerModeSupported() {
		return windows.GUID{}, ErrPowerModeUnsupported
	}
	var g windows.GUID
	if r, _, _ := procPowerGetEffectiveOverlayScheme.Call(uintptr(unsafe.Pointer(&g))); r != 0 {
		return windows.GUID{}, powerError("PowerGetEffectiveOverlayScheme", r)
	}
	return g, nil
}

// SetOverlay は現在の電源側の電源モード（overlay）を設定する。もう一方の電源側は変わらない。
// 電源プランは変更しない。バランス以外のプランが有効でも呼べるが、実効にはならないので、
// 呼び出し側で ActivePowerScheme を確認すること。
func SetOverlay(g windows.GUID) error {
	if !PowerModeSupported() {
		return ErrPowerModeUnsupported
	}
	if r, _, _ := procPowerSetActiveOverlayScheme.Call(uintptr(unsafe.Pointer(&g))); r != 0 {
		return powerError("PowerSetActiveOverlayScheme", r)
	}
	return nil
}
