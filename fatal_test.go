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
	oldDialog, oldExit := showFatalDialog, fatalExit
	d, code := []string{}, -1
	showFatalDialog = func(title, message string) { d = append(d, title+"|"+message) }
	fatalExit = func(c int) { code = c }
	t.Cleanup(func() { showFatalDialog, fatalExit = oldDialog, oldExit })
	return &d, &code
}

func TestFatalfLogsShowsDialogAndExits(t *testing.T) {
	logPath := useTempAppLog(t)
	dialogs, code := useFakeFatal(t)

	fatalf("ポートを確保できません: %v", "boom")

	if *code != 1 {
		t.Errorf("終了コードは 1 のはず: %d", *code)
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
	if len(*dialogs) != 0 || *code != -1 {
		t.Error("panic が無いのに、ダイアログや終了が起きた")
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
