package aiusage

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/jchv/go-webview2"
)

// rawCodexUsagePayload は JS が JSON.stringify して Bind に渡してくる構造体。
type rawCodexUsagePayload struct {
	Usage       codexUsageResponse `json:"usage"`
	Email       string             `json:"email"`
	DisplayName string             `json:"displayName"`
}

func resetCodexSnapshot() {
	codexUsageMu.Lock()
	cachedCodexUsage = UsageSnapshot{Provider: "codex", AuthState: "init"}
	codexUsageMu.Unlock()
}

// codexView は chatgpt.com 用の補助 WebView。Codex CLI を介さずにログインと使用量取得を行う。
var codexView *authView

// 初期化の循環参照（取得の反映処理が view を参照する）を避けるため、init で作る。
func init() {
	codexView = newAuthView(&provider{
		id:          "codex",
		title:       "widget-go Codex Auth",
		dataSubdir:  "CodexAuthWebView2",
		usageURL:    "https://chatgpt.com/codex/settings/usage",
		loginURL:    "https://chatgpt.com/auth/login",
		script:      codexAuthFetcherScript,
		fetchJS:     "window.__fetchCodexUsage && window.__fetchCodexUsage()",
		intervalJS:  "window.__setCodexUsageInterval && window.__setCodexUsageInterval(%d)",
		loginPos:    [2]int32{220, 100},
		enabled:     func(c Settings) bool { return c.CodexEnabled },
		reportError: updateCodexUsageError,
		reset:       resetCodexSnapshot,
		bind:        bindCodex,
	})
}

func bindCodex(aw webview2.WebView) {
	bind := func(name string, f any) {
		if err := aw.Bind(name, f); err != nil {
			fmt.Fprintln(os.Stderr, "[aiusage] codex Bind", name, "失敗:", err)
		}
	}
	bind("__postCodexUsageData", func(jsonStr string) {
		var p rawCodexUsagePayload
		if err := json.Unmarshal([]byte(jsonStr), &p); err != nil {
			updateCodexUsageError("network_error", fmt.Sprintf("レスポンス解析失敗: %v", err))
			codexView.signalDone()
			return
		}
		applyCodexUsageIdentity(p.Usage, p.Email, p.DisplayName)
		codexView.hideLogin()
		codexView.signalDone()
	})
	bind("__postCodexLoginRequired", func(reason string) {
		updateCodexUsageError("needs_login", reason)
		codexView.signalDone()
	})
	bind("__postCodexFetchError", func(msg string) {
		updateCodexUsageError("network_error", msg)
		codexView.signalDone()
	})
	bind("__getCodexUsageIntervalMs", func() int64 {
		return int64(usagePollInterval() / time.Millisecond)
	})
}
