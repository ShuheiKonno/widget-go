# MSIX 動作確認用パッケージ

これは **ローカル動作確認用**。Microsoft Store の製品パッケージ、Foundation の署名、一般配布用の証明書ではない。Store の製品 Identity / Publisher は未取得であり、公開申請は行わない。

## 作成

必要なもの: Windows x64、go.mod に指定した Go、Windows SDK の MakeAppx.exe / SignTool.exe、PowerShell 5.1 以降。

リポジトリのルートで実行する。

```powershell
.\packaging\msix\Build-TestMsix.ps1
```

`dist\msix-test\<バージョン-日時-識別子>\` に次を作る。既存の出力は削除・上書きしない。

- `SystemWidget-Test_<バージョン>_x64.msix`: 開発用署名付きパッケージ
- `widget-go-msix-test.cer`: **公開証明書のみ**。秘密鍵を含まない
- `.msix.sha256` / `package-info.json`: ハッシュ、証明書の拇印・期限、元コミットと未コミット変更の有無
- `Install-TestMsix.ps1`: 信頼登録・インストールを明示的に行う補助スクリプト
- `SystemWidget-Test_<バージョン>_x64-test-kit.zip`: 上記のインストールに必要なファイルと手順をまとめたZIP。別のテストPCで使う場合は展開して実行する
- `package\`: パッケージ化の元データ。EXE・マニフェスト・既存アイコンから生成した PNG・LICENSE・第三者表示・プライバシーのみ

ビルドスクリプトは、現在の利用者の `Cert:\CurrentUser\My` に有効期間30日の開発用証明書を作る。秘密鍵はエクスポート不可で保持し、PFX やパスワードファイルは作らない。同じ用途で有効な証明書があれば再利用する。**証明書の信頼登録、アプリのインストール、Developer Mode / sideloading 設定の変更は行わない。**

Windows SDK の検証を有効にした MakeAppx でパッケージを作り、SignTool で署名する。`-Version 2.10.0.1` などで4桁のテスト用バージョンを指定できる。既存インストールを更新するときはバージョンを上げる。指定しなければ winres のバージョンを使う。

## 通常版との分離

| 項目 | 通常版 | MSIX テスト版 |
| --- | --- | --- |
| アプリ名 / メインウィンドウ | システムモニター | SystemWidget-MSIX-Test（メニュー上は System Widget (MSIX Test)） |
| 設定・スキン・WebView2・診断ログ | `%LOCALAPPDATA%\widget-go` | `%LOCALAPPDATA%\widget-go-msix-test\data` |
| 多重起動判定 | `Global\widget-go-single-instance-mutex` | `Local\widget-go-msix-test-single-instance-mutex` |
| アプリが作る .lnk | `system-widget.lnk` | `system-widget-msix-test.lnk` |
| パッケージ Identity | なし | `ShuheiKonno.SystemWidget.Test` |

テスト用ビルドだけ Go の `-ldflags -X` で切り替える。通常版の既定値は変えない。設定をコピーせず、新規設定から起動する。旧 ClaudeMonitor の認証移行元も別の親フォルダになり、通常の認証情報を取り込まない。

MSIX のファイル仮想化により、実際のテストデータが `%LOCALAPPDATA%\Packages\<PackageFamilyName>\LocalCache\Local\widget-go-msix-test\data` に置かれる場合がある。仮想化の有無にかかわらず通常版の `widget-go` フォルダとは分かれる。

## テストPCへインストール

1. 作成した出力フォルダを開き、`package-info.json` の証明書拇印を確認する。
2. 必要なら管理者権限なしで、次のプレビューを実行する（変更しない）。

   ```powershell
   .\Install-TestMsix.ps1 -TrustTestCertificate -WhatIf
   ```

3. **テストする本人のWindowsアカウント**で管理者 PowerShell を開き、出力フォルダへ移動して実行する。

   ```powershell
   .\Install-TestMsix.ps1 -TrustTestCertificate
   ```

   この明示指定で、開発用の公開証明書を `LocalMachine\TrustedPeople` に信頼登録する。Root ストアには入れない。登録後、現在の利用者へ `Add-AppxPackage` でインストールする。他人の管理者アカウントで実行すると、その人のアカウントへのインストールになるため注意する。

   すでに証明書を信頼登録していれば、通常の PowerShell から `Install-TestMsix.ps1` のみでインストールできる。OS の組織ポリシーで拒否された場合は設定を自動変更せず、エラー内容を確認する。

4. スタートメニューの **System Widget (MSIX Test)** から起動する。スクリプト出力にある `shell:AppsFolder\<PackageFamilyName>!Widget` でも起動できる。

自己署名はこの証明書を信頼したPCだけで有効。インストーラーの発行元・確認画面が出ることはある。Microsoft Store 由来の署名や SmartScreen の一般配布向けの信頼を取得したことにはならない。

## 確認項目

2026-11-14 の初回作成時は、Windows SDK によるマニフェスト・パッケージ検証、自己署名の暗号検証、全148ブロックの SHA-256、梱包対象の許可リスト、アイコン寸法を確認した。`go test ./...` と `go vet ./...` は成功。インストールスクリプトの `-WhatIf` も確認した。証明書の信頼登録・MSIX のインストール・インストール後の実起動は未実施。

その後、利用者から「動作確認できました」と報告を受けた。これは利用者によるMSIXテスト版の確認結果であり、個々の任意機能・更新・アンインストールすべての検査記録や、Store認定の完了を意味しない。

以下は **パッケージを作れたことだけでは確認済みにならない**。実際のパッケージから起動して確認する。

- 起動、WebView2 の画面表示、CPU・メモリ・Disk・GPU/NPU・ネットワークの更新
- 設定ウィンドウ、テーマ・組み込みスキン・利用者スキン、保存して終了し再起動したときの保持
- トレイ格納と復帰、コンパクト表示、同じテスト版を2回起動したときの重複防止
- 通常版と同時起動でき、通常版の設定・ショートカット・認証情報が変わらないこと
- 勤務時間・ポモドーロ・Ping、AI のログインと表示（利用する機能だけ本人が確認する）
- IME 監視、任意の電源モード切替、集中モード。Windows や他アプリへ作用する機能なので、利用者の操作で確認する
- 更新インストール、アンインストール

### Store 製品化前の注意

このパッケージは Win32 を `packagedClassicApp` / `mediumIL` とし、`runFullTrust` を宣言する。AppContainer へ移植したものではない。WebView2 Runtime は従来と同じ前提で、テストパッケージにインストーラーを含めていない。

MSIX はスタートメニューへ自動登録される。既存アプリ内の「スタートメニュー」「スタートアップ」はテスト専用名だが、実行ファイルの物理パスへの .lnk を作る従来の方式であり、パッケージ更新後の自動起動までは保証しない。製品化時は重複する手動スタートメニュー項目を整理し、パッケージ用 StartupTask 等を検討する。

Store の Identity / Publisher への置換、Store 審査、制限された権限の説明、WebView2 の配布条件、実機での機能互換性の確認が別途必要。このテスト用証明書やテスト版 EXE を製品として公開しない。

## 削除

テスト版をトレイから終了し、通常の PowerShell で **テスト用 Identity だけ**を削除する。

```powershell
Get-AppxPackage -Name ShuheiKonno.SystemWidget.Test | Remove-AppxPackage
```

アプリ内でテスト用 .lnk を作った場合は、先にその設定を OFF にして削除する。通常版のリンクには触れない。テストデータがパッケージ外の `widget-go-msix-test` に残る場合は、内容を確認してその専用フォルダだけを削除する。

もうテスト署名を利用しないときは、出力フォルダで `package-info.json` の拇印を確認し、管理者 PowerShell で信頼登録だけを削除する。

```powershell
$testInfo = Get-Content .\package-info.json -Raw -Encoding UTF8 | ConvertFrom-Json
Remove-Item -LiteralPath "Cert:\LocalMachine\TrustedPeople\$($testInfo.certificateThumbprint)"
```

署名用の秘密鍵も不要になったときは、同じ本人の PowerShell で `Cert:\CurrentUser\My\<同じ拇印>` の証明書を削除する。別の証明書は削除しない。

## 参照

- [Generating MSIX package components](https://learn.microsoft.com/en-us/windows/msix/desktop/desktop-to-uwp-manual-conversion)
- [Create a certificate for package signing](https://learn.microsoft.com/en-us/windows/msix/package/create-certificate-package-signing)
- [Understanding how packaged desktop apps run on Windows](https://learn.microsoft.com/en-us/windows/msix/desktop/desktop-to-uwp-behind-the-scenes)
- [Code signing options](https://learn.microsoft.com/en-us/windows/apps/package-and-deploy/code-signing-options)
