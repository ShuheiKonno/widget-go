package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCenterInWork(t *testing.T) {
	work := RECT{Left: 100, Top: 50, Right: 2020, Bottom: 1130} // 1920x1080、左上が (100,50)
	x, y, w, h := centerInWork(work, 920, 780, 16)
	if w != 920 || h != 780 || x != 100+(1920-920)/2 || y != 50+(1080-780)/2 {
		t.Errorf("中央に置かれていない: (%d,%d) %dx%d", x, y, w, h)
	}
	// 作業領域より大きいときは、余白を残して縮める。
	x, y, w, h = centerInWork(RECT{Left: 0, Top: 0, Right: 800, Bottom: 600}, 920, 780, 16)
	if w != 768 || h != 568 || x != 16 || y != 16 {
		t.Errorf("縮められていない: (%d,%d) %dx%d", x, y, w, h)
	}
}

func TestSettingsDarkTitleBar(t *testing.T) {
	cases := []struct {
		theme       string
		systemLight bool
		want        bool
	}{
		{"dark", true, true},
		{"light", false, false},
		{"system", true, false},
		{"system", false, true},
	}
	for _, c := range cases {
		if got := settingsDarkTitleBar(c.theme, c.systemLight); got != c.want {
			t.Errorf("settingsDarkTitleBar(%q, %v) = %v, want %v", c.theme, c.systemLight, got, c.want)
		}
	}
}

// /settings は "/" と同じく、トークンなしで読め、CSP が付き、トークンが HTML に入る。
// /api/* はトークンが無ければ拒否される。
func TestSettingsPageServedLikeIndex(t *testing.T) {
	const token = "testtoken123"
	const port = 4321
	mux := http.NewServeMux()
	mux.HandleFunc("/settings", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(getSettingsHTML(token)))
	})
	mux.HandleFunc("/api/settingschanged", func(w http.ResponseWriter, r *http.Request) {})
	srv := securityMiddleware(mux, token, port)

	withPageKey(t)
	req := httptest.NewRequest(http.MethodGet, "/settings?k="+pageAccessKey, nil)
	req.Host = "127.0.0.1:4321"
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("/settings: code=%d", rec.Code)
	}
	if csp := rec.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "nonce-"+token) {
		t.Errorf("CSP に nonce が無い: %q", csp)
	}
	body := rec.Body.String()
	if strings.Contains(body, "__WIDGET_TOKEN__") || !strings.Contains(body, "const WIDGET_TOKEN = '"+token+"'") {
		t.Error("トークンが HTML に差し替わっていない")
	}

	req = httptest.NewRequest(http.MethodGet, "/api/settingschanged", nil)
	req.Host = "127.0.0.1:4321"
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("トークン無しの /api/* は拒否されるはず: code=%d", rec.Code)
	}
}
