package aiusage

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jchv/go-webview2"
)

// rawClaudeUsagePayload は JS が JSON.stringify して Bind に渡してくる構造体。
// claude.ai の /usage と /edge-api/bootstrap/.../app_start から JS 側で抽出済み。
type rawClaudeUsagePayload struct {
	FiveHour      *rawClaudeWindow   `json:"fiveHour"`
	SevenDay      *rawClaudeWindow   `json:"sevenDay"`
	Overage       *rawOveragePayload `json:"overage"`
	Email         string             `json:"email"`
	DisplayName   string             `json:"displayName"`
	Capabilities  []string           `json:"capabilities"`
	RateLimitTier string             `json:"rateLimitTier"`
	BillingType   string             `json:"billingType"`
}

type rawClaudeWindow struct {
	Utilization float64 `json:"utilization"`
	ResetsAt    *string `json:"resetsAt"`
}

type rawOveragePayload struct {
	AmountUsed    float64  `json:"amountUsed"`
	SpendingLimit *float64 `json:"spendingLimit"`
	ResetsAt      *string  `json:"resetsAt"`
}

// applyUsagePayload は JS から受け取った正常レスポンスをスナップショットへ反映する。
func applyUsagePayload(p rawClaudeUsagePayload) {
	snap := UsageSnapshot{
		FiveHour:         mapClaudeWindow(p.FiveHour),
		SevenDay:         mapClaudeWindow(p.SevenDay),
		Overage:          mapOverage(p.Overage),
		Email:            p.Email,
		DisplayName:      p.DisplayName,
		SubscriptionType: deriveSubscriptionType(p.Capabilities, p.RateLimitTier),
		Provider:         "claude",
		AuthState:        "ok",
		UpdatedAt:        time.Now(),
	}
	snap.FiveHour.Label = "5時間"
	snap.SevenDay.Label = "7日"
	usageMu.Lock()
	cachedUsage = snap
	usageMu.Unlock()

	// ログイン画面を出していた場合は、取得に成功した時点で画面外へ戻す。
	claudeView.hideLogin()
	notifyChanged()
	handleUsageNotification(snap)
	handleOverageNotification(snap)
}

func mapClaudeWindow(w *rawClaudeWindow) UsageWindow {
	if w == nil {
		return UsageWindow{}
	}
	out := UsageWindow{Utilization: w.Utilization}
	if w.ResetsAt != nil && *w.ResetsAt != "" {
		if t, err := time.Parse(time.RFC3339Nano, *w.ResetsAt); err == nil {
			out.ResetsAt = &t
		} else if t, err := time.Parse(time.RFC3339, *w.ResetsAt); err == nil {
			out.ResetsAt = &t
		}
	}
	return out
}

func mapOverage(o *rawOveragePayload) *OverageInfo {
	if o == nil {
		return nil
	}
	ov := &OverageInfo{
		AmountUsed:    o.AmountUsed,
		SpendingLimit: o.SpendingLimit,
	}
	if o.ResetsAt != nil && *o.ResetsAt != "" {
		if t, err := time.Parse(time.RFC3339Nano, *o.ResetsAt); err == nil {
			ov.ResetsAt = &t
		} else if t, err := time.Parse(time.RFC3339, *o.ResetsAt); err == nil {
			ov.ResetsAt = &t
		}
	}
	return ov
}

// deriveSubscriptionType は capabilities と rate_limit_tier から表示用文字列を導く。
// 例: ["claude_max", "chat"] + "default_claude_max_20x" → "Claude Max 20x"
func deriveSubscriptionType(caps []string, tier string) string {
	has := func(s string) bool {
		for _, c := range caps {
			if c == s {
				return true
			}
		}
		return false
	}
	base := ""
	switch {
	case has("claude_max"):
		base = "Claude Max"
	case has("claude_pro"):
		base = "Claude Pro"
	case has("claude_team") || has("team"):
		base = "Claude Team"
	case has("api"), has("api_individual"):
		base = "API"
	default:
		if len(caps) > 0 {
			base = caps[0]
		}
	}
	if suffix := extractTierMultiplier(tier); suffix != "" {
		if base != "" {
			return base + " " + suffix
		}
		return suffix
	}
	return base
}

// extractTierMultiplier は "default_claude_max_20x" → "20x" のように
// rate_limit_tier 文字列の末尾から N桁数字+x のサフィックスを抽出する。
// マッチしなければ空文字。
func extractTierMultiplier(tier string) string {
	if tier == "" {
		return ""
	}
	last := tier[strings.LastIndex(tier, "_")+1:]
	if len(last) < 2 || last[len(last)-1] != 'x' {
		return ""
	}
	for _, ch := range last[:len(last)-1] {
		if ch < '0' || ch > '9' {
			return ""
		}
	}
	return last
}

// claudeView は claude.ai 用の補助 WebView。
var claudeView *authView

// 初期化の循環参照（取得の反映処理が view を参照する）を避けるため、init で作る。
func init() {
	claudeView = newAuthView(&provider{
		id:          "claude",
		title:       "widget-go Claude Auth",
		dataSubdir:  "AuthWebView2",
		usageURL:    "https://claude.ai/settings/usage",
		loginURL:    "https://claude.ai/login",
		script:      authFetcherScript,
		fetchJS:     "window.__fetchClaudeUsage && window.__fetchClaudeUsage()",
		intervalJS:  "window.__setUsageInterval && window.__setUsageInterval(%d)",
		loginPos:    [2]int32{200, 100},
		enabled:     func(c Settings) bool { return c.ClaudeEnabled },
		reportError: updateUsageError,
		authState:   func() string { return getUsageSnapshot().AuthState },
		reset:       resetClaudeSnapshot,
		bind:        bindClaude,
	})
}

func bindClaude(aw webview2.WebView) {
	bind := func(name string, f any) {
		if err := aw.Bind(name, f); err != nil {
			fmt.Fprintln(os.Stderr, "[aiusage] claude Bind", name, "失敗:", err)
		}
	}
	bind("__postUsageData", func(jsonStr string) {
		var p rawClaudeUsagePayload
		if err := json.Unmarshal([]byte(jsonStr), &p); err != nil {
			updateUsageError("network_error", fmt.Sprintf("レスポンスパース失敗: %v", err))
			claudeView.signalDone()
			return
		}
		applyUsagePayload(p)
		claudeView.signalDone()
	})
	bind("__postLoginRequired", func(reason string) {
		updateUsageError("needs_login", reason)
		claudeView.signalDone()
	})
	bind("__postFetchError", func(msg string) {
		updateUsageError("network_error", msg)
		claudeView.signalDone()
	})
	// バックアップタイマーの間隔(ms)をページ内から問い合わせるための Bind。
	// 初期化スクリプトはドキュメント生成（再ナビゲーション含む）毎に再実行されるため、
	// 固定値を埋め込むと再ログイン後に起動時の値へ戻ってしまう。常に最新の設定値を返す。
	bind("__getUsageIntervalMs", func() int64 {
		return int64(usagePollInterval() / time.Millisecond)
	})
}
