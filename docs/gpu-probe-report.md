# GPU/NPU probe 実機検証（T1、2026-09-27）

## 実施環境と判定

- Windows 11、Intel Core Ultra 200V、Intel Arc 140V と Intel AI Boost。Go 1.25、amd64、`CGO_ENABLED=0`。
- 名前と容量は **方式 A（D3DKMT のみ）を推奨**。`KMTQAITYPE_DRIVER_DESCRIPTION=65`、`KMTQAITYPE_GETSEGMENTSIZE=3` が 3 アダプタとも NTSTATUS 0 で成功した。方式 B の DXGI `CreateDXGIFactory1` → `EnumAdapters1` → `GetDesc1` も試し、GPU と Basic Render の名前・容量・LUID が A と一致した。ただし DXGI の列挙に NPU は現れず、NPU を含む単一経路としては A の方が簡潔。B を実装する場合は `IID_IDXGIFactory1=770aae78-f26f-4dba-a829-253c83d1b387`、継承分を含む vtable の `EnumAdapters1=12`、`IDXGIAdapter1::GetDesc1=10`、`This` を第 1 引数、HRESULT 負値判定、`DXGI_ERROR_NOT_FOUND=0x887A0002` で終了、各 COM オブジェクトの `Release=2` が必要。
- NPU は **DXCore の `DXCORE_HARDWARE_TYPE_ATTRIBUTE_NPU` で列挙して LUID を D3DKMT と照合**する。実機では 1 件で `0x00000000_0x0001082C` が Intel AI Boost と一致した。`ComputeOnly` だけでは NPU と断定しない。DXCore がない OS または NPU 属性を返さない OS では NPU と断定せず、「計算専用アダプタ」とする。Windows 10/11 の全バージョンでこの属性が有効かは未検証。
- 除外は `SoftwareDevice` または `IndirectDisplayDevice`。`0x107EB` の Microsoft Basic Render Driver は `SoftwareDevice=1`、`IndirectDisplayDevice=0` で除外できる。固定 LUID 判定は不要。
- 専用容量のしきい値は **512 MiB を維持**。Arc 140V の専用容量は 128 MiB で、iGPU の予約領域を大容量 dGPU と誤認しない。本機に dGPU がないため複数 GPU の優先順位は未検証。
- `PdhAddEnglishCounterW` に `\\GPU Engine(*)\\Utilization Percentage` を渡すと実機でワイルドカードが展開され、初回サンプルを捨てた後の `PdhGetFormattedCounterArrayW` が多数の実インスタンスを返した。実機では `PdhExpandWildCardPathW` への代替は不要。二段階呼び出しは初回 `PDH_MORE_DATA=0x800007D2` で必要バイト数を取得し、各要素の `CStatus` 0/1 のみ採用した。代替が必要な別環境の具体的挙動は未検証。

## 実出力（抜粋）

```text
adapter 0x00000000_0x000103E9 flags=0x232B [RenderSupported,DisplaySupported,PostDevice,HybridIntegrated,ACGSupported,SupportSetTimingsFromVidPn,RuntimePowerManagement] name="Intel(R) Arc(TM) 140V GPU (16GB)" dedicated=134217728 shared=19293264568 nameStatus=0x0 capStatus=0x0
adapter 0x00000000_0x0001082C flags=0x2901 [RenderSupported,ACGSupported,ComputeOnly,RuntimePowerManagement] name="Intel(R) AI Boost" dedicated=0 shared=19293264568 nameStatus=0x0 capStatus=0x0
adapter 0x00000000_0x000107EB flags=0x105 [RenderSupported,SoftwareDevice,ACGSupported] name="Microsoft Basic Render Driver" dedicated=0 shared=19293264568 nameStatus=0x0 capStatus=0x0
DXGI 0x00000000_0x000103E9 name="Intel(R) Arc(TM) 140V GPU (16GB)" dedicated=134217728 shared=19293264568 flags=0x0
DXCore NPU count=1
DXCore NPU LUID=0x00000000_0x0001082C
instance pid_11052_luid_0x00000000_0x000103E9_phys_0_eng_5_engtype_Compute 0.000%
instance pid_13108_luid_0x00000000_0x0001082C_phys_0_eng_0_engtype_Compute 0.000%
engtypes=["" "3D" "Compute" "Copy" "GSC" "Neural" "VideoDecode" "VideoProcessing"]
sample 00000000_000103E9 all=0.091% compute=0.000% dedicated=0 shared=683020288 malformed=0
sample 00000000_0001082C all=0.000% compute=0.000% dedicated=0 shared=4667617280 malformed=0
```

`Neural` は GPU LUID にあるため NPU 判定に使えない。`engtype_` 空欄は未分類で全体 % には含める。各 `(LUID, phys, eng)` の PID 別値を合算し、アダプタ内の最大値を全体 %、`compute`/`cuda` を含む engtype の最大値を Compute % とした。生値は 100% で丸めない。

## x64 構造体照合

SDK `10.0.26100.0` の `shared/d3dkmthk.h`、`shared/dxgi.h`、`um/Pdh.h` と、probe の `unsafe.Sizeof/Offsetof` を比較。単位は byte。

| C / Go 構造体 | SDK の size、主要 offset | Go 実測 | 結果 |
|---|---:|---:|---|
| `LUID` | 8、Low 0、High 4 | 8、0、4 | 一致 |
| `D3DKMT_ADAPTERINFO` | 20、hAdapter 0、AdapterLuid 4、NumOfSources 12、bPrecise 16 | 20、0、4、12、16 | 一致 |
| `D3DKMT_ENUMADAPTERS3` | 24、Filter 0、NumAdapters 8、pAdapters 16 | 24、0、8、16 | 一致 |
| `D3DKMT_QUERYADAPTERINFO` | 24、hAdapter 0、Type 4、pPrivateDriverData 8、Size 16 | 24、0、4、8、16 | 一致 |
| `D3DKMT_SEGMENTSIZEINFO` | 24、DedicatedVideo 0、DedicatedSystem 8、SharedSystem 16 | 24、0、8、16 | 一致 |
| `PDH_FMT_COUNTERVALUE` | 16、CStatus 0、doubleValue 8 | 16、0、8 | 一致 |
| `PDH_FMT_COUNTERVALUE_ITEM_W` | 24、szName 0、FmtValue 8 | 24、0、8 | 一致 |
| `DXGI_ADAPTER_DESC1` | 312、Description 0、DedicatedVideo 272、SharedSystem 288、AdapterLuid 296、Flags 304 | 312、0、272、288、296、304 | 一致 |

## 性能

5 分間、2 秒周期、151 サンプル、標準出力を一時ログへリダイレクトして実測。プロセス CPU は累積 CPU 秒 `0.609375` を実時間 `302.994` 秒と 8 論理プロセッサで割って算出した。収集は 3 カウンタの `PdhCollectQueryData` 合計、整形は `PdhGetFormattedCounterArrayW` の二段階呼び出し、解析は正規表現による分類・集計・文字列整形であり、ログへの書き込み時間は解析値に含めない。

| 指標 | 5 分測定値 |
|---|---:|
| 平均プロセス CPU % | 0.0251%（8 論理プロセッサ全体を 100%） |
| PDH 収集 / 回 | 0.674 ms |
| PDH 整形 / 回 | 0.609 ms |
| 解析・表示文字列生成 / 回 | 0.641 ms |

## Task Manager 対照

Task Manager は起動して画面を読めたが、昇格した性能タブへの入力が成功せず、同時刻の 2 秒平均値を採れなかった。WebGPU / DirectML / OpenVINO の負荷もこの比較条件では実行していない。±5pt/±5% の合否は推測しない。

| 対象 | gpuprobe | Task Manager の対応項目 | 2 秒平均・許容差の判定 |
|---|---|---|---|
| GPU 全体 % | Arc の全エンジン最大 | GPU 見出し % | 未検証 |
| GPU Compute % | Arc の Compute 系最大 | Compute グラフ | 未検証 |
| NPU % | DXCore で一致した AI Boost の全エンジン最大 | NPU タブ | 未検証 |
| 専用メモリ | Arc の PDH Dedicated Usage | 専用 GPU メモリ | 未検証 |
| 共有メモリ | Arc の PDH Shared Usage | 共有 GPU メモリ | 未検証 |

代替確認として PowerShell `Get-Counter '\\GPU Adapter Memory(*)\\Shared Usage','\\GPU Adapter Memory(*)\\Dedicated Usage' -SampleInterval 1 -MaxSamples 2` の Arc LUID 値は、共有 705261568 byte、専用 0 byte だった。probe の近接サンプルは共有約 683–748 MB、専用 0 byte。別時刻の値なので厳密な許容差判定には使わない。

## 計画書 §2〜§3 との相違・T2 への注意

- §2.5 の「Arc 140V は専用メモリが 0」は、**PDH 専用使用量は 0 byte、D3DKMT/DXGI 専用容量は 128 MiB** と書き分けるのが正確。容量は 0 ではない。
- D3DKMT の Arc `DedicatedVideoMemorySize` と DXGI `DedicatedVideoMemory` は共に 128 MiB。使用量 0 byte と矛盾しない。
- `KMTQAITYPE_DRIVER_DESCRIPTION` は NPU にも成功したため、T2 で DXGI は必須でない。
- §3 の「初回 nil」と再列挙条件、Arm64、Windows 10 2004 などは T1 の実機では未検証。T2 のテストで扱う。
- Task Manager の 2 秒平均比較という T1 受け入れ条件は未達。T2 着手前に性能タブを操作できる環境で追試が必要。

## ビルド・静的検査

`CGO_ENABLED=0 go vet ./...` は成功。`CGO_ENABLED=0 go build -buildvcs=false ./cmd/gpuprobe` も成功し、上記の実行値を得た。フラグなしの `go build ./cmd/gpuprobe` は Go 1.25 が VCS スタンプ取得時に作業ディレクトリを `C:\Users\shuhe\work` として `git status --porcelain` を呼び、そこが Git リポジトリではないため `error obtaining VCS status: exit status 128` で失敗した。`git status` を本ワークツリーで直接実行すると成功する。Go ソースのビルドエラーではないが、受け入れ条件の**フラグなしビルドは未達**である。環境変数 `GOFLAGS=-buildvcs=false` を与えて同じパッケージをビルドする方法も成功した。`go.mod` は変更していない。
