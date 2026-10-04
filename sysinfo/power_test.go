package sysinfo

import (
	"errors"
	"testing"

	"golang.org/x/sys/windows"
)

func mustGUID(t *testing.T, s string) windows.GUID {
	t.Helper()
	g, err := windows.GUIDFromString(s)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func TestOverlayForMode(t *testing.T) {
	for _, tc := range []struct {
		mode string
		want windows.GUID
		ok   bool
	}{
		{PowerModeSaver, OverlayBetterBattery, true},
		{PowerModeBalanced, OverlayBalanced, true},
		{PowerModeBest, OverlayBestPerformance, true},
		// 許可リスト外。状態表示用の other も設定には使えない。
		{PowerModeOther, windows.GUID{}, false},
		{"", windows.GUID{}, false},
		{"Best", windows.GUID{}, false},
		{"BEST", windows.GUID{}, false},
		{" best", windows.GUID{}, false},
		{"ultimate", windows.GUID{}, false},
		{"ded574b5-45a0-4f42-8737-46345c09c238", windows.GUID{}, false},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			got, ok := OverlayForMode(tc.mode)
			if ok != tc.ok || got != tc.want {
				t.Fatalf("OverlayForMode(%q) = %s, %v; want %s, %v", tc.mode, got.String(), ok, tc.want.String(), tc.ok)
			}
		})
	}
}

func TestModeForOverlay(t *testing.T) {
	for _, tc := range []struct {
		name string
		g    windows.GUID
		want string
	}{
		{"最適な電力効率", OverlayBetterBattery, PowerModeSaver},
		{"バランス（GUID_NULL）", OverlayBalanced, PowerModeBalanced},
		{"最適なパフォーマンス", OverlayBestPerformance, PowerModeBest},
		// Windows 10 の「Better Performance」。Windows 11 のバランスではない（T1 で確認）。
		{"3af9b8d9", mustGUID(t, "{3af9b8d9-7c97-431d-ad78-34a8bfea439f}"), PowerModeOther},
		// 電源プランの GUID は overlay ではない。
		{"電源プラン（バランス）", PowerSchemeBalanced, PowerModeOther},
		{"未知", mustGUID(t, "{12345678-1234-1234-1234-123456789abc}"), PowerModeOther},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ModeForOverlay(tc.g); got != tc.want {
				t.Fatalf("ModeForOverlay = %q, want %q", got, tc.want)
			}
		})
	}
}

// 設定してよい 3 つの mode は、GUID を経由して元の mode に戻る。
func TestPowerModeRoundTrip(t *testing.T) {
	for _, mode := range []string{PowerModeSaver, PowerModeBalanced, PowerModeBest} {
		g, ok := OverlayForMode(mode)
		if !ok {
			t.Fatalf("OverlayForMode(%q) が ok でない", mode)
		}
		if got := ModeForOverlay(g); got != mode {
			t.Fatalf("%q → %s → %q", mode, g.String(), got)
		}
	}
}

func TestResolvePowerMode(t *testing.T) {
	saverPlan := mustGUID(t, "{a1841308-3541-4fab-bc81-f71556f20b4a}")
	highPlan := mustGUID(t, "{8c5e7fda-e8bf-4a96-9a85-a6e23a8c635c}")
	ultimatePlan := mustGUID(t, "{e9a42b02-d5df-448d-aa00-03f14749eb61}")
	unknownOverlay := mustGUID(t, "{3af9b8d9-7c97-431d-ad78-34a8bfea439f}")

	for _, tc := range []struct {
		name       string
		scheme     windows.GUID
		overlay    windows.GUID
		wantActive string
		wantOK     bool
	}{
		{"バランス + 最適な電力効率", PowerSchemeBalanced, OverlayBetterBattery, PowerModeSaver, true},
		{"バランス + GUID_NULL", PowerSchemeBalanced, OverlayBalanced, PowerModeBalanced, true},
		{"バランス + 最適なパフォーマンス", PowerSchemeBalanced, OverlayBestPerformance, PowerModeBest, true},
		{"バランス + 未知の overlay", PowerSchemeBalanced, unknownOverlay, PowerModeOther, true},
		// バランス以外では実効 overlay が GUID_NULL になる。バランスと誤認せず、切替不可にする。
		{"省電力プラン + GUID_NULL", saverPlan, OverlayBalanced, PowerModeOther, false},
		{"高パフォーマンスプラン + GUID_NULL", highPlan, OverlayBalanced, PowerModeOther, false},
		{"究極プラン + GUID_NULL", ultimatePlan, OverlayBalanced, PowerModeOther, false},
		// overlay が 3 つのどれかでも、電源プランがバランス以外なら強調しない。
		{"高パフォーマンスプラン + 最適", highPlan, OverlayBestPerformance, PowerModeOther, false},
		{"ゼロ値の電源プラン", windows.GUID{}, OverlayBestPerformance, PowerModeOther, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			active, ok := ResolvePowerMode(tc.scheme, tc.overlay)
			if active != tc.wantActive || ok != tc.wantOK {
				t.Fatalf("ResolvePowerMode = %q, %v; want %q, %v", active, ok, tc.wantActive, tc.wantOK)
			}
		})
	}
}

func TestSetPowerMode(t *testing.T) {
	highPlan := mustGUID(t, "{8c5e7fda-e8bf-4a96-9a85-a6e23a8c635c}")
	win32Err := errors.New("win32 error")

	scheme := func(g windows.GUID, err error) func() (windows.GUID, error) {
		return func() (windows.GUID, error) { return g, err }
	}

	for _, tc := range []struct {
		name        string
		mode        string
		scheme      func() (windows.GUID, error)
		setErr      error
		wantErr     error // errors.Is で比較。nil は成功
		wantSetGUID *windows.GUID
	}{
		{"最適なパフォーマンスへ", PowerModeBest, scheme(PowerSchemeBalanced, nil), nil, nil, &OverlayBestPerformance},
		{"最適な電力効率へ", PowerModeSaver, scheme(PowerSchemeBalanced, nil), nil, nil, &OverlayBetterBattery},
		{"バランスへ（GUID_NULL）", PowerModeBalanced, scheme(PowerSchemeBalanced, nil), nil, nil, &OverlayBalanced},
		// 拒否される場合は、設定 API を呼ばない（wantSetGUID = nil）。
		{"許可リスト外の mode", "ultimate", scheme(PowerSchemeBalanced, nil), nil, ErrPowerModeInvalid, nil},
		{"状態表示用の other は設定不可", PowerModeOther, scheme(PowerSchemeBalanced, nil), nil, ErrPowerModeInvalid, nil},
		{"空の mode", "", scheme(PowerSchemeBalanced, nil), nil, ErrPowerModeInvalid, nil},
		{"バランス以外のプラン", PowerModeBest, scheme(highPlan, nil), nil, ErrPowerPlanNotBalanced, nil},
		{"電源プランの取得に失敗", PowerModeBest, scheme(windows.GUID{}, ErrPowerModeUnsupported), nil, ErrPowerModeUnsupported, nil},
		// 設定 API の失敗はそのまま返す。
		{"設定に失敗", PowerModeBest, scheme(PowerSchemeBalanced, nil), win32Err, win32Err, &OverlayBestPerformance},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var called []windows.GUID
			set := func(g windows.GUID) error {
				called = append(called, g)
				return tc.setErr
			}
			err := setPowerMode(tc.mode, tc.scheme, set)
			if tc.wantErr == nil && err != nil || tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if tc.wantSetGUID == nil {
				if len(called) != 0 {
					t.Fatalf("設定 API を呼んではいけない: %v", called)
				}
				return
			}
			if len(called) != 1 || called[0] != *tc.wantSetGUID {
				t.Fatalf("設定された GUID = %v, want [%s]", called, tc.wantSetGUID.String())
			}
		})
	}
}

func TestGetPowerMode(t *testing.T) {
	highPlan := mustGUID(t, "{8c5e7fda-e8bf-4a96-9a85-a6e23a8c635c}")
	fail := errors.New("fail")
	ok := func(g windows.GUID) func() (windows.GUID, error) {
		return func() (windows.GUID, error) { return g, nil }
	}
	ng := func() (windows.GUID, error) { return windows.GUID{}, fail }

	for _, tc := range []struct {
		name    string
		scheme  func() (windows.GUID, error)
		overlay func() (windows.GUID, error)
		want    *PowerModeInfo // nil は取得不可
	}{
		{"バランス + 最適", ok(PowerSchemeBalanced), ok(OverlayBestPerformance), &PowerModeInfo{PlanOK: true, Active: PowerModeBest}},
		{"バランス + GUID_NULL", ok(PowerSchemeBalanced), ok(OverlayBalanced), &PowerModeInfo{PlanOK: true, Active: PowerModeBalanced}},
		{"バランス以外のプラン", ok(highPlan), ok(OverlayBalanced), &PowerModeInfo{PlanOK: false, Active: PowerModeOther}},
		{"電源プランの取得に失敗", ng, ok(OverlayBalanced), nil},
		{"overlay の取得に失敗", ok(PowerSchemeBalanced), ng, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := getPowerMode(tc.scheme, tc.overlay)
			if tc.want == nil {
				if got != nil {
					t.Fatalf("got %+v, want nil", *got)
				}
				return
			}
			if got == nil || *got != *tc.want {
				t.Fatalf("got %v, want %+v", got, *tc.want)
			}
		})
	}
}

// GUID の手書きリテラル（Data1〜Data4）の取り違えを、文字列表記から作った値との比較で検出する。
func TestPowerGUIDs(t *testing.T) {
	for _, tc := range []struct {
		name string
		got  windows.GUID
		want string
	}{
		{"バランス（電源プラン）", PowerSchemeBalanced, "{381b4222-f694-41f0-9685-ff5bb260df2e}"},
		{"最適な電力効率", OverlayBetterBattery, "{961cc777-2547-4f9d-8174-7d86181b8a7a}"},
		{"バランス（overlay なし）", OverlayBalanced, "{00000000-0000-0000-0000-000000000000}"},
		{"最適なパフォーマンス", OverlayBestPerformance, "{ded574b5-45a0-4f42-8737-46345c09c238}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			want, err := windows.GUIDFromString(tc.want)
			if err != nil {
				t.Fatal(err)
			}
			if tc.got != want {
				t.Fatalf("got %s, want %s", tc.got.String(), tc.want)
			}
		})
	}
}
