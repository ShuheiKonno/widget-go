# 計画: Ping監視機能（#52）

- Issue: #52「Ping監視機能」
  > Ping監視機能を追加する
  > 設定画面で監視先のホストをfqdnかIPアドレスで指定する
  > パケロスとRTT値を取得してウィジットに表示するようにする。
  > パケロスとRTTは閾値を設定できるようにして赤文字表示の警告とポップアップ通知を出すようにする
- ブランチ: `ShuheiKonno/1107update`
- ステータス: **計画確定（複数件・最大5件対応）**。#56「Ping監視の見直し」で D3・D4・D6・D10 と 3.2・3.3 を実装に合わせて更新した

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
| D3 | **監視間隔 & 並行度（#56 で更新）** | **既定 2 秒、1〜10 秒で設定可 / 開始時刻基準 / 各ホスト並行送信** | 最大5件を並行（goroutine）で送信。各タイムアウト1000ms。名前解決と Echo の待ちを合わせて 2 秒で打ち切る（Echo の待ちは期限までの残りに切り詰める）。周期は送信の開始時刻を基準にし、Ping の所要時間で延ばさない（遅れた周期は飛ばす）。上限を 10 秒にするのは、窓（D4）が回数固定のため。60 秒だと窓が 20 分に伸び、復帰判定（D11）が 1〜2 回の応答で済んでしまう。10 秒超の既存設定は読み込み時に 10 秒へ矯正する。間隔を変えたらバッファを捨てる。設定の変更（kick）では余分に測らず、追加・変更した監視先だけをすぐ測る |
| D4 | **パケロス算出（統計窓）** | **直近 20 回のリングバッファ** | 各ホストごとに直近20回（2 秒周期で約40秒分、10 秒周期で約200秒分）の送受信結果から `(失敗数 / 送信数) * 100 [%]` を算出し、閾値の判定に使う。表示は `失敗数/送信数`（例 `2/20`、立ち上がりは `1/3`。3 回分そろうまでは `--`）。名前解決の失敗は送信できていないので数えない（#56） |
| D5 | **表示OFF時の動作** | **監視も完全停止** | 非表示の間はパケット送信を停止。画面の省スペース化で監視を続けたい場合は「折りたたみ」を利用 |
| D6 | **区画名・アイコン** | 区画名: `ping`（表示名は「疎通」。#56）<br>アイコン: 📡（アンテナ） | fieldnote スキン等のアイコン番号は `10`（ポモドーロ `09` の次） |
| D7 | **初期表示状態** | **既定では非表示（`hidden: true`）** | ホストが登録されていない初期状態での余計な表示を防ぐ。設定画面でホストを登録すると表示 |
| D8 | **閾値の既定値** | **パケロス: 10% / RTT: 100ms** | ホストごとに入力・調整可能 |
| D9 | **通知スパム抑止** | **ホストごとに5分間のクールダウン** | 警告状態が続いている間に毎秒通知が連打されるのを防ぐ。ホスト名付きで通知 |
| D10 | **警告の判定（レビューで確定、#56 で更新）** | **ロスはサンプルが 3 回分以上あり、窓の中で 2 回以上かつ率が閾値以上。RTT は閾値以上が 3 回連続。名前解決の失敗が 3 回続いたら `unresolved`** | 起動直後の 1/1 = 100% や一瞬のスパイクで誤報しない。ロス閾値は 1〜100%（未指定・0 は 10%）。窓が満杯なら 1 回 = 5% なので、閾値は 5% 刻みで効く。警告かどうかは Go 側の `status`（`warn` / `error` / `unresolved`）だけで決め、UI と通知はそれに従う |
| D11 | **クールダウンの解除（レビューで確定）** | **警告なしが 1 分続いたら復帰とみなして解く** | 閾値の前後を行き来するホストで、すぐ解くと通知が連発する |
| D12 | **監視先の書式（レビューで確定）** | **FQDN と IPv4 のみ。IPv6 は未対応。重複は 1 件にまとめる** | 保存時に検証し、不正は 400。統計のバッファがホスト名で共有されるため、重複を許さない |
| D13 | **非表示から再開するとき（レビューで確定）** | **止めるときに履歴と通知の状態を捨てる** | 古いサンプルが新しい測定と混ざらないようにする |

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
[ pingloop.go ] (バックグラウンドgoroutine: 表示中のみ測定間隔（既定2秒）ごとに並行Ping実行、通知 `showBalloon`)
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
    LossPercent float64 `json:"lossPercent"` // パケットロス率 (%)。閾値の判定用
    SentCount   int     `json:"sentCount"`   // 窓内の送信数（名前解決の失敗は数えない）
    LossCount   int     `json:"lossCount"`   // 窓内のロス数
    LossReady   bool    `json:"lossReady"`   // ロスを表示・判定できるだけのサンプルがあるか（3 回以上）
    Status      string  `json:"status"`      // "idle" | "ok" | "warn" | "error" | "unresolved"
    IsWarnLoss  bool    `json:"isWarnLoss"`  // パケロス閾値超過（どの値を赤くするか用）
    IsWarnRTT   bool    `json:"isWarnRTT"`   // RTT閾値超過（同上）
    ResolveFail bool    `json:"resolveFail"` // 直近の 1 回が名前解決の失敗
}

type PingInfo struct {
    Targets    []PingTargetInfo `json:"targets"`    // 各監視先の状態（最大5件）
    SummaryRTT int              `json:"summaryRtt"` // 代表RTT（ヘッダー用。最悪値）
}
```

- 実装では `ping.TargetInfo` / `ping.Info`（`ping/types.go`）。`hasWarning` は UI で使っておらず、警告の判定を `status` に一本化したため #56 で削除した
- UI（`ui/index.html` の `applyPing`）が読むキー名は、`pingui_test.go` で JSON タグと突き合わせる（#56 の `isWarnRtt` の取り違えの再発防止）

### 3.3 ウィジェット表示 (`ui/index.html`)

- **ヘッダー部**:
  - アイコン: `📡`
  - タイトル: `疎通`（#56 で `Ping` から変更。内部キーは `ping` のまま）
  - 右端値（折りたたみ時も見える値）: 警告がないときは代表RTT。代表RTTは、直近の 1 回が成功したホストの最新RTTの**最大（最悪値）**（例: `12 ms`。1ms 未満は `<1 ms`、該当なしは `-- ms`）。直近の 1 回が失敗したホストの古い値は使わない。警告があるとき（`status` が `warn` / `error` / `unresolved` のホストがあるとき）は赤文字で件数（`⚠ 2 件`）
  - 折りたたみボタン
- **本体部 (`sec-body`)**:
  - 最大5件を行形式で一覧表示（ドライブ一覧のようなすっきりしたレイアウト）:
    ```text
    google.com          14 ms   0/20
    192.168.1.1         <1 ms   0/20
    vpn.internal       120 ms   5/20   <-- 閾値超過項目が赤文字
    nx.example            --  解決失敗  <-- 名前解決が続けて失敗（ロスには数えない）
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
