package aiusage

import (
	"fmt"
	"image"
	"math"
	"time"
)

// TrayBand はトレイアイコンの背景色（7 日窓のペース判定の結果）。移植した描画コードの型の別名。
type TrayBand = trayBand

// TrayInfo はトレイアイコン（trayDisplay = "usage"）とツールチップに出す内容。
// トレイに出すのは 1 プロバイダーだけで、Settings.TrayProvider が決める（D9）。
type TrayInfo struct {
	Provider string // "claude" | "codex"
	// Error が真のとき、認証・通信が使えない状態（グレーの "?" アイコン）。
	Error bool
	// Pct5h はアイコンの数字。短期枠（5 時間）の使用率。短期枠の無いプランでは長期枠。
	Pct5h int
	// Band は背景色。長期枠（7 日）のペース判定（緑 / 琥珀 / 赤）。
	Band TrayBand
	// Tooltip はツールチップ（1 行目: 名称、2 行目以降: 使用率など）。
	Tooltip string
}

// IconKey はアイコン画像が変わる条件。同じ値の間は、アイコンを作り直さなくてよい
// （2 秒ごとの作り直しで GDI ハンドルを無駄に消費しないため）。
func (t TrayInfo) IconKey() string {
	if t.Error {
		return t.Provider + ":error"
	}
	return fmt.Sprintf("%s:%d:%d", t.Provider, t.Pct5h, t.Band)
}

// Image はアイコン画像（16x16）を返す。
func (t TrayInfo) Image() *image.RGBA {
	if t.Error {
		return drawTrayIconErrorImage()
	}
	return drawTrayIconImage(t.Pct5h, t.Band)
}

// GetTrayInfo は現在のスナップショットから、トレイに出す内容を組み立てる。
func GetTrayInfo() TrayInfo {
	return buildTrayInfo(snapshotConfig(), getUsageSnapshot(), getCodexUsageSnapshot(), time.Now())
}

func buildTrayInfo(cfg Settings, claude, codex UsageSnapshot, now time.Time) TrayInfo {
	provider, name, snap := "claude", "Claude", claude
	if cfg.TrayProvider == "codex" {
		provider, name, snap = "codex", "Codex", codex
	}
	if snap.AuthState != "ok" {
		return TrayInfo{Provider: provider, Error: true, Tooltip: trayTooltipForError(name, snap)}
	}

	// 短期枠が無いプラン（Codex の週次のみなど）は、長期枠の値を数字に出す。
	primary := snap.FiveHour
	if primary.Label == "" {
		primary = snap.SevenDay
	}
	pct5h := clampPct(primary.Utilization)
	yellow, red := trayPaceThresholds(snap.SevenDay.ResetsAt, now, cfg.TraySplitDays)
	band := trayBandFor(snap.SevenDay.Utilization, yellow, red)

	tip := name + " モニター\n"
	switch {
	case snap.FiveHour.Label != "" && snap.SevenDay.Label != "":
		tip += fmt.Sprintf("%s: %d%% / %s: %d%%", snap.FiveHour.Label, clampPct(snap.FiveHour.Utilization),
			snap.SevenDay.Label, clampPct(snap.SevenDay.Utilization))
	case snap.SevenDay.Label != "":
		tip += fmt.Sprintf("%s: %d%%", snap.SevenDay.Label, clampPct(snap.SevenDay.Utilization))
	default:
		tip += fmt.Sprintf("短期: %d%%", pct5h)
	}
	if snap.CreditBalance != nil {
		tip += " / Credit: " + formatCredits(*snap.CreditBalance)
	}
	if ov := snap.Overage; ov != nil && (ov.AmountUsed > 0 || ov.SpendingLimit != nil) {
		ovStr := fmt.Sprintf("$%.2f", ov.AmountUsed)
		if cfg.OverageTipFormat == "percent" && ov.SpendingLimit != nil && *ov.SpendingLimit > 0 {
			ovStr = fmt.Sprintf("%d%%", min(100, int(math.Round(ov.AmountUsed / *ov.SpendingLimit * 100))))
		}
		tip += " / Eu: " + ovStr
	}
	return TrayInfo{Provider: provider, Pct5h: pct5h, Band: band, Tooltip: tip}
}

func trayTooltipForError(name string, snap UsageSnapshot) string {
	switch snap.AuthState {
	case "needs_login":
		return name + " モニター — 未ログイン\n右クリック → ログイン"
	case "network_error":
		return name + " モニター — 取得失敗\n" + truncateString(snap.LastError, 80)
	case "init":
		return name + " モニター — 取得中…"
	}
	return name + " モニター — エラー"
}

func truncateString(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// ---- トレイのメニューなど、呼び出し側（本体）が使う操作 ----

// Enabled は AI 使用量が ON かを返す。
func Enabled() bool { return snapshotConfig().Enabled }

// RefreshNow は有効なプロバイダーの使用量を今すぐ取得し、障害状況のキャッシュを失効させる。
// 完了まで最大 15 秒かかるので、UI スレッドやトレイのスレッドから同期で呼ばないこと。
func RefreshNow() {
	refreshAllUsage()
	invalidateStatusCache()
	notifyChanged()
}

// LoginNeeded は、有効で、ログインが必要なプロバイダー（"claude" / "codex"）を返す。
func LoginNeeded() []string {
	cfg := snapshotConfig()
	if !cfg.Enabled {
		return nil
	}
	var out []string
	if cfg.ClaudeEnabled && getUsageSnapshot().AuthState == "needs_login" {
		out = append(out, "claude")
	}
	if cfg.CodexEnabled && getCodexUsageSnapshot().AuthState == "needs_login" {
		out = append(out, "codex")
	}
	return out
}

// ShowLogin はプロバイダーのログイン画面を画面内に出す。不明なプロバイダーは何もしない。
func ShowLogin(provider string) {
	switch provider {
	case "claude":
		claudeView.showLogin()
	case "codex":
		codexView.showLogin()
	}
}

// ProviderName は表示用の名前（"Claude" / "Codex"）。
func ProviderName(provider string) string {
	if provider == "codex" {
		return "Codex"
	}
	return "Claude"
}

// Logf は診断ログ（notify.log）に 1 行追記する。
func Logf(format string, args ...any) { notifyLog(format, args...) }

// TestNotify は、通知の経路（バルーン）の確認用に、テスト通知を 1 つ出す。
func TestNotify() {
	defaultBalloon("AI 使用量 — テスト通知", "これはテスト用の通知です。表示されていれば、通知の経路は正常です。", NIIF_INFO)
}
