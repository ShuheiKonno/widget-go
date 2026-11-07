package ping

import (
	"context"
	"time"
)

// Pinger は 1 回の Ping（ICMP Echo Request/Reply）を実行する抽象インターフェース。
// テスト時に FakePinger で差し替える。
type Pinger interface {
	Ping(ctx context.Context, host string, timeout time.Duration) (ip string, rtt time.Duration, err error)
}
