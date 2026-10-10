# 起動時のちらつき対策

起動時に、メインウィジェットと補助ウィンドウ（Claude / Codex の認証用 WebView2）が一瞬見える問題の記録。

## 原因

go-webview2 の `CreateWithOptions` が、ウィンドウ作成の直後に無条件で `ShowWindow(SW_SHOW)` /
`UpdateWindow` / `SetFocus` を行い、そのあとで WebView2 の初期化（`Embed`、約 250ms）に入る。
位置・テーマ・中身が決まる前にウィンドウが画面に出るため、次のものが見えていた。

- メイン: `Center: true` で画面中央に作られた白い窓（右下へ動くまで約 0.3 秒）、ページ読み込み前の白い面、
  縦に伸びる途中の白い帯
- 補助ウィンドウ（AI 使用量が ON のとき）: 500×700 の窓が約 0.5 秒ずつ（Claude / Codex）

## 対策（根本）

`third_party/go-webview2`（フォーク。差分は `WIDGET-GO-PATCHES.md`）に `WindowOptions.Hidden` を足し、
ライブラリに表示させない。

- **補助ウィンドウ**（`aiusage/authview.go`）: `Hidden: true` で作り、`moveOffscreen` で画面外へ置いてから
  `SW_SHOWNOACTIVATE`。画面外のまま表示状態にするのは、隠し窓だと WebView2 がタイマーを
  スロットルするため（既存の設計）。ログイン画面は従来どおり `moveOnscreen` で画面内へ出す。
- **メイン**（`main.go` / `startup.go`）: `Hidden: true` で作り、フレーム除去と右下への配置を済ませたあと、
  完全な透明（alpha 0）で表示状態にして WebView2 に描画させる（`beginMainWindowStealth`）。
  フロントの `/api/ready`（設定取得と初回の高さ調整のあと、`ui/index.html`）か、3 秒のフォールバック
  タイマーで、元の状態（設定の透過 / 不透明）へ戻して見せる（`showMainWindowOnce`）。
  何で見せたかは `notify.log` の `main window shown reason=ready|timeout after=…ms`。
- `WEBVIEW2_DEFAULT_BACKGROUND_COLOR` も、テーマ（`dark` / `light` / `system`=Windows のアプリテーマ）に
  合わせた色に設定している。通常は見えない保険。

## 試して外した方法（同じ失敗を繰り返さないため）

- 画面外で作る `WH_CBT` フック + クラス背景ブラシの差し替え: 白・暗・中央・1 コマのダークと、見え方が
  変わるだけで消えなかった（ライブラリが先に表示するのが原因のため、打ち消しの積み重ねになる）。
- `SW_HIDE` のまま初期化して表示だけ後にする: 表示した直後に WebView2 が黒い面を数フレーム出す。
  フォークの `WM_SHOWWINDOW` で `Show` / `Resize` を呼んでも、完全には消えなかった。→ alpha 0 で表示状態にして描画させる。
- `PrintWindow` では WebView2 の中身が取れないことがある。確認は画面録画（`ffmpeg -f gdigrab`）で行う。
  透明化中も `IsWindowVisible` は true なので、ウィンドウの追跡だけでは「見えた」証拠にならない。

## 確認の観点

- 起動直後に、白・暗い四角、画面中央の窓が出ない。完成した画面が 1 フレームで現れる（ダーク / ライト / 透過 ON）。
- AI 使用量 ON でも、補助ウィンドウが画面内に出ない。ログインが必要なときは、前面に出る。
- トレイ操作: 見せる前（約 1 秒）にトレイへ格納されても、前面化されない。

## 挙動の変化

- ウィジェットが現れるのは起動から約 1 秒後（以前は約 0.4 秒で白い窓が出て、その後に中身が描画された）。
- 起動時のフォーカスは、見せる時点の `SetFocus` のみ（以前はライブラリの `SW_SHOW` で前面化されていた）。
