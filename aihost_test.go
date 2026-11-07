package main

import (
	"os"
	"strings"
	"testing"
	"unicode/utf16"

	"widget-go/aiusage"
)

// v1.9.3 までの config.json には "ai" が無い。既定（OFF）が残ること。
func TestLoadConfigWithoutAIUsesDefaults(t *testing.T) {
	withConfigFile(t, configV180)
	ai := snapshotConfig().AI
	if ai != aiusage.DefaultSettings() {
		t.Errorf("\"ai\" が無い config では既定になるはず:\n got  %+v\n want %+v", ai, aiusage.DefaultSettings())
	}
	if ai.Enabled {
		t.Error("AI 使用量は既定 OFF のはず（D2）")
	}
}

// 一部のキーだけある "ai" は、欠けたキーが既定のまま残る。
func TestLoadConfigPartialAIKeepsDefaults(t *testing.T) {
	withConfigFile(t, `{"ai": {"enabled": true, "codexEnabled": false}}`)
	ai := snapshotConfig().AI
	if !ai.Enabled || ai.CodexEnabled {
		t.Errorf("指定したキーが反映されていない: %+v", ai)
	}
	if !ai.ClaudeEnabled || !ai.NotifyUsage || ai.UsagePollSeconds != 300 || ai.TraySplitDays != 7 || ai.TrayProvider != "claude" {
		t.Errorf("欠けたキーが既定のまま残っていない: %+v", ai)
	}
}

// 自動で反転の間隔: キーが無い旧バージョンの config.json は従来の 4 秒、保存した値は再起動で保たれ、範囲外は矯正される。
func TestLoadConfigCompactRotateSeconds(t *testing.T) {
	withConfigFile(t, `{"ai": {"enabled": true}}`)
	if got := snapshotConfig().AI.CompactRotateSeconds; got != 4 {
		t.Errorf("キーが無いときの CompactRotateSeconds = %d, want 4", got)
	}
	withConfigFile(t, `{"ai": {"compactRotateSeconds": 15}}`)
	if got := snapshotConfig().AI.CompactRotateSeconds; got != 15 {
		t.Errorf("保存した値 = %d, want 15", got)
	}
	withConfigFile(t, `{"ai": {"compactRotateSeconds": 1}}`)
	if got := snapshotConfig().AI.CompactRotateSeconds; got != 2 {
		t.Errorf("下限未満 = %d, want 2", got)
	}
	withConfigFile(t, `{"ai": {"compactRotateSeconds": 100000}}`)
	if got := snapshotConfig().AI.CompactRotateSeconds; got != 60 {
		t.Errorf("上限超過 = %d, want 60", got)
	}
}

// 手編集で範囲外になった値は、読み込み時に矯正される。
func TestLoadConfigNormalizesAI(t *testing.T) {
	withConfigFile(t, `{"ai": {"usagePollSeconds": 5, "statusPollSeconds": 999999, "trayProvider": "codex", "codexEnabled": false, "resetTimeFormat": "x"}}`)
	ai := snapshotConfig().AI
	if ai.UsagePollSeconds != 60 || ai.StatusPollSeconds != 3600 {
		t.Errorf("ポーリング間隔が矯正されていない: %d / %d", ai.UsagePollSeconds, ai.StatusPollSeconds)
	}
	if ai.TrayProvider != "claude" {
		t.Errorf("無効な Codex をトレイに出さない（D9）: TrayProvider = %q", ai.TrayProvider)
	}
	if ai.ResetTimeFormat != "datetime" {
		t.Errorf("ResetTimeFormat = %q, want datetime", ai.ResetTimeFormat)
	}
}

// aiHost 経由の設定変更は config.json に保存され、再読み込みで保たれる。
func TestAIHostMutateSettingsPersists(t *testing.T) {
	path := withConfigFile(t, "")
	h := aiHost{}
	h.MutateSettings(func(s *aiusage.Settings) {
		s.Enabled = true
		s.TrayProvider = "codex"
	})
	if got := h.Settings(); !got.Enabled || got.TrayProvider != "codex" {
		t.Fatalf("Settings() に反映されていない: %+v", got)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"ai"`) || !strings.Contains(string(data), `"enabled": true`) {
		t.Errorf("config.json に \"ai\" が保存されていない:\n%s", data)
	}
	loadConfig()
	if got := h.Settings(); !got.Enabled || got.TrayProvider != "codex" {
		t.Errorf("再読み込みで保たれていない: %+v", got)
	}
}

func TestCopyUTF16(t *testing.T) {
	decode := func(b []uint16) string {
		for i, c := range b {
			if c == 0 {
				b = b[:i]
				break
			}
		}
		return string(utf16.Decode(b))
	}

	dst := make([]uint16, 8)
	copyUTF16(dst, "abc")
	if got := decode(dst); got != "abc" {
		t.Errorf("通常 = %q", got)
	}
	// 収まらない分は切り詰め、終端の NUL を残す（7 文字 + NUL）。
	copyUTF16(dst, "0123456789")
	if got := decode(dst); got != "0123456" || dst[7] != 0 {
		t.Errorf("切り詰め = %q, 終端 = %d", got, dst[7])
	}
	// サロゲートペアの途中では切らない（上位サロゲートだけを残さない）。
	copyUTF16(dst, "123456😀")
	if got := decode(dst); got != "123456" {
		t.Errorf("サロゲート境界 = %q, want 123456", got)
	}
	// 途中の NUL 以降は捨てる。
	copyUTF16(dst, "ab\x00cd")
	if got := decode(dst); got != "ab" {
		t.Errorf("途中の NUL = %q", got)
	}
	// 小さすぎる・空の dst でも落ちない。
	copyUTF16(make([]uint16, 1), "abc")
	copyUTF16(nil, "abc")
	// 前の内容が残らない（残りはゼロで埋める）。
	copyUTF16(dst, "xxxxxxx")
	copyUTF16(dst, "y")
	if got := decode(dst); got != "y" {
		t.Errorf("前の内容が残っている = %q", got)
	}
}
