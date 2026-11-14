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
	// resolveFails は、名前解決が続けて失敗している回数。名前解決の失敗はサンプルに積まない（ロスとして数えない）。
	resolveFails int
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
	b.resolveFails = 0
	if ip != "" {
		b.targetIP = ip
	}
	if len(b.samples) >= b.windowSize {
		b.samples = b.samples[1:]
	}
	b.samples = append(b.samples, pingSample{success: success, rtt: rtt})
}

// recordResolveFail は名前解決の失敗を記録する。窓のサンプル（ロス率）には入れない。
func (b *targetBuffer) recordResolveFail() {
	b.resolveFails++
}

func (b *targetBuffer) stats(host string, lossThresh, rttThresh int) TargetInfo {
	info := TargetInfo{
		Host:        host,
		TargetIP:    b.targetIP,
		Status:      StatusIdle,
		History:     make([]int, len(b.samples)),
		RTTMs:       -1,
		AvgRTTMs:    -1,
		MinRTTMs:    -1,
		MaxRTTMs:    -1,
		LossPercent: 0,
		ResolveFail: b.resolveFails > 0,
	}

	total := len(b.samples)
	if total == 0 {
		if b.resolveFails >= ResolveFailConsecutive {
			info.Status = StatusUnresolved
		}
		return info
	}
	info.SentCount = total

	var (
		lossCount = 0
		sumRTT    time.Duration
		succCount = 0
		minRTT    = time.Duration(math.MaxInt64)
		maxRTT    = time.Duration(0)
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
	info.LossReady = total >= MinLossSamples

	// 最新 RTT は、直近の 1 回が成功したときだけ出す。失敗（ロス・名前解決の失敗）したのに、窓の中の古い値を出さない
	if last := b.samples[total-1]; last.success && b.resolveFails == 0 {
		info.RTTMs = int(last.rtt.Milliseconds())
	}
	if succCount > 0 {
		avg := sumRTT / time.Duration(succCount)
		info.AvgRTTMs = int(avg.Milliseconds())
		info.MinRTTMs = int(minRTT.Milliseconds())
		info.MaxRTTMs = int(maxRTT.Milliseconds())
	}

	// 閾値判定
	// ロスは、窓に MinLossSamples 回以上のサンプルがあり、MinLossCount 回以上落ちていて、率が閾値以上のとき。
	// 立ち上がり（1/1 = 100% など）や 1 回の取りこぼしでは警告しない。
	if info.LossReady && lossCount >= MinLossCount && info.LossPercent >= float64(lossThresh) {
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
	// 直近の 1 回が名前解決の失敗なら、窓の末尾が遅くても RTT の警告にしない（RTT は -1 で、値が出せない）
	if run >= RTTConsecutive && b.resolveFails == 0 {
		info.IsWarnRTT = true
	}

	switch {
	case b.resolveFails >= ResolveFailConsecutive:
		// 名前解決が続けて失敗している。ロスとは別の状態にする（窓のロス率はそのまま残す）
		info.Status = StatusUnresolved
	case info.LossReady && lossCount == total:
		info.Status = StatusError
	case succCount == 0:
		// 立ち上がりで、まだ失敗だけのとき。判断を保留する
		info.Status = StatusIdle
	case info.IsWarnLoss || info.IsWarnRTT:
		info.Status = StatusWarn
	default:
		info.Status = StatusOK
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
// 測定間隔が変わったときは、すべてのバッファを捨てる（違う間隔のサンプルが窓の中で混ざらないように）。
// 閾値や監視先の変更は、次の測定を待たずにスナップショットへ反映する。
func (m *Monitor) UpdateConfig(cfg Config) {
	m.mu.Lock()
	defer m.mu.Unlock()

	norm := NormalizeConfig(cfg)
	intervalChanged := norm.IntervalSec != m.cfg.IntervalSec
	m.cfg = norm

	if intervalChanged {
		// 通知のクールダウン（lastNotified）は残す。間隔を変えただけで、同じ警告を再び通知しないように
		m.buffers = make(map[string]*targetBuffer)
		m.okSince = make(map[string]time.Time)
	}

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
// 定期の測定（1 周期に 1 回）で呼ぶ。
func (m *Monitor) PingOnce(ctx context.Context, now time.Time) (Info, []NotifyAlert) {
	return m.ping(ctx, now, false)
}

// PingMissing は、まだ 1 度も測っていない監視先（追加・変更した直後）だけに Ping を送る。
// 定期の周期の途中で設定が変わったときに呼び、測り済みのホストに余分なサンプルを足さずに、
// 新しい監視先の値をすぐに出す。測っていない監視先がなければ何も送らない。
func (m *Monitor) PingMissing(ctx context.Context, now time.Time) (Info, []NotifyAlert) {
	return m.ping(ctx, now, true)
}

func (m *Monitor) ping(ctx context.Context, now time.Time, missingOnly bool) (Info, []NotifyAlert) {
	m.mu.RLock()
	targets := make([]TargetConfig, 0, len(m.cfg.Targets))
	for _, t := range m.cfg.Targets {
		if missingOnly && m.buffers[t.Host] != nil {
			continue
		}
		targets = append(targets, t)
	}
	notifyEnabled := m.cfg.NotifyEnabled
	pinger := m.pinger
	noTargets := len(m.cfg.Targets) == 0
	m.mu.RUnlock()

	if noTargets || pinger == nil {
		m.mu.Lock()
		m.cachedInfo = Info{}
		m.mu.Unlock()
		return Info{}, nil
	}
	if len(targets) == 0 {
		// 測っていない監視先がない（PingMissing）。今の状態をそのまま返す
		return m.Snapshot(), nil
	}

	type pingResult struct {
		host string
		ip   string
		rtt  time.Duration
		err  error
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
				host: target.Host,
				ip:   ip,
				rtt:  rtt,
				err:  err,
			}
		}(i, t)
	}
	wg.Wait()

	m.mu.Lock()
	defer m.mu.Unlock()

	// 測っている間に監視先から外れたホストは、記録しない（UpdateConfig で掃除したバッファを作り直さない）
	active := make(map[string]bool, len(m.cfg.Targets))
	for _, t := range m.cfg.Targets {
		active[t.Host] = true
	}
	for _, res := range results {
		if !active[res.host] {
			continue
		}
		buf, ok := m.buffers[res.host]
		if !ok {
			buf = newTargetBuffer(DefaultWindowSize)
			m.buffers[res.host] = buf
		}
		if IsResolveError(res.err) {
			// 名前解決の失敗は、パケットを送れていないのでロスに数えない
			buf.recordResolveFail()
			continue
		}
		buf.record(res.ip, res.rtt, res.err == nil)
	}

	info := m.buildSnapshotLocked()
	m.cachedInfo = info

	var alerts []NotifyAlert
	if notifyEnabled {
		for _, tInfo := range info.Targets {
			if IsAlertStatus(tInfo.Status) {
				delete(m.okSince, tInfo.Host)
				last, hasNotified := m.lastNotified[tInfo.Host]
				if !hasNotified || now.Sub(last) >= m.cooldown {
					m.lastNotified[tInfo.Host] = now
					alerts = append(alerts, NotifyAlert{
						Host:    tInfo.Host,
						Message: alertMessage(tInfo),
					})
				}
			} else if tInfo.Status == StatusOK {
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

// alertMessage は警告の通知本文を作る。ロスは画面と同じ「窓内の失敗回数/サンプル数」で書く。
func alertMessage(t TargetInfo) string {
	loss := fmt.Sprintf("パケットロス %d/%d", t.LossCount, t.SentCount)
	switch {
	case t.Status == StatusUnresolved:
		return "名前解決に失敗しています"
	case t.IsWarnLoss && t.IsWarnRTT:
		return fmt.Sprintf("%s / RTT %dms (閾値超過)", loss, t.RTTMs)
	case t.IsWarnLoss || t.Status == StatusError:
		return loss + " (閾値超過)"
	default:
		return fmt.Sprintf("RTT %dms (閾値超過)", t.RTTMs)
	}
}

func (m *Monitor) buildSnapshotLocked() Info {
	if len(m.cfg.Targets) == 0 {
		return Info{}
	}

	info := Info{
		Targets:    make([]TargetInfo, 0, len(m.cfg.Targets)),
		SummaryRTT: -1,
	}

	for _, target := range m.cfg.Targets {
		buf := m.buffers[target.Host]
		if buf == nil {
			info.Targets = append(info.Targets, TargetInfo{
				Host:     target.Host,
				Status:   StatusIdle,
				RTTMs:    -1,
				AvgRTTMs: -1,
				MinRTTMs: -1,
				MaxRTTMs: -1,
				History:  []int{},
			})
			continue
		}
		tInfo := buf.stats(target.Host, target.LossThresholdPercent, target.RTTThresholdMs)
		// 代表 RTT は最悪値（最大）。良いホストの値で、遅いホストが隠れないようにする。
		// 直近の 1 回が失敗したホストは RTTMs が -1 なので含まれない
		if tInfo.RTTMs > info.SummaryRTT {
			info.SummaryRTT = tInfo.RTTMs
		}
		info.Targets = append(info.Targets, tInfo)
	}

	return info
}

// Snapshot は現在の最新スナップショットを返す（読み取り専用、スレッドセーフ）。
func (m *Monitor) Snapshot() Info {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cachedInfo
}
