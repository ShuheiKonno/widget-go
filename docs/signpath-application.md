# SignPath Foundation申請記録

日付: 2026-11-14

## 状態

- GitHub所有者ShuheiKonnoの認証設定画面で、Two-factor authenticationがEnabledであることを確認した。設定変更は行っていない。回復コードを表示・取得していない。
- Foundationの実際の申請フォームへ入力し、本人がreCAPTCHAを完了した後、**「Form submitted」「Thank you, we'll be in touch soon.」の表示を確認した。申請は送信済みで、審査・案内待ち。**
- Foundationの採用・証明書提供、SignPathアカウントのMFA、本番EXEへの署名は未完了。GitHubのMFA確認とSignPathのMFA設定は別の作業。

連絡先は所有者のGitHub認証済み主メールを使用する。メールアドレスそのものはこの公開文書へ記載しない。

## 入力したプロジェクト情報

| 項目 | 内容 |
| --- | --- |
| Project Name | widget-go (system-widget) |
| Repository / Homepage | https://github.com/ShuheiKonno/widget-go |
| Download URL | https://github.com/ShuheiKonno/widget-go/releases |
| Privacy Policy | https://github.com/ShuheiKonno/widget-go/blob/main/PRIVACY.md |
| Category | Productivity |
| Maintainer Type | Individual maintainer(s) |
| Build System | GitHub Actions |
| Primary Discovery Channel | AI / LLM tools |

Tagline:

> A lightweight Windows desktop widget for system monitoring and optional productivity timers.

Description:

> widget-go (system-widget) is a Windows desktop system-monitor widget that displays CPU, memory, GPU/NPU, disk and network status in a compact window. It also offers optional work and Pomodoro timers, ping monitoring, and Claude/Codex usage display. This MIT-licensed utility helps users monitor their own computers.

## 利用実績と技術上の確認事項

Reputation欄には、確認できた事実と未確認事項を分けて入力した。

- リポジトリ作成日は2026-04-11、publicへの変更日は2026-11-14。
- リリース記録は27件（プレリリースを含む）。入力時点でstars 0、forks 0。
- 外部の利用者数や報道実績は確認できていないため、普及しているとは記載していない。
- [リリース履歴](https://github.com/ShuheiKonno/widget-go/releases)、[CI](https://github.com/ShuheiKonno/widget-go/actions)、[署名ポリシー](code-signing-policy.md)を提示した。
- ローカルで改変したgo-webview2と、埋め込むMicrosoft署名付きWebView2Loader.dllについて審査側への確認を依頼した。[依存物確認記録](dependency-license-audit.md)と[第三者表示](../THIRD_PARTY_NOTICES.md)を提示した。
- Foundationの証明書による署名を希望し、本番署名前にFoundationの要件へ従うことを記載した。

## ダウンロードページの説明

フォームがダウンロードページ上の署名方針の説明を求めるため、最新リリースv2.10.0の本文へCode signing policy・プライバシー・第三者表示へのリンクを追加した。Foundationの採用・証明書発行はまだであり、そのリリースのEXEにFoundationの署名はないことを明示した。EXEや過去のタグは変更していない。

## 申請後に行うこと

1. 申請画面の受付完了表示は確認済み。画面に受付番号は表示されていない。重複送信しない。
2. 申請先からの確認・追加質問・採用結果を確認する。確認時点の関連メール検索では通知・招待を取得できていない。審査期間・採否を推測しない。
3. 採用・招待後にSignPathアカウントの本人確認とMFA設定を行う。共同作業者のMFAも本番運用前に確認する。
4. 提供されたOrganization ID、Project、Artifact Configuration、Signing Policy、公開用証明書、承認者を設定する。
5. GitHub Environmentへ署名要求用トークンを登録し、署名済みEXEの取得・検証後だけ公開する工程を実装する。

参照: [申請ページ](https://signpath.org/apply)、[Foundationの条件](https://signpath.org/terms)、[申請サービスのプライバシーポリシー](https://signpath.io/privacy-policy)。
