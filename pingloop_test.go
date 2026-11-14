package main

import (
	"context"
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
