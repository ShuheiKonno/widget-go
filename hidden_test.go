package main

import (
	"strings"
	"testing"
)

func TestLoadConfigWithoutHiddenShowsAll(t *testing.T) {
	withConfigFile(t, configV180)
	if c := snapshotConfig(); c.Hidden != (HiddenConfig{}) || !c.ShowGPU {
		t.Errorf("hidden キーなしはすべて表示のはず: %+v ShowGPU=%v", c.Hidden, c.ShowGPU)
	}
}

func TestLoadConfigShowGPUFalseHidesGPUAndNPU(t *testing.T) {
	withConfigFile(t, `{"showGPU": false}`)
	c := snapshotConfig()
	if !c.Hidden.GPU || !c.Hidden.NPU || c.Hidden.CPU || c.Hidden.Memory || c.Hidden.Network {
		t.Errorf("旧 showGPU=false は GPU と NPU だけ非表示に引き継ぐ: %+v", c.Hidden)
	}
	if c.ShowGPU {
		t.Error("両方非表示なら ShowGPU は false")
	}
}

func TestSetHiddenSyncsShowGPU(t *testing.T) {
	withConfigFile(t, "")
	for _, name := range []string{"cpu", "memory", "network"} {
		if err := setHidden(name, "true"); err != nil {
			t.Fatalf("setHidden(%q): %v", name, err)
		}
	}
	if !snapshotConfig().ShowGPU {
		t.Error("CPU/メモリ/ネットワークの非表示で GPU 収集は止まらない")
	}
	_ = setHidden("gpu", "true")
	if !snapshotConfig().ShowGPU {
		t.Error("NPU が表示なら収集は続く")
	}
	_ = setHidden("npu", "true")
	if snapshotConfig().ShowGPU {
		t.Error("GPU と NPU の両方が非表示なら収集を止める")
	}
	_ = setHidden("npu", "false")
	if !snapshotConfig().ShowGPU {
		t.Error("NPU を表示に戻したら収集を再開する")
	}
	// 再読込しても保たれる
	loadConfig()
	c := snapshotConfig()
	if !c.Hidden.GPU || c.Hidden.NPU || !c.ShowGPU {
		t.Errorf("再読込で状態が変わった: %+v ShowGPU=%v", c.Hidden, c.ShowGPU)
	}
}

func TestSetHiddenRejectsInvalid(t *testing.T) {
	withConfigFile(t, "")
	if err := setHidden("ai", "true"); err == nil {
		t.Error("ai は hidden の対象外（ai.enabled で切り替える）")
	}
	if err := setHidden("cpu", "yes"); err == nil {
		t.Error("true/false 以外は拒否するはず")
	}
	if snapshotConfig().Hidden != (HiddenConfig{}) {
		t.Error("エラー時は設定を変更しない")
	}
}

func TestClampVertical(t *testing.T) {
	cases := []struct {
		name                  string
		top, h, wTop, wBottom int32
		wantTop, wantH        int32
	}{
		{"収まる", 500, 300, 0, 1000, 500, 300},
		{"上にはみ出す", -200, 500, 0, 1000, 0, 500},
		{"下にはみ出す", 800, 500, 0, 1000, 500, 500},
		{"作業領域より高い", 300, 1500, 0, 1000, 0, 1000},
	}
	for _, c := range cases {
		top, h := clampVertical(c.top, c.h, c.wTop, c.wBottom)
		if top != c.wantTop || h != c.wantH {
			t.Errorf("%s: got (%d,%d), want (%d,%d)", c.name, top, h, c.wantTop, c.wantH)
		}
	}
}

func TestNormalizeOrder(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want string
	}{
		{"nil は既定順", nil, "cpu,memory,disk,gpu,npu,network,ai"},
		{"並べ替えを保つ", []string{"ai", "cpu", "memory", "disk", "gpu", "npu", "network"}, "ai,cpu,memory,disk,gpu,npu,network"},
		{"欠けた区画は末尾に足す", []string{"network", "cpu"}, "network,cpu,memory,disk,gpu,npu,ai"},
		{"未知と重複は捨てる", []string{"x", "gpu", "gpu", "cpu"}, "gpu,cpu,memory,disk,npu,network,ai"},
		// v2.7 より前の config.json には disk が無い。末尾（AI の後ろ）ではなくメモリの直後に入れる。
		{"disk が無い保存済みの順はメモリの直後に足す", []string{"ai", "cpu", "memory", "gpu", "npu", "network"}, "ai,cpu,memory,disk,gpu,npu,network"},
		{"メモリが先頭側でなくても直後に足す", []string{"network", "memory", "cpu", "gpu", "npu", "ai"}, "network,memory,disk,cpu,gpu,npu,ai"},
	}
	for _, c := range cases {
		if got := strings.Join(normalizeOrder(c.in), ","); got != c.want {
			t.Errorf("%s: got %s, want %s", c.name, got, c.want)
		}
	}
}

func TestSetOrder(t *testing.T) {
	withConfigFile(t, "")
	if got := strings.Join(snapshotConfig().Order, ","); got != "cpu,memory,disk,gpu,npu,network,ai" {
		t.Fatalf("既定の順が違う: %s", got)
	}
	if err := setOrder("network,ai,cpu,memory,disk,gpu,npu"); err != nil {
		t.Fatal(err)
	}
	loadConfig()
	if got := strings.Join(snapshotConfig().Order, ","); got != "network,ai,cpu,memory,disk,gpu,npu" {
		t.Errorf("再読込で順が変わった: %s", got)
	}
	for _, bad := range []string{"", "cpu", "cpu,cpu,memory,disk,gpu,npu,network", "cpu,memory,disk,gpu,npu,network,x", "cpu,memory,gpu,npu,network,ai"} {
		if err := setOrder(bad); err == nil {
			t.Errorf("setOrder(%q) は拒否するはず", bad)
		}
	}
	if got := strings.Join(snapshotConfig().Order, ","); got != "network,ai,cpu,memory,disk,gpu,npu" {
		t.Errorf("エラー時に設定が変わった: %s", got)
	}
}

// Disk 区画は GPU/NPU の収集（ShowGPU）とは独立に、非表示を保存して再読込でも保つ。
func TestSetHiddenDisk(t *testing.T) {
	withConfigFile(t, "")
	if err := setHidden("disk", "true"); err != nil {
		t.Fatal(err)
	}
	c := snapshotConfig()
	if !c.Hidden.Disk || !c.ShowGPU {
		t.Errorf("disk だけ非表示のはず（GPU の収集は続く）: %+v ShowGPU=%v", c.Hidden, c.ShowGPU)
	}
	loadConfig()
	if !snapshotConfig().Hidden.Disk {
		t.Error("再読込で disk の非表示が失われた")
	}
	if err := setCollapsed("disk", "true"); err != nil {
		t.Fatal(err)
	}
	if !snapshotConfig().Collapsed.Disk {
		t.Error("disk の折りたたみが保存されない")
	}
}
