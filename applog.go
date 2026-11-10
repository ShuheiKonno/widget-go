package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
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
