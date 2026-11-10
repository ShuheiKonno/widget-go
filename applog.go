package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

// アプリ共通のログ（%LOCALAPPDATA%\widget-go\app.log）。GUI アプリは標準エラー出力が見えないため、
// 設定ファイルの破損や保存の失敗など、利用者に知らせたい異常をここへ残す。
// ログの失敗で本来の動作を妨げないよう、書けなくても黙って捨てる。

const appLogMaxBytes = 512 * 1024

var (
	appLogMu   sync.Mutex
	appLogPath string // 空の間は書かない（テストや、保存先が決まる前）
)

// initAppLog はログの出力先を決め、大きくなりすぎていたら app.log.1 へ退避する（既存の退避は上書き）。
func initAppLog(dir string) {
	appLogMu.Lock()
	defer appLogMu.Unlock()
	appLogPath = filepath.Join(dir, "app.log")
	if fi, err := os.Stat(appLogPath); err == nil && fi.Size() > appLogMaxBytes {
		_ = os.Remove(appLogPath + ".1")
		_ = os.Rename(appLogPath, appLogPath+".1")
	}
}

func appLogf(format string, args ...any) {
	appLogMu.Lock()
	defer appLogMu.Unlock()
	if appLogPath == "" {
		return
	}
	_ = os.MkdirAll(filepath.Dir(appLogPath), 0755)
	f, err := os.OpenFile(appLogPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s "+format+"\n", append([]any{time.Now().Format("2006-01-02 15:04:05")}, args...)...)
}

// logToAppLog は標準の log パッケージ（ime など）の出力を app.log へ流す。
// 何もしないと、標準エラー出力の無い GUI アプリでは、それらのログが捨てられる。
type logToAppLog struct{}

func (logToAppLog) Write(p []byte) (int, error) {
	appLogf("%s", strings.TrimRight(string(p), "\r\n"))
	return len(p), nil
}

// redirectStdLog は標準の log の出力先を app.log にする。initAppLog のあとに呼ぶ。
func redirectStdLog() {
	log.SetFlags(0) // 時刻は appLogf が付ける
	log.SetOutput(logToAppLog{})
}

const crashLogMaxBytes = 256 * 1024

// initCrashLog は、回復できないクラッシュ（ほかの goroutine の panic など）のスタックを app.crash.log へ書くようにする。
// 何もしないと、GUI アプリでは、標準エラー出力に出るスタックが捨てられて原因が分からない。
// ファイルは終了まで開いたままにする（クラッシュの瞬間に書くため）。
func initCrashLog(dir string) {
	p := filepath.Join(dir, "app.crash.log")
	if fi, err := os.Stat(p); err == nil && fi.Size() > crashLogMaxBytes {
		_ = os.Remove(p)
	}
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	fmt.Fprintf(f, "\n==== %s 起動 ====\n", time.Now().Format("2006-01-02 15:04:05"))
	if err := debug.SetCrashOutput(f, debug.CrashOptions{}); err != nil {
		_ = f.Close()
	}
}

var procMessageBoxW = user32.NewProc("MessageBoxW")

const (
	mbIconError     = 0x00000010
	mbSetForeground = 0x00010000
)

// showFatalDialog は致命的なエラーを、利用者に見えるダイアログで表示する（OK を押すまで戻らない）。
// 窓もトレイもまだ無い起動の途中でも出せるように、所有ウィンドウなしで出す。テストで差し替える。
var showFatalDialog = func(title, message string) {
	t, err1 := syscall.UTF16PtrFromString(title)
	m, err2 := syscall.UTF16PtrFromString(message)
	if err1 != nil || err2 != nil {
		return
	}
	procMessageBoxW.Call(0, uintptr(unsafe.Pointer(m)), uintptr(unsafe.Pointer(t)), mbIconError|mbSetForeground)
}

// fatalExit は終了の実体。テストで差し替える。
var fatalExit = os.Exit

// fatalf は回復できないエラーを app.log に記録し、ダイアログで知らせて終了する。
// log.Fatalf / panic の代わり（GUI アプリでは、どちらも何も表示されないまま消える）。
func fatalf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	appLogf("致命的なエラー: %s", msg)
	showFatalDialog("システムモニター", msg+"\n\n詳細: "+appLogPathForDisplay())
	fatalExit(1)
}

func appLogPathForDisplay() string {
	appLogMu.Lock()
	defer appLogMu.Unlock()
	return appLogPath
}

// reportPanic は main の panic を記録して知らせる（defer で呼ぶ）。panic でなければ何もしない。
// スタックは app.log に残す。ほかの goroutine の panic は recover できないので、initCrashLog が受け持つ。
func reportPanic() {
	r := recover()
	if r == nil {
		return
	}
	appLogf("panic: %v\n%s", r, debug.Stack())
	showFatalDialog("システムモニター", fmt.Sprintf("予期しないエラーで終了します: %v\n\n詳細: %s", r, appLogPathForDisplay()))
	fatalExit(2)
}
