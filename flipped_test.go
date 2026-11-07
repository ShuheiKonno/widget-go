package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// 旧バージョンの config.json には "flipped" が無い。どの区画も表面のまま。
func TestLoadConfigWithoutFlippedUsesDefaults(t *testing.T) {
	withConfigFile(t, configV180)
	if f := snapshotConfig().Flipped; f != (FlippedConfig{}) {
		t.Errorf("\"flipped\" が無い config は全区画が表面のはず: %+v", f)
	}
	withConfigFile(t, "")
	if f := snapshotConfig().Flipped; f != (FlippedConfig{}) {
		t.Errorf("config.json が無いときは全区画が表面のはず: %+v", f)
	}
}

// 保存された面は、再起動（再読み込み）で保たれる。一部の区画だけ書かれていても、他は表面のまま。
func TestLoadConfigFlippedKeepsSavedFace(t *testing.T) {
	withConfigFile(t, `{"flipped": {"disk": true, "network": true}}`)
	f := snapshotConfig().Flipped
	if !f.Disk || !f.Network || f.AI {
		t.Errorf("保存した面が復元されていない: %+v", f)
	}
}

func TestSetFlippedPersistsAllSections(t *testing.T) {
	path := withConfigFile(t, "")
	for _, name := range []string{"disk", "network", "ai"} {
		if err := setFlipped(name, "true"); err != nil {
			t.Errorf("setFlipped(%q, true): %v", name, err)
		}
	}
	if f := snapshotConfig().Flipped; f != (FlippedConfig{Disk: true, Network: true, AI: true}) {
		t.Errorf("全区画が裏面になるはず: %+v", f)
	}
	if err := setFlipped("disk", "false"); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var saved struct {
		Flipped map[string]bool `json:"flipped"`
	}
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	if len(saved.Flipped) != 3 || saved.Flipped["disk"] || !saved.Flipped["network"] || !saved.Flipped["ai"] {
		t.Errorf("config.json の flipped が正しくない: %v\n%s", saved.Flipped, data)
	}

	// 再起動（再読み込み）しても同じ面で始まる
	loadConfig()
	if f := snapshotConfig().Flipped; f != (FlippedConfig{Network: true, AI: true}) {
		t.Errorf("再読み込みで面が保たれていない: %+v", f)
	}
}

// 許可リスト外の区画名と、true/false 以外の値は、設定を変えずにエラーにする。
func TestSetFlippedRejectsInvalid(t *testing.T) {
	path := withConfigFile(t, "")
	before := snapshotConfig().Flipped
	for _, c := range []struct{ name, val string }{
		{"cpu", "true"},        // 反転できない区画
		{"wifi", "true"},       // 区画名は network
		{"", "true"},           // 空
		{"disk ", "true"},      // 余分な空白
		{"Disk", "true"},       // 大文字小文字は区別する
		{"disk", "yes"},        // 値は true / false のみ
		{"disk", ""},           // 空
		{"disk", "TRUE"},       // 大文字小文字は区別する
		{"../disk", "true"},    // パスのような値
		{"ai.enabled", "true"}, // 他の設定のキーは触れない
	} {
		if err := setFlipped(c.name, c.val); err == nil {
			t.Errorf("setFlipped(%q, %q) はエラーのはず", c.name, c.val)
		}
	}
	if after := snapshotConfig().Flipped; after != before {
		t.Errorf("不正な入力で設定が変わった: %+v → %+v", before, after)
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("不正な入力で config.json が書き出された")
	}
}

// /api/settings の応答に載せる形（フロントが読むキー）が、反転できる区画そろっていること。
func TestFlippedJSONKeys(t *testing.T) {
	data, err := json.Marshal(FlippedConfig{})
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"disk", "network", "ai"} {
		if !strings.Contains(string(data), `"`+k+`":`) {
			t.Errorf("JSON に %q が無い: %s", k, data)
		}
	}
}

// フロントは /api/setoption?key=flip.<name> で面を保存し、起動時に /api/settings から読み戻す。
func TestSetOptionFlipAndSettingsRoundTrip(t *testing.T) {
	withConfigFile(t, "")
	h := securityMiddleware(newServerMux("flip-test-token"), "flip-test-token", 4321)
	call := func(url string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, url, nil)
		r.Host = "127.0.0.1:4321"
		r.Header.Set("X-Widget-Token", "flip-test-token")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}

	for _, u := range []string{
		"/api/setoption?key=flip.cpu&value=true",   // 反転できない区画
		"/api/setoption?key=flip.disk&value=bogus", // 値が不正
		"/api/setoption?key=flip.&value=true",      // 区画名が空
	} {
		if w := call(u); w.Code != http.StatusBadRequest {
			t.Errorf("%s: %d, want 400", u, w.Code)
		}
	}
	if f := snapshotConfig().Flipped; f != (FlippedConfig{}) {
		t.Fatalf("不正な要求で面が変わった: %+v", f)
	}

	if w := call("/api/setoption?key=flip.disk&value=true"); w.Code != http.StatusOK {
		t.Fatalf("flip.disk: %d, want 200", w.Code)
	}
	if w := call("/api/setoption?key=flip.network&value=true"); w.Code != http.StatusOK {
		t.Fatalf("flip.network: %d, want 200", w.Code)
	}

	w := call("/api/settings")
	if w.Code != http.StatusOK {
		t.Fatalf("/api/settings: %d", w.Code)
	}
	var s struct {
		Flipped map[string]bool `json:"flipped"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &s); err != nil {
		t.Fatal(err)
	}
	if !s.Flipped["disk"] || !s.Flipped["network"] || s.Flipped["ai"] {
		t.Errorf("/api/settings の flipped = %v, want disk/network が true", s.Flipped)
	}

	// 表面に戻したことも保存される
	if w := call("/api/setoption?key=flip.disk&value=false"); w.Code != http.StatusOK {
		t.Fatalf("flip.disk=false: %d", w.Code)
	}
	if snapshotConfig().Flipped.Disk {
		t.Error("flip.disk=false で表面に戻るはず")
	}
}
