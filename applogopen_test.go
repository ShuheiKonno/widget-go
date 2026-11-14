//go:build windows

package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// useTestAppLog は appLogPath をテスト用の場所に向け、app.log を開く関数を偽物に差し替える。
// 呼ばれたパスを opened に記録する（本物のアプリは開かない）。
func useTestAppLog(t *testing.T) (logPath string, opened *[]string) {
	t.Helper()
	logPath = filepath.Join(t.TempDir(), "widget-go", "app.log")
	appLogMu.Lock()
	oldPath := appLogPath
	appLogPath = logPath
	appLogMu.Unlock()
	oldOpen := openAppLogFile
	calls := []string{}
	openAppLogFile = func(p string) error { calls = append(calls, p); return nil }
	t.Cleanup(func() {
		appLogMu.Lock()
		appLogPath = oldPath
		appLogMu.Unlock()
		openAppLogFile = oldOpen
	})
	return logPath, &calls
}

func appLogOpenRequest(h http.Handler, method, url string, token bool) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, url, nil)
	r.Host = "127.0.0.1:4321"
	if token {
		r.Header.Set("X-Widget-Token", "applog-test-token")
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// /api/applog/open は、サーバー側で決めた app.log のパスで開く関数を呼ぶ。
// クエリでパスを渡しても無視する。ファイルが無いときは空のファイルを作ってから開く。
func TestAppLogOpenOpensFixedPath(t *testing.T) {
	logPath, opened := useTestAppLog(t)
	h := securityMiddleware(newServerMux("applog-test-token"), "applog-test-token", 4321)

	// まだ app.log が無い（フォルダも無い）
	if _, err := os.Stat(logPath); !os.IsNotExist(err) {
		t.Fatalf("前提: app.log が既にある: %v", err)
	}
	w := appLogOpenRequest(h, http.MethodPost, `/api/applog/open?path=C:\Windows\System32\calc.exe`, true)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %q)", w.Code, w.Body.String())
	}
	if len(*opened) != 1 || (*opened)[0] != logPath {
		t.Fatalf("開いたパス = %q, want [%q]", *opened, logPath)
	}
	if fi, err := os.Stat(logPath); err != nil || fi.Size() != 0 {
		t.Fatalf("空の app.log が作られていない: %v", err)
	}

	// 既にあるファイルは中身を変えずに開く
	if err := os.WriteFile(logPath, []byte("記録\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if w := appLogOpenRequest(h, http.MethodPost, "/api/applog/open", true); w.Code != http.StatusOK {
		t.Fatalf("2 回目: status = %d", w.Code)
	}
	if data, _ := os.ReadFile(logPath); string(data) != "記録\n" {
		t.Fatalf("既存の app.log の中身が変わった: %q", data)
	}
	if len(*opened) != 2 || (*opened)[1] != logPath {
		t.Fatalf("2 回目に開いたパス = %q", *opened)
	}
}

// POST 以外のメソッドと、秘密キー（X-Widget-Token）の無い要求は拒否し、開く関数を呼ばない。
func TestAppLogOpenRejects(t *testing.T) {
	_, opened := useTestAppLog(t)
	h := securityMiddleware(newServerMux("applog-test-token"), "applog-test-token", 4321)

	for _, m := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		w := appLogOpenRequest(h, m, "/api/applog/open", true)
		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: status = %d, want 405", m, w.Code)
		}
		if got := w.Header().Get("Allow"); got != http.MethodPost {
			t.Errorf("%s: Allow = %q, want POST", m, got)
		}
	}
	if w := appLogOpenRequest(h, http.MethodPost, "/api/applog/open", false); w.Code != http.StatusForbidden {
		t.Errorf("秘密キーなし: status = %d, want 403", w.Code)
	}
	r := httptest.NewRequest(http.MethodPost, "/api/applog/open", nil)
	r.Host = "127.0.0.1:4321"
	r.Header.Set("X-Widget-Token", "wrong-token")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Errorf("違う秘密キー: status = %d, want 403", w.Code)
	}
	if len(*opened) != 0 {
		t.Fatalf("拒否した要求で開く関数が呼ばれた: %q", *opened)
	}
}

// 保存先が決まっていない、または開けなかったときは 500 を返す。
func TestAppLogOpenErrors(t *testing.T) {
	call := func(path string, open func(string) error) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		appLogOpenHandler(func() string { return path }, open)(w, httptest.NewRequest(http.MethodPost, "/api/applog/open", nil))
		return w
	}
	called := false
	if w := call("", func(string) error { called = true; return nil }); w.Code != http.StatusInternalServerError || called {
		t.Errorf("保存先なし: status = %d, called = %v, want 500 / false", w.Code, called)
	}
	p := filepath.Join(t.TempDir(), "app.log")
	if w := call(p, func(string) error { return errors.New("関連付けが無い") }); w.Code != http.StatusInternalServerError {
		t.Errorf("開けない: status = %d, want 500", w.Code)
	}
}
