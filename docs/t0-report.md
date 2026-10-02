# T0 事前検証レポート

- 日付: 2026-10-02
- 対象: `docs/plan-claude-monitor.md` の §10 T0（T0-1〜T0-4）
- 状態: **T0-1〜T0-4 すべて実測済み**（T0-4 は実アカウントのセッションを使用。ユーザーの許可を得て、ユーザー自身が手元で実行）
- 検証プログラム: `scratchpad/t0spike/`（使い捨て。go-webview2 は rockfish と同じ固定コミット `v0.0.0-20260205173254-56598839c808`）と、実行スクリプト `scratchpad/run-t0.ps1`。WebView2 は画面外 (-32000,-32000) に作り、既存アプリのデータは変更していない。
- 実行: 検証プログラムの実行はこのセッションのツール権限で拒否されたため、ユーザーが手元で `-Step all` を実行した（所要約 8 分、終了コード 0）。
- 事実の根拠は、go-webview2 のソースと、この PC での実行ログ。「予測」と書いたものは実測で確定していない。

## 1. ソース読解でわかったこと（go-webview2）

| 項目 | わかったこと（根拠） | 計画への影響 |
|---|---|---|
| `Dispatch` の仕組み | **スレッドメッセージ**（`PostThreadMessageW(mainthread, WM_APP)`）を送り、`Run()` が `msg.Message == WMApp` を見て自分のキューを処理する（`webview.go:443-448`、`:351-366`）。キューは WebView 1 個ごとに別 | 補助 WebView の `Dispatch` は誰も処理しない。メイン WebView の `Dispatch` に寄せる設計は正しい |
| 生成時のメッセージループ | `Embed()` は初期化完了まで**入れ子の `GetMessageW` ループ**を回す（`pkg/edge/chromium.go:~95-110`）。このループはスレッドメッセージをキュー処理に渡さない | §3 の T0-1 で影響を確認 |
| `Destroy()` の副作用 | `Destroy()` は `WM_CLOSE` を送り、`WM_CLOSE` → `DestroyWindow` → `WM_DESTROY` → **`Terminate()`（`PostQuitMessage`）**（`webview.go:240-243`、`:347-349`）。補助 WebView を壊すとアプリ全体のメッセージループが終了する | サブクラスで `WM_DESTROY` を握りつぶす必要がある。§3 の T0-3 で、握りつぶせば安全に壊せることを確認 |
| コントローラの解放 | `Chromium` に `Close` 相当のメソッドは無い（`ICoreWebView2Controller` の vtable に `Close` はあるが未使用） | §3 の T0-3 で、`Destroy` だけでブラウザプロセスが終了することを確認 |
| クッキー操作 | `GetCookieManager` と `CallDevToolsProtocolMethod` の vtable 枠はあるが、ラッパーメソッドは無く、フィールドはすべて非公開 | `reflect` + `unsafe` で非公開フィールドに届き、CDP を呼べた（§3 の T0-3）。ただし本番に入れるなら、この手法は脆いので第 2 候補にとどめる |

## 2. この PC の実データの調査結果

| 項目 | 結果 | 計画への影響 |
|---|---|---|
| ClaudeMonitor `AuthWebView2` のサイズ | **624.1 MB / 13,707 ファイル**（`CodexAuthWebView2` は 112.8 MB / 1,250 ファイル） | **D1 のフォルダ全体コピーは現実的でない**。`EBWebView\Local State` と `Default\Network\Cookies`（+ `-journal`）だけに絞る（T0-4 で検証） |
| Cookie ファイルの更新日時 | Claude 2026-09-26 21:35、Codex 2026-09-26 21:37（6 日前） | T0-4 で認証が通らない結果が出たとき、コピーの失敗と失効を区別するため、ClaudeMonitor 本体での認証状態の確認も必要 |
| `notify.log` | **18,021,962 bytes（約 17.2 MB）**、最終更新 2026-09-26 | ClaudeMonitor にサイズ上限が無いことの実例。計画書 §7 の方針を「T1 で実装」に格上げ（起動時に一定サイズを超えていたら切り詰める） |
| `config.json`（ClaudeMonitor） | `activeProvider: "claude"`、`claudeEnabled` / `codexEnabled` ともに true、`layoutMode: "tabs"` | D9 の既定（Claude 優先）と整合 |
| 起動中のプロセス | `system-widget`（rockfish、PID 13488、Working Set 39.8 MB）のみ。ClaudeMonitor は停止中 | rockfish を改造して試すと単一起動で終了するため、独立した検証プログラムで行った |

## 3. 実測結果

### 3.1 T0-1: 実行中（`Run()` 開始後）に `Dispatch` 内で補助 WebView を生成

実行ログ（時刻は検証プログラム起動からの経過）:

```
[ 3368ms] ACTION create:late (inside Dispatch, UI thread)
[ 3619ms] late: created in 250ms (hwnd=0x709ac)
[ 4051ms] REPORT[late] loaded example.com/                      ← 補助 WebView の Bind コールバックが届いた
[ 4052ms] PROBE B (queued during creation) ran, +683ms after creation started
[ 9369ms] PROBE C (queued 6s later) ran                         ← 生成中でなければ遅れずに実行された
[12367ms] ping ok
```

- **生成できる**: `Run()` 開始後の `Dispatch` の中で、補助 WebView を約 250ms で生成でき、HWND を取得できた。ページの読み込みと、その後の Bind コールバック（`__report`）も届いた。メインのメッセージループは生き続けた（12 秒後の `ping` が実行された）。
- **止まる `Dispatch` がある**: 生成中（約 150ms 時点）に積んだプローブ B は、生成の完了（3619ms）から約 433ms 後の 4052ms に実行された。ちょうど補助 WebView の Bind 応答（4051ms）が `Dispatch` を積んだ直後で、**「次に誰かが `Dispatch` を積むまで止まる」という予測と整合する**。ただし、Bind 応答が無い条件での再実験はしていないので、「止まり続けるか」までは確認できていない。生成中でなかったプローブ C は遅れなかった。
- **結論**: D2 の「ON にした時に生成」は採用できる。生成の直後に空の `Dispatch` を 1 回積む対策を入れる（この対策そのものは未検証）。

### 3.2 T0-3: ログアウトの機構（テスト用クッキーのみ。資格情報は使っていない）

```
[ 5682ms] cookieset:late (document.cookie)
[ 7683ms] REPORT[late] before testcookie: ... t0cookiePresent=true
[ 9681ms] clear:late Network.clearBrowserCookies err=<nil>
[11681ms] nav:late -> https://example.com
[17682ms] REPORT[late] afterClear testcookie: ... t0cookiePresent=false
[22682ms] destroy:late requested
[22683ms] late: WM_DESTROY swallowed (no PostQuitMessage)
[24681ms] ping ok    [27681ms] ping ok                          ← Destroy 後もメインのループは生きている

t=22.8s  msedgewebview2 processes of aux dir = 5
t=23.5s  msedgewebview2 processes of aux dir = 0               ← 約 1 秒以内にブラウザプロセス一式が終了
t=33.4s  Remove-Item(aux dir) = OK                              ← 検証プログラム実行中でもデータフォルダを削除できた
```

- **CDP でのクッキー削除は動く**: `Network.clearBrowserCookies`（完了ハンドラ無し）が成功し（`err=<nil>`）、再読み込み後にテスト用クッキーが消えていた。限界: 使ったのは JavaScript で作った（HttpOnly でない）クッキー 1 つで、HttpOnly と localStorage は未検証。
- **Destroy → フォルダ削除 → 再作成も動く**: `WM_DESTROY` を握りつぶして `Destroy()` すると、アプリは終了せず、その補助 WebView のブラウザプロセスが約 1 秒で消え、データフォルダを削除できた。**これは D2 の「OFF にしたらメモリを解放」にも使える**。
- **結論**: D7 は「再起動なし」にできる。推奨は Destroy → 削除 → 再作成（WebView 内の全データを消せる。`reflect`/`unsafe` が不要）。CDP は第 2 候補。

### 3.3 T0-2: メモリ（WebView2 の追加コスト）

測定条件: 未ログイン状態、メインは `about:blank`（実際の UI より軽い）、起動から 45 秒待って 5 秒おきに 3 回採取した中央値、各シナリオ 1 回のみ。指標は Private Working Set（Win32_PerfFormattedData_PerfProc_Process）で、`--user-data-dir` が対象フォルダのプロセスと、その子孫だけを合計。

| シナリオ | WebView2 プロセス数 | WebView2 (MB) | Go ホスト (MB) | 合計 (MB) | S0 との差 |
|---|---:|---:|---:|---:|---:|
| S0 メインのみ | 6 | 81.3 | 3.1 | 84.4 | — |
| S1b + Claude（`about:blank`。インスタンスの固定費だけ） | 12 | 164.1 | 3.3 | 167.4 | +83.0 |
| S1 + Claude（claude.ai のログイン画面） | 14 | 509.6 | 3.4 | 513.0 | +428.6 |
| S2 + Claude + Codex（別フォルダ） | 20 | 670.6 | 3.6 | 674.2 | +589.8 |
| S3 + Claude + Codex（1 つのフォルダを共有） | 15 | 585.2 | 3.5 | 588.7 | +504.3 |

- **インスタンス 1 個の固定費は約 +83MB**（プロセス +6）。
- **ページの中身が大半を占める**: claude.ai のログイン画面で約 +345MB（S1 − S1b）。Codex（chatgpt.com のログイン画面）の追加は約 +161MB。
- **フォルダを共有すると約 −85MB**（インスタンス 1 個分）。代わりに、プロバイダー別に認証データを丸ごと消す方法（フォルダ削除）が使えなくなる。CDP でドメイン単位に消す必要がある。
- **限界**: ログイン後の画面（`settings/usage`）は別の値になりうる。実際の ClaudeMonitor 本体の数値は測っていない。S0 のメインは `about:blank` なので、実 UI を含む絶対値ではなく、差分で見ること。
- **結論**: 両プロバイダーを ON にすると約 +590MB（未ログイン時）。D2 を既定 OFF にする根拠として十分。

### 3.4 T0-4: Cookie の最小コピー（実アカウントのセッションを使用）

手順: ClaudeMonitor 停止中に、旧 `AuthWebView2` / `CodexAuthWebView2` から `Local State` と `Default\Network\Cookies`（+ `-journal`）だけを scratchpad の新規フォルダへコピー（Claude 約 56KB、Codex 約 88KB。元のフォルダは 624MB / 113MB）し、そのフォルダで補助 WebView を起動して認証状態を確認した。出力はステータスコードと真偽値のみ。コピーは検証後に削除済み（`copy deleted: True`）。

```
[15708ms] t14 claude: page=claude.ai/new hasOrgCookie=true usageStatus=200
[15831ms] t14 codex:  page=chatgpt.com/ sessionStatus=200 hasToken=true usageStatus=200
[25480ms] t24 claude: ... hasOrgCookie=true usageStatus=200      ← 10 秒後も同じ
[25480ms] t24 codex:  ... sessionStatus=200 hasToken=true usageStatus=200
[29079ms] clear:claude / clear:codex Network.clearBrowserCookies err=<nil>
[30207ms] claude: loaded claude.ai/login   → [31705ms] claude.ai/logout → [32613ms] claude.ai/login
[44080ms] afterClear claude: page=claude.ai/login hasOrgCookie=false usageStatus=n/a
[44115ms] afterClear codex:  page=chatgpt.com/ sessionStatus=200 hasToken=false usageStatus=n/a
```

- **最小コピーで認証が引き継がれる**: Claude も Codex も、コピー直後から使用量 API が 200 を返した。Cookie ファイルの更新日（9/26）から 6 日経っていたが、失効していなかった。D1 は (a) の最小コピーで確定できる。限界: 同じ PC・同じ Windows ユーザー内の検証（DPAPI の鍵は `Local State` に入っているが、Windows ユーザーに紐づく。別ユーザー・別 PC は未検証で、通らない想定）。コピー元は ClaudeMonitor 停止中。
- **CDP のクッキー削除は実アカウントでも効く**: 削除後、Claude は自動でログイン画面（`/logout` を経て `/login`）に戻り、`lastActiveOrg` クッキーが消えた。Codex は `/api/auth/session` が 200 でも `accessToken` が無くなり、ClaudeMonitor の判定どおり「要ログイン」になる状態。HttpOnly のセッションクッキーを含めて消えたことが、サイトの挙動（ログイン画面への遷移・トークン消失）から確認できた。サーバー側のセッション自体は無効化されない（現行の「データフォルダ削除」も同じ）。
- 副産物: 起動 URL `claude.ai/settings/usage` は `claude.ai/new` に遷移していた（ClaudeMonitor 側のコードはこの遷移後のページで取得している）。

## 4. 計画への反映

| 項目 | 変更 |
|---|---|
| D1 | フォルダ全体コピーを取り下げ、最小コピー（`Local State` + `Cookies`）に変更。**T0-4 で認証が通ることを確認済み**。(a) で確定できる |
| D2 | 既定 OFF を維持。ON で生成、OFF で Destroy して解放。実測値を根拠に追記 |
| D7 | 推奨を「現行方式（再起動）」から「再起動なし（Destroy → 削除 → 再作成）」に変更。`--restarted` の待機処理は不要になる |
| §2.3 | 制約 4a（生成後に空の `Dispatch`）と 4b（`WM_DESTROY` を握りつぶしてから `Destroy`）を追加 |
| §1 の「負荷」 | 推定を実測値に置き換え |
| §7 | `notify.log` のサイズ上限は T1 で実装（実例 約 17MB） |

## 5. 未実施・追加検証の候補

1. **生成直後の空 `Dispatch` が止まりを解消するか**: 4a の対策の確認。Bind 応答が無い条件での止まり方も合わせて。T1 の最初に確認すればよい（資格情報は不要）。
2. **軽量な同一オリジンのページで待機したときのメモリ**（新しい仮説）: ページの中身が 345MB 前後を占めるので、取得時以外は `https://claude.ai/robots.txt` のような軽いページに置けば、固定費（約 +83MB）近くまで下げられる可能性がある。同一オリジンの `fetch` が通るか、bot 対策に弾かれないかは未検証。
3. **ログイン後の画面でのメモリ**: T0-2 は未ログイン画面での計測。T0-4 と同じ最小コピーの仕組みで、ログイン済みのまま測れる（実アカウントのセッションを使うため、実施する場合は別途ご確認のうえ）。
4. 別ユーザー・別 PC への Cookie コピー（DPAPI）: 通らない想定だが未検証。統合の用途では不要。
