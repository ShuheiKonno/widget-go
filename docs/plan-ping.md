# 計画: Ping監視機能（#52）

- Issue: #52「Ping監視機能」
  > Ping監視機能を追加する
  > 設定画面で監視先のホストをfqdnかIPアドレスで指定する
  > パケロスとRTT値を取得してウィジットに表示するようにする。
  > パケロスとRTTは閾値を設定できるようにして赤文字表示の警告とポップアップ通知を出すようにする
- ブランチ: `ShuheiKonno/1107update`
- ステータス: **計画確定（複数件・最大5件対応）**

---

## 1. 要件整理

| # | 要件項目 | 内容 |
|---|---|---|
| R1 | Ping監視 | 指定ホストに対して定期的にPing（ICMP Echo）を送信し、疎通・応答時間を確認する |
| R2 | ホスト指定（複数件） | 設定画面でFQDNまたはIPアドレスを**最大5件まで登録可能**にする |
| R3 | 情報表示 | 各ホストのパケットロス率（%）とRTT値（ms）を取得し、ウィジェット本体に行形式で一覧表示する |
| R4 | 閾値設定 | 各ホスト（または全体）のパケットロス率（%）とRTT（ms）の閾値を設定可能にする |
| R5 | 警告・通知 | 閾値超過時にウィジェット上で対象ホストの数値を赤文字警告し、トレイポップアップ（トースト）通知を発報する |
| R6 | 省リソース | **表示OFF（非表示）時は監視も完全停止**し、不要な通信・パケット送信を行わない |

---

## 2. 決定事項

| # | 項目 | 決定内容 | 理由・備考 |
|---|---|---|---|
| D1 | **Ping実装方式** | **Windows ICMP API (`iphlpapi.dll`)** | 管理者権限不要。`ping.exe` 外部プロセスの起動オーバーヘッドがなく、OS言語（日/英）によるパース崩れもない。テスト時は Fake 実装に差し替え可能 |
| D2 | **監視先件数** | **最大 5 件** | ルーター（GW）、外部DNS（8.8.8.8）、社内サーバー等を同時監視して障害個所を切り分け可能 |
| D3 | **監視間隔 & 並行度** | **2 秒周期 / 各ホスト並行送信** | 最大5件を並行（goroutine）で送信。各タイムアウト1000msとし、2秒周期をブロックしない |
| D4 | **パケロス算出（統計窓）** | **直近 20 回のリングバッファ** | 各ホストごとに直近20回（約40秒分）の送受信結果から `(失敗数 / 送信数) * 100 [%]` を算出 |
| D5 | **表示OFF時の動作** | **監視も完全停止** | 非表示の間はパケット送信を停止。画面の省スペース化で監視を続けたい場合は「折りたたみ」を利用 |
| D6 | **区画名・アイコン** | 区画名: `ping`<br>アイコン: 📡（アンテナ） | fieldnote スキン等のアイコン番号は `10`（ポモドーロ `09` の次） |
| D7 | **初期表示状態** | **既定では非表示（`hidden: true`）** | ホストが登録されていない初期状態での余計な表示を防ぐ。設定画面でホストを登録すると表示 |
| D8 | **閾値の既定値** | **パケロス: 10% / RTT: 100ms** | ホストごとに入力・調整可能 |
| D9 | **通知スパム抑止** | **ホストごとに5分間のクールダウン** | 警告状態が続いている間に毎秒通知が連打されるのを防ぐ。ホスト名付きで通知 |

---

## 3. アーキテクチャ設計

### 3.1 レイヤー構成

```text
[ Win32 iphlpapi.dll ] (IcmpCreateFile / IcmpSendEcho / IcmpCloseHandle)
         │
         ▼
[ ping/pinger.go ] (Pinger インターフェース: Win32実装 / テスト用Fake実装)
         │
         ▼
[ ping/monitor.go ] (マルチターゲット監視エンジン: ホスト別リングバッファ、パケロス・RTT計算、閾値判定)
         │
         ▼
[ pingloop.go ] (バックグラウンドgoroutine: 表示中のみ2秒周期で並行Ping実行、通知 `showBalloon`)
         │
         ▼
[ main.go / server.go ] (/api/system への SystemData.Ping 追加、/api/setoption での設定変更)
         │
         ▼
[ ui/index.html / ui/settings.html ] (ウィジェット本体のマルチホスト行表示・赤文字警告、設定画面での最大5件管理)
```

### 3.2 データモデル

#### 設定 (`config.go`)
```go
type PingTargetConfig struct {
    Host                 string `json:"host"`                 // 監視先 (FQDN or IP)
    LossThresholdPercent int    `json:"lossThresholdPercent"` // パケロス閾値 (%)、既定 10
    RTTThresholdMs       int    `json:"rttThresholdMs"`       // RTT閾値 (ms)、既定 100
}

type PingConfig struct {
    Targets       []PingTargetConfig `json:"targets"`       // 最大5件
    NotifyEnabled bool               `json:"notifyEnabled"` // ポップアップ通知、既定 true
    IntervalSec   int                `json:"intervalSec"`   // 測定間隔 (秒)、既定 2
}
```

#### スナップショット (`SystemData.Ping`)
```go
type PingTargetInfo struct {
    Host        string  `json:"host"`        // 入力ホスト名
    TargetIP    string  `json:"targetIP"`    // 解決されたIPアドレス
    RTTMs       int     `json:"rttMs"`       // 最新RTT (ms)
    AvgRTTMs    int     `json:"avgRttMs"`    // 直近窓の平均RTT (ms)
    LossPercent float64 `json:"lossPercent"` // パケットロス率 (%)
    Status      string  `json:"status"`      // "ok" | "warn" | "error"
    IsWarnLoss  bool    `json:"isWarnLoss"`  // パケロス閾値超過
    IsWarnRTT   bool    `json:"isWarnRTT"`   // RTT閾値超過
}

type PingInfo struct {
    Targets    []PingTargetInfo `json:"targets"`    // 各監視先の状態（最大5件）
    HasWarning bool             `json:"hasWarning"` // いずれかのホストで警告が出ているか
    SummaryRTT int              `json:"summaryRtt"` // 代表RTT（ヘッダー用）
}
```

### 3.3 ウィジェット表示 (`ui/index.html`)

- **ヘッダー部**:
  - アイコン: `📡`
  - タイトル: `Ping`
  - 右端値: 正常時は代表RTT（例: `12 ms`）、警告時は赤文字（`! 警告` または超過した最悪値）
  - 折りたたみボタン
- **本体部 (`sec-body`)**:
  - 最大5件を行形式で一覧表示（ドライブ一覧のようなすっきりしたレイアウト）:
    ```text
    google.com          14 ms    0%
    192.168.1.1          2 ms    0%
    vpn.internal       120 ms   25%   <-- 閾値超過項目が赤文字
    ```
  - 各行をクリックするとホストのIPアドレス等の詳細チップを展開可能

### 3.4 設定画面 (`ui/settings.html`)

- ナビゲーションに「**Ping監視**」タブを追加
- パネル内容:
  - 監視先リスト（最大5件）:
    - ホスト名/IP入力ボックス
    - パケロス閾値（%）
    - RTT閾値（ms）
    - 削除ボタン
  - 「+ 監視先を追加」ボタン（5件に達したら自動で無効化）
  - ポップアップ通知のON/OFF
  - 区画の表示/非表示（「区画」タブと連動）

---

## 4. 実装タスク一覧

- [x] **T1: 設計確認**（本計画の合意）
- [x] **T2: 設定・ロジック実装（`ping/`）**
  - `PingConfig`, `PingTargetConfig` 定義
  - `Pinger` インターフェース、マルチターゲット監視エンジン（リングバッファ、パケロス・RTT計算、閾値判定）
  - 単体テスト（FakePinger による網羅的テスト）
- [x] **T3: Win32 ICMP API の実装（`ping/win32.go`）**
  - `iphlpapi.dll` の `IcmpCreateFile`, `IcmpSendEcho`, `IcmpCloseHandle` 呼び出し
  - 並行実行対応、名前解決
- [x] **T4: 定期監視ループ & 通知制御（`pingloop.go`）**
  - 表示中のみ動く 2 秒周期 goroutine
  - 閾値超過時のトレイ通知（`showBalloon`、ホスト別 5 分間クールダウン）
- [x] **T5: サーバー結合（`server.go`, `main.go`）**
  - `/api/system` に `Ping` スナップショットを追加
  - `/api/setoption` または `/api/ping/targets` による最大5件の設定更新
- [x] **T6: UI実装（`ui/index.html`, `ui/settings.html`）**
  - メインウィジェットにマルチホスト行表示と赤文字警告を追加
  - 設定画面に最大5件のリスト管理UIを追加
- [x] **T7: スキン対応 & ドキュメント更新**
  - `docs/skin-spec.md` に Ping 区画（アイコン番号 `10`）を追加
  - `skins/builtin/fieldnote/skin.css` にスタイル追加
  - `README.md` の更新
- [x] **T8: 動作検証 & テスト実行**
  - `go test ./...`
  - 実機での複数件 Ping 疎通と閾値超過時の赤文字警告・通知の確認
