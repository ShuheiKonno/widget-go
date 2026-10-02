package aiusage

import (
	"os"
	"path/filepath"
	"testing"
)

// useDataRoot は、host の DataRoot を <tmp>/widget-go にし、旧 ClaudeMonitor のフォルダを
// その隣の <tmp>/ClaudeMonitor として返す。
func useDataRoot(t *testing.T) (dataRoot, legacy string) {
	t.Helper()
	tmp := t.TempDir()
	dataRoot = filepath.Join(tmp, "widget-go")
	legacy = filepath.Join(tmp, "ClaudeMonitor")
	if err := os.MkdirAll(dataRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	h := host.(*fakeHost)
	h.mu.Lock()
	prev := h.root
	h.root = dataRoot
	h.mu.Unlock()
	t.Cleanup(func() { h.mu.Lock(); h.root = prev; h.mu.Unlock() })
	return dataRoot, legacy
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// 旧 ClaudeMonitor の Claude 用プロファイル（Cookie と鍵のほか、引き継がない大きなデータを含む）
func seedLegacy(t *testing.T, legacy, subdir string) string {
	t.Helper()
	eb := filepath.Join(legacy, subdir, "EBWebView")
	writeFile(t, filepath.Join(eb, "Local State"), "KEY")
	writeFile(t, filepath.Join(eb, "Default", "Network", "Cookies"), "COOKIES")
	writeFile(t, filepath.Join(eb, "Default", "Network", "Cookies-journal"), "")
	writeFile(t, filepath.Join(eb, "Default", "Cache", "data_0"), "cache that must not be copied")
	writeFile(t, filepath.Join(eb, "Default", "Local Storage", "leveldb", "000003.log"), "storage")
	return eb
}

// Cookie と鍵（と、あれば journal）だけを引き継ぎ、キャッシュなどは引き継がない。コピー元は変えない。
func TestMigrateLegacyCopiesOnlyCookiesAndKey(t *testing.T) {
	dataRoot, legacy := useDataRoot(t)
	srcEB := seedLegacy(t, legacy, "AuthWebView2")

	claudeView.migrateLegacy()

	dstEB := filepath.Join(dataRoot, "AuthWebView2", "EBWebView")
	if got := readFile(t, filepath.Join(dstEB, "Local State")); got != "KEY" {
		t.Errorf("Local State = %q", got)
	}
	if got := readFile(t, filepath.Join(dstEB, "Default", "Network", "Cookies")); got != "COOKIES" {
		t.Errorf("Cookies = %q", got)
	}
	if !pathExists(filepath.Join(dstEB, "Default", "Network", "Cookies-journal")) {
		t.Error("Cookies-journal も引き継ぐはず")
	}
	for _, p := range []string{"Cache", "Local Storage"} {
		if pathExists(filepath.Join(dstEB, "Default", p)) {
			t.Errorf("%s は引き継がないはず", p)
		}
	}
	if pathExists(dstEB + ".migrating") {
		t.Error("一時フォルダが残っている")
	}
	if !pathExists(claudeView.migrateMarker()) {
		t.Error("引き継いだら印を付けるはず")
	}
	// コピー元は変わらない
	if got := readFile(t, filepath.Join(srcEB, "Default", "Network", "Cookies")); got != "COOKIES" {
		t.Errorf("コピー元が変わった: %q", got)
	}
	if !pathExists(filepath.Join(srcEB, "Default", "Cache", "data_0")) {
		t.Error("コピー元のキャッシュが消えた")
	}
}

// 旧データが無ければ何もしない（印も付けない。後で旧データができたときに引き継げるように）。
func TestMigrateLegacyNoSourceDoesNothing(t *testing.T) {
	dataRoot, _ := useDataRoot(t)
	claudeView.migrateLegacy()
	if pathExists(filepath.Join(dataRoot, "AuthWebView2")) {
		t.Error("旧データが無いのにフォルダを作った")
	}
	if pathExists(claudeView.migrateMarker()) {
		t.Error("旧データが無いときは印を付けないはず")
	}
}

// Cookie が無い（鍵だけ）、鍵が無い（Cookie だけ）旧データは、引き継ぐ対象が無いものとして扱う。
func TestMigrateLegacyIncompleteSourceIsIgnored(t *testing.T) {
	dataRoot, legacy := useDataRoot(t)
	writeFile(t, filepath.Join(legacy, "AuthWebView2", "EBWebView", "Local State"), "KEY")
	claudeView.migrateLegacy()
	if pathExists(filepath.Join(dataRoot, "AuthWebView2", "EBWebView")) {
		t.Error("Cookie が無いのに引き継いだ")
	}
	if err := copyLegacyAuth(filepath.Join(legacy, "AuthWebView2", "EBWebView"), filepath.Join(dataRoot, "x")); err != errNoLegacy {
		t.Errorf("errNoLegacy のはず: %v", err)
	}
}

// 既に認証データがあれば（利用者が先にログインした等）、上書きせず、印だけを付ける。
func TestMigrateLegacyDoesNotOverwriteExisting(t *testing.T) {
	dataRoot, legacy := useDataRoot(t)
	seedLegacy(t, legacy, "AuthWebView2")
	existing := filepath.Join(dataRoot, "AuthWebView2", "EBWebView")
	writeFile(t, filepath.Join(existing, "Default", "Network", "Cookies"), "MINE")

	claudeView.migrateLegacy()

	if got := readFile(t, filepath.Join(existing, "Default", "Network", "Cookies")); got != "MINE" {
		t.Errorf("既存の認証データを上書きした: %q", got)
	}
	if !pathExists(claudeView.migrateMarker()) {
		t.Error("既存があるときも印を付けるはず（後でログアウトしても旧データを戻さない）")
	}
}

// ログアウト（認証フォルダの削除）の後に再作成しても、旧データは戻らない。戻ると、ログアウトが無効になる。
func TestMigrateLegacyNotRestoredAfterLogout(t *testing.T) {
	dataRoot, legacy := useDataRoot(t)
	seedLegacy(t, legacy, "AuthWebView2")

	claudeView.migrateLegacy() // 初回: 引き継ぐ
	dst := filepath.Join(dataRoot, "AuthWebView2")
	if !pathExists(filepath.Join(dst, "EBWebView")) {
		t.Fatal("初回に引き継げていない")
	}

	if err := os.RemoveAll(dst); err != nil { // ログアウト
		t.Fatal(err)
	}
	claudeView.migrateLegacy() // 再作成のとき
	if pathExists(filepath.Join(dst, "EBWebView")) {
		t.Error("ログアウトの後に旧データが戻った（ログアウトが無効になる）")
	}
}

// コピーに失敗したら、中途半端なものを残さず、印も付けない（次回に再試行する）。
func TestMigrateLegacyFailureLeavesNothing(t *testing.T) {
	dataRoot, legacy := useDataRoot(t)
	eb := filepath.Join(legacy, "AuthWebView2", "EBWebView")
	writeFile(t, filepath.Join(eb, "Local State"), "KEY")
	// Cookies の場所がフォルダ（読めない）: 存在はするので引き継ぎを試み、コピーで失敗する
	if err := os.MkdirAll(filepath.Join(eb, "Default", "Network", "Cookies"), 0o755); err != nil {
		t.Fatal(err)
	}

	claudeView.migrateLegacy()

	dstEB := filepath.Join(dataRoot, "AuthWebView2", "EBWebView")
	if pathExists(dstEB) || pathExists(dstEB+".migrating") {
		t.Error("失敗したのに中途半端なものが残っている")
	}
	if pathExists(claudeView.migrateMarker()) {
		t.Error("失敗したときは印を付けないはず（次回に再試行する）")
	}
}

// プロバイダーごとに別のフォルダ・別の印。Codex は CodexAuthWebView2。
func TestMigrateLegacyPerProvider(t *testing.T) {
	dataRoot, legacy := useDataRoot(t)
	seedLegacy(t, legacy, "CodexAuthWebView2")

	claudeView.migrateLegacy()
	if pathExists(filepath.Join(dataRoot, "AuthWebView2")) || pathExists(claudeView.migrateMarker()) {
		t.Error("Claude 側は旧データが無いので何もしないはず")
	}
	codexView.migrateLegacy()
	if !pathExists(filepath.Join(dataRoot, "CodexAuthWebView2", "EBWebView", "Default", "Network", "Cookies")) {
		t.Error("Codex の Cookie を引き継げていない")
	}
	if !pathExists(codexView.migrateMarker()) || codexView.migrateMarker() == claudeView.migrateMarker() {
		t.Error("プロバイダーごとの印のはず")
	}
}

// 生成の流れの中で呼ばれる（create の先頭）: 引き継いだ上で、WebView の生成に進む。
func TestCreateMigratesBeforeCreatingWebView(t *testing.T) {
	calls := stubWebView(t)
	dataRoot, legacy := useDataRoot(t)
	seedLegacy(t, legacy, "AuthWebView2")
	withConfig(t, func(c *Settings) { *c = DefaultSettings(); c.Enabled = true; c.CodexEnabled = false })

	claudeView.sync(snapshotConfig())

	if !pathExists(filepath.Join(dataRoot, "AuthWebView2", "EBWebView", "Default", "Network", "Cookies")) {
		t.Error("WebView の生成より前に引き継いでいない")
	}
	if calls.Load() != 1 {
		t.Errorf("WebView の生成は 1 回: %d 回", calls.Load())
	}
}
