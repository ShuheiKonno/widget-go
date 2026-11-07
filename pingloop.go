package main

import (
	"context"
	"sync"
	"time"

	"widget-go/aiusage"
	"widget-go/ping"
)

var (
	pingBalloon = func(title, msg string) {
		showBalloon(title, msg, aiusage.NIIF_WARNING)
	}

	pingKickCh = make(chan struct{}, 1)

	pingMonMu   sync.RWMutex
	pingMonitor *ping.Monitor

	newPinger = func() ping.Pinger {
		return ping.NewSystemPinger()
	}
)

func pingKick() {
	select {
	case pingKickCh <- struct{}{}:
	default:
	}
}

func initPingMonitor() {
	pingMonMu.Lock()
	defer pingMonMu.Unlock()
	cfg := snapshotConfig().Ping
	pingMonitor = ping.NewMonitor(newPinger(), cfg)
}

// pingShouldRun は Ping を送るべきかを返す。表示中（!Hidden.Ping）で、監視先が 1 件以上あるときだけ true。
func pingShouldRun(cfg Config) bool {
	return !cfg.Hidden.Ping && len(cfg.Ping.Targets) > 0
}

// currentPing は /api/system 用の Ping スナップショットを返す。
// 非表示（Hidden.Ping == true）または未設定の場合は nil。
func currentPing() *ping.Info {
	cfg := snapshotConfig()
	if !pingShouldRun(cfg) {
		return nil
	}
	pingMonMu.RLock()
	mon := pingMonitor
	pingMonMu.RUnlock()
	if mon == nil {
		return nil
	}
	snap := mon.Snapshot()
	return &snap
}

// pingStep は Ping 監視の 1 周期分。動作中なら true を返す。
// 非表示・監視先なしのときは何も送らず、動作中から止まった最初の周期で、履歴と通知の状態を捨てる
// （再開したときに、古いサンプルが新しい測定と混ざらないように）。
func pingStep(mon *ping.Monitor, cfg Config, wasActive bool, now time.Time) bool {
	if !pingShouldRun(cfg) {
		if wasActive {
			mon.Reset()
		}
		return false
	}
	mon.UpdateConfig(cfg.Ping)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	_, alerts := mon.PingOnce(ctx, now)
	cancel()
	for _, a := range alerts {
		pingBalloon("Ping 警告: "+a.Host, a.Message)
	}
	return true
}

// pingLoop は Ping 監視のバックグラウンド goroutine。
// 表示中（!Hidden.Ping）かつ監視先が 1 件以上ある場合のみ定期実行し、非表示時は完全に停止する。
func pingLoop() {
	initPingMonitor()

	active := false
	for {
		cfg := snapshotConfig()

		pingMonMu.RLock()
		mon := pingMonitor
		pingMonMu.RUnlock()

		if mon != nil {
			active = pingStep(mon, cfg, active, time.Now())
		}

		// 動作していないときは、不要なパケットを送らず、設定の変更（kick）か 5 秒ごとの確認を待つ
		wait := 5 * time.Second
		if active {
			intervalSec := cfg.Ping.IntervalSec
			if intervalSec < 1 {
				intervalSec = ping.DefaultIntervalSec
			}
			wait = time.Duration(intervalSec) * time.Second
		}
		select {
		case <-time.After(wait):
		case <-pingKickCh:
		}
	}
}
