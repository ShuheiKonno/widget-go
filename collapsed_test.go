package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// v1.9.3 までの config.json には "collapsed" が無い。区画は展開、AI だけ折りたたみ（D8）のまま。
func TestLoadConfigWithoutCollapsedUsesDefaults(t *testing.T) {
	withConfigFile(t, configV180)
	c := snapshotConfig().Collapsed
	want := CollapsedConfig{AI: true}
	if c != want {
		t.Errorf("\"collapsed\" が無い config の既定 = %+v, want %+v", c, want)
	}
}

// 一部の区画だけ書かれていても、欠けたキー（AI の既定の折りたたみ）は残る。
func TestLoadConfigPartialCollapsedKeepsDefaults(t *testing.T) {
	withConfigFile(t, `{"collapsed": {"cpu": true, "network": true}}`)
	c := snapshotConfig().Collapsed
	if !c.CPU || !c.Network || c.Memory || c.GPU || c.NPU {
		t.Errorf("指定した区画だけが折りたたまれるはず: %+v", c)
	}
	if !c.AI {
		t.Error("指定の無い AI は既定の折りたたみのままのはず")
	}
}

// 利用者が AI を展開した状態は、保存されて再読み込みで保たれる。
func TestLoadConfigExplicitAIExpandedIsKept(t *testing.T) {
	withConfigFile(t, `{"collapsed": {"ai": false}}`)
	if snapshotConfig().Collapsed.AI {
		t.Error("\"ai\": false は展開として保たれるはず")
	}
}

func TestSetCollapsedPersistsAllSections(t *testing.T) {
	path := withConfigFile(t, "")
	for _, name := range []string{"cpu", "memory", "disk", "gpu", "npu", "network", "ai"} {
		if err := setCollapsed(name, "true"); err != nil {
			t.Errorf("setCollapsed(%q, true): %v", name, err)
		}
	}
	if c := snapshotConfig().Collapsed; c != (CollapsedConfig{CPU: true, Memory: true, Disk: true, GPU: true, NPU: true, Network: true, AI: true}) {
		t.Errorf("全区画が折りたたまれるはず: %+v", c)
	}
	if err := setCollapsed("cpu", "false"); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var saved struct {
		Collapsed map[string]bool `json:"collapsed"`
	}
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	if len(saved.Collapsed) != 7 || saved.Collapsed["cpu"] || !saved.Collapsed["memory"] || !saved.Collapsed["ai"] {
		t.Errorf("config.json の collapsed が正しくない: %v\n%s", saved.Collapsed, data)
	}
}

// 許可リスト外の区画名と、true/false 以外の値は、設定を変えずにエラーにする。
func TestSetCollapsedRejectsInvalid(t *testing.T) {
	path := withConfigFile(t, "")
	before := snapshotConfig().Collapsed
	for _, c := range []struct{ name, val string }{
		{"wifi", "true"},       // 区画名は network
		{"", "true"},           // 空
		{"cpu ", "true"},       // 余分な空白
		{"CPU", "true"},        // 大文字小文字は区別する
		{"cpu", "yes"},         // 値は true / false のみ
		{"cpu", ""},            // 空
		{"cpu", "TRUE"},        // 大文字小文字は区別する
		{"../ai", "true"},      // パスのような値
		{"ai.enabled", "true"}, // 他の設定のキーは触れない
	} {
		if err := setCollapsed(c.name, c.val); err == nil {
			t.Errorf("setCollapsed(%q, %q) はエラーのはず", c.name, c.val)
		}
	}
	if after := snapshotConfig().Collapsed; after != before {
		t.Errorf("不正な入力で設定が変わった: %+v → %+v", before, after)
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("不正な入力で config.json が書き出された")
	}
}

// /api/settings の応答に載せる形（フロントが読むキー）が、7 区画そろっていること。
func TestCollapsedJSONKeys(t *testing.T) {
	data, err := json.Marshal(CollapsedConfig{})
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"cpu", "memory", "disk", "gpu", "npu", "network", "ai"} {
		if !strings.Contains(string(data), `"`+k+`":`) {
			t.Errorf("JSON に %q が無い: %s", k, data)
		}
	}
}
