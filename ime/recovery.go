//go:build windows

package ime

import (
	"context"
	"fmt"
	"log"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

// 復旧処理用の Win32 API 定義。
var (
	imm32             = syscall.NewLazyDLL("imm32.dll")
	procImmGetCtx     = imm32.NewProc("ImmGetContext")
	procImmAssocCtxEx = imm32.NewProc("ImmAssociateContextEx")
	procImmReleaseCtx = imm32.NewProc("ImmReleaseContext")

	procGetForeground       = imeUser32.NewProc("GetForegroundWindow")
	procSetForeground       = imeUser32.NewProc("SetForegroundWindow")
	procSetFocus            = imeUser32.NewProc("SetFocus")
	procPostMessageW        = imeUser32.NewProc("PostMessageW")
	procGetLastInputInfo    = imeUser32.NewProc("GetLastInputInfo")
	procGetWindowThreadProc = imeUser32.NewProc("GetWindowThreadProcessId")

	kernel32               = syscall.NewLazyDLL("kernel32.dll")
	procGetTickCount       = kernel32.NewProc("GetTickCount")
	procGetCurrentThreadID = kernel32.NewProc("GetCurrentThreadId")
	procGetCurrentProcID   = kernel32.NewProc("GetCurrentProcessId")
)

const (
	// iaceDefault は ImmAssociateContextEx の IACE_DEFAULT（Windows SDK の imm.h で 0x0010）。
	// 以前は 0x0003 を渡していた。これは IACE_DEFAULT ではなく、IACE_CHILDREN(0x0001) のビットに
	// imm.h で定義のないビット(0x0002) が立った値。未定義のビットを含む値を API がどう扱うか
	// （IACE_CHILDREN として子ウィンドウに hIMC=0 を関連付けるのか、FALSE で弾くのか）は未確認。
	iaceDefault = 0x0010
	wmActivate  = 0x0006
	waActive    = 1
)

// getCurrentThreadID は呼び出し元の OS スレッドの ID を返す。
func getCurrentThreadID() uint32 {
	tid, _, _ := procGetCurrentThreadID.Call()
	return uint32(tid)
}

// windowThreadProcess は hwnd を作ったスレッドとプロセスの ID を返す（診断ログ用）。
func windowThreadProcess(hwnd uintptr) (tid, pid uint32) {
	t, _, _ := procGetWindowThreadProc.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	return uint32(t), pid
}

// foregroundLabel は復旧対象の前面ウィンドウを診断ログ用の文字列にする。
// 自プロセスのウィンドウかどうかで、SetFocus や IMM の関数が効く前提が変わるため、
// 前面ウィンドウの PID/TID と自プロセスの PID/TID を並べて出す。
func foregroundLabel(hwnd uintptr) string {
	tid, pid := windowThreadProcess(hwnd)
	selfPID, _, _ := procGetCurrentProcID.Call()
	return fmt.Sprintf("hwnd=0x%X class=%q pid=%d tid=%d (自プロセス pid=%d tid=%d)",
		hwnd, getClassName(hwnd), pid, tid, uint32(selfPID), getCurrentThreadID())
}

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

// step1ResetIMEContext は ImmAssociateContextEx(IACE_DEFAULT) で、前面ウィンドウの
// IME コンテキストを既定に戻す。
//
// 前面ウィンドウは通常、別のプロセスのもの。IMM の関数が他のプロセスのウィンドウに効くかは
// Microsoft のドキュメントに明記がなく（入力コンテキストはスレッドごとの資源とだけある）、
// 実機で未検証。効いているかを確かめられるよう、各 API の戻り値をログに出す。
//
// ImmGetContext の戻り値（himc）は、IACE_DEFAULT では hIMC が無視されるため、リセット自体には
// 使わない。他プロセスのウィンドウから入力コンテキストを取れるか（0 が返るか）の診断として残す。
// 実機検証の結果、不要とわかれば ImmGetContext / ImmReleaseContext ごと削除してよい。
func (r *Recoverer) step1ResetIMEContext() {
	hwnd, _, _ := procGetForeground.Call()
	if hwnd == 0 {
		log.Printf("[%s]     GetForegroundWindow: 前面ウィンドウなし", time.Now().Format("15:04:05"))
		return
	}
	log.Printf("[%s]     対象: %s", time.Now().Format("15:04:05"), foregroundLabel(hwnd))

	himc, _, _ := procImmGetCtx.Call(hwnd)
	log.Printf("[%s]     ImmGetContext → himc=0x%X", time.Now().Format("15:04:05"), himc)

	ret, _, err := procImmAssocCtxEx.Call(hwnd, 0, uintptr(iaceDefault))
	log.Printf("[%s]     ImmAssociateContextEx(IACE_DEFAULT) → %s", time.Now().Format("15:04:05"), boolResult(ret, err))

	if himc != 0 {
		ret, _, err := procImmReleaseCtx.Call(hwnd, himc)
		log.Printf("[%s]     ImmReleaseContext → %s", time.Now().Format("15:04:05"), boolResult(ret, err))
	}
}

// step2RefocusForeground は SetForegroundWindow → SetFocus → PostMessage(WM_ACTIVATE)
// でフォーカスを再付与する。SetForegroundWindow の戻り値（BOOL）を返す。
//
// 仕様書 L162 のとおり keybd_event は使わない（ユーザーの画面操作を奪わないため）。
//
// 実機で効いているかは未検証。Microsoft のドキュメントでは、SetFocus は呼び出し元スレッドの
// メッセージキューに属するウィンドウにしか効かない（他のスレッドのウィンドウには NULL を返す）。
// また、対象はもともと前面にあるウィンドウなので、SetForegroundWindow で前面が変わることはない。
// 効いているかを確かめられるよう、各 API の戻り値をログに出す。
func (r *Recoverer) step2RefocusForeground() bool {
	hwnd, _, _ := procGetForeground.Call()
	if hwnd == 0 {
		log.Printf("[%s]     GetForegroundWindow: 前面ウィンドウなし", time.Now().Format("15:04:05"))
		return false
	}
	log.Printf("[%s]     対象: %s", time.Now().Format("15:04:05"), foregroundLabel(hwnd))

	ret, _, _ := procSetForeground.Call(hwnd)
	// SetForegroundWindow は失敗しても GetLastError を設定しないため、戻り値だけを出す。
	log.Printf("[%s]     SetForegroundWindow → %d", time.Now().Format("15:04:05"), ret)

	prev, _, err := procSetFocus.Call(hwnd)
	if prev == 0 {
		// 他のスレッドのウィンドウ（キューが別）を渡すと NULL が返る。
		log.Printf("[%s]     SetFocus → NULL (%v)", time.Now().Format("15:04:05"), err)
	} else {
		log.Printf("[%s]     SetFocus → 直前のフォーカス=0x%X", time.Now().Format("15:04:05"), prev)
	}

	pm, _, err := procPostMessageW.Call(hwnd, uintptr(wmActivate), uintptr(waActive), 0)
	log.Printf("[%s]     PostMessageW(WM_ACTIVATE) → %s", time.Now().Format("15:04:05"), boolResult(pm, err))
	return ret != 0
}

// boolResult は BOOL を返す Win32 API の結果を診断ログ用の文字列にする。
// GetLastError の値は、失敗したとき（0 のとき）だけ意味があるので、そのときだけ添える。
func boolResult(ret uintptr, err error) string {
	if ret != 0 {
		return fmt.Sprintf("%d (成功)", ret)
	}
	return fmt.Sprintf("0 (失敗: %v)", err)
}

// shouldSkipByCooldown は前回復旧から cooldown 以内かを純粋関数として判定する（テスト用）。
// last がゼロ値の場合は初回扱いで false を返す。
func shouldSkipByCooldown(last time.Time, now time.Time, cooldown time.Duration) bool {
	if last.IsZero() {
		return false
	}
	return now.Sub(last) < cooldown
}
