package main

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"widget-go/ping"
)

type alertFakePinger struct{}

func (a *alertFakePinger) Ping(_ context.Context, _ string, _ time.Duration) (string, time.Duration, error) {
	return "10.0.0.1", 0, errors.New("timeout")
}

// hostCountPinger はホストごとの送信回数を数える。
type hostCountPinger struct {
	mu    sync.Mutex
	count map[string]int
}

func (h *hostCountPinger) Ping(_ context.Context, host string, _ time.Duration) (string, time.Duration, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.count == nil {
		h.count = make(map[string]int)
	}
	h.count[host]++
	return "10.0.0.1", 15 * time.Millisecond, nil
}

func (h *hostCountPinger) sent(host string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.count[host]
}

// setupPingLoop は設定ファイルと Pinger を差し替え、Monitor を作って返す。
func setupPingLoop(t *testing.T, cfgJSON string, p ping.Pinger) *ping.Monitor {
	t.Helper()
	withConfigFile(t, cfgJSON)
	oldPinger := newPinger
	newPinger = func() ping.Pinger { return p }
	t.Cleanup(func() { newPinger = oldPinger })
	initPingMonitor()
	pingMonMu.RLock()
	defer pingMonMu.RUnlock()
	return pingMonitor
}

// 周期処理（pingStep）が、警告をトレイ通知に流す。ロスはサンプルが 3 件そろい、2 回以上落ちたときに出る（立ち上がりでは出さない）。
func TestPingStep_SendsNotification(t *testing.T) {
	cfgJSON := `{"hidden":{"ping":false},"ping":{"targets":[{"host":"bad-host","lossThresholdPercent":10,"rttThresholdMs":100}],"notifyEnabled":true,"intervalSec":2}}`
	mon := setupPingLoop(t, cfgJSON, &alertFakePinger{})

	var titles, msgs []string
	oldBalloon := pingBalloon
	pingBalloon = func(title, msg string) {
		titles = append(titles, title)
		msgs = append(msgs, msg)
	}
	defer func() { pingBalloon = oldBalloon }()

	cfg := snapshotConfig()
	now := time.Now()
	var st pingSched

	pingStep(mon, cfg, &st, now, false)
	if !st.active {
		t.Fatal("表示中で監視先があるので、動作するはず")
	}
	pingStep(mon, cfg, &st, now.Add(2*time.Second), false)
	if len(titles) != 0 {
		t.Fatalf("立ち上がり（2 回まで）の失敗では通知しないはず: %v", titles)
	}
	pingStep(mon, cfg, &st, now.Add(4*time.Second), false)
	if len(titles) != 1 || titles[0] != "疎通の警告: bad-host" || msgs[0] != "パケットロス 3/3 (閾値超過)" {
		t.Errorf("3 回続けて失敗したら、ホスト名付きで通知するはず: %v %v", titles, msgs)
	}
	pingStep(mon, cfg, &st, now.Add(6*time.Second), false)
	if len(titles) != 1 {
		t.Errorf("クールダウン中は重複して通知しないはず: %v", titles)
	}
}

// 非表示の間は 1 つも Ping を送らない（README の約束）。動作中から止まったときは、履歴を捨てる。
func TestPingStep_HiddenSendsNothingAndResets(t *testing.T) {
	fake := &loopFakePinger{}
	mon := setupPingLoop(t, `{"hidden":{"ping":false},"ping":{"targets":[{"host":"8.8.8.8"}]}}`, fake)
	now := time.Now()
	var st pingSched

	pingStep(mon, snapshotConfig(), &st, now, false)
	if !st.active || fake.pingCount.Load() != 1 {
		t.Fatalf("表示中は 1 回送るはず: count=%d", fake.pingCount.Load())
	}
	if mon.Snapshot().Targets[0].SentCount != 1 {
		t.Fatalf("履歴が 1 件あるはず")
	}

	// 非表示にする（設定の変更で kick される）
	if err := setHidden("ping", "true"); err != nil {
		t.Fatal(err)
	}
	if wait := pingStep(mon, snapshotConfig(), &st, now.Add(time.Second), true); st.active || wait != pingIdleWait {
		t.Errorf("非表示のときは動作しないはず: active=%v wait=%v", st.active, wait)
	}
	pingStep(mon, snapshotConfig(), &st, now.Add(4*time.Second), false)
	if fake.pingCount.Load() != 1 {
		t.Errorf("非表示の間は送らないはず: count=%d", fake.pingCount.Load())
	}
	if mon.Snapshot().Targets[0].SentCount != 0 {
		t.Errorf("止まったときに履歴を捨てるはず")
	}

	// 表示に戻すと、すぐに新しく測り直す
	if err := setHidden("ping", "false"); err != nil {
		t.Fatal(err)
	}
	pingStep(mon, snapshotConfig(), &st, now.Add(5*time.Second), true)
	if !st.active || fake.pingCount.Load() != 2 {
		t.Errorf("表示に戻したら再開するはず: count=%d", fake.pingCount.Load())
	}
	if mon.Snapshot().Targets[0].SentCount != 1 {
		t.Errorf("古いサンプルと混ざらず、1 件から始まるはず")
	}
}

func TestPingStep_NoTargetsSendsNothing(t *testing.T) {
	fake := &loopFakePinger{}
	mon := setupPingLoop(t, `{"hidden":{"ping":false},"ping":{"targets":[]}}`, fake)
	var st pingSched
	pingStep(mon, snapshotConfig(), &st, time.Now(), false)
	if st.active || fake.pingCount.Load() != 0 {
		t.Errorf("監視先がなければ何も送らないはず")
	}
}

// 周期は開始時刻基準。Ping の所要時間で延びず、予定より早く起きたときは送らない。
func TestPingStep_StartTimeBasedSchedule(t *testing.T) {
	fake := &loopFakePinger{}
	mon := setupPingLoop(t, `{"hidden":{"ping":false},"ping":{"targets":[{"host":"8.8.8.8"}],"intervalSec":2}}`, fake)
	t0 := time.Now()
	var st pingSched

	if wait := pingStep(mon, snapshotConfig(), &st, t0, false); wait != 2*time.Second || !st.due.Equal(t0.Add(2*time.Second)) {
		t.Fatalf("最初の予定は開始の 2 秒後: wait=%v due=%v", wait, st.due.Sub(t0))
	}
	// 予定より早く起きた（タイマーの誤差）: 送らず、残りを待つ
	if wait := pingStep(mon, snapshotConfig(), &st, t0.Add(1500*time.Millisecond), false); wait != 500*time.Millisecond || fake.pingCount.Load() != 1 {
		t.Fatalf("予定前は送らないはず: wait=%v count=%d", wait, fake.pingCount.Load())
	}
	// 予定の 300ms 後に起きても、次の予定は 4 秒（起点 + 2 周期）。所要時間ぶん後ろへずれない
	pingStep(mon, snapshotConfig(), &st, t0.Add(2300*time.Millisecond), false)
	if fake.pingCount.Load() != 2 || !st.due.Equal(t0.Add(4*time.Second)) {
		t.Errorf("開始時刻基準のはず: count=%d due=%v", fake.pingCount.Load(), st.due.Sub(t0))
	}
}

func TestPingNextDue(t *testing.T) {
	t0 := time.Unix(1000, 0)
	iv := 2 * time.Second
	cases := []struct {
		now  time.Duration
		want time.Duration
	}{
		{0, 2 * time.Second},                       // 予定どおり
		{1999 * time.Millisecond, 2 * time.Second}, // まだ次の予定の前
		{2 * time.Second, 4 * time.Second},         // ちょうど次の予定: その次へ
		{2500 * time.Millisecond, 4 * time.Second}, // 少し遅れた
		{7 * time.Second, 8 * time.Second},         // 大きく遅れた: 過ぎた周期は飛ばす（続けて送らない）
	}
	for _, c := range cases {
		if got := pingNextDue(t0, iv, t0.Add(c.now)).Sub(t0); got != c.want {
			t.Errorf("now=+%v: next=+%v, want +%v", c.now, got, c.want)
		}
	}
}

// 周期の途中の kick（通知の切り替えなど）では、余分なサンプルを足さない。監視先を追加したときは、新しいホストだけすぐ測る。
func TestPingStep_KickDoesNotAddSamples(t *testing.T) {
	fake := &hostCountPinger{}
	mon := setupPingLoop(t, `{"hidden":{"ping":false},"ping":{"targets":[{"host":"a"}],"intervalSec":2,"notifyEnabled":true}}`, fake)
	t0 := time.Now()
	var st pingSched
	pingStep(mon, snapshotConfig(), &st, t0, false)

	// 通知の切り替え
	if err := setPingOption("pingNotify", "false"); err != nil {
		t.Fatal(err)
	}
	pingStep(mon, snapshotConfig(), &st, t0.Add(500*time.Millisecond), true)
	if fake.sent("a") != 1 || mon.Snapshot().Targets[0].SentCount != 1 {
		t.Fatalf("通知の切り替えでは送らないはず: %d", fake.sent("a"))
	}

	// 監視先 b を追加。b だけすぐ測り、a には足さない。周期の予定も変えない
	mutateConfig(func(c *Config) {
		c.Ping.Targets = append(c.Ping.Targets, ping.TargetConfig{Host: "b", LossThresholdPercent: 10, RTTThresholdMs: 100})
	})
	due := st.due
	pingStep(mon, snapshotConfig(), &st, t0.Add(time.Second), true)
	snap := mon.Snapshot()
	if fake.sent("a") != 1 || fake.sent("b") != 1 || snap.Targets[0].SentCount != 1 || snap.Targets[1].SentCount != 1 {
		t.Errorf("新しい監視先だけ測るはず: a=%d b=%d %+v", fake.sent("a"), fake.sent("b"), snap.Targets)
	}
	if !st.due.Equal(due) {
		t.Errorf("周期の予定は変えないはず")
	}
	// 次の定期の時刻には、両方を測る
	pingStep(mon, snapshotConfig(), &st, t0.Add(2*time.Second), false)
	if fake.sent("a") != 2 || fake.sent("b") != 2 {
		t.Errorf("定期の時刻には全部測るはず: a=%d b=%d", fake.sent("a"), fake.sent("b"))
	}
}

// 測定間隔を変えたら、それまでのサンプルを捨て、すぐに測ってそこを周期の起点にする。
func TestPingStep_IntervalChangeRestarts(t *testing.T) {
	fake := &loopFakePinger{}
	mon := setupPingLoop(t, `{"hidden":{"ping":false},"ping":{"targets":[{"host":"8.8.8.8"}],"intervalSec":2}}`, fake)
	t0 := time.Now()
	var st pingSched
	pingStep(mon, snapshotConfig(), &st, t0, false)
	pingStep(mon, snapshotConfig(), &st, t0.Add(2*time.Second), false)
	if mon.Snapshot().Targets[0].SentCount != 2 {
		t.Fatalf("2 件あるはず")
	}

	if err := setPingOption("pingInterval", "5"); err != nil {
		t.Fatal(err)
	}
	wait := pingStep(mon, snapshotConfig(), &st, t0.Add(3*time.Second), true)
	if got := mon.Snapshot().Targets[0].SentCount; got != 1 || fake.pingCount.Load() != 3 {
		t.Errorf("古いサンプルを捨てて、すぐ 1 件測るはず: sent=%d count=%d", got, fake.pingCount.Load())
	}
	if wait != 5*time.Second || st.interval != 5*time.Second {
		t.Errorf("新しい間隔で、変えた時刻から数えるはず: wait=%v", wait)
	}
}

// 測定間隔は 1〜10 秒。範囲外は API で断り、以前の上限（60 秒）で保存された設定は読み込み時に 10 秒へ寄せる。
func TestPingInterval_Range(t *testing.T) {
	withConfigFile(t, `{"ping":{"intervalSec":60}}`)
	if got := snapshotConfig().Ping.IntervalSec; got != ping.MaxIntervalSec {
		t.Errorf("60 秒は読み込み時に %d 秒へ矯正するはず: %d", ping.MaxIntervalSec, got)
	}
	for _, v := range []string{"0", "11", "60", "abc"} {
		if err := setPingOption("pingInterval", v); err == nil {
			t.Errorf("pingInterval=%s は断るはず", v)
		}
	}
	if err := setPingOption("pingInterval", "10"); err != nil || snapshotConfig().Ping.IntervalSec != 10 {
		t.Errorf("10 は受け付けるはず: %v", err)
	}
	if err := setPingOption("pingInterval", "1"); err != nil || snapshotConfig().Ping.IntervalSec != 1 {
		t.Errorf("1 は受け付けるはず: %v", err)
	}
}
