package aiusage

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// 旧 ClaudeMonitor の認証データの引き継ぎ（D1）。
//
// 旧 ClaudeMonitor は %LOCALAPPDATA%\ClaudeMonitor\{AuthWebView2,CodexAuthWebView2} に認証データ
// （Cookie を含むブラウザのプロファイル。実測で Claude 側が 624MB）を持つ。統合版は初めて
// 認証用の WebView を作るときに、このうち Cookie と、それを復号する鍵（Local State）だけを
// コピーする。フォルダ全体は、キャッシュなどで大きいうえ、引き継ぐ必要がない。
// 同じ Windows ユーザー内なら、このコピーだけで認証が通ることを実測で確認した（docs/t0-report.md）。
//
// コピー元は変更しない（ClaudeMonitor を並行して使えるようにするため）。

// legacyFiles は引き継ぐファイル（EBWebView からの相対パス）。required が真のものが無ければ、
// 引き継ぐ対象が無いものとして何もしない。
var legacyFiles = []struct {
	rel      string
	required bool
}{
	{"Local State", true},
	{filepath.Join("Default", "Network", "Cookies"), true},
	{filepath.Join("Default", "Network", "Cookies-journal"), false},
}

var errNoLegacy = errors.New("no legacy auth data")

// legacyRoot は旧 ClaudeMonitor のデータの置き場（DataRoot と同じ階層の ClaudeMonitor）。
func legacyRoot() string {
	if host == nil {
		return ""
	}
	root := host.DataRoot()
	if root == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(root), "ClaudeMonitor")
}

func pathExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// migrateMarker は、引き継ぎを済ませた（または不要と判断した）ことを示す印のファイル。
// 印が無いと、ログアウト（認証フォルダの削除）の後の再作成で旧データが戻り、ログアウトが
// 無効になってしまう。
func (v *authView) migrateMarker() string {
	if host == nil {
		return ""
	}
	return filepath.Join(host.DataRoot(), v.p.dataSubdir+".migrated")
}

// migrateLegacy は、認証用の WebView を作る前に呼ぶ。
//   - 印があれば何もしない。
//   - 既に認証データがあれば（利用者が先にログインした等）、上書きせず、印だけを付ける。
//   - 旧データが無ければ何もしない（印も付けない）。
//   - コピーに失敗したら（旧 ClaudeMonitor が起動中で Cookie がロックされている等）、
//     中途半端なものを残さず、次回に再試行する（印は付けない）。
func (v *authView) migrateLegacy() {
	marker := v.migrateMarker()
	if marker == "" || pathExists(marker) {
		return
	}
	dst := filepath.Join(v.dataDir(), "EBWebView")
	if pathExists(dst) {
		writeMarker(marker)
		return
	}
	src := filepath.Join(legacyRoot(), v.p.dataSubdir, "EBWebView")
	switch err := copyLegacyAuth(src, dst); {
	case err == nil:
		writeMarker(marker)
		notifyLog("migrate %s: 旧 ClaudeMonitor の認証データを引き継いだ", v.p.id)
	case errors.Is(err, errNoLegacy):
		// 旧データが無い。何もしない。
	default:
		notifyLog("migrate %s: 引き継げなかった（次回に再試行する）: %v", v.p.id, err)
	}
}

func writeMarker(path string) {
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	_ = os.WriteFile(path, []byte("legacy ClaudeMonitor auth data handled\n"), 0o644)
}

// copyLegacyAuth は src（旧 EBWebView）から dst（新 EBWebView）へ、legacyFiles だけをコピーする。
// 一時フォルダに作ってから改名するので、途中で失敗しても dst には何も残らない。
func copyLegacyAuth(src, dst string) error {
	for _, f := range legacyFiles {
		if f.required && !pathExists(filepath.Join(src, f.rel)) {
			return errNoLegacy
		}
	}
	tmp := dst + ".migrating"
	_ = os.RemoveAll(tmp)
	for _, f := range legacyFiles {
		from := filepath.Join(src, f.rel)
		if !pathExists(from) { // 必須でないものが無いだけ
			continue
		}
		if err := copyFile(from, filepath.Join(tmp, f.rel)); err != nil {
			_ = os.RemoveAll(tmp)
			return fmt.Errorf("%s: %w", f.rel, err)
		}
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.RemoveAll(tmp)
		return err
	}
	return nil
}

func copyFile(from, to string) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return err
	}
	out, err := os.Create(to)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
