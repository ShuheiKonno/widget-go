package sysinfo

import (
	"github.com/shirou/gopsutil/v3/cpu"
)

type CPUInfo struct {
	Overall float64
	Cores   []float64
}

// PrimeCPU は非ブロッキングな cpu.Percent(0, ...) 計算のために
// 内部カウンタを初期化する。アプリ起動時に一度だけ呼ぶ。
func PrimeCPU() {
	_, _ = cpu.Percent(0, true)
}

// GetCPUInfo は前回呼び出しからの差分で CPU 使用率を算出する
// （ブロックしない）。初回呼び出しの値は意味を持たないので、
// 起動時に PrimeCPU を呼んでおくこと。
func GetCPUInfo() (*CPUInfo, error) {
	cores, err := cpu.Percent(0, true)
	if err != nil {
		return nil, err
	}

	// Overall は全コアの平均として算出（追加の計測呼び出しを避ける）
	overallPercent := 0.0
	if len(cores) > 0 {
		sum := 0.0
		for _, c := range cores {
			sum += c
		}
		overallPercent = sum / float64(len(cores))
	}

	return &CPUInfo{
		Overall: overallPercent,
		Cores:   cores,
	}, nil
}
