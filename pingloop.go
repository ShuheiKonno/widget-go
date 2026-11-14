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

// pingSched は Ping 監視の周期の状態。
type pingSched struct {
	active   bool          // 動作中か（前の周期で測ったか）
	interval time.Duration // 動作中の測定間隔
	due      time.Time     // 次に定期の測定をする時刻（開始時刻基準）
}

// pingIdleWait は、動作していないときに設定を確かめ直す間隔（設定の変更は kick ですぐに届く）。
const pingIdleWait = 5 * time.Second

// pingInterval は設定の測定間隔を、範囲内に収めて返す。
func pingInterval(cfg ping.Config) time.Duration {
	return time.Duration(ping.NormalizeConfig(cfg).IntervalSec) * time.Second
}

// pingNextDue は、開始時刻基準で次の測定の時刻を返す。Ping が長引いて予定を過ぎていたら、
// 過ぎた周期は飛ばす（遅れを取り戻すために続けて送らない）。
func pingNextDue(prev time.Time, interval time.Duration, now time.Time) time.Time {
	next := prev.Add(interval)
	if next.After(now) {
		return next
	}
	skipped := now.Sub(prev) / interval
	return prev.Add((skipped + 1) * interval)
}

// pingMeasure は Ping を送り、警告を通知する。missingOnly のときは、まだ測っていない監視先だけに送る。
func pingMeasure(mon *ping.Monitor, now time.Time, missingOnly bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	var alerts []ping.NotifyAlert
	if missingOnly {
		_, alerts = mon.PingMissing(ctx, now)
	} else {
		_, alerts = mon.PingOnce(ctx, now)
	}
	cancel()
	for _, a := range alerts {
		pingBalloon("疎通の警告: "+a.Host, a.Message)
	}
}

// pingStep は Ping 監視の周期処理の 1 回分で、次に起きるまでの待ち時間を返す。
// now は呼ばれた時刻、kicked は設定の変更（pingKick）で起こされたか。
//
//   - 非表示・監視先なし: 何も送らない。動作中から止まった最初の回で、履歴と通知の状態を捨てる
//     （再開したときに、古いサンプルが新しい測定と混ざらないように）。
//   - 開始・再開・測定間隔の変更: すぐに測り、そこを周期の起点にする（間隔の変更でバッファは Monitor が捨てる）。
//   - 定期の時刻: 全監視先を測る。次の時刻は開始時刻基準（Ping の所要時間で周期が延びない）。
//   - 周期の途中の kick（通知の切り替え・監視先の保存など）: 設定をスナップショットへ反映し、
//     まだ測っていない監視先（追加・変更したもの）だけを測る。測り済みのホストにサンプルを足さない。
func pingStep(mon *ping.Monitor, cfg Config, st *pingSched, now time.Time, kicked bool) time.Duration {
	if !pingShouldRun(cfg) {
		if st.active {
			mon.Reset()
		}
		*st = pingSched{}
		return pingIdleWait
	}
	mon.UpdateConfig(cfg.Ping)
	interval := pingInterval(cfg.Ping)
	switch {
	case !st.active || interval != st.interval:
		st.active = true
		st.interval = interval
		st.due = now.Add(interval)
		pingMeasure(mon, now, false)
	case !now.Before(st.due):
		pingMeasure(mon, now, false)
		st.due = pingNextDue(st.due, interval, now)
	case kicked:
		pingMeasure(mon, now, true)
	}
	return st.due.Sub(now)
}

// pingLoop は Ping 監視のバックグラウンド goroutine。
// 表示中（!Hidden.Ping）かつ監視先が 1 件以上ある場合のみ定期実行し、非表示時は完全に停止する。
func pingLoop() {
	initPingMonitor()

	var st pingSched
	kicked := false
	for {
		cfg := snapshotConfig()

		pingMonMu.RLock()
		mon := pingMonitor
		pingMonMu.RUnlock()

		wait := pingIdleWait
		if mon != nil {
			wait = pingStep(mon, cfg, &st, time.Now(), kicked)
			if st.active {
				// 測っている間に時間が経っているので、予定の時刻から測り直す
				wait = time.Until(st.due)
			}
		}
		if wait < 0 {
			wait = 0
		}
		timer := time.NewTimer(wait)
		select {
		case <-timer.C:
			kicked = false
		case <-pingKickCh:
			timer.Stop()
			kicked = true
		}
	}
}
