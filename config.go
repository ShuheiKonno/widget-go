package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"widget-go/aiusage"
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
		// モードごとの最後の位置。切替時に出ていく側を保存し、入る側に記憶が
		// あればそこへ移動する。
		//
		// 左上ではなく「右下」で覚える。幅の切替は右端固定、高さの自動調整は
		// 下端固定で行うため、右下はそれらのリサイズを通して不変。左上基準だと
		// 位置を戻したあとの高さ調整で上端がずれ、切替のたびにドリフトする。
		//
		// 原点 (0,0) も有効な位置なので、記憶の有無は座標ではなくフラグで判断する。
		NormalRight    int32 `json:"normalRight"`
		NormalBottom   int32 `json:"normalBottom"`
		NormalPosSaved bool  `json:"normalPosSaved"`

		CompactRight    int32 `json:"compactRight"`
		CompactBottom   int32 `json:"compactBottom"`
		CompactPosSaved bool  `json:"compactPosSaved"`
	} `json:"window"`
	Topmost     bool      `json:"topmost"`
	ViewMode    int       `json:"viewMode"`
	Transparent bool      `json:"transparent"`
	TrayDisplay string    `json:"trayDisplay"` // "icon" | "cpu" | "usage"
	Theme       string    `json:"theme"`       // "dark" | "light" | "system"
	Skin        string    `json:"skin"`        // 空文字または "default" は現行デザイン
	CoreBase    int       `json:"coreBase"`    // 0 | 1
	NetUnit     string    `json:"netUnit"`     // "byte" | "bit"
	DiskIOView  string    `json:"diskIoView"`  // Disk 区画の裏面（I/O）の見せ方。"number" | "graph"
	Zoom        int       `json:"zoom"`        // 拡大率（%）。zoomSteps のどれか。既定 100
	ShowGPU     bool      `json:"showGPU"`     // GPU/NPU の表示と収集
	IME         IMEConfig `json:"ime"`
	// PowerModeEnabled は電源モード（設定アプリの「電源モード」）の切替機能の ON/OFF。既定は OFF。
	// OFF の間は電源モードの読み取りも書き込みも行わず、Windows の設定・制御のままにする。
	// OS の電源状態を書き換える機能なので、利用者が設定で ON にしたときだけ働かせる。
	// OS の状態そのものは保存しない（起動時に再適用して設定アプリでの変更を戻さないため）。
	PowerModeEnabled bool `json:"powerModeEnabled"`
	// AI は Claude / Codex の使用量モニター（旧 ClaudeMonitor）の設定。既定は OFF。
	AI aiusage.Settings `json:"ai"`
	// Collapsed は各区画の折りたたみ状態（D10）。
	Collapsed CollapsedConfig `json:"collapsed"`
	// Hidden は各区画の非表示設定。true が非表示。既定はすべて表示。
	// AI は既存の ai.enabled（取得の ON/OFF を兼ねる）で切り替えるのでここには含めない。
	Hidden HiddenConfig `json:"hidden"`
	// Order は区画の表示順（上から）。defaultOrder の並べ替えで、常に全区画を 1 回ずつ含む。
	Order []string `json:"order"`
}

// defaultOrder は区画の既定の表示順。区画名は CollapsedConfig / HiddenConfig と同じ。
var defaultOrder = []string{"cpu", "memory", "disk", "gpu", "npu", "network", "ai"}

// normalizeOrder は保存された表示順を矯正する。未知・重複は捨て、欠けた区画は既定の順で末尾に足す。
// 手編集や、区画が増えた将来のバージョンから戻ったときでも全区画が必ず含まれる。
func normalizeOrder(in []string) []string {
	known := map[string]bool{}
	for _, n := range defaultOrder {
		known[n] = true
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(defaultOrder))
	for _, n := range in {
		if known[n] && !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	for _, n := range defaultOrder {
		if seen[n] {
			continue
		}
		// disk は v2.7 で足した区画。保存済みの順に無い場合は、末尾ではなくメモリの直後に入れる
		// （容量系の区画をメモリの隣に置くのが自然で、AI の後ろに回ると見つけにくい）。
		if n == "disk" {
			if i := slices.Index(out, "memory"); i >= 0 {
				out = slices.Insert(out, i+1, n)
				continue
			}
		}
		out = append(out, n)
	}
	return out
}

// setOrder は区画の表示順を保存する。val はカンマ区切りで、全区画をちょうど 1 回ずつ含むこと。
// それ以外はエラー（設定は変更しない）。
func setOrder(val string) error {
	parts := strings.Split(val, ",")
	if len(parts) != len(defaultOrder) {
		return fmt.Errorf("invalid order %q", val)
	}
	for i, n := range normalizeOrder(parts) {
		if n != parts[i] {
			return fmt.Errorf("invalid order %q", val)
		}
	}
	mutateConfig(func(c *Config) { c.Order = parts })
	return nil
}

// HiddenConfig は区画ごとの非表示状態。true が非表示。
type HiddenConfig struct {
	CPU     bool `json:"cpu"`
	Memory  bool `json:"memory"`
	Disk    bool `json:"disk"`
	GPU     bool `json:"gpu"`
	NPU     bool `json:"npu"`
	Network bool `json:"network"`
}

// field は区画名に対応するフィールドを返す。未知の名前は nil。
func (h *HiddenConfig) field(name string) *bool {
	switch name {
	case "cpu":
		return &h.CPU
	case "memory":
		return &h.Memory
	case "disk":
		return &h.Disk
	case "gpu":
		return &h.GPU
	case "npu":
		return &h.NPU
	case "network":
		return &h.Network
	}
	return nil
}

// syncShowGPU は GPU/NPU の収集可否（ShowGPU）を非表示設定に合わせる。
// GPU と NPU の両方が非表示のときだけ収集を止める。
func (c *Config) syncShowGPU() { c.ShowGPU = !c.Hidden.GPU || !c.Hidden.NPU }

// setHidden は区画の非表示状態を保存する。name は許可リスト（cpu / memory / disk / gpu / npu /
// network）、val は "true" か "false" のみ。それ以外はエラー（設定は変更しない）。
func setHidden(name, val string) error {
	if val != "true" && val != "false" {
		return fmt.Errorf("invalid hide value %q", val)
	}
	if (&HiddenConfig{}).field(name) == nil {
		return fmt.Errorf("unknown hide section %q", name)
	}
	mutateConfig(func(c *Config) {
		*c.Hidden.field(name) = val == "true"
		c.syncShowGPU()
	})
	return nil
}

// CollapsedConfig は区画ごとの折りたたみ状態。true が折りたたみ。
// AI 以外の既存の区画にも及ぶので、Config.AI の中ではなくトップレベルに置く。
type CollapsedConfig struct {
	CPU     bool `json:"cpu"`
	Memory  bool `json:"memory"`
	Disk    bool `json:"disk"`
	GPU     bool `json:"gpu"`
	NPU     bool `json:"npu"`
	Network bool `json:"network"`
	AI      bool `json:"ai"`
}

// field は区画名に対応するフィールドを返す。未知の名前は nil。
func (c *CollapsedConfig) field(name string) *bool {
	switch name {
	case "cpu":
		return &c.CPU
	case "memory":
		return &c.Memory
	case "disk":
		return &c.Disk
	case "gpu":
		return &c.GPU
	case "npu":
		return &c.NPU
	case "network":
		return &c.Network
	case "ai":
		return &c.AI
	}
	return nil
}

// setCollapsed は区画の折りたたみ状態を保存する。name は許可リスト（cpu / memory / disk / gpu / npu /
// network / ai）、val は "true" か "false" のみ。それ以外はエラー（設定は変更しない）。
func setCollapsed(name, val string) error {
	if val != "true" && val != "false" {
		return fmt.Errorf("invalid collapse value %q", val)
	}
	if (&CollapsedConfig{}).field(name) == nil {
		return fmt.Errorf("unknown collapse section %q", name)
	}
	mutateConfig(func(c *Config) { *c.Collapsed.field(name) = val == "true" })
	return nil
}

// setPowerModeEnabled は電源モードの切替機能の ON/OFF を保存する。val は "true" か "false" のみ。
// それ以外はエラー（設定は変更しない）。
func setPowerModeEnabled(val string) error {
	if val != "true" && val != "false" {
		return fmt.Errorf("invalid powerMode value %q", val)
	}
	mutateConfig(func(c *Config) { c.PowerModeEnabled = val == "true" })
	return nil
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

// appConfigDirectory は設定と利用者スキンに共通の保存先。
func appConfigDirectory() string {
	localAppData := os.Getenv("LOCALAPPDATA")
	if localAppData == "" {
		localAppData = os.Getenv("APPDATA")
	}
	return filepath.Join(localAppData, "widget-go")
}

func defaultConfig() Config {
	var c Config
	c.TrayDisplay = "icon"
	c.Theme = "system"
	c.CoreBase = 0
	c.NetUnit = "byte"
	c.DiskIOView = "number" // キーが無い旧バージョンの config.json でも数値のまま残る
	c.Zoom = defaultZoomPercent
	// v1.8.0 以前の config.json にはキーが無い。既定値を先に入れておくと
	// loadConfig の Unmarshal で上書きされず ON のまま残る。
	c.ShowGPU = true
	// 電源モードの切替機能は既定 OFF（OS の電源状態を書き換える機能なので、利用者が ON にするまで
	// 何もしない）。キーが無い旧バージョンの config.json でも OFF のまま残る（false はゼロ値）。
	c.PowerModeEnabled = false
	// v1.9.3 以前の config.json には "ai" が無い。既定値を先に入れておくと、
	// loadConfig の Unmarshal で欠けたキーが上書きされず既定のまま残る。
	c.AI = aiusage.DefaultSettings()
	// 区画は既定で展開。AI だけは既定で折りたたみ（D8）。旧 config にキーが無くても残る。
	c.Collapsed.AI = true
	c.Order = append([]string(nil), defaultOrder...)
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
	// 手編集や旧バージョンで範囲外・欠損になった値を安全な値へ矯正する。
	tmp.AI = aiusage.NormalizeSettings(tmp.AI)
	// v2.0.1 以前は「GPU/NPU を表示」(showGPU) 1 つだけだった。OFF だった設定は両区画の非表示に引き継ぐ。
	if !tmp.ShowGPU {
		tmp.Hidden.GPU, tmp.Hidden.NPU = true, true
	}
	tmp.syncShowGPU()
	tmp.Order = normalizeOrder(tmp.Order)
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
