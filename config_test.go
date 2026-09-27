package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// withConfigFile は configPath / config を一時ファイルに差し替えて loadConfig する。
// content が空ならファイルを作らない。グローバル状態はテスト終了時に元へ戻す。
func withConfigFile(t *testing.T, content string) string {
	t.Helper()
	oldPath, oldConfig := configPath, snapshotConfig()
	t.Cleanup(func() {
		configMu.Lock()
		configPath, config = oldPath, oldConfig
		configMu.Unlock()
	})
	path := filepath.Join(t.TempDir(), "config.json")
	if content != "" {
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	configMu.Lock()
	configPath = path
	configMu.Unlock()
	loadConfig()
	return path
}

// v1.8.0 が書き出す形式（showGPU キーなし）
const configV180 = `{
  "window": {"x": 100, "y": 200, "width": 230, "height": 426, "saved": true},
  "topmost": true,
  "viewMode": 1,
  "transparent": true,
  "trayDisplay": "cpu",
  "theme": "light",
  "coreBase": 1,
  "netUnit": "bit",
  "ime": {"enabled": false, "cooldownSec": 3, "thresholdPx": 7, "dryRun": true}
}`

func TestLoadConfigV180DefaultsShowGPU(t *testing.T) {
	withConfigFile(t, configV180)
	c := snapshotConfig()
	if !c.ShowGPU {
		t.Error("ShowGPU = false, want true (v1.8.0 形式はキーが無いので既定 ON)")
	}
	// 既存キーは保持される
	if c.Theme != "light" || c.CoreBase != 1 || c.NetUnit != "bit" || c.TrayDisplay != "cpu" ||
		!c.Topmost || !c.Transparent || c.ViewMode != 1 {
		t.Errorf("既存キーが保持されていない: %+v", c)
	}
	if !c.Window.Saved || c.Window.X != 100 || c.Window.Y != 200 || c.Window.Height != 426 {
		t.Errorf("window が保持されていない: %+v", c.Window)
	}
	if c.IME.Enabled || c.IME.CooldownSec != 3 || c.IME.ThresholdPx != 7 || !c.IME.DryRun {
		t.Errorf("ime が保持されていない: %+v", c.IME)
	}
}

func TestLoadConfigShowGPUFalse(t *testing.T) {
	withConfigFile(t, `{"theme": "dark", "showGPU": false}`)
	if c := snapshotConfig(); c.ShowGPU {
		t.Error("ShowGPU = true, want false (明示的な false を尊重する)")
	}
}

func TestLoadConfigMissingFile(t *testing.T) {
	withConfigFile(t, "")
	c := snapshotConfig()
	if !c.ShowGPU || c.Theme != "system" || c.TrayDisplay != "icon" || c.NetUnit != "byte" {
		t.Errorf("既定値になっていない: %+v", c)
	}
}

func TestLoadConfigBrokenJSONKeepsDefaults(t *testing.T) {
	withConfigFile(t, `{"showGPU": false,`)
	if c := snapshotConfig(); !c.ShowGPU {
		t.Error("壊れた JSON で既定値が失われた")
	}
}

func TestWriteConfigIncludesShowGPU(t *testing.T) {
	path := withConfigFile(t, configV180)
	mutateConfig(func(c *Config) { c.ShowGPU = false })
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	if v, ok := m["showGPU"]; !ok || v != false {
		t.Errorf("showGPU = %v (ok=%v), want false", v, ok)
	}
	// 書き戻した内容を読み直しても false のまま
	loadConfig()
	if snapshotConfig().ShowGPU {
		t.Error("再読込で ShowGPU が true に戻った")
	}
}
