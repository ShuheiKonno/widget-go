package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"widget-go/focus"
)

// ポモドーロタイマー（区画 pomodoro）。
//
// 区間の期限は壁時計の時刻で config.json に保存し、表示する値は pomodoroSnapshot（now を引数で受ける純関数）で
// 計算する。再起動やスリープをまたいで継続する。勤務時間タイマーと違い、区間の切り替えで副作用
// （集中モードの適用と解除、通知）が起きるので、切り替えは読み取りではなく 1 秒周期の pomodoroLoop が行う。
//
// 状態: Phase = idle | focus | short | long。Paused のときは PhaseEndsAt が空で、PausedRemainingSec に残りを持つ。
// 「次の区間を自動で始めない」設定のときは、次の区間を「全長で一時停止」の状態で用意する（開始 = 再開）。

const (
	pomoIdle  = "idle"
	pomoFocus = "focus"
	pomoShort = "short"
	pomoLong  = "long"

	pomoMinFocus, pomoMaxFocus = 1, 180
	pomoMinShort, pomoMaxShort = 1, 60
	pomoMinLong, pomoMaxLong   = 1, 120
	pomoMinEvery, pomoMaxEvery = 2, 10

	pomoMaxTargets = 20
)

// pomodoroClock は現在時刻の取得元。テストで差し替える。
var pomodoroClock = time.Now

// PomodoroConfig はポモドーロの設定と、区間の状態、集中モードで変えたものの記録。
type PomodoroConfig struct {
	FocusMinutes      int      `json:"focusMinutes"`      // 集中（分）。1〜180
	ShortBreakMinutes int      `json:"shortBreakMinutes"` // 休憩（分）。1〜60
	LongBreakMinutes  int      `json:"longBreakMinutes"`  // 長い休憩（分）。1〜120
	LongEvery         int      `json:"longEvery"`         // 何回の集中ごとに長い休憩か。2〜10
	AutoNext          bool     `json:"autoNext"`          // 次の区間を自動で始める
	FocusMode         bool     `json:"focusMode"`         // 集中の間、対象のアプリを隠して通知を止める
	HideMethod        string   `json:"hideMethod"`        // focus.MethodHide | focus.MethodMinimize
	Targets           []string `json:"targets"`           // 対象のプロセス名

	// 以下は状態。
	Phase              string `json:"phase"`
	Paused             bool   `json:"paused"`
	PhaseEndsAt        string `json:"phaseEndsAt"`        // RFC3339。動作中だけ
	PausedRemainingSec int    `json:"pausedRemainingSec"` // 一時停止中だけ
	Round              int    `json:"round"`              // いま（または次に）何回目の集中か。1〜LongEvery
	DoneToday          int    `json:"doneToday"`
	DoneDate           string `json:"doneDate"` // DoneToday の日付 "2006-01-02"
	Revealed           bool   `json:"revealed"` // この区間の間は隠さない（「一時的に表示する」）。区間が変わると戻る

	// Focus は、集中モードでいま変えているもの（隠したウィンドウ、止めた通知スイッチの元の値）の記録。
	// 異常終了のあとの起動で戻すために、config.json に持つ。
	Focus focus.State `json:"hidden"`
}

func defaultPomodoro() PomodoroConfig {
	return PomodoroConfig{
		FocusMinutes:      25,
		ShortBreakMinutes: 5,
		LongBreakMinutes:  15,
		LongEvery:         4,
		AutoNext:          true,
		FocusMode:         true,
		HideMethod:        focus.MethodHide,
		// 新しい Teams / 従来の Teams、新しい Outlook / 従来の Outlook
		Targets: []string{"ms-teams.exe", "teams.exe", "olk.exe", "outlook.exe"},
		Phase:   pomoIdle,
	}
}

func inRange(n, lo, hi int) bool { return n >= lo && n <= hi }

// normalizePomodoroTargets はプロセス名を、小文字・ベース名・.exe つきにそろえ、重複と空を捨てる。
// 不正な名前（ファイル名に使えない文字を含む）と、上限を超えた分は捨てる。
func normalizePomodoroTargets(in []string) []string {
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, t := range in {
		n := focus.NormalizeExe(t)
		if n == "" || len(n) > 64 || strings.ContainsAny(n, "<>:\"|?*\x00") || strings.ContainsFunc(n, func(r rune) bool { return r < 0x20 }) {
			continue
		}
		if !strings.HasSuffix(n, ".exe") {
			n += ".exe"
		}
		if n == ".exe" || seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
		if len(out) == pomoMaxTargets {
			break
		}
	}
	return out
}

// phaseSec は区間の長さ（秒）。
func (p PomodoroConfig) phaseSec(phase string) int {
	switch phase {
	case pomoFocus:
		return p.FocusMinutes * 60
	case pomoShort:
		return p.ShortBreakMinutes * 60
	case pomoLong:
		return p.LongBreakMinutes * 60
	}
	return 0
}

// normalizePomodoro は手編集や旧バージョンで範囲外・欠損になった値を安全な値へ矯正する。
func normalizePomodoro(p PomodoroConfig) PomodoroConfig {
	def := defaultPomodoro()
	if !inRange(p.FocusMinutes, pomoMinFocus, pomoMaxFocus) {
		p.FocusMinutes = def.FocusMinutes
	}
	if !inRange(p.ShortBreakMinutes, pomoMinShort, pomoMaxShort) {
		p.ShortBreakMinutes = def.ShortBreakMinutes
	}
	if !inRange(p.LongBreakMinutes, pomoMinLong, pomoMaxLong) {
		p.LongBreakMinutes = def.LongBreakMinutes
	}
	if !inRange(p.LongEvery, pomoMinEvery, pomoMaxEvery) {
		p.LongEvery = def.LongEvery
	}
	if p.HideMethod != focus.MethodHide && p.HideMethod != focus.MethodMinimize {
		p.HideMethod = def.HideMethod
	}
	if p.Targets == nil { // キーが無い（null）ときだけ既定。空の一覧は、利用者が空にしたものとして残す
		p.Targets = append([]string(nil), def.Targets...)
	} else {
		p.Targets = normalizePomodoroTargets(p.Targets)
	}

	// 区間の状態。壊れていたら、未開始に戻す（記録 Focus は、戻すために残す）
	reset := func() {
		p.Phase, p.Paused, p.PhaseEndsAt, p.PausedRemainingSec, p.Round, p.Revealed = pomoIdle, false, "", 0, 0, false
	}
	switch p.Phase {
	case pomoFocus, pomoShort, pomoLong:
		p.Round = min(max(p.Round, 1), p.LongEvery)
		if p.Paused {
			p.PhaseEndsAt = ""
			p.PausedRemainingSec = min(max(p.PausedRemainingSec, 1), p.phaseSec(p.Phase))
		} else if _, ok := parseStamp(p.PhaseEndsAt, time.Local); !ok {
			reset()
		} else {
			p.PausedRemainingSec = 0
		}
	default:
		reset()
	}
	if _, err := time.Parse("2006-01-02", p.DoneDate); err != nil || p.DoneToday < 0 {
		p.DoneToday, p.DoneDate = 0, ""
	}
	return p
}

// ---- 区間の計算（純関数）----

func (p PomodoroConfig) running() bool { return p.Phase != pomoIdle && !p.Paused }

// wantFocus は、集中モードを働かせるべき状態か（設定が ON で、集中が動いていて、「一時的に表示」でない）。
func (p PomodoroConfig) wantFocus() bool {
	return p.FocusMode && p.Phase == pomoFocus && !p.Paused && !p.Revealed
}

// begin は区間 phase を、いまから始める（run）か、全長で一時停止のまま用意する。
func (p *PomodoroConfig) begin(phase string, round int, now time.Time, run bool) {
	p.Phase, p.Round, p.Revealed = phase, round, false
	if run {
		p.Paused, p.PausedRemainingSec = false, 0
		p.PhaseEndsAt = formatStamp(now.Add(time.Duration(p.phaseSec(phase)) * time.Second))
	} else {
		p.Paused, p.PhaseEndsAt = true, ""
		p.PausedRemainingSec = p.phaseSec(phase)
	}
}

// afterFocus は集中のあとの区間（長い休憩は、集中が longEvery 回に 1 回）。
func (p PomodoroConfig) afterFocus() string {
	if p.Round >= p.LongEvery {
		return pomoLong
	}
	return pomoShort
}

// afterBreak は休憩のあとの集中の回（長い休憩のあとは 1 に戻る）。
func (p PomodoroConfig) afterBreak() int {
	if p.Phase == pomoLong || p.Round >= p.LongEvery {
		return 1
	}
	return p.Round + 1
}

func (p PomodoroConfig) endsAt(loc *time.Location) (time.Time, bool) {
	return parseStamp(p.PhaseEndsAt, loc)
}

// due は、動作中の区間が期限を過ぎているか。
func (p PomodoroConfig) due(now time.Time) bool {
	if !p.running() {
		return false
	}
	end, ok := p.endsAt(now.Location())
	return ok && !now.Before(end)
}

// pomodoroEvent は区間の切り替え（通知の材料）。
type pomodoroEvent struct{ From, To string }

// advance は期限を過ぎた区間を、次へ 1 回だけ進める。スリープ復帰などで期限を大きく過ぎていても、
// 過ぎた分の区間を連続して消化しない（進めたあとは、新しい区間の長さで、現在時刻から数え直す）。
func (p *PomodoroConfig) advance(now time.Time) (pomodoroEvent, bool) {
	if !p.due(now) {
		return pomodoroEvent{}, false
	}
	end, _ := p.endsAt(now.Location())
	from := p.Phase
	if from == pomoFocus {
		// 完了数は、集中が終わった日に数える（日付をまたいだら 0 から）
		day := end.Format("2006-01-02")
		if p.DoneDate != day {
			p.DoneToday, p.DoneDate = 0, day
		}
		p.DoneToday++
		to := p.afterFocus()
		p.begin(to, p.Round, now, p.AutoNext)
		return pomodoroEvent{from, to}, true
	}
	p.begin(pomoFocus, p.afterBreak(), now, p.AutoNext)
	return pomodoroEvent{from, pomoFocus}, true
}

// PomodoroInfo はフロントへ返す、ある時点の表示用の値。
type PomodoroInfo struct {
	Phase        string `json:"phase"`        // idle | focus | short | long
	Running      bool   `json:"running"`      // 動作中（Phase が idle でなく、一時停止でもない）
	Paused       bool   `json:"paused"`       // 一時停止中（次の区間の開始待ちを含む）
	RemainingSec int    `json:"remainingSec"` // 残り（秒）。期限を過ぎて、まだ切り替わっていないときは 0
	TotalSec     int    `json:"totalSec"`     // 区間の長さ（秒）。idle は集中の長さ
	EndsAtMs     int64  `json:"endsAtMs"`     // 動作中の期限（Unix ミリ秒）。フロントが 1 秒ごとに数えるための値。動作中でなければ 0
	Round        int    `json:"round"`        // 何回目の集中か。idle は 0
	LongEvery    int    `json:"longEvery"`
	DoneToday    int    `json:"doneToday"`
	FocusActive  bool   `json:"focusActive"` // 集中モードが働いている
	Revealed     bool   `json:"revealed"`    // 「一時的に表示する」で、この区間は隠さない
	FocusMode    bool   `json:"focusMode"`   // 設定（集中モード）
	AutoNext     bool   `json:"autoNext"`
	FocusMin     int    `json:"focusMin"`
	ShortMin     int    `json:"shortMin"`
	LongMin      int    `json:"longMin"`
}

// pomodoroSnapshot は now の時点の表示用の値を計算する（読み取りだけ。状態は進めない）。
func pomodoroSnapshot(p PomodoroConfig, now time.Time) *PomodoroInfo {
	info := &PomodoroInfo{
		Phase:     p.Phase,
		Running:   p.running(),
		Paused:    p.Phase != pomoIdle && p.Paused,
		LongEvery: p.LongEvery,
		Round:     p.Round,
		FocusMode: p.FocusMode,
		AutoNext:  p.AutoNext,
		FocusMin:  p.FocusMinutes,
		ShortMin:  p.ShortBreakMinutes,
		LongMin:   p.LongBreakMinutes,
	}
	if p.DoneDate == now.Format("2006-01-02") { // 日付が変わったら 0（保存し直すのは、次に状態を変えるとき）
		info.DoneToday = p.DoneToday
	}
	switch {
	case p.Phase == pomoIdle:
		info.TotalSec = p.phaseSec(pomoFocus)
		info.Round = 0
		return info
	case p.Paused:
		info.RemainingSec = p.PausedRemainingSec
	default:
		if end, ok := p.endsAt(now.Location()); ok {
			info.EndsAtMs = end.UnixMilli()
			if d := end.Sub(now); d > 0 {
				info.RemainingSec = int((d + time.Second - 1) / time.Second) // 切り上げ（0:00 は期限の瞬間）
			}
		}
	}
	info.TotalSec = p.phaseSec(p.Phase)
	info.Revealed = p.Revealed && p.Phase == pomoFocus
	info.FocusActive = p.wantFocus()
	return info
}

// ---- 操作 ----

// applyPomodoroAction は /api/pomodoro の action を適用する。受け付けられない状態では errWorkState。
func applyPomodoroAction(p *PomodoroConfig, action string, now time.Time) error {
	p.advance(now) // 期限を過ぎた区間は、先に進める（一時停止などが古い区間に掛からないように）
	switch action {
	case "start":
		if p.Phase != pomoIdle {
			return errWorkState
		}
		p.begin(pomoFocus, 1, now, true)
	case "pause":
		if !p.running() {
			return errWorkState
		}
		end, ok := p.endsAt(now.Location())
		remain := 1
		if ok {
			remain = max(int((end.Sub(now)+time.Second-1)/time.Second), 1)
		}
		p.Paused, p.PhaseEndsAt, p.PausedRemainingSec = true, "", remain
	case "resume":
		if p.Phase == pomoIdle || !p.Paused {
			return errWorkState
		}
		p.Paused = false
		p.PhaseEndsAt = formatStamp(now.Add(time.Duration(max(p.PausedRemainingSec, 1)) * time.Second))
		p.PausedRemainingSec = 0
	case "skip":
		// 集中のスキップは完了に数えない。休憩のスキップは、次の集中を始める
		switch p.Phase {
		case pomoIdle:
			return errWorkState
		case pomoFocus:
			p.begin(p.afterFocus(), p.Round, now, true)
		default:
			p.begin(pomoFocus, p.afterBreak(), now, true)
		}
	case "reset":
		p.Phase, p.Paused, p.PhaseEndsAt, p.PausedRemainingSec, p.Round, p.Revealed = pomoIdle, false, "", 0, 0, false
	case "reveal", "conceal":
		if !p.running() || p.Phase != pomoFocus || !p.FocusMode {
			return errWorkState
		}
		p.Revealed = action == "reveal"
	default:
		return fmt.Errorf("unknown action %q", action)
	}
	return nil
}

// pomodoroActionHandler は POST /api/pomodoro?action=start|pause|resume|skip|reset|reveal|conceal。
// 応答は操作後のスナップショット。集中モードの適用と解除は pomodoroLoop が（すぐに）行う。
func pomodoroActionHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "許可されていないメソッドです", http.StatusMethodNotAllowed)
		return
	}
	action := r.URL.Query().Get("action")
	now := pomodoroClock()
	var err error
	mutateConfig(func(c *Config) {
		next := c.Pomodoro
		if err = applyPomodoroAction(&next, action, now); err == nil {
			c.Pomodoro = next
		}
	})
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, errWorkState) {
			status = http.StatusConflict
		}
		http.Error(w, err.Error(), status)
		return
	}
	pomodoroKick()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(pomodoroSnapshot(snapshotConfig().Pomodoro, now))
}

// currentPomodoro は /api/system に載せる値。区画が非表示のときは nil。
// cacheMu を保持する前に呼ぶこと（config のロックと入れ子にしない）。
func currentPomodoro(now time.Time) *PomodoroInfo {
	c := snapshotConfig()
	if c.Hidden.Pomodoro {
		return nil
	}
	return pomodoroSnapshot(c.Pomodoro, now)
}

// pomodoroWantsFocus は、いま集中モードを働かせるべきか（区画が非表示のときは働かせない）。
func pomodoroWantsFocus(c Config) bool { return !c.Hidden.Pomodoro && c.Pomodoro.wantFocus() }

// ---- 設定 ----

// setPomodoroOption は /api/setoption の pomoXxx キーを保存する。不正な値はエラー（設定は変えない）。
// 戻り値の restore は、保存の結果、隠したものを一度戻して、あらためて適用し直す必要があるか（隠す方法の変更）。
//
//	pomoFocus / pomoShort / pomoLong = 分（1〜180 / 1〜60 / 1〜120）
//	pomoEvery = 2〜10
//	pomoAuto / pomoFocusMode = true | false
//	pomoHide = hide | minimize
//	pomoTargets = プロセス名をカンマで区切ったもの（最大 20。空にもできる）
func setPomodoroOption(key, val string) (restore bool, err error) {
	atoi := func(lo, hi int) (int, error) {
		n, e := strconv.Atoi(val)
		if e != nil || !inRange(n, lo, hi) {
			return 0, fmt.Errorf("invalid %s %q", key, val)
		}
		return n, nil
	}
	boolean := func() (bool, error) {
		if val != "true" && val != "false" {
			return false, fmt.Errorf("invalid %s %q", key, val)
		}
		return val == "true", nil
	}
	switch key {
	case "pomoFocus":
		n, e := atoi(pomoMinFocus, pomoMaxFocus)
		if e != nil {
			return false, e
		}
		mutateConfig(func(c *Config) { c.Pomodoro.FocusMinutes = n })
	case "pomoShort":
		n, e := atoi(pomoMinShort, pomoMaxShort)
		if e != nil {
			return false, e
		}
		mutateConfig(func(c *Config) { c.Pomodoro.ShortBreakMinutes = n })
	case "pomoLong":
		n, e := atoi(pomoMinLong, pomoMaxLong)
		if e != nil {
			return false, e
		}
		mutateConfig(func(c *Config) { c.Pomodoro.LongBreakMinutes = n })
	case "pomoEvery":
		n, e := atoi(pomoMinEvery, pomoMaxEvery)
		if e != nil {
			return false, e
		}
		mutateConfig(func(c *Config) {
			c.Pomodoro.LongEvery = n
			c.Pomodoro.Round = min(c.Pomodoro.Round, n) // 回数を減らしたとき、回の表示がはみ出さないように
		})
	case "pomoAuto":
		b, e := boolean()
		if e != nil {
			return false, e
		}
		mutateConfig(func(c *Config) { c.Pomodoro.AutoNext = b })
	case "pomoFocusMode":
		b, e := boolean()
		if e != nil {
			return false, e
		}
		mutateConfig(func(c *Config) { c.Pomodoro.FocusMode = b })
	case "pomoHide":
		if val != focus.MethodHide && val != focus.MethodMinimize {
			return false, fmt.Errorf("invalid pomoHide %q", val)
		}
		mutateConfig(func(c *Config) {
			restore = c.Pomodoro.HideMethod != val && !c.Pomodoro.Focus.Empty()
			c.Pomodoro.HideMethod = val
		})
	case "pomoTargets":
		var in []string
		if strings.TrimSpace(val) != "" {
			in = strings.Split(val, ",")
		}
		mutateConfig(func(c *Config) {
			c.Pomodoro.Targets = normalizePomodoroTargets(in)
			restore = !c.Pomodoro.Focus.Empty() // 対象から外したアプリが隠れたままにならないよう、いったん戻す
		})
	default:
		return false, fmt.Errorf("unknown key %q", key)
	}
	return restore, nil
}

// PomodoroSettingsView は /api/settings で設定ウィンドウとウィジェットへ返す、ポモドーロの設定（状態と、隠したものの記録は含めない）。
type PomodoroSettingsView struct {
	FocusMinutes      int      `json:"focusMinutes"`
	ShortBreakMinutes int      `json:"shortBreakMinutes"`
	LongBreakMinutes  int      `json:"longBreakMinutes"`
	LongEvery         int      `json:"longEvery"`
	AutoNext          bool     `json:"autoNext"`
	FocusMode         bool     `json:"focusMode"`
	HideMethod        string   `json:"hideMethod"`
	Targets           []string `json:"targets"`
}

func pomodoroSettingsView(p PomodoroConfig) PomodoroSettingsView {
	return PomodoroSettingsView{
		FocusMinutes: p.FocusMinutes, ShortBreakMinutes: p.ShortBreakMinutes, LongBreakMinutes: p.LongBreakMinutes,
		LongEvery: p.LongEvery, AutoNext: p.AutoNext, FocusMode: p.FocusMode, HideMethod: p.HideMethod,
		Targets: append([]string{}, p.Targets...),
	}
}
