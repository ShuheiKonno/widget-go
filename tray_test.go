package main

import (
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

// 電源モードの切替機能が使えないとき（OFF、API が無い環境）のトレイの帯は、従来どおりクロックで決まる。
func TestClockAccentColor(t *testing.T) {
	tests := []struct {
		name      string
		freq      *sysinfo.CPUFreqInfo
		wantColor uint32
		wantOK    bool
	}{
		{"nil (PDH 初期化失敗)", nil, 0, false},
		{"BaseMHz が 0", &sysinfo.CPUFreqInfo{CurrentMHz: 3000, BaseMHz: 0}, 0, false},
		{"通常域", &sysinfo.CPUFreqInfo{CurrentMHz: 1200, BaseMHz: 2400}, clockAccentNormal, true},
		{"ベース同値", &sysinfo.CPUFreqInfo{CurrentMHz: 2400, BaseMHz: 2400}, clockAccentNormal, true},
		// しきい値ちょうどは「超えていない」= 通常色 (ui/index.html の cur > base*1.02 と同じ)
		{"しきい値ちょうど", &sysinfo.CPUFreqInfo{CurrentMHz: 2400 * clockTurboRatio, BaseMHz: 2400}, clockAccentNormal, true},
		{"ターボ", &sysinfo.CPUFreqInfo{CurrentMHz: 3600, BaseMHz: 2400}, clockAccentTurbo, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			color, ok := clockAccentColor(tt.freq)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if color != tt.wantColor {
				t.Errorf("color = %#08x, want %#08x", color, tt.wantColor)
			}
		})
	}
}

// 電源モードが取れているときはそれが優先され、クロックは見ない。取れていないときだけクロックで決まる。
func TestTrayAccentColor(t *testing.T) {
	turbo := &sysinfo.CPUFreqInfo{CurrentMHz: 3600, BaseMHz: 2400}
	normal := &sysinfo.CPUFreqInfo{CurrentMHz: 1200, BaseMHz: 2400}
	saver := &sysinfo.PowerModeInfo{PlanOK: true, Active: sysinfo.PowerModeSaver}
	balanced := &sysinfo.PowerModeInfo{PlanOK: true, Active: sysinfo.PowerModeBalanced}
	tests := []struct {
		name      string
		pm        *sysinfo.PowerModeInfo
		freq      *sysinfo.CPUFreqInfo
		wantColor uint32
		wantOK    bool
	}{
		// 電源モードあり: クロックがターボでも通常でも電源モードの色
		{"省電力 + ターボ", saver, turbo, powerAccentSaver, true},
		{"バランス + ターボ", balanced, turbo, powerAccentBalanced, true},
		{"省電力 + クロック取得不可", saver, nil, powerAccentSaver, true},
		// 電源モードなし（切替機能が OFF、API が無い）: 従来どおりクロック
		{"なし + ターボ", nil, turbo, clockAccentTurbo, true},
		{"なし + 通常", nil, normal, clockAccentNormal, true},
		{"なし + クロック取得不可", nil, nil, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			color, ok := trayAccentColor(tt.pm, tt.freq)
			if ok != tt.wantOK || color != tt.wantColor {
				t.Errorf("= %#08x, %v; want %#08x, %v", color, ok, tt.wantColor, tt.wantOK)
			}
		})
	}
}

func TestPowerModeAccentColor(t *testing.T) {
	tests := []struct {
		name      string
		pm        *sysinfo.PowerModeInfo
		wantColor uint32
		wantOK    bool
	}{
		// 電源モードを取得できない環境では帯を描かない（従来の外観）
		{"nil (API が使えない)", nil, 0, false},
		{"省電力", &sysinfo.PowerModeInfo{PlanOK: true, Active: sysinfo.PowerModeSaver}, powerAccentSaver, true},
		{"バランス", &sysinfo.PowerModeInfo{PlanOK: true, Active: sysinfo.PowerModeBalanced}, powerAccentBalanced, true},
		{"パフォーマンス", &sysinfo.PowerModeInfo{PlanOK: true, Active: sysinfo.PowerModeBest}, powerAccentBest, true},
		// 不明な電源モード、電源プランがバランス以外は既定色（クロックのバーと同じ）
		{"不明な電源モード", &sysinfo.PowerModeInfo{PlanOK: true, Active: sysinfo.PowerModeOther}, powerAccentBalanced, true},
		{"バランス以外のプラン", &sysinfo.PowerModeInfo{PlanOK: false, Active: sysinfo.PowerModeOther}, powerAccentBalanced, true},
		// 電源プランがバランス以外のときは、active が 3 つのどれでも色を変えない
		{"バランス以外のプラン + パフォーマンス", &sysinfo.PowerModeInfo{PlanOK: false, Active: sysinfo.PowerModeBest}, powerAccentBalanced, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			color, ok := powerModeAccentColor(tt.pm)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if color != tt.wantColor {
				t.Errorf("color = %#08x, want %#08x", color, tt.wantColor)
			}
		})
	}
}

// トレイの代表色が、ui/index.html のクロックのバーのグラデーション両端の中間色になっていることを
// ピン留めする。CSS の色を変えたらここの端点も変え、トレイ側の定数を合わせること。
func TestPowerAccentIsGradientMidpoint(t *testing.T) {
	type rgb struct{ r, g, b uint8 }
	mid := func(a, b rgb) uint32 {
		return premulARGB(255, uint8((int(a.r)+int(b.r))/2), uint8((int(a.g)+int(b.g))/2), uint8((int(a.b)+int(b.b))/2))
	}
	tests := []struct {
		name       string
		from, to   rgb
		wantAccent uint32
	}{
		{".clock-fill.pm-saver", rgb{0x7b, 0xe0, 0xa0}, rgb{0x3c, 0xc4, 0x7c}, powerAccentSaver},
		{".clock-fill", rgb{0x5a, 0xd0, 0xff}, rgb{0x8a, 0x9c, 0xff}, powerAccentBalanced},
		{".clock-fill.pm-best", rgb{0xff, 0xc4, 0x6a}, rgb{0xff, 0x7a, 0x3c}, powerAccentBest},
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
