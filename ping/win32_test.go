//go:build windows

package ping

import (
	"context"
	"testing"
	"time"
)

func TestSystemPinger_Localhost(t *testing.T) {
	pinger := NewSystemPinger()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	ip, rtt, err := pinger.Ping(ctx, "127.0.0.1", 1000*time.Millisecond)
	if err != nil {
		t.Fatalf("Ping to 127.0.0.1 failed: %v", err)
	}
	if ip != "127.0.0.1" {
		t.Errorf("expected IP 127.0.0.1, got %q", ip)
	}
	t.Logf("Ping to 127.0.0.1 succeeded: IP=%s RTT=%v", ip, rtt)
}
