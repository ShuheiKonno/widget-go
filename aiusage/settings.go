package aiusage

import (
	"math"
	"time"
)

// Settings は AI 使用量機能の設定。widget-go の config.json の "ai" に保存される。
// ウィンドウ位置・最前面・透過は widget-go 側のものを使うので持たない。
type Settings struct {
	// Enabled が false の間は認証 WebView2 を作らず、取得も通知もしない（D2: 既定 OFF）。
	Enabled       bool `json:"enabled"`
	ClaudeEnabled bool `json:"claudeEnabled"`
	CodexEnabled  bool `json:"codexEnabled"`
	// TrayProvider はトレイに出すプロバイダー "claude" | "codex"（D9。旧 ActiveProvider の後継）。
	TrayProvider     string `json:"trayProvider"`
	NotifyUsage      bool   `json:"notifyUsage"`
	NotifyOverage    bool   `json:"notifyOverage"`
	NotifyStatus     bool   `json:"notifyStatus"`
	OverageTipFormat string `json:"overageTipFormat"` // "dollar" | "percent"

	// ResetTimeFormat は 5 時間枠のリセット時刻の表示形式。"datetime"（既定）| "relative"。
	ResetTimeFormat string `json:"resetTimeFormat"`

	// TraySplitDays は 7 日ウィンドウ配色のペース基準日数。7（既定）| 5 | 0（固定 60/80%）。
	TraySplitDays int `json:"traySplitDays"`

	// ポーリング間隔（秒）。[minPollSeconds, maxPollSeconds] にクランプされる。
	UsagePollSeconds  int `json:"usagePollSeconds"`
	StatusPollSeconds int `json:"statusPollSeconds"`

	// 通知済み状態を再起動越しに保持する。同一ウィンドウ（同じ ResetsAt）で既に通知した
	// 閾値を再通知しないため。
	Notify5hResetsAt      time.Time `json:"notify5hResetsAt,omitempty"`
	Notified5h60          bool      `json:"notified5h60,omitempty"`
	Notified5h80          bool      `json:"notified5h80,omitempty"`
	NotifiedOverage60     bool      `json:"notifiedOverage60,omitempty"`
	NotifiedOverage80     bool      `json:"notifiedOverage80,omitempty"`
	OverageResetsAt       time.Time `json:"overageResetsAt,omitempty"`
	CodexNotify5hResetsAt time.Time `json:"codexNotify5hResetsAt,omitempty"`
	CodexNotified5h60     bool      `json:"codexNotified5h60,omitempty"`
	CodexNotified5h80     bool      `json:"codexNotified5h80,omitempty"`
}

// ポーリング間隔の範囲（秒）。
// 60 秒未満は通信負荷が増えるだけで 5h/7d ウィンドウの更新価値がほぼないため下限とする。
// clampPollSeconds で範囲外・0・欠落を矯正し、time.NewTicker / Ticker.Reset が
// 間隔 <= 0 で panic するのを防ぐ。
const (
	minPollSeconds     = 60
	maxPollSeconds     = 3600
	defaultPollSeconds = 300
)

// DefaultSettings は既定の設定を返す。AI 使用量は既定 OFF（D2）。
func DefaultSettings() Settings {
	return Settings{
		Enabled:           false,
		ClaudeEnabled:     true,
		CodexEnabled:      true,
		TrayProvider:      "claude",
		NotifyUsage:       true,
		NotifyOverage:     true,
		NotifyStatus:      true,
		OverageTipFormat:  "dollar",
		ResetTimeFormat:   "datetime",
		TraySplitDays:     7,
		UsagePollSeconds:  defaultPollSeconds,
		StatusPollSeconds: defaultPollSeconds,
	}
}

// NormalizeSettings は手編集や旧バージョンで欠損・範囲外になった値を安全な値へ矯正する。
// config の読み込み後と、設定の更新時に呼ぶ。
func NormalizeSettings(s Settings) Settings {
	s.UsagePollSeconds = clampPollSeconds(s.UsagePollSeconds)
	s.StatusPollSeconds = clampPollSeconds(s.StatusPollSeconds)
	s.TraySplitDays = normalizeTraySplitDays(s.TraySplitDays)
	s.ResetTimeFormat = normalizeResetTimeFormat(s.ResetTimeFormat)
	s.OverageTipFormat = normalizeOverageTipFormat(s.OverageTipFormat)
	s.ClaudeEnabled, s.CodexEnabled, s.TrayProvider = normalizeProviderSelection(
		s.ClaudeEnabled, s.CodexEnabled, s.TrayProvider)
	return s
}

// clampPollSeconds は秒値を [minPollSeconds, maxPollSeconds] に収める。
func clampPollSeconds(v int) int {
	if v < minPollSeconds {
		return minPollSeconds
	}
	if v > maxPollSeconds {
		return maxPollSeconds
	}
	return v
}

// normalizeTraySplitDays は 7/5/0 以外の値（旧バージョンの欠損値や不正値）を既定の 7 に丸める。
func normalizeTraySplitDays(v int) int {
	switch v {
	case 0, 5, 7:
		return v
	default:
		return 7
	}
}

// normalizeResetTimeFormat は "datetime"/"relative" 以外の値を既定の "datetime" に丸める。
func normalizeResetTimeFormat(v string) string {
	switch v {
	case "datetime", "relative":
		return v
	default:
		return "datetime"
	}
}

// normalizeOverageTipFormat は "dollar"/"percent" 以外の値を既定の "dollar" に丸める。
func normalizeOverageTipFormat(v string) string {
	switch v {
	case "dollar", "percent":
		return v
	default:
		return "dollar"
	}
}

// normalizeProviderSelection は、最低 1 つのプロバイダーを有効にし、トレイに出す側が
// 無効なら有効な側へ寄せる。手編集された設定も安全に復旧する。
func normalizeProviderSelection(claudeEnabled, codexEnabled bool, tray string) (bool, bool, string) {
	if !claudeEnabled && !codexEnabled {
		claudeEnabled = true
	}
	if tray == "codex" && codexEnabled {
		return claudeEnabled, codexEnabled, "codex"
	}
	if claudeEnabled {
		return claudeEnabled, codexEnabled, "claude"
	}
	return claudeEnabled, codexEnabled, "codex"
}

// providerEnabled は、設定上そのプロバイダーが有効かを返す（Enabled のマスターは見ない）。
func providerEnabled(c Settings, provider string) bool {
	// ゼロ値の Settings はテストや初期化前に使われるため、両方有効とみなす。
	if !c.ClaudeEnabled && !c.CodexEnabled {
		return true
	}
	if provider == "codex" {
		return c.CodexEnabled
	}
	return c.ClaudeEnabled
}

// clampPct はサーバから返る float の使用率を表示用の整数 % に丸める（0–100 にクランプ）。
func clampPct(v float64) int {
	if math.IsNaN(v) || v <= 0 {
		return 0
	}
	if v >= 100 {
		return 100
	}
	return int(math.Round(v))
}
