package aiusage

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestClampPollSeconds(t *testing.T) {
	cases := []struct{ in, want int }{
		{0, minPollSeconds},     // present-and-0 / 欠落 → 下限へ（panic 防止の要）
		{-5, minPollSeconds},    // 負値 → 下限へ
		{59, minPollSeconds},    // 下限未満 → 下限
		{60, 60},                // 下限ちょうど
		{300, 300},              // 既定値はそのまま
		{3600, 3600},            // 上限ちょうど
		{99999, maxPollSeconds}, // 上限超過 → 上限
	}
	for _, c := range cases {
		if got := clampPollSeconds(c.in); got != c.want {
			t.Errorf("clampPollSeconds(%d) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestNormalizeResetTimeFormat(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", "datetime"}, // 旧 config・欠損キー → 既定
		{"datetime", "datetime"},
		{"relative", "relative"},
		{"bogus", "datetime"},
	}
	for _, c := range cases {
		if got := normalizeResetTimeFormat(c.in); got != c.want {
			t.Errorf("normalizeResetTimeFormat(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestNormalizeCompactDisplay(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", "both"}, // 旧 config・欠損キー → 既定（従来どおり 2 行を並べる）
		{"both", "both"},
		{"rotate", "rotate"},
		{"click", "click"},
		{"bogus", "both"},
	}
	for _, c := range cases {
		if got := normalizeCompactDisplay(c.in); got != c.want {
			t.Errorf("normalizeCompactDisplay(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestNormalizeRotateSeconds(t *testing.T) {
	cases := []struct{ in, want int }{
		{0, defaultRotateSeconds},  // 旧 config・欠損キー・空欄 → 下限ではなく従来どおりの 4 秒
		{-3, defaultRotateSeconds}, // 負値 → 既定
		{1, minRotateSeconds},      // 下限未満 → 下限
		{2, 2},                     // 下限ちょうど
		{4, 4},                     // 既定
		{60, 60},                   // 上限ちょうど
		{61, maxRotateSeconds},     // 上限超過 → 上限
		{99999, maxRotateSeconds},
	}
	for _, c := range cases {
		if got := normalizeRotateSeconds(c.in); got != c.want {
			t.Errorf("normalizeRotateSeconds(%d) = %d, want %d", c.in, got, c.want)
		}
	}
	if defaultRotateSeconds != 4 {
		t.Errorf("既定は従来の固定値（4 秒）のはず: %d", defaultRotateSeconds)
	}
	if got := DefaultSettings().CompactRotateSeconds; got != defaultRotateSeconds {
		t.Errorf("DefaultSettings().CompactRotateSeconds = %d, want %d", got, defaultRotateSeconds)
	}
	// 設定全体の矯正（読み込み後・更新時）でも同じ扱い
	if got := NormalizeSettings(Settings{}).CompactRotateSeconds; got != defaultRotateSeconds {
		t.Errorf("NormalizeSettings のゼロ値 = %d, want %d", got, defaultRotateSeconds)
	}
}

func TestNormalizeTraySplitDays(t *testing.T) {
	for in, want := range map[int]int{0: 0, 5: 5, 7: 7, 3: 7, -1: 7, 99: 7} {
		if got := normalizeTraySplitDays(in); got != want {
			t.Errorf("normalizeTraySplitDays(%d) = %d, want %d", in, got, want)
		}
	}
}

func TestNormalizeProviderSelection(t *testing.T) {
	claude, codex, tray := normalizeProviderSelection(false, false, "codex")
	if !claude || codex || tray != "claude" {
		t.Fatalf("empty selection = (%v,%v,%q), want Claude only", claude, codex, tray)
	}
	claude, codex, tray = normalizeProviderSelection(false, true, "claude")
	if claude || !codex || tray != "codex" {
		t.Fatalf("Codex only = (%v,%v,%q)", claude, codex, tray)
	}
	claude, codex, tray = normalizeProviderSelection(true, true, "codex")
	if !claude || !codex || tray != "codex" {
		t.Fatalf("both = (%v,%v,%q)", claude, codex, tray)
	}
	// トレイに出す側が無効なら、有効な側へ寄せる（D9）。
	claude, codex, tray = normalizeProviderSelection(true, false, "codex")
	if !claude || codex || tray != "claude" {
		t.Fatalf("tray=codex but Codex disabled = (%v,%v,%q), want tray=claude", claude, codex, tray)
	}
}

// 既定は OFF（D2）。トレイの既定は Claude 優先（D9）。
func TestDefaultSettings(t *testing.T) {
	d := DefaultSettings()
	if d.Enabled {
		t.Error("AI 使用量は既定 OFF のはず")
	}
	if !d.ClaudeEnabled || !d.CodexEnabled || d.TrayProvider != "claude" {
		t.Errorf("既定のプロバイダー = (%v,%v,%q)", d.ClaudeEnabled, d.CodexEnabled, d.TrayProvider)
	}
	if got := NormalizeSettings(d); got != d {
		t.Errorf("既定値は正規化で変わらないはず: %+v != %+v", got, d)
	}
}

func TestNormalizeSettingsRepairsZeroValue(t *testing.T) {
	got := NormalizeSettings(Settings{})
	if got.UsagePollSeconds != minPollSeconds || got.StatusPollSeconds != minPollSeconds {
		t.Errorf("ポーリング間隔が矯正されていない: %+v", got)
	}
	if got.ResetTimeFormat != "datetime" || got.OverageTipFormat != "dollar" {
		t.Errorf("表示設定が既定へ矯正されていない: %+v", got)
	}
	// 0 は「分割なし」（固定 60/80%）という有効な値なので、ゼロ値のままで正しい。
	// 旧 config でキーが欠けた場合に 7 になるのは、DefaultSettings を先に入れてから読むため。
	if got.TraySplitDays != 0 {
		t.Errorf("TraySplitDays の 0 は有効値のはず: %d", got.TraySplitDays)
	}
	if !got.ClaudeEnabled || got.TrayProvider != "claude" {
		t.Errorf("プロバイダー選択が矯正されていない: %+v", got)
	}
}

// TestPollIntervalsNeverZero は設定が 0/範囲外でも間隔が常に正であることを保証する。
// time.NewTicker / Ticker.Reset は間隔 <= 0 で panic するため、この不変条件が重要。
func TestPollIntervalsNeverZero(t *testing.T) {
	withConfig(t, func(c *Settings) {
		c.UsagePollSeconds = 0
		c.StatusPollSeconds = 0
	})
	if d := usagePollInterval(); d <= 0 {
		t.Errorf("usagePollInterval() = %v, want > 0", d)
	}
	if d := statusPollInterval(); d <= 0 {
		t.Errorf("statusPollInterval() = %v, want > 0", d)
	}
	if d := statusCacheTTL(); d <= 0 {
		t.Errorf("statusCacheTTL() = %v, want > 0", d)
	}
}

// TestStatusCacheTTLBelowInterval は TTL が監視間隔より短いことを確認する。
// 同値だと strict 比較 + 位相ずれで実効再取得間隔が約 2 倍になるため、
// 毎回のポーリングが確実に再取得できるよう TTL < 間隔 を保つ。
func TestStatusCacheTTLBelowInterval(t *testing.T) {
	withConfig(t, func(c *Settings) { c.StatusPollSeconds = 300 })
	interval := statusPollInterval()
	ttl := statusCacheTTL()
	if ttl >= interval {
		t.Errorf("statusCacheTTL() = %v, want < interval %v", ttl, interval)
	}
	if want := 270 * time.Second; ttl != want {
		t.Errorf("statusCacheTTL() = %v, want %v (= 300s * 0.9)", ttl, want)
	}
}

func TestClampPct(t *testing.T) {
	for in, want := range map[float64]int{-5: 0, 0: 0, 42.4: 42, 42.5: 43, 99.6: 100, 150: 100} {
		if got := clampPct(in); got != want {
			t.Errorf("clampPct(%v) = %d, want %d", in, got, want)
		}
	}
}

// notify.log は 1MiB を超えていたら 1 世代だけ退避して新しく始める。
func TestRotateNotifyLog(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "notify.log")

	// 上限以下: そのまま
	if err := os.WriteFile(p, make([]byte, 100), 0o644); err != nil {
		t.Fatal(err)
	}
	rotateNotifyLog(p, 100)
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("上限ちょうどでは退避しないはず: %v", err)
	}

	// 上限超過: .1 へ退避し、元は無くなる
	if err := os.WriteFile(p, make([]byte, 101), 0o644); err != nil {
		t.Fatal(err)
	}
	rotateNotifyLog(p, 100)
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Errorf("退避後に元のファイルが残っている: %v", err)
	}
	if fi, err := os.Stat(p + ".1"); err != nil || fi.Size() != 101 {
		t.Errorf("退避先が正しくない: %v %v", fi, err)
	}

	// 既存の退避は上書きされる
	if err := os.WriteFile(p, make([]byte, 200), 0o644); err != nil {
		t.Fatal(err)
	}
	rotateNotifyLog(p, 100)
	if fi, err := os.Stat(p + ".1"); err != nil || fi.Size() != 200 {
		t.Errorf("退避先が上書きされていない: %v %v", fi, err)
	}

	// ファイルが無くてもエラーにしない
	rotateNotifyLog(filepath.Join(dir, "none.log"), 100)
}
