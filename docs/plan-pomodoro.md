# 計画: ポモドーロタイマー（#50）

- Issue: #50「ポモドーロタイマー」
  > ポモドーロタイマーを実装する。タイマー実行中は集中モードとして Outlook、Teams をデスクトップから非表示にする。着信通知のポップアップも抑止する
- ブランチ: 未作成（案: `ShuheiKonno/ポモドーロ`。ベースは #49 / #51 の修正（`e628a75`）を取り込んだ main）
- ステータス: **計画の策定のみ（2026-11-07）。未着手。T0（実機での調査）の結果で、集中モードの方式を確定する。**

## 要点

タイマー（区画）は、既存の勤務時間タイマーと同じ作りで進められるので、危険は小さい。
難所は**集中モード**（他アプリのウィンドウの非表示と、通知の抑止）で、次の 2 点が不確かである。
そこで、実装の前に **T0（実機調査）** を置き、結果で方式を決める。

1. Windows の「応答不可（集中モード）」を切り替える**公式の API が見つからなかった**（下の「調査結果」）。
2. この開発機には Outlook / Teams が入っていない（確認: `Get-AppxPackage`、レジストリの Uninstall）。
   実際の Teams / Outlook のプロセス名・ウィンドウ・通知の出方は、**勤務先の PC で実測**する必要がある。

## 決定事項（案。ユーザー確認を待つ）

| # | 内容 | 推奨 |
|---|------|------|
| D1 | 区画名は `pomodoro`（アイコン 🍅、見出し「ポモドーロ」）。勤務時間と同じく、既定は非表示・独立した設定タブ | 採用 |
| D2 | 既定は 25 分集中 / 5 分休憩 / 4 回ごとに 15 分の長い休憩。いずれも設定で変更（集中 1〜180、休憩 1〜60、長い休憩 1〜120、間隔 2〜10 回） | 採用 |
| D3 | 集中モード（ウィンドウの非表示・通知の抑止）は**集中の間だけ**。休憩・一時停止・停止・リセットで戻す。設定で ON/OFF（既定 ON） | 採用 |
| D4 | 非表示にする方法の既定は**「隠す」（タスクバーにも出ない）**。「最小化」を選べる設定を用意する | 隠す |
| D5 | 通知の抑止は**アプリごとの通知スイッチ**（レジストリ）を第一候補にする。Windows 全体の応答不可の切替は使わない（公式 API が無く、Windows 11 の版で効かない報告がある） | T0 で確定 |
| D6 | 対象アプリの一覧は設定で編集できる。既定は Teams（新: `ms-teams.exe`、旧: `Teams.exe`）と Outlook（新: `olk.exe`、旧: `OUTLOOK.EXE`） | 採用 |
| D7 | 勤務時間タイマーとは独立（連動しない） | 採用 |

## 範囲外（やらないこと）

- Windows 全体の「応答不可」の切替（公式 API が無い）。
- Teams の在席状況（応答不可）の変更（Microsoft Graph の認証が要る）。
- **着信の呼び出し音の抑止**（ウィンドウを隠しても鳴る。Teams / Outlook 側の設定に任せる）。
- 管理者権限で動いているアプリの操作（UIPI で届かない。対象が昇格していても無視する）。

## 調査結果（2026-11-07）

- 公式 API: Windows の応答不可を切り替える公式の API は、検索では見つからなかった。レジストリ（`NOC_GLOBAL_SETTING_DND`、`FocusAssist\QuietHoursActive` など）での切替の報告はあるが、
  Windows 11 の一部の版で効かない、別の場所（CloudStore）に変わった、との報告がある。→ 使わない。
  - [Intune フォーラム: 応答不可を一括で切る](https://techcommunity.microsoft.com/discussions/microsoft-intune/how-to-centrally-switch-off-focus-assist--do-not-disturb/4336507)
  - [Microsoft Q&A](https://learn.microsoft.com/en-us/answers/a/12428433)
- アプリごとの通知: `HKCU\Software\Microsoft\Windows\CurrentVersion\Notifications\Settings\<AppUserModelID>` の `Enabled`（DWORD）を 0 にすると、そのアプリの通知が止まる、との記述がある（二次情報。2021 年のコミュニティの文書）。
  **Microsoft の公式な文書では確認できていない。** 設定アプリの「通知」のアプリごとのスイッチと同じ実体と推定されるが、**T0 で実機で確かめる**。
  - [Notifications & Actions（コミュニティ）](https://r-pufky.github.io/docs/operating-systems/windows/10/21H2/settings/system/notifications-and-actions.html)
- Outlook の実行ファイル: 新しい Outlook は `olk.exe`、従来の Outlook は `OUTLOOK.EXE`。両方を同時に使える（[Microsoft Learn](https://learn.microsoft.com/en-us/deployoffice/outlook/get-started/deployment-new-outlook)）。
  Teams の `ms-teams.exe` と、従来の Outlook のウィンドウクラス（`rctrl_renwnd32`）は、今回の検索では**裏が取れていない**（推定）。T0 で実測する。
- 従来の Outlook のデスクトップ通知（新着のポップアップ）は、Outlook 自身のウィンドウで出る。Office のポリシーのレジストリ値名は、今回の検索では確認できていない。→ ウィンドウの非表示で扱う。

## 設計

### 1. タイマー（勤務時間タイマーと同じ作り）

- 区画名 `pomodoro`。`config.json` の `hidden.pomodoro` / `collapsed.pomodoro` / `order` で扱う。`normalizeOrder` は、欠けた区画を末尾に足すので、旧 config はそのまま読める。
- 状態は**壁時計**で `config.json` の `pomodoro` に保存する。再起動やスリープをまたいで継続する。
  - 設定: `focusMinutes` / `shortBreakMinutes` / `longBreakMinutes` / `longEvery` / `autoNext`（次の区間を自動で始める。既定 ON）/ `focusMode`（集中モード。既定 ON）/ `hideMethod`（`hide` | `minimize`）/ `targets`（プロセス名の一覧）。
  - 状態: `phase`（`idle` | `focus` | `short` | `long`）、`phaseEndsAt`（RFC3339、動作中）、`pausedRemainingSec`（一時停止中）、`round`（いま何回目の集中か）、`doneToday` と `doneDate`（今日の完了数。日付が変わったら 0）。
- 計算は Go の純関数 `pomodoroSnapshot(cfg, now)`（`now` を引数で受ける）。表示用の値（残り秒、区間、完了数）を返す。
- **勤務時間タイマーと違い、区間の切り替えで副作用が起きる**（集中モードの適用と解除、通知）。表示を読んだときに切り替えると、ウィジェットがトレイに入っている間は動かない。
  そこで、**Go 側に 1 秒周期の goroutine（`pomodoroLoop`）**を置き、期限が来たら次の区間へ進める（`mutateConfig`）。`/api/system` の応答は読み取りだけにする。
  - スリープから復帰して、期限を大きく過ぎていたときは、**1 回だけ進める**（過ぎた分の区間を連続して消化しない）。進めたあとは、新しい区間の長さで、現在時刻から数え直す。
- API: `POST /api/pomodoro?action=start|pause|resume|skip|reset`。応答は最新のスナップショット。不正な状態の操作は 409（勤務時間と同じ `errWorkState` の方式）。
- 設定キー（`/api/setoption`）: `pomoFocus` / `pomoShort` / `pomoLong` / `pomoEvery` / `pomoAuto` / `pomoFocusMode` / `pomoHide` / `pomoTargets`。検証は `setPomodoroOption` に集める。
- `SystemData.Pomodoro`（非表示のときは null）。
- フロントは、2 秒周期の `/api/system` に加えて、**1 秒ごとに残り時間を手元で数える**（応答の `endsAt` から計算。周期の応答で補正）。2 秒刻みだと数字が飛んで見えるため。
- 区間の終わりの通知: トレイのバルーン（`showBalloon`。AI 使用量と同じ経路）で「休憩にしましょう」「集中を始めましょう」を出す。音は出さない。

### 2. 集中モード

新しいパッケージ `focus/`（`ime/` と同じ構成。Windows 専用のファイルと、テストできる純粋な部分を分ける）。

**ウィンドウの非表示**
- `EnumWindows` で最上位ウィンドウを列挙し、`GetWindowThreadProcessId` → `OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION)` → `QueryFullProcessImageNameW` で実行ファイル名を得て、`targets` と照合する。
- 隠す: `ShowWindowAsync(hwnd, SW_HIDE)`（`ShowWindow` は、相手が応答しないと、こちらも固まるので、非同期版を使う）。最小化の設定のときは `SW_SHOWMINNOACTIVE`。
- 隠したウィンドウのハンドルを、**メモリと `config.json` の両方に記録**する（`pomodoro.hidden`）。戻すときは、記録したものだけを、元の状態（表示 / 最小化 / 最大化）に戻す。
- 集中の間、**新しく現れたウィンドウ**（Outlook の新規メール、Teams の通話の呼び出しなど）も隠すため、**1 秒周期で再列挙**する（`pomodoroLoop` と同じ周期）。
  - 利用者が、集中中に自分で Teams を開き直した場合は、**次の周期でまた隠される**。これは困るので、「一時的に表示する」操作（区画のボタン、トレイのメニュー）を用意し、その区間の間は隠さない。→ 確認事項 Q3。
- ウィンドウクラスや可視判定で、**通知のポップアップ（トースト）や、システムのウィンドウを隠さない**ように絞る。対象は「プロセスが `targets` に一致し、可視で、所有者のいない最上位ウィンドウ」。

**通知の抑止**（T0 で方式を確定。下は第一候補）
- `HKCU\...\Notifications\Settings\<AUMID>` の `Enabled` を 0 にし、**元の値を `config.json` に保存**してから、戻すときに復元する（元のキーが無かった場合は、値を削除する）。
- `<AUMID>` は、`Notifications\Settings` の子キーの名前から、`targets` に対応するもの（名前に `Teams` / `Outlook` / `olk` を含むもの）を探す。固定値にしない。
- 通知が止まらない場合の代替（T0 の結果次第）: 通知のポップアップのウィンドウを隠す方式は、**Windows のシェルのウィンドウを触るので採用しない**。その場合は「ウィンドウの非表示のみ」にして、README に限界を明記する。

**復元を取りこぼさない（最重要）**
隠したままになる事故が、この機能の最大の危険（Teams / Outlook が見えなくなる）。次の経路をすべて復元に結ぶ。

| 契機 | 復元 |
|------|------|
| 集中の終了・休憩・一時停止・停止・リセット | 区間の切り替えで復元 |
| 区画を非表示・集中モードを OFF | 設定の変更で復元 |
| トレイの「終了」 | `tray.go` の `ID_TRAY_EXIT`（`cleanupTray()` → `os.Exit(0)`）の前に復元 |
| Windows のログオフ・シャットダウン | `WM_ENDSESSION` で復元（現状は未処理なので追加） |
| アプリの異常終了・強制終了 | **次の起動時**に、`pomodoro.hidden` と保存した通知の元の値から復元（集中中でなければ戻す。集中中なら再適用） |
| 取りこぼし（相手が再起動してハンドルが変わった等） | トレイのメニューに「Teams / Outlook の表示を戻す」（記録が無くても、`targets` の可視でないウィンドウを列挙して戻す）を置く |

### 3. UI

- `ui/index.html`: 区画 `pomodoro` を追加し、`hideableSections` / `collapsibleSections` / `settings.order` に登録する。`work` の追加（勤務時間の T3）と同じ手順。
  - 表示: 残り時間（大）、区間名（集中 / 休憩 / 長い休憩）、`● ● ○ ○`（4 回のうちの何回目か）、今日の完了数、バー（区間の経過）。
  - 操作: 開始 / 一時停止 / 再開 / スキップ / リセット（ボタン。`data-pm` 属性で、区画のドラッグ対象から外す）。
  - 集中中は、見出し横に「集中モード」の印を出す。
  - コンパクト（幅 130px）: 見出し 1 行（`🍅 24:31`）。**クリックで 開始 / 一時停止**（入力欄が無いので、勤務時間のような別ウィンドウは不要）。
  - 色: 集中は既定色、休憩は緑、一時停止は灰色（勤務時間の `.paused` と同じ扱い）。スキン用の変数は `--pomo-*` として `docs/skin-spec.md` に追加する。
- `ui/settings.html`: 区画の一覧に「ポモドーロ」を追加（チェックは `hide.pomodoro`）。独立した「ポモドーロ」タブに、時間・回数・自動で次へ・集中モード・隠す方法・対象アプリを置く。
  対象アプリの欄は、「検出した起動中のアプリ」の候補を出せると親切（`focus` の列挙を再利用。T6 で判断）。
- トレイ: ツールチップに残り時間を出す（任意。`trayusage.go` の流れを確認してから）。メニューに「ポモドーロ 開始 / 停止」「Teams / Outlook の表示を戻す」。

## 影響範囲（既存コードの変更点）

- `config.go`: `HiddenConfig` / `CollapsedConfig` に `Pomodoro` を追加。`defaultOrder` に追加。`Config.Pomodoro`。
- **区画の数・並びを前提にしたテスト**を直す（`grep` で確認済み）。`collapsed_test.go`（全区画の一覧 42・102 行目、`len(saved.Collapsed) != 8` の 64 行目、`CollapsedConfig{…}` の一括比較 47 行目）と、
  `worktimer_test.go` 399 行目（旧 config の読み込みで `work` が**末尾**であること。`pomodoro` を `defaultOrder` の末尾に足すと外れる。`pomodoro` を `work` の後ろに置いて、末尾の期待を `pomodoro` に直す）。
- `server.go`: `/api/pomodoro`、`setoption` のキー、`/api/settings`。
- `main.go`: `SystemData.Pomodoro`、`pomodoroLoop` の起動、`WM_ENDSESSION`。`tray.go`: 終了時の復元、メニュー。
- `ui/index.html` / `ui/settings.html`、`docs/skin-spec.md`、組み込みスキン 4 種（新しい区画の見た目の確認）、README。

## 作業ステップ

- [ ] **T0 実機調査（勤務先の PC）**: `cmd/focusprobe`（`cmd/diskprobe` と同じ位置づけの、探索用の小さなツール）を作り、次を確かめて `docs/focus-probe-report.md` に残す。
  1. Teams（新 / 旧）・Outlook（新 / 旧）の、プロセス名・ウィンドウのクラスとタイトル・可視・所有者。通話の呼び出しと新着メールのポップアップが、どのウィンドウで出るか。
  2. `SW_HIDE` / 最小化 → 戻す、の挙動（相手が勝手に前面へ出てこないか、戻したあとに正常か）。
  3. `Notifications\Settings` に、Teams / Outlook の子キーがあるか。`Enabled` を 0 にして通知が止まるか（反映の遅れ、再起動の要否）。戻すとどうなるか。
  4. 管理対象の PC で、レジストリの書き込みが、ポリシーで拒否されないか。
- [ ] T1 `config.go` / `pomodoro.go`: 設定の型・既定値・検証、区間の計算の純関数、`pomodoro_test.go`（境界: 期限ちょうど、一時停止、スリープ復帰、4 回目の長い休憩、日付またぎの完了数）。
- [ ] T2 `focus/`: 列挙と照合（純関数にして、偽のウィンドウ一覧でテスト）、隠す・戻す（`ShowWindowAsync`）、記録と復元、通知スイッチ（レジストリを差し替え可能にしてテスト）。T0 の結果で方式を確定。
- [ ] T3 `pomodoroLoop`、`server.go` / `main.go` / `tray.go`: API、設定キー、`SystemData`、終了・`WM_ENDSESSION`・起動時の復元、ハンドラのテスト。
- [ ] T4 `ui/index.html`: 区画、操作、色、コンパクト、1 秒の手元カウント、高さ調整。
- [ ] T5 `ui/settings.html`: 区画の一覧とタブ。
- [ ] T6 スキン 4 種の確認、`docs/skin-spec.md`、README、トレイのメニュー・ツールチップ。
- [ ] T7 `go vet` / `go test ./...` / `go test -race`（gcc が要る）、**実機確認（下記）**。

## 検証

**自動（テストで担保する）**
- 区間の計算（純関数）、状態遷移の API（不正な状態は 409）、設定の検証・矯正、旧 config の読み込み。
- 集中モード: 偽のウィンドウ一覧・偽のレジストリで、「隠したものだけを戻す」「元の値の復元（キーが無かった場合は削除）」「記録からの起動時の復元」「対象外のウィンドウは触らない」。

**実機（勤務先の PC。自動化できない）**
1. 集中を開始 → Teams / Outlook が隠れ、**新着メール・チャットの通知が出ない**。休憩で戻り、通知が出るようになる。
2. 集中中に**アプリを強制終了** → 再起動で、隠れたものが戻る（または集中が再開して隠れる）。
3. 集中中に **Teams を再起動**（ハンドルが変わる）→ 新しいウィンドウも隠れ、戻すと正常に戻る。
4. 集中中に **PC をスリープ → 復帰**（期限を過ぎる）→ 1 回だけ進み、窓が正しい状態になる。
5. トレイの「終了」・ログオフ・シャットダウンのあとに、Teams / Outlook が見える状態で残っている。
6. 通話の呼び出しがあったとき（相手に架けてもらう）の挙動を記録する（呼び出し音は鳴る想定）。

## リスク

| リスク | 対策 |
|--------|------|
| ウィンドウを隠したまま戻せなくなる | 上の「復元を取りこぼさない」の全経路。記録を `config.json` に持つ。手動の復元メニュー |
| 通知のレジストリが、版・ポリシーで効かない | T0 で確かめ、効かなければ「ウィンドウの非表示のみ」にして、限界を README に書く |
| 他アプリの再起動・更新でハンドルが変わる | 1 秒周期の再列挙。戻すときは、記録のハンドルが有効か（`IsWindow` とプロセスの照合）を確認する |
| 相手が管理者権限・別セッション | 触れない（無視する）。エラーにしない |
| 隠している間の、利用者の混乱（Teams が見つからない） | 集中中の表示（印）、「一時的に表示する」、トレイのメニュー |
| 区画数の増加で、既存のテストが壊れる | 影響範囲に列挙。T1 で最初に直す |
| スキンが新しい区画に追従しない | T6 で 4 種を確認 |

## 確認事項（ユーザーへ）

- Q1 勤務先の PC の Outlook / Teams は、**新旧のどれ**か（T0 の対象を絞れる。両方を既定の対象に入れるので、分からなくても進められる）。
- Q2 非表示は「隠す（タスクバーにも出ない）」と「最小化」のどちらを既定にするか。推奨は「隠す」。
- Q3 集中中に、利用者が Teams を開いたとき、**隠し続ける**か、**開いたものは許す**か。推奨は「一時的に表示する」の操作を設け、その区間は隠さない。
- Q4 区間の終わりの**音**は要るか（既定は無し）。
- Q5 ほかに隠したいアプリ（例: Slack）があるか（対象アプリの一覧で足せる設計にしてある）。
