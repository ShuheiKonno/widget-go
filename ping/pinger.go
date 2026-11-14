package ping

import (
	"context"
	"errors"
	"time"
)

// Pinger は 1 回の Ping（ICMP Echo Request/Reply）を実行する抽象インターフェース。
// テスト時に FakePinger で差し替える。
// 名前解決に失敗したときは、ResolveError で包んだエラーを返す（ロスと区別するため）。
type Pinger interface {
	Ping(ctx context.Context, host string, timeout time.Duration) (ip string, rtt time.Duration, err error)
}

// ResolveError は名前解決の失敗を表す。Monitor はこれをロスとして数えず、別の状態（StatusUnresolved）で扱う。
type ResolveError struct {
	Host string
	Err  error
}

func (e *ResolveError) Error() string { return "resolve " + e.Host + ": " + e.Err.Error() }
func (e *ResolveError) Unwrap() error { return e.Err }

// IsResolveError は err が名前解決の失敗かを返す。
func IsResolveError(err error) bool {
	var re *ResolveError
	return errors.As(err, &re)
}

// ErrNoTimeLeft は、ctx の期限までに Echo を待つ時間が残っていないことを表す（ロスとして数える）。
var ErrNoTimeLeft = errors.New("no time left before deadline")

// echoTimeout は IcmpSendEcho に渡す待ち時間（ms）を決める。IcmpSendEcho は途中で止められないので、
// ctx の期限（名前解決に使った残り）に収まるよう timeout を切り詰める。残りがなければ ErrNoTimeLeft
// （ctx が取り消されていればその理由）を返し、送らない。1ms 未満の端数は切り捨てず 1ms にする
// （0 を渡すと API 側の扱いが不定になるため）。
func echoTimeout(ctx context.Context, timeout time.Duration, now time.Time) (uint32, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if timeout <= 0 {
		timeout = time.Second
	}
	if dl, ok := ctx.Deadline(); ok {
		left := dl.Sub(now)
		if left <= 0 {
			return 0, ErrNoTimeLeft
		}
		if left < timeout {
			timeout = left
		}
	}
	ms := timeout.Milliseconds()
	if ms < 1 {
		ms = 1
	}
	return uint32(ms), nil
}
