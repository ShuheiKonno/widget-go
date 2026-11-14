//go:build windows

package ime

import (
	"bytes"
	"log"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

var (
	procCreateWindowExW = imeUser32.NewProc("CreateWindowExW")
	procDestroyWindow   = imeUser32.NewProc("DestroyWindow")
)

const (
	wsPopup          = 0x80000000
	wsVisible        = 0x10000000
	wsExToolWindow   = 0x00000080
	wsExNoActivate   = 0x08000000
	testWindowClass  = "Static"
	detectLogMessage = "IME左上ボックス検知"
)

// syncBuffer は複数の goroutine から書かれるログを集める。
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// TestWatcherDetectsAfterRestart は、本物の WinEvent Hook で、起動 → 停止 → 再起動の後も
// 左上のウィンドウの検知が Recoverer まで届くことを確かめる（OBJID_WINDOW での絞り込みを
// 誤ると、検知が黙って止まってもほかのテストは通ってしまうため）。
//
// IME のウィンドウは作れないので、テストの間だけ "Static" クラスを検知対象に加え、
// 画面の左上 (0,0) に小さなポップアップを出す。DryRun なので復旧処理（他のウィンドウへの操作）は行わない。
func TestWatcherDetectsAfterRestart(t *testing.T) {
	cfg := Config{Enabled: true, CooldownSec: 2, ThresholdPx: 5, DryRun: true}

	logs := &syncBuffer{}
	oldOut, oldFlags := log.Writer(), log.Flags()
	log.SetOutput(logs)
	t.Cleanup(func() { log.SetOutput(oldOut); log.SetFlags(oldFlags) })

	// 監視を止めている間に検知対象を変える（コールバックが読む map と競合しないように）
	imeClassNames[testWindowClass] = true
	t.Cleanup(func() {
		_ = StopWatcher()
		delete(imeClassNames, testWindowClass)
	})

	if err := StartWatcher(cfg); err != nil {
		t.Skipf("WinEvent Hook を張れない環境: %v", err)
	}
	if err := StopWatcher(); err != nil {
		t.Fatal(err)
	}
	if err := StartWatcher(cfg); err != nil {
		t.Fatal(err)
	}

	// ウィンドウは、作ったスレッドで壊す必要があるので OS スレッドを固定する
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	className, _ := syscall.UTF16PtrFromString(testWindowClass)
	hwnd, _, err := procCreateWindowExW.Call(
		uintptr(wsExToolWindow|wsExNoActivate),
		uintptr(unsafe.Pointer(className)),
		0,
		uintptr(wsPopup|wsVisible),
		0, 0, 40, 20,
		0, 0, 0, 0,
	)
	if hwnd == 0 {
		t.Skipf("テスト用のウィンドウを作れない: %v", err)
	}
	defer procDestroyWindow.Call(hwnd)
	if got := getClassName(hwnd); got != testWindowClass {
		t.Fatalf("作ったウィンドウのクラス名 = %q, want %q", got, testWindowClass)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(logs.String(), detectLogMessage) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("再起動後、3 秒以内に検知のログが出ない。ログ:\n%s", logs.String())
}
