package aiusage

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jchv/go-webview2"
)

// stubWebView は WebView2 の生成を失敗させる（テストで本物の WebView2 を作らない）。
// 生成が呼ばれた回数を返す。
func stubWebView(t *testing.T) *int {
	t.Helper()
	calls := new(int)
	prev := newWebView
	newWebView = func(webview2.WebViewOptions) webview2.WebView { *calls++; return nil }
	t.Cleanup(func() { newWebView = prev })
	resetClaudeSnapshot()
	resetCodexSnapshot()
	return calls
}

func newTestMux() *http.ServeMux {
	mux := http.NewServeMux()
	RegisterHandlers(mux)
	return mux
}

func do(mux *http.ServeMux, method, target, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestHandlersMethodGuard(t *testing.T) {
	mux := newTestMux()
	for target, wrong := range map[string]string{
		"/api/ai/usage":       http.MethodPost,
		"/api/ai/status":      http.MethodPost,
		"/api/ai/settings":    http.MethodPost,
		"/api/ai/setoption":   http.MethodGet,
		"/api/ai/provider":    http.MethodGet,
		"/api/ai/refresh":     http.MethodGet,
		"/api/ai/relogin":     http.MethodGet,
		"/api/ai/logout":      http.MethodGet,
		"/api/ai/test-notify": http.MethodGet,
		"/api/ai/open-usage":  http.MethodGet,
		"/api/ai/open-status": http.MethodGet,
	} {
		if rec := do(mux, wrong, target, ""); rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s %s = %d, want 405", wrong, target, rec.Code)
		}
	}
}

// OFF のうちは、外部通信や補助 WebView の操作を起こすエンドポイントを 409 で断る。
func TestHandlersRequireEnabled(t *testing.T) {
	stubWebView(t)
	withConfig(t, func(c *Settings) { c.Enabled = false })
	mux := newTestMux()
	for _, c := range []struct{ method, target string }{
		{http.MethodGet, "/api/ai/status"},
		{http.MethodPost, "/api/ai/refresh"},
		{http.MethodPost, "/api/ai/relogin?provider=claude"},
		{http.MethodPost, "/api/ai/logout?provider=claude"},
		{http.MethodPost, "/api/ai/test-notify"},
	} {
		if rec := do(mux, c.method, c.target, ""); rec.Code != http.StatusConflict {
			t.Errorf("%s %s = %d, want 409", c.method, c.target, rec.Code)
		}
	}
	// 読み取りのみのものは OFF でも応答する。
	if rec := do(mux, http.MethodGet, "/api/ai/usage", ""); rec.Code != http.StatusOK {
		t.Errorf("GET /api/ai/usage = %d, want 200", rec.Code)
	}
	if rec := do(mux, http.MethodGet, "/api/ai/settings", ""); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"enabled":false`) {
		t.Errorf("GET /api/ai/settings = %d %s", rec.Code, rec.Body.String())
	}
}

func TestHandlersInvalidProvider(t *testing.T) {
	stubWebView(t)
	withConfig(t, func(c *Settings) { c.Enabled = true })
	mux := newTestMux()
	for _, target := range []string{
		"/api/ai/relogin", "/api/ai/relogin?provider=gemini",
		"/api/ai/logout", "/api/ai/logout?provider=",
		"/api/ai/open-usage", "/api/ai/open-usage?provider=https://evil.example/",
		"/api/ai/open-status?provider=x",
	} {
		if rec := do(mux, http.MethodPost, target, ""); rec.Code != http.StatusBadRequest {
			t.Errorf("POST %s = %d, want 400", target, rec.Code)
		}
	}
}

// 部分更新: 指定したフィールドだけが変わり、範囲外の値はサーバ側で矯正される。
func TestSetOptionPartialUpdateAndNormalize(t *testing.T) {
	stubWebView(t)
	withConfig(t, func(c *Settings) { *c = DefaultSettings() })
	mux := newTestMux()

	rec := do(mux, http.MethodPost, "/api/ai/setoption", `{"usagePollSeconds":5,"resetTimeFormat":"relative","traySplitDays":3}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("setoption = %d %s", rec.Code, rec.Body.String())
	}
	c := snapshotConfig()
	if c.UsagePollSeconds != minPollSeconds {
		t.Errorf("UsagePollSeconds = %d, want %d（下限へ矯正）", c.UsagePollSeconds, minPollSeconds)
	}
	if c.ResetTimeFormat != "relative" {
		t.Errorf("ResetTimeFormat = %q, want relative", c.ResetTimeFormat)
	}
	if c.TraySplitDays != 7 {
		t.Errorf("TraySplitDays = %d, want 7（不正値は既定へ）", c.TraySplitDays)
	}
	if c.StatusPollSeconds != defaultPollSeconds || !c.NotifyUsage {
		t.Errorf("指定していないフィールドが変わった: %+v", c)
	}

	// 両プロバイダーを OFF にしても、最低 1 つは有効に戻る。
	do(mux, http.MethodPost, "/api/ai/setoption", `{"claudeEnabled":false,"codexEnabled":false}`)
	if c := snapshotConfig(); !c.ClaudeEnabled || c.CodexEnabled {
		t.Errorf("両方 OFF は Claude のみへ矯正されるはず: %+v", c)
	}

	if rec := do(mux, http.MethodPost, "/api/ai/setoption", `{not json`); rec.Code != http.StatusBadRequest {
		t.Errorf("壊れた JSON = %d, want 400", rec.Code)
	}
}

// ON にすると、有効なプロバイダーぶんの WebView を生成しにいく。OFF のうちは生成しない。
func TestSetOptionEnabledCreatesViews(t *testing.T) {
	calls := stubWebView(t)
	withConfig(t, func(c *Settings) { *c = DefaultSettings() })
	mux := newTestMux()

	do(mux, http.MethodPost, "/api/ai/setoption", `{"notifyUsage":false}`)
	if *calls != 0 {
		t.Fatalf("OFF のうちは WebView を作らないはず: %d 回", *calls)
	}

	do(mux, http.MethodPost, "/api/ai/setoption", `{"enabled":true}`)
	if *calls != 2 {
		t.Fatalf("ON で Claude と Codex の 2 つを作りにいくはず: %d 回", *calls)
	}
	if s := getUsageSnapshot(); s.AuthState != "network_error" {
		t.Errorf("生成に失敗したら network_error のはず: %+v", s)
	}

	*calls = 0
	do(mux, http.MethodPost, "/api/ai/setoption", `{"enabled":true,"codexEnabled":false}`)
	if *calls != 1 {
		t.Fatalf("Codex を外したら Claude の 1 つだけ: %d 回", *calls)
	}
}

func TestSetProvider(t *testing.T) {
	stubWebView(t)
	withConfig(t, func(c *Settings) { *c = DefaultSettings() })
	mux := newTestMux()

	if rec := do(mux, http.MethodPost, "/api/ai/provider", `{"provider":"codex"}`); rec.Code != http.StatusOK {
		t.Fatalf("provider=codex = %d", rec.Code)
	}
	if c := snapshotConfig(); c.TrayProvider != "codex" {
		t.Errorf("TrayProvider = %q, want codex", c.TrayProvider)
	}
	if rec := do(mux, http.MethodPost, "/api/ai/provider", `{"provider":"gemini"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("不明なプロバイダー = %d, want 400", rec.Code)
	}
	// 無効なプロバイダーはトレイに出せない（D9）。
	withConfig(t, func(c *Settings) { c.CodexEnabled = false })
	if rec := do(mux, http.MethodPost, "/api/ai/provider", `{"provider":"codex"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("無効なプロバイダー = %d, want 400", rec.Code)
	}
}

// ログアウトは、認証データのフォルダを消し、設定が有効なら作り直す（D7。プロセスは再起動しない）。
func TestLogoutWipesDataAndRecreates(t *testing.T) {
	calls := stubWebView(t)
	root := t.TempDir()
	h := host.(*fakeHost)
	h.mu.Lock()
	prevRoot := h.root
	h.root = root
	h.mu.Unlock()
	t.Cleanup(func() { h.mu.Lock(); h.root = prevRoot; h.mu.Unlock() })
	withConfig(t, func(c *Settings) { *c = DefaultSettings(); c.Enabled = true })

	dir := claudeView.dataDir()
	if filepath.Dir(dir) != root {
		t.Fatalf("dataDir = %q, want under %q", dir, root)
	}
	if err := os.MkdirAll(filepath.Join(dir, "EBWebView", "Default"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "EBWebView", "Default", "Cookies"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	claudeView.logout()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(filepath.Join(dir, "EBWebView")); os.IsNotExist(err) && *calls >= 1 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("ログアウトで認証データが消え、WebView を作り直すはず（生成の呼び出し %d 回）", *calls)
}
