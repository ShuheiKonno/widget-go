//go:build windows

package ime

import (
	"testing"
	"time"
)

func TestShouldSkipByCooldown(t *testing.T) {
	cooldown := 2 * time.Second
	base := time.Date(2026, 4, 29, 14, 0, 0, 0, time.UTC)

	cases := []struct {
		name string
		last time.Time
		now  time.Time
		want bool
	}{
		{"初回検知（last がゼロ値）", time.Time{}, base, false},
		{"1.9秒経過 → スキップ", base, base.Add(1900 * time.Millisecond), true},
		{"2.0秒経過 → スキップしない（境界）", base, base.Add(2 * time.Second), false},
		{"2.1秒経過 → スキップしない", base, base.Add(2100 * time.Millisecond), false},
		{"5秒経過 → スキップしない", base, base.Add(5 * time.Second), false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := shouldSkipByCooldown(tc.last, tc.now, cooldown)
			if got != tc.want {
				t.Errorf("shouldSkipByCooldown(last=%v, now=%v, cooldown=%v) = %v, want %v",
					tc.last, tc.now, cooldown, got, tc.want)
			}
		})
	}
}

func TestImeClassNamesMap(t *testing.T) {
	cases := []struct {
		className string
		want      bool
	}{
		{"MSCTFIME UI", true},
		{"IME", true},
		{"IMEUI", true},
		{"Notepad", false},
		{"", false},
		{"Chrome_WidgetWin_1", false},
	}

	for _, tc := range cases {
		t.Run(tc.className, func(t *testing.T) {
			got := imeClassNames[tc.className]
			if got != tc.want {
				t.Errorf("imeClassNames[%q] = %v, want %v", tc.className, got, tc.want)
			}
		})
	}
}

func TestIsIMETarget(t *testing.T) {
	const threshold int32 = 5

	cases := []struct {
		name      string
		className string
		visible   bool
		rect      RECT
		want      bool
	}{
		{
			name:      "典型的な左上ボックス",
			className: "IME",
			visible:   true,
			rect:      RECT{Left: 2, Top: 1, Right: 152, Bottom: 23}, // 150x22
			want:      true,
		},
		{
			name:      "MSCTFIME UI クラスでも検知",
			className: "MSCTFIME UI",
			visible:   true,
			rect:      RECT{Left: 0, Top: 0, Right: 80, Bottom: 18},
			want:      true,
		},
		{
			name:      "閾値ぴったり (Left=5) は検知",
			className: "IME",
			visible:   true,
			rect:      RECT{Left: 5, Top: 5, Right: 100, Bottom: 30},
			want:      true,
		},
		{
			name:      "Left が閾値を超える",
			className: "IME",
			visible:   true,
			rect:      RECT{Left: 10, Top: 1, Right: 110, Bottom: 23},
			want:      false,
		},
		{
			name:      "Top が閾値を超える",
			className: "IME",
			visible:   true,
			rect:      RECT{Left: 1, Top: 10, Right: 100, Bottom: 30},
			want:      false,
		},
		{
			name:      "幅 0 のウィンドウは無視",
			className: "IME",
			visible:   true,
			rect:      RECT{Left: 0, Top: 0, Right: 0, Bottom: 22},
			want:      false,
		},
		{
			name:      "高さ 0 のウィンドウは無視",
			className: "IME",
			visible:   true,
			rect:      RECT{Left: 0, Top: 0, Right: 150, Bottom: 0},
			want:      false,
		},
		{
			name:      "非表示ウィンドウは無視",
			className: "IME",
			visible:   false,
			rect:      RECT{Left: 0, Top: 0, Right: 150, Bottom: 22},
			want:      false,
		},
		{
			name:      "対象外クラス名は無視",
			className: "Notepad",
			visible:   true,
			rect:      RECT{Left: 0, Top: 0, Right: 150, Bottom: 22},
			want:      false,
		},
		{
			name:      "空クラス名は無視",
			className: "",
			visible:   true,
			rect:      RECT{Left: 0, Top: 0, Right: 150, Bottom: 22},
			want:      false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := isIMETarget(tc.className, tc.visible, tc.rect, threshold)
			if got != tc.want {
				t.Errorf("isIMETarget(%q, %v, %+v, %d) = %v, want %v",
					tc.className, tc.visible, tc.rect, threshold, got, tc.want)
			}
		})
	}
}

// mockLastInputProvider は LastInputInfoProvider のテスト用実装。
type mockLastInputProvider struct {
	elapsed time.Duration
	err     error
}

func (m mockLastInputProvider) LastInputElapsed() (time.Duration, error) {
	return m.elapsed, m.err
}

func TestLastInputInfoProviderInjection(t *testing.T) {
	// Recoverer に任意の Provider を注入できることを確認する
	// （実 Win32 API を呼ばずにユニットテストを成立させる根拠）。
	mock := mockLastInputProvider{elapsed: 200 * time.Millisecond}
	r := &Recoverer{
		Cooldown:      2 * time.Second,
		Threshold:     5,
		DryRun:        true,
		InputProvider: mock,
	}
	if r.InputProvider == nil {
		t.Fatal("InputProvider は注入後 nil であってはいけない")
	}
	got, err := r.InputProvider.LastInputElapsed()
	if err != nil {
		t.Fatalf("予期しないエラー: %v", err)
	}
	if got != 200*time.Millisecond {
		t.Errorf("LastInputElapsed = %v, want 200ms", got)
	}
}

func TestDefaultConfig(t *testing.T) {
	c := DefaultConfig()
	if !c.Enabled {
		t.Error("DefaultConfig().Enabled は true であるべき")
	}
	if c.CooldownSec != 2 {
		t.Errorf("DefaultConfig().CooldownSec = %d, want 2", c.CooldownSec)
	}
	if c.ThresholdPx != 5 {
		t.Errorf("DefaultConfig().ThresholdPx = %d, want 5", c.ThresholdPx)
	}
	if c.DryRun {
		t.Error("DefaultConfig().DryRun は false であるべき")
	}
}
