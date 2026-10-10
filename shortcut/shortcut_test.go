package shortcut

import (
	"os"
	"path/filepath"
	"testing"
)

func testLocations(t *testing.T) Locations {
	t.Helper()
	d := t.TempDir()
	return Locations{
		Programs: filepath.Join(d, "Programs"),
		Startup:  filepath.Join(d, "Programs", "Startup"),
	}
}

// testExe は、リンク先として実在するファイル（このテスト自身の実行ファイル）。
func testExe(t *testing.T) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return exe
}

func TestPath(t *testing.T) {
	l := Locations{Programs: `C:\P`, Startup: `C:\P\Startup`}
	if p, _ := l.Path(StartMenu); p != `C:\P\system-widget.lnk` {
		t.Errorf("StartMenu: %q", p)
	}
	if p, _ := l.Path(Startup); p != `C:\P\Startup\system-widget.lnk` {
		t.Errorf("Startup: %q", p)
	}
	if _, err := l.Path("other"); err == nil {
		t.Error("不明な種別はエラーのはず")
	}
}

func TestEnableStatusDisableRoundTrip(t *testing.T) {
	l := testLocations(t)
	exe := testExe(t)

	for _, kind := range []Kind{StartMenu, Startup} {
		st, err := Status(l, kind, exe)
		if err != nil || st.Exists {
			t.Fatalf("%s: 最初は無いはず: %+v err=%v", kind, st, err)
		}
		if err := Enable(l, kind, exe); err != nil {
			t.Fatalf("%s: Enable: %v", kind, err)
		}
		p, _ := l.Path(kind)
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("%s: ファイルが作られていない: %v", kind, err)
		}
		st, err = Status(l, kind, exe)
		if err != nil || !st.Exists || st.Mismatch || !samePath(st.Target, exe) {
			t.Fatalf("%s: 作成後の状態が違う: %+v err=%v", kind, st, err)
		}
		// 二重に Enable しても（上書きで）エラーにならない。
		if err := Enable(l, kind, exe); err != nil {
			t.Fatalf("%s: 2 回目の Enable: %v", kind, err)
		}
		if err := Disable(l, kind); err != nil {
			t.Fatalf("%s: Disable: %v", kind, err)
		}
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("%s: 削除されていない: %v", kind, err)
		}
		// 無いものを Disable してもエラーにならない。
		if err := Disable(l, kind); err != nil {
			t.Fatalf("%s: 2 回目の Disable: %v", kind, err)
		}
	}
}

func TestStatusMismatch(t *testing.T) {
	l := testLocations(t)
	exe := testExe(t)
	if err := Enable(l, Startup, exe); err != nil {
		t.Fatal(err)
	}
	st, err := Status(l, Startup, filepath.Join(filepath.Dir(exe), "other.exe"))
	if err != nil || !st.Exists || !st.Mismatch {
		t.Errorf("リンク先が違えば Mismatch のはず: %+v err=%v", st, err)
	}
}

func TestDisableLeavesOtherFiles(t *testing.T) {
	l := testLocations(t)
	if err := os.MkdirAll(l.Startup, 0o755); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(l.Startup, "other-app.lnk")
	if err := os.WriteFile(other, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Enable(l, Startup, testExe(t)); err != nil {
		t.Fatal(err)
	}
	if err := Disable(l, Startup); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(other); err != nil {
		t.Errorf("他のショートカットが消えた: %v", err)
	}
}

func TestDefaultLocationsAreAbsoluteAndDistinct(t *testing.T) {
	l, err := DefaultLocations()
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(l.Programs) || !filepath.IsAbs(l.Startup) || l.Programs == l.Startup {
		t.Errorf("既知フォルダが不正: %+v", l)
	}
}
