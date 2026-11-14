package main

import (
	"net/http"
	"os"
	"path/filepath"
)

// openAppLogFile は app.log を既定のアプリで開く。テストで本物を開かないよう差し替えられる。
// 中身はスキンフォルダを開く処理と同じ ShellExecute("open", path, SW_SHOWNORMAL) で、
// フォルダならエクスプローラ、ファイルなら関連付けられたアプリで開く。
var openAppLogFile = openSkinDirectory

// appLogOpenHandler は POST /api/applog/open のハンドラ。設定画面の「app.log を開く」ボタンから呼ぶ。
//
// 開く対象はサーバー側で固定する（appLogPath）。クライアントからパスは受け取らない。
// ファイルがまだ無いとき（ログが 1 行も書かれていない）は空のファイルを作ってから開く。
// 関連付けられたアプリに無いファイルを渡すとエラーになり、利用者には「開けない」としか
// 見えないため。空のファイルが開けば「まだ記録が無い」ことがそのまま伝わる。
// 既存のファイルは O_APPEND で開くだけなので、中身は変えない。
func appLogOpenHandler(path func() string, open func(string) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "許可されていないメソッドです", http.StatusMethodNotAllowed)
			return
		}
		p := path()
		if p == "" {
			http.Error(w, "app.log の保存先が決まっていません", http.StatusInternalServerError)
			return
		}
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			http.Error(w, "app.log を作成できません", http.StatusInternalServerError)
			return
		}
		f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			http.Error(w, "app.log を作成できません", http.StatusInternalServerError)
			return
		}
		f.Close()
		if err := open(p); err != nil {
			http.Error(w, "app.log を開けません", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}
}
