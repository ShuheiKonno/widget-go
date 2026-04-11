package sysinfo

import (
	"fmt"

	"github.com/shirou/gopsutil/v3/mem"
)

type MemoryInfo struct {
	Total       uint64
	Used        uint64
	Free        uint64
	UsedPercent float64
}

func GetMemoryInfo() (*MemoryInfo, error) {
	v, err := mem.VirtualMemory()
	if err != nil {
		return nil, err
	}

	return &MemoryInfo{
		Total:       v.Total,
		Used:        v.Used,
		Free:        v.Free,
		UsedPercent: v.UsedPercent,
	}, nil
}

// FormatBytes はバイト数をGB形式の文字列に変換
func FormatBytes(bytes uint64) string {
	gb := float64(bytes) / (1024 * 1024 * 1024)
	return fmt.Sprintf("%.1f GB", gb)
}
