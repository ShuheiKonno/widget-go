//go:build windows

// focusprobe は、ポモドーロの集中モード (focus パッケージ) を、実機の Teams / Outlook で調べるための探索用の小さなツール
// (docs/plan-pomodoro.md の T0)。cmd/diskprobe と同じ位置づけで、製品には含まれない。
//
// 既定は読み取りだけ: 対象のプロセスのウィンドウ (クラス・タイトル・可視・所有者・大きさ) と、
// 通知設定 (HKCU\...\Notifications\Settings) の子キーと Enabled の値を表示する。何も変えない。
//
//	go run ./cmd/focusprobe                      # 読み取りだけ
//	go run ./cmd/focusprobe -targets ms-teams.exe,olk.exe,outlook.exe,teams.exe
//	go run ./cmd/focusprobe -apply -hold 10s     # 対象を隠し、10 秒後に戻す (ウィンドウだけ)
//	go run ./cmd/focusprobe -apply -method minimize -hold 10s
//	go run ./cmd/focusprobe -apply -notif -hold 30s   # 通知スイッチも 0 にして、戻す (その間に通知が出るか確かめる)
//	go run ./cmd/focusprobe -restore             # 取りこぼしを戻す (前回の記録と、記録の無い隠れた対象)
//
// -apply / -notif は、実際に Teams / Outlook のウィンドウを隠し、通知設定を書き換える。変える前に、記録を
// %TEMP%\focusprobe-state.json に保存するので、途中で止まっても -restore で戻せる。
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"widget-go/focus"
)

var defaultTargets = "ms-teams.exe,teams.exe,olk.exe,outlook.exe"

func statePath() string { return filepath.Join(os.TempDir(), "focusprobe-state.json") }

func b2s(b bool) string {
	if b {
		return "Y"
	}
	return "-"
}

func printWindows(wins []focus.Window, targets []string) {
	fmt.Println("== 対象のプロセスの最上位ウィンドウ (不可視・所有者ありも含む) ==")
	fmt.Println("HWND        PID    exe                 vis min max own tool     size  class / title")
	n := 0
	for _, w := range wins {
		if !focus.MatchExe(w.Exe, targets) {
			continue
		}
		n++
		fmt.Printf("%#-10x %6d  %-19s  %s   %s   %s   %s   %s  %5dx%-5d %q / %q\n", w.HWND, w.PID, w.Exe,
			b2s(w.Visible), b2s(w.Minimized), b2s(w.Maximized), b2s(w.HasOwner), b2s(w.ToolWindow), w.Width, w.Height, w.Class, w.Title)
	}
	if n == 0 {
		fmt.Println("(対象のウィンドウが見つかりません。対象のアプリが起動しているか、-targets を確認してください)")
	}
	fmt.Println()
	fmt.Println("== 集中モードで隠す対象 (可視・所有者なし・自分のプロセスを除く) ==")
	sel := focus.Select(wins, targets, uint32(os.Getpid()))
	for _, w := range sel {
		fmt.Printf("%#x  %s  %q / %q\n", w.HWND, w.Exe, w.Class, w.Title)
	}
	if len(sel) == 0 {
		fmt.Println("(なし)")
	}
	fmt.Println()
}

func printNotifications(reg focus.Registry, targets []string) {
	fmt.Println("== 通知設定 (HKCU\\Software\\Microsoft\\Windows\\CurrentVersion\\Notifications\\Settings) の対象の子キー ==")
	hints := focus.NotifHints(targets)
	fmt.Printf("(名前に含まれる語: %s)\n", strings.Join(hints, ", "))
	n := 0
	for _, sub := range reg.Subkeys() {
		l := strings.ToLower(sub)
		for _, h := range hints {
			if strings.Contains(l, h) {
				v, ok := reg.Enabled(sub)
				if ok {
					fmt.Printf("%-80s Enabled=%d\n", sub, v)
				} else {
					fmt.Printf("%-80s Enabled=(値なし)\n", sub)
				}
				n++
				break
			}
		}
	}
	if n == 0 {
		fmt.Println("(該当する子キーがありません。そのアプリがまだ通知を出していないか、名前の付き方が違います。全件は reg query で確認してください)")
	}
	fmt.Println()
}

func main() {
	targetsFlag := flag.String("targets", defaultTargets, "対象のプロセス名 (カンマ区切り)")
	apply := flag.Bool("apply", false, "実際に対象のウィンドウを隠す (-hold のあと戻す)")
	notif := flag.Bool("notif", false, "通知スイッチも 0 にする (-apply と併用)")
	method := flag.String("method", focus.MethodHide, "隠し方: hide | minimize")
	hold := flag.Duration("hold", 10*time.Second, "隠している時間")
	restore := flag.Bool("restore", false, "取りこぼしを戻す")
	flag.Parse()

	targets := strings.Split(*targetsFlag, ",")
	mgr := focus.NewSystemManager()
	win := focus.SystemWindows{}
	reg := focus.SystemRegistry{}

	if *restore {
		var st focus.State
		if b, err := os.ReadFile(statePath()); err == nil {
			_ = json.Unmarshal(b, &st)
		}
		left := mgr.Restore(st)
		n := mgr.RestoreUnrecorded(targets)
		fmt.Printf("記録から戻しました (windows=%d notif=%d)。記録の無い隠れたウィンドウを %d 個、表示に戻しました。戻せなかった通知: %d\n", len(st.Windows), len(st.Notif), n, len(left.Notif))
		if left.Empty() {
			_ = os.Remove(statePath())
		}
		return
	}

	printWindows(win.EnumerateDetailed(true), targets)
	printNotifications(reg, targets)

	if !*apply {
		fmt.Println("(読み取りだけです。-apply で、対象を実際に隠して戻す動作を確かめられます)")
		return
	}
	mgr.Save = func(s focus.State) {
		b, _ := json.MarshalIndent(s, "", "  ")
		_ = os.WriteFile(statePath(), b, 0o600)
	}
	opt := focus.Options{Targets: targets, Method: *method, Notifications: *notif}
	fmt.Printf("== 隠します (method=%s notif=%v)。%v 後に戻します。途中で止まったら -restore ==\n", *method, *notif, *hold)
	st := mgr.Apply(focus.State{}, opt)
	fmt.Printf("隠した: %d 個、止めた通知: %d 個\n", len(st.Windows), len(st.Notif))
	fmt.Println("→ この間に、(1) 対象が隠れたまま、勝手に前面へ出てこないか (2) -notif のとき、新着メール・チャットの通知が出ないか を確かめてください")
	// 集中中と同じく、毎秒再列挙して、新しく現れたウィンドウも隠す
	deadline := time.Now().Add(*hold)
	for time.Now().Before(deadline) {
		time.Sleep(time.Second)
		st = mgr.Apply(st, opt)
	}
	left := mgr.Restore(st)
	fmt.Printf("戻しました (戻せなかった通知: %d)\n", len(left.Notif))
	if left.Empty() {
		_ = os.Remove(statePath())
	}
	printWindows(win.EnumerateDetailed(true), targets)
	printNotifications(reg, targets)
}
