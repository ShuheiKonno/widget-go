//go:build windows

package sysinfo

import (
	"encoding/binary"
	"testing"
)

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

func TestExtentsTotalLength(t *testing.T) {
	// VOLUME_DISK_EXTENTS: 件数(4) + 詰め物(4) + DISK_EXTENT{DiskNumber(4)+詰め物(4), StartingOffset(8), ExtentLength(8)}
	extent := func(buf []byte, i int, offset, length uint64) {
		base := 8 + i*24
		binary.LittleEndian.PutUint64(buf[base+8:], offset)
		binary.LittleEndian.PutUint64(buf[base+16:], length)
	}
	one := make([]byte, 32)
	binary.LittleEndian.PutUint32(one, 1)
	extent(one, 0, 290455552, 509722230784)
	if got := extentsTotalLength(one); got != 509722230784 {
		t.Errorf("1 件: %d", got)
	}
	// Google Drive の仮想ドライブは、件数 1・長さ 0 の領域を返す。
	virtual := make([]byte, 32)
	binary.LittleEndian.PutUint32(virtual, 1)
	if got := extentsTotalLength(virtual); got != 0 {
		t.Errorf("長さ 0: %d", got)
	}
	// 複数の領域は合計する。
	two := make([]byte, 56)
	binary.LittleEndian.PutUint32(two, 2)
	extent(two, 0, 0, 100)
	extent(two, 1, 4096, 250)
	if got := extentsTotalLength(two); got != 350 {
		t.Errorf("2 件: %d", got)
	}
	// 件数がバッファより大きくても（ERROR_MORE_DATA）、読める分だけ数える。
	short := make([]byte, 32)
	binary.LittleEndian.PutUint32(short, 5)
	extent(short, 0, 0, 7)
	if got := extentsTotalLength(short); got != 7 {
		t.Errorf("バッファ不足: %d", got)
	}
	if got := extentsTotalLength(nil); got != 0 {
		t.Errorf("空: %d", got)
	}
}
