# SignPath Foundationによるコード署名の導入手順

作成日: 2026-11-14

対象: https://github.com/ShuheiKonno/widget-go の `system-widget.exe`（Windows/amd64）。GitHub ReleasesでのEXE配布を継続する。

この文書はSignPath導入の対応計画です。2026-11-14にLICENSE、第三者表示、依存物確認記録、署名ポリシー、プライバシー説明を作成しました。GitHub公開と保護・リリース工程変更の実施状況は[実施記録](public-repository-execution.md)へまとめます。Foundationへの申請、SignPathアカウント設定、本番署名は未実施です。

作成した文書: [LICENSE](../LICENSE)、[第三者ライセンス表示](../THIRD_PARTY_NOTICES.md)、[依存物確認記録](dependency-license-audit.md)、[Code signing policy](code-signing-policy.md)、[プライバシーとシステムへの変更](../PRIVACY.md)。

初期調査時のGitHubリポジトリはprivateでした。公開前監査とmain・リリースタグ・署名権限の保護を先に進める[パブリック化と保護計画](public-repository-plan.md)に沿って作業を進めています。最新状態は実施記録を参照してください。

## 1. 到達目標と前提

- SignPath FoundationのOSS向け無償プログラムに申請し、採用後に公開用証明書で署名する。
- 証明書の名義はSignPath Foundationになる。本人名義の証明書を取得する方式ではない。
- 証明書の秘密鍵はSignPath側のHSMで管理される。ローカルへのPFX取得やUSBトークンの購入を前提にしない。[S1]
- 各リリースの署名には人による承認を入れる。GitHubへのタグpushだけで無承認の本番署名を完結させない。[S2]
- 完了条件は「信頼された署名とタイムスタンプを持つEXEが配布されること」。SmartScreenの警告表示は別途記録する。署名が有効でも新しい配布物では警告が残る場合がある。[S6]
- 無償プログラムは審査制。日本からの採用可否、審査期間、回数上限、当プロジェクトの採否は未確認。公開条件ページはDraft表記なので、申請時の案内と照合する。[S2]

## 2. 署名準備開始時の構成

| 項目 | 確認結果 | 必要な対応 |
| --- | --- | --- |
| ライセンス | MITのLICENSE本文と第三者ライセンス表示を追加済み | GitHubへの公開と、将来の配布物への添付 |
| リリース | `.github/workflows/release.yml`でタグ`v*`を契機に公開 | 署名・承認・検証を追加 |
| 検査 | Windows runnerでvet・test・govulncheck | 継続する |
| ビルド | Ubuntu runnerでWindows/amd64にクロスコンパイル | 継続可能。署名と検証はWindows runnerにまとめる案 |
| 配布物 | EXEとSHA-256。コード署名工程なし | 署名済みEXEと、そのEXEのSHA-256を公開 |
| メタデータ | `winres/winres.json`にProductName・ProductVersion等あり | SignPathで同じ製品名・対象バージョンを制約として設定 |
| ローカル依存 | `third_party/go-webview2`の上流比較・ライセンス確認済み | Foundationへの適格性の確認 |
| 埋め込みDLL | SDKのBSD条件を収録。3種のDLLは上流と一致し、Microsoftの有効な署名を確認 | 埋め込み構成のFoundationへの説明 |

リリース済みバージョン・公開範囲・リポジトリ管理権限は、申請直前にGitHub上でも確認する。

## 3. 対応タスク一覧

「申請者」はリポジトリ所有者または管理担当者。「開発」はリポジトリの文書・実装を変更する担当者。

| ID | タスク | 主担当 | 完了条件 |
| --- | --- | --- | --- |
| A0 | 公開前監査とリポジトリ保護 | 申請者・開発 | privateの履歴・ログ等の監査と、main・v*タグ・Actions保護を整えた上でpublicへ変更する |
| A1 | OSS条件と依存物の確認 | 開発・申請者 | ライセンス、改変依存、埋め込みDLLについて申請用に説明できる |
| A2 | ライセンス・署名ポリシー・プライバシー説明の公開 | 開発・申請者 | 公開URLと担当者が確定し、申請前の文書が公開されている |
| A3 | GitHubの多要素認証と権限確認 | 申請者 | 関係者のMFA、管理者、レビュー担当、承認担当が確定 |
| B1 | SignPath Foundationへの申請 | 申請者 | 申請を送信し、問い合わせに対応できる |
| B2 | 審査対応 | 申請者・開発 | 採否と必要な条件を確認。採用後に連携設定へ進める |
| C1 | SignPathアカウント・プロジェクト設定 | 申請者 | MFA、Trusted Build System、署名対象、証明書、手動承認設定が整う |
| C2 | GitHub連携設定 | 申請者・開発 | 必要な連携許可、APIトークン、識別子を設定 |
| C3 | メタデータ整合とリリース工程の変更 | 開発 | 署名・取得・検証・チェックサム・公開が依存関係を持って実行される |
| D1 | 初回の署名試験 | 開発・承認担当 | 承認待ち・承認・署名取得・検証が実際に成功 |
| D2 | 公開失敗条件の確認 | 開発 | 署名拒否・失敗・待機時間超過・検証失敗時に公開されない |
| D3 | 初回の署名済みリリース | 承認担当・開発 | ダウンロードしたEXEで署名・起動・SHA-256を確認 |
| D4 | 継続運用の記録 | 申請者・開発 | 承認、権限変更、トークン失効・再発行、障害時の手順を記録 |

## 4. 申請前の具体的な手順（A1〜A3）

1. [x] ルートに`LICENSE`を追加する。READMEのMIT表記に対応する本文を追加し、2026年の履歴・取り込み元の表示に基づきShuhei Konnoとwidget-go contributorsを記載した。
2. [x] Go依存物、同梱コード、アイコン等の素材、埋め込みDLLのライセンスと配布条件を確認し、`THIRD_PARTY_NOTICES.md`と`docs/dependency-license-audit.md`にまとめた。Goモジュール18件、本体で参照する7件、ツールチェーン内vendor依存、ClaudeMonitorのMIT表示を収録した。
3. [ ] `third_party/go-webview2/WIDGET-GO-PATCHES.md`を基に、上流リポジトリ、取り込みコミット、変更内容を説明できるようにする。Foundationの改変上流コードに関する条件がこの構成へどう適用されるかを確認する。単なるローカルコピーで条件を満たすとは断定しない。[S2]
4. [ ] `third_party/go-webview2/webviewloader/sdk/*/WebView2Loader.dll`について、出所、ライセンス、既存署名、バイナリ内包方法を確認する。OSS条件やSystem Librariesの例外との関係で判断が必要なら、申請時に説明して確認する。DLLの同梱だけで適格・不適格と決めない。[S2]
5. [ ] `docs/code-signing-policy.md`等を用意し、READMEとリリースページからリンクする。見出しまたはリンク名に`Code signing policy`を使い、Authors / Reviewers / Approversと実際の担当者、プライバシー説明へのリンクを記載する。[S2]
6. [x] Foundation指定のクレジット案とリンクを署名ポリシーに用意し、「申請準備中」「採用後の表記」と明示した。採用後の正式な提供表記への切替は今後行う。[S2]
7. [x] `PRIVACY.md`を作成し、通信先、利用者が有効化する条件、認証データの保存・移行、ログ、ログアウト・削除、システム変更をソースと照合した。アカウントを使った通信の実測・GUIでの通知確認は未実施。
8. [ ] システム設定変更と削除方法の説明を確認する。スタートアップ登録、電源モード切替、集中モードによるウィンドウ非表示・通知設定変更について、利用者への説明と有効化・無効化・復元の動作を確認する。READMEの既存アンインストール手順も照合する。公開説明だけでは足りない箇所は、必要なアプリ表示の修正タスクにする。[S2]
9. [ ] GitHubの公開リポジトリ、既存リリース、ダウンロード案内を確認する。READMEの機能説明と現行バイナリを一致させる。
10. [ ] GitHubとSignPathで関係者が多要素認証を利用する方針を確定する。署名を承認するGitHubユーザー／担当者を決める。個人開発で役割を兼任できるか等は、申請時の設定・案内で確認する。

申請前に必要な文書を公開する。アプリの通信・設定変更に関する確認は申請説明を正確にするためのものであり、現時点で条件違反と判断したものではない。

## 5. 申請手順（B1〜B2）

1. [ ] https://signpath.org/apply を開き、表示された申請フォームへ進む。
2. [ ] 次の情報を手元に用意し、フォームで求められた項目を記入する。これは準備用の一覧であり、フォーム項目を取得・確認できたという意味ではない。

| 準備する情報 | 当プロジェクトでの内容 |
| --- | --- |
| プロジェクト名 | system-widget / widget-go。表示名は申請前に統一 |
| リポジトリ | https://github.com/ShuheiKonno/widget-go |
| ダウンロード・リリース | https://github.com/ShuheiKonno/widget-go/releases |
| 機能概要 | Windows向け常駐システムモニター。任意のAI使用量・タイマー・Ping機能等も説明 |
| ライセンス | MIT。公開したLICENSEへのURL |
| 署名対象 | Windows/amd64向けsystem-widget.exe |
| CI | GitHub ActionsのGitHub-hosted runner。Goでビルド |
| 管理者・連絡先 | 申請者本人が記入 |
| 各種方針 | 署名ポリシー、プライバシー説明、依存物の説明への公開URL |
| 配布実績 | 既存リリース等の確認可能な情報。利用者数や実績は推測しない |

3. [ ] 「Foundationの証明書も必要なOSS向け無償提供」を希望することを明確にする。自前証明書を持ち込む無料サービス枠との混同を避ける。[S1][S2]
4. [ ] 改変依存コードと埋め込みDLLについて、必要な説明・確認事項を添える。
5. [ ] 公開条件と申請時の条件を照合して送信し、追加質問に回答する。採用、署名対象範囲、上限・運用条件を確認して記録する。

## 6. 採用後のアカウント・連携設定（C1〜C2）

1. [ ] 提供された案内からSignPathの組織・アカウントへ参加し、多要素認証を設定する。
2. [ ] 当リポジトリを対象とするProjectを確認／作成し、Trusted Build SystemとしてGitHub.comを関連付ける。案内に従ってSignPath GitHub Appを対象リポジトリへ許可する。Appが必須かは利用する監査機能・接続方法による。[S3]
3. [ ] 署名対象を`system-widget.exe`に絞るArtifact Configurationを作成する。GitHub ArtifactをZIPとして渡す方式なら、ZIP内のEXEを対象にする構成に合わせる。[S3][S4]
4. [ ] EXEにAuthenticodeのSHA-256署名を付ける設定と、タイムスタンプを確認する。製品名・製品バージョンのメタデータ制約を設定し、対象外のEXEを署名しない。[S2][S4]
5. [ ] Foundationから提供された公開用証明書をSigning Policyに関連付け、承認者と手動承認の条件を設定する。画面上の名称・識別子は実際に提供されたものを使用する。
6. [ ] プロジェクトの署名要求を送れるAPIトークンを発行し、GitHubの`Settings → Secrets and variables → Actions`で`SIGNPATH_API_TOKEN`として保存する。秘密鍵・トークンを文書やリポジトリへ記載しない。
7. [ ] 次の識別子を確認し、GitHub Variables等へ保存する。下記の変数名はこのプロジェクトで採用する案であり、SignPathが発行する固定名称ではない。

| GitHub側の設定名（案） | 内容 |
| --- | --- |
| Secret: SIGNPATH_API_TOKEN | 署名要求を送信するトークン |
| Variable: SIGNPATH_ORGANIZATION_ID | Organization ID |
| Variable: SIGNPATH_PROJECT_SLUG | Project slug |
| Variable: SIGNPATH_SIGNING_POLICY_SLUG | 公開用Signing Policy slug |
| Variable: SIGNPATH_ARTIFACT_CONFIGURATION_SLUG | Artifact Configuration slug。既定設定を使う場合は省略可 |

## 7. リリース工程の実装手順（C3）

変更対象の中心は`.github/workflows/release.yml`。アプリの機能改修とは分けて実装する。

```text
v*タグをpush
  → Windows runner: 既存のvet / test / govulncheck
  → Ubuntu runner: Windows/amd64向けEXEをビルド
  → GitHub Artifactに未署名EXEをアップロード
  → Windows runner: Artifact IDでSignPathへ署名要求
  → 承認担当者: SignPathで対象タグ・コミット・成果物を確認し手動承認
  → Windows runner: 署名済みEXEを別ディレクトリへ取得
  → 署名・タイムスタンプ・製品名・バージョンを検証
  → 署名済みEXEからSHA-256を生成
  → GitHub Releasesへ署名済みEXEとSHA-256を公開
```

- [ ] ビルド・署名要求までの全ジョブをGitHub-hosted runnerで実行する。ローカルビルドを任意アップロードして本番署名する設計にしない。[S3]
- [ ] `actions/upload-artifact`のArtifact IDをジョブ出力で受け渡す。
- [ ] `signpath/github-action-submit-signing-request`を組み込み、APIトークン、Organization ID、Project slug、Signing Policy slug、Artifact IDを渡す。[S3][S5]
- [ ] `wait-for-completion: true`と`output-artifact-directory`を設定し、署名済み成果物を取得する。未指定ではダウンロードされない点に注意する。[S3]
- [ ] 手動承認を待つ時間を決め、Actionの待機上限とGitHubジョブの上限を合わせる。Actionの既定待機上限は600秒。時間切れの場合は公開せず、SignPath側の要求状態を確認してから再試行する。[S3]
- [ ] 新たに使うActionsも、既存方針に合わせて確認済みのコミットSHAで固定する。
- [ ] 読み取り系ジョブは`contents: read`と必要な`actions: read`を設定し、Release作成を担うジョブに`contents: write`を付与する。署名トークンを使うのは信頼するリリース実行に限定する。
- [ ] `winres/winres.json`の製品名・バージョンと、ビルドされたEXE内の値を照合する。JSONを変更した場合は`rsrc_windows_*.syso`を再生成してからビルドする。
- [ ] タグ`v2.10.0`と数値版`2.10.0.0`のような表現差は正規化して照合する。SignPathの制約にも同じルールを適用する。
- [ ] 公開対象パスを署名済みEXEのパスに限定する。署名失敗時の未署名EXEへの代替、`continue-on-error`等での公開継続を入れない。
- [ ] SHA-256は署名後に生成する。署名後のEXEへリソース追加・再ビルド等を行わない。
- [ ] リリース本文へ署名ポリシーへのリンクを追加し、採用後の正式な提供表記を掲載する。

## 8. 検証と初回公開（D1〜D3）

1. [ ] 公開前の試験では、Release作成を実行しない構成で署名の一連の流れを確認する。試験用証明書が提供される場合は利用できるが、公開用証明書での信頼検証も別途行う。
2. [ ] 承認担当者がSignPathの要求で対象リポジトリ・タグ・コミット・ビルド結果・製品情報を確認し、承認する。
3. [ ] 署名済みEXEを取得したWindows runnerで、Windows SDKのSignToolを利用可能にして検証する。コマンドがPATH上にない場合はSDK配下から実行ファイルを特定する。

```powershell
signtool verify /pa /all /v .\signed\system-widget.exe
if ($LASTEXITCODE -ne 0) { throw 'コード署名の検証に失敗しました' }

$signature = Get-AuthenticodeSignature -FilePath .\signed\system-widget.exe
if ($signature.Status -ne 'Valid') { throw '署名が有効ではありません' }
if ($null -eq $signature.TimeStamperCertificate) { throw 'タイムスタンプがありません' }

$signature.SignerCertificate.Subject
$signature.TimeStamperCertificate.Subject
```

上記は実装時の検証例であり、この文書作成時に署名済みEXEで実行したものではない。証明書のSubject・発行元は提供された証明書と照合する。製品名・バージョンとSHA-256の検証も追加する。

4. [ ] 「承認待ちでは公開されない」「拒否・時間切れ・署名失敗・検証失敗では公開されない」を確認する。署名済みファイルが取得できない場合も同じ扱いにする。
5. [ ] 初回公開のタグを決め、通常のリリース工程で署名済みEXEを公開する。
6. [ ] テスト用Windows環境でGitHub Releasesから改めてダウンロードする。プロパティのデジタル署名、有効性、SHA-256、初回起動と主要機能を確認する。
7. [ ] SmartScreenの表示有無・Windowsのバージョン・取得経路を記録する。警告が出た場合は、まず署名検証の結果と区別して扱う。[S6]

## 9. 継続運用（D4）

- [ ] 各リリースで、変更確認 → 検査 → ビルド → SignPathの手動承認 → 検証 → 公開を繰り返す。
- [ ] 承認した要求ID、タグ、コミット、成果物ハッシュを追跡できるよう、実行ログまたはリリース記録に残す。
- [ ] トークンの失効・再発行、承認者の追加・削除、連携許可の変更手順を記録する。漏えい時は失効し、署名履歴を確認する。
- [ ] Foundationからの証明書・条件更新通知を確認し、必要な設定変更と署名検証を行う。手動で証明書を購入・更新する前提にはしない。
- [ ] サービス停止・審査条件変更時はリリースを保留し、未署名版を自動公開する運用を避ける。

## 10. 参照した公式情報

2026-11-14確認。申請フォームの内部項目は埋め込みフォームのため取得できておらず、画面での確認が必要。

- [S1: SignPath Foundationの無償提供](https://signpath.org/)
- [S2: FoundationのOSS向け公開条件（Draft表記あり）](https://signpath.org/terms)
- [申請ページ](https://signpath.org/apply)
- [S3: GitHub Actions連携と入力パラメーター](https://docs.signpath.io/trusted-build-systems/github)
- [S4: Artifact Configuration・Authenticode・メタデータ制約](https://docs.signpath.io/artifact-configuration/reference)
- [S5: 公式GitHub Action](https://github.com/SignPath/github-action-submit-signing-request)
- [S6: Microsoft SmartScreenとコード署名の関係](https://learn.microsoft.com/en-us/windows/apps/package-and-deploy/smartscreen-reputation)
