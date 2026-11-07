package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"widget-go/focus"
)

// ---- 道具 ----

func pt(day, h, m, s int) time.Time { return time.Date(2026, 11, 7+day, h, m, s, 0, time.Local) }

func fixPomodoroClock(t *testing.T, now time.Time) {
	t.Helper()
	old := pomodoroClock
	pomodoroClock = func() time.Time { return now }
	t.Cleanup(func() { pomodoroClock = old })
}

func newPomo() PomodoroConfig { return normalizePomodoro(defaultPomodoro()) }

func mustAction(t *testing.T, p *PomodoroConfig, action string, now time.Time) {
	t.Helper()
	if err := applyPomodoroAction(p, action, now); err != nil {
		t.Fatalf("%s: %v", action, err)
	}
}

// ---- 設定の矯正 ----

func TestPomodoroDefaults(t *testing.T) {
	p := defaultPomodoro()
	if p.FocusMinutes != 25 || p.ShortBreakMinutes != 5 || p.LongBreakMinutes != 15 || p.LongEvery != 4 ||
		!p.AutoNext || !p.FocusMode || p.HideMethod != focus.MethodHide || p.Phase != pomoIdle {
		t.Errorf("%+v", p)
	}
	want := "ms-teams.exe,teams.exe,olk.exe,outlook.exe"
	if got := strings.Join(p.Targets, ","); got != want {
		t.Errorf("既定の対象: %s", got)
	}
}

func TestNormalizePomodoroClampsOutOfRange(t *testing.T) {
	p := normalizePomodoro(PomodoroConfig{FocusMinutes: 999, ShortBreakMinutes: 0, LongBreakMinutes: -3, LongEvery: 1, HideMethod: "x"})
	d := defaultPomodoro()
	if p.FocusMinutes != d.FocusMinutes || p.ShortBreakMinutes != d.ShortBreakMinutes || p.LongBreakMinutes != d.LongBreakMinutes ||
		p.LongEvery != d.LongEvery || p.HideMethod != focus.MethodHide {
		t.Errorf("%+v", p)
	}
	if len(p.Targets) != len(d.Targets) {
		t.Errorf("targets が null のときは既定: %v", p.Targets)
	}
	// 境界は有効
	p = normalizePomodoro(PomodoroConfig{FocusMinutes: 180, ShortBreakMinutes: 60, LongBreakMinutes: 120, LongEvery: 10, HideMethod: focus.MethodMinimize, Targets: []string{}})
	if p.FocusMinutes != 180 || p.ShortBreakMinutes != 60 || p.LongBreakMinutes != 120 || p.LongEvery != 10 || p.HideMethod != focus.MethodMinimize {
		t.Errorf("%+v", p)
	}
	if p.Targets == nil || len(p.Targets) != 0 {
		t.Errorf("利用者が空にした対象は、空のまま残す: %v", p.Targets)
	}
}

func TestNormalizePomodoroTargets(t *testing.T) {
	in := []string{" MS-Teams.EXE ", `C:\x\Slack`, "slack.exe", "", "bad|name.exe", "a:b", "notepad.exe"}
	got := strings.Join(normalizePomodoroTargets(in), ",")
	if got != "ms-teams.exe,slack.exe,notepad.exe" {
		t.Errorf("got %s", got)
	}
	var many []string
	for i := 0; i < 50; i++ {
		many = append(many, "app"+string(rune('a'+i%26))+string(rune('a'+i/26))+".exe")
	}
	if n := len(normalizePomodoroTargets(many)); n != pomoMaxTargets {
		t.Errorf("上限: %d", n)
	}
}

func TestNormalizePomodoroRepairsBrokenState(t *testing.T) {
	// 動作中なのに期限が読めない → 未開始（記録 Focus は残す）
	p := newPomo()
	p.Phase, p.PhaseEndsAt, p.Round = pomoFocus, "garbage", 2
	p.Focus = focus.State{Windows: []focus.Hidden{{HWND: 5, Exe: "x.exe"}}}
	p = normalizePomodoro(p)
	if p.Phase != pomoIdle || p.Round != 0 || len(p.Focus.Windows) != 1 {
		t.Errorf("%+v", p)
	}
	// 未知の区間
	p = newPomo()
	p.Phase = "weird"
	if normalizePomodoro(p).Phase != pomoIdle {
		t.Error("未知の区間は未開始")
	}
	// 一時停止の残りは 1〜全長、回は 1〜LongEvery
	p = newPomo()
	p.Phase, p.Paused, p.PausedRemainingSec, p.Round = pomoShort, true, 99999, 99
	p = normalizePomodoro(p)
	if p.PausedRemainingSec != 5*60 || p.Round != 4 {
		t.Errorf("%+v", p)
	}
	// 日付が壊れた完了数は 0
	p = newPomo()
	p.DoneToday, p.DoneDate = 7, "yesterday"
	if p = normalizePomodoro(p); p.DoneToday != 0 || p.DoneDate != "" {
		t.Errorf("%+v", p)
	}
}

// ---- 区間の遷移 ----

func TestPomodoroFullCycle(t *testing.T) {
	p := newPomo()
	now := pt(0, 9, 0, 0)
	mustAction(t, &p, "start", now)
	if p.Phase != pomoFocus || p.Round != 1 || p.Paused {
		t.Fatalf("%+v", p)
	}
	if s := pomodoroSnapshot(p, now); s.RemainingSec != 25*60 || s.TotalSec != 25*60 || !s.Running || !s.FocusActive {
		t.Fatalf("%+v", *s)
	}

	type step struct {
		wantPhase string
		wantRound int
	}
	// 集中 1 → 休憩 → 集中 2 → 休憩 → 集中 3 → 休憩 → 集中 4 → 長い休憩 → 集中 1
	steps := []step{
		{pomoShort, 1}, {pomoFocus, 2}, {pomoShort, 2}, {pomoFocus, 3}, {pomoShort, 3}, {pomoFocus, 4}, {pomoLong, 4}, {pomoFocus, 1},
	}
	for i, st := range steps {
		end, _ := p.endsAt(time.Local)
		now = end // 期限ちょうど
		if !p.due(now) {
			t.Fatalf("step %d: 期限ちょうどで due でない", i)
		}
		if _, ok := p.advance(now); !ok {
			t.Fatalf("step %d: advance しない", i)
		}
		if p.Phase != st.wantPhase || p.Round != st.wantRound {
			t.Fatalf("step %d: phase=%s round=%d, want %s/%d", i, p.Phase, p.Round, st.wantPhase, st.wantRound)
		}
	}
	if p.DoneToday != 4 {
		t.Errorf("集中を 4 回終えた: %d", p.DoneToday)
	}
}

func TestPomodoroNotDueBeforeEnd(t *testing.T) {
	p := newPomo()
	mustAction(t, &p, "start", pt(0, 9, 0, 0))
	if p.due(pt(0, 9, 24, 59)) {
		t.Error("期限の 1 秒前は due でない")
	}
	if s := pomodoroSnapshot(p, pt(0, 9, 24, 59)); s.RemainingSec != 1 {
		t.Errorf("残り %d", s.RemainingSec)
	}
	if s := pomodoroSnapshot(p, pt(0, 9, 25, 0)); s.RemainingSec != 0 || s.Running != true {
		t.Errorf("期限の瞬間は残り 0（切り替えは周期が行う）: %+v", *s)
	}
}

// スリープから復帰して期限を大きく過ぎていても、1 回だけ進み、新しい区間は現在時刻から数え直す。
func TestPomodoroAdvancesOnlyOnceAfterSleep(t *testing.T) {
	p := newPomo()
	mustAction(t, &p, "start", pt(0, 9, 0, 0))
	wake := pt(0, 15, 0, 0) // 6 時間後
	ev, ok := p.advance(wake)
	if !ok || ev.From != pomoFocus || ev.To != pomoShort {
		t.Fatalf("%+v %v", ev, ok)
	}
	if _, again := p.advance(wake); again {
		t.Error("過ぎた分を連続して消化した")
	}
	if s := pomodoroSnapshot(p, wake); s.Phase != pomoShort || s.RemainingSec != 5*60 {
		t.Errorf("新しい区間は現在時刻から数える: %+v", *s)
	}
	if p.DoneToday != 1 || p.DoneDate != "2026-11-07" {
		t.Errorf("done=%d date=%s", p.DoneToday, p.DoneDate)
	}
}

func TestPomodoroAutoNextOffWaitsForStart(t *testing.T) {
	p := newPomo()
	p.AutoNext = false
	mustAction(t, &p, "start", pt(0, 9, 0, 0))
	end, _ := p.endsAt(time.Local)
	p.advance(end)
	if p.Phase != pomoShort || !p.Paused || p.PausedRemainingSec != 300 || p.PhaseEndsAt != "" {
		t.Fatalf("次の区間は全長で一時停止のまま用意する: %+v", p)
	}
	if p.due(end.Add(time.Hour)) {
		t.Error("開始待ちは期限切れにならない")
	}
	s := pomodoroSnapshot(p, end)
	if s.Running || !s.Paused || s.FocusActive || s.RemainingSec != 300 {
		t.Errorf("%+v", *s)
	}
	mustAction(t, &p, "resume", end.Add(time.Minute))
	if !p.running() || p.Phase != pomoShort {
		t.Errorf("%+v", p)
	}
}

func TestPomodoroPauseResume(t *testing.T) {
	p := newPomo()
	mustAction(t, &p, "start", pt(0, 9, 0, 0))
	mustAction(t, &p, "pause", pt(0, 9, 10, 30)) // 14 分 30 秒残っている
	if !p.Paused || p.PausedRemainingSec != 14*60+30 || p.PhaseEndsAt != "" {
		t.Fatalf("%+v", p)
	}
	s := pomodoroSnapshot(p, pt(0, 12, 0, 0)) // 一時停止中は何時間たっても減らない
	if s.RemainingSec != 14*60+30 || s.Running || s.FocusActive || s.EndsAtMs != 0 {
		t.Errorf("%+v", *s)
	}
	mustAction(t, &p, "resume", pt(0, 12, 0, 0))
	if end, _ := p.endsAt(time.Local); !end.Equal(pt(0, 12, 14, 30)) {
		t.Errorf("再開後の期限 %v", end)
	}
	if applyPomodoroAction(&p, "resume", pt(0, 12, 1, 0)) == nil {
		t.Error("動作中の再開は拒否")
	}
}

func TestPomodoroActionErrors(t *testing.T) {
	now := pt(0, 9, 0, 0)
	p := newPomo()
	for _, a := range []string{"pause", "resume", "skip", "reveal", "conceal"} {
		if err := applyPomodoroAction(&p, a, now); err != errWorkState {
			t.Errorf("未開始の %s は errWorkState のはず: %v", a, err)
		}
	}
	mustAction(t, &p, "reset", now) // 未開始のリセットは無害
	mustAction(t, &p, "start", now)
	if applyPomodoroAction(&p, "start", now) != errWorkState {
		t.Error("二重の開始は拒否")
	}
	if err := applyPomodoroAction(&p, "bogus", now); err == nil || err == errWorkState {
		t.Errorf("未知の action は別のエラー: %v", err)
	}
}

func TestPomodoroSkipDoesNotCountAndBreakSkipStartsFocus(t *testing.T) {
	now := pt(0, 9, 0, 0)
	p := newPomo()
	mustAction(t, &p, "start", now)
	mustAction(t, &p, "skip", now)
	if p.Phase != pomoShort || p.DoneToday != 0 || !p.running() {
		t.Fatalf("集中のスキップは完了に数えず、休憩を始める: %+v", p)
	}
	mustAction(t, &p, "skip", now)
	if p.Phase != pomoFocus || p.Round != 2 {
		t.Fatalf("休憩のスキップは次の集中: %+v", p)
	}
	// 一時停止中のスキップも、動作中の次の区間になる
	mustAction(t, &p, "pause", now)
	mustAction(t, &p, "skip", now)
	if p.Paused || p.Phase != pomoShort {
		t.Errorf("%+v", p)
	}
}

func TestPomodoroResetKeepsDoneAndSettings(t *testing.T) {
	now := pt(0, 9, 0, 0)
	p := newPomo()
	p.FocusMinutes = 50
	mustAction(t, &p, "start", now)
	end, _ := p.endsAt(time.Local)
	p.advance(end)
	mustAction(t, &p, "reset", end)
	if p.Phase != pomoIdle || p.Round != 0 || p.DoneToday != 1 || p.FocusMinutes != 50 || p.PhaseEndsAt != "" {
		t.Errorf("%+v", p)
	}
	if s := pomodoroSnapshot(p, end); s.Running || s.Paused || s.TotalSec != 50*60 || s.Round != 0 {
		t.Errorf("%+v", *s)
	}
}

func TestPomodoroDoneTodayRollsOverByDate(t *testing.T) {
	p := newPomo()
	p.DoneToday, p.DoneDate = 3, "2026-11-07"
	if s := pomodoroSnapshot(p, pt(0, 23, 59, 0)); s.DoneToday != 3 {
		t.Errorf("同じ日は %d", s.DoneToday)
	}
	if s := pomodoroSnapshot(p, pt(1, 0, 1, 0)); s.DoneToday != 0 {
		t.Errorf("翌日の読み取りは 0（保存し直すのは状態を変えるとき）: %d", s.DoneToday)
	}
	if p.DoneToday != 3 {
		t.Error("読み取りで状態を変えた")
	}
	// 翌日に集中を終えると 1 から数え直す
	mustAction(t, &p, "start", pt(1, 0, 0, 0))
	end, _ := p.endsAt(time.Local)
	p.advance(end)
	if p.DoneToday != 1 || p.DoneDate != "2026-11-08" {
		t.Errorf("%d %s", p.DoneToday, p.DoneDate)
	}
}

// 日付をまたいで終わった集中は、終わった日（期限の日）に数える。
func TestPomodoroDoneCountedOnEndDay(t *testing.T) {
	p := newPomo()
	p.DoneToday, p.DoneDate = 2, "2026-11-07"
	mustAction(t, &p, "start", pt(0, 23, 50, 0))
	p.advance(pt(1, 8, 0, 0)) // 翌朝に復帰。期限は 23:50 + 25 分 = 翌 0:15
	if p.DoneDate != "2026-11-08" || p.DoneToday != 1 {
		t.Errorf("%s %d", p.DoneDate, p.DoneToday)
	}
}

func TestPomodoroRevealIsPerPhase(t *testing.T) {
	now := pt(0, 9, 0, 0)
	p := newPomo()
	mustAction(t, &p, "start", now)
	if !p.wantFocus() {
		t.Fatal("集中中は集中モード")
	}
	mustAction(t, &p, "reveal", now)
	if p.wantFocus() || !pomodoroSnapshot(p, now).Revealed {
		t.Error("一時的に表示している間は隠さない")
	}
	mustAction(t, &p, "conceal", now)
	if !p.wantFocus() {
		t.Error("conceal で戻る")
	}
	mustAction(t, &p, "reveal", now)
	end, _ := p.endsAt(time.Local)
	p.advance(end) // 休憩
	mustAction(t, &p, "skip", end)
	if p.Revealed || !p.wantFocus() {
		t.Error("次の集中では、また隠す")
	}
	p.FocusMode = false
	if p.wantFocus() {
		t.Error("集中モードが OFF なら隠さない")
	}
	if err := applyPomodoroAction(&p, "reveal", now); err != errWorkState {
		t.Errorf("集中モード OFF の reveal は拒否: %v", err)
	}
}

func TestPomodoroWantFocusOnlyWhileRunningFocus(t *testing.T) {
	now := pt(0, 9, 0, 0)
	p := newPomo()
	if p.wantFocus() {
		t.Error("未開始")
	}
	mustAction(t, &p, "start", now)
	mustAction(t, &p, "pause", now)
	if p.wantFocus() {
		t.Error("一時停止中は戻す")
	}
	mustAction(t, &p, "resume", now)
	mustAction(t, &p, "skip", now)
	if p.wantFocus() {
		t.Error("休憩中は戻す")
	}
	mustAction(t, &p, "reset", now)
	if p.wantFocus() {
		t.Error("リセットで戻す")
	}
	c := defaultConfig()
	c.Pomodoro = newPomo()
	mustAction(t, &c.Pomodoro, "start", now)
	if pomodoroWantsFocus(c) {
		t.Error("区画が非表示（既定）の間は働かせない")
	}
	c.Hidden.Pomodoro = false
	if !pomodoroWantsFocus(c) {
		t.Error("表示していて集中中なら働かせる")
	}
}

// ---- API ----

func postPomodoro(query string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	pomodoroActionHandler(rec, httptest.NewRequest(http.MethodPost, "/api/pomodoro"+query, nil))
	return rec
}

func TestPomodoroHandler(t *testing.T) {
	withConfigFile(t, "")
	fixPomodoroClock(t, pt(0, 9, 0, 0))

	rec := httptest.NewRecorder()
	pomodoroActionHandler(rec, httptest.NewRequest(http.MethodGet, "/api/pomodoro?action=start", nil))
	if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != http.MethodPost {
		t.Errorf("GET は 405: %d", rec.Code)
	}
	if rec = postPomodoro("?action=pause"); rec.Code != http.StatusConflict {
		t.Errorf("未開始の一時停止は 409: %d", rec.Code)
	}
	if rec = postPomodoro("?action=bogus"); rec.Code != http.StatusBadRequest {
		t.Errorf("未知の action は 400: %d", rec.Code)
	}
	if rec = postPomodoro("?action=start"); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"phase":"focus"`) ||
		!strings.Contains(rec.Body.String(), `"remainingSec":1500`) {
		t.Errorf("start: %d %s", rec.Code, rec.Body.String())
	}
	if rec = postPomodoro("?action=start"); rec.Code != http.StatusConflict {
		t.Errorf("二重の開始は 409: %d", rec.Code)
	}
	if snapshotConfig().Pomodoro.Phase != pomoFocus {
		t.Error("拒否した操作で状態が変わった / 状態が保存されていない")
	}
	loadConfig() // 再起動しても継続する
	if p := snapshotConfig().Pomodoro; p.Phase != pomoFocus || !p.running() {
		t.Errorf("再読込で状態が失われた: %+v", p)
	}
	if rec = postPomodoro("?action=reset"); rec.Code != http.StatusOK {
		t.Errorf("reset: %d", rec.Code)
	}
}

func TestCurrentPomodoroHiddenByDefault(t *testing.T) {
	withConfigFile(t, "")
	now := pt(0, 9, 0, 0)
	if currentPomodoro(now) != nil {
		t.Error("既定は非表示なので /api/system には載せない（null）")
	}
	if err := setHidden("pomodoro", "false"); err != nil {
		t.Fatal(err)
	}
	if got := currentPomodoro(now); got == nil || got.Phase != pomoIdle || got.FocusMin != 25 {
		t.Errorf("表示にすると載るはず: %+v", got)
	}
}

func TestLoadConfigPomodoroDefaultsForOldConfig(t *testing.T) {
	withConfigFile(t, configV180)
	c := snapshotConfig()
	if !c.Hidden.Pomodoro || c.Collapsed.Pomodoro || c.Order[len(c.Order)-1] != "pomodoro" {
		t.Errorf("hidden=%v collapsed=%v order=%v", c.Hidden.Pomodoro, c.Collapsed.Pomodoro, c.Order)
	}
	if c.Pomodoro.FocusMinutes != 25 || !c.Pomodoro.AutoNext || !c.Pomodoro.FocusMode || len(c.Pomodoro.Targets) != 4 {
		t.Errorf("%+v", c.Pomodoro)
	}
}

func TestLoadConfigPomodoroKeepsUserValues(t *testing.T) {
	withConfigFile(t, `{"hidden": {"pomodoro": false}, "pomodoro": {"focusMinutes": 50, "autoNext": false, "focusMode": false, "targets": ["slack.exe"], "hideMethod": "minimize"}}`)
	c := snapshotConfig()
	p := c.Pomodoro
	if c.Hidden.Pomodoro || p.FocusMinutes != 50 || p.AutoNext || p.FocusMode || p.HideMethod != focus.MethodMinimize ||
		len(p.Targets) != 1 || p.Targets[0] != "slack.exe" || p.ShortBreakMinutes != 5 {
		t.Errorf("hidden=%v %+v", c.Hidden.Pomodoro, p)
	}
}

// ---- 設定キー ----

func TestSetPomodoroOption(t *testing.T) {
	withConfigFile(t, "")
	ok := []struct{ k, v string }{
		{"pomoFocus", "50"}, {"pomoShort", "10"}, {"pomoLong", "30"}, {"pomoEvery", "3"},
		{"pomoAuto", "false"}, {"pomoFocusMode", "false"}, {"pomoHide", "minimize"}, {"pomoTargets", "Slack.exe, teams.exe"},
	}
	for _, c := range ok {
		if _, err := setPomodoroOption(c.k, c.v); err != nil {
			t.Errorf("%s=%s: %v", c.k, c.v, err)
		}
	}
	p := snapshotConfig().Pomodoro
	if p.FocusMinutes != 50 || p.ShortBreakMinutes != 10 || p.LongBreakMinutes != 30 || p.LongEvery != 3 || p.AutoNext || p.FocusMode ||
		p.HideMethod != focus.MethodMinimize || strings.Join(p.Targets, ",") != "slack.exe,teams.exe" {
		t.Errorf("%+v", p)
	}
	before := snapshotConfig().Pomodoro
	for _, c := range []struct{ k, v string }{
		{"pomoFocus", "0"}, {"pomoFocus", "181"}, {"pomoFocus", "x"}, {"pomoShort", "61"}, {"pomoLong", "121"},
		{"pomoEvery", "1"}, {"pomoEvery", "11"}, {"pomoAuto", "yes"}, {"pomoFocusMode", ""}, {"pomoHide", "close"}, {"pomoBogus", "1"},
	} {
		if _, err := setPomodoroOption(c.k, c.v); err == nil {
			t.Errorf("%s=%q は拒否するはず", c.k, c.v)
		}
	}
	if after := snapshotConfig().Pomodoro; strings.Join(after.Targets, ",") != strings.Join(before.Targets, ",") || after.FocusMinutes != before.FocusMinutes {
		t.Error("エラー時に設定が変わった")
	}
	if _, err := setPomodoroOption("pomoTargets", ""); err != nil || len(snapshotConfig().Pomodoro.Targets) != 0 {
		t.Errorf("空にできる: %v", err)
	}
}

func TestSetPomodoroOptionRestoreFlag(t *testing.T) {
	withConfigFile(t, "")
	if r, _ := setPomodoroOption("pomoHide", "minimize"); r {
		t.Error("隠したものが無ければ戻す必要はない")
	}
	mutateConfig(func(c *Config) { c.Pomodoro.Focus = focus.State{Windows: []focus.Hidden{{HWND: 1, Exe: "x.exe"}}} })
	if r, _ := setPomodoroOption("pomoHide", "minimize"); r {
		t.Error("同じ値への変更で戻さない")
	}
	if r, _ := setPomodoroOption("pomoHide", "hide"); !r {
		t.Error("隠す方法が変わったら、戻して適用し直す")
	}
	if r, _ := setPomodoroOption("pomoTargets", "slack.exe"); !r {
		t.Error("対象を変えたら、戻して適用し直す")
	}
}

func TestSetPomodoroEveryClampsRound(t *testing.T) {
	withConfigFile(t, "")
	mutateConfig(func(c *Config) {
		c.Pomodoro.Phase, c.Pomodoro.Round, c.Pomodoro.Paused, c.Pomodoro.PausedRemainingSec = pomoFocus, 4, true, 60
	})
	if _, err := setPomodoroOption("pomoEvery", "2"); err != nil {
		t.Fatal(err)
	}
	if r := snapshotConfig().Pomodoro.Round; r != 2 {
		t.Errorf("回数を減らしたら、いまの回を詰める: %d", r)
	}
}

func TestSettingsViewOmitsStateAndRecords(t *testing.T) {
	p := newPomo()
	p.Focus = focus.State{Windows: []focus.Hidden{{HWND: 1}}}
	v := pomodoroSettingsView(p)
	if v.FocusMinutes != 25 || len(v.Targets) != 4 {
		t.Errorf("%+v", v)
	}
	v.Targets[0] = "changed.exe"
	if p.Targets[0] == "changed.exe" {
		t.Error("設定の配列を共有している")
	}
}
