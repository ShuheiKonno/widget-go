package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

var testLoc = time.FixedZone("JST", 9*3600)

// day は 2026-10-30（金）を基準にした日付の時刻。d は日のずれ（-1 = 前日）。
func day(d, h, m int) time.Time { return time.Date(2026, 10, 30+d, h, m, 0, 0, testLoc) }

func stamp(t time.Time) string { return formatStamp(t) }

func cfgWith(mod func(*WorkTimerConfig)) WorkTimerConfig {
	w := defaultWorkTimer()
	if mod != nil {
		mod(&w)
	}
	return w
}

func TestParseClock(t *testing.T) {
	cases := []struct {
		in   string
		want int
		ok   bool
	}{
		{"00:00", 0, true},
		{"7:45", 465, true},
		{"07:45", 465, true},
		{"23:59", 23*60 + 59, true},
		{"24:00", 0, false},
		{"12:60", 0, false},
		{"12:5", 0, false},
		{"12", 0, false},
		{"", 0, false},
		{"+1:00", 0, false},
		{"-1:00", 0, false},
		{"aa:bb", 0, false},
		{"123:00", 0, false},
	}
	for _, c := range cases {
		got, ok := parseClock(c.in)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("parseClock(%q) = %d, %v; want %d, %v", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestWorkTimerSnapshot(t *testing.T) {
	run := func(start time.Time) func(*WorkTimerConfig) {
		return func(w *WorkTimerConfig) { w.StartedAt = stamp(start) }
	}
	cases := []struct {
		name       string
		cfg        WorkTimerConfig
		now        time.Time
		state      string
		worked     int
		floored    int
		level      string
		paused     bool
		start, end string
	}{
		{name: "未開始", cfg: cfgWith(nil), now: day(0, 10, 0), state: workStateIdle},
		{name: "午前の計測", cfg: cfgWith(run(day(0, 9, 3))), now: day(0, 10, 30), state: workStateRunning, worked: 87, floored: 75, start: "09:03"},
		{name: "秒は切り捨てて数える", cfg: cfgWith(run(day(0, 9, 3))), now: day(0, 10, 30).Add(59 * time.Second), state: workStateRunning, worked: 87, floored: 75, start: "09:03"},
		{name: "昼休み中は止まって灰色（paused）", cfg: cfgWith(run(day(0, 9, 3))), now: day(0, 12, 30), state: workStateRunning, worked: 177, floored: 165, paused: true, start: "09:03"},
		{name: "昼休みの始まりちょうども止まる", cfg: cfgWith(run(day(0, 9, 3))), now: day(0, 12, 0), state: workStateRunning, worked: 177, floored: 165, paused: true, start: "09:03"},
		{name: "昼休みの終わりちょうどで再開", cfg: cfgWith(run(day(0, 9, 3))), now: day(0, 13, 0), state: workStateRunning, worked: 177, floored: 165, start: "09:03"},
		{name: "昼休みのあと加算が再開する", cfg: cfgWith(run(day(0, 9, 3))), now: day(0, 13, 10), state: workStateRunning, worked: 187, floored: 180, start: "09:03"},
		{name: "昼休みの後に開始すると控除なし", cfg: cfgWith(run(day(0, 13, 20))), now: day(0, 14, 0), state: workStateRunning, worked: 40, floored: 30, start: "13:20"},
		{name: "昼休み中に開始すると、終わりまでは 0 分", cfg: cfgWith(run(day(0, 12, 30))), now: day(0, 12, 45), state: workStateRunning, worked: 0, paused: true, start: "12:30"},
		{name: "昼休み中に開始すると、終わりから数える", cfg: cfgWith(run(day(0, 12, 30))), now: day(0, 13, 30), state: workStateRunning, worked: 30, floored: 30, start: "12:30"},
		{name: "勤務時間の 1 分前は黄色にならない", cfg: cfgWith(run(day(0, 9, 3))), now: day(0, 17, 47), state: workStateRunning, worked: 464, floored: 450, start: "09:03"},
		{name: "勤務時間ちょうどで黄色", cfg: cfgWith(run(day(0, 9, 3))), now: day(0, 17, 48), state: workStateRunning, worked: 465, floored: 465, level: "over", start: "09:03"},
		{name: "22:00 の 1 分前はまだ黄色", cfg: cfgWith(run(day(0, 9, 3))), now: day(0, 21, 59), state: workStateRunning, worked: 716, floored: 705, level: "over", start: "09:03"},
		{name: "22:00 ちょうどで赤", cfg: cfgWith(run(day(0, 9, 3))), now: day(0, 22, 0), state: workStateRunning, worked: 717, floored: 705, level: "late", start: "09:03"},
		{name: "赤の時刻は設定できる", cfg: cfgWith(func(w *WorkTimerConfig) { run(day(0, 9, 3))(w); w.RedAfter = "19:00" }), now: day(0, 19, 0), state: workStateRunning, worked: 537, floored: 525, level: "late", start: "09:03"},
		{name: "日付をまたいでも赤のまま", cfg: cfgWith(run(day(0, 20, 0))), now: day(1, 0, 30), state: workStateRunning, worked: 270, floored: 270, level: "late", start: "20:00"},
		{name: "ちょうど 24 時間までは計測中（開始日の昼休みの後〜翌日の昼休み 1 回分を引く）", cfg: cfgWith(run(day(0, 13, 30))), now: day(1, 13, 30), state: workStateRunning, worked: 1380, floored: 1380, level: "late", start: "13:30"},
		{name: "24 時間を超えた計測は未開始として扱う", cfg: cfgWith(run(day(0, 13, 30))), now: day(1, 13, 31), state: workStateIdle},
		{name: "開始が未来に見えるときは 0 分", cfg: cfgWith(run(day(0, 10, 0))), now: day(0, 9, 0), state: workStateRunning, worked: 0, start: "10:00"},
		{name: "昼休みが壊れていれば控除しない", cfg: cfgWith(func(w *WorkTimerConfig) { run(day(0, 9, 0))(w); w.LunchStart = "xx" }), now: day(0, 14, 0), state: workStateRunning, worked: 300, floored: 300, start: "09:00"},
		{
			name: "終了後は終了時刻で固定する",
			cfg:  cfgWith(func(w *WorkTimerConfig) { run(day(0, 9, 3))(w); w.EndedAt = stamp(day(0, 18, 5)) }),
			now:  day(0, 20, 0), state: workStateEnded, worked: 482, floored: 480, level: "over", start: "09:03", end: "18:05",
		},
		{
			name: "22 時を過ぎて終了すると、赤のまま固定する",
			cfg:  cfgWith(func(w *WorkTimerConfig) { run(day(0, 9, 3))(w); w.EndedAt = stamp(day(0, 22, 30)) }),
			now:  day(0, 23, 0), state: workStateEnded, worked: 747, floored: 735, level: "late", start: "09:03", end: "22:30",
		},
		{
			name: "前日に終了した計測は未開始として扱う",
			cfg:  cfgWith(func(w *WorkTimerConfig) { run(day(-1, 9, 3))(w); w.EndedAt = stamp(day(-1, 18, 5)) }),
			now:  day(0, 9, 0), state: workStateIdle,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := workTimerSnapshot(c.cfg, c.now)
			if got.State != c.state || got.WorkedMin != c.worked || got.FlooredMin != c.floored ||
				got.Level != c.level || got.Paused != c.paused || got.Start != c.start || got.End != c.end {
				t.Errorf("got %+v\nwant state=%s worked=%d floored=%d level=%q paused=%v start=%q end=%q",
					*got, c.state, c.worked, c.floored, c.level, c.paused, c.start, c.end)
			}
		})
	}
}

// 未開始のときも、フロントが設定の表示に使う値は載る。
func TestWorkTimerSnapshotCarriesSettings(t *testing.T) {
	got := workTimerSnapshot(cfgWith(func(w *WorkTimerConfig) { w.ShowFloor = false; w.StartLagMinutes = 5 }), day(0, 9, 0))
	if got.State != workStateIdle || got.TargetMin != 465 || got.LunchStart != "12:00" || got.LunchEnd != "13:00" || got.ShowFloor || got.StartLagMin != 5 {
		t.Errorf("設定が載っていない: %+v", *got)
	}
}

func TestWorkTimerAdjustAndPressed(t *testing.T) {
	cfg := cfgWith(func(w *WorkTimerConfig) {
		w.StartedAt = stamp(day(0, 9, 3))
		w.PressedAt = stamp(day(0, 9, 6))
	})
	got := workTimerSnapshot(cfg, day(0, 10, 0))
	if got.Pressed != "09:06" || got.AdjustMin != 3 || got.Start != "09:03" {
		t.Errorf("押下と補正が違う: %+v", *got)
	}
	cfg.PressedAt = ""
	if got := workTimerSnapshot(cfg, day(0, 10, 0)); got.Pressed != "" || got.AdjustMin != 0 {
		t.Errorf("手入力の開始は押下を持たない: %+v", *got)
	}
}

func TestWorkTimerStart(t *testing.T) {
	t.Run("押した時刻から補正を引き、秒は切り捨てる", func(t *testing.T) {
		w := cfgWith(nil)
		if err := w.start(day(0, 9, 6).Add(40 * time.Second)); err != nil {
			t.Fatal(err)
		}
		if w.StartedAt != stamp(day(0, 9, 3)) || w.PressedAt != stamp(day(0, 9, 6)) || w.EndedAt != "" {
			t.Errorf("開始 %s 押下 %s", w.StartedAt, w.PressedAt)
		}
	})
	t.Run("補正 0 分なら押した時刻", func(t *testing.T) {
		w := cfgWith(func(w *WorkTimerConfig) { w.StartLagMinutes = 0 })
		if err := w.start(day(0, 9, 6)); err != nil {
			t.Fatal(err)
		}
		if w.StartedAt != stamp(day(0, 9, 6)) {
			t.Errorf("開始 %s", w.StartedAt)
		}
	})
	t.Run("補正で昼休みに入る開始は、そのまま昼休みを控除する", func(t *testing.T) {
		w := cfgWith(nil)
		if err := w.start(day(0, 13, 1)); err != nil { // 12:58 開始
			t.Fatal(err)
		}
		got := workTimerSnapshot(w, day(0, 13, 11))
		if got.Start != "12:58" || got.WorkedMin != 11 {
			t.Errorf("昼休みの 2 分が引かれるはず: %+v", *got)
		}
	})
	t.Run("計測中・終了後（同じ日）は開始できない", func(t *testing.T) {
		w := cfgWith(nil)
		_ = w.start(day(0, 9, 6))
		if err := w.start(day(0, 10, 0)); !errors.Is(err, errWorkState) {
			t.Errorf("二重の開始は errWorkState のはず: %v", err)
		}
		_ = w.stop(day(0, 18, 0))
		if err := w.start(day(0, 19, 0)); !errors.Is(err, errWorkState) {
			t.Errorf("終了後は errWorkState のはず: %v", err)
		}
	})
	t.Run("前日に終了していれば翌朝そのまま開始できる", func(t *testing.T) {
		w := cfgWith(nil)
		_ = w.start(day(-1, 9, 6))
		_ = w.stop(day(-1, 18, 0))
		if err := w.start(day(0, 9, 0)); err != nil {
			t.Fatal(err)
		}
		if w.EndedAt != "" || w.StartedAt != stamp(day(0, 8, 57)) {
			t.Errorf("開始 %s 終了 %s", w.StartedAt, w.EndedAt)
		}
	})
}

func TestWorkTimerStopAndReset(t *testing.T) {
	w := cfgWith(nil)
	if err := w.stop(day(0, 18, 0)); !errors.Is(err, errWorkState) {
		t.Errorf("未開始の終了は errWorkState のはず: %v", err)
	}
	_ = w.start(day(0, 9, 6))
	if err := w.stop(day(0, 18, 0).Add(30 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if w.EndedAt != stamp(day(0, 18, 0)) {
		t.Errorf("終了 %s", w.EndedAt)
	}
	if err := w.stop(day(0, 18, 5)); !errors.Is(err, errWorkState) {
		t.Errorf("二重の終了は errWorkState のはず: %v", err)
	}
	w.reset()
	if w.StartedAt != "" || w.PressedAt != "" || w.EndedAt != "" {
		t.Errorf("リセットで状態が消えるはず: %+v", w)
	}
	// 設定は残る
	if w.TargetMinutes != 465 || w.StartLagMinutes != 3 {
		t.Errorf("リセットで設定を消してはいけない: %+v", w)
	}
}

func TestWorkTimerSetStart(t *testing.T) {
	t.Run("未開始で入力すると、その時刻から計測を始める（補正なし）", func(t *testing.T) {
		w := cfgWith(nil)
		if err := w.setStart("09:00", day(0, 10, 30)); err != nil {
			t.Fatal(err)
		}
		got := workTimerSnapshot(w, day(0, 10, 30))
		if got.State != workStateRunning || got.Start != "09:00" || got.WorkedMin != 90 || got.Pressed != "" {
			t.Errorf("%+v", *got)
		}
	})
	t.Run("計測中の変更は押下を消す", func(t *testing.T) {
		w := cfgWith(nil)
		_ = w.start(day(0, 9, 6))
		if err := w.setStart("8:55", day(0, 10, 0)); err != nil {
			t.Fatal(err)
		}
		if w.StartedAt != stamp(day(0, 8, 55)) || w.PressedAt != "" {
			t.Errorf("開始 %s 押下 %q", w.StartedAt, w.PressedAt)
		}
	})
	t.Run("未来の時刻は拒否し、設定は変えない", func(t *testing.T) {
		w := cfgWith(nil)
		_ = w.start(day(0, 9, 6))
		before := w
		if err := w.setStart("11:00", day(0, 10, 0)); err == nil {
			t.Error("未来は拒否するはず")
		}
		if w != before {
			t.Error("拒否したときは変更しない")
		}
		if err := w.setStart("10:00", day(0, 10, 0)); err != nil {
			t.Errorf("現在の分は受け付けるはず: %v", err)
		}
	})
	t.Run("形式が不正", func(t *testing.T) {
		w := cfgWith(nil)
		for _, in := range []string{"", "9", "25:00", "09:60", "9:5", "abc"} {
			if err := w.setStart(in, day(0, 10, 0)); err == nil {
				t.Errorf("setStart(%q) は拒否するはず", in)
			}
		}
	})
	t.Run("日付が変わったあとも、開始日の時刻で直せる", func(t *testing.T) {
		w := cfgWith(nil)
		w.StartedAt = stamp(day(0, 23, 5))
		now := day(1, 0, 10)
		if err := w.setStart("23:10", now); err != nil {
			t.Fatalf("開始日の 23:10 は受け付けるはず: %v", err)
		}
		if w.StartedAt != stamp(day(0, 23, 10)) {
			t.Errorf("開始 %s", w.StartedAt)
		}
		// 開始日の 00:05 は 24 時間より前になる
		if err := w.setStart("00:05", now); err == nil {
			t.Error("24 時間より前は拒否するはず")
		}
	})
	t.Run("未開始のときは今日に付く（深夜に前日の時刻は未来扱い）", func(t *testing.T) {
		w := cfgWith(nil)
		if err := w.setStart("23:05", day(1, 0, 10)); err == nil {
			t.Error("今日の 23:05 は未来なので拒否するはず")
		}
	})
	t.Run("終了後は終了時刻より後を拒否する", func(t *testing.T) {
		w := cfgWith(nil)
		_ = w.start(day(0, 9, 6))
		_ = w.stop(day(0, 18, 0))
		if err := w.setStart("18:30", day(0, 19, 0)); err == nil {
			t.Error("終了より後は拒否するはず")
		}
		if err := w.setStart("08:50", day(0, 19, 0)); err != nil {
			t.Errorf("終了より前は受け付けるはず: %v", err)
		}
		if w.EndedAt != stamp(day(0, 18, 0)) {
			t.Error("終了時刻は保たれるはず")
		}
	})
}

func TestWorkTimerNudge(t *testing.T) {
	w := cfgWith(nil)
	if err := w.nudge(1, day(0, 9, 0)); !errors.Is(err, errWorkState) {
		t.Errorf("未開始の nudge は errWorkState のはず: %v", err)
	}
	_ = w.start(day(0, 9, 6)) // 開始 09:03、押下 09:06
	if err := w.nudge(-1, day(0, 9, 7)); err != nil {
		t.Fatal(err)
	}
	if w.StartedAt != stamp(day(0, 9, 2)) || w.PressedAt != stamp(day(0, 9, 6)) {
		t.Errorf("開始 %s 押下 %s（押下は変えない）", w.StartedAt, w.PressedAt)
	}
	got := workTimerSnapshot(w, day(0, 9, 7))
	if got.AdjustMin != 4 {
		t.Errorf("補正の表示が追従するはず: %+v", *got)
	}
	// 上限は現在の分
	for i := 0; i < 10; i++ {
		if err := w.nudge(1, day(0, 9, 7)); err != nil {
			t.Fatal(err)
		}
	}
	if w.StartedAt != stamp(day(0, 9, 7)) {
		t.Errorf("現在の分で止まるはず: %s", w.StartedAt)
	}
	// 下限は 24 時間前
	w.StartedAt = stamp(day(0, 9, 7).Add(-24 * time.Hour))
	if err := w.nudge(-1, day(0, 9, 7)); err == nil {
		t.Error("24 時間より前は拒否するはず")
	}
	// 終了後は動かせない
	w2 := cfgWith(nil)
	_ = w2.start(day(0, 9, 6))
	_ = w2.stop(day(0, 18, 0))
	if err := w2.nudge(1, day(0, 18, 1)); !errors.Is(err, errWorkState) {
		t.Errorf("終了後の nudge は errWorkState のはず: %v", err)
	}
}

func TestApplyWorkTimerActionRejectsInvalid(t *testing.T) {
	w := cfgWith(nil)
	for _, c := range [][2]string{{"x", ""}, {"", ""}, {"nudge", "2"}, {"nudge", ""}, {"nudge", "+1"}, {"setstart", ""}} {
		if err := applyWorkTimerAction(&w, c[0], c[1], day(0, 9, 0)); err == nil {
			t.Errorf("action=%q value=%q は拒否するはず", c[0], c[1])
		}
	}
}

func TestNormalizeWorkTimer(t *testing.T) {
	def := defaultWorkTimer()
	t.Run("範囲外は既定へ", func(t *testing.T) {
		got := normalizeWorkTimer(WorkTimerConfig{TargetMinutes: 5, LunchStart: "13:00", LunchEnd: "12:00", RedAfter: "xx", StartLagMinutes: 99, ShowFloor: false})
		if got.TargetMinutes != def.TargetMinutes || got.LunchStart != "12:00" || got.LunchEnd != "13:00" || got.RedAfter != "22:00" || got.StartLagMinutes != 60 || got.ShowFloor {
			t.Errorf("%+v", got)
		}
		if got := normalizeWorkTimer(WorkTimerConfig{TargetMinutes: 2000, StartLagMinutes: -3}); got.TargetMinutes != def.TargetMinutes || got.StartLagMinutes != 0 {
			t.Errorf("%+v", got)
		}
	})
	t.Run("H:MM は HH:MM に揃える", func(t *testing.T) {
		got := normalizeWorkTimer(WorkTimerConfig{TargetMinutes: 465, LunchStart: "9:00", LunchEnd: "9:30", RedAfter: "8:15"})
		if got.LunchStart != "09:00" || got.LunchEnd != "09:30" || got.RedAfter != "08:15" {
			t.Errorf("%+v", got)
		}
	})
	t.Run("読めない時刻は捨てる", func(t *testing.T) {
		got := normalizeWorkTimer(WorkTimerConfig{TargetMinutes: 465, StartedAt: "昨日", PressedAt: stamp(day(0, 9, 6)), EndedAt: stamp(day(0, 18, 0))})
		if got.StartedAt != "" || got.PressedAt != "" || got.EndedAt != "" {
			t.Errorf("開始が読めなければ押下も終了も捨てる: %+v", got)
		}
		got = normalizeWorkTimer(WorkTimerConfig{TargetMinutes: 465, StartedAt: stamp(day(0, 9, 3)), PressedAt: "x", EndedAt: stamp(day(0, 8, 0))})
		if got.StartedAt == "" || got.PressedAt != "" || got.EndedAt != "" {
			t.Errorf("押下が読めなければ捨てる。開始より前の終了も捨てる: %+v", got)
		}
	})
}

func TestLoadConfigWorkTimerDefaults(t *testing.T) {
	t.Run("workTimer キーなし（旧バージョン）は既定のまま", func(t *testing.T) {
		withConfigFile(t, configV180)
		if got := snapshotConfig().WorkTimer; got != defaultWorkTimer() {
			t.Errorf("%+v", got)
		}
	})
	t.Run("一部のキーだけでも、残りは既定が残る", func(t *testing.T) {
		withConfigFile(t, `{"workTimer": {"targetMinutes": 480}}`)
		got := snapshotConfig().WorkTimer
		if got.TargetMinutes != 480 || got.LunchStart != "12:00" || !got.ShowFloor || got.StartLagMinutes != 3 {
			t.Errorf("%+v", got)
		}
	})
	t.Run("昼休みの前後が逆なら、2 つとも既定へ", func(t *testing.T) {
		withConfigFile(t, `{"workTimer": {"lunchStart": "14:00", "lunchEnd": "13:00"}}`)
		if got := snapshotConfig().WorkTimer; got.LunchStart != "12:00" || got.LunchEnd != "13:00" {
			t.Errorf("%+v", got)
		}
	})
	t.Run("区画は既定で非表示・折りたたみなし・pomodoro の直前", func(t *testing.T) {
		withConfigFile(t, "")
		c := snapshotConfig()
		if !c.Hidden.Work || c.Collapsed.Work || c.Order[len(c.Order)-3] != "work" || c.Order[len(c.Order)-2] != "pomodoro" || c.Order[len(c.Order)-1] != "ping" {
			t.Errorf("hidden=%v collapsed=%v order=%v", c.Hidden.Work, c.Collapsed.Work, c.Order)
		}
	})
	t.Run("hidden.work を ON にした設定は保たれる", func(t *testing.T) {
		withConfigFile(t, `{"hidden": {"work": false}}`)
		if snapshotConfig().Hidden.Work {
			t.Error("false は表示として保たれるはず")
		}
	})
}

func TestSetWorkTimerOption(t *testing.T) {
	withConfigFile(t, "")
	good := [][2]string{{"workTarget", "08:00"}, {"workLunch", "11:30-12:30"}, {"workRed", "21:00"}, {"workFloor", "false"}, {"workLag", "5"}}
	for _, c := range good {
		if err := setWorkTimerOption(c[0], c[1]); err != nil {
			t.Errorf("setWorkTimerOption(%q, %q): %v", c[0], c[1], err)
		}
	}
	loadConfig() // 保存されて再読込でも保たれる
	got := snapshotConfig().WorkTimer
	if got.TargetMinutes != 480 || got.LunchStart != "11:30" || got.LunchEnd != "12:30" || got.RedAfter != "21:00" || got.ShowFloor || got.StartLagMinutes != 5 {
		t.Errorf("%+v", got)
	}
	bad := [][2]string{
		{"workTarget", "00:10"}, {"workTarget", "abc"}, {"workTarget", ""},
		{"workLunch", "13:00-12:00"}, {"workLunch", "12:00-12:00"}, {"workLunch", "12:00"}, {"workLunch", "12:00-xx"},
		{"workRed", "25:00"}, {"workFloor", "yes"}, {"workLag", "61"}, {"workLag", "-1"}, {"workLag", "x"}, {"workNope", "1"},
	}
	for _, c := range bad {
		if err := setWorkTimerOption(c[0], c[1]); err == nil {
			t.Errorf("setWorkTimerOption(%q, %q) は拒否するはず", c[0], c[1])
		}
	}
	if snapshotConfig().WorkTimer != got {
		t.Error("エラー時は設定を変更しない")
	}
}

// 昼休みは 1 キーで始まりと終わりを送る。12:00-13:00 → 13:30-14:30 のように、始まりが今の終わりより後でも通る。
func TestSetWorkTimerOptionLunchMovesLater(t *testing.T) {
	withConfigFile(t, "")
	if err := setWorkTimerOption("workLunch", "13:30-14:30"); err != nil {
		t.Fatal(err)
	}
	if got := snapshotConfig().WorkTimer; got.LunchStart != "13:30" || got.LunchEnd != "14:30" {
		t.Errorf("%+v", got)
	}
}

func fixClock(t *testing.T, now time.Time) {
	t.Helper()
	old := workTimerClock
	workTimerClock = func() time.Time { return now }
	t.Cleanup(func() { workTimerClock = old })
}

func postWorkTimer(query string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	workTimerActionHandler(rec, httptest.NewRequest(http.MethodPost, "/api/worktimer"+query, nil))
	return rec
}

func TestWorkTimerHandler(t *testing.T) {
	withConfigFile(t, "")
	fixClock(t, day(0, 9, 6))

	rec := httptest.NewRecorder()
	workTimerActionHandler(rec, httptest.NewRequest(http.MethodGet, "/api/worktimer?action=start", nil))
	if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != http.MethodPost {
		t.Errorf("GET は 405 のはず: %d", rec.Code)
	}
	if snapshotConfig().WorkTimer.StartedAt != "" {
		t.Error("GET で状態が変わった")
	}

	if rec := postWorkTimer("?action=nope"); rec.Code != http.StatusBadRequest {
		t.Errorf("未知の action は 400 のはず: %d", rec.Code)
	}
	if rec := postWorkTimer("?action=stop"); rec.Code != http.StatusConflict {
		t.Errorf("未開始の終了は 409 のはず: %d", rec.Code)
	}

	rec = postWorkTimer("?action=start")
	if rec.Code != http.StatusOK {
		t.Fatalf("start: %d %s", rec.Code, rec.Body.String())
	}
	var info WorkTimerInfo
	if err := json.Unmarshal(rec.Body.Bytes(), &info); err != nil {
		t.Fatal(err)
	}
	if info.State != workStateRunning || info.Start != "09:03" || info.Pressed != "09:06" || info.AdjustMin != 3 {
		t.Errorf("応答が操作後のスナップショットになっていない: %+v", info)
	}
	if snapshotConfig().WorkTimer.StartedAt != stamp(day(0, 9, 3)) {
		t.Error("config に保存されていない")
	}
	if rec := postWorkTimer("?action=start"); rec.Code != http.StatusConflict {
		t.Errorf("二重の開始は 409 のはず: %d", rec.Code)
	}

	fixClock(t, day(0, 10, 0))
	if rec := postWorkTimer("?action=setstart&value=11:00"); rec.Code != http.StatusBadRequest {
		t.Errorf("未来の setstart は 400 のはず: %d", rec.Code)
	}
	if snapshotConfig().WorkTimer.StartedAt != stamp(day(0, 9, 3)) {
		t.Error("拒否したのに状態が変わった")
	}
	if rec := postWorkTimer("?action=setstart&value=09:00"); rec.Code != http.StatusOK {
		t.Errorf("setstart: %d %s", rec.Code, rec.Body.String())
	}
	if rec := postWorkTimer("?action=nudge&value=1"); rec.Code != http.StatusOK {
		t.Errorf("nudge: %d %s", rec.Code, rec.Body.String())
	}
	if rec := postWorkTimer("?action=nudge&value=5"); rec.Code != http.StatusBadRequest {
		t.Errorf("不正な nudge は 400 のはず: %d", rec.Code)
	}
	if got := snapshotConfig().WorkTimer.StartedAt; got != stamp(day(0, 9, 1)) {
		t.Errorf("開始 %s", got)
	}

	fixClock(t, day(0, 18, 0))
	if rec := postWorkTimer("?action=stop"); rec.Code != http.StatusOK {
		t.Errorf("stop: %d", rec.Code)
	}
	if rec := postWorkTimer("?action=reset"); rec.Code != http.StatusOK {
		t.Errorf("reset: %d", rec.Code)
	}
	if got := snapshotConfig().WorkTimer; got.StartedAt != "" || got.EndedAt != "" {
		t.Errorf("リセットされていない: %+v", got)
	}

	// 状態の書き込みが設定（勤務時間・補正）を壊さない
	if got := snapshotConfig().WorkTimer; got.TargetMinutes != 465 || got.StartLagMinutes != 3 {
		t.Errorf("設定が変わった: %+v", got)
	}
}

// 計測の状態は config.json に保存され、再読込でも継続する。
func TestWorkTimerSurvivesReload(t *testing.T) {
	withConfigFile(t, "")
	fixClock(t, day(0, 9, 6))
	if rec := postWorkTimer("?action=start"); rec.Code != http.StatusOK {
		t.Fatal(rec.Code)
	}
	loadConfig()
	got := workTimerSnapshot(snapshotConfig().WorkTimer, day(0, 10, 3))
	if got.State != workStateRunning || got.WorkedMin != 60 {
		t.Errorf("%+v", *got)
	}
}

func TestCurrentWorkTimerHiddenByDefault(t *testing.T) {
	withConfigFile(t, "")
	now := day(0, 10, 0)
	if currentWorkTimer(now) != nil {
		t.Error("既定は非表示なので /api/system には載せない（null）")
	}
	if err := setHidden("work", "false"); err != nil {
		t.Fatal(err)
	}
	if got := currentWorkTimer(now); got == nil || got.State != workStateIdle {
		t.Errorf("表示にすると載るはず: %+v", got)
	}
}
