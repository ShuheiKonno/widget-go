package main

import (
	"errors"
	"testing"

	"widget-go/focus"
)

// ---- 偽の Win32 / レジストリ（テストが実際のウィンドウや HKCU の通知設定に触れないように）----

type pomoFakeWin struct {
	wins  map[uintptr]*focus.Window
	calls []string
}

func newPomoFakeWin(ws ...focus.Window) *pomoFakeWin {
	f := &pomoFakeWin{wins: map[uintptr]*focus.Window{}}
	for _, w := range ws {
		w := w
		f.wins[w.HWND] = &w
	}
	return f
}

func (f *pomoFakeWin) Enumerate(includeHidden bool) []focus.Window {
	var out []focus.Window
	for _, w := range f.wins {
		if !includeHidden && (!w.Visible || w.HasOwner) {
			continue
		}
		out = append(out, *w)
	}
	return out
}

func (f *pomoFakeWin) Lookup(h uintptr) (focus.Window, bool) {
	if w, ok := f.wins[h]; ok {
		return *w, true
	}
	return focus.Window{}, false
}

func (f *pomoFakeWin) Hide(h uintptr) {
	f.calls = append(f.calls, "hide")
	f.wins[h].Visible = false
}

func (f *pomoFakeWin) Minimize(h uintptr) {
	f.calls = append(f.calls, "min")
	f.wins[h].Minimized = true
}

func (f *pomoFakeWin) Show(h uintptr, state string) {
	f.calls = append(f.calls, "show")
	f.wins[h].Visible = true
}

type pomoFakeReg struct{ vals map[string]*uint32 }

func (r *pomoFakeReg) Subkeys() []string {
	var out []string
	for k := range r.vals {
		out = append(out, k)
	}
	return out
}

func (r *pomoFakeReg) Enabled(sub string) (uint32, bool) {
	if p := r.vals[sub]; p != nil {
		return *p, true
	}
	return 0, false
}

func (r *pomoFakeReg) SetEnabled(sub string, v uint32) error {
	if r.vals == nil {
		return errors.New("no registry")
	}
	r.vals[sub] = &v
	return nil
}

func (r *pomoFakeReg) DeleteEnabled(sub string) error { r.vals[sub] = nil; return nil }

// このパッケージのテストは、既定で偽の実装を使う。実機のウィンドウ・レジストリ・トレイには触れない。
func init() {
	newFocusManager = func() *focus.Manager {
		return &focus.Manager{Win: newPomoFakeWin(), Reg: &pomoFakeReg{vals: map[string]*uint32{}}, SelfPID: 1, Save: saveFocusState}
	}
	pomodoroBalloon = func(title, msg string) {}
}

// useFakeFocus は、このテストの間、同じ偽のウィンドウとレジストリを使い続ける。
func useFakeFocus(t *testing.T, win *pomoFakeWin, reg *pomoFakeReg) {
	t.Helper()
	oldNew := newFocusManager
	newFocusManager = func() *focus.Manager {
		return &focus.Manager{Win: win, Reg: reg, SelfPID: 1, Save: saveFocusState}
	}
	t.Cleanup(func() { newFocusManager = oldNew })
}

func teamsWin(h uintptr) focus.Window {
	return focus.Window{HWND: h, PID: 10, Exe: "ms-teams.exe", Visible: true, Width: 800, Height: 600}
}

// 区画を表示にして、集中を始めた状態にする。
func startFocus(t *testing.T) {
	t.Helper()
	if err := setHidden("pomodoro", "false"); err != nil {
		t.Fatal(err)
	}
	now := pt(0, 9, 0, 0)
	fixPomodoroClock(t, now)
	if rec := postPomodoro("?action=start"); rec.Code != 200 {
		t.Fatalf("start: %d %s", rec.Code, rec.Body.String())
	}
}

func TestReconcileHidesDuringFocusAndRestoresOnBreak(t *testing.T) {
	withConfigFile(t, "")
	win := newPomoFakeWin(teamsWin(1), focus.Window{HWND: 2, PID: 11, Exe: "notepad.exe", Visible: true})
	reg := &pomoFakeReg{vals: map[string]*uint32{"MSTeams_x": u32p(1)}}
	useFakeFocus(t, win, reg)
	startFocus(t)

	pomodoroReconcile()
	if win.wins[1].Visible || !win.wins[2].Visible {
		t.Fatal("集中中は対象だけが隠れる")
	}
	if v := reg.vals["MSTeams_x"]; v == nil || *v != 0 {
		t.Error("通知が止まっていない")
	}
	rec := snapshotConfig().Pomodoro.Focus
	if len(rec.Windows) != 1 || len(rec.Notif) != 1 {
		t.Fatalf("隠したものが config に記録されていない: %+v", rec)
	}

	// 毎周期の再適用で、記録が変わらない
	pomodoroReconcile()
	if got := snapshotConfig().Pomodoro.Focus; len(got.Windows) != 1 || got.Notif[0].Value != 1 {
		t.Errorf("再適用で記録が壊れた: %+v", got)
	}

	// 休憩へ（スキップ）→ 戻る
	if rc := postPomodoro("?action=skip"); rc.Code != 200 {
		t.Fatal(rc.Code)
	}
	pomodoroReconcile()
	if !win.wins[1].Visible {
		t.Error("休憩で戻っていない")
	}
	if v := reg.vals["MSTeams_x"]; v == nil || *v != 1 {
		t.Error("通知が元の値に戻っていない")
	}
	if !snapshotConfig().Pomodoro.Focus.Empty() {
		t.Error("戻したのに記録が残っている")
	}
}

func u32p(v uint32) *uint32 { return &v }

func TestReconcileRestoresOnPauseResetHiddenAndFocusModeOff(t *testing.T) {
	for _, tc := range []struct {
		name string
		act  func(t *testing.T)
	}{
		{"一時停止", func(t *testing.T) { postPomodoro("?action=pause") }},
		{"リセット", func(t *testing.T) { postPomodoro("?action=reset") }},
		{"区画を非表示", func(t *testing.T) {
			if err := setHidden("pomodoro", "true"); err != nil {
				t.Fatal(err)
			}
		}},
		{"集中モードを OFF", func(t *testing.T) {
			if _, err := setPomodoroOption("pomoFocusMode", "false"); err != nil {
				t.Fatal(err)
			}
		}},
		{"一時的に表示", func(t *testing.T) { postPomodoro("?action=reveal") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			withConfigFile(t, "")
			win := newPomoFakeWin(teamsWin(1))
			useFakeFocus(t, win, &pomoFakeReg{vals: map[string]*uint32{}})
			startFocus(t)
			pomodoroReconcile()
			if win.wins[1].Visible {
				t.Fatal("隠れていない")
			}
			tc.act(t)
			pomodoroReconcile()
			if !win.wins[1].Visible || !snapshotConfig().Pomodoro.Focus.Empty() {
				t.Errorf("戻っていない: visible=%v rec=%+v", win.wins[1].Visible, snapshotConfig().Pomodoro.Focus)
			}
		})
	}
}

// 異常終了のあとの起動: 記録だけが残り、集中中でなければ、最初の周期で戻す。
func TestStartupRestoresLeftoverRecordsWhenNotFocusing(t *testing.T) {
	withConfigFile(t, "")
	hidden := teamsWin(1)
	hidden.Visible = false
	win := newPomoFakeWin(hidden)
	reg := &pomoFakeReg{vals: map[string]*uint32{"MSTeams_x": u32p(0)}}
	useFakeFocus(t, win, reg)
	mutateConfig(func(c *Config) {
		c.Pomodoro.Focus = focus.State{
			Windows: []focus.Hidden{{HWND: 1, Exe: "ms-teams.exe", State: focus.StateNormal, Method: focus.MethodHide}},
			Notif:   []focus.NotifBackup{{Key: "MSTeams_x", Existed: true, Value: 1}},
		}
	})
	fixPomodoroClock(t, pt(0, 9, 0, 0))
	pomodoroTick(pt(0, 9, 0, 0))
	if !win.wins[1].Visible {
		t.Error("隠れたままのウィンドウが戻っていない")
	}
	if v := reg.vals["MSTeams_x"]; v == nil || *v != 1 {
		t.Error("止めたままの通知が戻っていない")
	}
	if !snapshotConfig().Pomodoro.Focus.Empty() {
		t.Error("記録が残っている")
	}
}

// 起動時に集中が続いている（期限前）なら、戻さず、そのまま（再）適用する。
func TestStartupReappliesWhenStillFocusing(t *testing.T) {
	withConfigFile(t, "")
	win := newPomoFakeWin(teamsWin(1))
	useFakeFocus(t, win, &pomoFakeReg{vals: map[string]*uint32{}})
	startFocus(t)
	pomodoroTick(pt(0, 9, 10, 0))
	if win.wins[1].Visible {
		t.Error("集中中の起動では隠れるはず")
	}
}

func TestTickAdvancesAndNotifiesAndRestores(t *testing.T) {
	withConfigFile(t, "")
	win := newPomoFakeWin(teamsWin(1))
	useFakeFocus(t, win, &pomoFakeReg{vals: map[string]*uint32{}})
	var msgs []string
	old := pomodoroBalloon
	pomodoroBalloon = func(title, msg string) { msgs = append(msgs, msg) }
	t.Cleanup(func() { pomodoroBalloon = old })
	startFocus(t)
	pomodoroTick(pt(0, 9, 1, 0))
	if len(msgs) != 0 || win.wins[1].Visible {
		t.Fatalf("期限前は通知しない: %v", msgs)
	}

	pomodoroTick(pt(0, 9, 25, 0)) // 期限
	if p := snapshotConfig().Pomodoro; p.Phase != pomoShort {
		t.Fatalf("休憩に進んでいない: %+v", p)
	}
	if len(msgs) != 1 || !win.wins[1].Visible || !snapshotConfig().Pomodoro.Focus.Empty() {
		t.Errorf("休憩で通知して、戻すはず: msgs=%v visible=%v", msgs, win.wins[1].Visible)
	}
	pomodoroTick(pt(0, 9, 30, 0)) // 休憩の期限 → 集中。また隠す
	if len(msgs) != 2 || win.wins[1].Visible {
		t.Errorf("次の集中で通知して、また隠すはず: %v", msgs)
	}
}

func TestTickDoesNothingWhileSectionHidden(t *testing.T) {
	withConfigFile(t, "")
	win := newPomoFakeWin(teamsWin(1))
	useFakeFocus(t, win, &pomoFakeReg{vals: map[string]*uint32{}})
	mutateConfig(func(c *Config) { c.Pomodoro.begin(pomoFocus, 1, pt(0, 9, 0, 0), true) }) // 区画は既定で非表示のまま
	pomodoroTick(pt(0, 11, 0, 0))
	if !win.wins[1].Visible || len(win.calls) != 0 {
		t.Error("区画が非表示の間は、何も隠さない")
	}
	if snapshotConfig().Pomodoro.Phase != pomoFocus {
		t.Error("区画が非表示の間は、区間を進めない")
	}
}

func TestRestoreAllKeepsTimerState(t *testing.T) {
	withConfigFile(t, "")
	win := newPomoFakeWin(teamsWin(1))
	reg := &pomoFakeReg{vals: map[string]*uint32{"Teams_y": u32p(1)}}
	useFakeFocus(t, win, reg)
	startFocus(t)
	pomodoroReconcile()
	pomodoroRestoreAll()
	if !win.wins[1].Visible || *reg.vals["Teams_y"] != 1 {
		t.Error("戻っていない")
	}
	if p := snapshotConfig().Pomodoro; p.Phase != pomoFocus || !p.Focus.Empty() {
		t.Errorf("タイマーは継続し、記録だけ空になるはず: %+v", p)
	}
	pomodoroReconcile() // 終了ではないので、集中中ならまた隠す（隠す方法・対象を変えたときの「戻して適用し直す」）
	if win.wins[1].Visible {
		t.Error("RestoreAll のあと、集中中なのに再適用されない")
	}
}

// 終了の処理で戻したあと、1 秒周期の処理が、集中中のままのタイマーを見て、また隠してはいけない
// （ログオフの通知のあと、プロセスはしばらく生きていることがある）。
func TestShutdownRestoresAndNeverHidesAgain(t *testing.T) {
	withConfigFile(t, "")
	win := newPomoFakeWin(teamsWin(1))
	reg := &pomoFakeReg{vals: map[string]*uint32{"MSTeams_x": u32p(1)}}
	useFakeFocus(t, win, reg)
	t.Cleanup(func() { pomoShuttingDown.Store(false) })
	startFocus(t)
	pomodoroReconcile()
	if win.wins[1].Visible || *reg.vals["MSTeams_x"] != 0 {
		t.Fatal("集中中は隠れて、通知が止まっているはず")
	}

	pomodoroShutdown()
	if !win.wins[1].Visible || *reg.vals["MSTeams_x"] != 1 || !snapshotConfig().Pomodoro.Focus.Empty() {
		t.Fatal("終了時に戻っていない")
	}
	pomodoroTick(pt(0, 9, 5, 0)) // 周期の処理が、終了の途中で走る
	pomodoroKick()
	pomodoroReconcile()
	if !win.wins[1].Visible || *reg.vals["MSTeams_x"] != 1 || !snapshotConfig().Pomodoro.Focus.Empty() {
		t.Error("終了の処理のあと、また隠した / 通知を止めた")
	}
	if n := pomodoroRevealAll(); n != 0 || !win.wins[1].Visible {
		t.Errorf("終了後の revealAll: %d", n)
	}
	if p := snapshotConfig().Pomodoro; p.Phase != pomoFocus || !p.running() {
		t.Errorf("終了してもタイマーは継続する（次の起動で再適用する）: %+v", p)
	}
	pomodoroShutdown() // 何度呼んでもよい
}

func TestRevealAllRestoresRecordedAndUnrecordedForThisPhase(t *testing.T) {
	withConfigFile(t, "")
	stray := focus.Window{HWND: 9, PID: 10, Exe: "olk.exe", Visible: false, Width: 900, Height: 700, Title: "Outlook"} // 記録の無い、隠れた Outlook
	win := newPomoFakeWin(teamsWin(1), stray)
	useFakeFocus(t, win, &pomoFakeReg{vals: map[string]*uint32{}})
	startFocus(t)
	pomodoroReconcile()
	if n := pomodoroRevealAll(); n != 1 {
		t.Errorf("記録なしで戻した数: %d", n)
	}
	if !win.wins[1].Visible || !win.wins[9].Visible {
		t.Error("戻っていない")
	}
	if !snapshotConfig().Pomodoro.Revealed {
		t.Error("この区間は隠さない状態になっていない")
	}
	pomodoroReconcile() // 次の周期で、また隠れてはいけない
	if !win.wins[1].Visible {
		t.Error("戻した直後に、また隠した")
	}
}

func TestTrayToggle(t *testing.T) {
	withConfigFile(t, "")
	fixPomodoroClock(t, pt(0, 9, 0, 0))
	pomodoroTrayToggle()
	if snapshotConfig().Pomodoro.Phase != pomoFocus {
		t.Error("未開始なら開始")
	}
	pomodoroTrayToggle()
	if snapshotConfig().Pomodoro.Phase != pomoIdle {
		t.Error("開始済みなら停止（リセット）")
	}
}

func TestSameFocusState(t *testing.T) {
	a := focus.State{Windows: []focus.Hidden{{HWND: 1}}, Notif: []focus.NotifBackup{{Key: "k"}}}
	b := focus.State{Windows: []focus.Hidden{{HWND: 1}}, Notif: []focus.NotifBackup{{Key: "k"}}}
	if !sameFocusState(a, b) || !sameFocusState(focus.State{}, focus.State{Windows: []focus.Hidden{}}) {
		t.Error("同じ内容は等しい（nil と空も）")
	}
	b.Notif[0].Value = 1
	if sameFocusState(a, b) {
		t.Error("違う内容は等しくない")
	}
}
