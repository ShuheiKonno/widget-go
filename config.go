package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// Config はプロセスをまたいで永続化される設定。
// WebView2 の localStorage は os.Exit(0) で flush されずに失われる可能性があるため、
// Go 側で JSON ファイルに保存する。
type Config struct {
	Window struct {
		X      int32 `json:"x"`
		Y      int32 `json:"y"`
		Width  int32 `json:"width"`
		Height int32 `json:"height"`
		Saved  bool  `json:"saved"`
		// NormalWidth はコンパクト表示に切り替える直前のフル表示時の幅。
		// コンパクト中は Width が専用幅（compactWindowWidth）で上書きされるため、
		// 戻すときの幅をここに退避しておく。
		NormalWidth int32 `json:"normalWidth"`
	} `json:"window"`
	Topmost     bool      `json:"topmost"`
	ViewMode    int       `json:"viewMode"`
	Transparent bool      `json:"transparent"`
	TrayDisplay string    `json:"trayDisplay"` // "icon" | "cpu"
	Theme       string    `json:"theme"`       // "dark" | "light" | "system"
	CoreBase    int       `json:"coreBase"`    // 0 | 1
	NetUnit     string    `json:"netUnit"`     // "byte" | "bit"
	ShowGPU     bool      `json:"showGPU"`     // GPU/NPU の表示と収集
	IME         IMEConfig `json:"ime"`
}

// IMEConfig は IME Composition Window 検知・自動復旧機能の設定。
type IMEConfig struct {
	Enabled     bool  `json:"enabled"`
	CooldownSec int   `json:"cooldownSec"`
	ThresholdPx int32 `json:"thresholdPx"`
	DryRun      bool  `json:"dryRun"`
}

var (
	configMu   sync.Mutex
	configPath string
	config     Config
)

func defaultConfig() Config {
	var c Config
	c.TrayDisplay = "icon"
	c.Theme = "system"
	c.CoreBase = 0
	c.NetUnit = "byte"
	// v1.8.0 以前の config.json にはキーが無い。既定値を先に入れておくと
	// loadConfig の Unmarshal で上書きされず ON のまま残る。
	c.ShowGPU = true
	c.IME = IMEConfig{
		Enabled:     true,
		CooldownSec: 2,
		ThresholdPx: 5,
		DryRun:      false,
	}
	return c
}

func loadConfig() {
	configMu.Lock()
	defer configMu.Unlock()
	config = defaultConfig()
	if configPath == "" {
		return
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		return
	}
	// デフォルト値を上書きする形で JSON を読み込む
	tmp := config
	if err := json.Unmarshal(data, &tmp); err != nil {
		return
	}
	config = tmp
}

// writeConfigLocked は config を JSON でアトミック（tmp+rename）に書き出す。
// configMu を保持した状態で呼ぶこと。マーシャルと書き込みをロック内で一体化することで、
// 並行 mutateConfig 呼び出しによる lost update（古いスナップショットの後勝ち）を防ぐ。
func writeConfigLocked() {
	if configPath == "" {
		return
	}
	data, err := json.MarshalIndent(&config, "", "  ")
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(configPath), 0755)
	tmp := configPath + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return
	}
	_ = os.Rename(tmp, configPath)
}

// mutateConfig は config をロックしたうえで変更関数を適用し、ロックを保持したまま保存する。
func mutateConfig(f func(c *Config)) {
	configMu.Lock()
	defer configMu.Unlock()
	f(&config)
	writeConfigLocked()
}

func snapshotConfig() Config {
	configMu.Lock()
	defer configMu.Unlock()
	return config
}
