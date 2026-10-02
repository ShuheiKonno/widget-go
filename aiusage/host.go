// Package aiusage は Claude / Codex の使用量とサービス障害を取得する層。
//
// ClaudeMonitor（取り込み元: ShuheiKonno/ClaudeMonitor main cc10dda）から、ウィンドウ・
// トレイ・設定の保存・メイン UI を除いて切り出したもの。ウィンドウ・トレイ・設定・HTTP は
// 呼び出し側（widget-go の main）が持ち、この層は Host を通してそれらを使う。
// claude.ai / chatgpt.com の内部 API が変わったときの修正は、ClaudeMonitor と見比べて両方に当てる。
//
// # スレッドの規則（go-webview2 の仕様による。docs/plan-claude-monitor.md §2.3）
//
//   - 補助 WebView の生成・破棄・Eval・Navigate・Win32 呼び出しは、Host.Dispatch で
//     メイン WebView の UI スレッドへ寄せる。
//   - UI スレッドから refresh 系（refreshUsage / refreshAllUsage / refreshCodexUsage）を同期で
//     呼ばない。Dispatch(Eval) した後に Bind コールバックの完了を待つため、デッドロックする。
//   - Bind コールバックは UI スレッドで呼ばれる。ブロックする処理は go で逃がす。
package aiusage

// バルーン通知のアイコン種別（NIIF_*）。
const (
	NIIF_INFO    = 0x00000001
	NIIF_WARNING = 0x00000002
	NIIF_ERROR   = 0x00000003
)

// Host は呼び出し側（widget-go の main）が実装する。
type Host interface {
	// Dispatch はメイン WebView の UI スレッドで f を実行する。メイン WebView の生成前
	// （起動の最序盤）は f をその場で実行してよい。
	Dispatch(f func())
	// Balloon はトレイ経由でバルーン通知を出す。flag は NIIF_INFO / WARNING / ERROR。
	Balloon(title, msg string, flag uint32)
	// OnSnapshotChanged は使用量・認証状態のスナップショットが更新されたときに呼ばれる
	// （トレイの再描画用）。ブロックしないこと。
	OnSnapshotChanged()
	// OnAuthWindowHidden はログイン用の補助ウィンドウを閉じた直後に呼ばれる。
	// 補助ウィンドウの表示・非表示でメインウィンドウの最前面が外れることがあるため、
	// 呼び出し側で再適用する。
	OnAuthWindowHidden()
	// Settings は現在の設定のコピーを返す。
	Settings() Settings
	// MutateSettings は f で設定を変更して保存する。
	MutateSettings(f func(*Settings))
	// DataRoot は認証データと notify.log の置き場（%LOCALAPPDATA%\widget-go）。
	DataRoot() string
}

// host は Start で設定される。起動後は書き換えない（読み取りは複数 goroutine から行われる）。
var host Host

func hostDispatch(f func()) {
	if host == nil {
		f()
		return
	}
	host.Dispatch(f)
}

func notifyChanged() {
	if host != nil {
		host.OnSnapshotChanged()
	}
}

func snapshotConfig() Settings {
	if host == nil {
		return DefaultSettings()
	}
	return host.Settings()
}

func mutateConfig(f func(*Settings)) {
	if host != nil {
		host.MutateSettings(f)
	}
}

func defaultBalloon(title, msg string, flag uint32) {
	if host != nil {
		host.Balloon(title, msg, flag)
	}
}
