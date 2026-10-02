package main

import (
	"image"
	"image/color"
	"strings"
	"testing"
	"time"

	"widget-go/aiusage"
)

// "usage" を選んだまま AI 使用量を OFF にすると、実際の表示だけアイコンのみに落ちる（D3）。
func TestEffectiveTrayDisplay(t *testing.T) {
	cases := []struct {
		mode      int
		aiEnabled bool
		want      int
	}{
		{TRAY_DISPLAY_USAGE, true, TRAY_DISPLAY_USAGE},
		{TRAY_DISPLAY_USAGE, false, TRAY_DISPLAY_ICON}, // AI が OFF のときは落とす
		{TRAY_DISPLAY_CPU, true, TRAY_DISPLAY_CPU},
		{TRAY_DISPLAY_CPU, false, TRAY_DISPLAY_CPU}, // 他の表示は AI の ON/OFF に影響されない
		{TRAY_DISPLAY_ICON, true, TRAY_DISPLAY_ICON},
		{TRAY_DISPLAY_ICON, false, TRAY_DISPLAY_ICON},
	}
	for _, c := range cases {
		if got := effectiveTrayDisplay(c.mode, c.aiEnabled); got != c.want {
			t.Errorf("effectiveTrayDisplay(%d, %v) = %d, want %d", c.mode, c.aiEnabled, got, c.want)
		}
	}
}

func TestSplitTipParts(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"CPU: 12%", []string{"CPU: 12%"}},
		{"CPU: 12% | Mem: 45% | 5GHz(36ch)", []string{"CPU: 12%", "Mem: 45%", "5GHz(36ch)"}},
		{"a | b | ", []string{"a", "b"}},
		{"Ethernet 1Gbps | GPU: ---", []string{"Ethernet 1Gbps", "GPU: ---"}},
	}
	for _, c := range cases {
		got := splitTipParts(c.in)
		if len(got) != len(c.want) {
			t.Errorf("splitTipParts(%q) = %q, want %q", c.in, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("splitTipParts(%q)[%d] = %q, want %q", c.in, i, got[i], c.want[i])
			}
		}
	}
}

// AI の行を先頭に、収まる範囲で widget-go の項目を足す。収まらない項目は途中で切らず、
// 末尾から落とす。結果は szTip の上限（127 単位）を超えない。
func TestBuildUsageTooltip(t *testing.T) {
	ai := "Claude モニター\n5時間: 42% / 7日: 18%"
	sys := "CPU: 12% | Mem: 45% | 5GHz(36ch) | GPU: 14% | NPU: 0%"

	got := buildUsageTooltip(ai, sys)
	if !strings.HasPrefix(got, ai+"\nCPU: 12%") {
		t.Errorf("AI の行の後に 1 項目目が続くはず: %q", got)
	}
	if utf16Len(got) > tipMaxUnits {
		t.Errorf("上限 %d を超えた: %d 単位 %q", tipMaxUnits, utf16Len(got), got)
	}
	if !strings.Contains(got, "NPU: 0%") {
		t.Errorf("収まるなら全項目が入るはず: %q", got)
	}

	// 長い AI の行（クレジットと追加利用枠つき）: 収まらない項目を末尾から落とす。切れ端は残さない。
	long := "Claude モニター\n5時間: 100% / 7日: 100% / Credit: $203.70 / Eu: $12.50"
	got = buildUsageTooltip(long, sys)
	if utf16Len(got) > tipMaxUnits {
		t.Errorf("上限を超えた: %d 単位 %q", utf16Len(got), got)
	}
	if !strings.HasPrefix(got, long) {
		t.Errorf("AI の行は必ず先頭に全文が残る: %q", got)
	}
	for _, part := range splitTipParts(strings.TrimPrefix(got, long+"\n")) {
		if !strings.Contains(sys, part) {
			t.Errorf("項目が途中で切れている: %q（元: %q）", part, sys)
		}
	}
	if strings.Contains(got, "NPU: 0%") && strings.Contains(got, "GPU: 14%") == false {
		t.Errorf("後ろの項目だけが残っている（末尾から落とすはず）: %q", got)
	}

	// 項目が 1 つも収まらないときは AI の行だけ
	hugeAI := strings.Repeat("あ", 120)
	if got := buildUsageTooltip(hugeAI, sys); got != hugeAI {
		t.Errorf("収まらないときは AI の行だけのはず: %q", got)
	}
	// widget-go 側が空なら AI の行だけ
	if got := buildUsageTooltip(ai, ""); got != ai {
		t.Errorf("sys が空: %q", got)
	}
}

// 絵文字などサロゲートペアを含んでも、上限の計算が UTF-16 単位で正しい。
func TestBuildUsageTooltipCountsUTF16Units(t *testing.T) {
	ai := strings.Repeat("😀", 55) // 55 文字 = 110 単位
	got := buildUsageTooltip(ai, "CPU: 12% | Mem: 45%")
	if utf16Len(got) > tipMaxUnits {
		t.Errorf("上限を超えた: %d 単位", utf16Len(got))
	}
	if got != ai+"\nCPU: 12%" && got != ai {
		t.Errorf("収まる分だけのはず: %q", got)
	}
}

func TestImageToHICON(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			img.SetRGBA(x, y, color.RGBA{R: 200, G: 100, B: 90, A: 255})
		}
	}
	h := imageToHICON(img)
	if h == 0 {
		t.Fatal("HICON を作れなかった")
	}
	procDestroyIcon.Call(h)

	// 正方形でない画像は作らない
	if got := imageToHICON(image.NewRGBA(image.Rect(0, 0, 16, 8))); got != 0 {
		procDestroyIcon.Call(got)
		t.Error("正方形でない画像は 0 を返すはず")
	}
	// aiusage の実際のアイコンでも作れる（通常・エラー）
	for _, info := range []aiusage.TrayInfo{{Provider: "claude", Pct5h: 42}, {Provider: "claude", Error: true}} {
		if h := buildUsageIcon(info); h == 0 {
			t.Errorf("buildUsageIcon(%+v) が 0", info)
		} else {
			procDestroyIcon.Call(h)
		}
	}
}

// kickTray は待たない（バッファが埋まっていても止まらない）。
func TestKickTrayNeverBlocks(t *testing.T) {
	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			kickTray()
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("kickTray が止まった")
	}
	select { // 溜まった合図を掃除する
	case <-trayKick:
	default:
	}
}
