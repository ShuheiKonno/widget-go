package ping

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"
)

type pingSample struct {
	success bool
	rtt     time.Duration
}

type targetBuffer struct {
	samples    []pingSample
	targetIP   string
	windowSize int
}

func newTargetBuffer(windowSize int) *targetBuffer {
	if windowSize <= 0 {
		windowSize = DefaultWindowSize
	}
	return &targetBuffer{
		samples:    make([]pingSample, 0, windowSize),
		windowSize: windowSize,
	}
}

func (b *targetBuffer) record(ip string, rtt time.Duration, success bool) {
	if ip != "" {
		b.targetIP = ip
	}
	if len(b.samples) >= b.windowSize {
		b.samples = b.samples[1:]
	}
	b.samples = append(b.samples, pingSample{success: success, rtt: rtt})
}

func (b *targetBuffer) stats(host string, lossThresh, rttThresh int) TargetInfo {
	info := TargetInfo{
		Host:        host,
		TargetIP:    b.targetIP,
		Status:      "idle",
		History:     make([]int, len(b.samples)),
		RTTMs:       -1,
		AvgRTTMs:    -1,
		MinRTTMs:    -1,
		MaxRTTMs:    -1,
		LossPercent: 0,
	}

	total := len(b.samples)
	if total == 0 {
		return info
	}
	info.SentCount = total

	var (
		lossCount = 0
		sumRTT    time.Duration
		succCount = 0
		minRTT    = time.Duration(math.MaxInt64)
		maxRTT    = time.Duration(0)
		latestRTT = time.Duration(-1)
	)

	for i, s := range b.samples {
		if !s.success {
			lossCount++
			info.History[i] = -1
		} else {
			ms := int(s.rtt.Milliseconds())
			if ms < 0 {
				ms = 0
			}
			info.History[i] = ms
			succCount++
			sumRTT += s.rtt
			latestRTT = s.rtt
			if s.rtt < minRTT {
				minRTT = s.rtt
			}
			if s.rtt > maxRTT {
				maxRTT = s.rtt
			}
		}
	}

	info.LossCount = lossCount
	info.LossPercent = math.Round((float64(lossCount)/float64(total))*1000) / 10 // 小数点第1位まで

	if succCount > 0 {
		info.RTTMs = int(latestRTT.Milliseconds())
		avg := sumRTT / time.Duration(succCount)
		info.AvgRTTMs = int(avg.Milliseconds())
		info.MinRTTMs = int(minRTT.Milliseconds())
		info.MaxRTTMs = int(maxRTT.Milliseconds())
	}

	// 閾値判定
	// ロスは、窓の中で MinLossCount 回以上落ちていて、率が閾値以上のとき。1 回の取りこぼしでは警告しない。
	if lossCount >= MinLossCount && info.LossPercent >= float64(lossThresh) {
		info.IsWarnLoss = true
	}
	// RTT は、直近 RTTConsecutive 回が連続して閾値以上のとき。一瞬のスパイクでは警告しない。
	run := 0
	for i := len(b.samples) - 1; i >= 0; i-- {
		smp := b.samples[i]
		if !smp.success || int(smp.rtt.Milliseconds()) < rttThresh {
			break
		}
		run++
	}
	if run >= RTTConsecutive {
		info.IsWarnRTT = true
	}

	switch {
	case lossCount == total && total >= MinLossCount:
		info.Status = "error"
	case succCount == 0:
		// まだ 1 回の失敗だけのとき。判断を保留する
		info.Status = "idle"
	case info.IsWarnLoss || info.IsWarnRTT:
		info.Status = "warn"
	default:
		info.Status = "ok"
	}

	return info
}

// Monitor は複数ホストの Ping 監視を管理する。
type Monitor struct {
	mu           sync.RWMutex
	pinger       Pinger
	cfg          Config
	buffers      map[string]*targetBuffer
	lastNotified map[string]time.Time
	okSince      map[string]time.Time // 警告が出ていない状態になった時刻（復帰判定用）
	cachedInfo   Info
	cooldown     time.Duration
}

// NewMonitor は新しい Monitor インスタンスを作成する。
func NewMonitor(pinger Pinger, cfg Config) *Monitor {
	return NewMonitorWithCooldown(pinger, cfg, DefaultCooldown)
}

// NewMonitorWithCooldown はクールダウン時間を指定して Monitor を作成する（テスト用）。
func NewMonitorWithCooldown(pinger Pinger, cfg Config, cooldown time.Duration) *Monitor {
	m := &Monitor{
		pinger:       pinger,
		cfg:          NormalizeConfig(cfg),
		buffers:      make(map[string]*targetBuffer),
		lastNotified: make(map[string]time.Time),
		okSince:      make(map[string]time.Time),
		cooldown:     cooldown,
	}
	m.cachedInfo = m.buildSnapshotLocked()
	return m
}

// UpdateConfig は設定を更新する。不要になったバッファは掃除する。
func (m *Monitor) UpdateConfig(cfg Config) {
	m.mu.Lock()
	defer m.mu.Unlock()

	norm := NormalizeConfig(cfg)
	m.cfg = norm

	activeHosts := make(map[string]bool)
	for _, t := range norm.Targets {
		activeHosts[t.Host] = true
	}

	for host := range m.buffers {
		if !activeHosts[host] {
			delete(m.buffers, host)
			delete(m.lastNotified, host)
			delete(m.okSince, host)
		}
	}
	m.cachedInfo = m.buildSnapshotLocked()
}

// Reset は測定の履歴と通知の状態を捨てる。監視を止めて（区画を非表示にして）再開するときに呼び、
// 古いサンプルが新しい測定と混ざらないようにする。
func (m *Monitor) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.buffers = make(map[string]*targetBuffer)
	m.lastNotified = make(map[string]time.Time)
	m.okSince = make(map[string]time.Time)
	m.cachedInfo = m.buildSnapshotLocked()
}

// PingOnce は登録された全ターゲットに対して並行に Ping を実行し、最新情報とアラートを返す。
func (m *Monitor) PingOnce(ctx context.Context, now time.Time) (Info, []NotifyAlert) {
	m.mu.RLock()
	targets := make([]TargetConfig, len(m.cfg.Targets))
	copy(targets, m.cfg.Targets)
	notifyEnabled := m.cfg.NotifyEnabled
	pinger := m.pinger
	m.mu.RUnlock()

	if len(targets) == 0 || pinger == nil {
		m.mu.Lock()
		m.cachedInfo = Info{}
		m.mu.Unlock()
		return Info{}, nil
	}

	type pingResult struct {
		host    string
		ip      string
		rtt     time.Duration
		success bool
	}

	results := make([]pingResult, len(targets))
	var wg sync.WaitGroup

	for i, t := range targets {
		wg.Add(1)
		go func(idx int, target TargetConfig) {
			defer wg.Done()
			timeout := 1000 * time.Millisecond
			ip, rtt, err := pinger.Ping(ctx, target.Host, timeout)
			results[idx] = pingResult{
				host:    target.Host,
				ip:      ip,
				rtt:     rtt,
				success: err == nil,
			}
		}(i, t)
	}
	wg.Wait()

	m.mu.Lock()
	defer m.mu.Unlock()

	for _, res := range results {
		buf, ok := m.buffers[res.host]
		if !ok {
			buf = newTargetBuffer(DefaultWindowSize)
			m.buffers[res.host] = buf
		}
		buf.record(res.ip, res.rtt, res.success)
	}

	info := m.buildSnapshotLocked()
	m.cachedInfo = info

	var alerts []NotifyAlert
	if notifyEnabled {
		for _, tInfo := range info.Targets {
			if tInfo.IsWarnLoss || tInfo.IsWarnRTT {
				delete(m.okSince, tInfo.Host)
				last, hasNotified := m.lastNotified[tInfo.Host]
				if !hasNotified || now.Sub(last) >= m.cooldown {
					m.lastNotified[tInfo.Host] = now
					var msg string
					if tInfo.IsWarnLoss && tInfo.IsWarnRTT {
						msg = fmt.Sprintf("パケットロス %.1f%% / RTT %dms (閾値超過)", tInfo.LossPercent, tInfo.RTTMs)
					} else if tInfo.IsWarnLoss {
						msg = fmt.Sprintf("パケットロス %.1f%% (閾値超過)", tInfo.LossPercent)
					} else {
						msg = fmt.Sprintf("RTT %dms (閾値超過)", tInfo.RTTMs)
					}
					alerts = append(alerts, NotifyAlert{
						Host:    tInfo.Host,
						Message: msg,
					})
				}
			} else if tInfo.Status == "ok" {
				// 警告なしが RecoverHold 続いたら「復帰した」とみなし、クールダウンを解く（次の悪化を即時に通知できる）。
				// すぐには解かない。閾値の前後を行き来するホストで、通知が連発するのを防ぐため。
				since, ok := m.okSince[tInfo.Host]
				if !ok {
					m.okSince[tInfo.Host] = now
				} else if now.Sub(since) >= RecoverHold {
					delete(m.lastNotified, tInfo.Host)
				}
			} else {
				delete(m.okSince, tInfo.Host)
			}
		}
	}

	return info, alerts
}

func (m *Monitor) buildSnapshotLocked() Info {
	if len(m.cfg.Targets) == 0 {
		return Info{}
	}

	info := Info{
		Targets:    make([]TargetInfo, 0, len(m.cfg.Targets)),
		HasWarning: false,
		SummaryRTT: -1,
	}

	var bestRTT = math.MaxInt32

	for _, target := range m.cfg.Targets {
		buf := m.buffers[target.Host]
		if buf == nil {
			info.Targets = append(info.Targets, TargetInfo{
				Host:     target.Host,
				Status:   "idle",
				RTTMs:    -1,
				AvgRTTMs: -1,
			})
			continue
		}
		tInfo := buf.stats(target.Host, target.LossThresholdPercent, target.RTTThresholdMs)
		if tInfo.IsWarnLoss || tInfo.IsWarnRTT || tInfo.Status == "error" {
			info.HasWarning = true
		}
		if tInfo.RTTMs >= 0 && tInfo.RTTMs < bestRTT {
			bestRTT = tInfo.RTTMs
		}
		info.Targets = append(info.Targets, tInfo)
	}

	if bestRTT != math.MaxInt32 {
		info.SummaryRTT = bestRTT
	}

	return info
}

// Snapshot は現在の最新スナップショットを返す（読み取り専用、スレッドセーフ）。
func (m *Monitor) Snapshot() Info {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cachedInfo
}
