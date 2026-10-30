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

func TestExtentsDiskNumbers(t *testing.T) {
	// 件数(4) + 詰め物(4) の後ろに、1 件 24 バイトの DISK_EXTENT（先頭 4 バイトが DiskNumber）が続く
	buf := make([]byte, 8+3*24)
	binary.LittleEndian.PutUint32(buf, 3)
	binary.LittleEndian.PutUint32(buf[8:], 2)
	binary.LittleEndian.PutUint32(buf[8+24:], 0)
	binary.LittleEndian.PutUint32(buf[8+48:], 2) // 同じディスクの別領域は 1 つにまとめる
	got := extentsDiskNumbers(buf)
	if len(got) != 2 || got[0] != 2 || got[1] != 0 {
		t.Errorf("extentsDiskNumbers = %v, want [2 0]", got)
	}
	// 件数がバッファより大きくても、読める分だけ返す
	short := make([]byte, 8+24)
	binary.LittleEndian.PutUint32(short, 5)
	binary.LittleEndian.PutUint32(short[8:], 7)
	if got := extentsDiskNumbers(short); len(got) != 1 || got[0] != 7 {
		t.Errorf("バッファ不足: %v", got)
	}
	if got := extentsDiskNumbers(nil); got != nil {
		t.Errorf("空: %v", got)
	}
}

// descriptor は STORAGE_DEVICE_DESCRIPTOR の、ベンダー（12）と製品（16）のオフセットだけを持つ最小の buffer を作る。
func descriptor(vendor, product string) []byte {
	buf := make([]byte, 40)
	put := func(field int, s string) {
		if s == "" {
			return
		}
		binary.LittleEndian.PutUint32(buf[field:], uint32(len(buf)))
		buf = append(buf, s...)
		buf = append(buf, 0)
	}
	put(12, vendor)
	put(16, product)
	return buf
}

func TestModelFromDescriptor(t *testing.T) {
	cases := []struct{ vendor, product, want string }{
		{"", "Samsung SSD 980 PRO 1TB", "Samsung SSD 980 PRO 1TB"},             // NVMe: ベンダー無し
		{"ATA     ", "Samsung SSD 860 EVO 500GB", "Samsung SSD 860 EVO 500GB"}, // SATA: バス名はベンダーではない
		{"NVMe", "KXG60ZNV512G KIOXIA", "KXG60ZNV512G KIOXIA"},                 // 同上
		{"WDC", "WD10EZEX-00BN5A0", "WDC WD10EZEX-00BN5A0"},                    // 実ベンダーは付ける
		{"Samsung", "Samsung Portable SSD T7", "Samsung Portable SSD T7"},      // 製品名が名乗っているなら重ねない
		{"", "", ""},
	}
	for _, c := range cases {
		if got := modelFromDescriptor(descriptor(c.vendor, c.product)); got != c.want {
			t.Errorf("modelFromDescriptor(%q, %q) = %q, want %q", c.vendor, c.product, got, c.want)
		}
	}
	if got := modelFromDescriptor(nil); got != "" {
		t.Errorf("空の buffer: %q", got)
	}
	// オフセットが buffer の外を指していても落ちない
	bad := make([]byte, 40)
	binary.LittleEndian.PutUint32(bad[12:], 9999)
	binary.LittleEndian.PutUint32(bad[16:], 9999)
	if got := modelFromDescriptor(bad); got != "" {
		t.Errorf("範囲外のオフセット: %q", got)
	}
}
