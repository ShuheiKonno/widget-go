package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"widget-go/ping"
)

type loopFakePinger struct {
	pingCount atomic.Int32
}

func (l *loopFakePinger) Ping(_ context.Context, _ string, _ time.Duration) (string, time.Duration, error) {
	l.pingCount.Add(1)
	return "10.0.0.1", 15 * time.Millisecond, nil
}

func TestCurrentPing_NilWhenHidden(t *testing.T) {
	withConfigFile(t, `{"hidden":{"ping":true},"ping":{"targets":[{"host":"8.8.8.8"}]}}`)
	initPingMonitor()
	if got := currentPing(); got != nil {
		t.Errorf("expected nil when hidden, got %+v", got)
	}
}

func TestCurrentPing_NilWhenNoTargets(t *testing.T) {
	withConfigFile(t, `{"hidden":{"ping":false},"ping":{"targets":[]}}`)
	initPingMonitor()
	if got := currentPing(); got != nil {
		t.Errorf("expected nil when no targets, got %+v", got)
	}
}

func TestCurrentPing_ReturnsSnapshotWhenVisible(t *testing.T) {
	withConfigFile(t, `{"hidden":{"ping":false},"ping":{"targets":[{"host":"8.8.8.8"}]}}`)
	fake := &loopFakePinger{}
	oldPinger := newPinger
	newPinger = func() ping.Pinger { return fake }
	defer func() { newPinger = oldPinger }()

	initPingMonitor()
	got := currentPing()
	if got == nil {
		t.Fatalf("expected non-nil snapshot, got nil")
	}
	if len(got.Targets) != 1 || got.Targets[0].Host != "8.8.8.8" {
		t.Errorf("unexpected snapshot: %+v", got)
	}
}

type alertFakePinger struct{}

func (a *alertFakePinger) Ping(_ context.Context, _ string, _ time.Duration) (string, time.Duration, error) {
	return "10.0.0.1", 0, errors.New("timeout")
}

// 周期処理（pingStep）が、警告をトレイ通知に流す。ロスは 2 回続いたときに出る（1 回では出さない）。
func TestPingStep_SendsNotification(t *testing.T) {
	cfgJSON := `{"hidden":{"ping":false},"ping":{"targets":[{"host":"bad-host","lossThresholdPercent":10,"rttThresholdMs":100}],"notifyEnabled":true}}`
	withConfigFile(t, cfgJSON)
	oldPinger := newPinger
	newPinger = func() ping.Pinger { return &alertFakePinger{} }
	defer func() { newPinger = oldPinger }()

	var titles, msgs []string
	oldBalloon := pingBalloon
	pingBalloon = func(title, msg string) {
		titles = append(titles, title)
		msgs = append(msgs, msg)
	}
	defer func() { pingBalloon = oldBalloon }()

	initPingMonitor()
	pingMonMu.RLock()
	mon := pingMonitor
	pingMonMu.RUnlock()
	cfg := snapshotConfig()
	now := time.Now()

	if !pingStep(mon, cfg, false, now) {
		t.Fatal("表示中で監視先があるので、動作するはず")
	}
	if len(titles) != 0 {
		t.Fatalf("1 回の失敗では通知しないはず: %v", titles)
	}
	pingStep(mon, cfg, true, now.Add(2*time.Second))
	if len(titles) != 1 || titles[0] != "Ping 警告: bad-host" || msgs[0] == "" {
		t.Errorf("2 回続けて失敗したら、ホスト名付きで通知するはず: %v %v", titles, msgs)
	}
	pingStep(mon, cfg, true, now.Add(4*time.Second))
	if len(titles) != 1 {
		t.Errorf("クールダウン中は重複して通知しないはず: %v", titles)
	}
}

// 非表示の間は 1 つも Ping を送らない（README の約束）。動作中から止まったときは、履歴を捨てる。
func TestPingStep_HiddenSendsNothingAndResets(t *testing.T) {
	withConfigFile(t, `{"hidden":{"ping":false},"ping":{"targets":[{"host":"8.8.8.8"}]}}`)
	fake := &loopFakePinger{}
	oldPinger := newPinger
	newPinger = func() ping.Pinger { return fake }
	defer func() { newPinger = oldPinger }()

	initPingMonitor()
	pingMonMu.RLock()
	mon := pingMonitor
	pingMonMu.RUnlock()
	now := time.Now()

	if !pingStep(mon, snapshotConfig(), false, now) || fake.pingCount.Load() != 1 {
		t.Fatalf("表示中は 1 回送るはず: count=%d", fake.pingCount.Load())
	}
	if mon.Snapshot().Targets[0].SentCount != 1 {
		t.Fatalf("履歴が 1 件あるはず")
	}

	// 非表示にする
	if err := setHidden("ping", "true"); err != nil {
		t.Fatal(err)
	}
	if pingStep(mon, snapshotConfig(), true, now.Add(2*time.Second)) {
		t.Error("非表示のときは動作しない（false）はず")
	}
	pingStep(mon, snapshotConfig(), false, now.Add(4*time.Second))
	if fake.pingCount.Load() != 1 {
		t.Errorf("非表示の間は送らないはず: count=%d", fake.pingCount.Load())
	}
	if mon.Snapshot().Targets[0].SentCount != 0 {
		t.Errorf("止まったときに履歴を捨てるはず")
	}

	// 表示に戻すと、新しく測り直す
	if err := setHidden("ping", "false"); err != nil {
		t.Fatal(err)
	}
	if !pingStep(mon, snapshotConfig(), false, now.Add(6*time.Second)) || fake.pingCount.Load() != 2 {
		t.Errorf("表示に戻したら再開するはず: count=%d", fake.pingCount.Load())
	}
	if mon.Snapshot().Targets[0].SentCount != 1 {
		t.Errorf("古いサンプルと混ざらず、1 件から始まるはず")
	}
}

func TestPingStep_NoTargetsSendsNothing(t *testing.T) {
	withConfigFile(t, `{"hidden":{"ping":false},"ping":{"targets":[]}}`)
	fake := &loopFakePinger{}
	oldPinger := newPinger
	newPinger = func() ping.Pinger { return fake }
	defer func() { newPinger = oldPinger }()

	initPingMonitor()
	pingMonMu.RLock()
	mon := pingMonitor
	pingMonMu.RUnlock()
	if pingStep(mon, snapshotConfig(), false, time.Now()) || fake.pingCount.Load() != 0 {
		t.Errorf("監視先がなければ何も送らないはず")
	}
}

// /api/ping/targets: 書式が正しくないホスト・6 件以上は 400 で断り、設定を変えない。重複は 1 件にまとめて保存する。
func TestPingTargetsEndpoint(t *testing.T) {
	withConfigFile(t, "")
	h := securityMiddleware(newServerMux("ping-test-token"), "ping-test-token", 4321)
	post := func(body string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(http.MethodPost, "/api/ping/targets", strings.NewReader(body))
		r.Host = "127.0.0.1:4321"
		r.Header.Set("X-Widget-Token", "ping-test-token")
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}

	for name, body := range map[string]string{
		"IPv6":     `[{"host":"2001:db8::1"}]`,
		"空のホスト":    `[{"host":"  "}]`,
		"不正な文字":    `[{"host":"bad host"}]`,
		"6 件":      `[{"host":"a"},{"host":"b"},{"host":"c"},{"host":"d"},{"host":"e"},{"host":"f"}]`,
		"JSON が不正": `{`,
		"整数でない閾値":  `[{"host":"a","lossThresholdPercent":1.5}]`,
	} {
		if w := post(body); w.Code != http.StatusBadRequest {
			t.Errorf("%s: %d, want 400", name, w.Code)
		}
	}
	if n := len(snapshotConfig().Ping.Targets); n != 0 {
		t.Fatalf("不正な要求で設定が変わった: %d 件", n)
	}

	w := post(`[{"host":"8.8.8.8"},{"host":"8.8.8.8","lossThresholdPercent":50},{"host":"Example.com","lossThresholdPercent":20,"rttThresholdMs":30}]`)
	if w.Code != http.StatusOK {
		t.Fatalf("%d, want 200: %s", w.Code, w.Body.String())
	}
	got := snapshotConfig().Ping.Targets
	if len(got) != 2 || got[0].Host != "8.8.8.8" || got[0].LossThresholdPercent != 10 || got[1].Host != "Example.com" || got[1].RTTThresholdMs != 30 {
		t.Errorf("重複は 1 件にまとめ、未指定の閾値は既定値になるはず: %+v", got)
	}
}
