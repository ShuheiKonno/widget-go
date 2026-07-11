package sysinfo

import (
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/net"
)

type NetworkInfo struct {
	RxBytesPerSec uint64
	TxBytesPerSec uint64
}

var (
	netMu          sync.Mutex
	prevBytesRecv  uint64
	prevBytesSent  uint64
	prevTimestamp  time.Time
	netInitialized bool
)

// GetNetworkInfo は全 NIC 合計の送受信バイト数の差分からスループットを算出する。
// 初回呼び出しはカウンタ初期化のみで速度 0 を返す。
func GetNetworkInfo() (*NetworkInfo, error) {
	counters, err := net.IOCounters(false)
	if err != nil || len(counters) == 0 {
		return nil, err
	}
	totalRecv := counters[0].BytesRecv
	totalSent := counters[0].BytesSent
	now := time.Now()

	netMu.Lock()
	defer netMu.Unlock()

	if !netInitialized {
		prevBytesRecv = totalRecv
		prevBytesSent = totalSent
		prevTimestamp = now
		netInitialized = true
		return &NetworkInfo{}, nil
	}

	dt := now.Sub(prevTimestamp).Seconds()
	var rx, tx uint64
	if dt > 0 {
		if totalRecv >= prevBytesRecv {
			rx = uint64(float64(totalRecv-prevBytesRecv) / dt)
		}
		if totalSent >= prevBytesSent {
			tx = uint64(float64(totalSent-prevBytesSent) / dt)
		}
	}
	prevBytesRecv = totalRecv
	prevBytesSent = totalSent
	prevTimestamp = now

	return &NetworkInfo{
		RxBytesPerSec: rx,
		TxBytesPerSec: tx,
	}, nil
}
