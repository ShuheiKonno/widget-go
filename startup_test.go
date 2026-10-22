package main

import (
	"testing"
	"testing/fstest"
)

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

func TestWidgetStartupBackgroundKeepsSettingsTitleBar(t *testing.T) {
	s := newTestSkinStore(t)
	old := skins
	skins = s
	t.Cleanup(func() { skins = old })
	for _, tc := range []struct {
		theme string
		light bool
		want  rgb
	}{{"dark", true, rgb{0x12, 0x34, 0x56}}, {"light", false, rgb{0xab, 0xcd, 0xef}}, {"system", true, rgb{0xab, 0xcd, 0xef}}, {"system", false, rgb{0x12, 0x34, 0x56}}, {"unknown", true, rgb{0xab, 0xcd, 0xef}}} {
		if got := widgetStartupBackground(tc.theme, "shukei", tc.light); got != tc.want {
			t.Errorf("背景色 %+v: got %+v", tc, got)
		}
		if got := settingsDarkTitleBar(tc.theme, tc.light); got != (startupBackground(tc.theme, tc.light) == startupBgDark) {
			t.Errorf("設定タイトルバーがスキンの影響を受けた: %+v", tc)
		}
		for _, id := range []string{"default", "", "missing"} {
			if got := widgetStartupBackground(tc.theme, id, tc.light); got != startupBackground(tc.theme, tc.light) {
				t.Errorf("スキンなしのフォールバック: %s %+v", id, got)
			}
		}
	}
	// startupBackground 省略時は従来の背景に戻す。
	fixture := s.builtin.(fstest.MapFS)
	m := skinTestManifest("shukei")
	m.StartupBackground.Dark, m.StartupBackground.Light = "", ""
	fixture["shukei/skin.json"] = &fstest.MapFile{Data: skinTestJSON(t, m)}
	for _, theme := range []string{"dark", "light", "system"} {
		if got := widgetStartupBackground(theme, "shukei", true); got != startupBackground(theme, true) {
			t.Errorf("色省略時: %s %+v", theme, got)
		}
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
