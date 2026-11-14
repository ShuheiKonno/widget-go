# Code signing policy

更新日: 2026-11-14

## 現在の状態

system-widget（widget-go）は、2026-11-14にSignPath FoundationのOSS向け無償コード署名プログラムへ**申請を送信し、審査・案内待ち**です。採用、証明書の提供、本番署名の開始を示すものではありません。現行のリリースワークフローにはコード署名工程がありません。[申請記録](signpath-application.md)を参照してください。

リポジトリはpublicへ変更済みで、main・リリースタグ・GitHub上の公開権限を保護しています。[公開前監査・設定の実施記録](public-repository-execution.md)を参照してください。公開リポジトリであることやGitHub上の公開承認は、EXEのコード署名済みを意味しません。

本書は署名運用開始に向けた方針です。SignPath側の審査結果、提供された条件・設定を確認し、実装と運用を整えてから署名を開始します。

## 対象と提供元

- リポジトリ: https://github.com/ShuheiKonno/widget-go
- 配布先: https://github.com/ShuheiKonno/widget-go/releases
- 対象: このリポジトリのソースからGitHub-hosted runnerでビルドしたWindows/amd64の`system-widget.exe`。
- 導入予定: [SignPath.io](https://signpath.io/)による署名サービスと、[SignPath Foundation](https://signpath.org/)名義の証明書。

採用と提供開始を確認した後、READMEとリリースページには次の提供表記を掲載する予定です。**現時点の提供実績を表す記載ではありません。**

> Free code signing provided by [SignPath.io](https://about.signpath.io/), certificate by [SignPath Foundation](https://signpath.org/).

改変した上流コード・埋め込みDLLの説明は[依存物確認記録](dependency-license-audit.md)と[第三者ライセンス表示](../THIRD_PARTY_NOTICES.md)にまとめています。これらの署名対象としての適格性はFoundationに確認します。

## 担当と承認

署名運用開始時の担当予定を、現在のリポジトリ管理者に合わせて以下のとおり定義します。GitHub所有者の多要素認証は設定画面でEnabledを確認済みです。SignPath側の権限・多要素認証・役割の兼任可否と、共同作業者のMFAは本番署名開始前に確認・設定します。

| 役割 | 担当予定 | 責任 |
| --- | --- | --- |
| Authors / Committers | [ShuheiKonno](https://github.com/ShuheiKonno) | 本体、依存物、ビルド・CIの変更を管理 |
| Reviewers | [ShuheiKonno](https://github.com/ShuheiKonno) | 外部貢献の変更を確認してから取り込み。署名対象に関わる変更を確認 |
| Approvers | [ShuheiKonno](https://github.com/ShuheiKonno) | 各署名要求のリポジトリ、コミット、タグ、検査結果、成果物を確認して手動承認 |

関係者はGitHubとSignPathで多要素認証を使用します。担当変更時は公開方針と両サービスの権限を更新します。自動化エージェントやCIのトークンを人による署名承認の代わりにしません。

## 運用開始後のリリース方針

1. ソース・依存物・ビルド定義を確認し、既存のvet・test・脆弱性検査を通す。
2. GitHub-hosted runnerでビルドし、GitHub Artifactとして保存した対象EXEをSignPathへ提出する。
3. 製品名・製品バージョンとビルドの由来を制約で確認し、担当者が各リリースを手動承認する。
4. 公開用証明書によるAuthenticode署名とタイムスタンプを持つEXEを取得し、Windowsで信頼性と製品情報を検証する。
5. 署名済みEXEのSHA-256を生成し、同じEXEとライセンス表示を公開する。署名後にEXEを変更しない。
6. 署名拒否、承認待ちの時間切れ、取得失敗、検証失敗時は公開を止める。自動的に未署名EXEへ切り替えない。

署名済みEXEのプロパティではデジタル署名を、添付SHA-256では配布物との一致を確認できます。SHA-256ファイルだけでは発行元の保証にはなりません。有効な署名があってもSmartScreenの警告が残る場合があります。

## プライバシー・問題の報告

アプリの通信、保存データ、設定変更、削除方法は[プライバシーとシステムへの変更](../PRIVACY.md)を参照してください。現行本体にはSignPathへの利用情報送信機能はありません。

通常の不具合は[GitHub Issues](https://github.com/ShuheiKonno/widget-go/issues)へ、脆弱性や漏えいの報告は[SECURITY.md](../SECURITY.md)の非公開経路へお願いします。認証情報・署名用トークン等は公開しないでください。署名運用開始後に署名権限やトークンの漏えいが疑われた場合は、要求を停止し、トークン失効・署名履歴の確認と必要なFoundationへの連絡を行います。

## 根拠と導入手順

- [Foundation公開条件（Draft表記あり）](https://signpath.org/terms)
- [GitHub Actionsとの連携](https://docs.signpath.io/trusted-build-systems/github)
- [このプロジェクトの導入手順](plan-code-signing.md)
- [MicrosoftによるSmartScreenの説明](https://learn.microsoft.com/en-us/windows/apps/package-and-deploy/smartscreen-reputation)
