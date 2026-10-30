//go:build windows

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
)

func TestZoomStepsAreSortedAndContainDefault(t *testing.T) {
	if !sort.IntsAreSorted(zoomSteps) {
		t.Errorf("zoomSteps が昇順でない: %v", zoomSteps)
	}
	if !isZoomStep(defaultZoomPercent) {
		t.Errorf("既定の %d%% が一覧に無い", defaultZoomPercent)
	}
}

func TestNormalizeZoom(t *testing.T) {
	cases := []struct{ in, want int }{
		{100, 100}, {50, 50}, {200, 200}, {80, 80},
		{0, 100},   // キーが無い旧 config
		{-5, 100},  // 手編集
		{99, 100},  // 一覧に無い値
		{300, 100}, // 範囲外
	}
	for _, c := range cases {
		if got := normalizeZoom(c.in); got != c.want {
			t.Errorf("normalizeZoom(%d) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestNextZoomStep(t *testing.T) {
	cases := []struct{ cur, dir, want int }{
		{100, 1, 110}, {100, -1, 90}, {110, 1, 125}, {75, -1, 67}, {67, -1, 50},
		{200, 1, 200}, // 上端では動かない
		{50, -1, 50},  // 下端では動かない
		{100, 0, 100},
		{99, 1, 110}, {99, -1, 90}, // 一覧に無い値は、最も近い段（100）を起点にする
	}
	for _, c := range cases {
		if got := nextZoomStep(c.cur, c.dir); got != c.want {
			t.Errorf("nextZoomStep(%d, %d) = %d, want %d", c.cur, c.dir, got, c.want)
		}
	}
	// 一覧を端から端まで動くと、全部の段を 1 回ずつ通る
	cur := zoomSteps[0]
	for i := 1; i < len(zoomSteps); i++ {
		cur = nextZoomStep(cur, 1)
		if cur != zoomSteps[i] {
			t.Fatalf("拡大 %d 段目: %d, want %d", i, cur, zoomSteps[i])
		}
	}
}

func withZoom(t *testing.T, percent int) {
	t.Helper()
	old := widgetZoom.Load()
	t.Cleanup(func() { widgetZoom.Store(old) })
	widgetZoom.Store(int32(percent))
}

func TestLayoutScaleFollowsZoom(t *testing.T) {
	// ウィンドウが無いとき DPI 倍率は 1.0 なので、換算倍率はズームそのもの
	withZoom(t, 50)
	if got := layoutScale(); got != 0.5 {
		t.Errorf("layoutScale = %v, want 0.5", got)
	}
	widgetZoom.Store(125)
	if got := layoutScale(); got != 1.25 {
		t.Errorf("layoutScale = %v, want 1.25", got)
	}
}

func TestZoomDefaultsInConfig(t *testing.T) {
	// 旧バージョンの config.json にはキーが無い。既定の 100% のまま残る
	withConfigFile(t, configV180)
	if got := snapshotConfig().Zoom; got != 100 {
		t.Errorf("Zoom = %d, want 100", got)
	}
	withConfigFile(t, `{"zoom": 80}`)
	if got := snapshotConfig().Zoom; got != 80 {
		t.Errorf("Zoom = %d, want 80", got)
	}
}

func TestSetOptionZoom(t *testing.T) {
	withConfigFile(t, "")
	withZoom(t, 100)
	h := securityMiddleware(newServerMux("zoom-test-token"), "zoom-test-token", 4321)
	call := func(url string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, url, nil)
		r.Host = "127.0.0.1:4321"
		r.Header.Set("X-Widget-Token", "zoom-test-token")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}

	for _, bad := range []string{"abc", "0", "99", "300", "-100", ""} {
		if w := call("/api/setoption?key=zoom&value=" + bad); w.Code != http.StatusBadRequest {
			t.Errorf("zoom=%q: %d, want 400", bad, w.Code)
		}
	}
	if got := snapshotConfig().Zoom; got != 100 {
		t.Fatalf("不正な値で Zoom が変わった: %d", got)
	}

	if w := call("/api/setoption?key=zoom&value=80"); w.Code != http.StatusOK {
		t.Fatalf("zoom=80: %d, want 200", w.Code)
	}
	if got := snapshotConfig().Zoom; got != 80 {
		t.Errorf("Zoom = %d, want 80", got)
	}
	// ウィンドウが無いテストでも、適用した値は保持される（applyZoom は UI スレッドの処理だが、その場で走る）
	if got := widgetZoom.Load(); got != 80 {
		t.Errorf("widgetZoom = %d, want 80", got)
	}

	w := call("/api/settings")
	var s struct {
		Zoom      int   `json:"zoom"`
		ZoomSteps []int `json:"zoomSteps"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &s); err != nil {
		t.Fatal(err)
	}
	if s.Zoom != 80 || len(s.ZoomSteps) != len(zoomSteps) {
		t.Errorf("/api/settings: zoom=%d steps=%v", s.Zoom, s.ZoomSteps)
	}
}
