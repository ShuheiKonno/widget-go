# system-widget

Windows 向けのデスクトップ常駐型システムモニターウィジェットです。CPU 使用率・メモリ使用量・Wi-Fi / Ethernet 接続情報・ネットワークトラフィックをリアルタイムで表示します。

![Go](https://img.shields.io/badge/Go-1.21-00ADD8?logo=go&logoColor=white)
![Windows](https://img.shields.io/badge/Windows-only-0078D6?logo=windows&logoColor=white)

## 機能

- **CPU モニタリング** — 全体使用率 + コアごとの使用率をバーグラフで表示
- **メモリモニタリング** — 使用率・使用量 / 総容量を表示
- **Wi-Fi 情報** — SSID・帯域 (2.4GHz/5GHz)・規格 (Wi-Fi 4〜7)・信号強度・チャネル
- **Ethernet 情報** — Wi-Fi 未接続時に有線 NIC を自動検出し、アダプタ名とリンク速度 (100Mbps / 1Gbps / 2.5Gbps / 10Gbps 等) を表示
- **ネットワークトラフィックグラフ** — Wi-Fi/Ethernet セクションをクリックすると 3D 反転で裏面に切替、送受信スループットを折れ線グラフで表示 (直近 2 分)。ヘッダのピーク値をクリックすると `KB/s` ⇔ `Mbps` の単位切替 (`localStorage` に記憶)
- **フレームレスウィンドウ** — タイトルバーなしのコンパクトなデザイン、ドラッグで移動可能
- **3 段階表示モード** — 通常 → コンパクト → システムトレイ
- **システムトレイ** — トレイ格納時は CPU 使用率をアイコンに数値表示、ツールチップで概要確認
- **常に前面表示 (ピン留め)** — ワンクリックで最前面に固定
- **ウィンドウリサイズ** — 端をドラッグしてサイズ変更可能
- **DPI 対応** — 高 DPI ディスプレイでも正しくスケーリング

## 必要環境

- Windows 10 以降
- [WebView2 Runtime](https://developer.microsoft.com/ja-jp/microsoft-edge/webview2/) (Windows 11 ではプリインストール済み)
- Go 1.21 以降 (ビルドする場合)

## ビルド

```bash
go build -ldflags "-H windowsgui" -o system-widget.exe .
```

`-H windowsgui` フラグによりコンソールウィンドウが表示されません。

## 実行

```bash
./system-widget.exe
```

起動するとフレームレスのウィジェットウィンドウが画面中央に表示されます。

## 使い方

| 操作 | 説明 |
|------|------|
| ドラッグ | ウィンドウを移動 |
| 端をドラッグ | ウィンドウをリサイズ |
| 📌 ボタン | 常に前面に表示をトグル |
| ▬ ボタン (1 回目) | コンパクト表示に切替 |
| ▬ ボタン (2 回目) | システムトレイに格納 |
| トレイアイコンをクリック | ウィンドウを復元 |
| ✕ ボタン | アプリケーションを終了 |

## プロジェクト構成

```
widget-go/
├── main.go              # エントリポイント、WebView2 ウィンドウ、HTTP サーバー、Win32 API
├── sysinfo/
│   ├── cpu.go           # CPU 使用率取得 (gopsutil)
│   ├── memory.go        # メモリ使用量取得 (gopsutil)
│   ├── wifi.go          # Wi-Fi 情報取得 (netsh)
│   ├── ethernet.go      # Ethernet 検出・リンク速度取得 (iphlpapi GetIfEntry2)
│   └── network.go       # ネットワーク送受信スループット取得 (gopsutil)
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

- **バックグラウンドコレクター** — CPU/メモリ/ネットワークトラフィックは 2 秒間隔、Wi-Fi/Ethernet は 3 秒間隔でキャッシュを更新
- **フロントエンド** — 2 秒間隔で `/api/system` をポーリングし UI を更新
- **ウィンドウ制御** — Win32 API を直接呼び出してフレームレス化・ドラッグ・リサイズ・トレイを実現

## 依存ライブラリ

- [go-webview2](https://github.com/jchv/go-webview2) — WebView2 バインディング
- [gopsutil](https://github.com/shirou/gopsutil) — CPU・メモリ情報取得

## 変更履歴

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
