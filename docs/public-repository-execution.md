# 公開前監査・設定の実施記録

日付: 2026-11-14。利用者の指示に基づき、[ShuheiKonno/widget-go](https://github.com/ShuheiKonno/widget-go)をpublicへ変更し、改ざん・不正な署名・秘密情報流出への対策を適用した。

## 公開前監査

Gitleaks v8.30.1の公式Windows/x64配布物を、GitHubの公式リリースAPIにあるSHA-256と照合して使用した。取得・検査はこのPC内で行い、ソース・ログを外部の検査サービスに送っていない。

| 検査対象 | 範囲・結果 |
| --- | --- |
| Git履歴 | privateのミラーを別途取得し、PRのhead参照も取得。177コミット、696個のblobを対象に、Gitの変更履歴と各blobを検査。検出0件 |
| バイナリ | 大きさで除外せず、履歴中のDLL・EXE等からASCII／UTF-16LEの文字列を取り出して検査。検出0件 |
| Actionsログ | 121実行のうち115実行・450ファイルを取得・検査し、検出0件。残る6実行はGitHubがHTTP 410（期限切れ）を返し、本文取得不可 |
| Release | 27リリース・29添付ファイル（約211MB）を取得。EXE等の文字列とRelease本文を検査し、検出0件 |
| Issue・PR | 62件の本文とIssueコメント4件を取得・検査し、検出0件。レビュー・インラインコメントはAPI上0件。GitHub添付URLは検出0件 |
| 現行作業ツリー | 署名準備文書を含む作業ツリーを検査し、検出0件。変更後も公開前に再検査する |
| その他 | GitHub APIでWiki・Pages・Discussionsはいずれも無効 |

検出0件は、利用した検知ルールで見つからなかったという結果であり、あらゆる秘密情報の不存在やEXEの安全性を保証するものではない。バイナリの文字列検査は暗号化・圧縮された情報や全実行動作の解析ではない。

既存のコミット著者情報、通常の開発パス・機器情報・例示のネットワーク情報は、ソース公開の範囲として保持する。漏えいした実トークン等は見つからなかったため、履歴・タグ・過去のReleaseやログの削除／書換えは行わない。監査用ミラー・取得ログ・rawレポートはリポジトリ外に保管する。

## リポジトリへ追加した対策

- LICENSE・第三者表示・プライバシー・署名ポリシーを公開できる形に整備。
- `.gitignore`でローカル認証データ・ログ・環境変数ファイル・署名用秘密鍵等の混入を防止。
- `SECURITY.md`で非公開の脆弱性報告を案内。
- CODEOWNERSで既存の管理者・共同作業者を担当にし、公開・署名・依存物設定の変更は管理者を担当に設定。
- 読み取り権限のSecret scanワークフローを追加。固定版・固定SHA-256のGitleaksでGit履歴を検査。
- checkoutに認証情報を残さない設定を適用。govulncheckの実行版をv1.9.0へ固定。
- リリースタグがmainの履歴内にあり、実行コミットと一致し、EXEリソースのバージョンと一致することを検証。分岐した未取込みコードや不一致を拒否するテストを追加。
- ビルドと公開を分離し、write権限を持つ公開ジョブを`release` Environmentの手動承認対象に変更。LICENSE・第三者表示を配布物へ追加。

これはSignPath署名の実装ではない。Foundationの採用・設定・証明書提供後、署名・タイムスタンプ・署名検証の工程を追加する。

## GitHub側の設定結果

公開切替中はActionsを一時停止し、設定後に再開した。切替直後の一時的なリポジトリロック解除を待って適用し、APIで読み直して確認した。

| 対象 | 適用・確認結果 |
| --- | --- |
| 公開範囲 | `visibility: public`、`private: false`。未認証HTTPでもリポジトリ情報・LICENSE・SECURITY.mdを取得できることを確認 |
| mainのCI・履歴保護 | `main-integrity`（Ruleset 27250607）。test・lint・vuln・secretsをGitHub Actions発行元（15368）に限定して必須化。最新mainとの整合を要求し、force push・削除禁止。bypassは空 |
| mainのレビュー | `main-reviewed-changes`（27250608）。PR・承認1件・Code Owner承認・最新pushの別担当承認・会話解決を要求。所有者ShuheiKonnoだけがPR経由でレビューを省略できる。CIは別ルールなので省略できない |
| リリースタグ | `release-tag-ownership`（27250609）。v*タグの作成・更新・削除をShuheiKonnoだけに限定 |
| Actions | 再開済み。`allowed_actions: selected`、SHA固定必須。許可した7種類のActionパターンに限定。GitHub所有Action全体・verified Action全体の一括許可はいずれもfalse |
| 外部PR | `all_external_contributors`。外部貢献者のfork PRワークフローは実行承認必須 |
| トークン権限 | 既定read、ActionsによるPRレビュー承認はfalse |
| 公開Environment | releaseはShuheiKonnoの手動承認を要求し、対象をv*タグに限定。本人が自分の公開を承認できる設定。これは独立した二人による承認ではない |
| 秘密情報検知 | secret scanningとpush protectionがenabled。設定後のopen alert取得時は0件 |
| 脆弱性報告 | private vulnerability reportingがenabled |
| CODEOWNERS | GitHubのCODEOWNERS検証APIのerrorsは空 |

許可Actionはactions/checkout、actions/setup-go、actions/upload-artifact、actions/download-artifact、golangci/golangci-lint-action、softprops/action-gh-release、将来のSignPath連携用signpath/github-action-submit-signing-request。利用時にはすべてコミットSHA固定が必要。

既存共同作業者の権限、Git履歴、タグ、過去Release、過去ログは維持した。GitHub側で変更前のワークフロー実行まで新しいYAMLへ書き換わるわけではない。将来のSignPathトークンは保護したEnvironment内で管理し、旧ワークフローやPRテストへ渡さない。

## 検証と残る事項

- Gitleaks v8.30.1による公開前の最終履歴・作業ツリー検査も検出0件。
- リリース元検証のローカルテスト6件が成功。actionlint v1.7.12によるワークフロー検査も成功。
- ブートストラップコミット`257845334d1058fe22fc0bf1894ce47a3033ee5f`のGitHub CIでtest・lint・vuln・secretsがすべて成功してから公開した。testにはvet・通常テスト・raceテスト・ビルドが含まれる。
- 非公開の署名サービス利用ではなく、publicなOSSとして申請できる準備文書を公開した。Foundationへの申請・採用、SignPathアカウント設定、署名・タイムスタンプ工程の実装、本番EXEの署名はまだ行っていない。
- release Environmentを使う新しい製品リリースはまだ実行していない。実際の公開承認と、LICENSE／第三者表示の添付は次回リリース時にも確認する。
- GitHub・SignPathのMFA設定や個人の回復コードは本人による確認事項。APIがnullを返したため、MFAを確認済みとは扱わない。
