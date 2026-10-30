//go:build windows

package main

import "testing"

func TestNormalizeViewMode(t *testing.T) {
	cases := []struct{ in, want int }{
		{viewModeNormal, viewModeNormal},
		{viewModeCompact, viewModeCompact},
		{viewModeTray, viewModeNormal}, // トレイは実行時の状態。起動時の復元対象ではない
		{-1, viewModeNormal},
		{99, viewModeNormal},
	}
	for _, c := range cases {
		if got := normalizeViewMode(c.in); got != c.want {
			t.Errorf("normalizeViewMode(%d) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestStartupWidth(t *testing.T) {
	cases := []struct {
		name               string
		mode               int
		normalWidth, width int32
		want               int32
	}{
		{"コンパクトは専用幅", viewModeCompact, 300, 130, compactWindowWidth},
		{"コンパクトは保存幅が無くても専用幅", viewModeCompact, 0, 0, compactWindowWidth},
		{"フルは NormalWidth を優先", viewModeNormal, 300, 130, 300},
		{"フルで NormalWidth が無ければ width", viewModeNormal, 0, 260, 260},
		{"フルで何も無ければ既定幅", viewModeNormal, 0, 0, windowWidth},
	}
	for _, c := range cases {
		if got := startupWidth(c.mode, c.normalWidth, c.width); got != c.want {
			t.Errorf("%s: startupWidth = %d, want %d", c.name, got, c.want)
		}
	}
}
