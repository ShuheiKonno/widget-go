# widget-go による変更

このディレクトリは `github.com/jchv/go-webview2`（v0.0.0-20260205173254-56598839c808）のコピーで、
ルートの `go.mod` の `replace` で使っています。元のライセンス（LICENSE）はそのままです。

## 変更点

起動時にウィンドウが一瞬ちらつく（位置や中身が決まる前に表示される）のを避けるための変更です。

- `webview.go`: `WindowOptions.Hidden` を追加。`true` のとき、`CreateWithOptions` が作成直後に行う
  `ShowWindow(SW_SHOW)` / `UpdateWindow` / `SetFocus` を行わない（呼び出し側が準備できてから表示する）。
- `webview.go`: `Hidden` で作ったウィンドウが初めて表示されたとき（`WM_SHOWWINDOW`）に、
  WebView2 を可視・実寸にし直す（`Show` / `Resize` / `NotifyParentWindowPositionChanged`）。
  これが無いと、表示後も中身が描画されず黒いままになる。
- `webview.go`: `browser` インターフェースに `Show() error` を追加（`Chromium` は実装済み）。
- `internal/w32/w32.go`: `WMShowWindow` を追加。

アップストリームを更新するときは、上記の差分を当て直すこと。
