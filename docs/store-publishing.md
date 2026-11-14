# Microsoft Store 公開・申請記録

2026-11-14、利用者の指示でStore公開を開始。公開名は **System Widget - システムモニター**、価格は無料、日本語掲載。開発者登録・Partner Centerへのログインは本人が完了。

## 製品登録

Partner Centerで新規MSIXアプリの名前を予約し、製品Identityを実画面から確認した。

- Store ID: `9PH5180TT115`
- Name: `ShuheiKonno.SystemWidget-`
- Publisher: `CN=FB535124-1763-4024-8993-A4F3C1304D3C`
- PublisherDisplayName: `Shuhei Konno`
- Package Family Name: `ShuheiKonno.SystemWidget-_e5w12gdjjk7rj`
- 製品情報は[store-identity.json](../packaging/msix/store-identity.json)へ記録。これらはパッケージに含まれる公開識別子であり、認証情報ではない。

2026-11-14 19:50 JST時点で、Submission 1を認定へ送信済み。Partner Centerの製品状態は「認定中」、進行状況は「申請: 完了」「前処理中: 処理中」「認定・公開処理中: 開始されていません」。認定合格後、すぐに公開する設定。**一般公開・認定合格はまだ確認していない。**

## Submission 1 の送信内容

- 公開名: **System Widget - システムモニター**
- 価格: **無料**（JPY基準価格0、将来の価格変更なし）
- 提供範囲: 選択可能な全240市場、公開・検索可能
- 掲載言語: 日本語（日本）`ja-jp`
- パッケージ: `SystemWidget-Store_2.10.0.0_x64.msix`（提出前に`Validated`を確認）
- 対象: Windowsデスクトップ、x64、Windows 10 バージョン2004以降
- 年齢区分: 回答を保存済み。IARC利用規約と成人年齢の確認は本人が操作した。
- 掲載画像: 実アプリのスクリーンショット1枚（1450×1080）、アイコン300×300
- 提出前に、価格・提供、プロパティ、年齢区分、パッケージ、Store登録情報、申請オプションの6項目すべてで「完了」を確認
- 公開後に利用する製品リンク: <https://apps.microsoft.com/detail/9PH5180TT115>（一般公開は未確認）

提出パッケージのSHA-256:

```text
487E08DE0F373D98E7EB1B1368D969C056BD959674192E898756F070CD3750B6
```

梱包元は未コミット変更のない`0eab0fe36aa19bd7033580402f1c369f776e366b`。[PR #65](https://github.com/ShuheiKonno/widget-go/pull/65)のCI（test・lint・vuln・secrets、Windows上のStore MSIX生成・検証を含む）成功後にmainへ統合した。mainのマージコミット`8e9ed6e74a5bbdfd349b88767258f94ecf8ac294`とのソース差分がないことを確認して提出した。

制限付き機能`runFullTrust`の必須説明は、500文字制限内の461文字で登録済み。任意の「追加のテスト情報」は空欄。基本機能の審査に第三者サービスの個人アカウント認証情報を提供していない。初回申請のため、掲載情報のリリースノートも空欄とした。

今後はPartner Centerの認定結果と追加質問を確認し、認定合格・公開完了をそれぞれ確認する。画面の所要時間案内は通常数時間、場合によって最大3営業日であり、公開日の確約ではない。

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
