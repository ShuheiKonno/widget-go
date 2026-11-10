package main

import (
	"log"
	"os"
	"strings"
	"testing"
)

// useFakeFatal は、ダイアログと終了を偽物に差し替える（テストが実際に表示・終了しないように）。
func useFakeFatal(t *testing.T) (dialogs *[]string, exitCode *int) {
	t.Helper()
	oldDialog, oldExit, oldCleanup := showFatalDialog, fatalExit, fatalCleanup
	d, code := []string{}, -1
	showFatalDialog = func(title, message string) { d = append(d, title+"|"+message) }
	fatalExit = func(c int) { code = c }
	fatalCleanup = func() { fatalCleanupCalls++ }
	fatalCleanupCalls = 0
	t.Cleanup(func() { showFatalDialog, fatalExit, fatalCleanup = oldDialog, oldExit, oldCleanup })
	return &d, &code
}

// fatalCleanupCalls は、偽の fatalCleanup が呼ばれた回数。
var fatalCleanupCalls int

// このパッケージのテストは、既定で本物のダイアログも終了も後始末も使わない
// （useFakeFatal なしで fatalf に届いても、MessageBox で CI が止まったり、プロセスが終わったりしないように）。
func init() {
	showFatalDialog = func(title, message string) {}
	fatalExit = func(int) {}
	fatalCleanup = func() {}
}

func TestFatalfLogsShowsDialogAndExits(t *testing.T) {
	logPath := useTempAppLog(t)
	dialogs, code := useFakeFatal(t)

	fatalf("ポートを確保できません: %v", "boom")

	if *code != 1 {
		t.Errorf("終了コードは 1 のはず: %d", *code)
	}
	if fatalCleanupCalls != 1 {
		t.Errorf("終了の前に、隠したものを戻す後始末が必要: %d 回", fatalCleanupCalls)
	}
	if len(*dialogs) != 1 || !strings.Contains((*dialogs)[0], "boom") || !strings.Contains((*dialogs)[0], logPath) {
		t.Errorf("ダイアログに原因とログの場所が必要: %v", *dialogs)
	}
	if b, _ := os.ReadFile(logPath); !strings.Contains(string(b), "致命的なエラー: ポートを確保できません: boom") {
		t.Errorf("ログに残っていない: %s", b)
	}
}

func TestReportPanicRecordsStackAndExits(t *testing.T) {
	logPath := useTempAppLog(t)
	dialogs, code := useFakeFatal(t)

	func() {
		defer reportPanic()
		panic("kaboom")
	}()

	if *code != 2 {
		t.Errorf("panic の終了コードは 2 のはず: %d", *code)
	}
	if fatalCleanupCalls != 1 {
		t.Errorf("panic でも、隠したものを戻す後始末が必要: %d 回", fatalCleanupCalls)
	}
	if len(*dialogs) != 1 || !strings.Contains((*dialogs)[0], "kaboom") {
		t.Errorf("ダイアログに panic の内容が必要: %v", *dialogs)
	}
	b, _ := os.ReadFile(logPath)
	if !strings.Contains(string(b), "panic: kaboom") || !strings.Contains(string(b), "goroutine") {
		t.Errorf("ログに panic とスタックが必要: %s", b)
	}
}

func TestReportPanicDoesNothingWithoutPanic(t *testing.T) {
	useTempAppLog(t)
	dialogs, code := useFakeFatal(t)
	func() { defer reportPanic() }()
	if len(*dialogs) != 0 || *code != -1 || fatalCleanupCalls != 0 {
		t.Error("panic が無いのに、ダイアログや終了や後始末が起きた")
	}
}

func TestRedirectStdLogWritesToAppLog(t *testing.T) {
	logPath := useTempAppLog(t)
	oldW, oldF := log.Writer(), log.Flags()
	t.Cleanup(func() { log.SetOutput(oldW); log.SetFlags(oldF) })

	redirectStdLog()
	log.Printf("IME 検知 %d", 42)

	b, _ := os.ReadFile(logPath)
	if !strings.Contains(string(b), "IME 検知 42") {
		t.Errorf("標準 log の出力が app.log に出ていない: %s", b)
	}
	if strings.Count(string(b), "\n") != 1 {
		t.Errorf("1 回の出力は 1 行のはず: %q", b)
	}
}
