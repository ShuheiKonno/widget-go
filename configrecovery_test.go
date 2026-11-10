package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// brokenBackups は dir にある、退避された config.json（config.json.broken-*）を返す。
func brokenBackups(t *testing.T, path string) []string {
	t.Helper()
	m, err := filepath.Glob(path + ".broken-*")
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func useTempAppLog(t *testing.T) string {
	t.Helper()
	old := appLogPath
	dir := t.TempDir()
	initAppLog(dir)
	t.Cleanup(func() { appLogMu.Lock(); appLogPath = old; appLogMu.Unlock() })
	return filepath.Join(dir, "app.log")
}

func TestLoadConfigSyntaxErrorBacksUpAndNotifies(t *testing.T) {
	logPath := useTempAppLog(t)
	const broken = `{"theme": "light",`
	path := withConfigFile(t, broken)

	if c := snapshotConfig(); c.Theme != "system" {
		t.Errorf("構文エラーでは既定値のはず: %q", c.Theme)
	}
	b := brokenBackups(t, path)
	if len(b) != 1 {
		t.Fatalf("退避が 1 つあるはず: %v", b)
	}
	if got, _ := os.ReadFile(b[0]); string(got) != broken {
		t.Errorf("退避した中身が元と違う: %q", got)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("退避したので元のファイルは無いはず")
	}
	if configLoadNotice == "" || !strings.Contains(configLoadNotice, filepath.Base(b[0])) {
		t.Errorf("利用者への知らせに退避先が無い: %q", configLoadNotice)
	}
	if log, _ := os.ReadFile(logPath); !strings.Contains(string(log), "解釈できません") {
		t.Errorf("ログに残っていない: %q", log)
	}
}

func TestLoadConfigTypeErrorKeepsOtherFields(t *testing.T) {
	useTempAppLog(t)
	// zoom の型だけ間違っている。ほかの項目は引き継ぐ
	path := withConfigFile(t, `{"theme": "light", "zoom": "big", "netUnit": "bit"}`)

	c := snapshotConfig()
	if c.Theme != "light" || c.NetUnit != "bit" {
		t.Errorf("型の間違いの外にある項目が失われた: theme=%q netUnit=%q", c.Theme, c.NetUnit)
	}
	if c.Zoom != defaultZoomPercent {
		t.Errorf("読めなかった項目は既定のはず: %d", c.Zoom)
	}
	if len(brokenBackups(t, path)) != 1 || configLoadNotice == "" {
		t.Error("型の間違いでも、元のファイルの退避と知らせが必要")
	}
}

func TestLoadConfigValidFileNeitherBacksUpNorNotifies(t *testing.T) {
	useTempAppLog(t)
	path := withConfigFile(t, `{"theme": "dark"}`)
	if len(brokenBackups(t, path)) != 0 || configLoadNotice != "" {
		t.Error("正常なファイルを退避した、または知らせた")
	}
	if _, err := os.Stat(path); err != nil {
		t.Error("正常なファイルが消えた")
	}
}

func TestLoadConfigMissingFileDoesNotNotify(t *testing.T) {
	useTempAppLog(t)
	withConfigFile(t, "")
	if configLoadNotice != "" {
		t.Errorf("初回起動（ファイルなし）で知らせてはいけない: %q", configLoadNotice)
	}
}

func TestWriteConfigLeavesNoTempFileAndRoundTrips(t *testing.T) {
	path := withConfigFile(t, "")
	mutateConfig(func(c *Config) { c.Theme = "light" })
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Error("保存後に一時ファイルが残っている")
	}
	loadConfig()
	if snapshotConfig().Theme != "light" {
		t.Error("保存した内容を読み直せない")
	}
}

func TestWriteConfigFailureIsLoggedOnce(t *testing.T) {
	logPath := useTempAppLog(t)
	path := withConfigFile(t, "")
	// 保存先に同名のディレクトリがあると、置き換えに失敗する
	if err := os.Mkdir(path, 0755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { configMu.Lock(); configWriteFailing = false; configMu.Unlock() })

	for i := 0; i < 3; i++ {
		mutateConfig(func(c *Config) { c.Zoom = 100 + i })
	}
	log, _ := os.ReadFile(logPath)
	if n := strings.Count(string(log), "保存できません"); n != 1 {
		t.Errorf("失敗は続けて出さず 1 回だけのはず: %d 回\n%s", n, log)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Error("失敗後に一時ファイルが残っている")
	}

	// 回復したら、その旨を残す
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	mutateConfig(func(c *Config) { c.Zoom = 110 })
	if log, _ := os.ReadFile(logPath); !strings.Contains(string(log), "回復") {
		t.Errorf("回復がログに残っていない: %s", log)
	}
}

func TestAppLogRotatesWhenTooLarge(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "app.log")
	if err := os.WriteFile(p, make([]byte, appLogMaxBytes+1), 0644); err != nil {
		t.Fatal(err)
	}
	old := appLogPath
	t.Cleanup(func() { appLogMu.Lock(); appLogPath = old; appLogMu.Unlock() })
	initAppLog(dir)
	if _, err := os.Stat(p + ".1"); err != nil {
		t.Error("大きいログが退避されていない")
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Error("退避後は新しく始めるはず")
	}
}

// 保存に失敗した後は、内容が変わらない呼び出しでも書き直す。
// 集中モードの記録はメモリにしか無く、毎秒「同じ」と判定されて保存されないままだと、異常終了で隠したものを戻せなくなる。
func TestMutateConfigIfRetriesAfterFailedWrite(t *testing.T) {
	useTempAppLog(t)
	path := withConfigFile(t, "")
	if err := os.Mkdir(path, 0755); err != nil { // 置き換えに失敗させる
		t.Fatal(err)
	}
	mutateConfigIf(func(c *Config) bool { c.Zoom = 125; return true })
	configMu.Lock()
	failing := configWriteFailing
	configMu.Unlock()
	if !failing {
		t.Fatal("保存の失敗を覚えていない")
	}

	if err := os.Remove(path); err != nil { // 障害が解消した
		t.Fatal(err)
	}
	mutateConfigIf(func(c *Config) bool { return false }) // 内容は変わらない
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("障害が解消しても、メモリにある内容が保存されない")
	}
	if !strings.Contains(string(data), `"zoom": 125`) {
		t.Errorf("失敗していた内容が保存されていない: %s", data)
	}

	// 保存できたので、以後は変更なしなら書かない
	_ = os.Remove(path)
	mutateConfigIf(func(c *Config) bool { return false })
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("回復後は、変更なしで書いてはいけない")
	}
}

func TestBackupBrokenConfigFallsBackToCopyWhenRenameFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	const content = `{"broken"`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	// 開いたままだと、Windows では（共有モードによって）rename が失敗する。失敗したときは内容を書き出して退避する。
	// 共有モードで rename が通る環境でも、退避先に内容があることは変わらない。
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	dst := backupBrokenConfig(path, []byte(content))
	if dst == "" {
		t.Fatal("退避できていない")
	}
	if got, err := os.ReadFile(dst); err != nil || string(got) != content {
		t.Errorf("退避先の内容が元と違う: %q %v", got, err)
	}
}

func TestAppLogRotatesWhileRunning(t *testing.T) {
	dir := t.TempDir()
	old := appLogPath
	t.Cleanup(func() { appLogMu.Lock(); appLogPath = old; appLogMu.Unlock() })
	initAppLog(dir)
	p := filepath.Join(dir, "app.log")
	if err := os.WriteFile(p, make([]byte, appLogMaxBytes+1), 0644); err != nil {
		t.Fatal(err)
	}
	appLogf("起動後に書く")
	if _, err := os.Stat(p + ".1"); err != nil {
		t.Error("常駐中でも、大きくなったログは退避されるはず")
	}
	if b, _ := os.ReadFile(p); !strings.Contains(string(b), "起動後に書く") || len(b) > 1024 {
		t.Errorf("退避後は新しいファイルに書くはず: %d バイト", len(b))
	}
}
