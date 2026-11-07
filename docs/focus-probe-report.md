# 集中モードの実機調査（T0）の報告

`cmd/focusprobe` で、ポモドーロの集中モード（`focus/`）が前提にしていることを、実機の Teams / Outlook で確かめた記録。
**読み取りだけの調査は、この開発機で行った（2026-11-07）。実際に隠す・通知を止める確認（-apply）は、まだ行っていない**（この PC の Teams / Outlook は実際に使われているため、実装の検証で勝手に隠さなかった）。

実行のしかた（`docs/plan-pomodoro.md` の T0）:

```
go run ./cmd/focusprobe                          # 読み取りだけ（ウィンドウ・通知キーの一覧）
go run ./cmd/focusprobe -apply -hold 10s         # 対象を隠して、10 秒後に戻す（ウィンドウだけ）
go run ./cmd/focusprobe -apply -notif -hold 30s  # 通知スイッチも 0 にする。この間に新着を出して、通知が出ないか確かめる
go run ./cmd/focusprobe -restore                 # 途中で止まったときの取りこぼしを戻す
```

## 1. 読み取りの結果（この開発機。2026-11-07）

計画では「この開発機には Outlook / Teams が入っていない」としていたが、**入っていて、起動中だった**（新しい Teams と、新しい Outlook）。
計画の確認方法（`Get-AppxPackage` など）では見つからなかっただけで、実機のプロセスとウィンドウは取れる。

| 項目 | 結果 |
|------|------|
| 新しい Teams | プロセス名 `ms-teams.exe`（2 プロセス）。主なウィンドウのクラスは `TeamsWebView`（タイトルは `Microsoft Teams` と、開いているチャット名）。ほかに、メッセージ用の `TeamsMessageWindow`（**1280×735 で、タイトルなし**。不可視）と、`ContextSwitcherImpl` / `SkyLibWindowsReactor` / `RtcPalVideoPnPMonitorHWND` / `DeviceNotificationObserverWinClass2` / IME（小さい不可視）がある |
| 新しい Outlook | プロセス名 `olk.exe`。主なウィンドウのクラスは `Outlook Host`（1462×865）。ほかに `TrayInstanceWindow` / `SystemHealthMonitor` / `OlkMessageHandler` / `Outlook Staging Area` / `ContextSwitcherImpl`（小さい不可視）と IME がある |
| 可視のウィンドウ | **調査時は、Teams / Outlook の主なウィンドウも含めて、すべて不可視**（トレイに格納されていた状態と思われる）。そのため「隠す対象（可視・所有者なし）」は 0 件だった |
| 従来の Teams / Outlook（`Teams.exe` / `OUTLOOK.EXE`） | 起動しておらず、未確認（`outlook.exe` のウィンドウクラス `rctrl_renwnd32` も未確認） |
| 通知設定の子キー（`HKCU\...\Notifications\Settings`） | `Microsoft.OutlookForWindows_8wekyb3d8bbwe!Microsoft.OutlookforWindows` に `Enabled=0` の値がある（この PC では、新しい Outlook の通知は、すでに OFF）。`Microsoft.Office.OUTLOOK.EXE.15` は `Enabled` の値なし（既定の ON）。**Teams の子キーは無かった**（`MSTeams_...` も `Teams` を含む名前も無い） |

### この結果からわかったこと（実装に反映したもの）

- **「表示を戻す」（記録の無いウィンドウの救済）の候補は、タイトルのあるウィンドウに絞る**。不可視・所有者なし・1280×735 の `TeamsMessageWindow` が実在し、これを表示してしまうと、白い空のウィンドウが出る。大きさだけでは補助ウィンドウを除けないので、タイトルなしを除く（`focus.RestoreUnrecorded`）。
- **元の値が `Enabled=0` のキー**（新しい Outlook）は、止める操作を省き、戻すときも 0 に戻す（元の値のまま）。
- **Teams は、通知設定の子キーが無い**ので、この PC では、**通知の抑止は Teams には効かない**（キーを新しく作ることはしない: 正しい AppUserModelID が確かでなく、Windows の管理するキーを作るのは危険）。Teams が最初の通知を出すと子キーができる可能性があるが、未確認。→ 下の「2」の確認事項。
- 新しい Outlook の AppUserModelID は、`outlookforwindows` を含む（照合の語 `outlookforwindows` で当たる）。

## 1b. ダミーのウィンドウでの、隠す・戻すの実機確認（Win32 層。2026-11-07）

Teams / Outlook には触れず、可視の最上位ウィンドウを 1 つ持つだけのダミー（`probewin.exe`。製品に含まれない）を対象にして、`focusprobe -apply -targets probewin.exe -hold 3s` を実行した。ダミー自身が、100ms ごとに可視・最小化・最大化を記録した。

| ダミーの状態 | 方法 | 結果 |
|--------------|------|------|
| 通常 | 隠す | 不可視になり、3 秒後に可視に戻った |
| 最大化 | 隠す | 不可視になり、戻したあとも最大化のまま（`IsZoomed`） |
| 最小化済み | 隠す | 不可視になり、戻したあとも最小化のまま |
| 通常 | 最小化 | 最小化され（可視のまま）、3 秒後に通常に戻った |

可視の判定・隠す・戻す・元の状態（通常 / 最小化 / 最大化）の記録は、実際のウィンドウで動く。**Teams / Outlook 自身が、隠されたあとに勝手に前面へ出てこないかは、未確認**（下の 1）。

## 2. 実機で確かめること（未実施）

| # | 確認 | 結果 |
|---|------|------|
| 1 | Teams / Outlook を前面に出した状態で `-apply -hold 10s`。隠れるか。相手が勝手に前面へ出てこないか。戻したあとに正常か（最大化していたものは最大化に戻るか） | 未 |
| 2 | 最小化（`-method minimize`）でも同様か | 未 |
| 3 | 新着メール・通話の呼び出しのポップアップが、どのウィンドウで出るか（クラス・所有者・可視）。`-apply` の間に再列挙で隠れるか（最大 1 秒、一瞬見える） | 未 |
| 4 | `-apply -notif -hold 30s` の間に、新着メール・チャットが来たとき、**通知（トースト）が出ないか**。反映の遅れ、Teams / Outlook の再起動が要るか。戻すと出るようになるか | 未（**通知の抑止が実際に効くかは未確認**） |
| 5 | Teams の通知設定の子キーが、通知を一度出したあとにできるか。できる場合、名前に `teams` を含むか（`MSTeams_8wekyb3d8bbwe!MSTeams` か） | 未 |
| 6 | 勤務先の管理された PC で、`HKCU` の通知設定の書き込みが、ポリシーで拒否されないか（拒否されると、その子キーは記録せず、ウィンドウだけ隠す） | 未 |
| 7 | 従来の Outlook（`OUTLOOK.EXE`）・従来の Teams（`Teams.exe`）のウィンドウのクラスと、通知の子キー名 | 未 |
| 8 | 通話の呼び出しの音（鳴る想定。止められない） | 未 |

実機の結果は、この表に書き足す。通知の抑止が効かない場合は、README の「できないこと」に結果を明記し、ウィンドウの非表示だけの機能として扱う
（通知のポップアップのウィンドウを隠す方式は、Windows のシェルのウィンドウを触るので採用しない — 計画のとおり）。
