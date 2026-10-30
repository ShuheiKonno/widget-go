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

## 拡大・縮小（ズーム）のための変更

ウィジェット全体の拡大・縮小を、WebView2 のズーム率（`ICoreWebView2Controller::ZoomFactor`）で行うための変更です。

- `pkg/edge/ICoreWebView2Controller.go`: `GetZoomFactor` / `PutZoomFactor` を追加（vtbl の枠だけあって、
  ラッパーが無かった）。`double` の引数は、Go の syscall が amd64 では先頭 4 つの引数を XMM0〜3 にも写すので、
  `uintptr(math.Float64bits(f))` で渡す。386 では届かない（release は amd64 のみ）。
- `pkg/edge/chromium.go`: `SetZoomFactor` / `ZoomFactor` を追加。
- `webview.go` / `common.go`: `WebView` インターフェースに `SetZoomFactor` / `ZoomFactor` を追加。
  `browser` インターフェースにも同じものを足した。
- `webview.go`: `WebViewOptions.DisableZoomControl` を追加。`true` のとき `IsZoomControlEnabled=false` にして、
  WebView2 既定の Ctrl+ホイール / Ctrl+プラス・マイナスによるズームを止める（メイン WebView だけに使う。
  既定のままだと、ウィンドウを変えずにページだけが拡大縮小されて、下に空白ができる）。

ズームの設定・読み取りは UI スレッドから行うこと（`Dispatch` 経由）。

アップストリームを更新するときは、上記の差分を当て直すこと。
