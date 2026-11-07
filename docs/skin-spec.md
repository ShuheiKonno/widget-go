# スキン仕様書(スキン作者向け)

ウィジェット本体(`/`)の見た目を差し替える「スキン」の作り方をまとめた仕様書です。実装(`skins.go`、`server.go`、`ui/index.html`、`ui/settings.html`、`skins/builtin/*`)に合わせて書いています。実装との食い違いを見つけたら、実装を正としてください。

## 1. スキンとは

スキンは `skin.json`(名前や色見本などの情報)と `skin.css`(見た目)の **2 ファイル**で、利用者が自作したり、zip でインポートしたりできます。

- スキンの適用先は**ウィジェット本体だけ**です。設定ウィンドウ(⚙)はスキンの影響を受けず、従来どおりテーマ(ダーク / ライト / システム)だけで決まります。
- スキンの選択は、テーマ(ダーク / ライト / システム)・透過モードと**独立**です。どのテーマでも同じスキンを選べるので、スキンはダーク用とライト用の**両方**を持ちます(§4)。
- スキンに**スクリプトは含められません**。スキンが書けるのは CSS だけです。CSS は検証を通ったものだけが読み込まれ(§3)、さらに本体の CSP(§5)が外部への通信を止めます。
- スキンを選ばない(`default`)ときは、これまでと同じデザインのまま、スキンの CSS は何も読み込みません。
- 組み込みのスキンが 4 つあります。`default` は予約 id で、スキンなし(従来のデザイン)を表し、常に一覧の先頭に出ます。

| id | 表示名 |
|---|---|
| `shukei` | 朱罫帳 |
| `fieldnote` | 計測票 FIELDNOTE |
| `orbital` | Orbital Zero-G |
| `paper-tape` | 紙テープ / PAPER TAPE |

組み込みのスキンは exe に同梱されていて読み取り専用です。手本として「コピーして編集」(§9)で複製できます。ソースは `skins/builtin/<id>/` にあります。

## 2. フォルダ構成

利用者のスキンは次の場所に置きます。設定ウィンドウの「スキンフォルダを開く」で開けます(無ければ作られます)。

```
%LOCALAPPDATA%\widget-go\skins\
└── my-first-skin\        # フォルダ名(スキンの id と一致していなくてもよい)
    ├── skin.json
    └── skin.css
```

- `skins\` の直下にある**フォルダ 1 つが 1 スキン**です。フォルダの中に `skin.json` と、`skin.json` の `css` が指す CSS ファイルを置きます。フォルダ以外のファイル(`skins\` 直下のファイルなど)は無視されます。
- 画面に出るスキンの識別は `skin.json` の `id` で決まり、フォルダ名ではありません。フォルダ名に使えない文字は `/` `\` `:` で、末尾を `.` や空白にもできません。
- シンボリックリンク(リンクのフォルダ、フォルダ外を指す CSS)は使えません。
- zip でインポートすると、`skins\<id>\` に保存されます。このとき保存されるのは `skin.json` と CSS ファイルの 2 つだけです(`skin.json` は整形し直して書かれます)。
- `skins\` は、一覧を取得するたび(設定ウィンドウを開いたとき、「再読み込み」を押したときなど)に再スキャンされます。

## 3. skin.json

```json
{
  "schema": 1,
  "id": "my-first-skin",
  "name": "はじめてのスキン",
  "author": "あなたの名前",
  "version": "1.0.0",
  "description": "説明文(省略可)",
  "css": "skin.css",
  "swatch": ["#101820", "#ff7a59", "#f6f1e9"],
  "startupBackground": { "dark": "#101820", "light": "#f6f1e9" }
}
```

| フィールド | 必須 | 規則 |
|---|---|---|
| `schema` | 必須 | 整数の `1` のみ。それ以外は拒否 |
| `id` | 必須 | 正規表現 `^[a-z0-9][a-z0-9-]{0,31}$`(小文字の英数字とハイフン、32 文字まで、先頭はハイフン不可)。`default` と、組み込みの 4 つの id、すでにある利用者スキンの id とは重複不可 |
| `name` | 必須 | 1〜40 文字(バイトではなく文字数) |
| `author` | 任意 | 0〜100 文字 |
| `version` | 任意 | 0〜20 文字 |
| `description` | 任意 | 0〜200 文字 |
| `css` | 任意(既定 `skin.css`) | 正規表現 `^[A-Za-z0-9._-]+\.css$`。`skin.json` と同じフォルダ内のファイル名で、サブフォルダや `..` は不可 |
| `swatch` | 任意 | `#RRGGBB` の文字列を**ちょうど 3 つ**。設定画面の色見本。省略するか 3 つ書くかのどちらかで、`[]` や 2 つ以下・4 つ以上は拒否される |
| `startupBackground` | 任意 | `dark` / `light` にそれぞれ `#RRGGBB`。起動時に WebView2 が最初に塗る背景色で、ちらつき対策。省略した側は、従来の既定色になる |

- `skin.json` は 16 KiB 以下、UTF-8(先頭の BOM は可)で書きます。上の表にないフィールドは無視されます。
- 色は `#RRGGBB` の 6 桁だけです(`#fff` や `rgb()` は不可)。大文字小文字はどちらでも構いません。
- `startupBackground` は、ウィンドウが現れる前の一瞬の背景色です。スキンの `--container-bg` に近い色にすると、起動時のちらつきが目立ちません。どちらを使うかは、そのときのテーマ(ダーク / ライト / システムの結果)で決まります。設定ウィンドウのタイトルバーの色には影響しません。

### skin.css の検証

CSS は読み込み時(一覧の再スキャンのたび)に次の検査を受け、通らないスキンは一覧に出ず、「読み込めなかったスキン」に理由つきで出ます(§12)。

- UTF-8 で、256 KiB 以下(先頭の BOM は取り除かれる)
- 次の文字列を含まない(大文字小文字は区別しない): `</style`、`@import`、`expression(`、`javascript:`、`-moz-binding`、`behavior:`
- `url(` は、直後(空白や引用符を除いたところ)が `data:image/` のときだけ使える
- コメントを除いた版と、CSS のエスケープ(`\75 rl(` のような書き方)を戻した版でも、同じ検査を受ける

注意点があります。

- 検査は**コメントの中身にもかかります**。コメントに `@import` や、`url(` のあとに `data:image/` 以外を書いても拒否されます。
- `behavior:` は、直前が英数字・`-`・`_` でない場合だけ拒否します。IE 由来の `behavior` プロパティ(`behavior: url(…)`)が対象で、`scroll-behavior: smooth` や `overscroll-behavior: contain` は使えます。
- `@font-face` でフォントを同梱することはできません。`url(data:font/…)` は `data:image/` ではないので拒否されます。使えるのは、利用者の PC に入っているフォントを `font-family` で名指しする方法だけです。
- SVG の参照(`url(#id)`)も `data:image/` ではないので拒否されます。

## 4. skin.css の書き方

### セレクタの規則(厳守)

スキンのセレクタは、必ず **`body[data-skin="<id>"]`** で始めます。

- 属性値は**二重引用符**のままにします。「コピーして編集」(§9)は、CSS の中の文字列 `data-skin="<id>"` を置換して別 id のスキンを作るので、単引用符や引用符なし(`body[data-skin=x]`)にすると、複製したスキンが元のスキンの id を指したままになります。
- `<id>` は `skin.json` の `id` と**同じ**にします。違うと、そのスキンを選んでも何も当たりません。
- 本体は有効なスキンの CSS だけを `<style id="skin-css">` に入れ、`<body data-skin="<id>">` を付けます。他のスキンの CSS は読み込まれません。

### ダーク / ライトの 2 系統

テーマがライトのとき、`<body>` に `theme-light` クラスが付きます。そこで、次の 2 系統で書きます。

```css
/* ダーク(と、ライトとの共通部分) */
body[data-skin="my-first-skin"] { --section-value: #ff7a59; }
/* ライト */
body[data-skin="my-first-skin"].theme-light { --section-value: #c4421f; }
```

- 本体のライト用の変数は `body.theme-light { … }`(詳細度 0,1,1)で定義されています。スキンの `body[data-skin="x"]`(0,1,1)も同じ詳細度で、`<style id="skin-css">` が `<head>` の最後に置かれるため、**順序でスキンが勝ちます**。
- そのため、ダーク用の `body[data-skin="x"] { --foo: … }` は**ライトでも効きます**。ダーク用に上書きした変数は、ライト用の `body[data-skin="x"].theme-light { … }` でも**同じ変数を必ず書き直してください**。書かないと、本体のライト用の値ではなく、スキンのダーク用の値がライトに漏れます。
- 色以外のプロパティ(角丸、フォント、余白など)がテーマで変わらないなら、共通として `body[data-skin="x"] …` だけで書けます。

### `.compact` の罠(必読)

コンパクト表示(幅 130px)では、`<div class="container" id="dragArea">` に `compact` クラスが付きます。`theme-light` は `<body>` に、`compact` は `#dragArea`(= body の子孫)に付く点に注意してください。

- ライト: `body[data-skin="x"].theme-light …`
- コンパクト: `body[data-skin="x"] .compact …`(または `.container.compact`)

スキンのセレクタは、頭に `body[data-skin="x"]` が付くぶん、本体の `.compact .section-value`(詳細度 0,2,0)より強くなります。そのため、**本体が `.compact …` で別の値を指定しているプロパティを、スキンが `.compact` なしで上書きすると、コンパクトの寸法が崩れます**(幅 130px に収まらなくなります)。上書きするなら、`body[data-skin="x"] .compact …` の対応するルールも必ず書いてください。

本体が `.compact` で値を変えている主なもの:

| 本体のセレクタ | 変えているプロパティ |
|---|---|
| `.container.compact` | `padding`(`23px 4px 4px 4px`) |
| `.compact .top-btns` / `.compact .top-btn` | `top` `right` `gap` / `width` `height` `font-size` |
| `.compact .section` | `padding`(`4px 6px`)、`margin-bottom`(`3px`) |
| `.compact .flip-card > .flip-face` | `padding`(`4px 6px`) |
| `.compact .section-icon` | `font-size`(`12px`)、`margin-right` |
| `.compact .section-title` / `.compact .section-value` | `font-size`(`11px`) |
| `.compact .ai-section` ほか | `background` `border` `padding` など(AI 区画の見た目を行に移す) |

さらに、次の点にも注意してください。

- `.section` に `padding` を付けると、Wi-Fi / ネットワーク区画(`.wifi-section`。本体は `padding:0` で、余白は `.flip-face` が持つ)まで巻き込みます。`padding` を変えたいときは `.section:not(.wifi-section)` と `.flip-card > .flip-face` を分けて書き、それぞれに `.compact` の対応も書いてください。
- `.compact` の他にも、状態を表すクラス(`.flipped` `.ethernet-mode` `.collapsed` `.ai-rot` など。§7)で本体が値を変えているプロパティは、同じ理由で、スキン側にも対応するルールが必要です。
- 「コンパクト用の短縮ラベル」(`Net` `LAN`)は、`.compact .section-title[data-compact] { font-size:0 }` と `::before` で出しています。タイトルの `font-size` を上書きするときは、この仕組みを壊さないようにします。

### 詳細度で勝てないもの

次の本体のルールは、スキンのセレクタでは上書きできません。

- `!important` が付いたもの: `.is-hidden { display:none !important }`、`.progress-fill.warn` / `.progress-fill.crit` の `background`(警告色・危険色)
- ID を含むセレクタ: `#dragArea:not(.compact) .ai-section.collapsed.ai-rot …`(AI 区画の折りたたみ + 反転表示)

スキンの側からも `!important` を付ければ上書きできますが、状態の見分けに使われている色(警告・危険)は残すことをおすすめします。

## 5. 使えるもの・使えないもの(CSP による制約)

ウィジェット本体のページは、次の CSP(Content-Security-Policy)付きで配信されます。

```
default-src 'none'; script-src 'nonce-…'; style-src 'unsafe-inline'; connect-src 'self'; img-src 'self' data:
```

このため、スキンでは次のようになります。

| やりたいこと | 可否 |
|---|---|
| 外部の Web フォント(`@import` や `@font-face` の外部 URL、Google Fonts など) | **不可**(`@import` は検証で拒否。外部 URL は CSP でも遮断) |
| フォントファイルの同梱(`data:font/…`) | **不可**(検証で拒否。font-src も無い) |
| PC に入っているフォントの `font-family` での指定 | 可(その PC にフォントがない場合は、次の候補に落ちる) |
| 外部画像(`url(https://…)`) | **不可** |
| 画像の埋め込み(`url("data:image/svg+xml,…")` `url("data:image/png;base64,…")`) | 可(`data:image/` だけ) |
| `@import` | **不可** |
| `<link rel="stylesheet">` | 使えない(スキンの CSS は、本体の JS が取得して `<style id="skin-css">` に入れる) |
| スクリプト(`<script>`、`javascript:`、`expression()`) | **不可** |

`data:image/svg+xml,…` を引用符なしで書くと、`#` や `(` で CSS が壊れやすいので、`url("data:image/svg+xml,…")` のように引用符で囲み、`#` は `%23` にしてください。

## 6. CSS 変数の一覧

本体は、見た目の主な色を CSS 変数にしています。スキンは、まず**変数を上書きする**のが最も簡単で壊れにくい方法です。変数は `ui/index.html` の `:root`(ダーク)と `body.theme-light`(ライト)に定義されていて、表の値は本体の既定値です。

### 基本の変数(スキン導入前からあるもの)

| 変数 | 用途 | 既定(ダーク) | 既定(ライト) |
|---|---|---|---|
| `--bg-grad` | `body` の背景 | `linear-gradient(135deg, #0d0d12 0%, #16161e 100%)` | `linear-gradient(135deg, #e6e9f5 0%, #cdd6ec 100%)` |
| `--container-bg` | ウィンドウ全体(`.container`)の背景 | `rgba(16,16,22,0.97)` | `rgba(245,246,252,0.96)` |
| `--text-color` | 本文の文字色 | `#f0f0f5` | `#24263a` |
| `--muted-color` | 補助的な文字色(ラベル、モデル名など) | `#a8a8b8` | `#606078` |
| `--section-bg` | 区画の背景 | `rgba(255,255,255,0.05)` | `rgba(0,0,0,0.04)` |
| `--section-border` | 区画の枠線の色 | `rgba(255,255,255,0.10)` | `rgba(0,0,0,0.1)` |
| `--section-title` | 区画の見出しの文字色 | `#d8d8e8` | `#3b3b5a` |
| `--section-value` | 区画の値(右端の数値)の文字色 | `#7bffe0` | `#0a7f68` |
| `--bar-track` | バーの溝の色 | `rgba(255,255,255,0.12)` | `rgba(0,0,0,0.1)` |
| `--core-track` | コアごとのバーの溝の色 | `rgba(255,255,255,0.10)` | `rgba(0,0,0,0.08)` |
| `--net-canvas-bg` | ネットワークグラフの背景 | `rgba(0,0,0,0.35)` | `rgba(0,0,0,0.06)` |
| `--btn-bg` | 右上のボタンなどの背景 | `rgba(255,255,255,0.12)` | `rgba(0,0,0,0.08)` |
| `--btn-bg-hover` | 同、ホバー時 | `rgba(255,255,255,0.22)` | `rgba(0,0,0,0.16)` |
| `--btn-color` | 同、文字色 | `#bbb` | `#444` |
| `--input-bg` | (設定ウィンドウと共通の名前。ウィジェット本体では使われていない) | `rgba(255,255,255,0.05)` | `rgba(0,0,0,0.04)` |
| `--input-border` | (同上) | `rgba(255,255,255,0.14)` | `rgba(0,0,0,0.15)` |

### スキン用に新設した変数(直書きだった色)

既定値は従来の見た目と同じです。Canvas のネットワークグラフの色(`--net-*`)も、描画のたびに本体の JS がこれらの変数を読みます(読めない・色として不正なときは従来の色)。テーマやスキンが変わると描き直されます。

| 変数 | 用途 | 既定(ダーク) | 既定(ライト) |
|---|---|---|---|
| `--core-bar-bg` | コアごとのバー(`.core-bar`)の `background` | `linear-gradient(180deg,#8a9cff,#b07cff)` | 同左 |
| `--core-value-color` | コアごとの数値(`.core-value`)の文字色 | `#fff` | `#fff` |
| `--core-value-shadow` | 同、`text-shadow` | `0 1px 2px rgba(0,0,0,0.6)` | `0 1px 2px rgba(0,0,0,0.35)` |
| `--memory-used-color` | メモリの使用量(`.memory-details .used`)の色 | `#ffa3ff` | `#e85ad0` |
| `--wifi-ok-color` | 接続中の表示(`.wifi-connected`)の色 | `#7fffaa` | `#1aa86b` |
| `--wifi-ng-color` | 未接続の表示(`.wifi-disconnected`)の色 | `#ff8a8a` | `#d33` |
| `--gpu-fill-bg` | GPU のバー(`.gpu-fill`)の `background` | `linear-gradient(90deg,#6ad7ff,#5a8cff)` | 同左 |
| `--npu-fill-bg` | NPU のバー(`.npu-fill`)の `background` | `linear-gradient(90deg,#ffd36a,#ff9f5a)` | 同左 |
| `--disk-fill-bg` | Disk のバー(`.disk-fill`)の `background` | `linear-gradient(90deg,#7bffb8,#2fbf8f)` | 同左 |
| `--work-fill-bg` | 勤務時間のバー(`.work-fill`)の `background`(黄・赤・灰色のときは下の変数や `--muted-color` に置き換わる) | `linear-gradient(90deg,#7bd0ff,#5a8cff)` | 同左 |
| `--work-over` | 勤務時間に到達したときの色(`.over`。数値・見出し・バー) | `#facc15` | `#d9a400` |
| `--work-late` | 赤表示の時刻以降の色(`.late`。同上と、`.wk-err`) | `#ef6a6a` | `#d33` |
| `--pomo-fill-bg` | ポモドーロのバー(`.pomo-fill`)の `background`(休憩・一時停止・未開始のときは下の変数や `--muted-color` に置き換わる) | `linear-gradient(90deg,#ff9f7a,#ef6a5a)` | 同左 |
| `--pomo-break` | ポモドーロの休憩の色(`.break`。数値・見出し・バー) | `#7fffaa` | `#1aa86b` |
| `--net-rx` | 受信(↓)の凡例の色と、グラフの線の色。Disk の読み取りにも使う | `#7fffaa` | `#1aa86b` |
| `--net-rx-fill` | 受信のグラフの塗り。Disk の読み取りにも使う | `rgba(127,255,170,0.25)` | `rgba(26,168,107,0.20)` |
| `--net-tx` | 送信(↑)の凡例の色と、グラフの線の色。Disk の書き込みにも使う | `#ffa3ff` | `#b74da6` |
| `--net-tx-fill` | 送信のグラフの塗り。Disk の書き込みにも使う | `rgba(255,163,255,0.22)` | `rgba(183,77,166,0.18)` |
| `--net-empty-color` | グラフの「計測中...」の文字色 | `rgba(255,255,255,0.35)` | `rgba(0,0,0,0.4)` |

### AI 使用量の変数

AI 使用量(Claude / Codex)の区画で使う変数です。

| 変数 | 用途 | 既定(ダーク) | 既定(ライト) |
|---|---|---|---|
| `--claude-a` / `--claude-b` | Claude のドットとバーのグラデーションの両端 | `#f0a27a` / `#d06b5b` | `#e58f63` / `#c4553f` |
| `--codex-a` / `--codex-b` | Codex のドットとバーのグラデーションの両端 | `#7be0c0` / `#34b3d6` | `#2fbf94` / `#1f8fb3` |
| `--warn` | 警告(使用率 60% 以上)の色 | `#facc15` | `#d9a400` |
| `--crit` | 危険(使用率 80% 以上)の色 | `#ef6a6a` | `#d33` |
| `--ok` | 正常(● の印)の色 | `#7fffaa` | `#1aa86b` |
| `--chip-warn-bg` / `--chip-warn-fg` | 警告チップの背景 / 文字 | `rgba(250,204,21,.14)` / `#ffd84d` | `rgba(217,164,0,.16)` / `#8a6500` |
| `--chip-login-bg` / `--chip-login-fg` | 「未ログイン」チップの背景 / 文字 | `rgba(90,160,255,.16)` / `#9cc6ff` | `rgba(30,100,220,.12)` / `#1d5fc2` |

### 変数にしていない色(クラスを直接上書きする)

次の色は変数になっていません。変えたいときは、クラスを `body[data-skin="x"]` つきで上書きします。

| クラス | 内容 |
|---|---|
| `.cpu-fill` / `.memory-fill` | CPU・メモリのバーの `background`(グラデーション) |
| `.clock-fill`、`.clock-fill.clk-low`、`.clock-fill.clk-high` | クロックのバー。ベースクロックとの差で、通常 / 低(緑)/ 高(橙)が切り替わる |
| `.clock-base-marker` | クロックのバーのベース位置の印(`background`) |
| `.pin-btn.active` | 📌 が有効のときの背景(`#4CAF50`) |
| `.compact-btn.expanded` | コンパクト時の切替ボタンの背景(`#5aa0ff`) |
| `.ai-act`、`.ai-act.quiet` | AI 区画のボタン |

電源モードのスライダーは `.power-mode` 内のローカル変数 `--pm-color` `--pm-text` で色を決め、`--pm-fill` は JS がスタイル属性で書き込みます。`--pm-fill` はスキンから上書きできません。

## 7. 実 DOM の主なクラスと data-sec 属性

スキンの当て先になる、`ui/index.html` の実際の構造です。ウィジェットは `<body>` の中の `<div class="container" id="dragArea">` 1 つで、`compact` クラスはこの要素に付きます。

```
body[data-skin="<id>"]  (テーマがライトのとき .theme-light も付く)
└── #dragArea.container  (コンパクトのとき .compact も付く)
    ├── .top-btns
    │   ├── .top-btn.pin-btn#pinBtn   (有効なとき .active)
    │   ├── .top-btn.settings-btn#settingsBtn
    │   ├── .top-btn.compact-btn#compactBtn   (コンパクトのとき .expanded)
    │   └── .top-btn.tray-btn#closeBtn
    └── .section[data-sec="…"]  × 区画の数
        ├── .section-header
        │   ├── .section-icon   (絵文字)
        │   ├── .section-title
        │   ├── .section-value
        │   └── button.chev[data-sec="…"]   (折りたたみ)
        └── .sec-body …
```

### 区画(`data-sec`)

`data-sec` は区画ごとに付いています。**`.chev`(折りたたみボタン)にも同じ `data-sec` が付いている**ため、区画だけを選ぶときは `.section[data-sec="cpu"]` のようにクラスも付けてください(`[data-sec="cpu"]` だけだとボタンも当たります)。

| `data-sec` | 区画 | 主なクラス(中身) |
|---|---|---|
| `cpu` | CPU | `.progress-bar.cpu-progress` > `.progress-fill.cpu-fill`、`.clock-row`(`.clock-label` `.clock-value` `.clock-base`)、`.progress-bar.clock-progress` > `.clock-fill`(+ `.clock-base-marker`)、`.power-mode`(`.pm-slider` `.pm-labels`)、`.model-name.cpu-model`、`.cores-container` > `.core-item`(`.core-label` `.core-bar-container` > `.core-bar`、`.core-value`) |
| `memory` | メモリ | `.progress-bar.mem-progress` > `.progress-fill.memory-fill`、`.memory-details`(`.used`) |
| `disk` | Disk と I/O(`.wifi-section.disk-section`。`network` と同じ `.flip-card` の作り) | `.flip-card` > 表 `.flip-face.flip-front`(`.disk-body` > `.progress-bar.disk-progress` > `.progress-fill.disk-fill`、`.disk-info` > `.disk-models`(物理ディスクの製品名。1 つにつき `.model-name` が 1 行。取れないときは空)と `.disk-drives`。ドライブごとの `.wifi-row.disk-drive` は使用率 90% 以上で `.disk-warn`)と、裏 `.flip-face.flip-back`(`.disk-body.disk-io-num` = アクティブな時間の `.disk-progress` と `.disk-io` > `.wifi-row` > `.wifi-label` `.wifi-value`、`canvas.net-canvas.disk-graph-part`、`.net-legend.disk-graph-part` > `.net-leg-rx`(読み取り) `.net-leg-tx`(書き込み))。裏面は設定で数値(`.disk-io-num`)かグラフ(`.disk-graph-part`)の一方だけが出る(区画に `.disk-io-graph` が付くとグラフ)。コンパクトでは `.disk-body` が隠れる |
| `gpu` | GPU(`.accel-section`) | `.progress-bar.gpu-progress` > `.progress-fill.gpu-fill`、`.accel-info`(`.model-name`、`.wifi-row` > `.wifi-label` `.wifi-value`) |
| `npu` | NPU(`.accel-section`) | `.progress-bar.npu-progress` > `.progress-fill.npu-fill`、`.model-name.npu-model` |
| `network` | Wi-Fi / Ethernet とトラフィック(`.wifi-section`) | `.flip-card` > 表 `.flip-face.flip-front`(`.wifi-info`、`.wifi-row`、`.wifi-label`、`.wifi-value`、`.wifi-connected` / `.wifi-disconnected`)と、裏 `.flip-face.flip-back`(`.net-peak-toggle`、`canvas.net-canvas`、`.net-legend` > `.net-leg-rx` `.net-leg-tx`) |
| `ai` | AI 使用量(`.ai-section`。既定では `.is-hidden`) | `.ai-refresh`、`.ai-compact` > `.ai-crow`(`.claude` / `.codex`、`i`、`.ai-cname`、`.ai-cval`)、`.sec-body` > `.prov`(`.claude` / `.codex`、`.prov-head`、`.prov-name`、`.plan`、`.prov-state`、`.wrow`(`.lab` `.progress-bar` `.pct`)、`.wreset`、`.extra`、`.chip`(`.warn` `.login` `.info`)、`.ai-act`) |
| `work` | 勤務時間タイマー(`.work-section`。既定では `.is-hidden`) | `.section-value#work-head`(`実勤務時間 (15分切り下げ)`。状態クラス `.idle` / `.paused` / `.over` / `.late`)、`.sec-body` > `.wk-cap`、`.wk-big`(`small`。見出しと同じ状態クラス)、`.progress-bar` > `.progress-fill.work-fill`(同)、`.wifi-row`(開始 = `.wk-nudge` ×2 と `.wk-start` と `.wk-time`、目標、昼休み)、`.wk-note`(押下と補正)、`.wk-err`、`.wk-chip`(昼休み中・終了)、`.wk-btns` > `.wk-btn`(`.primary`)。コンパクトでは `.sec-body` とタイトルが隠れる |
| `pomodoro` | ポモドーロタイマー(`.pomo-section`。既定では `.is-hidden`) | `.section-value#pomo-head`(残り時間。状態クラス `.idle` / `.paused` / `.break`)、`.pm-mark`(集中モードの印。集中モードが働いている間だけ表示)、`.sec-body` > `.pm-cap`(区間名)、`.pm-big`(残り時間。見出しと同じ状態クラス)、`.progress-bar` > `.progress-fill.pomo-fill`(同)、`.wifi-row`(回 = `.pm-dots`、今日の完了数)、`.pm-chip`(集中モード・一時表示・開始待ち)、`.pm-err`、`.pm-btns` > `.pm-btn`(`.primary`)。コンパクトでは `.sec-body`・タイトル・`.pm-mark` が隠れ、見出し 1 行(アイコンと残り時間)になる。コンパクトでは区画のクリックで 開始 / 一時停止 / 再開 |
| `ping` | Ping 監視(`.ping-section`。既定では `.is-hidden`) | `.section-value#ping-head`(代表 RTT。警告時は `.danger`)、`.sec-body` > `.ping-list` > `.ping-row`(`.ping-host`、`.ping-rtt`、`.ping-loss`。閾値超過時は `.danger`)。コンパクトでは `.sec-body` が隠れる |

区画はすべて `.section` で、Wi-Fi 区画と Disk 区画には `.wifi-section` が加わり(Disk は反転する区画で、Wi-Fi と同じ `.flip-card` の作りです)、本体の CSS では背景も枠もなく(`.flip-face` が持つ)、`padding:0` です。`.wifi-section` を Wi-Fi 専用のつもりで書いたスキンの規則は、Disk にも効きます。Wi-Fi だけに効かせたいときは `[data-sec="network"]` を併用してください。

### 状態を表すクラス

| クラス | 付く要素 | 意味 |
|---|---|---|
| `.theme-light` | `body` | ライトテーマ |
| `.compact` | `#dragArea` | コンパクト表示(幅 130px) |
| `.dragging` / `.resizing` | `#dragArea` / 同 | ドラッグ中 / リサイズ中 |
| `.collapsed` | `.section` | 折りたたみ中(`.sec-body` が隠れる。Wi-Fi は表の `.wifi-info` が隠れる) |
| `.flipped` | `.wifi-section` | 裏面(Wi-Fi はトラフィック、Disk は I/O)を表示中 |
| `.disk-io-graph` | `.disk-section` | Disk の裏面がグラフ表示(付いていないときは数値表示) |
| `.ethernet-mode` | `.wifi-section` | 有線接続の表示(`.wifi-wireless-only` が隠れ、`.wifi-ethernet-only` が出る) |
| `.last-visible` | `.section` | 表示中の最後の区画(下の余白がなくなる) |
| `.is-hidden` | 区画など | 非表示(`display:none !important`) |
| `.is-empty` | `.model-name` | モデル名が空(非表示) |
| `.ai-rot` / `.click-flip` | `.ai-section` | AI の折りたたみ時の表示方式(反転で交互表示 / クリックで反転) |
| `.idle` / `.paused` / `.break` | `.section-value`、`.pm-big`、`.pomo-fill`(`.pomo-section` の中) | ポモドーロの状態: 未開始 / 一時停止・開始待ち(どちらも灰色)/ 休憩(緑)。集中中はクラスなし(既定色) |
| `.idle` / `.paused` / `.over` / `.late` | `.section-value`、`.wk-big`、`.work-fill`(`.work-section` の中) | 勤務時間の状態: 未開始 / 昼休み中(どちらも灰色)/ 勤務時間に到達(黄)/ 赤表示の時刻以降(赤)。昼休み中は `.over` `.late` より優先して `.paused` になる |
| `.warn` / `.crit` / `.na` | `.progress-fill`、`.pct`、`.ai-cval` | 警告 / 危険 / 値なし |
| `.clk-low` / `.clk-high` | `.clock-fill` | ベースクロックとの差が -11% 以下 / +21% 以上 |
| `.pm-saver` / `.pm-balanced` / `.pm-best` | `.power-mode` | 電源モードの段階。`.locked`、`.err` も付くことがある |

CPU のコアごとの要素(`.core-item` ほか)、AI の `.prov` / `.ai-crow` は、JS がデータの更新に合わせて生成します。DOM が増減するものを選ぶときは、親から子孫セレクタで選んでください。

## 8. 最小のサンプルスキン(コピペで動く)

次の 2 ファイルを同じフォルダに保存すれば、そのまま動きます。id は `my-first-skin` です。変えるときは、`skin.json` の `id` と、CSS のすべての `body[data-skin="…"]` を同じ文字列に置換してください。

`skin.json`:

<!-- sample:skin.json -->
```json
{
  "schema": 1,
  "id": "my-first-skin",
  "name": "はじめてのスキン",
  "author": "あなたの名前",
  "version": "1.0.0",
  "description": "紺の地に珊瑚色のアクセントを置いた、最小のサンプル。",
  "css": "skin.css",
  "swatch": ["#101820", "#ff7a59", "#f6f1e9"],
  "startupBackground": { "dark": "#101820", "light": "#f6f1e9" }
}
```

`skin.css`:

<!-- sample:skin.css -->
```css
/* スキン「はじめてのスキン」(my-first-skin)。
   セレクタは必ず body[data-skin="my-first-skin"] から始める(二重引用符のまま)。 */

/* ---- ダーク(と共通) ---- */
body[data-skin="my-first-skin"] {
    --bg-grad: none;
    --container-bg: rgba(16,24,32,0.97);
    --text-color: #f6f1e9;
    --muted-color: #9aa7b2;
    --section-bg: rgba(255,122,89,0.06);
    --section-border: rgba(255,122,89,0.35);
    --section-title: #f6f1e9;
    --section-value: #ff7a59;
    --bar-track: rgba(246,241,233,0.14);
    --core-track: rgba(246,241,233,0.10);
    --core-bar-bg: #ff7a59;
    --memory-used-color: #7cc4e8;
    --net-rx: #7cc4e8;
    --net-rx-fill: rgba(124,196,232,0.22);
    --net-tx: #ff7a59;
    --net-tx-fill: rgba(255,122,89,0.22);
}

/* ---- ライト(ダークで上書きした変数は、ここでも必ず書き直す) ---- */
body[data-skin="my-first-skin"].theme-light {
    --bg-grad: none;
    --container-bg: rgba(246,241,233,0.97);
    --text-color: #1c2630;
    --muted-color: #5f6b75;
    --section-bg: rgba(196,66,31,0.05);
    --section-border: rgba(196,66,31,0.35);
    --section-title: #1c2630;
    --section-value: #c4421f;
    --bar-track: rgba(28,38,48,0.12);
    --core-track: rgba(28,38,48,0.08);
    --core-bar-bg: #c4421f;
    --memory-used-color: #1f6f99;
    --net-rx: #1f6f99;
    --net-rx-fill: rgba(31,111,153,0.18);
    --net-tx: #c4421f;
    --net-tx-fill: rgba(196,66,31,0.18);
}

/* ---- 形: 角を小さく、バーを細く ---- */
body[data-skin="my-first-skin"] .section,
body[data-skin="my-first-skin"] .flip-card > .flip-face { border-radius: 2px; }
body[data-skin="my-first-skin"] .progress-fill,
body[data-skin="my-first-skin"] .progress-bar { border-radius: 0; }

/* 値を大きくする。本体は .compact で 11px にしているので、コンパクト用も必ず書く */
body[data-skin="my-first-skin"] .section-value { font-size: 14px; }
body[data-skin="my-first-skin"] .compact .section-value { font-size: 11px; }
```

サンプルの意図:

- `--bg-grad: none` は、`body` の背景グラデーションを消す指定です。
- ダークとライトの両方に、同じ変数を書いています(ライトに漏れないように)。
- `.section-value` の `font-size` は、本体が `.compact` で値を変えているプロパティなので、`.compact` 用のルールを足しています。

## 9. 「コピーして編集」を使う作業手順

組み込みスキンを手本に作るのが近道です。

1. ウィジェットの ⚙ で設定ウィンドウを開き、スキンのカードで、手本にしたい組み込みスキンの「コピーして編集」を押します。`skins\<id>-copy\` に複製され、`skin.json` の `id` は `<id>-copy`、`name` は「<元の名前> (コピー)」になり、CSS の中の `data-skin="<id>"` が `data-skin="<id>-copy"` に置換されます。複製しただけでは有効になりません。
2. 「スキンフォルダを開く」でエクスプローラを開き、`<id>-copy` フォルダの `skin.css`(と必要なら `skin.json`)をテキストエディタで編集します。
3. 設定ウィンドウの「再読み込み」を押します。一覧が再スキャンされ、ウィジェットが有効なスキンの CSS を取り直すので、`skin.css` の編集がそのまま反映されます(同じスキンを選んだままでも、毎回取り直されます)。
4. 一覧の `<name> (コピー)` を選ぶと、有効になります。
5. 別の名前のスキンにしたいときは、`skin.json` の `id` と `name` を変え、CSS の `body[data-skin="<id>-copy"]` をすべて新しい id に置換します。

補足:

- 同じ組み込みスキンをもう一度コピーすると、`<id>-copy` が既にあるので「既にコピーがあります」と出て作られません(先に削除するか、フォルダ名と id を変えてください)。
- 組み込みスキンには「削除」はありません。利用者スキンは、カードの「削除」で消せます(フォルダごと削除。選択中ならデフォルトに戻る)。

## 10. zip の作り方とインポート手順

### 作り方

`skin.json` と CSS ファイルの **2 ファイルだけ**を選んで zip にします(エクスプローラで 2 つを選び、右クリック → 「ZIP ファイルに圧縮する」)。フォルダごと圧縮する場合は、zip の中身が次のどちらかになるようにしてください。

- ルート直下に `skin.json` と CSS がある
- **唯一のトップレベルフォルダ**があり、その直下に `skin.json` と CSS がある(`my-first-skin/skin.json` など)

zip の制約:

- zip 本体は 1 MiB 以下、エントリ数は 20 以下、展開後の合計は 1 MiB 以下
- `skin.json` と CSS 以外のエントリは**保存されません**が、検査の対象にはなります。パスに `..`、絶対パス、ドライブ指定(`:`)、バックスラッシュ(`\`)を含むエントリ、シンボリックリンクがあると、zip 全体が拒否されます(zip-slip 対策)
- フォルダ形式の zip では、すべてのエントリが同じトップレベルフォルダの下になければなりません。ルートに `README` が混ざっていたり、`__MACOSX/` が入っていたりすると、「トップレベルフォルダは 1 つにしてください」で拒否されます
- 同じパスのエントリが 2 つあると拒否されます

PowerShell で作る例(2 ファイルだけを圧縮するので、パスの問題が出ません):

```powershell
Compress-Archive -Path .\my-first-skin\skin.json, .\my-first-skin\skin.css -DestinationPath .\my-first-skin.zip
```

### インポート

1. ⚙ の設定ウィンドウで、スキンのカードの「インポート…」を押し、zip を選びます。
2. 検査を通れば `skins\<id>\` に保存され、一覧に出ます(**自動では有効になりません**)。一覧から選ぶと有効になります。
3. すでに同じ id の利用者スキンがあるときは、「同じ ID のスキンが既にあります。上書きしますか？」と確認が出ます。「上書き」を選ぶと、既存のスキンのフォルダを置き換えます(失敗したときは元に戻ります)。
4. `default` と、組み込みの 4 つの id は予約済みで、インポートできません。

## 11. 保存先と設定

| 項目 | 場所 |
|---|---|
| 利用者のスキン | `%LOCALAPPDATA%\widget-go\skins\<フォルダ名>\` |
| 有効なスキン | `%LOCALAPPDATA%\widget-go\config.json` の `skin`(`default` または空文字はスキンなし。キーが無い古い `config.json` はそのまま読める) |
| 組み込みスキン | exe に同梱(`skins/builtin/<id>/`)。読み取り専用 |

有効にしていたスキンが削除されたり、壊れたりして読み込めなくなったときは、画面ではデフォルトに戻ります(`config.json` の値はそのまま)。スキンを直して「再読み込み」すると、また使えます。設定画面から利用者スキンを削除したときは、有効だったなら `config.json` の値も `default` に戻します。

## 12. トラブルシュート

### 読み込めなかったスキンの見方

読み込みに失敗した利用者スキンは、スキン一覧には出ず、設定ウィンドウのスキンのカードの下に赤い枠で「**読み込めなかったスキン**」として、`<フォルダ名>: <理由>` の形で出ます。フォルダ名を手がかりに、`skins\` の中の該当フォルダを直してから「再読み込み」を押してください。

| 表示される理由 | 原因と対処 |
|---|---|
| `skin.json を読み取れません` | JSON の文法の誤り(カンマ、引用符など)。`skin.json` の型の誤り(`schema` が文字列、など)もここに出る |
| `skin.json の schema は 1 にしてください` | `schema` が無い、または `1` 以外 |
| `skin.json の id が不正です` | `id` が規則(§3)に合わない。大文字、アンダースコア、33 文字以上、`default` など |
| `skin.json の文字数が不正です` | `name` が空または 41 文字以上、`author` `version` `description` が長すぎる |
| `CSS ファイル名が不正です` | `css` が `^[A-Za-z0-9._-]+\.css$` に合わない(`.css` 以外、サブフォルダ、日本語名など) |
| `swatch は色を 3 つ指定してください` / `色は #RRGGBB で指定してください` / `swatch の色が不正です` | `swatch` が 3 つでない(`[]` を含む)、色が `#RRGGBB` でない、空文字が混ざっている |
| `スキンのファイルを開けません` | `skin.json` か、`css` が指すファイルが無い |
| `スキンのファイルサイズまたは形式が不正です` / `スキンのファイルを読み取れません` | `skin.json` が 16 KiB を、CSS が 256 KiB を超えている、またはファイルでない |
| `CSS のサイズまたは文字コードが不正です` | UTF-8 でない(Shift_JIS で保存したなど)、256 KiB を超えている |
| `CSS に許可されていない記述があります` | §3 の禁止文字列を含む(コメント内も対象)。`@import`、`javascript:` なども |
| `CSS の url は data:image/ のみ使用できます` | `url(` の直後が `data:image/` でない(外部画像、フォント、`url(#id)`、コメント内の `url(` を含む) |
| `予約済みの id は使用できません` | `id` が `default` または組み込みの 4 つのどれか |
| `skin.json の id が重複しています` | 同じ `id` のフォルダが 2 つ以上ある(**両方とも**一覧から外れる)。どちらかの `id` を変えるか、フォルダを削除する |
| `フォルダ名が不正です` / `リンクは使用できません` | フォルダ名に `\` `:` を含む、末尾が `.` か空白 / シンボリックリンク |
| `フォルダを開けません` | アクセス権などの問題 |

### インポートで拒否されたとき

画面に出る理由は次のとおりです(メッセージの全体は、スキン一覧のエラーと同じものも含みます)。

| 表示される理由 | 原因と対処 |
|---|---|
| `zip は 1 MiB 以下にしてください` / `zip の展開サイズが大きすぎます` | サイズが上限を超えている |
| `zip の形式またはエントリ数が不正です` | zip として読めない、またはエントリが 21 以上 |
| `zip のパスが不正です` / `zip のエントリが不正です` | `..` や `\`、絶対パス、`:` を含む、リンクが含まれる |
| `トップレベルフォルダは 1 つにしてください` | フォルダ形式で、複数のトップレベルがある(`__MACOSX` や `README` など)。§10 の「2 ファイルだけを圧縮」を使う |
| `CSS ファイルがありません` | `skin.json` の `css` が指すファイルが zip に無い(zip のフォルダ階層も確認) |
| `同じスキンが既にあります`(上書きの確認) | 同じ id の利用者スキンがある。上書きするか、`id` を変える |
| `重複したスキンの id を修正してください` | `skins\` に同じ id のスキンが 2 つ以上ある。先に片方を直す |
| 上記以外 | `skin.json` と CSS の検査の理由(上の表と同じ) |

### 見た目が思ったようにならないとき

- **何も変わらない**: `body[data-skin="…"]` の `…` が `skin.json` の `id` と一致しているか確認します。属性値を二重引用符で囲んでいるかも確認してください。
- **ライトだけ(またはダークだけ)おかしい**: ダークで上書きした変数を、ライト用の `.theme-light` のルールにも書いているか確認します(§4)。
- **コンパクトで崩れる**: `.compact` の罠(§4)です。`body[data-skin="x"] .compact …` の対応するルールを足します。
- **Wi-Fi 区画だけ余白がずれる**: `.section` に `padding` を付けていないか確認します。
- **警告色(オレンジ・赤)が変わらない**: `.progress-fill.warn` / `.crit` は `!important` なので、変数(`--warn` `--crit`)で変えます。
- **「再読み込み」しても反映されない**: エラー欄に理由が出ていないか確認します。壊れているスキンは、画面ではデフォルトに戻ります。
- **フォントが違う**: そのフォントが PC に入っていないと、次の候補に落ちます。`font-family` に代替を並べます。外部フォントは使えません(§5)。
