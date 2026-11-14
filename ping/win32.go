//go:build windows

package ping

import (
	"context"
	"errors"
	"fmt"
	"net"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	iphlpapi            = windows.NewLazySystemDLL("iphlpapi.dll")
	procIcmpCreateFile  = iphlpapi.NewProc("IcmpCreateFile")
	procIcmpCloseHandle = iphlpapi.NewProc("IcmpCloseHandle")
	procIcmpSendEcho    = iphlpapi.NewProc("IcmpSendEcho")
)

const (
	ipSuccess        = 0
	ipReqTimedOut    = 11010
	invalidHandleVal = ^windows.Handle(0)
)

// SystemPinger は Windows の Win32 ICMP API (iphlpapi.dll) を使った Ping 実装。
type SystemPinger struct{}

// NewSystemPinger は SystemPinger を作成する。
func NewSystemPinger() *SystemPinger {
	return &SystemPinger{}
}

// Ping は host に対して ICMP Echo Request を 1 回送信し、解決された IP と RTT を返す。
// ctx の期限は、名前解決と Echo の待ち時間の両方にかかる（Echo は、期限までの残りと timeout の短いほうだけ待つ）。
// 名前解決の失敗は *ResolveError で返す（ロスと区別するため）。
func (s *SystemPinger) Ping(ctx context.Context, host string, timeout time.Duration) (string, time.Duration, error) {
	// 名前解決 (IPv4 を優先取得)
	ipStr, ip4, err := resolveIPv4(ctx, host)
	if err != nil {
		return "", 0, &ResolveError{Host: host, Err: err}
	}

	// 名前解決で使った残りの時間で、Echo の待ち時間を決める
	timeoutMs, err := echoTimeout(ctx, timeout, time.Now())
	if err != nil {
		return ipStr, 0, err
	}

	// ICMP ハンドルのオープン
	hIcmp, _, err := procIcmpCreateFile.Call()
	if windows.Handle(hIcmp) == invalidHandleVal || hIcmp == 0 {
		return ipStr, 0, fmt.Errorf("IcmpCreateFile failed: %v", err)
	}
	defer procIcmpCloseHandle.Call(hIcmp)

	// IPAddr (ネットワークバイトオーダーの IPv4 を uint32 に変換)
	// 例: 192.168.1.1 -> [192, 168, 1, 1]
	// Windows の in_addr はメモリ上に [192, 168, 1, 1] と並ぶため、
	// リトルエンディアン CPU では uint32(b[0]) | uint32(b[1])<<8 | ... となる。
	destIP := *(*uint32)(unsafe.Pointer(&ip4[0]))

	sendData := []byte("widget-go-ping-request-data-32b")
	replySize := uint32(256) // ICMP_ECHO_REPLY (約40バイト) + データ + パディング
	replyBuffer := make([]byte, replySize)

	ret, _, callErr := procIcmpSendEcho.Call(
		hIcmp,
		uintptr(destIP),
		uintptr(unsafe.Pointer(&sendData[0])),
		uintptr(len(sendData)),
		0, // RequestOptions (nil)
		uintptr(unsafe.Pointer(&replyBuffer[0])),
		uintptr(replySize),
		uintptr(timeoutMs),
	)

	if ret == 0 {
		// 失敗またはタイムアウト
		errno, ok := callErr.(syscall.Errno)
		if ok && errno == ipReqTimedOut {
			return ipStr, 0, errors.New("request timed out")
		}
		return ipStr, 0, fmt.Errorf("ping failed: %v", callErr)
	}

	// ICMP_ECHO_REPLY のパース
	// オフセット 4: Status (uint32)
	// オフセット 8: RoundTripTime (uint32, ms)
	status := *(*uint32)(unsafe.Pointer(&replyBuffer[4]))
	if status != ipSuccess {
		return ipStr, 0, fmt.Errorf("icmp reply status: %d", status)
	}

	rttMs := *(*uint32)(unsafe.Pointer(&replyBuffer[8]))
	return ipStr, time.Duration(rttMs) * time.Millisecond, nil
}

func resolveIPv4(ctx context.Context, host string) (string, net.IP, error) {
	// 既に IP アドレスとしてパースできるかチェック
	if ip := net.ParseIP(host); ip != nil {
		if ip4 := ip.To4(); ip4 != nil {
			return ip4.String(), ip4, nil
		}
		return "", nil, fmt.Errorf("IPv6 not supported: %s", host)
	}

	// FQDN の名前解決
	resolver := &net.Resolver{}
	ips, err := resolver.LookupIP(ctx, "ip4", host)
	if err != nil {
		return "", nil, err
	}
	for _, ip := range ips {
		if ip4 := ip.To4(); ip4 != nil {
			return ip4.String(), ip4, nil
		}
	}
	return "", nil, fmt.Errorf("no IPv4 address found for %s", host)
}
