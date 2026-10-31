package main

import (
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"widget-go/sysinfo"
)

// 既存の背景定数 (α=200, RGB=(20,22,40) → premul(15,17,31)) を再現できることで
// premulARGB の実装をピン留めする。
func TestPremulARGBMatchesLegacyBackground(t *testing.T) {
	const want = uint32(200)<<24 | uint32(15)<<16 | uint32(17)<<8 | uint32(31)
	if got := premulARGB(200, 20, 22, 40); got != want {
		t.Errorf("premulARGB(200,20,22,40) = %#08x, want %#08x", got, want)
	}
	if iconBgColor != want {
		t.Errorf("iconBgColor = %#08x, want %#08x", iconBgColor, want)
	}
}

func TestPremulARGBOpaqueIsPassThrough(t *testing.T) {
	const want = uint32(0xFF72B6FF)
	if got := premulARGB(255, 0x72, 0xb6, 0xff); got != want {
		t.Errorf("premulARGB(255,0x72,0xb6,0xff) = %#08x, want %#08x", got, want)
	}
}

// トレイの帯の色は、電源モードではなく、ベースクロックとの差で決まる（ウィジェットのバーと同じ規則）。
// -10% 〜 +20% は青、-11% 以下は緑、+21% 以上は橙。差は整数 % に丸めて判定する。
func TestTrayAccentColor(t *testing.T) {
	at := func(pct float64) *sysinfo.CPUFreqInfo {
		return &sysinfo.CPUFreqInfo{CurrentMHz: 2400 * (1 + pct/100), BaseMHz: 2400}
	}
	tests := []struct {
		name      string
		freq      *sysinfo.CPUFreqInfo
		wantColor uint32
		wantOK    bool
	}{
		{"nil (PDH 初期化失敗)", nil, 0, false},
		{"BaseMHz が 0", &sysinfo.CPUFreqInfo{CurrentMHz: 3000, BaseMHz: 0}, 0, false},
		{"-30%", at(-30), clockAccentLow, true},
		{"-11%", at(-11), clockAccentLow, true},
		{"-10%", at(-10), clockAccentNormal, true},
		{"-9%", at(-9), clockAccentNormal, true},
		{"ベース同値", at(0), clockAccentNormal, true},
		{"+20%", at(20), clockAccentNormal, true},
		{"+21%", at(21), clockAccentHigh, true},
		{"+60%", at(60), clockAccentHigh, true},
		// 端数は整数 % に丸めてから判定する（-10.4 → -10 で青、-10.6 → -11 で緑、+20.4 → +20 で青、+20.6 → +21 で橙）
		{"-10.4%", at(-10.4), clockAccentNormal, true},
		{"-10.6%", at(-10.6), clockAccentLow, true},
		{"+20.4%", at(20.4), clockAccentNormal, true},
		{"+20.6%", at(20.6), clockAccentHigh, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			color, ok := trayAccentColor(tt.freq)
			if ok != tt.wantOK || color != tt.wantColor {
				t.Errorf("= %#08x, %v; want %#08x, %v", color, ok, tt.wantColor, tt.wantOK)
			}
		})
	}
}

// .5 の丸めが、フロントの Math.round（+∞ 方向）と同じになること。math.Round（0 から遠い方）だと負の側で食い違う。
// 比が 2 の冪の分数（1.125 = +12.5% など）なら、浮動小数点でも差がちょうど .5 になる。
func TestClockPercentDiffRoundsHalfUp(t *testing.T) {
	tests := []struct {
		cur, base float64
		want      int
	}{
		{2250, 2000, 13},  // +12.5% → 13
		{1750, 2000, -12}, // -12.5% → -12（0 から遠い方の -13 にはならない）
		{2000, 2000, 0},
		{2500, 2000, 25},
	}
	for _, tt := range tests {
		f := &sysinfo.CPUFreqInfo{CurrentMHz: tt.cur, BaseMHz: tt.base}
		if got := clockPercentDiff(f); got != tt.want {
			t.Errorf("clockPercentDiff(%v/%v) = %d, want %d", tt.cur, tt.base, got, tt.want)
		}
	}
}

// 色の境界が、ui/index.html のバーの定数と一致していること。ずれると、トレイとバーの色が境界で食い違う。
func TestClockThresholdsMatchFrontend(t *testing.T) {
	for name, want := range map[string]int{"CLOCK_LOW_MAX_PCT": clockLowMaxPct, "CLOCK_HIGH_MIN_PCT": clockHighMinPct} {
		m := regexp.MustCompile(`const ` + name + ` = (-?\d+);`).FindStringSubmatch(indexHTML())
		if m == nil {
			t.Fatalf("ui/index.html に %s が見つからない", name)
		}
		got, _ := strconv.Atoi(m[1])
		if got != want {
			t.Errorf("%s: ui/index.html = %d, tray.go = %d", name, got, want)
		}
	}
}

// トレイの代表色が、ui/index.html のクロックのバーのグラデーション両端の中間色になっていることを
// ピン留めする。CSS の色を変えたらここの端点も変え、トレイ側の定数を合わせること。
func TestClockAccentIsGradientMidpoint(t *testing.T) {
	type rgb struct{ r, g, b uint8 }
	mid := func(a, b rgb) uint32 {
		return premulARGB(255, uint8((int(a.r)+int(b.r))/2), uint8((int(a.g)+int(b.g))/2), uint8((int(a.b)+int(b.b))/2))
	}
	tests := []struct {
		name       string
		from, to   rgb
		wantAccent uint32
	}{
		{".clock-fill.clk-low", rgb{0x7b, 0xe0, 0xa0}, rgb{0x3c, 0xc4, 0x7c}, clockAccentLow},
		{".clock-fill", rgb{0x5a, 0xd0, 0xff}, rgb{0x8a, 0x9c, 0xff}, clockAccentNormal},
		{".clock-fill.clk-high", rgb{0xff, 0xc4, 0x6a}, rgb{0xff, 0x7a, 0x3c}, clockAccentHigh},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mid(tt.from, tt.to); got != tt.wantAccent {
				t.Errorf("中間色 = %#08x, トレイの定数 = %#08x", got, tt.wantAccent)
			}
		})
	}
}

// winres.json の 4 桁 file_version が VS_FIXEDFILEINFO でどう表現されるかを再現し、
// メニュー表示が 3 桁 (git タグ・README と同じ表記) になることを確かめる。
func TestFormatFileVersion(t *testing.T) {
	tests := []struct {
		name string
		ms   uint32
		ls   uint32
		want string
	}{
		{"1.9.0.0 (現行 winres.json)", 1<<16 | 9, 0, "v1.9.0"},
		{"ビルド番号は落とす", 1<<16 | 7, 2<<16 | 345, "v1.7.2"},
		{"2桁以上の数値", 10<<16 | 20, 30<<16 | 40, "v10.20.30"},
		{"すべて 0", 0, 0, "v0.0.0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatFileVersion(tt.ms, tt.ls); got != tt.want {
				t.Errorf("formatFileVersion(%#x, %#x) = %q, want %q", tt.ms, tt.ls, got, tt.want)
			}
		})
	}
}

func TestBuildTrayTooltip(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	cpu := &sysinfo.CPUInfo{Overall: 23.4}
	mem := &sysinfo.MemoryInfo{UsedPercent: 56.6}
	wifi := &sysinfo.WiFiInfo{SSID: "home", Band: "5GHz", Channel: 36}
	gpu := &sysinfo.GPUInfo{Name: "Arc", TotalPct: f(12.4)}
	npu := &sysinfo.NPUInfo{Name: "AI Boost", UsagePct: f(3)}
	base := "CPU: 23% | Mem: 57% | 5GHz(36ch)"
	tests := []struct {
		name    string
		gpu     *sysinfo.GPUInfo
		npu     *sysinfo.NPUInfo
		showGPU bool
		want    string
	}{
		{"GPU と NPU", gpu, npu, true, base + " | GPU: 12% | NPU: 3%"},
		{"NPU なしは項目省略", gpu, nil, true, base + " | GPU: 12%"},
		{"GPU なし・NPU あり", nil, npu, true, base + " | NPU: 3%"},
		{"値なしは ---", &sysinfo.GPUInfo{}, &sysinfo.NPUInfo{}, true, base + " | GPU: --- | NPU: ---"},
		{"100 超はクランプ", &sysinfo.GPUInfo{TotalPct: f(137.2)}, &sysinfo.NPUInfo{UsagePct: f(-1)}, true, base + " | GPU: 100% | NPU: 0%"},
		{"ShowGPU=false は省略", gpu, npu, false, base},
		{"デバイスなし", nil, nil, true, base},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := buildTrayTooltip(cpu, mem, wifi, nil, tt.gpu, tt.npu, tt.showGPU); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
	if got := buildTrayTooltip(nil, nil, nil, nil, nil, nil, true); got != "CPU: --- | Mem: --- | 未接続" {
		t.Errorf("全 nil: got %q", got)
	}
}

func TestTipUTF16(t *testing.T) {
	short := tipUTF16("CPU: 1%")
	if got := syscall.UTF16ToString(short[:]); got != "CPU: 1%" {
		t.Errorf("short = %q", got)
	}
	long := tipUTF16(strings.Repeat("あ", 200))
	if long[127] != 0 {
		t.Errorf("NUL 終端されていない: %#x", long[127])
	}
	if got := len([]rune(syscall.UTF16ToString(long[:]))); got != 127 {
		t.Errorf("長さ = %d, want 127", got)
	}
	// 127 単位目がサロゲートペアの上位に当たる場合はペアごと落とす
	pair := tipUTF16(strings.Repeat("a", 126) + "😀")
	if pair[126] != 0 || pair[125] != 'a' {
		t.Errorf("サロゲートの途中で切れている: %#x %#x", pair[125], pair[126])
	}
	// 途中の NUL 以降は捨て、残りはゼロ埋め
	nul := tipUTF16("ab\x00cd")
	if nul[1] != 'b' || nul[2] != 0 || nul[3] != 0 {
		t.Errorf("途中 NUL の扱いが不正: %v", nul[:5])
	}
}
