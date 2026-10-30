//go:build windows

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDiskIOViewDefaultsToNumber(t *testing.T) {
	// 旧バージョンの config.json にはキーが無い。既定の数値表示のまま残る
	withConfigFile(t, configV180)
	if got := snapshotConfig().DiskIOView; got != "number" {
		t.Errorf("DiskIOView = %q, want number", got)
	}
	withConfigFile(t, "")
	if got := snapshotConfig().DiskIOView; got != "number" {
		t.Errorf("DiskIOView = %q, want number (ファイル無し)", got)
	}
}

func TestDiskIOViewLoadsSavedValue(t *testing.T) {
	withConfigFile(t, `{"diskIoView": "graph"}`)
	if got := snapshotConfig().DiskIOView; got != "graph" {
		t.Errorf("DiskIOView = %q, want graph", got)
	}
}

func TestSetOptionDiskIOView(t *testing.T) {
	withConfigFile(t, "")
	h := securityMiddleware(newServerMux("disk-test-token"), "disk-test-token", 4321)
	call := func(method, url string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, url, nil)
		r.Host = "127.0.0.1:4321"
		r.Header.Set("X-Widget-Token", "disk-test-token")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}

	if w := call(http.MethodGet, "/api/setoption?key=diskIoView&value=bogus"); w.Code != http.StatusBadRequest {
		t.Fatalf("不正な値: %d, want 400", w.Code)
	}
	if got := snapshotConfig().DiskIOView; got != "number" {
		t.Fatalf("不正な値で DiskIOView が変わった: %q", got)
	}

	if w := call(http.MethodGet, "/api/setoption?key=diskIoView&value=graph"); w.Code != http.StatusOK {
		t.Fatalf("graph: %d, want 200", w.Code)
	}
	if got := snapshotConfig().DiskIOView; got != "graph" {
		t.Errorf("DiskIOView = %q, want graph", got)
	}

	w := call(http.MethodGet, "/api/settings")
	if w.Code != http.StatusOK {
		t.Fatalf("/api/settings: %d", w.Code)
	}
	var s map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &s); err != nil {
		t.Fatal(err)
	}
	if s["diskIoView"] != "graph" {
		t.Errorf("/api/settings の diskIoView = %v, want graph", s["diskIoView"])
	}
}
