package focus

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// ---- 偽の Win32 / レジストリ ----

type fakeWin struct {
	wins  map[uintptr]*Window
	calls []string
}

func newFakeWin(ws ...Window) *fakeWin {
	f := &fakeWin{wins: map[uintptr]*Window{}}
	for _, w := range ws {
		w := w
		f.wins[w.HWND] = &w
	}
	return f
}

func (f *fakeWin) Enumerate(includeHidden bool) []Window {
	var out []Window
	for _, w := range f.wins {
		if !includeHidden && (!w.Visible || w.HasOwner) {
			continue
		}
		out = append(out, *w)
	}
	return out
}

func (f *fakeWin) Lookup(h uintptr) (Window, bool) {
	w, ok := f.wins[h]
	if !ok {
		return Window{}, false
	}
	return *w, true
}

func (f *fakeWin) Hide(h uintptr) {
	f.calls = append(f.calls, "hide")
	if w := f.wins[h]; w != nil {
		w.Visible = false
	}
}

func (f *fakeWin) Minimize(h uintptr) {
	f.calls = append(f.calls, "min")
	if w := f.wins[h]; w != nil {
		w.Minimized = true
	}
}

func (f *fakeWin) Show(h uintptr, state string) {
	f.calls = append(f.calls, "show:"+state)
	if w := f.wins[h]; w != nil {
		w.Visible = true
		w.Minimized = state == StateMin
		w.Maximized = state == StateMax
	}
}

type fakeReg struct {
	vals    map[string]*uint32 // nil = Enabled の値が無い
	setFail map[string]bool
	writes  []string
}

func (r *fakeReg) Subkeys() []string {
	var out []string
	for k := range r.vals {
		out = append(out, k)
	}
	return out
}

func (r *fakeReg) Enabled(sub string) (uint32, bool) {
	if p := r.vals[sub]; p != nil {
		return *p, true
	}
	return 0, false
}

func (r *fakeReg) SetEnabled(sub string, v uint32) error {
	if r.setFail[sub] {
		return errors.New("denied")
	}
	r.writes = append(r.writes, "set:"+sub)
	r.vals[sub] = &v
	return nil
}

func (r *fakeReg) DeleteEnabled(sub string) error {
	r.writes = append(r.writes, "del:"+sub)
	r.vals[sub] = nil
	return nil
}

func u32(v uint32) *uint32 { return &v }

var targets = []string{"ms-teams.exe", "olk.exe"}

const self = 4242

func teams(h uintptr) Window {
	return Window{HWND: h, PID: 10, Exe: "ms-teams.exe", Visible: true, Width: 800, Height: 600}
}

// ---- 純粋な部分 ----

func TestMatchExe(t *testing.T) {
	for _, c := range []struct {
		exe  string
		want bool
	}{
		{"ms-teams.exe", true}, {"MS-Teams.EXE", true}, {`C:\Program Files\WindowsApps\ms-teams.exe`, true},
		{"explorer.exe", false}, {"", false}, {"ms-teams.exe.bak", false},
	} {
		if got := MatchExe(c.exe, targets); got != c.want {
			t.Errorf("MatchExe(%q) = %v", c.exe, got)
		}
	}
	if MatchExe("x.exe", nil) {
		t.Error("対象が空なら何にも当たらない")
	}
}

func TestSelectOnlyVisibleUnownedTargetsAndNotSelf(t *testing.T) {
	wins := []Window{
		teams(1),
		{HWND: 2, PID: 10, Exe: "ms-teams.exe", Visible: false},                // 不可視
		{HWND: 3, PID: 10, Exe: "ms-teams.exe", Visible: true, HasOwner: true}, // ダイアログ
		{HWND: 4, PID: 11, Exe: "explorer.exe", Visible: true},                 // 対象外（トースト・シェルなど）
		{HWND: 5, PID: self, Exe: "ms-teams.exe", Visible: true},               // 自分のプロセス
		{HWND: 6, PID: 12, Exe: "OLK.EXE", Visible: true},
	}
	var got []uintptr
	for _, w := range Select(wins, targets, self) {
		got = append(got, w.HWND)
	}
	if !reflect.DeepEqual(got, []uintptr{1, 6}) {
		t.Errorf("選ばれたもの: %v", got)
	}
}

func TestNotifHints(t *testing.T) {
	got := NotifHints([]string{"ms-teams.exe", "teams.exe", "olk.exe", "outlook.exe", "slack.exe", "ab.exe"})
	want := []string{"teams", "outlookforwindows", "outlook", "slack"} // 短い名前（ab）は無関係なキーに当たるので使わない
	if !reflect.DeepEqual(got, want) {
		t.Errorf("hints = %v", got)
	}
}

// ---- 隠す・戻す ----

func TestApplyHidesTargetsAndRestoreRestoresOnlyRecorded(t *testing.T) {
	maxed := teams(2)
	maxed.Maximized = true
	other := Window{HWND: 3, PID: 11, Exe: "notepad.exe", Visible: true}
	userHidden := Window{HWND: 4, PID: 10, Exe: "ms-teams.exe", Visible: false} // もともと見えない
	win := newFakeWin(teams(1), maxed, other, userHidden)
	m := &Manager{Win: win, SelfPID: self}

	st := m.Apply(State{}, Options{Targets: targets, Method: MethodHide})
	if len(st.Windows) != 2 {
		t.Fatalf("記録: %+v", st.Windows)
	}
	if win.wins[1].Visible || win.wins[2].Visible {
		t.Error("対象が隠れていない")
	}
	if !win.wins[3].Visible {
		t.Error("対象外のウィンドウを隠した")
	}

	// 毎周期の再適用: すでに隠したものには何もしない（記録も増えない）
	win.calls = nil
	st2 := m.Apply(st, Options{Targets: targets, Method: MethodHide})
	if len(win.calls) != 0 || !reflect.DeepEqual(st.Windows, st2.Windows) {
		t.Errorf("再適用で変わった: calls=%v", win.calls)
	}

	// 元からあった不可視ウィンドウ（4）は戻さない。最大化は最大化へ戻す
	win.calls = nil
	left := m.Restore(st2)
	if !left.Empty() {
		t.Errorf("戻し残し: %+v", left)
	}
	if !win.wins[1].Visible || !win.wins[2].Visible || !win.wins[2].Maximized {
		t.Error("記録したものが元の状態に戻っていない")
	}
	if win.wins[4].Visible {
		t.Error("記録に無い（元から見えない）ウィンドウを表示した")
	}
	if len(win.calls) != 2 {
		t.Errorf("calls=%v", win.calls)
	}
}

func TestApplyHidesNewlyAppearedWindowsAndReHidesUserShown(t *testing.T) {
	win := newFakeWin(teams(1))
	m := &Manager{Win: win, SelfPID: self}
	st := m.Apply(State{}, Options{Targets: targets, Method: MethodHide})

	// 新しい窓（通話の呼び出しなど）が現れる。利用者が自分で 1 を開き直す
	win.wins[7] = &Window{HWND: 7, PID: 10, Exe: "ms-teams.exe", Visible: true, Width: 300, Height: 100}
	win.wins[1].Visible = true
	st = m.Apply(st, Options{Targets: targets, Method: MethodHide})
	if win.wins[7].Visible || win.wins[1].Visible {
		t.Error("新しい窓・開き直した窓が隠れていない")
	}
	if len(st.Windows) != 2 {
		t.Errorf("記録: %+v", st.Windows)
	}
}

func TestRestoreSkipsReusedHandleAndMissingWindow(t *testing.T) {
	win := newFakeWin(teams(1), teams(2))
	m := &Manager{Win: win, SelfPID: self}
	st := m.Apply(State{}, Options{Targets: targets, Method: MethodHide})

	// 1 は相手が再起動して消えた。2 のハンドルは別のアプリのウィンドウに使い回された（隠れたまま）
	delete(win.wins, 1)
	win.wins[2] = &Window{HWND: 2, PID: 99, Exe: "notepad.exe", Visible: false}
	win.calls = nil
	m.Restore(st)
	if len(win.calls) != 0 {
		t.Errorf("別のウィンドウに触った: %v", win.calls)
	}
}

func TestApplyPrunesStaleRecords(t *testing.T) {
	win := newFakeWin()
	m := &Manager{Win: win, SelfPID: self}
	st := m.Apply(State{Windows: []Hidden{{HWND: 99, Exe: "ms-teams.exe", State: StateNormal, Method: MethodHide}}}, Options{Targets: targets, Method: MethodHide})
	if len(st.Windows) != 0 {
		t.Errorf("存在しないウィンドウの記録が残った: %+v", st.Windows)
	}
}

func TestNeverTouchesOwnProcess(t *testing.T) {
	win := newFakeWin(Window{HWND: 1, PID: self, Exe: "ms-teams.exe", Visible: true})
	m := &Manager{Win: win, SelfPID: self}
	st := m.Apply(State{}, Options{Targets: targets, Method: MethodHide})
	if len(win.calls) != 0 || !st.Empty() {
		t.Errorf("自分のプロセスのウィンドウに触った: %v %+v", win.calls, st)
	}
	win.calls = nil
	m.Restore(State{Windows: []Hidden{{HWND: 1, Exe: "ms-teams.exe", State: StateNormal, Method: MethodHide}}})
	if len(win.calls) != 0 {
		t.Errorf("自分のプロセスのウィンドウを戻した: %v", win.calls)
	}
}

func TestMinimizeMethodRestoresOnlyWhileStillMinimized(t *testing.T) {
	win := newFakeWin(teams(1), teams(2))
	win.wins[2].Minimized = true // もともと最小化
	m := &Manager{Win: win, SelfPID: self}
	st := m.Apply(State{}, Options{Targets: targets, Method: MethodMinimize})
	if len(st.Windows) != 1 || st.Windows[0].HWND != 1 {
		t.Fatalf("もともと最小化のものは記録しない: %+v", st.Windows)
	}
	if !win.wins[1].Minimized || !win.wins[1].Visible {
		t.Error("最小化されていない（隠してはいけない）")
	}
	// 利用者が自分で元に戻していたら、戻す操作はしない
	win.wins[1].Minimized = false
	win.calls = nil
	m.Restore(st)
	if len(win.calls) != 0 {
		t.Errorf("利用者が戻したウィンドウに触った: %v", win.calls)
	}
}

func TestSaveCalledBeforeChanging(t *testing.T) {
	win := newFakeWin(teams(1))
	var saved []State
	var visibleAtSave []bool
	m := &Manager{Win: win, SelfPID: self, Save: func(s State) {
		saved = append(saved, s)
		visibleAtSave = append(visibleAtSave, win.wins[1].Visible)
	}}
	m.Apply(State{}, Options{Targets: targets, Method: MethodHide})
	if len(saved) != 1 || len(saved[0].Windows) != 1 {
		t.Fatalf("Save: %+v", saved)
	}
	if !visibleAtSave[0] {
		t.Error("Save がウィンドウを隠したあとに呼ばれた（クラッシュで記録を失う）")
	}
	m.Apply(saved[0], Options{Targets: targets, Method: MethodHide})
	if len(saved) != 1 {
		t.Error("変化が無い周期で Save した")
	}
}

func TestRestoreUnrecorded(t *testing.T) {
	win := newFakeWin(
		Window{HWND: 1, PID: 10, Exe: "ms-teams.exe", Visible: false, Width: 900, Height: 700, Title: "Microsoft Teams"},     // 隠れた本体 → 戻す
		Window{HWND: 2, PID: 10, Exe: "ms-teams.exe", Visible: false, Width: 1, Height: 1, Title: "t"},                       // 補助 → 戻さない
		Window{HWND: 8, PID: 10, Exe: "ms-teams.exe", Visible: false, Width: 1280, Height: 735},                              // タイトルなし（TeamsMessageWindow）→ 戻さない
		Window{HWND: 3, PID: 10, Exe: "ms-teams.exe", Visible: false, Width: 900, Height: 700, Title: "t", ToolWindow: true}, // ツール → 戻さない
		Window{HWND: 4, PID: 10, Exe: "ms-teams.exe", Visible: false, Width: 900, Height: 700, Title: "t", HasOwner: true},   // 所有者あり → 戻さない
		Window{HWND: 5, PID: 11, Exe: "notepad.exe", Visible: false, Width: 900, Height: 700, Title: "t"},                    // 対象外
		Window{HWND: 6, PID: self, Exe: "ms-teams.exe", Visible: false, Width: 900, Height: 700, Title: "t"},                 // 自分
		teams(7), // すでに見えている
	)
	m := &Manager{Win: win, SelfPID: self}
	if n := m.RestoreUnrecorded(targets); n != 1 {
		t.Errorf("戻した数: %d", n)
	}
	if !win.wins[1].Visible || win.wins[2].Visible || win.wins[3].Visible || win.wins[4].Visible || win.wins[5].Visible || win.wins[6].Visible || win.wins[8].Visible {
		t.Error("戻す対象が違う")
	}
}

// ---- 通知スイッチ ----

func TestNotificationsBackupAndRestore(t *testing.T) {
	reg := &fakeReg{vals: map[string]*uint32{
		"MSTeams_8wekyb3d8bbwe!MSTeams":                 u32(1),
		"Microsoft.OutlookForWindows_8wekyb3d8bbwe!App": nil, // 値が無かった
		"Microsoft.WindowsStore_8wekyb3d8bbwe!App":      u32(1),
		"com.squirrel.Teams.Teams":                      u32(0), // もともと OFF
	}}
	m := &Manager{Win: newFakeWin(), Reg: reg, SelfPID: self}
	o := Options{Targets: targets, Method: MethodHide, Notifications: true}

	st := m.Apply(State{}, o)
	if *reg.vals["MSTeams_8wekyb3d8bbwe!MSTeams"] != 0 || *reg.vals["Microsoft.OutlookForWindows_8wekyb3d8bbwe!App"] != 0 {
		t.Error("対象の通知が止まっていない")
	}
	if *reg.vals["Microsoft.WindowsStore_8wekyb3d8bbwe!App"] != 1 {
		t.Error("無関係なアプリの通知を止めた")
	}
	if len(st.Notif) != 3 {
		t.Fatalf("元の値の記録: %+v", st.Notif)
	}

	// 毎周期の再適用で、元の値の記録が 0 で上書きされない
	reg.writes = nil
	st = m.Apply(st, o)
	if len(reg.writes) != 0 {
		t.Errorf("再適用でレジストリを書いた: %v", reg.writes)
	}

	left := m.Restore(st)
	if !left.Empty() {
		t.Errorf("戻し残し: %+v", left)
	}
	if v := reg.vals["MSTeams_8wekyb3d8bbwe!MSTeams"]; v == nil || *v != 1 {
		t.Error("元の値（1）に戻っていない")
	}
	if reg.vals["Microsoft.OutlookForWindows_8wekyb3d8bbwe!App"] != nil {
		t.Error("元から値が無かったキーは、値を削除して戻す")
	}
	if v := reg.vals["com.squirrel.Teams.Teams"]; v == nil || *v != 0 {
		t.Error("もともと OFF のものは OFF のまま")
	}
}

func TestNotificationsOptionOffLeavesRegistryAlone(t *testing.T) {
	reg := &fakeReg{vals: map[string]*uint32{"MSTeams_x": u32(1)}}
	m := &Manager{Win: newFakeWin(), Reg: reg, SelfPID: self}
	st := m.Apply(State{}, Options{Targets: targets, Method: MethodHide, Notifications: false})
	if len(reg.writes) != 0 || !st.Empty() {
		t.Errorf("通知を触らない設定で書いた: %v %+v", reg.writes, st)
	}
}

func TestNotificationWriteFailureIsNotRecordedAndRestoreFailureIsKept(t *testing.T) {
	reg := &fakeReg{vals: map[string]*uint32{"MSTeams_x": u32(1), "Teams_y": u32(1)}, setFail: map[string]bool{"Teams_y": true}}
	m := &Manager{Win: newFakeWin(), Reg: reg, SelfPID: self}
	st := m.Apply(State{}, Options{Targets: targets, Method: MethodHide, Notifications: true})
	if len(st.Notif) != 1 || st.Notif[0].Key != "MSTeams_x" {
		t.Fatalf("書けなかったキーは記録しない: %+v", st.Notif)
	}
	reg.setFail["MSTeams_x"] = true // 戻すときに失敗
	left := m.Restore(st)
	if len(left.Notif) != 1 {
		t.Errorf("戻せなかった分は記録に残して、次に再試行する: %+v", left)
	}
}

func TestStateJSONKeysAreStable(t *testing.T) {
	// config.json に保存される形。キー名を変えると、旧バージョンが残した記録から戻せなくなる
	s := State{Windows: []Hidden{{HWND: 1, Exe: "a.exe", State: StateMax, Method: MethodHide}}, Notif: []NotifBackup{{Key: "k", Existed: true, Value: 1}}}
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{`"hwnd"`, `"exe"`, `"state"`, `"method"`, `"key"`, `"existed"`, `"value"`} {
		if !strings.Contains(string(b), k) {
			t.Errorf("JSON に %s が無い", k)
		}
	}
}
