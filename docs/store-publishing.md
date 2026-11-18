# Microsoft Store 公開・更新手順

**System Widget - システムモニター**は、Microsoft Storeで無料公開中のWindows向けMSIXアプリです。初回公開パッケージは`2.10.0.0`（x64）、製品IDは`9PH5180TT115`です。2026-11-18にPartner CenterのAPIで公開済みの申請を確認しました。[Microsoft Store製品ページ](https://apps.microsoft.com/detail/9PH5180TT115)

## 更新申請

1. [Partner Centerの製品ページ](https://partner.microsoft.com/dashboard/products/9PH5180TT115/overview)で、現在公開中のパッケージのバージョンと申請状況を確認する。既存の下書きや審査中の申請がある場合は、その状態を確認してから次の更新を進める。
2. `winres/winres.json`の5か所のバージョンを公開版より上げ、リポジトリ直下で`go-winres make`を実行する。再生成した`rsrc_windows_386.syso`と`rsrc_windows_amd64.syso`をコミットに含め、`README.md`の変更履歴も更新する。例えばGitタグ`v2.10.1`に対応するMSIXバージョンは`2.10.1.0`で、Store版の4桁目は`0`にする。
3. PRレビューとCIを通して`main`へマージする。Windows上のクリーンなチェックアウトを、確定したリリースコミットに合わせる。GitHub Releaseも作る場合は、同じコミットにバージョンと一致するタグを付ける。
4. Windows上で以下のコマンドを実行し、`dist/msix-store/`に作られた今回の出力先の`package-info.json`を指定して検証する。`sourceCommit`、`version`、`storeId`とMSIXのSHA-256を確認する。提出には無署名MSIXを使い、`-AllowDirty`やテスト版の証明書を使わない。

   ```powershell
   .\packaging\msix\Build-StoreMsix.ps1
   python .\packaging\msix\verify_msix.py --info '.\dist\msix-store\<今回の出力フォルダー>\package-info.json' --store
   ```

5. Partner Centerで既存製品の更新申請を開き、検証済みのMSIXをアップロードする。パッケージの検証結果、価格・提供範囲、日本語の「このバージョンの最新情報」、申請メモ、公開オプションを確認して認定へ提出する。[MicrosoftのMSIX申請手順](https://learn.microsoft.com/en-us/windows/apps/publish/publish-your-app/msix/create-app-submission)
6. 今回の申請IDと対象バージョンが公開済みになったことを確認し、Store経由で更新・起動を確認する。認証済みのPCでは`msstore submission status 9PH5180TT115`でも進捗を参照できる。保留中の申請がないときは前回の公開済み申請が返ることがあるため、`Published`表示だけで今回の公開完了を判断しない。

### GitHub Releaseとの連携

現在の`release.yml`は`v*`タグを起点にGitHub Release用のEXEとSHA-256を公開し、`test.yml`はStore用MSIXをビルド・検証します。Storeの提出ジョブを導入するまでは、上記のPartner Center手順で提出します。Store提出用の無署名MSIXはGitHub Releaseに添付せず、Store配信時の署名は認定後にMicrosoftが行います。

連携を導入するときは、同じタグのコミットからStore用MSIXを生成・検証し、承認が必要なGitHub Environment内の提出ジョブで、CI専用のMicrosoft Entraアプリを使います。最初は読み取りで認証を確認し、提出は下書きで止めてPartner Centerで確認します。CLIは既存の下書きを置き換えることがあるため、保留申請がある場合は提出ジョブを止めます。ローカルPCのCLI認証用秘密鍵やCIのシークレットはリポジトリに保存しません。

## 初回公開の記録

2026-11-14に無料・日本語掲載でStore公開の手続きを開始。開発者登録・Partner Centerへのログインは本人が完了しました。

### 製品登録

Partner Centerで新規MSIXアプリの名前を予約し、製品Identityを実画面から確認した。

- Store ID: `9PH5180TT115`
- Name: `ShuheiKonno.SystemWidget-`
- Publisher: `CN=FB535124-1763-4024-8993-A4F3C1304D3C`
- PublisherDisplayName: `Shuhei Konno`
- Package Family Name: `ShuheiKonno.SystemWidget-_e5w12gdjjk7rj`
- 製品情報は[store-identity.json](../packaging/msix/store-identity.json)へ記録。これらはパッケージに含まれる公開識別子であり、認証情報ではない。

2026-11-14 19:50 JSTにSubmission 1の認定への送信を確認。送信直後は前処理中で、認定合格後すぐに公開する設定でした。2026-11-18に公開済み申請を確認しました。

### Submission 1 の送信内容

- 公開名: **System Widget - システムモニター**
- 価格: **無料**（JPY基準価格0、将来の価格変更なし）
- 提供範囲: 選択可能な全240市場、公開・検索可能
- 掲載言語: 日本語（日本）`ja-jp`
- パッケージ: `SystemWidget-Store_2.10.0.0_x64.msix`（提出前に`Validated`を確認）
- 対象: Windowsデスクトップ、x64、Windows 10 バージョン2004以降
- 年齢区分: 回答を保存済み。IARC利用規約と成人年齢の確認は本人が操作した。
- 掲載画像: 実アプリのスクリーンショット1枚（1450×1080）、アイコン300×300
- 提出前に、価格・提供、プロパティ、年齢区分、パッケージ、Store登録情報、申請オプションの6項目すべてで「完了」を確認
- [Microsoft Store製品ページ](https://apps.microsoft.com/detail/9PH5180TT115)

提出パッケージのSHA-256:

```text
487E08DE0F373D98E7EB1B1368D969C056BD959674192E898756F070CD3750B6
```

梱包元は未コミット変更のない`0eab0fe36aa19bd7033580402f1c369f776e366b`。[PR #65](https://github.com/ShuheiKonno/widget-go/pull/65)のCI（test・lint・vuln・secrets、Windows上のStore MSIX生成・検証を含む）成功後にmainへ統合した。mainのマージコミット`8e9ed6e74a5bbdfd349b88767258f94ecf8ac294`とのソース差分がないことを確認して提出した。

制限付き機能`runFullTrust`の必須説明は、500文字制限内の461文字で登録済み。任意の「追加のテスト情報」は空欄。基本機能の審査に第三者サービスの個人アカウント認証情報を提供していない。初回申請のため、掲載情報のリリースノートも空欄とした。

Partner CenterにSubmission 1を作成済み。プロパティはユーティリティ＆ツール、個人情報を使用する（任意のAI機能・Cookie・アカウント情報等）、プライバシーURLとGitHubのサポートURLを設定して保存した。認証用プロファイルがあるため、Storeのデータ自動バックアップ許可はOFFにした。生成AIによるコンテンツ生成機能やアプリ内課金があるとは申告していない。

## 提出用MSIX

```powershell
.\packaging\msix\Build-StoreMsix.ps1
```

Go、Windows SDK、PowerShellを使用する。Identityと予約名をマニフェストへ設定し、x64のWin32デスクトップアプリを`packagedClassicApp` / `mediumIL`として梱包。`runFullTrust`の必要理由は審査メモへ明記する。

通常は未コミット変更を拒否する。ローカルの事前検証に限り`-AllowDirty`を使用できるが、その出力は提出用にしない。提出にはレビュー・CI後の確定ソースから作り直したパッケージを使う。

出力は`dist/msix-store/<バージョン-日時-識別子>/`。MSIX、SHA-256、元コミット・ハッシュを含むメタデータ、梱包元、掲載用300pxアイコンを作る。梱包物はEXE、マニフェスト、既存アイコン由来のPNG、LICENSE、第三者表示、プライバシーに限定する。

**Store提出用パッケージに自己署名は行わない。** Store審査後の署名をMicrosoftへ任せる。MSIXテスト版の証明書を製品用に使わず、証明書・秘密鍵・ブラウザープロファイルを提出しない。

Store版は`widget-go-store`に設定・ログ・プロファイルを保存し、多重起動の識別子も通常版と分ける。スタートメニューはOSが登録する。従来の`.lnk`を操作せず、無効状態のStartupTaskを宣言し、自動起動の変更はWindows設定で行う。通常版の既存の登録を削除・更新しない。

## 掲載・審査情報

[store-listing.ja-jp.json](../packaging/msix/store-listing.ja-jp.json)に、日本語説明・機能・プライバシー／サポートURL・審査メモの原稿を置く。実装されていない機能や外部サービスの利用権を提供するとは記載しない。第三者サービスへのログインは任意機能であり、基本表示の審査に個人の認証情報を渡さない。

デスクトップの掲載画像には実際のアプリ画面を使用し、1366×768以上のPNG、最低1枚を用意する。メール・認証情報・個別のSSID等が写る場合は撮影前に表示しない状態にする。画面を捏造しない。

掲載用画像は[実画面の撮影記録](../packaging/store-assets/README.md)を参照。Store向けビルドをPC上で起動し、CPU等の実表示と、Store向け起動設定の説明・旧ショートカット操作の非表示を確認した。パッケージとしてのStoreインストールや、StartupTaskを有効にした後のWindowsログイン時の起動確認は別途必要。

価格・提供範囲、プロパティ、年齢区分、パッケージ、Store登録情報、申請オプションを保存し、画面上の未完了項目を解消する。IARC回答はアプリの実際の内容に基づく。認定へ送信した後は、結果・追加質問を確認する。認定されるまでは「公開済み」と案内しない。

## これまでの署名準備との関係

SignPath Foundation申請は2026-11-14に、公開実績・外部認知の不足を理由として今回は承認されなかった。コード品質を否定する判断ではないとの説明があった。StoreへのMSIX提出では、Foundationの証明書を前提にしない。GitHubで直接配るEXEまでStoreの署名が付くわけではない。

MSIXテスト版は[動作確認記録](msix-test.md)のとおり利用者から動作確認の報告を受けた。Store向けの起動設定の扱いは別途実装したため、テスト版で全製品動作・Store認定を保証したとは扱わない。

## 公式資料

- [MSIXアプリの申請](https://learn.microsoft.com/en-us/windows/apps/publish/publish-your-app/msix/create-app-submission)
- [署名の選択肢](https://learn.microsoft.com/en-us/windows/apps/package-and-deploy/code-signing-options)
- [スクリーンショットと画像](https://learn.microsoft.com/en-us/windows/apps/publish/publish-your-app/msix/screenshots-and-images)
- [パッケージの拡張による起動統合](https://learn.microsoft.com/en-us/windows/apps/desktop/modernize/desktop-to-uwp-extensions)
