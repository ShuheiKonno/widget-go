package main

import "testing"

func TestStartupBackground(t *testing.T) {
	cases := []struct {
		theme       string
		systemLight bool
		want        rgb
	}{
		{"dark", false, startupBgDark},
		{"dark", true, startupBgDark},
		{"light", false, startupBgLight},
		{"light", true, startupBgLight},
		{"system", false, startupBgDark},
		{"system", true, startupBgLight},
		{"", true, startupBgLight},
		{"unknown", false, startupBgDark},
	}
	for _, c := range cases {
		if got := startupBackground(c.theme, c.systemLight); got != c.want {
			t.Errorf("startupBackground(%q, %v) = %+v, want %+v", c.theme, c.systemLight, got, c.want)
		}
	}
}

func TestStartupBackgroundArgbHex(t *testing.T) {
	if got := (rgb{0x0D, 0x0E, 0x12}).argbHex(); got != "FF0D0E12" {
		t.Errorf("argbHex = %q", got)
	}
}

func TestShowMainWindowOnceRunsOnlyOnce(t *testing.T) {
	prevShown := mainShown.Load()
	mainShown.Store(false)
	var reasons []string
	prevLog := startupShowLog
	startupShowLog = func(r string) { reasons = append(reasons, r) }
	t.Cleanup(func() { startupShowLog = prevLog; mainShown.Store(prevShown) })

	showMainWindowOnce("ready")
	showMainWindowOnce("timeout")
	showMainWindowOnce("ready")
	if len(reasons) != 1 || reasons[0] != "ready" {
		t.Errorf("最初の 1 回だけ実行されるはず: %v", reasons)
	}
}
