# 依存物とライセンスの確認記録

確認日: 2026-11-14。対象ソース: `1d5db2b`を基点とする申請準備作業。申請時の適格性をSignPath Foundationが承認したという意味ではありません。

## 確認方法と範囲

- `go list -m -json all`で解決済みの外部Goモジュール18件を列挙し、各バージョンのLICENSE等の原文を確認。
- `GOOS=windows GOARCH=amd64 CGO_ENABLED=0`を指定し、`go list -deps -json -buildvcs=false .`で本体ビルドの依存パッケージを確認。うち7モジュールが本体ビルドで参照され、残り11件は本体の依存パッケージ一覧に現れない。
- 参照されないモジュールも、テスト・他OS・別パッケージ向けの依存グラフの一部として一覧に残し、ライセンス原文を収録。
- `go.mod`の指定バージョンとローカル置換を使用。`go.sum`だけに残る過去バージョンを現行配布物の依存物とは数えない。
- Go 1.26.11のLICENSE・PATENTSと、本体ビルドで参照される標準ライブラリ内のvendor依存（x/crypto、x/net、x/sys、x/text）のLICENSE・PATENTSも確認。
- 各ライセンス・著作権表示・NOTICE・PATENTSは[THIRD_PARTY_NOTICES.md](../THIRD_PARTY_NOTICES.md)へ原文を収録。分類名より原文を優先する。
- これはソースと依存パッケージの確認であり、公開済みEXEの逆解析・実行追跡や、最終リンクで到達可能な全関数の判定は行っていない。

## 本体で参照するGoモジュール

| モジュール | バージョン | 確認したライセンス |
| --- | --- | --- |
| github.com/go-ole/go-ole | v1.3.0 | MIT |
| github.com/jchv/go-webview2 | v0.0.0-20260205173254-56598839c808をローカル改変 | MIT |
| github.com/jchv/go-winloader | v0.0.0-20250406163304-c1995be93bd1 | ISC |
| github.com/shirou/gopsutil/v4 | v4.26.6 | BSD-3-Clause。コピー元Goコードの表示も原文に含む |
| github.com/yusufpapurcu/wmi | v1.2.4 | MIT |
| golang.org/x/image | v0.48.0 | BSD-3-Clause、PATENTS |
| golang.org/x/sys | v0.50.0 | BSD-3-Clause、PATENTS |

残り11件の正確なバージョンとライセンスはTHIRD_PARTY_NOTICESの一覧を参照。Apache-2.0、BSD-2-Clause、BSD-3-Clause、ISC、MIT、およびyaml.v3のファイル別MIT／Apache条件を確認した。

## go-webview2のローカル改変

上流`github.com/jchv/go-webview2@v0.0.0-20260205173254-56598839c808`をGoモジュールとして別途取得し、ローカルコピーと比較した。改行差を除く変更ファイルは次のとおり。

- `common.go`
- `webview.go`
- `pkg/edge/chromium.go`
- `pkg/edge/ICoreWebView2Controller.go`
- `internal/w32/w32.go`
- 追加した`WIDGET-GO-PATCHES.md`

変更内容は[既存の変更記録](../third_party/go-webview2/WIDGET-GO-PATCHES.md)に記載されている。LICENSE、SDKのLICENSE.txt、その他の比較対象テキストは改行差を除き上流と一致した。

Foundationの条件には改変上流コードに関する条件がある。ローカルコピーはGitHub上の独立したforkとして管理していないため、取り込みコミット・差分・用途を申請時に説明し、この構成での扱いを確認する。確認前に対応済みとはしない。

## WebView2Loader.dll

`third_party/go-webview2/webviewloader/README.md`は、同SDKをBSD-3-Clauseで再配布すると説明している。同梱の`webviewloader/sdk/LICENSE.txt`にMicrosoftの著作権表示とBSD-3-Clause条件があるため、原文をTHIRD_PARTY_NOTICESに収録した。

各DLLのファイルバージョンは`1.0.992.28`。指定した上流モジュールに含まれる各DLLとバイト単位で一致。このPCでの`Get-AuthenticodeSignature`は3件とも`Valid`、署名者はMicrosoft Corporationだった。

| SDK内のパス | SHA-256 | 現在のamd64配布との関係 |
| --- | --- | --- |
| x64/WebView2Loader.dll | C4674ACF95F0800793A4A6D61132ADF5DFA694C218E482D86093494C4E84100A | go:embedで本体EXEに内包 |
| x86/WebView2Loader.dll | 758F4003D91B3B81951D95DE28E0E03F96E23922A73D7FE0ABF6F607E2A89F2B | リポジトリ内に保持。現在のamd64ビルドの内包対象外 |
| arm64/WebView2Loader.dll | 255CAD81B768FB29E68D7B1605EC64E4DAEED8AD0F8E8A4547E1304E8AEC505D | リポジトリ内に保持。現在のamd64ビルドの内包対象外 |

これはDLLファイル自体の確認結果であり、本体EXEが署名済みという意味ではない。DLLの再配布表記とFoundationのOSS署名対象条件の適用は分け、埋め込み構成を申請時に説明する。WebView2 Runtime本体はEXEに同梱せず、利用環境にインストールされたものを使用する。

## 取り込み元・素材

- AI使用量機能は`ShuheiKonno/ClaudeMonitor`の`cc10dda`を取り込み元とする（`aiusage/host.go`等の記録）。そのコミットの[LICENSE](https://raw.githubusercontent.com/ShuheiKonno/ClaudeMonitor/cc10dda/LICENSE)でMITと`Copyright (c) 2026 Shuhei Konno`を確認し、原文をTHIRD_PARTY_NOTICESに収録した。
- `icon.ico`にはリポジトリ内の生成コード`cmd/genicon/main.go`がある。棒グラフの描画コードから生成する構成。
- 組み込みスキンはリポジトリ内のCSS/JSON。確認範囲で外部配信の画像・フォント・スクリプト依存は見つからない。利用者が後からインポートするスキンの権利は、本体同梱物の確認対象とは別。
- `golang.org/x/image/font/basicfont`のソースはBSD条件を示し、Face7x13をpublic domainのX11 misc-fixedフォント由来と説明している。別途ダウンロードするフォントは不要。
- GitHub Actionsや開発時の検査ツールは本体のGoモジュール依存とは別管理。Actionsのソースやツール本体を今回のEXEへ同梱する構成ではない。

## 申請・配布までに残る作業

- [ ] Foundationに改変上流コードとSDK DLLの内包構成を説明し、署名対象としての扱いを確認する。
- [ ] この文書、LICENSE、署名ポリシー、プライバシー説明をGitHubへ公開する。
- [ ] 次回リリースで、LICENSEとTHIRD_PARTY_NOTICESがEXEと一緒に取得できることを確認する。ワークフローへ添付を追加したが、変更後の製品リリースはまだ実施していない。リリース本文から署名ポリシー・プライバシー説明への案内も確認する。
- [ ] 依存物・Goツールチェーン・ビルド対象・素材を更新したら再確認し、表示を更新する。

ソースライセンスの確認は、Foundationの採否や将来のすべての配布条件を一括して保証するものではない。未確認事項を申請時に引き継ぐ。
