package main

import (
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"widget-go/aiusage"
	"widget-go/focus"
)

// ポモドーロの 1 秒周期の処理（区間の切り替え）と、集中モードの適用・解除。
//
// Win32 とレジストリの操作は、mutateConfig（configMu を保持する）の外で行う。記録の保存は、
// focus.Manager.Save（変える前）と、reconcile の最後の 2 回に分けて、そのつど mutateConfig を取り直す。

// newFocusManager は集中モードの実体の生成。テストで偽の実装に差し替える
// （テストが実際のウィンドウや HKCU の通知設定に触れないように）。
var newFocusManager = func() *focus.Manager {
	m := focus.NewSystemManager()
	m.Save = saveFocusState
	return m
}

// pomodoroBalloon は区間の切り替えの通知（トレイのバルーン）。テストで差し替える。
var pomodoroBalloon = func(title, msg string) {
	// 音は出さない（NIIF_NOSOUND）
	showBalloon(title, msg, aiusage.NIIF_INFO|niifNoSound)
}

const niifNoSound = 0x10

var (
	// pomodoroKickCh は、設定や操作が変わったとき、次の周期を待たずに処理させる合図。
	pomodoroKickCh = make(chan struct{}, 1)
	// pomoFocusMu は、集中モードの適用・解除（Win32 とレジストリ）を 1 つずつにする。
	pomoFocusMu sync.Mutex
	// pomoShuttingDown は、終了の処理（pomodoroShutdown）に入ったあと、二度と隠さないための一方向の印。
	// 戻したあとに、1 秒周期の処理が、集中中のままのタイマーを見て、また隠してしまうのを防ぐ。
	pomoShuttingDown atomic.Bool
)

func pomodoroKick() {
	select {
	case pomodoroKickCh <- struct{}{}:
	default:
	}
}

// pomodoroLoop はポモドーロの周期処理。プロセスの起動時に 1 回だけ goroutine で起動する。
// 最初の周期で、前回の異常終了で隠れたままのものを戻す（集中中なら再適用する）。
func pomodoroLoop() {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		pomodoroTick(pomodoroClock())
		select {
		case <-t.C:
		case <-pomodoroKickCh:
		}
	}
}

// pomodoroTick は、期限を過ぎた区間を進めて（通知して）、集中モードの状態を合わせる。
func pomodoroTick(now time.Time) {
	var (
		ev      pomodoroEvent
		changed bool
	)
	// 毎秒の config 書き込みを避けるため、期限が来たときだけ mutateConfig を呼ぶ
	if c := snapshotConfig(); !c.Hidden.Pomodoro && c.Pomodoro.due(now) {
		mutateConfig(func(c *Config) {
			next := c.Pomodoro
			if e, ok := next.advance(now); ok {
				c.Pomodoro, ev, changed = next, e, true
			}
		})
	}
	if changed {
		pomodoroNotify(ev)
	}
	pomodoroReconcile()
}

func pomodoroNotify(ev pomodoroEvent) {
	switch ev.To {
	case pomoShort:
		pomodoroBalloon("ポモドーロ", "集中が終わりました。休憩にしましょう。")
	case pomoLong:
		pomodoroBalloon("ポモドーロ", "集中が終わりました。長い休憩にしましょう。")
	case pomoFocus:
		pomodoroBalloon("ポモドーロ", "休憩が終わりました。集中を始めましょう。")
	}
}

func focusOptions(p PomodoroConfig) focus.Options {
	return focus.Options{Targets: p.Targets, Method: p.HideMethod, Notifications: true}
}

func sameFocusState(a, b focus.State) bool {
	if len(a.Windows) != len(b.Windows) || len(a.Notif) != len(b.Notif) {
		return false
	}
	for i := range a.Windows {
		if a.Windows[i] != b.Windows[i] {
			return false
		}
	}
	for i := range a.Notif {
		if a.Notif[i] != b.Notif[i] {
			return false
		}
	}
	return true
}

// saveFocusState は集中モードの記録を保存する。記録が同じなら書き込まない
// （集中中は pomodoroReconcile が毎秒呼ぶので、そのたびに config.json を書き直さないため）。
func saveFocusState(s focus.State) {
	mutateConfigIf(func(c *Config) bool {
		if sameFocusState(c.Pomodoro.Focus, s) {
			return false
		}
		c.Pomodoro.Focus = s
		return true
	})
}

// pomodoroReconcile は、集中モードをいまの状態に合わせる。働かせるべきなら適用（新しいウィンドウも隠す）、
// そうでなくて記録が残っていれば戻す。何も変えておらず、働かせる必要もないときは、Win32 を呼ばない。
func pomodoroReconcile() {
	pomoFocusMu.Lock()
	defer pomoFocusMu.Unlock()
	if pomoShuttingDown.Load() {
		return
	}
	c := snapshotConfig()
	p := c.Pomodoro
	switch {
	case pomodoroWantsFocus(c):
		next := newFocusManager().Apply(p.Focus, focusOptions(p))
		saveFocusState(next)
	case !p.Focus.Empty():
		saveFocusState(newFocusManager().Restore(p.Focus))
	}
}

// pomodoroRestoreAll は記録したものをすべて元に戻す。隠す方法や対象を変えたときに呼ぶ（このあと、あらためて適用される）。
// タイマーの状態は変えない。終了時は、これではなく pomodoroShutdown を使う。
func pomodoroRestoreAll() {
	pomoFocusMu.Lock()
	defer pomoFocusMu.Unlock()
	restoreAllLocked()
}

func restoreAllLocked() {
	if p := snapshotConfig().Pomodoro; !p.Focus.Empty() {
		saveFocusState(newFocusManager().Restore(p.Focus))
	}
}

// pomodoroShutdown は終了時（トレイの「終了」・/api/close・ログオフ・シャットダウン・ウィンドウを閉じて Run を抜けたとき）に、
// 記録したものを戻して、以後は二度と隠さない。タイマーの状態は変えない（次の起動で集中中なら、またそこから適用される）。
// 何度呼んでもよい。
func pomodoroShutdown() {
	pomoFocusMu.Lock()
	defer pomoFocusMu.Unlock()
	pomoShuttingDown.Store(true)
	restoreAllLocked()
}

// pomodoroRevealAll はトレイの「Teams / Outlook の表示を戻す」。集中中は、この区間の間だけ隠さない状態にしたうえで、
// 記録したものを戻し、さらに記録の無い（取りこぼした）対象のウィンドウも戻す。戻した（記録なしの）数を返す。
func pomodoroRevealAll() int {
	now := pomodoroClock()
	mutateConfig(func(c *Config) {
		next := c.Pomodoro
		next.advance(now)
		if next.running() && next.Phase == pomoFocus && next.FocusMode {
			next.Revealed = true
		}
		c.Pomodoro = next
	})
	pomoFocusMu.Lock()
	defer pomoFocusMu.Unlock()
	if pomoShuttingDown.Load() {
		return 0
	}
	p := snapshotConfig().Pomodoro
	m := newFocusManager()
	saveFocusState(m.Restore(p.Focus))
	return m.RestoreUnrecorded(p.Targets)
}

// ---- トレイのメニュー ----

// appendPomodoroMenu は、ポモドーロの区画を表示しているときの、トレイの右クリックメニューの項目を足す。
func appendPomodoroMenu(menu uintptr) {
	c := snapshotConfig()
	if c.Hidden.Pomodoro {
		return
	}
	add := func(id uintptr, text string) {
		p, _ := syscall.UTF16PtrFromString(text)
		procAppendMenuW.Call(menu, MF_STRING, id, uintptr(unsafe.Pointer(p)))
	}
	if c.Pomodoro.Phase == pomoIdle {
		add(ID_TRAY_POMO_TOGGLE, "ポモドーロを開始")
	} else {
		add(ID_TRAY_POMO_TOGGLE, "ポモドーロを停止")
	}
	add(ID_TRAY_POMO_REVEAL, "Teams / Outlook の表示を戻す")
	procAppendMenuW.Call(menu, MF_SEPARATOR, 0, 0)
}

// pomodoroTrayToggle はトレイの「ポモドーロを開始 / 停止」。
func pomodoroTrayToggle() {
	now := pomodoroClock()
	mutateConfig(func(c *Config) {
		next := c.Pomodoro
		action := "reset"
		if next.Phase == pomoIdle {
			action = "start"
		}
		if applyPomodoroAction(&next, action, now) == nil {
			c.Pomodoro = next
		}
	})
	pomodoroKick()
}
