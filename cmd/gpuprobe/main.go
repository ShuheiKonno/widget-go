// gpuprobe exercises the public sysinfo GPU/NPU collector on Windows.
package main

import (
	"flag"
	"fmt"
	"time"

	"widget-go/sysinfo"
)

func pct(v *float64) string {
	if v == nil {
		return "---"
	}
	return fmt.Sprintf("%.3f%%", *v)
}

func main() {
	interval := flag.Duration("interval", time.Second, "sample interval")
	duration := flag.Duration("duration", 0, "run duration (0: until interrupted)")
	flag.Parse()
	sysinfo.InitGPU()
	start := time.Now()
	for {
		g, n := sysinfo.GetGPUInfo()
		if g == nil {
			fmt.Println("GPU: none")
		} else {
			fmt.Printf("GPU=%s total=%s compute=%s dedicated=%d/%d shared=%d/%d\n", g.Name, pct(g.TotalPct), pct(g.ComputePct), g.DedicatedUsed, g.DedicatedTotal, g.SharedUsed, g.SharedTotal)
		}
		if n == nil {
			fmt.Println("NPU: none")
		} else {
			fmt.Printf("NPU=%s usage=%s mem=%d\n", n.Name, pct(n.UsagePct), n.MemUsed)
		}
		if *duration > 0 && time.Since(start) >= *duration {
			break
		}
		time.Sleep(*interval)
	}
}
