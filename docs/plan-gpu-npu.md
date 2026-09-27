# 計画: GPU / NPU / VRAM 使用率の表示（Go 実装・ベンダー非依存）— 改訂 2

- ブランチ: `ShuheiKonno/GPU-NPU`（ベース v1.8.0 / `6668a02`）、リリース予定 v1.9.0
- 決定事項
  - 2026-09-27: C 移植は行わず、現行の Go 実装に機能を追加する。理由は本機での実測で、Go ホストの Working Set 35.7MB に対して WebView2 は約 407MB と、C 化で削れる余地が小さいため。
  - 2026-09-27: 対象 GPU の選択機能は v1.9.0 では入れない。表示する GPU は自動で決める（§2.4）。
  - 2026-09-27: GPU は **全体 %**（Task Manager と同じ定義）と **Compute %** の両方を表示する。
- 改訂 2 は、Orca の 2 レビュー（run `run_c89acda3de6b`: API・技術の正確さ／既存コードとの整合）の指摘をすべて反映したもの。

## 1. 表示仕様

| 表示箇所 | GPU | NPU |
|---|---|---|
| 通常表示 | 名前、**全体 %**、**Compute %**、メモリ（専用・共有を別々に） | 名前、**使用率 %** |
| コンパクト表示（幅 230px） | Compute %。Compute 系エンジンが無い GPU では全体 % を「GPU」ラベルで表示する。1 行に収め、折り返さない | 使用率 % |
| トレイのツールチップ | `GPU: 12%` | `NPU: 3%`（NPU が無ければ項目ごと省略） |

- 取得できない値は「---」と表示する。
- NPU が存在しない環境では NPU セクション自体を出さない。
- 表示オプションに「GPU/NPU を表示」（既定 ON）を追加する。OFF のときは収集も止め、CPU 負荷を下げる。
- NPU のメモリは v1.9.0 では表示しない（値は収集するが UI には出さない）。

## 2. データソースと判定規則

### 2.1 使用する API（すべて Windows 標準・ベンダー非依存）

| 取得項目 | API | 実装上の注意 |
|---|---|---|
| アダプタの列挙 | `D3DKMTEnumAdapters3`（`Filter.IncludeComputeOnly=1`） | 動作するのは **Windows 10 2004 以降**（[MS Learn](https://learn.microsoft.com/en-us/windows-hardware/drivers/ddi/d3dkmthk/nf-d3dkmthk-d3dkmtenumadapters3)）。呼ぶ前に `proc.Find()` で存在を確認し、無ければ GPU/NPU 機能を無効にする。**panic させない**。列挙は `pAdapters=nil` で必要数を得てから再度呼ぶ。得たハンドルはすぐに `D3DKMTCloseAdapter` で閉じる |
| 種別フラグ | `D3DKMTQueryAdapterInfo(KMTQAITYPE_ADAPTERTYPE)` | `RenderSupported` / `ComputeOnly` / `SoftwareDevice` / `IndirectDisplayDevice` を読む（`d3dkmthk.h` 1985〜2025 行） |
| 名前・メモリ容量 | **T1 の結果、方式 A（D3DKMT のみ: `KMTQAITYPE_DRIVER_DESCRIPTION=65`、`KMTQAITYPE_GETSEGMENTSIZE=3`）に決定した**。DXGI の列挙には NPU が現れないため。以下は検討時の記述。T1 で次のどちらかに決める。**A** は D3DKMT だけで取得（`KMTQAITYPE_DRIVER_DESCRIPTION` 等とセグメント情報）。**B** は DXGI（`CreateDXGIFactory1` → `EnumAdapters1` → `GetDesc1`）を COM の vtable 経由で直接呼ぶ | CGO は使えない（`release.yml` が `CGO_ENABLED=0`）。B にする場合は次を T2 の仕様に書く。IID、継承分を含む vtable の番号、`This` を第 1 引数にすること、HRESULT の負値判定、`DXGI_ERROR_NOT_FOUND` で列挙終了、`Release`。`Win32_VideoController.AdapterRAM` は 32bit で上限があるので使わない |
| エンジン使用率 | PDH `\GPU Engine(*)\Utilization Percentage` をワイルドカードで指定 | `PdhGetFormattedCounterArrayW` は 2 段階で呼ぶ（1 回目の `PDH_MORE_DATA` でバッファサイズを得る）。要素ごとに `CStatus` を検査する。初回サンプルは捨てる。`PdhAddEnglishCounterW` でワイルドカードが展開されるかは T1 で確認する。PDH の proc 定義は `cpufreq.go:21-25` のものを共有し、再宣言しない |
| メモリ使用量 | PDH `\GPU Adapter Memory(*)\Dedicated Usage` と `Shared Usage` | システム全体の値を使う。`IDXGIAdapter3::QueryVideoMemoryInfo` は自プロセス分しか返さないので使わない |

`D3DKMTQueryStatistics` は Microsoft Learn に「system use reserved; do not use」とあるので採用しない。

### 2.2 PDH インスタンス名と集計

- 形式（本機で実測）: `pid_<n>_luid_0x<HHHHHHHH>_0x<LLLLLLLL>_phys_<p>_eng_<N>_engtype_<Type>`
  - engtype が空のもの（`engtype_`）もある。
  - この形式に合わない名前は捨て、診断用にカウントだけ残す。
- 集計の手順:
  1. 同じ `(LUID, phys, eng)` の値を全 pid で合算する。
  2. アダプタごとに次の 2 つを求める。
     - **全体 %** = 全エンジンの最大値
     - **Compute %** = Compute 系と判定したエンジンの最大値
  3. 100 でクランプするのは表示段階で行う。元の値は保持する。
- **Compute 系エンジンの判定**: engtype に `compute` または `cuda` を含むもの（大文字小文字は区別しない）。
  - NVIDIA の `Cuda`、AMD の `Compute_0` などはベンダーごとの名前の推定で、実機では確認していない。
  - 判定できない engtype は「未分類」のまま扱い、全体 % には含める。
  - `Neural` を NPU と見なさない。本機の Arc GPU にも `Neural` エンジンがあるため。

### 2.3 アダプタの分類

- **除外するもの**: `SoftwareDevice`（Basic Render）と `IndirectDisplayDevice`。LUID の固定値では判定しない。本機の `0x107EB` を除外できるかは T1 で確認する。
- **NPU**: 次のどちらかを満たすもの。どちらでも判定できない ComputeOnly アダプタは「計算専用アダプタ」として NPU 扱いしない。
  - DXCore の NPU ハードウェア種別属性（[DXCore 列挙](https://learn.microsoft.com/en-us/windows/win32/dxcore/dxcore-enum-adapters)）で NPU と判定され、LUID が一致する。
  - PnP の `ComputeAccelerator` クラスのデバイスと対応づけられる。
  - どちらの方法を使うか、またそれぞれ使える OS の範囲は T1 で決める。
- **GPU**: `RenderSupported` かつ `ComputeOnly` でなく、除外対象でもなく、NPU と判定されていないもの。
  - T1 の実測で、NPU（AI Boost）にも `RenderSupported` が立っていた（flags=0x2901）。そのため `RenderSupported` だけでは NPU が GPU に混ざる。
- **T1 で確定したこと**（`docs/gpu-probe-report.md`）:
  - NPU は DXCore の `DXCORE_HARDWARE_TYPE_ATTRIBUTE_NPU` で列挙し、LUID を D3DKMT の列挙結果と照合して判定する。
  - Basic Render（`0x107EB`）は `SoftwareDevice=1` なので除外できる。
  - `PdhAddEnglishCounterW` でワイルドカードは展開される。
  - 5 分平均のプロセス CPU は 0.0251%。

### 2.4 表示する GPU の自動選択（v1.9.0 は固定）

- 専用メモリ（`DedicatedVideoMemory` またはセグメント容量）が**しきい値以上**の GPU を優先する。しきい値は T1 で決め、仮に 512MB とする。条件を満たすものが複数あれば、容量が最大のものを選ぶ。
- 該当がなければ、残った GPU の先頭（iGPU）を選ぶ。
- 選択は Init 時と、再列挙したとき（§3）にだけ行う。設定には保存しない。LUID は再起動で変わるため、保存キーに使えない。

### 2.5 メモリの表示

- 専用メモリと共有メモリを**別々に**持ち、表示する。分母はそれぞれ対応する容量とする。
- 分母が 0 のときは比率を出さず、使用量だけを表示する。
- 本機の Arc 140V（T1 実測）は、専用容量が 128MiB（iGPU の予約領域）で、専用使用量は PDH で 0 byte だった。表示は「専用 0 / 128MB」と「共有 xx / 18GB」の 2 行になる。

## 3. 収集・状態管理（既存の慣習に合わせる）

- `sysinfo/gpu.go`: `InitGPU()`、`GetGPUInfo() (*GPUInfo, *NPUInfo)`。
  - PDH の状態は `gpuMu` で保護する（`freqMu` と同じ方式）。
  - 戻り値は毎回新しく確保した構造体で返す（スライスもコピーする）。
- **InitGPU は専用 goroutine で実行する**。メインスレッドでは COM の初期化・解除をしない。go-webview2 がメインスレッドで STA 初期化しているため（`pkg/edge/corewebview2.go:17-24`）。
- `main.go` の `refreshGPU()` は `refreshCPUAndMem` と同じ形にする。ロックの外で取得し、`cacheMu` の中では代入するだけにする。既存の 2 秒周期のループから呼ぶ。
- **「存在しない」と「一時的に取れない」を区別する**:
  - `GPU == nil` / `NPU == nil` は「デバイスが存在しない」ことだけを意味し、Init または再列挙の時点で確定させる。
  - 一時的な取得失敗は `GPUInfo` 内の値を `*float64` の nil にし、「---」と表示する。これでセクションの点滅を防ぐ。
- **再列挙**: 次の場合に低頻度で再列挙する。ドライバ更新・TDR・eGPU の着脱に追従するため。
  - PDH に未知の LUID が現れたとき
  - 選択中の LUID が連続 N 周期見えなくなったとき
- JSON（既存に合わせて PascalCase にする）:
  ```go
  type GPUInfo struct {
      Name          string
      TotalPct      *float64 // 全エンジン最大（Task Manager の GPU %）
      ComputePct    *float64 // Compute 系エンジン最大。無ければ nil
      DedicatedUsed uint64; DedicatedTotal uint64
      SharedUsed    uint64; SharedTotal    uint64
  }
  type NPUInfo struct { Name string; UsagePct *float64; MemUsed uint64 }
  // SystemData に GPU *GPUInfo `json:"GPU"` と NPU *NPUInfo `json:"NPU"` を追加
  ```

## 4. 作業（Orca Task DAG・深さ 3）

**T1 probe と方式の確定**
- 対象ファイル: `cmd/gpuprobe/main.go`、`docs/gpu-probe-report.md`
- 作業内容:
  - アダプタ一覧を出す（LUID、種別フラグ、名前、容量）。
  - engtype の実名一覧と、インスタンス名のサンプルを出す。
  - 全体 % / Compute % / メモリを 1 秒周期で出す。
  - x64 で `unsafe.Sizeof` とフィールドオフセットを検証する。
- 決めること（レポートに記載する）:
  - 名前と容量の取得方式（A か B か）
  - NPU の判定方法
  - 除外フラグ
  - 専用メモリのしきい値
  - ワイルドカードの展開方法
- 受け入れ条件:
  - 全体 % は Task Manager の GPU の見出し % と ±5pt で一致する。
  - Compute % は Task Manager のエンジン別グラフ（Compute）と ±5pt で一致する。
  - NPU % は Task Manager の NPU タブと ±5pt で一致する。
  - メモリは専用・共有それぞれ ±5% で一致する。
  - 比較は 2 秒平均どうしで行う。負荷は WebGPU / DirectML のサンプルと OpenVINO の NPU サンプルでかける。
  - 5 分間の平均プロセス CPU% と、PDH の収集・整形・解析それぞれの所要時間を計測し、レポートに記載する。

**T2 収集モジュール**（T1 に依存）
- 対象ファイル: `sysinfo/gpu.go`、`sysinfo/gpu_test.go`、`sysinfo/cpufreq.go`（PDH の proc 共有部分だけ）
- 作業内容: §2 と §3 を実装する。インスタンス名のパーサ、集計、engtype の判定、GPU の自動選択は純粋関数にする。
- 受け入れ条件:
  - 純粋関数のテストが通る。テストデータは T1 レポートの実名に、AMD / NVIDIA 風の名前、空の engtype、`phys_1` を加えたもの。
  - 実 API を呼ぶスモークテストで「panic しない、nil または範囲内の値を返す」ことを確認する。CI の GPU なしランナーでも通ること。
  - `go test -race` で refresh と getSystemData を並行に走らせるテストが通る。
  - 本機で 5 分間の平均 CPU が 1% 未満（T1 と同じ方法で測る）。
  - T2 の完了後、gpuprobe は `sysinfo` を呼ぶだけの薄い CLI に作り替える。

**T3 統合と UI**（T2 に依存）
- 対象ファイル: `main.go`、`config.go`、`config_test.go`（新規）、`server.go`、`tray.go`、`tray_test.go`、`ui/index.html`
- 作業内容:
  - `SystemData` への追加と `refreshGPU` の追加。
  - 設定 `ShowGPU` を追加する（既定 true を `defaultConfig` に入れる）。表示と収集の ON/OFF に使う Go 側の状態として持つ。
  - `/api/setoption` への分岐追加、`/api/settings` への反映。
  - `index.html` の GPU/NPU セクション（通常表示とコンパクト表示）。
  - 設定モーダルの変更。対象は `settings` の既定値、`refreshModalUI`、change ハンドラ、settings の反映処理。
  - **高さの追従**: `lastConnMode` と同じ方法で GPU/NPU の表示状態を追跡し、変化したら `adjustWindowHeight` と `persistwindow` を呼ぶ。
  - トレイのツールチップ: 組み立て処理を純粋関数に分ける。`setTipText` は 127 文字で切り詰め、NUL 終端する。
- 受け入れ条件:
  - `config_test.go` で v1.8.0 形式の config.json を読み込み、新しいキーの既定値が正しいことを確認する。
  - 次の 3 場面で内容が切れず、余白も残らないこと。通常表示とコンパクト表示の両方で確認し、スクリーンショットを残す。
    - トグルの ON/OFF
    - NPU が無い場合
    - 初回が nil で、後から値が来る場合
  - コンパクト表示の幅 230px で折り返さない。
  - ツールチップのテストが通る。

**T4 ドキュメント**（T3 と並行してよい）
- 対象ファイル: `README.md`
- 更新する節: 機能、必要環境（**Windows 10 2004 以降**、GPU は WDDM 2.0 以上、NPU は MCDM 対応ドライバ）、表示オプション表、プロジェクト構成、アーキテクチャ（収集間隔）、変更履歴。変更履歴には「AMD / NVIDIA / Qualcomm・Arm64 は実機未検証」と書く。

**T5 リリース準備**（T3 と T4 に依存）
- 対象ファイル: `winres/winres.json`（1.9.0.0 にする）、`rsrc_windows_*.syso` の再生成、`tray_test.go` のバージョンのケース、`.github/workflows/test.yml`（`-race` を追加）
- 作業内容: タグを打った後、Release 本文に「実機未検証」の注記を手で追記する。

**Orca の運用**
- worker ごとに編集できるファイルは、上の「対象ファイル」に限る。
- worker はコミットしない。コーディネータが受け入れ条件の証拠を確認してからコミットする。
- T1 の結果が §2 の前提と食い違った場合（例: NPU を特定できない、CPU 負荷が 1% を超える）は、T2 に進む前にユーザーに確認する。

## 5. 検証の限界

- 実機で確認できるのは Intel（Arc 140V と AI Boost、Windows 11）だけ。
- AMD / NVIDIA / Qualcomm は engtype 名の単体テストまでしか確認できない。未知の engtype でも全体 % は出るように作る。
- Windows 10 2004 より前の OS で「GPU 機能だけ無効になり、ウィジェットは起動する」ことは、`Find()` 失敗時の処理のテストで代わりに確認する。
- Arm64 は x64 エミュレーションで動くと推定しているが、確認していない。

## 出典

- D3DKMTEnumAdapters3（Windows 10 2004 以降、ComputeOnly の既定除外、CloseAdapter の義務）: https://learn.microsoft.com/en-us/windows-hardware/drivers/ddi/d3dkmthk/nf-d3dkmthk-d3dkmtenumadapters3
- D3DKMT_QUERYSTATISTICS（do not use）: https://learn.microsoft.com/en-us/windows-hardware/drivers/ddi/d3dkmthk/ns-d3dkmthk-d3dkmt_querystatistics
- DXCore のアダプタ列挙: https://learn.microsoft.com/en-us/windows/win32/dxcore/dxcore-enum-adapters
- MCDM: https://learn.microsoft.com/en-us/windows-hardware/drivers/display/mcdm
- PdhGetFormattedCounterArrayW: https://learn.microsoft.com/en-us/windows/win32/api/pdh/nf-pdh-pdhgetformattedcounterarrayw
- PdhAddEnglishCounterW: https://learn.microsoft.com/en-us/windows/win32/api/pdh/nf-pdh-pdhaddenglishcounterw
- DXGI_ADAPTER_DESC1: https://learn.microsoft.com/en-us/windows/win32/api/dxgi/ns-dxgi-dxgi_adapter_desc1
- IDXGIAdapter3::QueryVideoMemoryInfo（自プロセス分のみ）: https://learn.microsoft.com/en-us/windows/win32/api/dxgi1_4/nf-dxgi1_4-idxgiadapter3-queryvideomemoryinfo
- GPUs in the Task Manager（GPU % は最も忙しいエンジンの値、ベンダー非依存）: https://devblogs.microsoft.com/directx/gpus-in-the-task-manager/
- Copilot+ PCs developer guide（NPU と Task Manager）: https://learn.microsoft.com/en-us/windows/ai/npu-devices/
- レビュー原本: Orca run `run_c89acda3de6b` の `review-api.md` と `review-fit.md`（scratchpad）
