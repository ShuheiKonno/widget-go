# スキン機能 実装計画・インターフェース仕様

> 実装済み。仕様の正は [docs/skin-spec.md](skin-spec.md)(このファイルは実装時の計画・契約の記録)。

ウィジェットの見た目を差し替える「スキン」機能。スキンは `skin.json` + `skin.css` の 2 ファイルで、
利用者が自作・インポート(zip)できる。組み込みは 4 つ(モックは `docs/skins/mock-*.html`)。

このファイルは並列実装の**契約**。各担当は自分の担当範囲だけを実装し、ここに書かれた名前・形・URL を変えない。
変えたい点があれば作業せずに escalation / question で調整する。

## 1. 組み込みスキン(id / 表示名 / 元モック)

| id | 表示名 | 元モック |
|---|---|---|
| `shukei` | 朱罫帳 | `docs/skins/mock-claude.html` |
| `fieldnote` | 計測票 FIELDNOTE | `docs/skins/mock-codex.html` |
| `orbital` | Orbital Zero-G | `docs/skins/mock-antigravity.html` |
| `paper-tape` | 紙テープ / PAPER TAPE | `docs/skins/mock-opencode.html` |

`default` は予約 id(スキンなし = 現行デザイン)。ファイルは持たず、常に一覧の先頭に出す。

## 2. スキンのファイル形式

保存場所(利用者スキン): `%LOCALAPPDATA%\widget-go\skins\<フォルダ名>\skin.json` と、その `css` が指す CSS。
組み込み: リポジトリの `skins/builtin/<id>/{skin.json,skin.css}` を `go:embed` で exe に同梱(読み取り専用)。

```json
{
  "schema": 1,
  "id": "shukei",
  "name": "朱罫帳",
  "author": "Claude",
  "version": "1.0.0",
  "description": "帳簿の朱罫をモチーフにした…",
  "css": "skin.css",
  "swatch": ["#14110f", "#c0392b", "#f4efe3"],
  "startupBackground": { "dark": "#14110f", "light": "#f4efe3" }
}
```

| フィールド | 必須 | 規則 |
|---|---|---|
| `schema` | 必須 | 整数。現在は `1` のみ。それ以外は拒否 |
| `id` | 必須 | `^[a-z0-9][a-z0-9-]{0,31}$`。`default` と組み込み id、既存の利用者スキンの id とは重複不可 |
| `name` | 必須 | 1〜40 文字 |
| `author` / `version` / `description` | 任意 | 各 0〜100 / 0〜20 / 0〜200 文字 |
| `css` | 任意(既定 `skin.css`) | `^[A-Za-z0-9._-]+\.css$`。同じフォルダ内のファイル |
| `swatch` | 任意 | `#RRGGBB` の文字列を 3 つ。設定画面の色見本 |
| `startupBackground` | 任意 | `dark` / `light` に `#RRGGBB`。起動時の WebView2 既定背景色(ちらつき対策)。省略時は現行の既定色 |

CSS の検証(Go 側。防御の本体は CSP で、これは多重防御):
UTF-8(BOM は除去)、256 KiB 以下、`</style` を含まない、`@import` を含まない、`url(` は直後が `data:image/` のときだけ許可、
`expression(` / `javascript:` / `-moz-binding` / `behavior:` を含まない。コメントを除去した版でも同じ検査を行う。
`skin.json` は 16 KiB 以下。

### zip(インポート形式)

`skin.json` と CSS ファイルを含む zip。ルート直下、または**唯一のトップレベルフォルダ**の直下に置く。
`skin.json` と CSS 以外のエントリは無視する。パスに `..` や絶対パス、ドライブ指定を含むエントリは拒否する(zip-slip)。
展開後の合計サイズ 1 MiB 以下、zip 本体 1 MiB 以下、エントリ数 20 以下。

## 3. 実行時の仕組み

- CSP は `style-src 'unsafe-inline'` のみ(`server.go`)。`<link rel=stylesheet>` は使えない。**スキン CSS は JS が fetch して `<style id="skin-css">` の `textContent` に入れる**。
  外部フォント・外部画像・外部 `@import` は CSP が遮断する。`data:` の画像は使える。
- 有効なスキンは `body` の `data-skin="<id>"` 属性で表す。`default` のときは属性を付けず、`<style id="skin-css">` も空(または無し)にする。
- 有効なスキンの CSS だけを注入する(他のスキンの CSS は読み込まない)。
- スキンの選択はテーマ(ダーク/ライト/システム)・透過モードと**独立**。`skin.css` は
  `body[data-skin="<id>"]`(ダーク/共通)と `body[data-skin="<id>"].theme-light`(ライト)の 2 系統で書く。
  `body.theme-light` は詳細度 (0,1,1) なので、スキンのセレクタを `body[data-skin=...]` で始めて詳細度を揃え、注入する `<style>` を `<head>` の最後に置いて順序で勝たせる。
- **セレクタの書き方(厳守)**: スキンのセレクタは必ず `body[data-skin="<id>"]` で始める。**属性値は二重引用符のまま**
  (`/api/skins/copy` がこの文字列 `data-skin="<id>"` を置換するため。単引用符や引用符なしは不可)。
  モックの `.win` / `.thumb` のようなモック専用のラッパーは使わず、実 DOM のセレクタに直す。
  実 DOM では `theme-light` は `<body>`、`compact` は `<div class="container" id="dragArea">`(= body の子孫)に付く。
  したがって「ダーク/共通」は `body[data-skin="x"] …`、ライトは `body[data-skin="x"].theme-light …`、
  コンパクトは `body[data-skin="x"] .compact …`(または `.container.compact`)と書く。
- **状態クラスの罠(C1・C2 必読)**: スキンのセレクタは `body[data-skin=…]` を頭に付けるぶん、本体の `.compact .section-value`(詳細度 0,2,0)より強くなる。
  そのため、スキンが上書きするプロパティのうち、本体が `.compact …` で別の値を指定しているものは、
  スキン側にも `body[data-skin="x"] .compact …` の対応ルールを書かないと、コンパクト(幅 130px)の寸法がスキンの値で潰れて崩れる。
  `.flipped` / `.ethernet-mode` / 折りたたみなど他の状態クラスも同じ。モックで `.compact` を明示していた箇所を、実 DOM の形に直して必ず残す。
- スキンは**ウィジェット本体(`/`)だけ**に適用する。設定ウィンドウ(`/settings`)は従来どおりテーマだけで、スキンの影響を受けない。

## 4. 本体に新設する CSS 変数(ui/index.html の担当が定義する)

現在 CSS/JS に直書きされている色を変数にする。**既定値は現行と同一**(ダーク用を `:root`、ライト用を `body.theme-light` に置く)。
名前は固定。`ui/index.html` の担当は、この表の変数を必ず作る(他にも変数化したいものがあれば足してよいが、この表の名前は変えない)。

| 変数 | 置き換える現行の直書き | ダーク(既定) | ライト |
|---|---|---|---|
| `--core-bar-bg` | `.core-bar` の background | `linear-gradient(180deg,#8a9cff,#b07cff)` | 同左 |
| `--core-value-color` | `.core-value` の color | `#fff` | `#fff` |
| `--core-value-shadow` | `.core-value` の text-shadow | `0 1px 2px rgba(0,0,0,0.6)` | `0 1px 2px rgba(0,0,0,0.35)` |
| `--memory-used-color` | `.memory-details .used` | `#ffa3ff` | `#e85ad0` |
| `--wifi-ok-color` | `.wifi-connected` | `#7fffaa` | `#1aa86b` |
| `--wifi-ng-color` | `.wifi-disconnected` | `#ff8a8a` | `#d33` |
| `--gpu-fill-bg` | `.gpu-fill` の background | `linear-gradient(90deg,#6ad7ff,#5a8cff)` | 同左 |
| `--npu-fill-bg` | `.npu-fill` の background | `linear-gradient(90deg,#ffd36a,#ff9f5a)` | 同左 |
| `--net-rx` | 凡例 `.net-leg-rx` の color と Canvas の rx の線色 | `#7fffaa` | `#1aa86b` |
| `--net-rx-fill` | Canvas の rx の塗り | `rgba(127,255,170,0.25)` | `rgba(26,168,107,0.20)` |
| `--net-tx` | 凡例 `.net-leg-tx` の color と Canvas の tx の線色 | `#ffa3ff` | `#b74da6` |
| `--net-tx-fill` | Canvas の tx の塗り | `rgba(255,163,255,0.22)` | `rgba(183,77,166,0.18)` |
| `--net-empty-color` | Canvas の「計測中...」の色 | `rgba(255,255,255,0.35)` | `rgba(0,0,0,0.4)` |

Canvas の色は JS が `getComputedStyle(document.body)` で上の変数を読んで描く(読めない/空のときは現行の直書き値にフォールバック)。
テーマ変更・スキン変更のたびに再描画する。`body:not(.theme-light)` の直書き色ルールは、この変数に置き換えて無くす。

既存の変数(`--container-bg` `--section-bg` `--section-border` `--section-title` `--section-value` `--bar-track` `--text-color` `--muted-color` `--btn-bg` など)は、そのままスキンの上書き対象。

## 5. HTTP API(Go 側が実装、フロントが使う)

すべて既存と同じトークン検証(`X-Widget-Token`)の内側。状態を変えるものは POST。エラー本文は日本語の短い文(`http.Error`)。

| メソッド/パス | 内容 |
|---|---|
| `GET /api/skins` | 呼び出しのたびに `skins\` を再スキャンして一覧を返す。下記 JSON |
| `GET /api/skin/css?id=<id>` | 有効なスキンの CSS(`Content-Type: text/css; charset=utf-8`)。`default` は空本文で 200。未知の id は 404 |
| `POST /api/setoption?key=skin&value=<id>` | 有効なスキンを保存。`default` で解除。未知の id は 400 |
| `POST /api/skins/import[?overwrite=1]` | 本文は zip(生バイト、1 MiB 以下)。成功は 200 + `{"id":"…","name":"…"}`(自動では有効にしない)。検証エラーは 400。同じ id の利用者スキンが既にあり `overwrite=1` が無いときは 409。組み込み id・`default` との衝突は 400 |
| `POST /api/skins/delete?id=<id>` | 利用者スキンを削除(フォルダごと)。組み込み・`default`・未知の id は 400。削除したスキンが有効だったら `default` に戻す |
| `POST /api/skins/opendir` | `skins\` フォルダをエクスプローラで開く(無ければ作る) |
| `POST /api/skins/copy?id=<組み込み id>` | 組み込みスキンを `skins\<id>-copy\` に複製(`skin.json` の `id` を `<id>-copy`、`name` を「<name> (コピー)」に、CSS 内の `data-skin="<id>"` を `data-skin="<id>-copy"` に置換)。成功は 200 + `{"id":"…","name":"…"}`。既にあれば 409。組み込み以外は 400 |
| `GET /api/settings` | 既存の応答に `"skin": "<有効な id>"` を足す(未設定・不明は `"default"`) |

`GET /api/skins` の応答:

```json
{
  "active": "shukei",
  "dir": "C:\\Users\\…\\AppData\\Local\\widget-go\\skins",
  "skins": [
    { "id": "default", "name": "デフォルト", "builtin": true, "author": "", "version": "", "description": "現行のデザイン", "swatch": [] },
    { "id": "shukei", "name": "朱罫帳", "builtin": true, "author": "Claude", "version": "1.0.0", "description": "…", "swatch": ["#14110f", "#c0392b", "#f4efe3"] },
    { "id": "my-skin", "name": "自作", "builtin": false, "author": "", "version": "", "description": "", "swatch": [] }
  ],
  "errors": [ { "folder": "broken-skin", "message": "skin.json の id が不正です" } ]
}
```

`skins` の順序は `default`、組み込み(上の表の順)、利用者スキン(id の昇順)。読み込みに失敗した利用者スキンは `skins` に入れず `errors` に入れる。
`active` が存在しないスキンを指すときは `default` に矯正して返す(保存値も `default` に戻してよい)。

設定の保存: `config.json` の `skin`(文字列)。`default` は空文字で保存してよい。古い `config.json`(キー無し)はそのまま読める。

## 6. フロント(ui/index.html)の挙動

- 起動時の `/api/settings` の `skin` を読み、`default` 以外なら CSS を fetch して注入する。**初回の適用が済むまで `/api/ready`(ウィンドウを見せる合図)を送らない**(起動時のちらつき対策。`docs/startup-flicker.md` を読むこと)。失敗したら `default` で続行する。
- 設定ウィンドウが `/api/settingschanged` を呼んだとき(= ウィジェット側の再取得の合図)、`skin` が前と同じでも**常に CSS を再取得して注入し直す**(利用者が `skin.css` を編集して「再読み込み」したときに反映するため)。連続呼び出しで古い応答が後から勝たないよう、世代番号で最新の応答だけを適用する。
- スキンの適用後は、Canvas を再描画し、高さの自動調整(内容に合わせたウィンドウの高さ調整)を既存の仕組みで再実行する。

## 6.5 Go 側の補足(B 必読)

- 組み込みの埋め込みは `//go:embed all:skins/builtin`(ドットファイル `.keep` が今あるだけでもコンパイルできるように `all:` を付ける)。ローダはディレクトリ以外のエントリを読み飛ばす。
- 設定ウィンドウのタイトルバー判定 `settingsDarkTitleBar` は `startupBackground(theme, systemLight) == startupBgDark` に依存している。
  **この関数(テーマだけで決める)は現状の挙動のまま残す**。スキンの背景色を考慮する関数は別名で新設し、**ウィジェット本体の起動時背景色の決定にだけ**使う(`main.go` の呼び出し元)。スキンの色で設定ウィンドウのタイトルバーが反転しないこと。
- 組み込みスキンが §2 の検証をすべて通ることを確かめるテスト(`TestBuiltinSkinsLoad` 相当)を書く。組み込みの 4 スキンは別の担当(C1・C2)が並行して作るので、
  それ以外のテストは `testdata` や `t.TempDir()` のフィクスチャで書き、C の作業途中の影響を受けないようにする。組み込みスキンのテストは最後に実行し、C のファイルが未完成で落ちるなら、その旨を報告に書く(待ってから再実行する)。
- 現行の `go build` は `-buildvcs=false` を付けないと失敗する環境(worktree)がある。

## 7. 担当と所有ファイル

| 担当 | 範囲 | 編集してよいファイル |
|---|---|---|
| A | フロント基盤(§4 §6) | `ui/index.html` のみ |
| B | Go 側(§2 の検証・§5 の API・設定・起動背景色) | `skins.go` `skins_test.go` `config.go` `config_test.go` `server.go` `startup.go` `startup_test.go` `main.go` `settingswin.go` と、必要なら新規の `*_test.go` |
| C1 | 組み込み `shukei` `fieldnote` | `skins/builtin/shukei/*` `skins/builtin/fieldnote/*` |
| C2 | 組み込み `orbital` `paper-tape` | `skins/builtin/orbital/*` `skins/builtin/paper-tape/*` |
| D | 設定画面のスキン UI | `ui/settings.html` のみ |

全員共通: **git commit / stash / checkout / reset / clean は禁止**(同じ worktree を同時に使う)。他の担当のファイルは編集しない。
他の担当の作業途中でビルドが一時的に壊れることがある。自分の範囲に原因がなければ、少し待って再実行する。
一時ファイル(動作確認用のハーネスなど)はリポジトリの外(`$env:TEMP` 配下)に置く。
