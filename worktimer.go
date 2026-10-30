package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// 勤務時間タイマー（区画 work）。
//
// 開始・終了の時刻は壁時計の時刻を config.json に保存し、表示する値は読み取りのたびに
// workTimerSnapshot で計算する。毎秒のカウンタ状態は持たないので、再起動しても継続する
// （PC のスリープ中も壁時計で進む）。計算は now と time.Location を引数で受ける純関数。

const (
	workStateIdle    = "idle"
	workStateRunning = "running"
	workStateEnded   = "ended"

	// 計測の最大の長さ。これを超えた計測と、前日に終了した計測は、未開始として扱う
	// （読み取りでは config を書き換えない）。深夜残業は手動で終了するまで継続できる。
	workMaxSpan = 24 * time.Hour

	workMinTargetMinutes = 15
	workMaxTargetMinutes = 23*60 + 59
	workMaxLagMinutes    = 60
	workFloorMinutes     = 15
)

// workTimerClock は現在時刻の取得元。テストで差し替える。
var workTimerClock = time.Now

// errWorkState は、今の状態では受け付けられない操作（二重の開始、未開始での終了など）。HTTP では 409。
var errWorkState = errors.New("今の状態ではこの操作はできません")

// WorkTimerConfig は勤務時間タイマーの設定と、計測中の状態。
type WorkTimerConfig struct {
	TargetMinutes   int    `json:"targetMinutes"`   // 勤務時間（分）。既定 465 = 7:45
	LunchStart      string `json:"lunchStart"`      // 昼休みの始まり "HH:MM"。この間は計測を止める
	LunchEnd        string `json:"lunchEnd"`        // 昼休みの終わり "HH:MM"
	RedAfter        string `json:"redAfter"`        // 勤務開始日のこの時刻以降は赤 "HH:MM"
	ShowFloor       bool   `json:"showFloor"`       // 15 分切り下げの時間も表示する
	StartLagMinutes int    `json:"startLagMinutes"` // 勤務開始の補正（打刻から押すまでの遅れ）。0〜60
	// 以下は計測中の状態。RFC3339、分に切り捨てて保存する。StartedAt が空なら未開始。
	StartedAt string `json:"startedAt"`
	PressedAt string `json:"pressedAt"` // ボタンを押した時刻。手入力で開始したときは空
	EndedAt   string `json:"endedAt"`   // 空なら計測中
}

func defaultWorkTimer() WorkTimerConfig {
	return WorkTimerConfig{
		TargetMinutes:   7*60 + 45,
		LunchStart:      "12:00",
		LunchEnd:        "13:00",
		RedAfter:        "22:00",
		ShowFloor:       true,
		StartLagMinutes: 3,
	}
}

// WorkTimerInfo はフロントへ返す、ある時点の表示用の値。
type WorkTimerInfo struct {
	State      string `json:"state"`      // idle | running | ended
	WorkedMin  int    `json:"workedMin"`  // 実勤務時間（分）
	FlooredMin int    `json:"flooredMin"` // 15 分切り下げ
	Level      string `json:"level"`      // "" | "over"（勤務時間に到達）| "late"（赤表示の時刻以降）
	Paused     bool   `json:"paused"`     // 昼休み中（計測が止まっている）。表示は Level より優先して灰色
	Start      string `json:"start"`      // "HH:MM"。未開始は空
	Pressed    string `json:"pressed"`    // ボタンを押した時刻 "HH:MM"。無ければ空
	End        string `json:"end"`        // 終了した時刻 "HH:MM"。計測中・未開始は空
	AdjustMin  int    `json:"adjustMin"`  // 押下 − 開始（分）。押下が無いときは 0

	// 設定（フロントが表示に使う）
	TargetMin   int    `json:"targetMin"`
	LunchStart  string `json:"lunchStart"`
	LunchEnd    string `json:"lunchEnd"`
	ShowFloor   bool   `json:"showFloor"`
	StartLagMin int    `json:"startLagMin"`
}

// parseClock は "H:MM" / "HH:MM" を 0 時からの分に変える。
func parseClock(s string) (int, bool) {
	h, m, ok := strings.Cut(s, ":")
	if !ok || len(m) != 2 || len(h) < 1 || len(h) > 2 || !isDigits(h) || !isDigits(m) {
		return 0, false
	}
	hh, _ := strconv.Atoi(h)
	mm, _ := strconv.Atoi(m)
	if hh > 23 || mm > 59 {
		return 0, false
	}
	return hh*60 + mm, true
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}

func formatClock(min int) string { return fmt.Sprintf("%02d:%02d", min/60, min%60) }

// normalizeClock は "H:MM" を "HH:MM" に揃える。不正なら fallback。
func normalizeClock(s, fallback string) string {
	if m, ok := parseClock(s); ok {
		return formatClock(m)
	}
	return fallback
}

// normalizeWorkTimer は手編集や旧バージョンで範囲外・欠損になった値を安全な値へ矯正する。
func normalizeWorkTimer(w WorkTimerConfig) WorkTimerConfig {
	def := defaultWorkTimer()
	if w.TargetMinutes < workMinTargetMinutes || w.TargetMinutes > workMaxTargetMinutes {
		w.TargetMinutes = def.TargetMinutes
	}
	ls, lsOK := parseClock(w.LunchStart)
	le, leOK := parseClock(w.LunchEnd)
	if lsOK && leOK && le > ls {
		w.LunchStart, w.LunchEnd = formatClock(ls), formatClock(le)
	} else {
		w.LunchStart, w.LunchEnd = def.LunchStart, def.LunchEnd
	}
	w.RedAfter = normalizeClock(w.RedAfter, def.RedAfter)
	w.StartLagMinutes = min(max(w.StartLagMinutes, 0), workMaxLagMinutes)

	// 読めない時刻は捨てる。開始が無ければ押下も終了も意味がない。終了が開始より前なら捨てる（計測中に戻る）。
	loc := time.Local
	start, ok := parseStamp(w.StartedAt, loc)
	if !ok {
		w.StartedAt, w.PressedAt, w.EndedAt = "", "", ""
		return w
	}
	if _, ok := parseStamp(w.PressedAt, loc); !ok {
		w.PressedAt = ""
	}
	if end, ok := parseStamp(w.EndedAt, loc); !ok || end.Before(start) {
		w.EndedAt = ""
	}
	return w
}

func parseStamp(s string, loc *time.Location) (time.Time, bool) {
	if s == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, false
	}
	return t.In(loc), true
}

func formatStamp(t time.Time) string { return t.Format(time.RFC3339) }

// truncMinute は秒以下を捨てる（t の時刻帯のまま）。
func truncMinute(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), 0, 0, t.Location())
}

func atClock(day time.Time, minutes int) time.Time {
	return time.Date(day.Year(), day.Month(), day.Day(), minutes/60, minutes%60, 0, 0, day.Location())
}

func laterOf(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

func earlierOf(a, b time.Time) time.Time {
	if b.Before(a) {
		return b
	}
	return a
}

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

// effective は now の時点での状態を返す。ref は計算の基準になる時刻（計測中は now、終了後は終了時刻）。
// 24 時間を超えた計測と、前日に終了した計測は未開始として扱う。
func (w WorkTimerConfig) effective(now time.Time) (start, ref time.Time, state string) {
	loc := now.Location()
	start, ok := parseStamp(w.StartedAt, loc)
	if !ok {
		return time.Time{}, time.Time{}, workStateIdle
	}
	if end, ok := parseStamp(w.EndedAt, loc); ok {
		if !sameDay(end, now) {
			return time.Time{}, time.Time{}, workStateIdle
		}
		return start, laterOf(end, start), workStateEnded
	}
	if now.Sub(start) > workMaxSpan {
		return time.Time{}, time.Time{}, workStateIdle
	}
	// 時計が戻されたときなど、開始が未来に見える場合は 0 分として扱う
	return start, laterOf(truncMinute(now), start), workStateRunning
}

// lunchOverlap は [start, ref] と、日ごとの昼休みの重なりの合計。
func lunchOverlap(start, ref time.Time, lunchStart, lunchEnd int) time.Duration {
	var total time.Duration
	day := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, start.Location())
	for ; !day.After(ref); day = day.AddDate(0, 0, 1) {
		lo := laterOf(start, atClock(day, lunchStart))
		hi := earlierOf(ref, atClock(day, lunchEnd))
		if hi.After(lo) {
			total += hi.Sub(lo)
		}
	}
	return total
}

// workTimerSnapshot は now の時点の表示用の値を計算する。
func workTimerSnapshot(w WorkTimerConfig, now time.Time) *WorkTimerInfo {
	info := &WorkTimerInfo{
		State:       workStateIdle,
		TargetMin:   w.TargetMinutes,
		LunchStart:  w.LunchStart,
		LunchEnd:    w.LunchEnd,
		ShowFloor:   w.ShowFloor,
		StartLagMin: w.StartLagMinutes,
	}
	start, ref, state := w.effective(now)
	if state == workStateIdle {
		return info
	}
	info.State = state
	ls, lsOK := parseClock(w.LunchStart)
	le, leOK := parseClock(w.LunchEnd)
	var overlap time.Duration
	if lsOK && leOK {
		overlap = lunchOverlap(start, ref, ls, le)
		if state == workStateRunning {
			info.Paused = !ref.Before(atClock(ref, ls)) && ref.Before(atClock(ref, le))
		}
	}
	worked := int((ref.Sub(start) - overlap) / time.Minute)
	info.WorkedMin = max(worked, 0)
	info.FlooredMin = info.WorkedMin / workFloorMinutes * workFloorMinutes

	// 赤は勤務開始日の指定時刻以降（日付をまたいでも赤のまま）。黄は実勤務時間が勤務時間に達したとき。
	if red, ok := parseClock(w.RedAfter); ok && !ref.Before(atClock(start, red)) {
		info.Level = "late"
	} else if info.WorkedMin >= w.TargetMinutes {
		info.Level = "over"
	}

	info.Start = start.Format("15:04")
	if state == workStateEnded {
		info.End = ref.Format("15:04")
	}
	if p, ok := parseStamp(w.PressedAt, now.Location()); ok {
		info.Pressed = p.Format("15:04")
		info.AdjustMin = int(p.Sub(start) / time.Minute)
	}
	return info
}

// ---- 操作（開始・終了・リセット・開始時刻の変更）----

// start は「勤務開始」。開始時刻は押した時刻 − 補正。
func (w *WorkTimerConfig) start(now time.Time) error {
	if _, _, st := w.effective(now); st != workStateIdle {
		return errWorkState
	}
	pressed := truncMinute(now)
	w.StartedAt = formatStamp(pressed.Add(-time.Duration(w.StartLagMinutes) * time.Minute))
	w.PressedAt = formatStamp(pressed)
	w.EndedAt = ""
	return nil
}

// stop は「勤務終了」。表示は終了時刻で固定される。
func (w *WorkTimerConfig) stop(now time.Time) error {
	start, ref, st := w.effective(now)
	if st != workStateRunning {
		return errWorkState
	}
	w.EndedAt = formatStamp(laterOf(ref, start))
	return nil
}

func (w *WorkTimerConfig) reset() {
	w.StartedAt, w.PressedAt, w.EndedAt = "", "", ""
}

// setStart は開始時刻を "HH:MM" で指定する。計測中・終了後は開始日に、未開始のときは今日に付ける
// （深夜 0 時を過ぎてから前日の時刻を直せるように）。未開始のときは、この時刻から計測を始める。
// 手入力なので補正はしない。未来、24 時間より前、終了時刻より後は受け付けない。
func (w *WorkTimerConfig) setStart(hhmm string, now time.Time) error {
	m, ok := parseClock(hhmm)
	if !ok {
		return fmt.Errorf("開始時刻は HH:MM の形式で指定してください: %q", hhmm)
	}
	start, ref, st := w.effective(now)
	base := now
	if st != workStateIdle {
		base = start
	}
	t := atClock(base, m)
	if t.After(now) {
		return errors.New("未来の時刻は設定できません")
	}
	if now.Sub(t) > workMaxSpan {
		return errors.New("24 時間より前の時刻は設定できません")
	}
	if st == workStateEnded && t.After(ref) {
		return errors.New("終了時刻より後の時刻は設定できません")
	}
	w.StartedAt = formatStamp(t)
	w.PressedAt = ""
	if st == workStateIdle {
		w.EndedAt = ""
	}
	return nil
}

// nudge は計測中の開始時刻を delta 分ずらす（−1 = 早める、+1 = 遅らせる）。
// 上限は現在時刻、下限は 24 時間前。押下の時刻は変えない（補正の表示が追従する）。
func (w *WorkTimerConfig) nudge(delta int, now time.Time) error {
	start, _, st := w.effective(now)
	if st != workStateRunning {
		return errWorkState
	}
	t := start.Add(time.Duration(delta) * time.Minute)
	if cur := truncMinute(now); t.After(cur) {
		t = cur
	}
	if now.Sub(t) > workMaxSpan {
		return errors.New("24 時間より前の時刻は設定できません")
	}
	w.StartedAt = formatStamp(t)
	return nil
}

// applyWorkTimerAction は /api/worktimer の action を適用する。
func applyWorkTimerAction(w *WorkTimerConfig, action, value string, now time.Time) error {
	switch action {
	case "start":
		return w.start(now)
	case "stop":
		return w.stop(now)
	case "reset":
		w.reset()
		return nil
	case "setstart":
		return w.setStart(value, now)
	case "nudge":
		if value != "-1" && value != "1" {
			return fmt.Errorf("invalid nudge value %q", value)
		}
		d, _ := strconv.Atoi(value)
		return w.nudge(d, now)
	}
	return fmt.Errorf("unknown action %q", action)
}

// workTimerActionHandler は POST /api/worktimer?action=start|stop|reset|setstart|nudge&value=。
// 状態の読み出し・検証・書き込みは mutateConfig の中で一体に行う（並行する操作で古い値を上書きしない）。
// 応答は操作後のスナップショット（フロントが次の周期を待たずに描画する）。
func workTimerActionHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "許可されていないメソッドです", http.StatusMethodNotAllowed)
		return
	}
	q := r.URL.Query()
	action, value := q.Get("action"), q.Get("value")
	now := workTimerClock()
	var err error
	mutateConfig(func(c *Config) {
		// 受け付けられないときは、設定を変えない（正規化された値をそのまま書き戻すだけ）
		next := c.WorkTimer
		if err = applyWorkTimerAction(&next, action, value, now); err == nil {
			c.WorkTimer = next
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
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(workTimerSnapshot(snapshotConfig().WorkTimer, now))
}

// currentWorkTimer は /api/system に載せる値。区画が非表示のときは nil。
// cacheMu を保持する前に呼ぶこと（config のロックと入れ子にしない）。
func currentWorkTimer(now time.Time) *WorkTimerInfo {
	c := snapshotConfig()
	if c.Hidden.Work {
		return nil
	}
	return workTimerSnapshot(c.WorkTimer, now)
}

// ---- 設定 ----

// setWorkTimerOption は /api/setoption の workXxx キーを保存する。不正な値はエラー（設定は変えない）。
//
//	workTarget = HH:MM（勤務時間。0:15〜23:59）
//	workLunch  = HH:MM-HH:MM（始まりと終わりを 1 キーで送る。終わり > 始まり。別キーだと変更の途中で検証に引っかかる）
//	workRed    = HH:MM
//	workFloor  = true | false
//	workLag    = 0〜60（分）
func setWorkTimerOption(key, val string) error {
	switch key {
	case "workTarget":
		m, ok := parseClock(val)
		if !ok || m < workMinTargetMinutes || m > workMaxTargetMinutes {
			return fmt.Errorf("invalid workTarget %q", val)
		}
		mutateConfig(func(c *Config) { c.WorkTimer.TargetMinutes = m })
	case "workLunch":
		a, b, ok := strings.Cut(val, "-")
		ls, lsOK := parseClock(a)
		le, leOK := parseClock(b)
		if !ok || !lsOK || !leOK || le <= ls {
			return fmt.Errorf("invalid workLunch %q", val)
		}
		mutateConfig(func(c *Config) { c.WorkTimer.LunchStart, c.WorkTimer.LunchEnd = formatClock(ls), formatClock(le) })
	case "workRed":
		m, ok := parseClock(val)
		if !ok {
			return fmt.Errorf("invalid workRed %q", val)
		}
		mutateConfig(func(c *Config) { c.WorkTimer.RedAfter = formatClock(m) })
	case "workFloor":
		if val != "true" && val != "false" {
			return fmt.Errorf("invalid workFloor %q", val)
		}
		mutateConfig(func(c *Config) { c.WorkTimer.ShowFloor = val == "true" })
	case "workLag":
		n, err := strconv.Atoi(val)
		if err != nil || n < 0 || n > workMaxLagMinutes {
			return fmt.Errorf("invalid workLag %q", val)
		}
		mutateConfig(func(c *Config) { c.WorkTimer.StartLagMinutes = n })
	default:
		return fmt.Errorf("unknown key %q", key)
	}
	return nil
}
