# 計画: ClaudeMonitor の統合（Claude / Codex 使用量セクションの追加）— 改訂 1（T0 の実測と決定事項を反映）

- ブランチ: `ShuheiKonno/feature-integrate-claude-monitor`（ベース v1.9.3 / `0cf5399`）
- 統合元: `C:\Users\shuhe\work\code\ClaudeMonitor`（`package main`、約 6,900 行、go-webview2 は rockfish と同一コミット）
- 位置づけ: ClaudeMonitor を widget-go **の中へ**取り込む。ウィンドウ・トレイ・設定・HTTP は rockfish が持ち、ClaudeMonitor は「データ取得層」として切り出して載せる。
- この文書は実装案であり、コードはまだ変更していない。アドバイザー（別モデル）のレビューを 1 回反映済み。
- 事実の根拠は、両リポジトリのコードを読んだ範囲。実測していない値は「推定」と書く。

## 0. 決定事項と未決事項

### 決定事項

- 2026-10-02 **D9: トレイに出すプロバイダーは、設定の「AI 使用量で表示するサービス」（Claude / Codex）で選ぶ。既定は Claude 優先。** 理由は、ClaudeMonitor でこれを決めていたタブ選択（`ActiveProvider`）をタブ廃止で失うため。仕様は次のとおり。
  - 設定キーは `ai.trayProvider`（`"claude"` | `"codex"`、既定 `"claude"`）。ClaudeMonitor の `ActiveProvider` を置き換える（タブが無いので、タブの表示切替としての役割は引き継がない）。
  - 選んだ側が無効（`ClaudeEnabled` / `CodexEnabled` が false）なときは、有効な側へ自動で寄せる。ClaudeMonitor の `normalizeProviderSelection(claudeEnabled, codexEnabled, active)` に `active = trayProvider` を渡せばそのまま使える（テストも移植する）。
  - 片方しか有効でないときは、設定の選択肢を無効表示にして有効な側に固定する。両方有効なときだけ選べる。
  - 変更は保存時に即トレイへ反映する（アイコン・ツールチップを再生成）。
  - 「使用率の高い方を自動で出す」「アイコン 2 個」は採らない。

- 2026-10-02 **D1: 認証 Cookie は、旧 ClaudeMonitor のデータから最小コピーで引き継ぐ。** 初回起動時に、旧 `ClaudeMonitor\{AuthWebView2,CodexAuthWebView2}` から `EBWebView\Local State` と `Default\Network\Cookies`（+ `-journal`）だけを `widget-go\` へコピーする（コピー先に `EBWebView` が無い場合のみ）。フォルダ全体は 624MB / 13,707 ファイルあるので不可。T0-4 で、この最小コピーのまま Claude・Codex とも認証が通ることを確認済み（詳細は §7）。
- 2026-10-02 **D2: AI 使用量は既定 OFF。** 設定で ON にしたときに初めて認証 WebView2 を作り、OFF にしたら Destroy して解放する（GPU/NPU の `ShowGPU` と同じ「表示と収集を止める」流儀）。T0 で、実行中の生成（約 250ms）と Destroy による解放（約 1 秒でプロセス終了）が動くこと、両プロバイダー ON で約 +590MB（未ログイン画面での計測）かかることを確認済み。
- 2026-10-02 **D3: トレイアイコンは 1 個。表示内容を設定で 3 つから切り替える。** 「AI 使用量」「CPU 使用率」「アイコンのみ（従来の既定）」。仕様は次のとおり。
  - 設定キーは既存の `trayDisplay`（`"icon"` | `"cpu"` | `"usage"`）。設定モーダルの「タスクバー (最小化時)」の選択肢を 2 つから 3 つにする。既定は従来どおり `"icon"`。
  - `"usage"` は、AI 使用量が ON のときだけ選べる。OFF のときは選択肢を無効表示にして理由を添える（D2 の既定が OFF なので、初期状態では選べない）。
  - `"usage"` を選んだまま AI 使用量を OFF にしたら、保存値は `"usage"` のまま、実際の表示だけ `"icon"` に落とす。再び ON にすると `"usage"` が戻る。
  - `"usage"` の意匠は ClaudeMonitor 最新版の角丸四角（中央に 5h の数字、背景色が 7d のペース判定。リングは無い）。認証エラー・通信エラーのときはグレーの `?`（§6）。
  - アイコンを 2 個並べる案は採らない。
- 2026-10-02 **D4: コンパクト（幅 130px）表示は、モックアップ案のとおり。** AI セクションは、見出しの文字を出さず、アイコン + 1 行サマリ `C 42% · X 18%`。値の色は 5h 使用率（61% 以上が黄、81% 以上が赤）、未ログインは `?`、有効なプロバイダーが 1 つなら 1 項目だけ。AI 使用量が OFF のときはセクションごと非表示（§4、`docs/mockup-ai-section.html`）。
- 2026-10-02 **D5: ClaudeMonitor リポジトリは当面そのまま残す（archive しない・移行案内も書かない）。** 統合版が正式版になった時点で見直す。仕様は次のとおり。
  - ClaudeMonitor は今までどおり単体で使え、保守も続く。統合版（プレリリース）は並行して試す位置づけ。
  - 統合側の取り込みは**コピーのみ**（D1 の Cookie コピーを含め、ClaudeMonitor 側のデータは一切変更しない）。
  - 保守が二重になるため、`aiusage/` の先頭コメントか `aiusage/README.md` に、**取り込み元の ClaudeMonitor のコミット（取り込み時点の `main` HEAD。調査時点は `cc10dda`）を記録**する。claude.ai / chatgpt.com の内部 API が変わったときの修正を、両方へ取り込めるようにするため。
  - 両方を同時に起動すると、同じサービスへの取得が二重になる（読み取りのみで実害は無い）。README に書く。
  - 見直しの契機: 統合版のプレリリースが外れて正式版になったとき（D6）。
- 2026-10-02 **D6: 統合版のバージョンは `v2.0.0`。GitHub 上ではプレリリースとして公開する。** アプリ名は `system-widget` のまま。実装は T4。
  - **バージョン表記**: タグは `v2.0.0`、`winres.json` は `2.0.0.0`、変更履歴は `### 2.0.0 (日付)`（プレリリースである旨を 1 行添える）。バージョン文字列に `-beta` は付けない。そのため、トレイメニューの版数表示（`システムモニター v2.0.0`、`formatFileVersion`）は現行のまま使え、`winres.json` の bump だけで表示が追従する。表示側の改修は不要。
  - **プレリリース設定**: `release.yml` は `v*` タグで起動し、`prerelease: false` 固定（`.github/workflows/release.yml:35`）なので、このままだと正式リリースとして公開される。**プレリリース期間中は `prerelease: true` にする**。タグ名からは判定できない（`-` を含まないため）。正式版にするときは、`release.yml` を `false` に戻し、公開済みのリリースは `gh release edit v2.0.0 --prerelease=false` で外す。
  - GitHub はプレリリースを「Latest」に含めない（認識。T4 のリリース時に画面で確認する）。そのため `releases/latest` は、正式版にするまで `v1.9.3` のままになる。
  - アプリ内では、プレリリースかどうかは分からない。README の冒頭にプレリリースである旨を書く。
  - プレリリース期間中の修正は `v2.0.1` のように通常の番号で出す。**プレリリースを外す（正式版にする）条件は未定**。T3 完了後に決める。
- 2026-10-02 **D7: ログアウトは、プロセスを再起動せずに行う。** 補助 WebView を Destroy → 認証フォルダを削除 → 再作成する（WebView 内のクッキー・ストレージをすべて消せる。現行の「データフォルダ削除」と同じ効果）。第 2 候補は CDP の `Network.clearBrowserCookies`。T0 で、Destroy 後もメインのループが生き続け、ブラウザプロセスが約 1 秒で終了し、データフォルダを削除できることを確認済み。CDP は、実アカウントのセッションでも T0-4 で動作を確認済み（削除後、Claude は `claude.ai/login` に戻り `lastActiveOrg` が消え、Codex は `accessToken` が取れなくなった）。サーバー側のセッションは無効化されない（現行と同じ）。これに伴い、`--restarted` の待機処理は不要になる（§8）。実装の前提は §2.3 の 4a・4b。

- 2026-10-02 **D8: AI セクションは既存セクションの下に置き、既定は折りたたみ。** 折りたたみ時は見出しに 1 行サマリ（`C 42% · X 18%`）を出す。高さはモック上で +48px（展開時は +280〜300px）。D2 の既定は AI 使用量 OFF なので、ON にするまで AI セクション自体が出ない。
- 2026-10-02 **D10（追加要件）: AI 以外の CPU・メモリ・GPU・NPU・Wi-Fi/Network も折りたためる。** 仕様は次のとおり（実装は T2）。
  - **対象は 6 区画**: `cpu` / `memory` / `gpu` / `npu` / `network`（Wi-Fi・Ethernet とネットワークグラフの区画）/ `ai`。
  - **操作は見出し右端のシェブロン（▼、18×18px のボタン）のクリックのみ。** 見出し全体のクリックにはしない。理由: Wi-Fi/Network の区画は、区画のどこをクリックしても表裏が反転し（`ui/index.html:438`）、見出しのピーク値も単位切替でクリックされる（同 `:424`）ため、見出しクリックと衝突する。他の区画も同じ操作に揃える。シェブロンのクリックは `stopPropagation` で反転・ドラッグ開始に伝えない。
  - **折りたたむと、見出し（アイコン・タイトル・値）だけが残る。** 値は常に見える。これは現行のコンパクト表示の各区画と同じ見た目なので、本体を隠す既存の CSS 規則（`.compact .cpu-progress` など）を `.collapsed` でも効くように共通化して使う。
  - **コンパクト（幅 130px）ではシェブロンを出さず**、折りたたみ状態にかかわらず見出しのみ（現行どおり）。折りたたみ状態は保持され、フル表示に戻すと復元される。
  - **Wi-Fi/Network**: 折りたたむと表面（Wi-Fi/Ethernet 情報）に戻して本体を隠す。裏面のグラフは、折りたたみ中は描画せず、展開したときに再描画する（非表示の canvas はサイズが 0 になるため）。展開後は表面から始まる。
  - **保存**: `config.json` のトップレベルに `collapsed: {cpu, memory, gpu, npu, network, ai}`（bool）。`Config.AI` の中ではなく、既存の区画にも及ぶのでトップレベル（§3）。既定はすべて `false`（展開）、ただし `ai` は `true`（D8）。`defaultConfig()` に先に入れてから `json.Unmarshal` する（`ShowGPU` と同じ。旧 config にキーが無くても `ai: true` が残る）。
  - **API**: 既存の `/api/setoption` に `key=collapse.<name>&value=true|false` を追加する（`<name>` は上の 6 つの許可リスト。それ以外は 400）。`/api/settings` の応答に `collapsed` を足す（§5）。
  - **収集は止めない**: 折りたたんでも CPU / メモリ / GPU / NPU / ネットワークの収集は続ける（トレイのアイコンとツールチップが使うため）。収集を止めるのは、`showGPU`（GPU/NPU）と D2（AI 使用量）の ON/OFF だけ。GPU / NPU は、デバイスが無い場合に区画ごと非表示のままで、折りたたみ状態だけが保存される。
  - **高さ**: 折りたたみ・展開のたびに、既存の `adjustWindowHeight()` で高さを再測定してリサイズする。この自動調整は下端固定（既存の仕様。§2 のウィンドウ位置の記憶）なので、折りたたむと**ウィンドウは下端を保ったまま縮み、上端が下がる**。モック（既存の区画はダミーで、CPU コア 8 個・NPU あり。実機の総高とは一致しない）で測った総高は、全展開 918px ／ 既定（AI のみ折りたたみ）689px ／ 全折りたたみ 322px。画面の高さが足りない環境では、全折りたたみで 322px まで詰められる。
  - 「すべて折りたたむ／展開」のショートカットは作らない（必要になったら後で足す）。

### 未決事項

なし（D1〜D10 はすべて決定済み）。ただし、プレリリースを外す（正式版にする）条件は、T3 完了後に決める（D6）。

## 1. 前提の確認結果（コードから確認済み）

| 項目 | 結果 | 計画への影響 |
|---|---|---|
| 構成の近さ | 両者とも「Go + go-webview2 + 127.0.0.1 の HTTP + 埋め込み HTML」。`go-webview2` は両方 `v0.0.0-20260205173254-56598839c808` | 依存の衝突なし。`golang.org/x/sys` は ClaudeMonitor が 2021 年版を固定しているが、rockfish の v0.47.0 を採用する |
| 幅 | rockfish 通常 230px / ClaudeMonitor タブ表示 230px | 幅を揃えられる。ClaudeMonitor の「1 画面表示」(460px) は持ち込まない（§4） |
| 高さ | rockfish はフロントが実測して自動調整（初期 426px）。ClaudeMonitor は固定 320px | AI セクション分だけ縦に伸びる。実測が必要（§4） |
| ビルド対象 | `release.yml` は windows/amd64 のみ。`rsrc_windows_386.syso` は同梱だが CI では使われない | ClaudeMonitor の 64bit 前提コード（`uintptr(uint32(X)) \| (uintptr(uint32(Y)) << 32)`、`SetWindowLongPtrW`）は問題にならない。386 を将来対象にする場合のみ書き直す |
| CGO | `release.yml` は `CGO_ENABLED=0`。ClaudeMonitor は純 syscall | 問題なし |
| トレイの通知 API | rockfish の `NOTIFYICONDATAW`（`main.go:177`）は `SzInfo` / `SzInfoTitle` / `DwInfoFlags` を既に持つ | バルーン通知のために構造体を拡張する必要はない。`NIF_INFO` 定数と送信関数の追加だけ |
| Explorer 再起動 | rockfish も `TaskbarCreated` を処理している（`tray.go:464`） | ClaudeMonitor 側のトレイ処理は丸ごと不要 |
| ローカル API の防御 | rockfish: Host 検証 + `X-Widget-Token` + CSP（`server.go:38`）。ClaudeMonitor: `http.Server{Handler: mux}` のみで検証なし（`api.go:239`） | 統合すると `/api/close`・`/api/relogin` などが rockfish の防御配下に入る（副次的な改善）。代わりに ClaudeMonitor の UI は `apiFetch`（トークン付与）経由に直す必要がある |
| トレイ ツールチップ | `SzTip` は 128 文字。現状 `CPU \| Mem \| 回線 \| GPU \| NPU` で既に長い | AI の行を足すと切れる。§6 で優先順位を決める |
| ログアウト | ClaudeMonitor は WebView2 のデータフォルダのロックを避けるため、`--restarted` 付きで自分を再起動し、新プロセスが認証データを削除（`auth_webview.go:373`、`main.go:327`） | 統合版ではこの方式を採らない。D7 で、再起動せずに Destroy → 削除 → 再作成する方式に決定（§2.3 の 4b、§8） |
| 負荷 | `plan-gpu-npu.md` の実測で WebView2 ≈ 407MB / Go ホスト 35.7MB。ClaudeMonitor は WebView2 を **3 個**（メイン UI + Claude 認証 + Codex 認証）持つ | 統合すると、ClaudeMonitor 単体運用に比べメイン UI 分が 1 個減る。**T0 の実測（未ログイン画面。Private Working Set）**: WebView2 のインスタンス 1 個の固定費は約 +83MB、claude.ai のログイン画面を開くと約 +430MB、Codex を足すと +約 160MB（合計 約 +590MB）。1 つのフォルダを共有させると約 −85MB。ログイン後の画面は別の値になりうる（`docs/t0-report.md` §3） |

## 2. 構造の方針

### 2.1 衝突する識別子

両者とも `package main` で、そのままファイルを持ち込むとコンパイルできない。確認できた衝突:
`Config` / `loadConfig` / `mutateConfig` / `snapshotConfig`、`windowHandle`（rockfish は `atomic.Uintptr`、ClaudeMonitor は素の `uintptr`）、`setTopmost`、`setTransparent`、`startTray`、`startCollector`、`startServer`、`ensureSingleInstance`、`moveToBottomRight`、`findMainWindow`、`setupMainWindow`、`getWindowDpiScale`、`dragMu`、`POINT` / `RECT` / `MONITORINFO`、`WM_CLOSE`、`GWL_EXSTYLE`、`WS_EX_LAYERED`、`ALPHA_*`、`SW_SHOW`、`MONITOR_DEFAULTTONEAREST`。

### 2.2 方針: 取得層をサブパッケージ `aiusage/` に切り出す

```
aiusage/
├── host.go            # Host インターフェース（§2.3）
├── settings.go        # Settings 構造体 + normalize*（clampPollSeconds 等）
├── usage.go           # スナップショット・コレクター（旧 usage.go）
├── claude_auth.go     # claude.ai 認証 WebView（旧 auth_webview.go）
├── codex_auth.go      # chatgpt.com 認証 WebView（旧 codex_auth_webview.go）
├── codex_usage.go
├── status.go          # status.claude.com / status.openai.com（旧 status.go, codex_status.go）
├── notify.go          # 閾値・障害の通知判定（旧 notify.go。表示は Host.Balloon に委譲）
├── handlers.go        # /api/ai/* を mux に登録
└── *_test.go          # 旧テストを移植
```

- ウィンドウ・トレイ・単一起動・メイン UI の HTML・アイコン生成（`tray.go` / `single_instance.go` / `html.go` / `main.go` / `config.go` / `cmd/genicon`）は**持ち込まず**、rockfish 側の同等機能に寄せる。
- すべての Win32 宣言は rockfish 側と重複しないよう、`aiusage` 内では必要最小限を `golang.org/x/sys/windows` か自前の小さな宣言で持つ。
- `windowHandle` は rockfish の atomic 版に統一する（ClaudeMonitor の素の読み書きは `go test -race` の対象にしないが、将来の競合の元になる）。

### 2.3 Host インターフェース（スレッド制約を明示する）

```go
type Host interface {
    Dispatch(f func())                                  // メイン WebView の UI スレッドで実行
    Balloon(title, msg string, flag uint32)             // トレイ経由の通知
    OnSnapshotChanged()                                 // トレイ再描画の合図
    Settings() Settings                                 // 現在の設定（コピー）
    MutateSettings(f func(*Settings))                   // 設定を変更して保存
    DataRoot() string                                   // %LOCALAPPDATA%\widget-go
}
```

**守るべき制約（ClaudeMonitor の既存コードが前提にしているもの）:**

1. 認証 WebView2 の生成は、`runtime.LockOSThread()` 済みのメイン goroutine で行う。rockfish の `main()` では `windowHandle.Store(...)`（`main.go:1020`）の後、`w.Run()`（`main.go:1047`）の前が挿入位置。
2. `Eval` / `Navigate` / Win32 の呼び出しは、**メイン WebView の** `Dispatch` を通す。go-webview2 の `Run` は自分のキューしか処理しないため、認証 WebView 側の `Dispatch` は実行されない（`auth_webview.go:86` のコメント）。
3. rockfish の `main()` では WebView が局所変数 `w` なので、パッケージ変数 `mainWebViewInst` に昇格させて `Host.Dispatch` の実体にする。
4. Bind コールバック（`__postUsageData` など）は UI スレッドで呼ばれる。ブロックする処理を直接書かず、`go` で逃がす。
4a. **実行中に補助 WebView を生成してよいが、直後に空の `Dispatch` を 1 回積む。** T0-1 で、`Run()` 開始後に `Dispatch` の内側で生成できること（約 250ms）、Bind コールバックも届くことを確認した。ただし生成中（`Embed` の入れ子ループの間）に積まれたメイン WebView の `Dispatch` は、次に誰かが `Dispatch` を積むまで止まる（計測では 433ms 遅れて、補助 WebView の Bind 応答が出た時点で実行された）。生成の完了後に `mainWebViewInst.Dispatch(func() {})` を呼んで掃き出す（この対策そのものは未検証）。
4b. **補助 WebView の `Destroy()` は、サブクラスで `WM_DESTROY` を握りつぶしてから呼ぶ。** go-webview2 は `WM_DESTROY` で `PostQuitMessage` を呼び、アプリ全体のメッセージループを終了させる（`webview.go:240-243`）。握りつぶしてから `Destroy()` すると、メインのループは生き続け、その補助 WebView のブラウザプロセス一式が約 1 秒で終了し、データフォルダも削除できる（T0-3）。ClaudeMonitor の `authWndProc`（`WM_CLOSE` を握りつぶす）を `WM_DESTROY` にも広げる形になる。複数の補助 WebView が 1 つのデータフォルダを共有している場合は、1 つを壊してもプロセスは残る。
5. **UI スレッドから `refreshUsage` / `refreshAllUsage` / `refreshCodexUsage` を同期で呼ばない。** これらは `Dispatch(Eval)` した後、Bind コールバックの完了をチャネルで待つ（`usage.go:96`）。UI スレッド上（`Dispatch` したクロージャ、将来のトレイメニューのハンドラなど）から呼ぶと、Eval が実行されないままデッドロックする。ClaudeMonitor は呼び出しをすべて `go func() { ... }()` で包んでいる（`tray.go:249`）。この規則を `Host` のコメントとコードレビューの観点に残す。

## 3. 設定（`config.json`）

- ClaudeMonitor 固有の項目は `Config.AI`（`aiusage.Settings`）の 1 か所にまとめる。キー名の衝突（`window.*` / `topmost` / `transparent`）は、ClaudeMonitor 側を**捨てて** rockfish 側を使う。
- `Settings` に入れるもの: `Enabled`（D2）、`ClaudeEnabled` / `CodexEnabled` / `TrayProvider`（D9。旧 `ActiveProvider` の後継）、`ResetTimeFormat`、`TraySplitDays`、`UsagePollSeconds` / `StatusPollSeconds`、`NotifyUsage` / `NotifyOverage` / `NotifyStatus`、`OverageTipFormat`、および再起動をまたぐ通知済みフラグ（`Notified5h60` 等）。
- `LayoutMode`（tabs/overview）は持ち込まない（§4）。
- 区画の折りたたみ状態（D10）は、AI 以外の既存の区画にも及ぶので、`Config.AI` ではなく **`Config.Collapsed`（トップレベル、JSON は `collapsed`）** に置く。型は構造体 `{CPU, Memory, GPU, NPU, Network, AI bool}`。`defaultConfig()` で `AI = true`、他は `false`。
- 既定値は `defaultConfig()` に先に入れてから `json.Unmarshal` する（`ShowGPU` と同じ。旧 config にキーが無くても既定が残る）。
- `clampPollSeconds` などの正規化は `loadConfig` の後段で呼ぶ。ClaudeMonitor の `mutateConfig` は rockfish の `writeConfigLocked`（tmp → rename）と重複するので捨てる。
- 旧 ClaudeMonitor の `config.json` は読まない（ウィンドウ位置以外に移す価値のある設定が少ない）。通知設定だけは移行したい場合は T4 で検討。

## 4. UI

- ClaudeMonitor の「タブ」と「1 画面表示（460px）」は持ち込まない。幅 230 / 130 の 2 段階という rockfish の前提と合わないため。
- 代わりに、**プロバイダーごとの行**を 1 つのセクション（`aiSection`）にする。
  - 通常表示: `Claude` / `Codex` の見出し + 5h バー + 7d バー + リセット時刻 + プラン名。認証が必要なときはバナー（ログイン）、障害時はステータスの印。
  - コンパクト表示（D4）: `C 42% · X 18%` の 1 行。折り返さない。
  - 無効なプロバイダー、または D2 が OFF のときはセクションごと非表示（`gpuSection` と同じ `display:none` 方式）。
- 設定モーダル（`ui/index.html:203`）に追加: AI セクション ON/OFF、Claude/Codex のチェック、ポーリング間隔、通知 3 種、リセット時刻の形式。
- ClaudeMonitor の `html.go` の JS/CSS は、rockfish の `ui/index.html` にある nonce 付き `<script>` / `<style>` の中へ移す。別ページにすると CSP（`default-src 'none'`）で動かない。
- すべての `fetch` は `apiFetch`（`X-Widget-Token` 付与）に置き換える。
- 縦幅: 現状 426px に AI セクションが加わる。`docs/mockup-ai-section.html`（ブラウザで測ったダミー値。実機の WebView2 とは誤差あり）では、**両プロバイダー展開で +280〜300px、折りたたみで +48px、コンパクトで +32px**。当初の推定（1 プロバイダー 80〜110px）より大きく、1 プロバイダーあたり約 135〜150px だった（見出し + バー 2 本 + リセット時刻 2 行が主因）。そのため、セクションの折りたたみ（既存の自動高さ調整 `adjustWindowHeight` に乗る）は必須とし、折りたたみ時は見出しに 1 行サマリ（`C 42% · X 18%`）を出す。展開を既定にするか折りたたみを既定にするかは D8 で決める。
- コンパクト（130px）は、見出しの文字（「AI」）を出さず、アイコン + サマリのみにする。「AI」を残すと 3 桁（`C 100% · X 100%`）でサマリが溢れる（モックで確認）。
- **区画の折りたたみ（D10）**: 6 区画（`cpu` / `memory` / `gpu` / `npu` / `network` / `ai`）すべてに、見出し右端のシェブロン（▼）を付ける。操作はシェブロンのクリックのみ（Wi-Fi/Network は区画クリックで表裏が反転するため、見出しクリックにはしない）。折りたたむと見出しだけが残り、値は見える。コンパクトではシェブロンを出さない。状態は `config.json` に保存し、`adjustWindowHeight()` で高さを詰め直す（下端固定なので、折りたたむと上端が下がる）。AI は既定で折りたたみ（D8）。仕様の全文は §0 の D10。見た目は `docs/mockup-ai-section.html` で確認できる。
- `ui-reviewer` エージェントの「256px 制約」ルールは ClaudeMonitor 専用なので持ち込まない。

## 5. HTTP API

rockfish と ClaudeMonitor で同名のエンドポイントがある: `/api/settings`、`/api/setoption`、`/api/close`、`/api/dragstart`、`/api/dragmove`、`/api/persistwindow`。ウィンドウ操作系は rockfish 側を使い、ClaudeMonitor 側は捨てる。AI 固有のものは `/api/ai/` 配下に置く。

| 新パス | 元 | 備考 |
|---|---|---|
| `GET /api/ai/usage` | `/api/usage` | `{claude, codex}` のスナップショット |
| `GET /api/ai/status` | `/api/status` | |
| `POST /api/ai/refresh` | `/api/refresh` | 同期取得 |
| `POST /api/ai/relogin?provider=` | `/api/relogin` | ログイン画面を画面内に出す |
| `POST /api/ai/logout?provider=` | （新規。旧: プロセス再起動） | D7: Destroy → 認証フォルダ削除 → 再作成。再起動しない |
| `POST /api/ai/provider` | `/api/provider` | 役割を変更: タブ切替ではなく `trayProvider` の更新（D9）。無効なプロバイダーは 400。保存後にトレイを再描画 |
| `GET /api/ai/settings` / `POST /api/ai/setoption` | `/api/settings` / `/api/setoption` の AI 分 | 既存の `/api/settings` に混ぜるか別にするかは T2 で決める |
| `POST /api/ai/open-usage` / `open-status` | 同名（旧は GET） | 外部ブラウザで開く。`openExternalURL` は許可リスト方式にする（任意 URL を開かない） |

**既存 API の拡張（D10）**: `/api/setoption` に `key=collapse.<name>&value=true|false` を追加（`<name>` は `cpu` / `memory` / `gpu` / `npu` / `network` / `ai` の許可リスト。それ以外のキー・値は 400）。`/api/settings` の応答に `"collapsed": {cpu, memory, gpu, npu, network, ai}` を足す。

すべて `securityMiddleware` の内側に置く（`startServer` から `aiusage.RegisterHandlers(mux)` を呼ぶ）。

## 6. トレイ

- アイコンは 1 個（D3、決定済み）。`TrayDisplay` を `"icon" | "cpu"` から `"icon" | "cpu" | "usage"` に拡張する（設定モーダルの選択肢は 2 つから 3 つ。既定は従来どおり `"icon"`）。`trayDisplayMode` の定数（`TRAY_DISPLAY_*`）にも追加。`"usage"` は AI 使用量が ON のときだけ選べる。`"usage"` を選んだまま AI 使用量を OFF にしたときは、保存値を変えず、実際の表示だけ `"icon"` に落とす（`effectiveTrayDisplay()` のような純粋関数にして、テストを書く）。
- **意匠は ClaudeMonitor 最新版のものをそのまま使う。** 角丸四角（半径 3.5px、縁 1.2px）に、中央へ 5h 使用率の数字、背景色で 7d を表す（緑 `#7BB07B` / 琥珀 `#E8A853` / 赤 `#D06B5B`）。**リングは無い**（コミット `c66799a` で円形から角丸四角に変更済み）。ClaudeMonitor の README と `CLAUDE.md`、`cmd/genicon` の冒頭コメントは「円形・リング」のままで古い。この計画書の初版もそれを根拠にしていて誤りだったので、描画コード（`tray.go` の `drawTrayIconImage`）とプレビュー画像（`assets/tray-preview.png`）を正とする。認証エラー時はグレーの角丸四角に `?`。
- 背景色の判定は固定の 60/80% ではなく**ペース判定**（`trayPaceThresholds`）。7d 窓の経過割合に応じて黄・赤の閾値を縮める（リセット直後のちらつき防止に最小 12 時間の床あり、`traySplitDays` = 7/5/0 で基準日数を変更）。`aiusage` に純粋関数ごと移し、`tray_test.go` のテストも移植する。
- `"usage"` のとき、`runTrayTicker`（2 秒周期、`tray.go:625`）が上のアイコンに差し替える。トレイ用のスレッドとメッセージループは rockfish のものを使い、ClaudeMonitor のものは持ち込まない。画像生成は `aiusage` の純粋関数（`image.RGBA` を返す）に置き、HICON への変換（`imageToHICON`、約 25 行）だけ rockfish 側に持つ。HICON の所有権は既存の `currentCpuIcon` と同じ扱いにし、(5h の値, 色, プロバイダー, エラー有無) が変わったときだけ再生成する（2 秒ごとの作り直しで GDI ハンドルを無駄に消費しない）。
- 数字の描画は ClaudeMonitor のまま `golang.org/x/image`（`basicfont` 7x13、100% のみ 3x5 の専用詰め描画）を使う。当初は rockfish の `drawDigits`（5x7）に置き換える案だったが取り下げる。ClaudeMonitor 側のコメントに「5/6 など紛らわしい数字の判読性を優先した意匠」とあり、見た目を変えないのが統合の前提のため。代償として `go.mod` に `golang.org/x/image`（ClaudeMonitor は v0.39.0）が増える。純 Go なので `CGO_ENABLED=0` でも問題ない。
- **トレイに出るのは 1 プロバイダーだけ。** ClaudeMonitor ではタブの選択（`ActiveProvider`）がこれを決めていた（`updateTrayFromSnapshot`、`tray.go:420`）。タブを廃止するとその決め手が無くなるので、設定の「AI 使用量で表示するサービス」（`ai.trayProvider`、既定 Claude）で決める（D9、決定済み）。右クリックメニューの「ログイン…」は、トレイの表示対象に限らず、認証が必要なプロバイダーごとに出す。ツールチップは ClaudeMonitor の文面（`Claude モニター` / `5時間: 42% / 7日: 18%`、追加利用枠があれば `/ Eu: $3.20`）を先頭に置く。
- ツールチップ（128 文字）: `usage` モードのときは AI の行を先頭にし、CPU/Mem/回線/GPU/NPU は末尾から削る。他のモードでは現状を変えない。純粋関数 `buildTrayTooltip` にテストを足す。
- 右クリックメニュー: 現状は「バージョン + 終了」。AI が有効なときだけ「AI 使用量を更新」と、認証が必要なプロバイダーの「ログイン…」を足す。
- バルーン通知: `Balloon(title, msg, flag)` を `NIF_INFO` で実装し、`aiusage/notify.go` の判定（60% / 80% の閾値、障害の新規検出、起動直後は基準値扱いで抑制）をそのまま使う。

## 7. データ保存先と認証 Cookie

```
%LOCALAPPDATA%\widget-go\
├── config.json
├── WebView2\             # メイン UI（既存）
├── AuthWebView2\         # claude.ai の Cookie（新規）
├── CodexAuthWebView2\    # chatgpt.com の Cookie（新規）
└── notify.log            # 通知の診断ログ（新規。ClaudeMonitor の notifyLog を踏襲）
```

- `notify.log` は残す。バルーンが出ないときの切り分け（`balloon skip trayAdded=false` など）に使えるため。`debug.log`（ClaudeMonitor の「一時デバッグログ」、`auth_webview.go:94`）は持ち込まない。
- 両ログとも肥大化の上限は ClaudeMonitor 側に無い。`notify.log` は T1 でサイズ上限（例: 起動時に 1MB 超なら切り詰め）を決める。

- D1 の (a): 初回起動で、旧 `%LOCALAPPDATA%\ClaudeMonitor\{AuthWebView2,CodexAuthWebView2}` が存在し、コピー先に `EBWebView` が無ければ、**次の 3 ファイルだけ**をコピーする（フォルダ全体は 624MB / 13,707 ファイルあるので不可）。コピー元は触らない（ClaudeMonitor を並行して使えるように）。
  - `EBWebView\Local State`（約 15KB。Cookie を暗号化する鍵 `os_crypt.encrypted_key` を含む）
  - `EBWebView\Default\Network\Cookies`（Claude 約 41KB、Codex 約 74KB）
  - `EBWebView\Default\Network\Cookies-journal`（存在する場合のみ。実測では 0 バイト）
- **T0-4 の実測で、この最小コピーのまま認証が通ることを確認した**（Claude: `usageStatus=200`、Codex: `sessionStatus=200` / `hasToken=true` / `usageStatus=200`）。同じ PC・同じ Windows ユーザー内での検証。Cookie は DPAPI で暗号化されているため、別ユーザー・別 PC へのコピーは通らない想定（統合の用途では問題にならない。未検証）。
- 注意: コピー時に旧 ClaudeMonitor が起動中だと Cookie DB がロックされて失敗しうる（T0-4 は ClaudeMonitor 停止中に実施）。失敗したら黙って未ログイン状態で続行し、バナーで案内する。コピー後は、ClaudeMonitor 側のセッションと独立して扱われる（片方でログアウトしても、もう片方のブラウザ側の Cookie は消えない。サーバー側のセッションは無効化されない）。

## 8. 単一起動・ビルド・リリース

- 単一起動は rockfish のミューテックス（`Global\widget-go-single-instance-mutex`）のみ。ClaudeMonitor の `single_instance.go`（応答なしインスタンスの強制終了ダイアログ付き）は持ち込まない。必要になったら別タスクにする。
- **`--restarted` の処理は実装しない（D7）。** ClaudeMonitor の `logoutUser` は `exe --restarted` を起動して自分は終了し、新プロセス側が 600ms 待って認証フォルダを消していた（`auth_webview.go:373`、`main.go:325-362`）。統合版は再起動せずにログアウトする（§2.3 の 4b。Destroy → 削除 → 再作成）ので、この処理も、ミューテックス競合（`ensureSingleInstance` が旧プロセスに握られて新プロセスが黙って終了する問題）も発生しない。万一、再起動方式に戻す場合は、待機 → 削除 → 通常起動の順を `ensureSingleInstance` より前に入れ、終了時のトレイ掃除は rockfish の `cleanupTray()` を使うこと。
- ClaudeMonitor を同時に起動している間は、両者が同じ claude.ai / chatgpt.com へアクセスするため、取得が二重になる。実害は無い（読み取りのみ）が、README に書く。
- バージョン情報: rockfish は `winres/winres.json`、ClaudeMonitor は `versioninfo.json` + `goversioninfo`。ClaudeMonitor 側の `release` スキルと PostToolUse フックは**持ち込まない**。
- CI: `go vet` / `go test` / `go test -race` / `golangci-lint`（`govet`・`ineffassign`・`staticcheck`・`unused`・`misspell`）を通す。`aiusage` の素の `uintptr` 共有変数が `-race` と `staticcheck` に引っかからないかを T1 で確認する。
- `go.mod`: ClaudeMonitor の依存は go-webview2 / go-winloader / x/sys / x/image のみ。go-webview2 と x/sys は rockfish に既にあるので、**追加は `golang.org/x/image` だけ**（トレイ数字の描画用、§6）。

## 9. テスト

- ClaudeMonitor のテスト（`notify_test.go` 498 行、`status_test.go`、`codex_*_test.go`、`config_test.go`、`tray_test.go`）を `aiusage/` に移植する。通知の閾値判定と障害判定は純粋関数に近く、そのまま価値がある。
- 新規: `Settings` の正規化（旧 config から欠損キーで起動しても既定が残る）、`buildTrayTooltip` の長さ上限、cookie コピーの「コピー先が既にあれば何もしない」。
- GUI（WebView2 + ログイン）が必要な部分は自動化しない。`/api/ai/usage` を token 付きで叩く手順を T1 の受け入れ条件にする。

## 10. 実施順（各段階でビルドとテストが通ること）

### T0: 事前検証（スパイク。コードは捨ててよい）

結果は `docs/t0-report.md`。

1. 実行中（`w.Run()` 開始後）に、`Dispatch` の中から認証 WebView2 を新規生成できるか。**→ できる**（§2.3 の 4a。D2 の「ON にした時に作る」はそのまま採用）。
2. 認証 WebView2 を 0 / 1 / 2 個作ったときの実メモリ（Private Working Set の合計）。**→ 実測済み**（未ログイン画面で、Claude +約 430MB、両方で +約 590MB、フォルダ共有で約 −85MB。D2 を既定 OFF とする根拠）。
3. 再起動せずにログアウト（Cookie 削除）できる方法があるか。**→ ある**（Destroy → フォルダ削除 → 再作成、または CDP でクッキー削除。D7 を「再起動なし」に変更）。
4. 旧データフォルダのコピーで認証が引き継がれるか（§7）。**→ 引き継がれる**（`Local State` + `Cookies` の最小コピーで Claude・Codex とも認証が通った。D1 は (a) 最小コピーで確定できる）。

### T1: `aiusage/` の切り出し（UI なし）— 2026-10-02 実装済み（認証済みの実データでの確認を除く）

- 旧ファイルを移し、`package aiusage` と `Host` を導入。`main.go` に `mainWebViewInst` と `aiusage.Start(host)` を挿入。`/api/ai/*` を登録。
- 受け入れ条件: `go vet` / `go test` / `go test -race` / lint が通る。AI を ON にして起動し、`curl -H "X-Widget-Token: …" http://127.0.0.1:<port>/api/ai/usage` で Claude の値が返る。

**T1 の実施結果（2026-10-02）**

- 作ったもの: `aiusage/`（`host.go` / `settings.go` / `authview.go` / `usage.go` / `claude.go` / `codex.go` / `codex_usage.go` / `status.go` / `codex_status.go` / `notify.go` / `handlers.go` / `win.go` / `scripts.go`）と、本体側の `aihost.go`（Host 実装）。`config.go` に `AI aiusage.Settings`、`server.go` に `aiusage.RegisterHandlers(mux)`、`main.go` に `setMainWebView(w)` と `aiusage.Start(aiHost{})`、`tray.go` に `showBalloon`（`NIF_INFO`）を追加。取り込み元は ClaudeMonitor `cc10dda`。
- 構造の整理: Claude 用と Codex 用で重複していた補助 WebView の処理（約 830 行の 2 ファイル）を、`authView` 1 つ（生成・破棄・ログイン画面・取得・ログアウト）に統合した。取得スクリプト・状態判定・通知判定・変換関数は、内容を変えずに移植した。
- 取り込まなかったもの: `__debugLog`（一時デバッグ用）、`tray.go` の描画（T3）、`single_instance.go`、メイン UI の HTML、`--restarted` の処理（D7 により不要）、`applyCodexUsage`（未使用の ID トークン経路）。
- `notify.log` は DataRoot に置き、起動時に 1MiB を超えていたら `notify.log.1` へ退避する。
- 自動テスト: `go vet`・`go test`（全パッケージ）・`golangci-lint`（0 件）が通る。ClaudeMonitor のテスト（通知・状態・変換）を移植し、設定の矯正、ハンドラ（メソッド制限・409・400・部分更新）、ON/OFF での WebView 生成回数、ログアウトでのフォルダ削除と再作成、`config.json` の旧形式の読み込み、`copyUTF16` を追加した。`go test -race` はこの PC に gcc が無く実行していない（CI で実行される）。
- **実機の確認（独立した `LOCALAPPDATA`、認証データ無しの空のプロファイルで、24 項目すべて成功）**: ON にすると実行中の `Dispatch` 内で補助 WebView を生成でき（T0-1 の経路。Bind コールバックが届き `needs_login` が報告される）、Codex も同様。ログアウトはプロセスを再起動せずに Destroy → フォルダ削除 → 再作成が動き、未ログインに戻る。OFF にすると約 0.9 秒でブラウザプロセスが全て終了し、メインの UI は生きたまま、スナップショットは `init` に戻る。再度 ON/OFF も動く。トークン無しの API は 403、OFF 中の `refresh` は 409、メソッド違いは 405、不正なプロバイダーや任意の URL の `open-usage` は 400。
- **認証済みの実データでの確認（2026-10-02、独立した `LOCALAPPDATA` で利用者がアプリ内でログインして実施。出力はステータスと使用率・プラン名のみ）**:
  - Claude: `authState=ok`、Claude Pro、5 時間・7 日の使用率とリセット時刻、追加使用量（上限あり）が取れた。
  - Codex: `authState=ok`、ChatGPT Plus、5 時間・7 日の使用率とリセット時刻、クレジット残高が取れた。
  - ログイン済みのプロファイルでアプリを再起動し、AI を ON のまま起動すると、起動の約 2〜4 秒で `ok` になる（起動時の生成経路と、認証の永続化）。
  - ログイン済みのセッションで `logout` を実行すると、約 6 秒で `needs_login` に戻る。プロセスは再起動せず、アプリは生きたまま（D7 を実セッションで確認）。
- **未解決の現象（再現せず）**: ログイン直後の最初の再起動で、補助 WebView は生成されたのに、ページ内からの報告が届かず `init` のまま、`refresh` は「fetch timeout」になった（24 秒と 40 秒の 2 回）。以降、同じ手順を 3 回試しても再現しない（強制終了の 3 秒後の再起動も、2 秒で `ok`）。空のプロファイルでは起動経路でも 4 秒で `needs_login` が報告される。原因は特定できていない（検証での強制終了の繰り返し、claude.ai 側の一時的な遅延などが候補）。対策の候補は、取得がタイムアウトしたときに補助 WebView のページを再読み込みして回復させる処理だが、再現しないため効果を確かめられない。T2 以降で、再現した場合に備えて `refresh` のタイムアウトを `notify.log` に残す。
- **既知の問題（ClaudeMonitor から引き継いだもの）**: 障害監視の対象名が古い。`status.openai.com` の現在の構成要素（25 件）に、`Codex Web` と `CLI` が存在せず（`Codex in ChatGPT Desktop` だけが一致）、この 2 つは常に `unknown` になる。`codexStatusTargets`（`aiusage/codex_status.go`、ClaudeMonitor `codex_status.go:17` と同じ）を、現在のステータスページに合わせて見直す必要がある。何を監視対象にするか（`Codex in ChatGPT Desktop` だけにする、`ChatGPT Work` や `Agent` を加えるなど）は利用者の判断。T2 でステータス表示を作るときに決める。
- **未確認**: (1) 生成直後の空 `Dispatch`（4a）が止まりを解消するか（Bind が届いたことは確認したが、止まりの有無は測っていない）。(2) ログイン画面を × で閉じたときにアプリが終了しないこと、初回生成時の補助ウィンドウのちらつき（利用者の目視が必要）。(3) バルーン通知の実表示（`showBalloon`。T3 でも確認する）。


### T2: UI と設定

- `aiSection` と設定モーダル、コンパクト表示、折りたたみ。縦幅の実測値を §4 に反映。
- D9 の「AI 使用量で表示するサービス」（片方のみ有効なときは固定表示）と `/api/ai/provider`。`normalizeProviderSelection` を `trayProvider` で使うテストを移植。
- D10 の区画の折りたたみ: 6 区画のシェブロン、`.collapsed` の CSS（コンパクト用の隠し規則を共通化）、`Config.Collapsed`、`/api/setoption` の `collapse.<name>`、`/api/settings` への `collapsed`、折りたたみ・展開時の `adjustWindowHeight()`、Wi-Fi/Network の表面への復帰とグラフの再描画。
- テスト: `defaultConfig` と、`collapsed` キーが無い旧 config を読んだときに `ai == true`・他は `false` になること。`/api/setoption` の許可リスト（不正な `<name>` と値は 400）。折りたたみ状態の `/api/settings` への反映。


**T2 の実施結果（2026-10-02）**

- 実装: `ui/index.html`（6 区画のシェブロンと折りたたみ、AI 区画、設定モーダルの AI 項目、コンパクト表示）、`config.go`（`Config.Collapsed`、`CollapsedConfig`、`setCollapsed`）、`server.go`（`/api/setoption` の `collapse.<name>`、`/api/settings` の `collapsed`）。テスト: `collapsed_test.go`（旧 config の既定、部分指定、AI の展開の保持、6 区画の保存、不正な区画名・値の拒否と設定が変わらないこと）。`go vet`・`go test`・`golangci-lint`（0 件）が通る。
- 折りたたみは見出し右端のシェブロンのクリックだけで行う。シェブロンのクリックは Wi-Fi の反転とドラッグ開始に伝えない（`stopPropagation` と、`mousedown` の除外）。折りたたむと見出しだけが残り、値は見える。コンパクトではシェブロンを出さない。AI は既定で折りたたみで、見出しに `C 42% · X 18%` の 1 行サマリが出る（未ログインは `?`、取得中は `…`）。
- AI 区画は、使用量の取得（短時間は 3 秒、安定したら 15 秒ごと）と、障害状況（`statusPollSeconds` ごと）を `/api/ai/*` から読む。**サービス障害の表示は、構成要素の名前ではなく、進行中のインシデントと、障害を示す構成要素の有無で判断する**。`status.openai.com` に `Codex Web` と `CLI` が無い問題（T1 の欄）の影響を受けず、監視対象の名前の見直しは、表示には必要ない（`codexStatusTargets` 自体は、通知の判定でまだ使われる）。
- サービス側（外部）由来の文字列（インシデント名、エラー文）は、必ずエスケープして埋め込む。HTML を含む文字列を流し込んでも、タグとして解釈されず、そのまま表示されることを確認した。
- 設定モーダルに、AI 使用量の ON/OFF、Claude / Codex のチェック（両方 OFF にしても最低 1 つは残る）、5 時間枠のリセット表示（日時 / 残り時間）、トレイに出すサービス（D9。両方有効なときだけ選べる）、取得間隔・障害監視の間隔、通知 3 種、ログイン済みのサービスのログアウトを追加した。
- **画面の確認（実際のアプリを独立した `LOCALAPPDATA` で起動し、Edge のヘッドレスを DevTools Protocol で操作）**: 既定の表示（AI は折りたたみ、見出しにサマリ）、AI の展開（Claude は未ログインのバナーとログインボタン、Codex は使用率・リセット時刻・クレジット残高）、全折りたたみ、コンパクト、設定モーダルを撮って確認した。動作は、Wi-Fi の区画クリックで反転しシェブロンでは反転しないこと、折りたたみで表面へ戻ること、折りたたみ状態が `config.json` と `/api/settings` に保存されリロードで復元されること、AI の ON/OFF が画面とサーバーの設定に反映されること、両方 OFF にしても 1 つ残ること、取得間隔の範囲外が矯正されること、エスケープ。
- **未確認**: (1) 実際のウィジェットのウィンドウで、折りたたみ・展開に合わせて高さが詰まること（ヘッドレスでは `adjustWindowHeight` の結果のウィンドウサイズは見えない。下端固定で上端が動く想定）。(2) ライトテーマでの AI 区画の見た目。(3) 設定モーダルが、実際の（高さが自動調整された）ウィンドウでスクロールして使えること。(4) 画面からのログイン（`ログイン` ボタン）と、ログイン画面を × で閉じたときの動作。(5) `trayDisplay = "usage"` の選択肢は T3 で追加する（モーダルの「タスクバー」の項目は未変更）。
### T3: トレイと通知

- `trayDisplay = "usage"`（角丸四角の意匠・ペース判定・表示プロバイダーの設定 D9）、ツールチップ、メニュー項目、`Balloon`。`TestDumpTrayPreview`（`DUMP_TRAY_PREVIEW=1`）も移植し、`assets/tray-preview.png` 相当の画像で 5/6 と 100% の判読性を実機の 16px で確認する。



**T3 の実施結果（2026-10-02）**

- 実装: `aiusage/trayicon.go`（ClaudeMonitor の角丸四角の描画、ペース判定、エラーアイコンを内容を変えずに移植。依存は `golang.org/x/image` v0.39.0 を追加）、`aiusage/tray.go`（`TrayInfo`: プロバイダー選択・アイコンのキー・画像・ツールチップ、`RefreshNow` / `ShowLogin` / `LoginNeeded` / `TestNotify`）、`trayusage.go`（`effectiveTrayDisplay`、`buildUsageTooltip`、`imageToHICON`、右クリックメニューの AI 項目、`kickTray`）。`tray.go` の `runTrayTicker` に AI 使用量のアイコンと合図の受信、メニューのコマンド（更新・ログイン・通知テスト）、`showBalloon` の結果の返却を追加。`trayDisplay` は `"icon" | "cpu" | "usage"`、設定モーダルのタスクバーの選択肢に「AI 使用量」を追加（AI が OFF のときは選べない）。
- 仕様どおりの動作: トレイは 1 個。`"usage"` は AI が ON のときだけ有効で、OFF のときは保存値は `"usage"` のまま、実際の表示だけアイコンのみに落ちる（`effectiveTrayDisplay`）。アイコンは画像が変わるとき（数字・色・プロバイダー・エラー）だけ作り直す。ツールチップは AI の行を先頭に、widget-go の項目を 127 文字（UTF-16 単位）に収まる分だけ足し、収まらない項目は末尾から落とす（途中で切らない）。スナップショットの更新・設定変更・表示の切替で、次の周期（2 秒）を待たずに更新する。
- 改善（ClaudeMonitor からの変更）: 短期枠が無いプラン（Codex の週次のみなど）は、アイコンの数字とツールチップに長期枠を使う（存在しない短期枠の 0% を出さない）。
- 自動テスト: `go vet`・`go test`（全パッケージ）・`golangci-lint`（0 件）が通る。ClaudeMonitor のペース判定のテストを移植し、`buildTrayInfo`（プロバイダー選択、週次のみ、クレジットと追加利用枠、エラー状態）、アイコンのキー、描画（0〜100%、3 桁の詰め描画、角が透明、色と数字の違い）、`effectiveTrayDisplay`、ツールチップの文字数配分（UTF-16 単位）、HICON 変換、`kickTray` を追加した。`DUMP_TRAY_PREVIEW=<PNG のパス> go test ./aiusage -run TestDumpTrayPreview` で、状態ごとのアイコンの一覧画像を出せる。
- **実機の確認（独立した `LOCALAPPDATA`）**: (1) 隠れているアイコンの領域を UI Automation で読み、ツールチップを確認した。`Claude モニター — 未ログイン / 右クリック → ログイン / CPU: … | Mem: … | …`、Codex に切り替えると `Codex モニター / 5時間: 4% / 7日: 45% / Credit: … / CPU: … | …`（全項目が 127 文字に収まる）。AI を OFF にするとシステムの項目だけになり、再び ON にすると AI の行が戻る。(2) 実際のトレイに描画されたアイコンを画面から取り出して確認した（緑の角丸四角に数字）。(3) `POST /api/ai/test-notify` で `Shell_NotifyIcon` が成功し、`notify.log` に `balloon … ok=true` が残る。(4) トレイに出すサービスの切替（Claude / Codex）が反映される。
- **不具合の修正（利用者の確認で発覚）**: ログインを終えても、次の取得（既定 5 分ごと）まで「未ログイン」のまま残っていた。ログイン後の画面遷移では、ページ内の取得スクリプト（ドキュメント生成の 1.5 秒後に 1 回）が再実行されないため。ClaudeMonitor から引き継いだ挙動で、T1 の確認では私が毎回 `refresh` を呼んでいたため気づけなかった。修正: (1) ログイン画面を出している間は 4 秒おきに取得し直す（最長 10 分。成功するとログイン画面が自動で閉じて止まる。`watchLoop`）。(2) ログイン画面を × で閉じたときも 1 回取得し直す。(3) AI 区画の見出しに「↻ 今すぐ更新」ボタンを追加（取得中は回転し、終わると値と障害状況を描き直す）。実機で、ログアウト → ログイン画面 → 利用者がログイン、の流れで、`refresh` を呼ばなくても自動で `ok` になり、ログイン画面が自動で画面外へ戻ることを確認した。`watchLoop` のテスト（画面が閉じたら止まる、最長時間で止まる、最初から閉じていれば取得しない）を追加。
- **未確認**: (1) トレイの右クリックメニューの見た目と、実際のクリック操作（利用者の目視が必要）。なお、メニューのコマンド（更新・通知テスト）は、後日、トレイのウィンドウ（メッセージ専用ウィンドウ）へ `WM_COMMAND` を送って確認した。通知テストは `notify.log` に `balloon … ok=true` が残り、更新は使用量の更新時刻が進んだ。当初「この環境から送れなかった」と書いたのは、確認用スクリプトの不備（PowerShell は文字列引数の `$null` を空文字に変換するため、`FindWindowEx` がウィンドウを見つけられなかった）によるもので、誤りだった。ログインのコマンドは、同じ `ShowLogin` を `relogin` の API 経由で確認済み。(2) バルーン通知（トースト）が画面に実際に表示されること。`Shell_NotifyIcon` の成功までは確認したが、表示は集中モード（通知の抑止）などの OS 設定に依存する。(3) 閾値（60% / 80%）を実際に超えたときの通知。判定ロジックは ClaudeMonitor から移植したテストで確認済み。(4) 高 DPI 環境でのアイコンの見え方（アイコンは 16px で作り、Windows が拡大する。ClaudeMonitor と同じ）。(5) ダークとライトのタスクバーでの視認性。
### T4: 移行・文書・リリース

- Cookie コピー（D1）、README（機能・保存先・アンインストール手順に `AuthWebView2` 等を追記。冒頭にプレリリースである旨を書く）、`winres.json` を `2.0.0.0` に更新し、変更履歴に `### 2.0.0` を追記、`release.yml` の `prerelease` を `true` にする（D6）。ClaudeMonitor 側は変更しない（D5）。



**T4 の実施結果（2026-10-02）。リリース（タグの作成と公開）は未実施**

- **D1 の引き継ぎ（`aiusage/migrate.go`）**: 認証用の WebView を初めて作るときに、旧 ClaudeMonitor（DataRoot と同じ階層の `ClaudeMonitor\\{AuthWebView2,CodexAuthWebView2}\\EBWebView`）から、`Local State` と `Default\\Network\\Cookies`（あれば `-journal`）だけをコピーする。一時フォルダに作ってから改名するので、失敗しても中途半端なものを残さない。コピー元は変更しない。
  - 印（`AuthWebView2.migrated` / `CodexAuthWebView2.migrated`）: 引き継いだあと、または、既に認証データがあって上書きしなかったときに付ける。印が無いと、**ログアウト（認証フォルダの削除）→ 再作成で旧データが戻り、ログアウトが無効になる**ため。旧データが無いとき、コピーに失敗したとき（旧 ClaudeMonitor が起動中で Cookie がロックされているなど）は、印を付けず、次回に再試行する。
  - テスト: Cookie と鍵だけを引き継ぎキャッシュなどは引き継がない／コピー元が変わらない／旧データが無ければ何もしない／必須ファイルが欠けた旧データは無視／既存を上書きしない／**ログアウトの後に旧データが戻らない**／失敗時に何も残さず印も付けない／プロバイダーごとに別／生成の前に引き継ぐ。
  - 実機（独立した `LOCALAPPDATA`。旧 ClaudeMonitor のフォルダは、利用者がテスト用のプロファイルでログインしたセッションの Cookie で用意した。実際の ClaudeMonitor のデータには触れていない）: 初回の起動で自動的にログイン済み（`ok`、Claude Pro）になる／ダミーのキャッシュは引き継がれない／コピー元は無傷／ログアウトで `needs_login` になり、**再起動しても旧データが戻らない**。
- **バージョン**: `winres/winres.json` を `2.0.0.0`（5 か所）にし、`go-winres make` で `rsrc_windows_386.syso` と `rsrc_windows_amd64.syso` を再生成。リリース用の設定でビルドしたバイナリの埋め込みバージョンが `2.0.0.0` であることを確認した（トレイメニューは `formatFileVersion` により `v2.0.0` と表示される）。
- **`release.yml`**: `prerelease: true` にした（コメント付き。正式版にするときは `false` に戻し、公開済みのリリースは `gh release edit <タグ> --prerelease=false` で外す）。
- **README**: 冒頭にプレリリースの案内、機能・使い方・表示オプション・「AI 使用量」の節（ON の手順、旧 ClaudeMonitor からの引き継ぎ、ログアウト、通知、メモリの目安、注意）・プロジェクト構成・アーキテクチャ・依存・設定ファイルの保存場所（`AuthWebView2` など）・アンインストール、変更履歴の `2.0.0` を追記。
- ClaudeMonitor 側は変更していない（D5）。
- **未実施・利用者の判断が要るもの**: (1) タグ `v2.0.0` の作成とプッシュ（`release.yml` が起動し、プレリリースとして公開される）。(2) 実際の旧 ClaudeMonitor のデータでの引き継ぎの確認（利用者の環境で、AI 使用量を初めて ON にして確認する）。(3) 変更をリモートへプッシュし、PR を作成する。
## 11. リスク

| リスク | 影響 | 対処 |
|---|---|---|
| 認証 WebView2 の追加でメモリが増える | 常駐ウィジェットとして重くなる | D2 を既定 OFF にし、無効なプロバイダーでは生成しない。T0 で実測 |
| 使用量 API は非公式（claude.ai の内部 API、chatgpt.com の `wham/usage`） | 仕様変更で取得不能になる | ClaudeMonitor の README にも同じ注意がある。取得失敗は AI セクションだけに閉じ、システム監視側に波及させない |
| `securityMiddleware` の導入で、ClaudeMonitor の UI の `fetch` が 403 になる | AI セクションが空になる | `apiFetch` へ一括置換し、T1 の curl 検証と T2 の実機確認で見る |
| ログアウトでウィジェット全体が再起動する | **解消**: D7 で、再起動しない方式（Destroy → 削除 → 再作成）に決定。T0 で動作を確認済み | — |
| 認証 WebView2 の生成が UI スレッド制約に反して固まる | 起動時のハング | T0 の 1 で確認。固まる場合は起動時に生成する方式へ固定 |
