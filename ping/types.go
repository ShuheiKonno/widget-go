package ping

import (
	"errors"
	"net"
	"strings"
	"time"
)

const (
	// MaxTargets は登録可能な監視先の最大数。
	MaxTargets = 5
	// DefaultWindowSize は直近の統計窓サイズ（回数）。
	DefaultWindowSize = 20
	// DefaultIntervalSec は定期測定の既定間隔（秒）。
	DefaultIntervalSec = 2
	// MinIntervalSec・MaxIntervalSec は測定間隔（秒）の範囲。
	// 窓は回数（DefaultWindowSize）で固定なので、間隔が長いと窓が何分にも伸び、
	// 復帰判定（RecoverHold）が 1〜2 回の応答で済んでしまう。上限を 10 秒（窓は最長約 200 秒）に抑える。
	MinIntervalSec = 1
	MaxIntervalSec = 10
	// DefaultLossThreshold はパケットロス率の既定閾値（%）。
	DefaultLossThreshold = 10
	// DefaultRTTThreshold は RTT の既定閾値（ms）。
	DefaultRTTThreshold = 100
	// DefaultCooldown は同一ホストの再通知を抑止するクールダウン時間（要因ごとではなく、ホストごと）。
	DefaultCooldown = 5 * time.Minute
	// RecoverHold は、警告が出ていないことがこの時間続いたら「復帰した」とみなし、クールダウンを解く時間。
	// 閾値の前後を行き来する不安定なホストで、通知が連発しないようにする。
	RecoverHold = time.Minute
	// MinLossSamples は、ロスを表示・判定するのに必要な、窓の中のサンプル数の下限。
	// これ未満の立ち上がりの間は、ロスを「--」と表示し、ロスの警告もエラーも出さない（1/1 = 100% などを出さない）。
	MinLossSamples = 3
	// MinLossCount は、パケロスで警告するのに必要な、窓の中のロス回数の下限。
	// 1 回の取りこぼしでは警告しない。
	MinLossCount = 2
	// ResolveFailConsecutive は、名前解決の失敗を「解決できない」状態（StatusUnresolved）とみなす連続回数。
	// 一時的な DNS の失敗 1 回では警告しない。
	ResolveFailConsecutive = 3
	// RTTConsecutive は、RTT で警告するのに必要な、閾値以上の応答が直近で連続する回数。
	// 一瞬のスパイクでは警告しない。
	RTTConsecutive = 3
	// MaxHostLen は監視先ホスト名の最大長（DNS の上限）。
	MaxHostLen = 253
)

// TargetConfig は監視先 1 件の設定。
type TargetConfig struct {
	Host                 string `json:"host"`                 // 監視先ホスト名または IPv4 アドレス (FQDN/IPv4。IPv6 は未対応)
	LossThresholdPercent int    `json:"lossThresholdPercent"` // パケットロス率閾値 (1-100%)、既定 10
	RTTThresholdMs       int    `json:"rttThresholdMs"`       // RTT 閾値 (ms)、既定 100
}

// Config は Ping 監視機能全体の設定。
type Config struct {
	Targets       []TargetConfig `json:"targets"`       // 監視先リスト（最大 5 件）
	NotifyEnabled bool           `json:"notifyEnabled"` // 閾値超過時のポップアップ通知、既定 true
	IntervalSec   int            `json:"intervalSec"`   // 測定間隔 (秒、1〜10)、既定 2
}

// TargetInfo.Status の値。
const (
	StatusIdle       = "idle"       // まだ判断できない（サンプルがない、または立ち上がりで失敗だけ）
	StatusOK         = "ok"         // 警告なし
	StatusWarn       = "warn"       // ロスか RTT が閾値を超えている
	StatusError      = "error"      // 窓の中がすべてロス（MinLossSamples 回以上）
	StatusUnresolved = "unresolved" // 名前解決が ResolveFailConsecutive 回続けて失敗している（ロスとは別に扱う）
)

// IsAlertStatus は、その状態が警告（見出しの件数・通知の対象）かを返す。警告の判定はここに一本化する。
func IsAlertStatus(status string) bool {
	return status == StatusWarn || status == StatusError || status == StatusUnresolved
}

// TargetInfo は監視先 1 件のリアルタイムスナップショット。
type TargetInfo struct {
	Host        string  `json:"host"`        // 設定されたホスト名
	TargetIP    string  `json:"targetIP"`    // 解決された IP アドレス
	RTTMs       int     `json:"rttMs"`       // 最新 RTT (ms)。直近の 1 回が失敗（ロス・名前解決の失敗）なら -1。0 は 1ms 未満
	AvgRTTMs    int     `json:"avgRttMs"`    // 直近窓の平均 RTT (ms)
	MinRTTMs    int     `json:"minRttMs"`    // 直近窓の最小 RTT (ms)
	MaxRTTMs    int     `json:"maxRttMs"`    // 直近窓の最大 RTT (ms)
	LossPercent float64 `json:"lossPercent"` // パケットロス率 (%)。閾値の判定に使う。表示は LossCount/SentCount
	SentCount   int     `json:"sentCount"`   // 直近窓内の送信数（名前解決の失敗は数えない）
	LossCount   int     `json:"lossCount"`   // 直近窓内のロス数（名前解決の失敗は数えない）
	LossReady   bool    `json:"lossReady"`   // ロスを表示・判定できるだけのサンプルがあるか（SentCount >= MinLossSamples）
	Status      string  `json:"status"`      // StatusIdle | StatusOK | StatusWarn | StatusError | StatusUnresolved
	IsWarnLoss  bool    `json:"isWarnLoss"`  // パケロス閾値超過フラグ（どの値を赤くするか用。警告かどうかは Status で見る）
	IsWarnRTT   bool    `json:"isWarnRTT"`   // RTT 閾値超過フラグ（同上）
	ResolveFail bool    `json:"resolveFail"` // 直近の 1 回が名前解決の失敗だったか
	History     []int   `json:"history"`     // 直近窓の RTT 履歴 (-1 はロス)
}

// Info は Ping 監視区画全体のスナップショット。
type Info struct {
	Targets    []TargetInfo `json:"targets"`    // 各監視先の状態
	SummaryRTT int          `json:"summaryRtt"` // 見出しで使う代表 RTT (ms)。直近の 1 回が成功したホストの最新 RTT の最大（最悪値）。なければ -1
}

// NotifyAlert は閾値超過時に発行される通知情報。
type NotifyAlert struct {
	Host    string // 対象ホスト
	Message string // 通知本文
}

// DefaultConfig は初期設定を返す。
func DefaultConfig() Config {
	return Config{
		Targets:       nil,
		NotifyEnabled: true,
		IntervalSec:   DefaultIntervalSec,
	}
}

// ValidateHost は監視先ホストの書式を検証する。空白は取り除いてから見る。
// FQDN（ASCII の英数字・ハイフン・アンダースコア・ドット）か IPv4 だけを受け付ける。IPv6 は未対応。
func ValidateHost(host string) error {
	h := strings.TrimSpace(host)
	if h == "" {
		return errors.New("ホスト名が空です")
	}
	if len(h) > MaxHostLen {
		return errors.New("ホスト名が長すぎます: " + h[:20] + "…")
	}
	if strings.Contains(h, ":") {
		return errors.New("IPv6 アドレスは未対応です: " + h)
	}
	if net.ParseIP(h) != nil {
		return nil
	}
	h = strings.TrimSuffix(h, ".")
	if h == "" {
		return errors.New("ホスト名が正しくありません")
	}
	for _, label := range strings.Split(h, ".") {
		if label == "" || len(label) > 63 {
			return errors.New("ホスト名が正しくありません: " + host)
		}
		for _, r := range label {
			ok := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_'
			if !ok {
				return errors.New("ホスト名に使えない文字があります: " + host)
			}
		}
	}
	return nil
}

// NormalizeConfig は手編集や外部入力の値を安全な範囲に矯正する。
func NormalizeConfig(in Config) Config {
	out := in
	switch {
	case out.IntervalSec < MinIntervalSec:
		out.IntervalSec = DefaultIntervalSec
	case out.IntervalSec > MaxIntervalSec:
		// 以前の上限（60 秒）で保存された値は、上限に寄せる（既定値に戻すと、長くしたい意図から離れすぎる）
		out.IntervalSec = MaxIntervalSec
	}
	cleanTargets := make([]TargetConfig, 0, len(in.Targets))
	seen := make(map[string]bool)
	for _, t := range in.Targets {
		host := strings.TrimSpace(t.Host)
		// 空・書式が正しくない・大文字小文字違いも含めた重複は捨てる（統計のバッファがホスト名で共有されるため）
		if ValidateHost(host) != nil {
			continue
		}
		key := strings.ToLower(host)
		if seen[key] {
			continue
		}
		seen[key] = true
		loss := t.LossThresholdPercent
		if loss < 1 || loss > 100 {
			loss = DefaultLossThreshold
		}
		rtt := t.RTTThresholdMs
		if rtt <= 0 || rtt > 10000 {
			rtt = DefaultRTTThreshold
		}
		cleanTargets = append(cleanTargets, TargetConfig{
			Host:                 host,
			LossThresholdPercent: loss,
			RTTThresholdMs:       rtt,
		})
		if len(cleanTargets) >= MaxTargets {
			break
		}
	}
	out.Targets = cleanTargets
	return out
}
