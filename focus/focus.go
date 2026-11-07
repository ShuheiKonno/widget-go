// Package focus はポモドーロの集中モードで、Teams / Outlook などのウィンドウを隠し、
// アプリごとの通知を止める。戻すための記録（隠したウィンドウ・通知の元の値）も持つ。
//
// Win32 とレジストリは Windows / Registry のインターフェースの向こうに置く（win32.go）。
// このファイルは純粋な部分で、偽の実装でテストできる。
//
// 最大の危険は「隠したまま戻せなくなる」こと。そのため、
//   - 変えるものは、変える前に Manager.Save で記録を保存する（クラッシュしても次の起動で戻せる）。
//   - 戻すのは記録したものだけ。相手が再起動してハンドルが別のウィンドウになっていても、
//     IsWindow とプロセス名の照合に通らなければ触らない。
package focus

import "strings"

// 隠し方（Options.Method）。
const (
	MethodHide     = "hide"     // 非表示（タスクバーにも出ない）
	MethodMinimize = "minimize" // 最小化
)

// 隠す前の状態（Hidden.State）。戻すときにここへ戻す。
const (
	StateNormal = "normal"
	StateMin    = "min"
	StateMax    = "max"
)

// Window は最上位ウィンドウ 1 つの情報。
type Window struct {
	HWND       uintptr
	PID        uint32
	Exe        string // 実行ファイル名（ベース名。例: ms-teams.exe）。取得できなければ空
	Class      string
	Title      string
	Visible    bool
	Minimized  bool
	Maximized  bool
	HasOwner   bool // 所有者がいるウィンドウ（ダイアログなど）
	ToolWindow bool // WS_EX_TOOLWINDOW
	Width      int32
	Height     int32
}

// Windows は Win32 のウィンドウ操作。
type Windows interface {
	// Enumerate は最上位ウィンドウを列挙する。includeHidden が false なら、可視で所有者のいないものだけ。
	Enumerate(includeHidden bool) []Window
	// Lookup は hwnd がいまも有効なウィンドウなら、その情報を返す。
	Lookup(hwnd uintptr) (Window, bool)
	Hide(hwnd uintptr)
	Minimize(hwnd uintptr)
	// Show は、隠す前の状態（StateNormal / StateMin / StateMax）へ戻す。前面には出さない。
	Show(hwnd uintptr, state string)
}

// Registry はアプリごとの通知スイッチ（HKCU\Software\Microsoft\Windows\CurrentVersion\Notifications\Settings）。
type Registry interface {
	Subkeys() []string
	// Enabled は子キーの Enabled の値。値が無ければ ok=false。
	Enabled(sub string) (value uint32, ok bool)
	SetEnabled(sub string, value uint32) error
	DeleteEnabled(sub string) error
}

// Hidden は、集中モードで隠したウィンドウ 1 つの記録。
type Hidden struct {
	HWND   uint64 `json:"hwnd"`
	Exe    string `json:"exe"`
	State  string `json:"state"`  // 隠す前の状態
	Method string `json:"method"` // 隠したときの方法
}

// NotifBackup は、止めた通知スイッチの元の値。
type NotifBackup struct {
	Key     string `json:"key"`     // Notifications\Settings の子キー名
	Existed bool   `json:"existed"` // Enabled の値が元からあったか（無ければ戻すときに削除する）
	Value   uint32 `json:"value"`
}

// State は、いま変えているものの記録（config.json に保存する）。空なら何も変えていない。
type State struct {
	Windows []Hidden      `json:"windows,omitempty"`
	Notif   []NotifBackup `json:"notif,omitempty"`
}

func (s State) Empty() bool { return len(s.Windows) == 0 && len(s.Notif) == 0 }

// Options は集中モードの設定。
type Options struct {
	Targets       []string // 対象のプロセス名（ms-teams.exe など）
	Method        string   // MethodHide | MethodMinimize
	Notifications bool     // 通知スイッチも止める
}

// Manager は隠す・戻すを行う。Win / Reg は差し替え可能。
type Manager struct {
	Win     Windows
	Reg     Registry // nil なら通知には触れない
	SelfPID uint32   // 自分のプロセス（ウィジェット本体のウィンドウは、対象の名前でも触らない）
	// Save は、ウィンドウや通知を実際に変える前に、変えたあとの記録を保存する（任意）。
	Save func(State)
}

// NormalizeExe はプロセス名を比較用にそろえる（小文字・ベース名）。空なら空。
func NormalizeExe(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndexAny(s, `\/`); i >= 0 {
		s = s[i+1:]
	}
	return strings.ToLower(s)
}

// MatchExe は exe が targets のどれかに当たるか（大文字小文字は区別しない）。
func MatchExe(exe string, targets []string) bool {
	e := NormalizeExe(exe)
	if e == "" {
		return false
	}
	for _, t := range targets {
		if NormalizeExe(t) == e {
			return true
		}
	}
	return false
}

// Select は隠す対象を選ぶ: 対象のプロセスの、可視で所有者のいない最上位ウィンドウ（自分のプロセスは除く）。
// 通知のポップアップ（トースト）やシェルのウィンドウは別のプロセスなので、プロセス名で外れる。
func Select(wins []Window, targets []string, selfPID uint32) []Window {
	var out []Window
	for _, w := range wins {
		if !w.Visible || w.HasOwner || w.PID == selfPID || !MatchExe(w.Exe, targets) {
			continue
		}
		out = append(out, w)
	}
	return out
}

func originalState(w Window) string {
	switch {
	case w.Minimized:
		return StateMin
	case w.Maximized:
		return StateMax
	}
	return StateNormal
}

func findHidden(s []Hidden, hwnd uint64) int {
	for i, h := range s {
		if h.HWND == hwnd {
			return i
		}
	}
	return -1
}

func (m *Manager) save(s State) {
	if m.Save != nil {
		m.Save(s)
	}
}

// Apply は集中モードを適用（または、毎周期の差分を適用）して、新しい記録を返す。
//   - 記録のウィンドウが消えている、別のプロセスになっている → 記録から外す（触らない）。
//   - 対象のウィンドウで、まだ記録に無いものは、元の状態を記録して隠す。新しく現れたウィンドウもここで隠れる。
//   - 記録にあって、利用者が自分で表示したウィンドウは、また隠す。
//   - 通知スイッチを止める（元の値は初回だけ記録する）。
func (m *Manager) Apply(s State, o Options) State {
	// 記録の整理
	kept := make([]Hidden, 0, len(s.Windows))
	for _, h := range s.Windows {
		if w, ok := m.Win.Lookup(uintptr(h.HWND)); ok && NormalizeExe(w.Exe) == NormalizeExe(h.Exe) && w.PID != m.SelfPID {
			kept = append(kept, h)
		}
	}
	next := State{Windows: kept, Notif: s.Notif}

	// ウィンドウ: 計画（何を隠すか）
	type op struct {
		hwnd     uintptr
		minimize bool
	}
	var ops []op
	added := false
	for _, w := range Select(m.Win.Enumerate(false), o.Targets, m.SelfPID) {
		hw := uint64(w.HWND)
		if findHidden(next.Windows, hw) < 0 {
			if o.Method == MethodMinimize && w.Minimized {
				continue // すでに最小化されている。何も変えない
			}
			next.Windows = append(next.Windows, Hidden{HWND: hw, Exe: NormalizeExe(w.Exe), State: originalState(w), Method: o.Method})
			added = true
		}
		ops = append(ops, op{w.HWND, o.Method == MethodMinimize})
	}

	// 通知: 計画（元の値の記録）。ここでは読むだけ
	type sw struct{ key string }
	var sets []sw
	if o.Notifications && m.Reg != nil {
		hints := NotifHints(o.Targets)
		for _, sub := range m.Reg.Subkeys() {
			if !matchSub(sub, hints) {
				continue
			}
			v, existed := m.Reg.Enabled(sub)
			if findBackup(next.Notif, sub) < 0 {
				next.Notif = append(next.Notif[:len(next.Notif):len(next.Notif)], NotifBackup{Key: sub, Existed: existed, Value: v})
				added = true
			}
			if !existed || v != 0 {
				sets = append(sets, sw{sub})
			}
		}
	}

	// 変える前に記録を保存する（新しい記録があるときだけ。毎周期は書かない）
	if added || len(kept) != len(s.Windows) {
		m.save(next)
	}
	for _, o2 := range ops {
		if o2.minimize {
			m.Win.Minimize(o2.hwnd)
		} else {
			m.Win.Hide(o2.hwnd)
		}
	}
	// 通知を止められなかったキーは、バックアップを外す（変えていないものを「戻す」ことになるのを避ける）
	for _, k := range sets {
		if err := m.Reg.SetEnabled(k.key, 0); err != nil {
			if i := findBackup(next.Notif, k.key); i >= 0 {
				next.Notif = append(append([]NotifBackup(nil), next.Notif[:i]...), next.Notif[i+1:]...)
			}
		}
	}
	return next
}

// Restore は記録したものを元に戻して、戻せなかった分の記録を返す（通知のレジストリ書き込みの失敗など）。
// ウィンドウは、いまも同じプロセスのウィンドウで、隠した状態のままのものだけを戻す。
func (m *Manager) Restore(s State) State {
	for _, h := range s.Windows {
		w, ok := m.Win.Lookup(uintptr(h.HWND))
		if !ok || NormalizeExe(w.Exe) != NormalizeExe(h.Exe) || w.PID == m.SelfPID {
			continue
		}
		stillHidden := !w.Visible
		if h.Method == MethodMinimize {
			stillHidden = w.Minimized
		}
		if stillHidden {
			m.Win.Show(uintptr(h.HWND), h.State)
		}
	}
	var left State
	if m.Reg != nil {
		for _, b := range s.Notif {
			var err error
			if b.Existed {
				err = m.Reg.SetEnabled(b.Key, b.Value)
			} else {
				err = m.Reg.DeleteEnabled(b.Key)
			}
			if err != nil {
				left.Notif = append(left.Notif, b)
			}
		}
	}
	return left
}

// 記録の無いウィンドウを戻す「表示を戻す」（トレイのメニュー）の候補は、見えていない（隠れている）
// 通常のウィンドウだけ。Teams / Outlook が持つ、見えないままが正しい補助ウィンドウは除く。
// 実機（新しい Teams / Outlook）では、メッセージ用の 1280x735 のウィンドウ（TeamsMessageWindow）が
// タイトルなしで隠れているので、タイトルの無いウィンドウは候補にしない。そのほか、小さい・ツールウィンドウ・
// 所有者ありも除く（IME / DDE / PnP の監視用など）。
const (
	minRevealWidth  = 200
	minRevealHeight = 100
)

// RestoreUnrecorded は、記録が無くても、対象のプロセスの隠れたウィンドウを表示に戻す（取りこぼしの救済）。戻した数を返す。
func (m *Manager) RestoreUnrecorded(targets []string) int {
	n := 0
	for _, w := range m.Win.Enumerate(true) {
		if w.Visible || w.HasOwner || w.ToolWindow || w.Title == "" || w.PID == m.SelfPID || !MatchExe(w.Exe, targets) {
			continue
		}
		if w.Width < minRevealWidth || w.Height < minRevealHeight {
			continue
		}
		m.Win.Show(w.HWND, StateNormal)
		n++
	}
	return n
}

// ---- 通知スイッチの対象 ----

func findBackup(s []NotifBackup, key string) int {
	for i, b := range s {
		if b.Key == key {
			return i
		}
	}
	return -1
}

// NotifHints は、対象のプロセス名から、Notifications\Settings の子キー名（AppUserModelID）に含まれる語を作る。
// AUMID は固定値にしない（版や配布方式で変わる）ので、子キーを列挙して、名前に語を含むものを対象にする。
func NotifHints(targets []string) []string {
	var hints []string
	add := func(h string) {
		for _, x := range hints {
			if x == h {
				return
			}
		}
		hints = append(hints, h)
	}
	for _, t := range targets {
		e := NormalizeExe(t)
		switch e {
		case "ms-teams.exe", "teams.exe":
			add("teams")
		case "olk.exe":
			add("outlookforwindows")
		case "outlook.exe":
			add("outlook")
		default:
			// 短すぎる語（3 文字以下）は、無関係なキーに当たるので使わない
			if base := strings.TrimSuffix(e, ".exe"); len(base) >= 4 {
				add(base)
			}
		}
	}
	return hints
}

func matchSub(sub string, hints []string) bool {
	l := strings.ToLower(sub)
	for _, h := range hints {
		if strings.Contains(l, h) {
			return true
		}
	}
	return false
}
