//go:build windows

package ime

import (
	"context"
	"log"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

// 復旧処理用の Win32 API 定義。
var (
	imm32        = syscall.NewLazyDLL("imm32.dll")
	procImmGetCtx       = imm32.NewProc("ImmGetContext")
	procImmAssocCtxEx   = imm32.NewProc("ImmAssociateContextEx")
	procImmReleaseCtx   = imm32.NewProc("ImmReleaseContext")

	procGetForeground   = imeUser32.NewProc("GetForegroundWindow")
	procSetForeground   = imeUser32.NewProc("SetForegroundWindow")
	procSetFocus        = imeUser32.NewProc("SetFocus")
	procPostMessageW    = imeUser32.NewProc("PostMessageW")
	procGetLastInputInfo = imeUser32.NewProc("GetLastInputInfo")

	kernel32         = syscall.NewLazyDLL("kernel32.dll")
	procGetTickCount = kernel32.NewProc("GetTickCount")
)

const (
	iaceDefault = 0x0003
	wmActivate  = 0x0006
	waActive    = 1
)

// LastInputInfoProvider は GetLastInputInfo の薄いラッパーをモック化するための
// インターフェース。仕様書 L248 の「interface で注入できる構造」要件を満たす。
type LastInputInfoProvider interface {
	LastInputElapsed() (time.Duration, error)
}

// LASTINPUTINFO は Win32 の LASTINPUTINFO 構造体に対応する。
type lastInputInfo struct {
	CbSize uint32
	DwTime uint32
}

// WinLastInputInfo は本物の GetLastInputInfo を呼ぶ実装。
type WinLastInputInfo struct{}

// LastInputElapsed は最終ユーザー入力からの経過時間を返す。
func (WinLastInputInfo) LastInputElapsed() (time.Duration, error) {
	var info lastInputInfo
	info.CbSize = uint32(unsafe.Sizeof(info))
	ret, _, err := procGetLastInputInfo.Call(uintptr(unsafe.Pointer(&info)))
	if ret == 0 {
		return 0, err
	}
	tick, _, _ := procGetTickCount.Call()
	elapsedMs := uint32(tick) - info.DwTime
	return time.Duration(elapsedMs) * time.Millisecond, nil
}

// Recoverer は検知チャネルを受信して復旧処理を実行するワーカー。
type Recoverer struct {
	Cooldown      time.Duration
	Threshold     int32
	DryRun        bool
	InputProvider LastInputInfoProvider

	mu           sync.Mutex
	lastRecovery time.Time
}

// Run は detectChan を受信し、復旧処理を逐次実行する。ctx.Done で終了する。
func (r *Recoverer) Run(ctx context.Context, ch <-chan DetectEvent) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-ch:
			if !ok {
				return
			}
			r.handle(ev)
		}
	}
}

func (r *Recoverer) handle(ev DetectEvent) {
	now := time.Now()

	r.mu.Lock()
	last := r.lastRecovery
	r.mu.Unlock()

	w := ev.Rect.Right - ev.Rect.Left
	h := ev.Rect.Bottom - ev.Rect.Top

	if shouldSkipByCooldown(last, now, r.Cooldown) {
		log.Printf("[%s] ⚠ IME左上ボックス検知 (クールダウン中のためスキップ)", now.Format("15:04:05"))
		return
	}

	log.Printf("[%s] ⚠ IME左上ボックス検知: class=%s pos=(%d,%d) size=%dx%d",
		now.Format("15:04:05"), ev.ClassName, ev.Rect.Left, ev.Rect.Top, w, h)

	if r.DryRun {
		log.Printf("[%s]   → DryRun モードのため復旧処理スキップ", time.Now().Format("15:04:05"))
		r.mu.Lock()
		r.lastRecovery = now
		r.mu.Unlock()
		return
	}

	// アイドル状態（スリープ復帰直後など）での誤発動を防ぐためのガード。
	// 直近 500ms 以内にユーザー入力があった時のみ復旧する。
	// アイドルスキップ時は lastRecovery を更新しない。更新すると、ユーザーが
	// タイピング再開した直後の検知でクールダウンに引っかかり、復旧が遅れる。
	if r.InputProvider != nil {
		elapsed, err := r.InputProvider.LastInputElapsed()
		if err == nil && elapsed > 500*time.Millisecond {
			log.Printf("[%s]   → 直近入力から %dms 経過のため復旧スキップ（アイドル判定）",
				time.Now().Format("15:04:05"), elapsed/time.Millisecond)
			return
		}
	}

	// Step 1: IME コンテキストのリセット（最優先・副作用最小）
	log.Printf("[%s]   → Step1: IMEコンテキストリセット実行", time.Now().Format("15:04:05"))
	r.step1ResetIMEContext()
	time.Sleep(100 * time.Millisecond)
	if _, _, _, found := findIMETopLeft(r.Threshold); !found {
		log.Printf("[%s]   → ✓ 解消確認", time.Now().Format("15:04:05"))
		r.mu.Lock()
		r.lastRecovery = now
		r.mu.Unlock()
		return
	}

	// Step 2: フォーカスの再付与
	log.Printf("[%s]   → Step1: 未解消", time.Now().Format("15:04:05"))
	log.Printf("[%s]   → Step2: フォーカス再付与 実行", time.Now().Format("15:04:05"))
	setFgOK := r.step2RefocusForeground()
	if !setFgOK {
		// SetForegroundWindow は Windows のフォーカス奪取制限により静かに失敗する場合がある。
		// 戻り値の BOOL を可視化してデバッグの手がかりにする。
		log.Printf("[%s]   → SetForegroundWindow が拒否されました（Windows のフォーカス奪取制限）", time.Now().Format("15:04:05"))
	}
	time.Sleep(100 * time.Millisecond)
	if _, _, _, found := findIMETopLeft(r.Threshold); !found {
		log.Printf("[%s]   → ✓ 解消確認", time.Now().Format("15:04:05"))
		r.mu.Lock()
		r.lastRecovery = now
		r.mu.Unlock()
		return
	}

	// Step 3: ログ記録のみ（ctfmon.exe 再起動は管理者権限が必要なため自動実行しない）
	log.Printf("[%s]   → Step2: 未解消", time.Now().Format("15:04:05"))
	log.Printf("[%s]   → ✗ 自動復旧失敗。手動対応が必要", time.Now().Format("15:04:05"))
	r.mu.Lock()
	r.lastRecovery = now
	r.mu.Unlock()
}

// step1ResetIMEContext は ImmAssociateContextEx で IME コンテキストを既定にリセットする。
func (r *Recoverer) step1ResetIMEContext() {
	hwnd, _, _ := procGetForeground.Call()
	if hwnd == 0 {
		return
	}
	himc, _, _ := procImmGetCtx.Call(hwnd)
	procImmAssocCtxEx.Call(hwnd, 0, uintptr(iaceDefault))
	if himc != 0 {
		procImmReleaseCtx.Call(hwnd, himc)
	}
}

// step2RefocusForeground は SetForegroundWindow → SetFocus → PostMessage(WM_ACTIVATE)
// でフォーカスを再付与する。SetForegroundWindow の戻り値（BOOL）を返す。
//
// 仕様書 L162 のとおり keybd_event は使わない（ユーザーの画面操作を奪わないため）。
func (r *Recoverer) step2RefocusForeground() bool {
	hwnd, _, _ := procGetForeground.Call()
	if hwnd == 0 {
		return false
	}
	ret, _, _ := procSetForeground.Call(hwnd)
	procSetFocus.Call(hwnd)
	procPostMessageW.Call(hwnd, uintptr(wmActivate), uintptr(waActive), 0)
	return ret != 0
}

// shouldSkipByCooldown は前回復旧から cooldown 以内かを純粋関数として判定する（テスト用）。
// last がゼロ値の場合は初回扱いで false を返す。
func shouldSkipByCooldown(last time.Time, now time.Time, cooldown time.Duration) bool {
	if last.IsZero() {
		return false
	}
	return now.Sub(last) < cooldown
}
