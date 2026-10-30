//go:build windows

package sysinfo

import "testing"

func TestDriveFromSpace(t *testing.T) {
	const gib = 1 << 30
	d, ok := driveFromSpace("C:", 500*gib, 125*gib)
	if !ok || d.Letter != "C:" || d.Total != 500*gib || d.Used != 375*gib || d.UsedPercent != 75 {
		t.Errorf("driveFromSpace = %+v, %v", d, ok)
	}
	if _, ok := driveFromSpace("D:", 0, 0); ok {
		t.Error("総容量 0（メディアなし）は除外するはず")
	}
	// 空きが総容量を超える値が返っても、使用量が負（アンダーフロー）にならない。
	d, ok = driveFromSpace("E:", 100, 200)
	if !ok || d.Used != 0 || d.UsedPercent != 0 {
		t.Errorf("空き > 総容量: %+v, %v", d, ok)
	}
}

func TestActiveFromIdle(t *testing.T) {
	cases := []struct{ idle, want float64 }{
		{100, 0}, {0, 100}, {95.5, 4.5}, {-3, 100}, {130, 0},
	}
	for _, c := range cases {
		if got := activeFromIdle(c.idle); got != c.want {
			t.Errorf("activeFromIdle(%v) = %v, want %v", c.idle, got, c.want)
		}
	}
}
