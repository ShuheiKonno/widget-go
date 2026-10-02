package aiusage

import (
	"encoding/json"
	"net/http"
	"os/exec"
	"syscall"
)

// RegisterHandlers は /api/ai/* を mux に登録する。mux は呼び出し側で Host 検証・
// X-Widget-Token の検証（securityMiddleware）の内側に置くこと。状態を変えるものは POST、
// 読み取りのみのものは GET に限る。
func RegisterHandlers(mux *http.ServeMux) {
	mux.HandleFunc("/api/ai/usage", get(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, getAllUsageSnapshots())
	}))
	mux.HandleFunc("/api/ai/status", get(requireEnabled(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, getAllStatusSnapshots())
	})))
	mux.HandleFunc("/api/ai/settings", get(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, currentPayload())
	}))
	mux.HandleFunc("/api/ai/setoption", post(handleSetOption))
	mux.HandleFunc("/api/ai/provider", post(handleSetProvider))
	mux.HandleFunc("/api/ai/refresh", post(requireEnabled(func(w http.ResponseWriter, r *http.Request) {
		// 同期で取得し、完了後の最新のスナップショットを返す。
		// 「更新」を押した直後に古い値を返さないために同期にしている。
		refreshAllUsage()
		invalidateStatusCache()
		writeJSON(w, getAllUsageSnapshots())
	})))
	mux.HandleFunc("/api/ai/relogin", post(requireEnabled(withView(func(w http.ResponseWriter, v *authView) {
		go v.showLogin()
		writeJSON(w, map[string]any{"ok": true})
	}))))
	mux.HandleFunc("/api/ai/logout", post(requireEnabled(withView(func(w http.ResponseWriter, v *authView) {
		v.logout()
		writeJSON(w, map[string]any{"ok": true})
	}))))
	mux.HandleFunc("/api/ai/open-usage", post(handleOpen(map[string]string{
		"claude": "https://claude.ai/settings/usage",
		"codex":  "https://chatgpt.com/codex/settings/usage",
	})))
	mux.HandleFunc("/api/ai/open-status", post(handleOpen(map[string]string{
		"claude": "https://status.claude.com/",
		"codex":  "https://status.openai.com/",
	})))
}

func get(h http.HandlerFunc) http.HandlerFunc  { return method(http.MethodGet, h) }
func post(h http.HandlerFunc) http.HandlerFunc { return method(http.MethodPost, h) }
func method(m string, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != m {
			w.Header().Set("Allow", m)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h(w, r)
	}
}

// requireEnabled は AI 使用量が OFF のときに 409 を返す（外部への通信や補助 WebView の操作を
// 起こさないため）。
func requireEnabled(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !snapshotConfig().Enabled {
			http.Error(w, "ai usage is disabled", http.StatusConflict)
			return
		}
		h(w, r)
	}
}

// withView は ?provider=claude|codex から対象の補助 WebView を引く。不明な値は 400。
func withView(h func(http.ResponseWriter, *authView)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("provider") {
		case "claude":
			h(w, claudeView)
		case "codex":
			h(w, codexView)
		default:
			http.Error(w, "invalid provider", http.StatusBadRequest)
		}
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

// settingsPayload は /api/ai/setoption の入力。ポインタのフィールドは「未指定なら変更しない」。
type settingsPayload struct {
	Enabled           *bool   `json:"enabled"`
	ClaudeEnabled     *bool   `json:"claudeEnabled"`
	CodexEnabled      *bool   `json:"codexEnabled"`
	TrayProvider      *string `json:"trayProvider"`
	NotifyUsage       *bool   `json:"notifyUsage"`
	NotifyOverage     *bool   `json:"notifyOverage"`
	NotifyStatus      *bool   `json:"notifyStatus"`
	OverageTipFormat  *string `json:"overageTipFormat"`
	ResetTimeFormat   *string `json:"resetTimeFormat"`
	TraySplitDays     *int    `json:"traySplitDays"`
	UsagePollSeconds  *int    `json:"usagePollSeconds"`
	StatusPollSeconds *int    `json:"statusPollSeconds"`
}

// settingsView は /api/ai/settings の出力。通知済みフラグなどの内部状態は含めない。
type settingsView struct {
	Enabled           bool   `json:"enabled"`
	ClaudeEnabled     bool   `json:"claudeEnabled"`
	CodexEnabled      bool   `json:"codexEnabled"`
	TrayProvider      string `json:"trayProvider"`
	NotifyUsage       bool   `json:"notifyUsage"`
	NotifyOverage     bool   `json:"notifyOverage"`
	NotifyStatus      bool   `json:"notifyStatus"`
	OverageTipFormat  string `json:"overageTipFormat"`
	ResetTimeFormat   string `json:"resetTimeFormat"`
	TraySplitDays     int    `json:"traySplitDays"`
	UsagePollSeconds  int    `json:"usagePollSeconds"`
	StatusPollSeconds int    `json:"statusPollSeconds"`
}

func currentPayload() settingsView {
	c := snapshotConfig()
	return settingsView{
		Enabled: c.Enabled, ClaudeEnabled: c.ClaudeEnabled, CodexEnabled: c.CodexEnabled,
		TrayProvider: c.TrayProvider, NotifyUsage: c.NotifyUsage, NotifyOverage: c.NotifyOverage,
		NotifyStatus: c.NotifyStatus, OverageTipFormat: c.OverageTipFormat,
		ResetTimeFormat: c.ResetTimeFormat, TraySplitDays: c.TraySplitDays,
		UsagePollSeconds: c.UsagePollSeconds, StatusPollSeconds: c.StatusPollSeconds,
	}
}

func handleSetOption(w http.ResponseWriter, r *http.Request) {
	var p settingsPayload
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&p); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	before := snapshotConfig()
	mutateConfig(func(c *Settings) {
		if p.Enabled != nil {
			c.Enabled = *p.Enabled
		}
		if p.ClaudeEnabled != nil {
			c.ClaudeEnabled = *p.ClaudeEnabled
		}
		if p.CodexEnabled != nil {
			c.CodexEnabled = *p.CodexEnabled
		}
		if p.TrayProvider != nil {
			c.TrayProvider = *p.TrayProvider
		}
		if p.NotifyUsage != nil {
			c.NotifyUsage = *p.NotifyUsage
		}
		if p.NotifyOverage != nil {
			c.NotifyOverage = *p.NotifyOverage
		}
		if p.NotifyStatus != nil {
			c.NotifyStatus = *p.NotifyStatus
		}
		if p.OverageTipFormat != nil {
			c.OverageTipFormat = *p.OverageTipFormat
		}
		if p.ResetTimeFormat != nil {
			c.ResetTimeFormat = *p.ResetTimeFormat
		}
		if p.TraySplitDays != nil {
			c.TraySplitDays = *p.TraySplitDays
		}
		if p.UsagePollSeconds != nil {
			c.UsagePollSeconds = *p.UsagePollSeconds
		}
		if p.StatusPollSeconds != nil {
			c.StatusPollSeconds = *p.StatusPollSeconds
		}
		// ポーリング間隔などはサーバ側の矯正を正とする（panic 防止と最終防衛線）。
		*c = NormalizeSettings(*c)
	})
	after := snapshotConfig()
	if after.UsagePollSeconds != before.UsagePollSeconds {
		applyUsagePollInterval(after.UsagePollSeconds)
	}
	// 障害監視間隔は statusCacheTTL() が動的に設定を読むので、Go 側の追加処理は不要。
	Apply()
	notifyChanged()
	writeJSON(w, currentPayload())
}

func handleSetProvider(w http.ResponseWriter, r *http.Request) {
	var p struct {
		Provider string `json:"provider"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&p); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	cfg := snapshotConfig()
	if (p.Provider != "claude" && p.Provider != "codex") || !providerEnabled(cfg, p.Provider) {
		http.Error(w, "invalid provider", http.StatusBadRequest)
		return
	}
	mutateConfig(func(c *Settings) { c.TrayProvider = p.Provider })
	notifyChanged()
	writeJSON(w, map[string]any{"ok": true, "provider": p.Provider})
}

// handleOpen は許可リストの URL だけを既定のブラウザで開く（任意の URL は開かない）。
func handleOpen(urls map[string]string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		url, ok := urls[r.URL.Query().Get("provider")]
		if !ok {
			http.Error(w, "invalid provider", http.StatusBadRequest)
			return
		}
		go openExternalURL(url)
		writeJSON(w, map[string]any{"ok": true})
	}
}

// openExternalURL は既定のブラウザで URL を開く。CREATE_NO_WINDOW でコンソールの瞬間表示を抑止する。
func openExternalURL(url string) {
	cmd := exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	_ = cmd.Start()
}
