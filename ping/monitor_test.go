package ping

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakePinger struct {
	responses map[string]fakeResp
}

type fakeResp struct {
	ip  string
	rtt time.Duration
	err error
}

func (f *fakePinger) Ping(_ context.Context, host string, _ time.Duration) (string, time.Duration, error) {
	if resp, ok := f.responses[host]; ok {
		return resp.ip, resp.rtt, resp.err
	}
	return "1.2.3.4", 10 * time.Millisecond, nil
}

var (
	respOK   = fakeResp{ip: "10.0.0.2", rtt: 10 * time.Millisecond}
	respLoss = fakeResp{ip: "10.0.0.2", err: errors.New("timeout")}
)

// runCycles は PingOnce を n 回、step ずつ時刻を進めながら実行し、出たアラートを集めて返す。
func runCycles(mon *Monitor, now *time.Time, n int, step time.Duration) []NotifyAlert {
	var all []NotifyAlert
	for i := 0; i < n; i++ {
		*now = now.Add(step)
		_, alerts := mon.PingOnce(context.Background(), *now)
		all = append(all, alerts...)
	}
	return all
}

func oneTarget(host string) Config {
	return Config{
		NotifyEnabled: true,
		IntervalSec:   2,
		Targets:       []TargetConfig{{Host: host, LossThresholdPercent: 10, RTTThresholdMs: 100}},
	}
}

func TestNormalizeConfig(t *testing.T) {
	in := Config{
		IntervalSec:   0,
		NotifyEnabled: true,
		Targets: []TargetConfig{
			{Host: "  google.com  ", LossThresholdPercent: -5, RTTThresholdMs: 0},
			{Host: "", LossThresholdPercent: 10, RTTThresholdMs: 100},
			{Host: "8.8.8.8", LossThresholdPercent: 120, RTTThresholdMs: 999999},
			{Host: "host3", LossThresholdPercent: 20, RTTThresholdMs: 50},
			{Host: "host4", LossThresholdPercent: 30, RTTThresholdMs: 60},
			{Host: "host5", LossThresholdPercent: 40, RTTThresholdMs: 70},
			{Host: "host6", LossThresholdPercent: 50, RTTThresholdMs: 80}, // 超過
		},
	}

	norm := NormalizeConfig(in)
	if norm.IntervalSec != DefaultIntervalSec {
		t.Fatalf("expected interval %d, got %d", DefaultIntervalSec, norm.IntervalSec)
	}
	if len(norm.Targets) != 5 {
		t.Fatalf("expected 5 targets, got %d", len(norm.Targets))
	}
	if norm.Targets[0].Host != "google.com" {
		t.Errorf("expected trimmed host 'google.com', got %q", norm.Targets[0].Host)
	}
	if norm.Targets[0].LossThresholdPercent != DefaultLossThreshold {
		t.Errorf("expected loss threshold %d, got %d", DefaultLossThreshold, norm.Targets[0].LossThresholdPercent)
	}
	if norm.Targets[0].RTTThresholdMs != DefaultRTTThreshold {
		t.Errorf("expected rtt threshold %d, got %d", DefaultRTTThreshold, norm.Targets[0].RTTThresholdMs)
	}
	if norm.Targets[1].Host != "8.8.8.8" {
		t.Errorf("expected second host '8.8.8.8', got %q", norm.Targets[1].Host)
	}
}

// 閾値が未指定（0）のときは既定値（10%）になる。「1 回のロスで警告」にはならない。
func TestNormalizeConfig_MissingThresholdsUseDefaults(t *testing.T) {
	norm := NormalizeConfig(Config{Targets: []TargetConfig{{Host: "8.8.8.8"}}})
	if got := norm.Targets[0]; got.LossThresholdPercent != DefaultLossThreshold || got.RTTThresholdMs != DefaultRTTThreshold {
		t.Errorf("既定値になるはず: %+v", got)
	}
}

// 大文字小文字違いも含めて重複は 1 件にまとめ、書式の正しくないホストは捨てる。
func TestNormalizeConfig_DedupeAndInvalid(t *testing.T) {
	norm := NormalizeConfig(Config{Targets: []TargetConfig{
		{Host: "8.8.8.8", LossThresholdPercent: 5},
		{Host: "Example.com"},
		{Host: "example.COM", LossThresholdPercent: 50},
		{Host: "8.8.8.8", LossThresholdPercent: 99},
		{Host: "2001:db8::1"},
		{Host: "bad host"},
		{Host: "ok-host.local"},
	}})
	if len(norm.Targets) != 3 {
		t.Fatalf("3 件になるはず: %+v", norm.Targets)
	}
	if norm.Targets[0].Host != "8.8.8.8" || norm.Targets[0].LossThresholdPercent != 5 {
		t.Errorf("最初のものを残すはず: %+v", norm.Targets[0])
	}
	if norm.Targets[1].Host != "Example.com" || norm.Targets[2].Host != "ok-host.local" {
		t.Errorf("unexpected: %+v", norm.Targets)
	}
}

func TestValidateHost(t *testing.T) {
	good := []string{"google.com", "8.8.8.8", "192.168.1.1", "gw.local", "my_host-1", "example.com.", "  8.8.8.8  "}
	bad := []string{"", "   ", "2001:db8::1", "::1", "bad host", "a..b", ".example.com", "http://example.com", "host/path", "日本語.jp"}
	for _, h := range good {
		if err := ValidateHost(h); err != nil {
			t.Errorf("ValidateHost(%q) は通るはず: %v", h, err)
		}
	}
	for _, h := range bad {
		if err := ValidateHost(h); err == nil {
			t.Errorf("ValidateHost(%q) は拒否するはず", h)
		}
	}
}

func TestTargetBuffer(t *testing.T) {
	buf := newTargetBuffer(5)
	for i := 1; i <= 7; i++ {
		// 1〜5 は成功、6・7 は失敗
		if i >= 6 {
			buf.record("1.1.1.1", 0, false)
		} else {
			buf.record("1.1.1.1", time.Duration(i*10)*time.Millisecond, true)
		}
	}

	// 5件のバッファなので、10・20ms は押し出され、30ms, 40ms, 50ms, 失敗, 失敗 の5件
	stats := buf.stats("test", 10, 100)
	if stats.SentCount != 5 {
		t.Fatalf("expected sent 5, got %d", stats.SentCount)
	}
	if stats.LossCount != 2 {
		t.Fatalf("expected loss 2, got %d", stats.LossCount)
	}
	if stats.LossPercent != 40.0 {
		t.Fatalf("expected loss 40.0%%, got %.1f%%", stats.LossPercent)
	}
	// 直近の 1 回が失敗しているので、窓の中の古い成功（50ms）は出さない
	if stats.RTTMs != -1 {
		t.Fatalf("直近が失敗なら最新 RTT は -1 のはず: %d", stats.RTTMs)
	}
	if !stats.LossReady {
		t.Fatalf("5 件あればロスを表示できるはず")
	}
	// avg: (30+40+50)/3 = 40ms
	if stats.AvgRTTMs != 40 {
		t.Fatalf("expected avg RTT 40ms, got %d", stats.AvgRTTMs)
	}
	if stats.MinRTTMs != 30 || stats.MaxRTTMs != 50 {
		t.Fatalf("expected min 30 max 50, got min %d max %d", stats.MinRTTMs, stats.MaxRTTMs)
	}
	if !stats.IsWarnLoss {
		t.Errorf("expected IsWarnLoss true (2 回のロス、40%% >= 10%%)")
	}
}

// 立ち上がり（サンプルが MinLossSamples 未満）では、率が閾値を超えていても警告せず、ロスも表示しない（1/1 = 100% など）。
func TestStats_SingleLossDoesNotWarn(t *testing.T) {
	buf := newTargetBuffer(20)
	buf.record("1.1.1.1", 0, false)
	st := buf.stats("h", 10, 100)
	if st.IsWarnLoss || st.Status != StatusIdle || st.LossReady {
		t.Errorf("1 回のロスだけでは警告しないはず: %+v", st)
	}
	buf.record("1.1.1.1", 0, false)
	st = buf.stats("h", 10, 100)
	if st.IsWarnLoss || st.Status != StatusIdle || st.LossReady {
		t.Errorf("サンプルが %d 件未満の間は警告しないはず: %+v", MinLossSamples, st)
	}
	buf.record("1.1.1.1", 0, false)
	st = buf.stats("h", 10, 100)
	if !st.IsWarnLoss || st.Status != StatusError || !st.LossReady || st.LossCount != 3 || st.SentCount != 3 {
		t.Errorf("%d 回続けて落ちたら警告するはず: %+v", MinLossSamples, st)
	}

	// 成功が混じる 1/20 = 5% は、閾値未満で警告しない
	buf = newTargetBuffer(20)
	buf.record("1.1.1.1", 0, false)
	for i := 0; i < 19; i++ {
		buf.record("1.1.1.1", 10*time.Millisecond, true)
	}
	if st := buf.stats("h", 10, 100); st.IsWarnLoss || st.Status != "ok" {
		t.Errorf("1/20 は ok のはず: %+v", st)
	}
}

// RTT は、閾値以上の応答が連続するまで警告しない。一瞬のスパイクは無視する。
func TestStats_RTTNeedsConsecutiveSamples(t *testing.T) {
	buf := newTargetBuffer(20)
	for i := 0; i < 5; i++ {
		buf.record("1.1.1.1", 20*time.Millisecond, true)
	}
	buf.record("1.1.1.1", 300*time.Millisecond, true)
	if st := buf.stats("h", 10, 100); st.IsWarnRTT {
		t.Errorf("1 回のスパイクでは警告しないはず: %+v", st)
	}
	buf.record("1.1.1.1", 300*time.Millisecond, true)
	buf.record("1.1.1.1", 300*time.Millisecond, true)
	if st := buf.stats("h", 10, 100); !st.IsWarnRTT {
		t.Errorf("%d 回連続したら警告するはず: %+v", RTTConsecutive, st)
	}
	// 間に失敗が入ると連続ではない
	buf.record("1.1.1.1", 0, false)
	buf.record("1.1.1.1", 300*time.Millisecond, true)
	if st := buf.stats("h", 10, 100); st.IsWarnRTT {
		t.Errorf("連続が途切れたら警告しないはず: %+v", st)
	}
}

func TestMonitor_PingOnce_AlertsAndCooldown(t *testing.T) {
	pinger := &fakePinger{
		responses: map[string]fakeResp{
			"ok-host":   {ip: "10.0.0.1", rtt: 15 * time.Millisecond},
			"loss-host": respLoss,
			"slow-host": {ip: "10.0.0.3", rtt: 250 * time.Millisecond},
		},
	}
	cfg := Config{
		NotifyEnabled: true,
		IntervalSec:   2,
		Targets: []TargetConfig{
			{Host: "ok-host", LossThresholdPercent: 10, RTTThresholdMs: 100},
			{Host: "loss-host", LossThresholdPercent: 10, RTTThresholdMs: 100},
			{Host: "slow-host", LossThresholdPercent: 10, RTTThresholdMs: 100},
		},
	}

	now := time.Now()
	mon := NewMonitorWithCooldown(pinger, cfg, 5*time.Minute)

	// 1・2 回目: まだサンプルが少ない。警告しない（起動直後の誤報を避ける）
	if alerts := runCycles(mon, &now, 2, 2*time.Second); len(alerts) != 0 {
		t.Fatalf("2 回目までは警告しないはず: %+v", alerts)
	}
	// 3 回目: loss-host が 3 回続けて落ち、slow-host が 3 回連続で遅い
	alerts := runCycles(mon, &now, 1, 2*time.Second)
	if len(alerts) != 2 || alerts[0].Host != "loss-host" || alerts[1].Host != "slow-host" {
		t.Fatalf("3 回目は loss-host と slow-host を警告するはず: %+v", alerts)
	}
	if alerts[0].Message != "パケットロス 3/3 (閾値超過)" || alerts[1].Message != "RTT 250ms (閾値超過)" {
		t.Errorf("通知の本文: %q / %q", alerts[0].Message, alerts[1].Message)
	}
	snap := mon.Snapshot()
	if snap.Targets[0].Status != StatusOK || snap.Targets[1].Status != StatusError || snap.Targets[2].Status != StatusWarn {
		t.Errorf("status: %+v", snap.Targets)
	}

	// クールダウン中（1 分後）はアラートなし
	if alerts := runCycles(mon, &now, 1, time.Minute); len(alerts) != 0 {
		t.Fatalf("クールダウン中は 0 件のはず: %+v", alerts)
	}
	// クールダウン経過後（5 分後）は再度アラート
	if alerts := runCycles(mon, &now, 1, 5*time.Minute); len(alerts) != 2 {
		t.Fatalf("クールダウン後は 2 件のはず: %+v", alerts)
	}
}

// 閾値の前後を行き来するホストでは、短い「ok」でクールダウンを解かない。通知が連発しないように。
func TestMonitor_FlappingDoesNotResetCooldown(t *testing.T) {
	pinger := &fakePinger{responses: map[string]fakeResp{"h": respLoss}}
	now := time.Now()
	mon := NewMonitorWithCooldown(pinger, oneTarget("h"), DefaultCooldown)

	// 3 回落ちて警告 → 通知 1 件
	if alerts := runCycles(mon, &now, 3, 2*time.Second); len(alerts) != 1 {
		t.Fatalf("最初の警告で 1 件のはず: %+v", alerts)
	}
	// 窓が一巡するまで成功（約 40 秒）。窓が入れ替わって ok に戻る（RecoverHold 未満）
	pinger.responses["h"] = respOK
	runCycles(mon, &now, 20, 2*time.Second)
	if st := mon.Snapshot().Targets[0].Status; st != "ok" {
		t.Fatalf("ok に戻るはず: %s", st)
	}
	// また 2 回落ちる。クールダウン内なので通知しない
	pinger.responses["h"] = respLoss
	if alerts := runCycles(mon, &now, 2, 2*time.Second); len(alerts) != 0 {
		t.Fatalf("行き来しても、クールダウン内は通知しないはず: %+v", alerts)
	}
}

// 警告なしが RecoverHold 続いたら復帰とみなし、次の悪化は（クールダウン内でも）すぐ通知する。
func TestMonitor_RecoveredHostNotifiesImmediately(t *testing.T) {
	pinger := &fakePinger{responses: map[string]fakeResp{"h": respLoss}}
	now := time.Now()
	mon := NewMonitorWithCooldown(pinger, oneTarget("h"), DefaultCooldown)

	if alerts := runCycles(mon, &now, 3, 2*time.Second); len(alerts) != 1 {
		t.Fatalf("最初の警告で 1 件のはず: %+v", alerts)
	}
	pinger.responses["h"] = respOK
	// 窓が入れ替わって ok になり、さらに RecoverHold 以上 ok が続く
	runCycles(mon, &now, 20, 2*time.Second)
	runCycles(mon, &now, 14, 5*time.Second)
	pinger.responses["h"] = respLoss
	if alerts := runCycles(mon, &now, 2, 2*time.Second); len(alerts) != 1 {
		t.Fatalf("復帰後の悪化はすぐ通知するはず: %+v", alerts)
	}
}

func TestMonitor_Reset(t *testing.T) {
	pinger := &fakePinger{responses: map[string]fakeResp{"h": respLoss}}
	now := time.Now()
	mon := NewMonitorWithCooldown(pinger, oneTarget("h"), DefaultCooldown)
	runCycles(mon, &now, 3, 2*time.Second)
	if mon.Snapshot().Targets[0].SentCount != 3 {
		t.Fatalf("3 回分の履歴があるはず: %+v", mon.Snapshot().Targets[0])
	}

	mon.Reset()
	st := mon.Snapshot().Targets[0]
	if st.SentCount != 0 || st.Status != "idle" || st.LossPercent != 0 {
		t.Errorf("履歴は空になるはず: %+v", st)
	}
	// 通知の状態も捨てるので、再開後の最初の警告は通知される（クールダウンに阻まれない）
	if alerts := runCycles(mon, &now, 3, 2*time.Second); len(alerts) != 1 {
		t.Errorf("Reset 後の警告は通知されるはず: %+v", alerts)
	}
}

func TestMonitor_UpdateConfig(t *testing.T) {
	pinger := &fakePinger{responses: map[string]fakeResp{}}
	cfg := Config{
		Targets: []TargetConfig{
			{Host: "host1", LossThresholdPercent: 10, RTTThresholdMs: 100},
			{Host: "host2", LossThresholdPercent: 10, RTTThresholdMs: 100},
		},
	}
	mon := NewMonitor(pinger, cfg)
	mon.PingOnce(context.Background(), time.Now())

	if len(mon.Snapshot().Targets) != 2 {
		t.Fatalf("expected 2 targets, got %d", len(mon.Snapshot().Targets))
	}

	// host2 を削除し host3 を追加
	cfg2 := Config{
		Targets: []TargetConfig{
			{Host: "host1", LossThresholdPercent: 10, RTTThresholdMs: 100},
			{Host: "host3", LossThresholdPercent: 10, RTTThresholdMs: 100},
		},
	}
	mon.UpdateConfig(cfg2)

	snap := mon.Snapshot()
	if len(snap.Targets) != 2 {
		t.Fatalf("expected 2 targets, got %d", len(snap.Targets))
	}
	if snap.Targets[0].Host != "host1" || snap.Targets[1].Host != "host3" {
		t.Errorf("unexpected targets: %+v", snap.Targets)
	}
}
