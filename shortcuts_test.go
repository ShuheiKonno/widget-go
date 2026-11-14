package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"widget-go/shortcut"
)

func testShortcutService(t *testing.T) (*shortcutService, shortcut.Locations) {
	t.Helper()
	d := t.TempDir()
	l := shortcut.Locations{Programs: filepath.Join(d, "Programs"), Startup: filepath.Join(d, "Programs", "Startup")}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return &shortcutService{
		locations: func() (shortcut.Locations, error) { return l, nil },
		exe:       func() (string, error) { return exe, nil },
	}, l
}

func doReq(h http.HandlerFunc, method, target string) (*httptest.ResponseRecorder, shortcutsResponse) {
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(method, target, nil))
	var res shortcutsResponse
	_ = json.NewDecoder(rec.Body).Decode(&res)
	return rec, res
}

func TestShortcutsSetEnableDisable(t *testing.T) {
	s, l := testShortcutService(t)

	rec, res := doReq(s.handleStatus, http.MethodGet, "/api/shortcuts")
	if rec.Code != http.StatusOK || res.StartMenu.Exists || res.Startup.Exists || res.Exe == "" {
		t.Fatalf("最初は両方とも未登録のはず: code=%d %+v", rec.Code, res)
	}

	rec, res = doReq(s.handleSet, http.MethodPost, "/api/shortcuts/set?kind=startup&enable=true")
	if rec.Code != http.StatusOK || !res.Startup.Exists || res.Startup.Mismatch || res.StartMenu.Exists {
		t.Fatalf("startup だけ作られるはず: code=%d %+v", rec.Code, res)
	}
	if p, _ := l.Path(shortcut.Startup); !fileExists(p) {
		t.Errorf("ファイルが無い: %s", p)
	}

	rec, res = doReq(s.handleSet, http.MethodPost, "/api/shortcuts/set?kind=startmenu&enable=true")
	if rec.Code != http.StatusOK || !res.Startup.Exists || !res.StartMenu.Exists {
		t.Fatalf("両方とも作られるはず: code=%d %+v", rec.Code, res)
	}

	rec, res = doReq(s.handleSet, http.MethodPost, "/api/shortcuts/set?kind=startup&enable=false")
	if rec.Code != http.StatusOK || res.Startup.Exists || !res.StartMenu.Exists {
		t.Fatalf("startup だけ消えるはず: code=%d %+v", rec.Code, res)
	}
	if p, _ := l.Path(shortcut.Startup); fileExists(p) {
		t.Errorf("削除されていない: %s", p)
	}
}

func TestShortcutsSetValidation(t *testing.T) {
	s, _ := testShortcutService(t)
	cases := []struct {
		method, target string
		want           int
	}{
		{http.MethodGet, "/api/shortcuts/set?kind=startup&enable=true", http.StatusMethodNotAllowed},
		{http.MethodPost, "/api/shortcuts/set?kind=desktop&enable=true", http.StatusBadRequest},
		{http.MethodPost, "/api/shortcuts/set?kind=startup&enable=yes", http.StatusBadRequest},
		{http.MethodPost, "/api/shortcuts/set?kind=startup", http.StatusBadRequest},
	}
	for _, c := range cases {
		if rec, _ := doReq(s.handleSet, c.method, c.target); rec.Code != c.want {
			t.Errorf("%s %s: code=%d, want %d", c.method, c.target, rec.Code, c.want)
		}
	}
}

// ショートカットを作れなかったときは、成功として返さず 500 を返す（以前は、内側の err が外の err を
// 隠していて、Enable の失敗が握りつぶされていた）。保存先のフォルダが、ファイルの下にあって作れない状態で試す。
func TestShortcutsSetReportsCreateFailure(t *testing.T) {
	s, _ := testShortcutService(t)
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	bad := shortcut.Locations{Programs: filepath.Join(blocker, "Programs"), Startup: filepath.Join(blocker, "Startup")}
	s.locations = func() (shortcut.Locations, error) { return bad, nil }

	rec, _ := doReq(s.handleSet, http.MethodPost, "/api/shortcuts/set?kind=startup&enable=true")
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("作成に失敗したのに code=%d（500 のはず）", rec.Code)
	}
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func TestStoreShortcutsLeavePortableLinksUntouched(t *testing.T) {
	s, _ := testShortcutService(t)
	s.packaged = true
	s.locations = func() (shortcut.Locations, error) {
		t.Fatal("Store build must not read or mutate portable shortcut locations")
		return shortcut.Locations{}, nil
	}
	rec, res := doReq(s.handleStatus, http.MethodGet, "/api/shortcuts")
	if rec.Code != http.StatusOK || !res.Packaged || !res.StartMenu.Exists {
		t.Fatalf("Store-managed registration was not reported: code=%d %+v", rec.Code, res)
	}
	for _, kind := range []string{"startup", "startmenu"} {
		for _, enable := range []string{"true", "false"} {
			rec, _ := doReq(s.handleSet, http.MethodPost, "/api/shortcuts/set?kind="+kind+"&enable="+enable)
			if rec.Code != http.StatusConflict {
				t.Errorf("Store shortcut mutation accepted: %s=%s code=%d", kind, enable, rec.Code)
			}
		}
	}
}
