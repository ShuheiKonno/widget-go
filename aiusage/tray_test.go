package aiusage

import (
	"bytes"
	"image"
	"image/draw"
	"image/png"
	"os"
	"strings"
	"testing"
	"time"
)

func tsPtr(t time.Time) *time.Time { return &t }

func okSnap(provider string, five, seven float64, resetsAt time.Time) UsageSnapshot {
	return UsageSnapshot{
		Provider: provider, AuthState: "ok",
		FiveHour: UsageWindow{Utilization: five, Label: "5時間", ResetsAt: tsPtr(resetsAt.Add(-5 * time.Hour))},
		SevenDay: UsageWindow{Utilization: seven, Label: "7日", ResetsAt: tsPtr(resetsAt)},
	}
}

func TestBuildTrayInfoClaude(t *testing.T) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	// 7 日窓の 2 日目（経過 2/7）: 黄の閾値は 60*2/7≒17.1、赤は 80*2/7≒22.9
	resetsAt := now.Add(5 * 24 * time.Hour)
	cfg := DefaultSettings()

	info := buildTrayInfo(cfg, okSnap("claude", 42.4, 10, resetsAt), UsageSnapshot{}, now)
	if info.Provider != "claude" || info.Error || info.Pct5h != 42 || info.Band != trayBandGreen {
		t.Errorf("緑のはず: %+v", info)
	}
	if want := "Claude モニター\n5時間: 42% / 7日: 10%"; info.Tooltip != want {
		t.Errorf("Tooltip = %q, want %q", info.Tooltip, want)
	}
	if got := buildTrayInfo(cfg, okSnap("claude", 5, 20, resetsAt), UsageSnapshot{}, now); got.Band != trayBandAmber {
		t.Errorf("7 日が黄の閾値を超えたら琥珀: %+v", got)
	}
	if got := buildTrayInfo(cfg, okSnap("claude", 5, 50, resetsAt), UsageSnapshot{}, now); got.Band != trayBandRed {
		t.Errorf("7 日が赤の閾値を超えたら赤: %+v", got)
	}
}

// トレイに出すのは TrayProvider の側だけ（D9）。
func TestBuildTrayInfoProviderSelection(t *testing.T) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	r := now.Add(3 * 24 * time.Hour)
	claude, codex := okSnap("claude", 11, 1, r), okSnap("codex", 77, 1, r)
	cfg := DefaultSettings()

	if got := buildTrayInfo(cfg, claude, codex, now); got.Provider != "claude" || got.Pct5h != 11 {
		t.Errorf("既定は Claude: %+v", got)
	}
	cfg.TrayProvider = "codex"
	if got := buildTrayInfo(cfg, claude, codex, now); got.Provider != "codex" || got.Pct5h != 77 || !strings.HasPrefix(got.Tooltip, "Codex モニター") {
		t.Errorf("Codex を選んだら Codex: %+v", got)
	}
}

// 短期枠の無いプラン（Codex の週次のみなど）は、長期枠の値を数字に出す。
func TestBuildTrayInfoWeeklyOnly(t *testing.T) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	cfg := DefaultSettings()
	cfg.TrayProvider = "codex"
	snap := UsageSnapshot{
		Provider: "codex", AuthState: "ok",
		SevenDay: UsageWindow{Utilization: 45, Label: "7日", ResetsAt: tsPtr(now.Add(2 * 24 * time.Hour))},
	}
	info := buildTrayInfo(cfg, UsageSnapshot{}, snap, now)
	if info.Error || info.Pct5h != 45 {
		t.Errorf("数字は長期枠の 45 のはず: %+v", info)
	}
	if want := "Codex モニター\n7日: 45%"; info.Tooltip != want {
		t.Errorf("Tooltip = %q, want %q（存在しない短期枠の 0%% を出さない）", info.Tooltip, want)
	}
}

func TestBuildTrayInfoExtras(t *testing.T) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	r := now.Add(3 * 24 * time.Hour)
	bal := 203.7
	limit := 50.0

	cfg := DefaultSettings()
	cfg.TrayProvider = "codex"
	codex := okSnap("codex", 4, 45, r)
	codex.CreditBalance = &bal
	if got := buildTrayInfo(cfg, UsageSnapshot{}, codex, now); !strings.HasSuffix(got.Tooltip, " / Credit: 204") {
		t.Errorf("クレジット残高が出ていない: %q", got.Tooltip)
	}

	cfg = DefaultSettings()
	claude := okSnap("claude", 4, 45, r)
	claude.Overage = &OverageInfo{AmountUsed: 12.5, SpendingLimit: &limit}
	if got := buildTrayInfo(cfg, claude, UsageSnapshot{}, now); !strings.HasSuffix(got.Tooltip, " / Eu: $12.50") {
		t.Errorf("追加利用枠（ドル）が出ていない: %q", got.Tooltip)
	}
	cfg.OverageTipFormat = "percent"
	if got := buildTrayInfo(cfg, claude, UsageSnapshot{}, now); !strings.HasSuffix(got.Tooltip, " / Eu: 25%") {
		t.Errorf("追加利用枠（%%）が出ていない: %q", got.Tooltip)
	}
	// 利用額 0 で上限も無い（実質無効）は出さない
	claude.Overage = &OverageInfo{}
	if got := buildTrayInfo(cfg, claude, UsageSnapshot{}, now); strings.Contains(got.Tooltip, "Eu:") {
		t.Errorf("無効な追加利用枠は出さない: %q", got.Tooltip)
	}
}

func TestBuildTrayInfoErrorStates(t *testing.T) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	cfg := DefaultSettings()
	for _, c := range []struct {
		state, lastErr, wantTip string
	}{
		{"needs_login", "", "Claude モニター — 未ログイン\n右クリック → ログイン"},
		{"network_error", "fetch timeout", "Claude モニター — 取得失敗\nfetch timeout"},
		{"init", "", "Claude モニター — 取得中…"},
		{"", "", "Claude モニター — エラー"},
	} {
		info := buildTrayInfo(cfg, UsageSnapshot{AuthState: c.state, LastError: c.lastErr}, UsageSnapshot{}, now)
		if !info.Error || info.Tooltip != c.wantTip {
			t.Errorf("state=%q: %+v, want error tooltip %q", c.state, info, c.wantTip)
		}
	}
	long := strings.Repeat("あ", 200)
	info := buildTrayInfo(cfg, UsageSnapshot{AuthState: "network_error", LastError: long}, UsageSnapshot{}, now)
	if n := len([]rune(info.Tooltip)); n > 80+len([]rune("Claude モニター — 取得失敗\n"))+1 {
		t.Errorf("長いエラー文は 80 文字で切る: %d 文字", n)
	}
}

// 同じ見た目なら同じキー。値・色・プロバイダー・エラーのどれかが変われば別のキー。
func TestTrayIconKey(t *testing.T) {
	a := TrayInfo{Provider: "claude", Pct5h: 42, Band: trayBandGreen, Tooltip: "x"}
	b := a
	b.Tooltip = "y" // ツールチップだけの違いではアイコンを作り直さない
	if a.IconKey() != b.IconKey() {
		t.Error("ツールチップだけの違いでキーが変わった")
	}
	for name, mod := range map[string]func(*TrayInfo){
		"pct":      func(x *TrayInfo) { x.Pct5h = 43 },
		"band":     func(x *TrayInfo) { x.Band = trayBandRed },
		"provider": func(x *TrayInfo) { x.Provider = "codex" },
		"error":    func(x *TrayInfo) { x.Error = true },
	} {
		c := a
		mod(&c)
		if c.IconKey() == a.IconKey() {
			t.Errorf("%s が変わってもキーが同じ", name)
		}
	}
}

func opaquePixels(img *image.RGBA) int {
	n := 0
	for i := 3; i < len(img.Pix); i += 4 {
		if img.Pix[i] > 0 {
			n++
		}
	}
	return n
}

func TestDrawTrayIcon(t *testing.T) {
	for _, band := range []TrayBand{trayBandGreen, trayBandAmber, trayBandRed} {
		for _, pct := range []int{0, 5, 9, 10, 56, 99, 100} { // 100 は 3 桁の専用の詰め描画
			img := drawTrayIconImage(pct, band)
			if img.Bounds() != image.Rect(0, 0, trayIconSize, trayIconSize) {
				t.Fatalf("pct=%d band=%d: size %v", pct, band, img.Bounds())
			}
			if opaquePixels(img) < 100 {
				t.Errorf("pct=%d band=%d: 角丸四角が描かれていない（不透明 %d px）", pct, band, opaquePixels(img))
			}
			// 角は透明（角丸）
			if img.RGBAAt(0, 0).A != 0 || img.RGBAAt(trayIconSize-1, trayIconSize-1).A != 0 {
				t.Errorf("pct=%d band=%d: 角が丸くない", pct, band)
			}
		}
	}
	// 色（バンド）と数字が違えば、画像も違う
	if bytes.Equal(drawTrayIconImage(42, trayBandGreen).Pix, drawTrayIconImage(42, trayBandRed).Pix) {
		t.Error("バンドが違っても画像が同じ")
	}
	if bytes.Equal(drawTrayIconImage(42, trayBandGreen).Pix, drawTrayIconImage(43, trayBandGreen).Pix) {
		t.Error("数字が違っても画像が同じ")
	}
	// 同じ入力は同じ画像
	if !bytes.Equal(drawTrayIconImage(42, trayBandAmber).Pix, drawTrayIconImage(42, trayBandAmber).Pix) {
		t.Error("同じ入力で画像が変わる")
	}
	// 範囲外の値は 0〜100 に収める（落ちない）
	_ = drawTrayIconImage(-5, trayBandGreen)
	_ = drawTrayIconImage(250, trayBandRed)

	errImg := drawTrayIconErrorImage()
	if errImg.Bounds() != image.Rect(0, 0, trayIconSize, trayIconSize) || opaquePixels(errImg) < 100 {
		t.Errorf("エラーアイコンが正しくない: %v 不透明 %d px", errImg.Bounds(), opaquePixels(errImg))
	}
	if bytes.Equal(errImg.Pix, drawTrayIconImage(0, trayBandGreen).Pix) {
		t.Error("エラーアイコンが通常アイコンと同じ")
	}
}

func TestTrayInfoImage(t *testing.T) {
	ok := TrayInfo{Provider: "claude", Pct5h: 42, Band: trayBandAmber}
	if !bytes.Equal(ok.Image().Pix, drawTrayIconImage(42, trayBandAmber).Pix) {
		t.Error("通常: Image() が drawTrayIconImage と違う")
	}
	bad := TrayInfo{Provider: "claude", Error: true}
	if !bytes.Equal(bad.Image().Pix, drawTrayIconErrorImage().Pix) {
		t.Error("エラー: Image() が drawTrayIconErrorImage と違う")
	}
}

// DUMP_TRAY_PREVIEW=<出力先の PNG> go test ./aiusage -run TestDumpTrayPreview
// 主要な状態を、原寸と 8 倍拡大で 1 枚の画像に出す（目視確認用。ClaudeMonitor の
// assets/tray-preview.png と同じ見せ方）。
func TestDumpTrayPreview(t *testing.T) {
	out := os.Getenv("DUMP_TRAY_PREVIEW")
	if out == "" {
		t.Skip("DUMP_TRAY_PREVIEW=<出力先の PNG> で有効化")
	}
	samples := []TrayInfo{
		{Pct5h: 25, Band: trayBandGreen}, {Pct5h: 56, Band: trayBandGreen}, {Pct5h: 68, Band: trayBandAmber},
		{Pct5h: 95, Band: trayBandRed}, {Pct5h: 100, Band: trayBandRed}, {Error: true},
	}
	const scale, pad = 8, 4
	cell := trayIconSize*scale + pad
	canvas := image.NewRGBA(image.Rect(0, 0, len(samples)*cell+pad, trayIconSize*scale+2*pad))
	for i := range canvas.Pix {
		canvas.Pix[i] = 0x50 // 暗いグレーの背景（透明部分が分かるように）
	}
	for i, s := range samples {
		img := s.Image()
		x0 := pad + i*cell
		for y := 0; y < trayIconSize; y++ {
			for x := 0; x < trayIconSize; x++ {
				c := img.RGBAAt(x, y)
				for dy := 0; dy < scale; dy++ {
					for dx := 0; dx < scale; dx++ {
						canvas.SetRGBA(x0+x*scale+dx, pad+y*scale+dy, c)
					}
				}
			}
		}
		_ = draw.Over
	}
	f, err := os.Create(out)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, canvas); err != nil {
		t.Fatal(err)
	}
}
