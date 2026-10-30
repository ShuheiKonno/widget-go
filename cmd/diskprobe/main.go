// diskprobe exercises the sysinfo disk collector on Windows.
package main

import (
	"flag"
	"fmt"
	"time"

	"widget-go/sysinfo"
)

func val(v *float64, unit string) string {
	if v == nil {
		return "---"
	}
	return fmt.Sprintf("%.2f%s", *v, unit)
}

func main() {
	interval := flag.Duration("interval", time.Second, "sample interval")
	duration := flag.Duration("duration", 10*time.Second, "run duration (0: until interrupted)")
	flag.Parse()
	for _, d := range sysinfo.GetDrives() {
		fmt.Printf("%s used=%d total=%d (%.1f%%)\n", d.Letter, d.Used, d.Total, d.UsedPercent)
	}
	sysinfo.InitDiskIO()
	start := time.Now()
	for {
		time.Sleep(*interval)
		io := sysinfo.GetDiskIO()
		if io == nil {
			fmt.Println("IO: none")
		} else {
			fmt.Printf("IO active=%s read=%s write=%s\n", val(io.ActivePct, "%"), val(io.ReadBps, " B/s"), val(io.WriteBps, " B/s"))
		}
		if *duration > 0 && time.Since(start) >= *duration {
			break
		}
	}
}
