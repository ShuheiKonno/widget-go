# system-widget

Windows 向けのデスクトップ常駐型システムモニターウィジェットです。CPU 使用率・メモリ使用量・Wi-Fi / Ethernet 接続情報・ネットワークトラフィックをリアルタイムで表示します。

![Go](https://img.shields.io/badge/Go-1.21-00ADD8?logo=go&logoColor=white)
![Windows](https://img.shields.io/badge/Windows-only-0078D6?logo=windows&logoColor=white)

## 機能

- **CPU モニタリング** — 全体使用率 + コアごとの使用率をバーグラフで表示
- **CPU クロック周波数** — 現在の実効クロックを表示。ベースクロックに対する位置をバーで示し、ターボ動作中 (ベース超過) は橙色に変化
- **メモリモニタリング** — 使用率・使用量 / 総容量を表示
- **Wi-Fi 情報** — SSID・帯域 (2.4GHz/5GHz/6GHz)・規格 (Wi-Fi 4〜7)・信号強度・チャネル
- **Ethernet 情報** — Wi-Fi 未接続時に有線 NIC を自動検出し、アダプタ名とリンク速度 (10Mbps / 100Mbps / 1Gbps / 2.5Gbps / 5Gbps / 10Gbps) を表示。ドライバが `995Mbps` のように報告する場合も標準値にスナップ
- **ネットワークトラフィックグラフ** — Wi-Fi/Ethernet セクションをクリックすると 3D 反転で裏面に切替、送受信スループットを折れ線グラフで表示 (直近 2 分)。ヘッダのピーク値をクリックすると `KB/s` ⇔ `Mbps` の単位切替
- **フレームレスウィンドウ** — タイトルバーなしのコンパクトなデザイン、ドラッグで移動可能
- **表示モード** — 通常 / コンパクトの 2 段階 + タスクバー格納。コンパクト時の切替ボタンは色と形が変わり、tooltip も「フル表示」に切り替わる
- **表示オプション (⚙)** — モーダルから以下を設定
  - 配色テーマ (ダーク / ライト / システム追従)
  - 透過モード
  - CPU コア番号の表示 (0 始まり / 1 始まり)
  - タスクバー表示 (アイコンのみ / CPU 使用率)
- **システムトレイ (タスクバー格納)** — `_` ボタンで格納、トレイ設定が「CPU 使用率」の場合はアイコンに使用率を描画。ツールチップで概要確認
- **常に前面表示 (ピン留め)** — ワンクリックで最前面に固定
- **ウィンドウリサイズ** — 端をドラッグしてサイズ変更可能
- **IME Composition Window の自動復旧** — MS-IME の変換ウィンドウが画面左上に張り付く不具合を WinEvent Hook で検知し、IME コンテキストのリセット等で自動復旧 (v1.5.0〜)
- **DPI 対応** — 高 DPI ディスプレイでも正しくスケーリング
- **設定の永続化** — ウィンドウ位置・サイズ・各種設定を `%LOCALAPPDATA%\widget-go\config.json` に保存し、次回起動時に復元

## 必要環境

- Windows 10 以降
- [WebView2 Runtime](https://developer.microsoft.com/ja-jp/microsoft-edge/webview2/) (Windows 11 ではプリインストール済み)
- Go 1.25 以降 (ビルドする場合)

## ビルド

```bash
go build -ldflags "-H windowsgui" -o system-widget.exe .
```

`-H windowsgui` フラグによりコンソールウィンドウが表示されません。

## 実行

```bash
./system-widget.exe
```

起動時、記憶された位置があればそこに、無ければ画面右下にマージンを取って配置されます。

## 使い方

| 操作 | 説明 |
|------|------|
| ドラッグ | ウィンドウを移動 (終了時に位置を保存) |
| 端をドラッグ | ウィンドウをリサイズ (終了時にサイズを保存) |
| 📌 ボタン | 常に前面に表示をトグル |
| ⚙ ボタン | 表示オプション (テーマ / 透過 / Core 番号 / タスクバー表示) を開く |
| ▬ ⇄ ▢ ボタン | コンパクト表示とフル表示を切替。コンパクト時は青背景 `▢` になり tooltip が「フル表示」に |
| `_` ボタン | タスクバー (システムトレイ) に格納 |
| Wi-Fi/Ethernet セクション クリック | ネットワーク トラフィック表示と反転 |
| トレイアイコン クリック | ウィンドウを復元 |
| トレイアイコン 右クリック → 終了 | アプリケーションを完全終了 |

### 表示オプションの各設定

| 項目 | 値 | 説明 |
|------|------|------|
| テーマ | ダーク / ライト / システム | システムは `prefers-color-scheme` に追従 |
| 透過モード | チェック | レイヤードウィンドウで背景を半透明化 |
| CPU Core 番号 | 0 から / 1 から | コアラベルの起点番号を切替 |
| タスクバー (最小化時) | アイコンのみ / CPU 使用率 | トレイアイコンを 5×7 ドットフォントで CPU % に差替 |

## プロジェクト構成

```
widget-go/
├── main.go              # エントリポイント、WebView2 ウィンドウ、HTTP サーバー、Win32 API
├── ui/
│   └── index.html       # UI (HTML/CSS/JS)。//go:embed で main.go に埋め込む
├── sysinfo/
│   ├── cpu.go           # CPU 使用率取得 (gopsutil)
│   ├── cpufreq.go       # CPU クロック周波数取得 (PDH + CallNtPowerInformation)
│   ├── memory.go        # メモリ使用量取得 (gopsutil)
│   ├── wifi.go          # Wi-Fi 情報取得 (netsh)
│   ├── ethernet.go      # Ethernet 検出・リンク速度取得 (iphlpapi GetIfEntry2)
│   └── network.go       # ネットワーク送受信スループット取得 (gopsutil)
├── ime/
│   ├── watcher.go       # IME Composition Window の検知 (WinEvent Hook)
│   └── recovery.go      # 検知時の自動復旧処理
├── cmd/
│   └── genicon/         # アイコン生成ツール
├── winres/
│   ├── icon.ico         # アプリケーションアイコン
│   └── winres.json      # Windows リソース設定
├── go.mod
└── go.sum
```

## アーキテクチャ

ローカル HTTP サーバーをバックグラウンドで起動し、WebView2 がそのサーバーから配信される HTML/CSS/JS を描画するという構成です。

- **バックグラウンドコレクター** — CPU/クロック/メモリ/ネットワークトラフィックは 2 秒間隔、Wi-Fi/Ethernet は 3 秒間隔でキャッシュを更新
- **フロントエンド** — 2 秒間隔で `/api/system` をポーリングし UI を更新
- **ウィンドウ制御** — Win32 API を直接呼び出してフレームレス化・ドラッグ・リサイズ・トレイを実現

## 依存ライブラリ

- [go-webview2](https://github.com/jchv/go-webview2) — WebView2 バインディング
- [gopsutil/v4](https://github.com/shirou/gopsutil) — CPU・メモリ情報取得

## 設定ファイルの保存場所

起動時に以下のパスを作成し、ここに設定・WebView2 ユーザーデータを保存します。

```
%LOCALAPPDATA%\widget-go\
├── config.json       # ウィンドウ位置・サイズ・テーマ・透過・Core 番号・トレイ表示モードなど
└── WebView2\         # WebView2 ランタイムのユーザーデータ (キャッシュ等)
```

`config.json` は変更のたびに原子的に書き込まれる (`.tmp` → `rename`) ため、`os.Exit` やプロセス強制終了でも設定の欠落が起きません。

## アンインストール

1. **アプリを終了** — トレイアイコンを右クリック → 「終了」を選択。ウィンドウが表示されている場合は `_` ボタンでトレイに格納してから終了する
2. **実行ファイルを削除** — 任意の場所に置いた `system-widget.exe` を削除
3. **設定・WebView2 データを削除** — エクスプローラのアドレスバーに `%LOCALAPPDATA%\widget-go` を入れて開き、フォルダごと削除
4. **スタートアップに登録していた場合は解除** — `Win + R` → `shell:startup` で開くフォルダに `system-widget.exe` のショートカットを置いていたら削除

> 多重起動防止に使用しているミューテックス (`Global\widget-go-single-instance-mutex`) はプロセス終了時に自動解放されます。レジストリは使用しません。

## 変更履歴

### 1.7.0 (2026-08-24)

- **CPU クロック周波数の表示を追加** — CPU セクションに現在の実効クロックとベースクロックを表示し、ベースを基準としたバーで可視化。ベース超過 (ターボ動作) は橙色で区別する。コンパクト表示では非表示のため、コンパクト時の高さは従来どおり
- **クロックの取得方式** — PDH カウンタ `\Processor Information(_Total)\% Processor Performance` × ベースクロックで算出 (タスクマネージャーの「速度」と同じ方式)。`CallNtPowerInformation` の `CurrentMhz` は多くの環境でベース値に固定され変動しないため使用しない。ベースクロックは同 API の `MaxMhz` から取得。カウンタ名はロケール依存のため `PdhAddEnglishCounterW` で登録し、日本語版 Windows でも動作する
- **ウィンドウサイズの自動調整** — 既定サイズを 210×400 から 230×426 に変更。旧バージョンの保存値が残っている場合も、初回描画後に内容の実寸へ自動フィットする (幅は 230 未満のときのみ広げる)
- **不具合修正** — 起動 500ms 後の位置再適用が、フロント側の自動フィット直後にサイズまで巻き戻していた問題を修正 (再適用の対象を位置のみに変更)

### 1.6.0 (2026-07-11)

- **セキュリティ強化** — ローカル HTTP API に CSRF 防御を追加。起動毎のトークンを UI に埋め込み、全 `/api` 呼び出しをカスタムヘッダで検証。`Host` ヘッダ検証 (DNS rebinding 対策) と CSP を付与
- **不具合修正** — Explorer 再起動でトレイアイコンが消える問題 (`TaskbarCreated` 未対応) を修正。設定保存の競合 (lost update)、`windowHandle` のデータ競合、トレイメッセージループの `GetMessageW` 戻り値判定を修正
- **保守性** — `w.Window()` でウィンドウハンドルを直接取得し FindWindow ポーリングを撤廃。UI を `//go:embed` で外部ファイル化。CI (go vet / go test / golangci-lint) を追加し、gopsutil v4 へ移行
- **UX** — ドラッグ/リサイズの通信を最適化しガタつきを解消。下端リサイズのアンカーを修正。ネットワークグラフをライトテーマ対応化

### 1.5.0 (2026-05-21)

- **IME Composition Window の検知・自動復旧** — MS-IME の変換 (Composition) ウィンドウが画面左上に張り付く不具合を WinEvent Hook で監視し、IME コンテキストのリセット・フォーカス再付与で自動復旧する機能を追加。クールダウン・閾値・DryRun を `config.json` の `ime` セクションで調整可能

### 1.4.2 (2026-04-29)

- **VPN / 仮想アダプタを Ethernet 検出から除外** — Cato Networks Tunnel のような VPN クライアントの仮想アダプタが Windows 側で `TransmitLinkSpeed=100,000,000,000 bps`（100 Gbps のダミー値）を報告し、ウィジェットが物理 NIC ではなくこちらを拾って "100 Gbps" 表示になっていたのを修正。`MIB_IF_ROW2.InterfaceAndOperStatusFlags` の `HardwareInterface` ビット（物理 NIC のみ 1）と `TunnelType` を判定軸に追加し、さらに `MIB_IF_ROW2.Description`（"Cato Networks Tunnel" 等）にも既存の名前ヒント（`tun` / `vpn` / `virtual` 等）を適用。Alias（フレンドリ名）が "CatoNetworks" のように除外語を含まなくても確実に弾けるようにした。
- **ダークテーマをより黒く** — 背景グラデーションを `#3b3b6d → #2e4a7a` の濃紫青から `#0d0d12 → #16161e` のほぼ純黒に変更。`--container-bg` も `rgba(60,60,95,0.96)` → `rgba(16,16,22,0.97)` に。セクション枠の白オーバーレイ（`--section-bg`/`--section-border`/`--bar-track`）も控えめにして、黒地で輝度が浮きすぎないように調整。アクセント色（CPU シアン・メモリピンク・Wi-Fi 緑）はそのまま維持。

### 1.4.1 (2026-04-22)

- **Ethernet リンク速度表示を標準値にスナップ** — NIC ドライバによっては 1Gbps リンクでも `TransmitLinkSpeed` を `995,000,000 bps` と報告するため、ウィジェット UI もタスクバーのツールチップも `995 Mbps` と表示されていた。標準 Ethernet 速度 (10 / 100 / 1000 / 2500 / 5000 / 10000 Mbps) に対して ±2% 以内ならスナップする処理を Go 側 (`formatLinkSpeedShort`) と JS 側 (`formatLinkSpeed`) の両方に追加し、`1 Gbps` / `2.5 Gbps` のように正しく表示されるよう修正。

### 1.4.0 (2026-04-18)

- **表示オプション (⚙ ボタン + モーダル) を追加** — 配色テーマ (ダーク / ライト / システム追従)、透過モード、CPU Core 番号の起点 (0 / 1)、タスクバー表示モード (アイコン / CPU 使用率) を GUI で切替可能に。
- **タスクバーアイコンの CPU 使用率表示** — GDI で 16×16 の ARGB ビットマップを動的生成し、5×7 ドットフォントで数値を描画。トレイ格納時にリアルタイム更新。
- **設定の永続化を Go 側 `config.json` に変更** — ブラウザ `localStorage` は WebView2 の非同期 flush 挙動により `os.Exit(0)` で失われる場合があったため、Go 側で `%LOCALAPPDATA%\widget-go\config.json` に原子的書き込み (`.tmp` → `rename`) で保存する方式に変更。ウィンドウ位置・サイズ、ピン留め、コンパクト状態、透過、トレイ表示モード、テーマ、Core 番号、ネットワーク単位を記憶。
- **起動位置の改善** — 右下端にマージン 8px を確保、WebView2 の `SetSize(HintFixed)` による位置リセットを相殺するため 500ms 後に再配置。
- **タスクバー格納ボタンの再デザイン** — macOS 風の赤丸 `✕` は「閉じる」と誤解されるため、他ボタンと揃えた `_` (最小化) アイコンに変更。tooltip は「タスクバーに格納」。
- **コンパクト時の表示ボタン切替** — 通常時は `▬` で「コンパクト表示」、コンパクト時は青背景 `▢` で「フル表示」と切替状態を明示。
- **コンパクト時のネットワーク表示を簡潔化** — グラフと rx/tx 数値を非表示にし、ヘッダーのピーク値のみ表示。CPU / メモリ セクションと同じ高さに統一 (`.flip-face` の padding を compact 時も縮小)。
- **WebView2 の `DataPath` を固定** — 既定の `%APPDATA%\{exe 名}` 依存を止めて `%LOCALAPPDATA%\widget-go\WebView2\` に明示、プロファイル分離を防止。

### 1.3.0 (2026-04-17)

- **Wi-Fi 7 / Windows 11 新 netsh フォーマット対応** — Windows 11 build 26200 で `netsh wlan show interfaces` の出力フィールドが変わっており、BSSID と帯域が取れなくなっていたのを修正。
  - `BSSID` → `AP BSSID` 表記への改名に追随 (`HasPrefix` → `Contains` で "AP BSSID" も吸収)
  - 新フィールド `バンド` (ja) / `Band` (en) をパースし最優先で採用。Wi-Fi 6E/7 では 6 GHz と 2.4 GHz のチャネル番号が被るため、バンド行がないとチャネルだけでは帯域を決定できない
  - フォールバック帯域判定を 6 GHz 対応に拡張 (`ch >= 178` は UNII-8 上位として 6GHz 確定、`36-177` 5GHz、`1-14` 2.4GHz)
  - `バンド: 5 GHz` のような値を `5GHz` 形式に正規化する `normalizeBand` ヘルパを追加
- **前提条件**: `netsh wlan show interfaces` は Windows 11 で位置情報サービス ON が必須。グループポリシー `DisableLocation=1` が有効だと権限エラーで取得不能。

### 1.2.0 (2026-04-17)

- **Ethernet 接続の表示に対応** — Wi-Fi 未接続時に有線 NIC を検出し、アダプタ名と `iphlpapi.GetIfEntry2` 経由で取得した `TransmitLinkSpeed` (100Mbps / 1Gbps / 2.5Gbps / 10Gbps 等) を表示。セクションのアイコンとタイトルも `🔌 Ethernet` に切り替わり、トレイツールチップにも反映。
- **ネットワークトラフィックの単位切替を追加** — ネットワークグラフ右上の peak 表示をクリックすると `KB/s` ⇔ `Mbps` を切替。`localStorage` に記憶するため次回起動時も引き継がれる。
- **起動位置計算を改善** — `SPI_GETWORKAREA` (プライマリモニタ固定) を `MonitorFromWindow` + `GetMonitorInfoW` に差し替え、マルチモニタ環境でもウィンドウが実際に乗っているモニタの右下に揃うようにした。
- **コンパクトモードでのネットワーク欄幅バグを修正** — `.compact .section { padding: 5px 10px }` が CSS 特異度で `.wifi-section { padding: 0 }` を上書きしており、コンパクト時だけ左右にマージンが入って他セクションより狭くなっていたのを修正。

### 1.0.1 (2026-04-17)

- **ネットワークトラフィック表示機能を追加** — Wi-Fi セクションをクリックすると `rotateY(180deg)` のアニメーションで裏面に切り替わり、`<canvas>` で送受信スループットの折れ線グラフを描画。Rx (緑) / Tx (ピンク) を半透明塗り + ラインで重ねて表示、最低スケール 8KB/s、最大 60 サンプル (2 分間) をフロント側リングバッファに保持、単位は B/s〜GB/s で自動切替。
- **`sysinfo/network.go` を追加** — gopsutil の `net.IOCounters` を使い全 NIC 合計のバイトカウンタ差分からスループットを算出。初回呼び出しは基準値の仕込みのみで 0 を返す。
- **`SystemData` に `Network` を追加し、`startCollector` に `refreshNetwork()` を 2 秒間隔で追加** — キャッシュ済みの最新スループットを `/api/system` から返すようにした。
- **`✕` ボタンでタスクトレイ格納が時々失敗する不具合を修正** — WebView2 のウィンドウ取得が固定 200ms スリープ + `FindWindow` 一発だったため初期化遅延時に `windowHandle = 0` で固まり、`enterTray()` が `trayActive = true` だけ立てて本体が隠れない状態に陥っていた。50ms 間隔で最大 5 秒間ポーリングする形に変更し、`enterTray()` 側でも未取得時にリカバリを試みる安全弁を追加。
- **ビルドフラグを README に明記** — `go build -ldflags "-H windowsgui"` を付けないとコンソールウィンドウが出てしまうため、コマンド例に反映済み。

## ライセンス

MIT
