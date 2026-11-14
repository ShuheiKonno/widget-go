//go:build windows

package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// fakeIMEWatcher は IME 監視の起動・停止を差し替え、本物の WinEvent Hook を張らずに呼び出しを記録する。
type fakeIMEWatcher struct {
	starts, stops int
	startErr      error
}

func useFakeIMEWatcher(t *testing.T) *fakeIMEWatcher {
	t.Helper()
	f := &fakeIMEWatcher{}
	oldStart, oldStop := imeStartWatcher, imeStopWatcher
	imeStartWatcher = func(IMEConfig) error { f.starts++; return f.startErr }
	imeStopWatcher = func() error { f.stops++; return nil }
	t.Cleanup(func() { imeStartWatcher, imeStopWatcher = oldStart, oldStop })
	return f
}

// /api/setoption?key=imeWatch で IME 自動復旧を ON/OFF し、保存と /api/settings に反映されること、
// すぐに監視を起動・停止すること。
func TestSetOptionIMEWatch(t *testing.T) {
	path := withConfigFile(t, "")
	fake := useFakeIMEWatcher(t)
	h := securityMiddleware(newServerMux("ime-test-token"), "ime-test-token", 4321)
	call := func(url string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, url, nil)
		r.Host = "127.0.0.1:4321"
		r.Header.Set("X-Widget-Token", "ime-test-token")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	settingsIMEWatch := func() interface{} {
		t.Helper()
		w := call("/api/settings")
		if w.Code != http.StatusOK {
			t.Fatalf("/api/settings: %d", w.Code)
		}
		var s map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &s); err != nil {
			t.Fatal(err)
		}
		return s["imeWatch"]
	}

	// 既定は ON
	if !snapshotConfig().IME.Enabled {
		t.Fatal("既定で IME.Enabled が false")
	}
	if got := settingsIMEWatch(); got != true {
		t.Fatalf("/api/settings の imeWatch = %v, want true (既定)", got)
	}

	// 不正な値は 400。設定は変えず、起動・停止もしない
	for _, v := range []string{"", "1", "TRUE", "yes", "off"} {
		if w := call("/api/setoption?key=imeWatch&value=" + v); w.Code != http.StatusBadRequest {
			t.Errorf("value=%q: %d, want 400", v, w.Code)
		}
	}
	if !snapshotConfig().IME.Enabled {
		t.Fatal("不正な値で IME.Enabled が変わった")
	}
	if fake.starts != 0 || fake.stops != 0 {
		t.Fatalf("不正な値で起動・停止が呼ばれた: starts=%d stops=%d", fake.starts, fake.stops)
	}

	// OFF: 保存して停止する
	if w := call("/api/setoption?key=imeWatch&value=false"); w.Code != http.StatusOK {
		t.Fatalf("false: %d, want 200 (body %q)", w.Code, w.Body.String())
	}
	if snapshotConfig().IME.Enabled {
		t.Fatal("false を保存したのに IME.Enabled が true")
	}
	if fake.stops != 1 || fake.starts != 0 {
		t.Fatalf("OFF で starts=%d stops=%d, want 0/1", fake.starts, fake.stops)
	}
	if got := settingsIMEWatch(); got != false {
		t.Fatalf("/api/settings の imeWatch = %v, want false", got)
	}
	data, err := os.ReadFile(path)
	if err != nil || !strings.Contains(strings.Join(strings.Fields(string(data)), ""), `"ime":{"enabled":false`) {
		t.Fatalf("config.json に ime.enabled=false が無い: %v\n%s", err, data)
	}
	// 再読み込みしても OFF のまま
	loadConfig()
	if snapshotConfig().IME.Enabled {
		t.Fatal("再読み込みで ON に戻った")
	}

	// ON: 保存して起動する。ほかの IME 設定（閾値など）は既定のまま残る
	if w := call("/api/setoption?key=imeWatch&value=true"); w.Code != http.StatusOK {
		t.Fatalf("true: %d, want 200", w.Code)
	}
	if c := snapshotConfig().IME; !c.Enabled || c.ThresholdPx != 5 || c.CooldownSec != 2 {
		t.Fatalf("ON にした後の IME 設定 = %+v", c)
	}
	if fake.starts != 1 || fake.stops != 1 {
		t.Fatalf("ON で starts=%d stops=%d, want 1/1", fake.starts, fake.stops)
	}
	if got := settingsIMEWatch(); got != true {
		t.Fatalf("/api/settings の imeWatch = %v, want true", got)
	}

	// 起動に失敗したときは 500。設定は保存したまま
	fake.startErr = errors.New("フックを張れない")
	if err := setIMEWatch(false); err != nil {
		t.Fatal(err)
	}
	if w := call("/api/setoption?key=imeWatch&value=true"); w.Code != http.StatusInternalServerError {
		t.Fatalf("起動失敗: %d, want 500", w.Code)
	}
	if !snapshotConfig().IME.Enabled {
		t.Fatal("起動に失敗したら設定まで戻った")
	}
}
