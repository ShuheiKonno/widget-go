package sysinfo

import (
	"strings"
	"sync"
	"testing"
)

func TestGPUParseInstance(t *testing.T) {
	for _, tc := range []struct {
		name           string
		ok             bool
		low, phys, eng uint32
		kind           string
	}{
		{`pid_11052_luid_0x00000000_0x000103E9_phys_0_eng_5_engtype_Compute`, true, 0x103e9, 0, 5, "Compute"},
		{`pid_13108_luid_0x00000000_0x0001082C_phys_0_eng_0_engtype_Compute`, true, 0x1082c, 0, 0, "Compute"},
		{`pid_2_luid_0x00000000_0x000103E9_phys_1_eng_3_engtype_Compute_0`, true, 0x103e9, 1, 3, "Compute_0"},
		{`pid_2_luid_0x00000000_0x000103E9_phys_1_eng_3_engtype_Cuda`, true, 0x103e9, 1, 3, "Cuda"},
		{`pid_2_luid_0x00000000_0x000103E9_phys_1_eng_3_engtype_3D`, true, 0x103e9, 1, 3, "3D"},
		{`pid_2_luid_0x00000000_0x000103E9_phys_1_eng_3_engtype_`, true, 0x103e9, 1, 3, ""},
		{`pid_x_luid_0x00000000_0x000103E9_phys_0_eng_5_engtype_Compute`, false, 0, 0, 0, ""},
		{`pid_1_luid_0x00000000_0x000103E9_phys_4294967296_eng_5_engtype_Compute`, false, 0, 0, 0, ""},
		{`luid_0x00000000_0x000103E9_phys_0`, false, 0, 0, 0, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v, ok := gpuParseInstance(tc.name)
			if ok != tc.ok {
				t.Fatalf("ok=%v", ok)
			}
			if ok && (v.LUID.Low != tc.low || v.Phys != tc.phys || v.Eng != tc.eng || v.Type != tc.kind) {
				t.Fatalf("%+v", v)
			}
		})
	}
}

func TestGPUComputeType(t *testing.T) {
	for _, tc := range []struct {
		s    string
		want bool
	}{{"Compute", true}, {"Compute_0", true}, {"Cuda", true}, {"CUDA_1", true}, {"3D", false}, {"Neural", false}, {"", false}} {
		if got := gpuComputeType(tc.s); got != tc.want {
			t.Errorf("%q: %v", tc.s, got)
		}
	}
}

func TestGPUAggregate(t *testing.T) {
	base := `pid_1_luid_0x00000000_0x000103E9_phys_0_eng_0_engtype_`
	samples := []gpuSample{
		{base + "Compute", 60},
		{strings.Replace(base, "pid_1", "pid_2", 1) + "Compute", 55},
		{strings.Replace(base, "eng_0", "eng_1", 1) + "Neural", 130},
		{strings.Replace(strings.Replace(base, "phys_0", "phys_1", 1), "eng_0", "eng_1", 1) + "Cuda", 42},
		{"malformed", 100},
	}
	u, bad := gpuAggregate(samples)
	if bad != 1 {
		t.Fatalf("malformed=%d", bad)
	}
	v := u[gpuLUID{Low: 0x103e9}]
	if v.Total == nil || *v.Total != 130 || v.Compute == nil || *v.Compute != 115 {
		t.Fatalf("usage=%+v", v)
	}
	u, bad = gpuAggregate([]gpuSample{{base + "3D", 3}})
	if bad != 0 || u[gpuLUID{Low: 0x103e9}].Compute != nil {
		t.Fatal("non-compute engine")
	}
}

func TestGPUChoose(t *testing.T) {
	arc := gpuAdapter{LUID: gpuLUID{Low: 0x103e9}, Flags: 1, Name: "Arc", Segments: gpuSegments{DedicatedVideo: 128 << 20}}
	npu := gpuAdapter{LUID: gpuLUID{Low: 0x1082c}, Flags: 0x2901, Name: "AI Boost"}
	basic := gpuAdapter{LUID: gpuLUID{Low: 0x107eb}, Flags: 0x105, Name: "Basic Render"}
	discrete := gpuAdapter{LUID: gpuLUID{Low: 4}, Flags: 1, Name: "Discrete", Segments: gpuSegments{DedicatedVideo: 1 << 30}}
	indirect := gpuAdapter{Flags: 1<<6 | 1, Name: "Indirect"}
	computeOnly := gpuAdapter{Flags: 1<<11 | 1, Name: "Unknown compute"}
	for _, tc := range []struct {
		list             []gpuAdapter
		npu              map[gpuLUID]bool
		gpuName, npuName string
	}{
		{[]gpuAdapter{arc, npu, basic, indirect, computeOnly}, map[gpuLUID]bool{npu.LUID: true}, "Arc", "AI Boost"},
		{[]gpuAdapter{arc, discrete}, nil, "Discrete", ""},
		{[]gpuAdapter{arc, npu}, nil, "Arc", ""},
		{[]gpuAdapter{basic, indirect, computeOnly}, nil, "", ""},
	} {
		g, n := gpuChoose(tc.list, tc.npu)
		if (g != nil && g.Name != tc.gpuName) || (g == nil && tc.gpuName != "") || (n != nil && n.Name != tc.npuName) || (n == nil && tc.npuName != "") {
			t.Errorf("got gpu=%v npu=%v", g, n)
		}
	}
}

func TestGPUAPISmoke(t *testing.T) {
	InitGPU()
	for i := 0; i < 2; i++ {
		g, n := GetGPUInfo()
		if g != nil {
			if g.Name == "" || (g.TotalPct != nil && *g.TotalPct < 0) || (g.ComputePct != nil && *g.ComputePct < 0) {
				t.Fatalf("bad GPU: %+v", g)
			}
		}
		if n != nil && (n.Name == "" || (n.UsagePct != nil && *n.UsagePct < 0)) {
			t.Fatalf("bad NPU: %+v", n)
		}
	}
}

func TestGPUConcurrentSnapshots(t *testing.T) {
	InitGPU()
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				g, n := GetGPUInfo()
				if g != nil {
					_ = g.Name
					_ = g.TotalPct
				}
				if n != nil {
					_ = n.Name
					_ = n.UsagePct
				}
			}
		}()
	}
	wg.Wait()
}
