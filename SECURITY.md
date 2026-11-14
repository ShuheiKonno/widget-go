# セキュリティに関する報告

## 対象

原則として最新の正式リリースとmainの問題を確認します。修正時期や過去のすべてのバージョンへの修正を保証するものではありません。

## 非公開で報告する方法

脆弱性、認証情報の露出、正規配布物の改ざん、不正な署名の疑いは、[GitHubの非公開脆弱性報告](https://github.com/ShuheiKonno/widget-go/security/advisories/new)から報告してください。リポジトリのSecurityタブからも進めます。

再現条件、対象バージョン、影響、必要最小限の証拠を添えてください。公開Issueへ未修正の攻撃手順、Cookie、アクセストークン、署名用トークン、認証用ブラウザープロファイルを掲載しないでください。通常の不具合・改善提案は公開Issueを利用できます。

## 正規の配布物

正規配布先は[GitHub Releases](https://github.com/ShuheiKonno/widget-go/releases)です。[Code signing policy](docs/code-signing-policy.md)で署名の導入状況を確認してください。SHA-256ファイルだけでは発行元の保証にはなりません。

現時点ではSignPath Foundationの申請準備中であり、コード署名の提供開始を示すものではありません。GitHub Environmentの公開承認と、将来のSignPath署名承認は別の手順です。

## 開発・リリースでの取扱い

- 秘密情報はソース、Git履歴、Issue、ログ、テスト用データへ入れず、許可した用途のSecrets等へ保存します。
- `.gitignore`は履歴の削除や漏えい済みトークンの無効化を行いません。漏えいが疑われたら、まず該当情報を失効・再発行します。
- 外部PRのコードには本番署名・公開の権限を渡しません。
- mainのCI、リリースタグの保護、公開Environmentの承認を通し、署名導入後は署名検証も通した成果物だけを公開します。

保存データと削除方法については[PRIVACY.md](PRIVACY.md)も参照してください。
