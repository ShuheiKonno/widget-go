package aiusage

import (
	"fmt"
	"os"
	"sync"
	"time"
)

// UsageWindow は 1 つの使用率ウィンドウ（%）と次回リセット時刻。
type UsageWindow struct {
	Utilization float64    `json:"utilization"`
	ResetsAt    *time.Time `json:"resetsAt"`
	Label       string     `json:"label,omitempty"`
}

// OverageInfo は追加使用量（従量課金）の情報を保持する。
type OverageInfo struct {
	AmountUsed    float64    `json:"amountUsed"`
	SpendingLimit *float64   `json:"spendingLimit"` // nil = 無制限
	ResetsAt      *time.Time `json:"resetsAt"`
}

// UsageSnapshot はウィジェット/トレイが参照する統合ビュー。
// AuthState により UI がエラーバナーを出すか判断する。
type UsageSnapshot struct {
	FiveHour UsageWindow  `json:"fiveHour"`
	SevenDay UsageWindow  `json:"sevenDay"`
	Overage  *OverageInfo `json:"overage,omitempty"`

	Email            string   `json:"email"`
	DisplayName      string   `json:"displayName"`
	SubscriptionType string   `json:"subscriptionType"`
	Provider         string   `json:"provider,omitempty"`
	CreditBalance    *float64 `json:"creditBalance,omitempty"`

	// AuthState: "ok" | "needs_login" | "network_error" | "init"
	// "needs_login" は Cookie が無い／失効で、補助 WebView を可視化して利用者に
	// ログインさせる必要がある状態。
	AuthState string    `json:"authState"`
	LastError string    `json:"lastError,omitempty"`
	UpdatedAt time.Time `json:"updatedAt"`
}

var (
	usageMu     sync.RWMutex
	cachedUsage UsageSnapshot

	// usageIntervalReset は設定変更時に収集ループの ticker を張り直す合図。
	// バッファ 1 のためノンブロッキング送信でよい（連続変更は最新値で 1 回 Reset されれば十分）。
	usageIntervalReset = make(chan struct{}, 1)

	startOnce sync.Once
)

// usagePollInterval は設定された使用量取得間隔を返す。
// clampPollSeconds により 0/範囲外でも [60,3600] 秒に収まるため、
// time.NewTicker / Ticker.Reset の「間隔 <= 0 で panic」を確実に回避する。
func usagePollInterval() time.Duration {
	return time.Duration(clampPollSeconds(snapshotConfig().UsagePollSeconds)) * time.Second
}

// applyUsagePollInterval は使用量取得間隔の変更を即時反映する。
// (1) 収集ループの ticker にリセットを合図し、(2) 補助 WebView のバックアップタイマーを
// Eval で張り直す（使用量間隔に追従させ、二重保険を維持する）。
func applyUsagePollInterval(sec int) {
	select {
	case usageIntervalReset <- struct{}{}:
	default:
	}
	for _, v := range allViews {
		v.setInterval(sec * 1000)
	}
}

// refreshUsage は Claude 用の補助 WebView に取得を依頼し、完了まで待つ。
// UI スレッドから同期で呼ばないこと（authView.refresh を参照）。
func refreshUsage() { claudeView.refresh() }

// refreshCodexUsage は Codex 用。
func refreshCodexUsage() { codexView.refresh() }

// updateUsageError は AuthState とエラー文字列を更新する。
// UpdatedAt は触らない（最終成功フェッチ時刻を保持）。
func updateUsageError(state, msg string) {
	usageMu.Lock()
	cachedUsage.AuthState = state
	cachedUsage.LastError = msg
	usageMu.Unlock()
	fmt.Fprintln(os.Stderr, "[usage]", state, msg)
	notifyChanged()
}

func resetClaudeSnapshot() {
	usageMu.Lock()
	cachedUsage = UsageSnapshot{Provider: "claude", AuthState: "init"}
	usageMu.Unlock()
}

// startCollector はバックグラウンドの定期取得ループを開始する。
// 補助 WebView のページ内にも 5 分の setInterval があるが、Go 側からも明示的に
// トリガーすることでスロットル時の保険とする。
// プロセス寿命 = アプリ寿命なので明示的な停止は行わない。無効のときは何も取得しない。
func startCollector() {
	resetClaudeSnapshot()
	resetCodexSnapshot()
	go func() {
		ticker := time.NewTicker(usagePollInterval())
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				refreshAllUsage()
			case <-usageIntervalReset:
				// 設定保存で間隔が変わったら新しい値で張り直す。
				ticker.Reset(usagePollInterval())
			}
		}
	}()
}

// refreshAllUsage は有効なプロバイダーの取得を並行実行する。
func refreshAllUsage() {
	var wg sync.WaitGroup
	cfg := snapshotConfig()
	if !cfg.Enabled {
		return
	}
	if cfg.ClaudeEnabled {
		wg.Add(1)
		go func() { defer wg.Done(); refreshUsage() }()
	}
	if cfg.CodexEnabled {
		wg.Add(1)
		go func() { defer wg.Done(); refreshCodexUsage() }()
	}
	wg.Wait()
}

type AllUsageSnapshots struct {
	Claude UsageSnapshot `json:"claude"`
	Codex  UsageSnapshot `json:"codex"`
}

func getAllUsageSnapshots() AllUsageSnapshots {
	return AllUsageSnapshots{Claude: getUsageSnapshot(), Codex: getCodexUsageSnapshot()}
}

func getUsageSnapshot() UsageSnapshot {
	usageMu.RLock()
	defer usageMu.RUnlock()
	return cachedUsage
}

// Start は AI 使用量の機能を始める。メインの goroutine（runtime.LockOSThread 済み）から、
// メイン WebView の生成後・w.Run() の前に呼ぶ。設定が OFF（既定）のうちは、認証 WebView2 を
// 作らず、取得もしない。
func Start(h Host) {
	host = h
	startOnce.Do(func() {
		initNotifyLog()
		loadNotifyState()
		startCollector()
	})
	Apply()
}

// Apply は設定の変更を反映する: ON になったプロバイダーの補助 WebView を生成し、
// OFF になったものを破棄する。どの goroutine からでも呼べる。
func Apply() {
	hostDispatch(func() {
		cfg := snapshotConfig()
		for _, v := range allViews {
			v.sync(cfg)
		}
	})
}
