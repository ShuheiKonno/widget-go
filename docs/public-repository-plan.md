# パブリック化とソース・署名権限の保護計画

確認日: 2026-11-14

対象: `ShuheiKonno/widget-go`。利用者の目的は「改ざん・不正な署名・秘密情報流出を防ぐこと」。ソースの閲覧・コピーを禁止することは今回の目的に含めない。

この文書は初期調査時点の検討結果と実施計画です。その後、利用者から実施指示を受けています。監査・公開・保護設定の実績と残る事項は[実施記録](public-repository-execution.md)を参照してください。

2026-11-14にpublicへの変更とGitHub保護設定の適用・再取得を完了しました。以下のprivate状態等は比較用の初期調査記録です。

## 1. 推奨する方針

**公開前監査を終えてから既存リポジトリをpublicにし、main・リリースタグ・署名承認を別々に保護する。** Git履歴を初めから捨てたり、コードを難読化したりすることは、今回の目的への基本対策にはしない。

公開すると誰でもソースを閲覧・forkできるが、第三者が元リポジトリへ書き込めるようになるわけではない。元リポジトリの権限、変更のレビュー、CI、リリース権限、署名の検証で正規配布物を保護する。[G1][G2]

MITは著作権表示等を条件にコピー・改変・再配布・販売を許可するライセンスであり、ソースを秘密にする仕組みではない。著作権表示を維持し、正規配布先と署名方針を明確にする。将来、利用制限が目的になった場合は、今回の公開方針とFoundationのOSS条件を再検討する。[L1][S1]

## 2. 初期調査時点の状態

GitHub APIの読み取り結果とローカルの検査結果を分けて記録する。

| 項目 | 確認結果 |
| --- | --- |
| 公開範囲 | GitHub APIで`private: true`、`visibility: private` |
| main | リモートとローカルのorigin/mainは`1d5db2b6998dca85480de24d1bb7fcc7782da4b6`で一致。APIの`protected`はfalse |
| 書き込み権限 | 管理者1アカウント、write権限1アカウント。公開前に継続要否とMFAを確認する |
| Ruleset・詳細保護設定 | privateの現契約では取得APIが403（Proへのアップグレードまたはpublic化を要求）。設定できるとは仮定しない |
| Actions | 有効。利用Actionは`all`、SHA固定の強制はfalse。現行YAML内のusesはコミットSHAで固定 |
| GITHUB_TOKENの既定値 | read。ActionsによるPRレビュー承認は無効 |
| Secrets・Environment | リポジトリActions Secretsは0件、Environmentは0件。実行時環境や個人PCまで秘密情報がないという意味ではない |
| Deploy key・Webhook | API取得時点ではともに0件 |
| MFA | APIの値がnullであり、本人・共同作業者のMFAは確認できていない |
| リモート参照 | 通常ブランチはmainのみ、タグは27件。PR参照や添付物までは監査済みではない |
| Actions履歴 | 121実行。Artifact一覧は0件。ログ本文は未検査 |
| Issue・PR | API取得時点でIssue 11件、PR 51件。本文・コメント・添付物は未検査 |
| .gitignore | EXE・IDE等の除外はあるが、認証用プロファイル・ローカルログ・署名鍵・.env等への包括的な防止ルールは未整備 |

### ローカル履歴の予備検査

- `git rev-list --all`でローカル到達可能な155コミットを対象にした。
- 680個の到達可能なblobのうち、3MB以下でNULを含まない609個をテキストとして検査した。
- 秘密鍵ヘッダー、GitHub/AWS/OpenAI/Slackトークン形式、JWT形式、認証情報付きURL、一定長の資格情報リテラルを検査した。**このルールに該当する候補は0件**だった。
- 秘密情報の値を出力したり、ソースを外部の検査サービスへ送信したりしていない。
- 大きさで除外した10個は過去の`system-widget.exe`（約9.8〜10.1MB）。小さいバイナリ61個もこのテキスト検査の対象外。過去のEXEを含めた漏えい不存在の証明ではない。
- 汎用の高エントロピー検査、プロバイダー照会、リモートにしかない参照、Actionsログ、Release添付物、Issue/PR本文・添付物は対象外。専用スキャナーによる公開前監査は残っている。
- 現在の追跡ファイルには、開発PCの絶対パスやプライベートIP形式がある。例示・テストデータも含むため、一括削除せず内容を分類する。
- コミットの作成者・コミッター情報には、GitHub noreply以外のアドレスが6種類ある。一般公開してよいか関係者ごとに確認する。値そのものはこの文書へ転記していない。

絶対パスの確認候補: `docs/gpu-probe-report.md:80`、`docs/plan-claude-monitor.md:4`、`docs/plan-skins.md:129`、`docs/settings-window.md:54`、`shortcut/shortcut.go:132`、`shortcut/shortcut_test.go:114`。後二つ等には一般的な例もある。現行ファイルだけを直しても過去履歴の記載は消えない。

## 3. 公開前に完了させる作業

| 順序 | 作業 | 完了条件 |
| --- | --- | --- |
| P1 | 全体の秘密情報監査 | 専用スキャナーをローカルで使い、全履歴・タグ・現行作業ツリー・大きいファイル／バイナリの確認を行う。除外と誤検出を記録する |
| P2 | ソース以外の公開対象を監査 | Actionsの過去ログ、Release本文・添付EXE、Issue/PR・コメント・画像、Wiki/Pages等の有無を確認。公開不可情報を解決する |
| P3 | 個人・社内情報の扱いを決定 | コミットメール、開発パス、実在するネットワーク情報、プローブ結果等を確認。残すものは本人・関係者が公開を許容できる |
| P4 | 発見時の対処 | 実際のトークンなら先に失効・再発行し、その後に履歴・タグ・ログ等の除去を検討。削除だけで無効化済みとは扱わない |
| P5 | 混入防止を実装 | .gitignore、ローカル／CIの秘密情報検査、文書による取扱いルールを追加 |
| P6 | 権限と回復手段を確認 | 管理者・write担当・GitHub Apps・MFA・回復コード管理を確認。不要な権限は実施時に整理 |
| P7 | 署名準備文書を反映 | LICENSE・第三者表示・プライバシー・署名ポリシーをレビューしmainへ反映。サービス未提供の状態を正しく表示 |
| P8 | 公開と保護設定をまとめて実施する準備 | mainとv*タグの設定値、必須チェック名、外部PRの実行方針を決める。実施前に確認可能な内容を揃える |

初期調査ではP1・P2の一部のみを予備確認した。追加監査・実装・設定の結果は実施記録へ追記する。

混入防止ルール案は、`.env*`（安全なexampleだけ例外）、`*.pfx`・`*.p12`・秘密鍵、`app*.log*`・`notify.log*`・ダンプ、本体の`config.json`と認証用WebView2プロファイル等を対象にする。意図して配布する公開証明書やテスト資材まで隠さないよう個別に調整する。`.gitignore`は既に追跡済みのファイルやGit履歴を削除する機能ではない。

## 4. mainへの改ざん防止

公開後に使えるbranch Rulesetでmainを保護する。publicではGitHub FreeでもRulesetを利用できる。privateで現在403となる機能のため、公開後の設定確認までを一つの実施手順として扱う。[G2]

- mainへの変更はPR経由にする。force pushと削除を禁止する。
- 現在のTestワークフローの`test`・`lint`・`vuln`について、実際にGitHubが返すチェック名と発行元を確認して必須にする。文字列を推測して登録しない。
- 原則、他の信頼する人によるレビューを1件要求し、追加変更時は古い承認を無効化する。作成者は自分のPRを承認できないため、承認可能な別の担当者との運用合意が必要。
- CODEOWNERSで`.github/workflows/`、署名設定、依存物・ビルド定義、LICENSE／署名ポリシー等の変更を担当者へ通知する。CODEOWNERSだけではレビューは強制されないため、保護ルールと合わせる。
- CODEOWNERSの担当者を一人だけにして本人のPRまでその本人の承認を必須にすると作業が止まる。既存の共同作業者を含むレビュー体制を決めるか、単独開発時の例外と署名時の確認を明示する。
- 管理者の無制限なbypassを通常経路にしない。緊急時の例外担当と記録方法を限定する。

## 5. 不正なリリース・署名の防止

| 段階 | 保護案 |
| --- | --- |
| リリースタグ | `v*`のtag Rulesetで作成者を限定し、更新・削除を制限。管理者ロール等をbypassへ広く許可する場合は、その権限範囲を確認する |
| タグが指すソース | 署名要求前にタグのコミットが許可したmainの履歴に含まれることと、対象バージョンを検証。任意ブランチへのタグだけで署名しない |
| ビルド | GitHub-hosted runnerで対象コミットからビルド。外部PRの成果物を本番署名へ流用しない |
| トークン | SignPathの署名要求用トークンは保護したEnvironment等へ保存。main／許可するリリースタグの実行へ限定し、PRテストには渡さない。Environmentに設定できる保護項目は実施時に確認する |
| 署名許可 | SignPath側でリポジトリ・由来・対象ファイル・製品名／バージョンを制約化し、各要求を人が手動承認。GitHubのwrite権限やAPIトークンだけを承認の代わりにしない |
| 正規配布物 | 署名・タイムスタンプ検証後のEXEからSHA-256を生成し、検証成功時だけReleaseへ公開。署名済みパスを固定 |
| 障害時 | 拒否・時間切れ・取得失敗・検証失敗時は公開停止。未署名版へ自動で代替しない |

トークンの漏えいだけで承認を完結できない設計にする。GitHub署名付きコミットを採用する場合も、それは変更者の識別を助ける別の機能であり、EXEのAuthenticode署名とは区別する。[S1]

## 6. 公開後のGitHub設定

- 外部forkのPRワークフローは、すべての外部貢献者について実行前承認を要求する。レビュー前にPRコードを実行しない。[G3]
- 現行の`pull_request`テストと本番リリースを分離する。`pull_request_target`等の権限の強いイベントで、未信頼のPRコードをcheckoutして実行しない。[G3]
- GITHUB_TOKENの既定readと、ActionsによるPRレビュー承認の無効を維持する。`contents: write`は最終公開ジョブに限定する。
- 利用Actionの許可範囲とSHA固定をGitHub設定でも制約する。Goの検査ツール等、`uses:`以外で取得・実行するツールの固定・更新方針も決める。
- 公開リポジトリ向けのsecret scanningの稼働を確認し、push protectionを設定・確認する。検知対象に限界があるため、ローカル検査の代わりにはしない。[G4]
- SECURITY.mdと非公開の脆弱性報告経路を整える。公開Issueへ認証情報や未修正の詳細を投稿させない。

## 7. publicへの切替手順案

1. 公開前監査、必要な失効・除去、文書レビューを完了し、公開対象コミットと残す履歴・タグ・ログを確定する。履歴書換えやログ削除が必要な場合は、対象と影響を示して別途実施判断する。
2. 公開と保護設定を実施する時間帯を決め、共同作業者の変更・タグ操作を止める。必要なバックアップは公開リポジトリ外で保管する。
3. 切替中にリリースが走らないよう、実施時にActionsを一時停止する案を採る。この文書作成時は停止していない。
4. 公開範囲変更の対象・公開される内容を確認し、明示された実施指示の下でGitHubの`Settings → General → Danger Zone → Change repository visibility`からpublicへ変更する。[G1]
5. 直後にmainとv*タグのRulesetを適用し、Actionsの外部PR承認、許可Action、トークン権限、secret scanning等を設定・確認する。private向けpush Rulesetがpublicで維持されるとは考えない。[G1][G2]
6. 設定をAPI・管理画面で読み直し、PR／タグの保護とCIを確認してからActionsを再開する。本番署名用トークンの登録は保護経路を確認してから行う。
7. ログインしていない閲覧者の視点でも、公開ファイル・履歴・ログ・添付物を確認する。READMEに正規配布先と署名方針が表示されることを確認する。
8. 公開したリポジトリ・説明文書のURLを使ってFoundationへ申請する。

public化は完全には取り消せない。後からprivateに戻しても、既に作られた公開forkや第三者が取得したコピーは回収できないため、切替より前の監査を必須とする。[G1]

## 8. 公式資料

- [G1: GitHubの公開範囲変更と影響](https://docs.github.com/en/repositories/managing-your-repositorys-settings-and-features/managing-repository-settings/setting-repository-visibility)
- [G2: Rulesetの利用範囲](https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/managing-rulesets/about-rulesets)、[設定できるルール](https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/managing-rulesets/available-rules-for-rulesets)
- [G3: GitHub Actionsの権限と外部PR承認](https://docs.github.com/en/repositories/managing-your-repositorys-settings-and-features/enabling-features-for-your-repository/managing-github-actions-settings-for-a-repository)
- [G4: Secret scanning](https://docs.github.com/en/code-security/secret-scanning/introduction/about-secret-scanning)
- [L1: OSIのMITライセンス原文](https://opensource.org/license/mit)
- [S1: SignPath FoundationのOSS条件](https://signpath.org/terms)
