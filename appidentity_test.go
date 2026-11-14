package main

import (
	"path/filepath"
	"testing"
)

// MSIX で仮想化されるかどうかに依存せず、テスト版が通常版の設定を参照しないことを確認する。
func TestAppConfigDirectoryDistributionIsolation(t *testing.T) {
	old := appDataDirectoryName
	t.Cleanup(func() { appDataDirectoryName = old })
	root := t.TempDir()
	t.Setenv("LOCALAPPDATA", root)
	t.Setenv("APPDATA", filepath.Join(root, "roaming"))
	appDataDirectoryName = "widget-go"
	regular := appConfigDirectory()
	appDataDirectoryName = "widget-go-msix-test/data"
	isolated := appConfigDirectory()
	if isolated != filepath.Join(root, "widget-go-msix-test", "data") || isolated == regular {
		t.Fatalf("test config=%q, regular config=%q", isolated, regular)
	}
	// 旧認証データの移行元（保存先と同階層の ClaudeMonitor）も通常の保存先と分離する。
	if filepath.Join(filepath.Dir(isolated), "ClaudeMonitor") == filepath.Join(root, "ClaudeMonitor") {
		t.Fatal("legacy auth migration source must be isolated")
	}
	t.Setenv("LOCALAPPDATA", "")
	if got := appConfigDirectory(); got != filepath.Join(root, "roaming", "widget-go-msix-test", "data") {
		t.Fatalf("APPDATA fallback=%q", got)
	}
}
