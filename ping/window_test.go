package ping

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// countingPinger はホストごとの送信回数を数える。応答は fakePinger と同じく responses で決める。
type countingPinger struct {
	mu        sync.Mutex
	responses map[string]fakeResp
	count     map[string]int
}

func newCountingPinger(responses map[string]fakeResp) *countingPinger {
	return &countingPinger{responses: responses, count: make(map[string]int)}
}

func (c *countingPinger) Ping(_ context.Context, host string, _ time.Duration) (string, time.Duration, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.count[host]++
	if resp, ok := c.responses[host]; ok {
		return resp.ip, resp.rtt, resp.err
	}
	return "1.2.3.4", 10 * time.Millisecond, nil
}

func (c *countingPinger) sent(host string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.count[host]
}

var respResolve = fakeResp{err: &ResolveError{Host: "nx.example", Err: errors.New("no such host")}}

func targets(hosts ...string) Config {
	cfg := Config{NotifyEnabled: true, IntervalSec: 2}
	for _, h := range hosts {
		cfg.Targets = append(cfg.Targets, TargetConfig{Host: h, LossThresholdPercent: 10, RTTThresholdMs: 100})
	}
	return cfg
}

// 測定間隔の範囲は 1〜10 秒。10 秒を超える値（以前の上限 60 秒で保存されたものなど）は 10 秒に寄せる。
func TestNormalizeConfig_IntervalRange(t *testing.T) {
	for in, want := range map[int]int{-1: DefaultIntervalSec, 0: DefaultIntervalSec, 1: 1, 2: 2, 10: 10, 11: 10, 30: 10, 60: 10} {
		if got := NormalizeConfig(Config{IntervalSec: in}).IntervalSec; got != want {
			t.Errorf("IntervalSec %d → %d, want %d", in, got, want)
		}
	}
}

// ロスは「窓内の失敗回数/サンプル数」で出す。窓は直近 DefaultWindowSize 回で、古いものは押し出される。
func TestStats_LossCountWindow(t *testing.T) {
	buf := newTargetBuffer(DefaultWindowSize)
	for i := 0; i < 30; i++ {
		// 最初の 10 回はすべてロス。窓（20 回）から押し出される
		buf.record("1.1.1.1", 10*time.Millisecond, i >= 10)
	}
	st := buf.stats("h", 10, 100)
	if st.SentCount != DefaultWindowSize || st.LossCount != 0 || st.Status != StatusOK {
		t.Fatalf("古いロスは窓から外れるはず: %+v", st)
	}
	buf.record("1.1.1.1", 0, false)
	buf.record("1.1.1.1", 0, false)
	st = buf.stats("h", 10, 100)
	if st.SentCount != 20 || st.LossCount != 2 || !st.LossReady || !st.IsWarnLoss || st.Status != StatusWarn {
		t.Errorf("2/20 = 10%% は閾値 10%% で警告するはず: %+v", st)
	}
	if st.RTTMs != -1 {
		t.Errorf("直近が失敗なら最新 RTT は出さない: %d", st.RTTMs)
	}
	// 閾値 15% では 2/20 は警告しない（窓が満杯なら 5% 刻み）
	if st := buf.stats("h", 15, 100); st.IsWarnLoss || st.Status != StatusOK {
		t.Errorf("2/20 は閾値 15%% 未満: %+v", st)
	}
}

// 立ち上がりの途中（1/3 など）は、サンプル数が下限に達していればそのまま n/sent で判定する。
func TestStats_StartupUsesActualSampleCount(t *testing.T) {
	buf := newTargetBuffer(DefaultWindowSize)
	buf.record("1.1.1.1", 0, false)
	buf.record("1.1.1.1", 10*time.Millisecond, true)
	if st := buf.stats("h", 10, 100); st.LossReady || st.Status != StatusOK {
		t.Errorf("2 件ではロスを出さず、警告もしないはず: %+v", st)
	}
	buf.record("1.1.1.1", 10*time.Millisecond, true)
	st := buf.stats("h", 10, 100)
	if !st.LossReady || st.LossCount != 1 || st.SentCount != 3 || st.IsWarnLoss {
		t.Errorf("1/3 を出し、1 回のロスでは警告しないはず: %+v", st)
	}
	if st.RTTMs != 10 {
		t.Errorf("直近が成功なら最新 RTT を出す: %d", st.RTTMs)
	}
}

// 1ms 未満の応答は 0 として扱う（UI は <1 ms と表示する）。0 は「値なし」(-1) と区別する。
func TestStats_SubMillisecondRTT(t *testing.T) {
	buf := newTargetBuffer(DefaultWindowSize)
	buf.record("127.0.0.1", 300*time.Microsecond, true)
	if st := buf.stats("h", 10, 100); st.RTTMs != 0 {
		t.Errorf("1ms 未満は 0 のはず: %d", st.RTTMs)
	}
}

// 代表 RTT は、直近の 1 回が成功したホストの最新 RTT の最大（最悪値）。直近が失敗したホストの古い値は使わない。
func TestMonitor_SummaryRTTIsWorst(t *testing.T) {
	p := newCountingPinger(map[string]fakeResp{
		"fast": {ip: "10.0.0.1", rtt: 5 * time.Millisecond},
		"slow": {ip: "10.0.0.2", rtt: 80 * time.Millisecond},
		"flap": {ip: "10.0.0.3", rtt: 300 * time.Millisecond},
	})
	mon := NewMonitor(p, targets("fast", "slow", "flap"))
	now := time.Now()
	runCycles(mon, &now, 1, 2*time.Second)
	if got := mon.Snapshot().SummaryRTT; got != 300 {
		t.Fatalf("代表 RTT は最悪値 300 のはず: %d", got)
	}
	// flap の直近が落ちた。古い 300ms は使わず、残りの最悪値 80ms
	p.responses["flap"] = respLoss
	runCycles(mon, &now, 1, 2*time.Second)
	snap := mon.Snapshot()
	if snap.SummaryRTT != 80 || snap.Targets[2].RTTMs != -1 {
		t.Errorf("直近が失敗したホストは除くはず: summary=%d flap=%d", snap.SummaryRTT, snap.Targets[2].RTTMs)
	}
	// すべて直近が失敗なら -1
	p.responses["fast"], p.responses["slow"] = respLoss, respLoss
	runCycles(mon, &now, 1, 2*time.Second)
	if got := mon.Snapshot().SummaryRTT; got != -1 {
		t.Errorf("値がなければ -1 のはず: %d", got)
	}
}

// 名前解決の失敗はロスに数えない。続けて ResolveFailConsecutive 回失敗したら StatusUnresolved（警告）にして通知する。
func TestMonitor_ResolveFailureIsNotLoss(t *testing.T) {
	p := newCountingPinger(map[string]fakeResp{"h": respOK})
	mon := NewMonitorWithCooldown(p, targets("h"), DefaultCooldown)
	now := time.Now()
	runCycles(mon, &now, 5, 2*time.Second)

	p.responses["h"] = respResolve
	for i := 1; i < ResolveFailConsecutive; i++ {
		if alerts := runCycles(mon, &now, 1, 2*time.Second); len(alerts) != 0 {
			t.Fatalf("%d 回目の解決失敗では通知しないはず: %+v", i, alerts)
		}
		st := mon.Snapshot().Targets[0]
		if st.SentCount != 5 || st.LossCount != 0 || !st.ResolveFail || st.RTTMs != -1 || st.Status != StatusOK {
			t.Fatalf("解決失敗はサンプルに入れず、RTT は -1 にするはず: %+v", st)
		}
	}
	alerts := runCycles(mon, &now, 1, 2*time.Second)
	if len(alerts) != 1 || alerts[0].Message != "名前解決に失敗しています" {
		t.Fatalf("続けて失敗したら通知するはず: %+v", alerts)
	}
	st := mon.Snapshot().Targets[0]
	if st.Status != StatusUnresolved || !IsAlertStatus(st.Status) || st.LossCount != 0 || st.IsWarnLoss {
		t.Errorf("ロスとは別の状態のはず: %+v", st)
	}

	// 解決できるようになったら、元の窓のまま戻る
	p.responses["h"] = respOK
	runCycles(mon, &now, 1, 2*time.Second)
	st = mon.Snapshot().Targets[0]
	if st.Status != StatusOK || st.ResolveFail || st.SentCount != 6 || st.RTTMs != 10 {
		t.Errorf("解決できたら元に戻るはず: %+v", st)
	}
}

// 遅い応答が続いたあとに名前解決が失敗しても、値の出せない RTT（-1）で警告・通知しない。
func TestMonitor_ResolveFailureClearsRTTWarning(t *testing.T) {
	p := newCountingPinger(map[string]fakeResp{"h": {ip: "10.0.0.1", rtt: 300 * time.Millisecond}})
	// クールダウン 0: 警告の状態なら毎回通知が出る。通知が出ないことで、警告でないことを確かめる
	mon := NewMonitorWithCooldown(p, targets("h"), 0)
	now := time.Now()
	if alerts := runCycles(mon, &now, RTTConsecutive, 2*time.Second); len(alerts) != 1 || !mon.Snapshot().Targets[0].IsWarnRTT {
		t.Fatalf("遅い応答が続いたら RTT 警告のはず: %+v", alerts)
	}
	p.responses["h"] = respResolve
	alerts := runCycles(mon, &now, 1, 2*time.Second)
	st := mon.Snapshot().Targets[0]
	if st.IsWarnRTT || st.RTTMs != -1 || st.Status != StatusOK || len(alerts) != 0 {
		t.Errorf("解決失敗の間は RTT 警告にしないはず: %+v alerts=%+v", st, alerts)
	}
}

// 一度も解決できないホスト（サンプルなし）も、続けて失敗したら StatusUnresolved になる。
func TestMonitor_NeverResolvedHost(t *testing.T) {
	p := newCountingPinger(map[string]fakeResp{"nx": respResolve})
	mon := NewMonitorWithCooldown(p, targets("nx"), DefaultCooldown)
	now := time.Now()
	alerts := runCycles(mon, &now, ResolveFailConsecutive, 2*time.Second)
	st := mon.Snapshot().Targets[0]
	if st.Status != StatusUnresolved || st.SentCount != 0 || len(alerts) != 1 {
		t.Errorf("unresolved で 1 回通知するはず: %+v alerts=%+v", st, alerts)
	}
}

// 実装の Pinger が返す形（*ResolveError を %w で包んだもの）でも、名前解決の失敗として扱う。
func TestIsResolveError(t *testing.T) {
	re := &ResolveError{Host: "x", Err: errors.New("no such host")}
	if !IsResolveError(re) || !IsResolveError(errors.Join(errors.New("ctx"), re)) {
		t.Error("ResolveError を見分けるはず")
	}
	if IsResolveError(errors.New("request timed out")) || IsResolveError(nil) {
		t.Error("ほかのエラーはロス扱いのはず")
	}
}

// 測定間隔を変えたら、バッファを捨てる（違う間隔のサンプルを混ぜない）。通知のクールダウンは残す。
func TestMonitor_IntervalChangeDropsBuffers(t *testing.T) {
	p := newCountingPinger(map[string]fakeResp{"h": respLoss})
	cfg := targets("h")
	mon := NewMonitorWithCooldown(p, cfg, DefaultCooldown)
	now := time.Now()
	if alerts := runCycles(mon, &now, 3, 2*time.Second); len(alerts) != 1 {
		t.Fatalf("警告で 1 件のはず: %+v", alerts)
	}

	// 間隔が同じなら残す
	mon.UpdateConfig(cfg)
	if got := mon.Snapshot().Targets[0].SentCount; got != 3 {
		t.Fatalf("間隔が同じなら残すはず: %d", got)
	}
	cfg.IntervalSec = 5
	mon.UpdateConfig(cfg)
	if st := mon.Snapshot().Targets[0]; st.SentCount != 0 || st.Status != StatusIdle {
		t.Fatalf("間隔を変えたら捨てるはず: %+v", st)
	}
	// クールダウンは残るので、同じ警告を再び通知しない
	if alerts := runCycles(mon, &now, 3, 5*time.Second); len(alerts) != 0 {
		t.Errorf("間隔の変更だけで再通知しないはず: %+v", alerts)
	}
}

// PingMissing は、まだ測っていない監視先だけに送る。測り済みのホストにサンプルを足さない。
func TestMonitor_PingMissingOnlyNewTargets(t *testing.T) {
	p := newCountingPinger(nil)
	mon := NewMonitor(p, targets("a", "b"))
	now := time.Now()
	runCycles(mon, &now, 2, 2*time.Second)

	// 監視先に変更がない（通知の切り替えなど）ときは、何も送らない
	mon.UpdateConfig(targets("a", "b"))
	mon.PingMissing(context.Background(), now)
	if p.sent("a") != 2 || p.sent("b") != 2 {
		t.Fatalf("測り済みのホストには送らないはず: a=%d b=%d", p.sent("a"), p.sent("b"))
	}

	// c を追加すると、c だけを測る
	mon.UpdateConfig(targets("a", "b", "c"))
	if st := mon.Snapshot().Targets[2]; st.Host != "c" || st.Status != StatusIdle {
		t.Fatalf("追加した監視先は、測る前から一覧に出るはず: %+v", st)
	}
	mon.PingMissing(context.Background(), now)
	snap := mon.Snapshot()
	if p.sent("a") != 2 || p.sent("b") != 2 || p.sent("c") != 1 {
		t.Errorf("c だけ送るはず: a=%d b=%d c=%d", p.sent("a"), p.sent("b"), p.sent("c"))
	}
	if snap.Targets[0].SentCount != 2 || snap.Targets[2].SentCount != 1 || snap.Targets[2].RTTMs != 10 {
		t.Errorf("既存はサンプルが増えず、c は 1 件入るはず: %+v", snap.Targets)
	}
}

// IcmpSendEcho に渡す待ち時間は、ctx の期限までの残りで切り詰める。
func TestEchoTimeout(t *testing.T) {
	now := time.Now()
	bg := context.Background()
	if ms, err := echoTimeout(bg, time.Second, now); err != nil || ms != 1000 {
		t.Errorf("期限なし: %d %v", ms, err)
	}
	if ms, err := echoTimeout(bg, 0, now); err != nil || ms != 1000 {
		t.Errorf("timeout 0 は 1 秒: %d %v", ms, err)
	}

	ctx, cancel := context.WithDeadline(bg, now.Add(300*time.Millisecond))
	defer cancel()
	if ms, err := echoTimeout(ctx, time.Second, now); err != nil || ms != 300 {
		t.Errorf("残り 300ms に切り詰めるはず: %d %v", ms, err)
	}
	if ms, err := echoTimeout(ctx, 100*time.Millisecond, now); err != nil || ms != 100 {
		t.Errorf("残りのほうが長ければ timeout のまま: %d %v", ms, err)
	}
	if ms, err := echoTimeout(ctx, time.Second, now.Add(300*time.Millisecond-500*time.Microsecond)); err != nil || ms != 1 {
		t.Errorf("1ms 未満の残りは 1ms（0 にしない）: %d %v", ms, err)
	}
	if _, err := echoTimeout(ctx, time.Second, now.Add(400*time.Millisecond)); !errors.Is(err, ErrNoTimeLeft) {
		t.Errorf("期限を過ぎていたら送らないはず: %v", err)
	}

	cctx, ccancel := context.WithCancel(bg)
	ccancel()
	if _, err := echoTimeout(cctx, time.Second, now); !errors.Is(err, context.Canceled) {
		t.Errorf("取り消されていたら送らないはず: %v", err)
	}
}

func TestIsAlertStatus(t *testing.T) {
	for st, want := range map[string]bool{StatusIdle: false, StatusOK: false, StatusWarn: true, StatusError: true, StatusUnresolved: true} {
		if IsAlertStatus(st) != want {
			t.Errorf("IsAlertStatus(%q) != %v", st, want)
		}
	}
}
