package aiusage

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/jchv/go-webview2"
)

// provider は補助 WebView 1 つぶんの違い（URL・スクリプト・Bind）をまとめたもの。
// ClaudeMonitor では Claude 用と Codex 用でほぼ同じコードが 2 ファイルに重複していた。
type provider struct {
	id         string // "claude" | "codex"
	title      string // 補助ウィンドウのタイトル（診断用）
	dataSubdir string // DataRoot 配下の認証データフォルダ名
	usageURL   string // 使用量を取得するページ（取得時はここに居る）
	loginURL   string // ログイン画面
	script     string // ページ内で動く取得スクリプト（ドキュメント生成毎に実行される）
	fetchJS    string // 取得を即時に走らせる式
	intervalJS string // バックアップタイマーの間隔(ms)を差し替える式（%d）
	loginPos   [2]int32
	enabled    func(Settings) bool
	// reportError は認証状態を更新する（updateUsageError / updateCodexUsageError）。
	reportError func(state, msg string)
	// authState は現在の認証状態（"init" | "ok" | "needs_login" | "network_error"）を返す。
	// 起動直後に初回取得が来ないまま止まっていないか（init のまま）の確認に使う。
	authState func() string
	// reset は無効化時にスナップショットを初期状態へ戻す。
	reset func()
	bind  func(aw webview2.WebView)
}

// authView は 1 プロバイダーぶんの補助 WebView と、その寿命管理。
// WebView2 は設定で ON にしたときに生成し、OFF にしたら破棄して解放する（D2）。
type authView struct {
	p *provider

	mu      sync.RWMutex
	inst    webview2.WebView
	hwnd    uintptr
	visible atomic.Bool // ログイン画面として画面内に出しているか

	refreshMu sync.Mutex
	// lastReload は、取得タイムアウトで補助ページを再読み込みした最後の時刻（refreshMu で保護）。
	lastReload time.Time
	notifyMu   sync.Mutex
	notify     chan struct{}

	loggingOut atomic.Bool
}

var allViews []*authView

func newAuthView(p *provider) *authView {
	v := &authView{p: p}
	allViews = append(allViews, v)
	return v
}

// authWin は補助ウィンドウ 1 枚ぶんの、サブクラス化に必要な状態。
type authWin struct {
	v          *authView
	orig       uintptr
	destroying atomic.Bool
}

// newWebView は WebView2 の生成。テストで本物を作らないよう、差し替えられる変数にしている。
var newWebView = webview2.NewWithOptions

var (
	authWins            sync.Map // hwnd(uintptr) -> *authWin
	authWndProcCallback = syscall.NewCallback(authWndProc)
)

// authWndProc は補助ウィンドウのサブクラス化プロシージャ（全ウィンドウで共通の 1 つ）。
//
//   - WM_CLOSE: 利用者が × を押したとき。アプリを終了させず、画面外へ退避する。
//     破棄中（destroy が Destroy を呼んだ後）は本来の処理（DestroyWindow）へ進める。
//   - WM_DESTROY: go-webview2 はこれを受けると PostQuitMessage を呼び、メインを含む
//     アプリ全体のメッセージループを終了させる（webview.go の wndproc）。握りつぶす。
func authWndProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	val, ok := authWins.Load(hwnd)
	if !ok {
		return 0
	}
	w := val.(*authWin)
	switch msg {
	case wmClose:
		if w.destroying.Load() {
			break
		}
		if w.v.visible.CompareAndSwap(true, false) {
			moveOffscreen(hwnd)
			if host != nil {
				host.OnAuthWindowHidden()
			}
			// 閉じる直前にログインが済んでいた場合に備えて、すぐ取得し直す（UI スレッドで待たない）。
			go w.v.refresh()
		}
		return 0
	case wmDestroy:
		return 0
	case wmNCDestroy:
		authWins.Delete(hwnd)
	}
	r, _, _ := procCallWindowProc.Call(w.orig, hwnd, msg, wParam, lParam)
	return r
}

func (v *authView) state() (webview2.WebView, uintptr) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.inst, v.hwnd
}

func (v *authView) instance() webview2.WebView {
	inst, _ := v.state()
	return inst
}

func (v *authView) dataDir() string {
	root := ""
	if host != nil {
		root = host.DataRoot()
	}
	return filepath.Join(root, v.p.dataSubdir)
}

// sync は設定に合わせて生成・破棄する。UI スレッドで呼ぶこと。
func (v *authView) sync(cfg Settings) {
	want := cfg.Enabled && v.p.enabled(cfg)
	has := v.instance() != nil
	switch {
	case want && !has:
		v.create()
	case !want && has:
		v.destroy()
	}
}

// create は補助 WebView を生成してオフスクリーンに置く。UI スレッドで呼ぶこと。
// 実行中（w.Run 開始後）の Dispatch の内側からでも生成できる（T0-1。約 250ms）。
func (v *authView) create() {
	if v.instance() != nil {
		return
	}
	// 初めて作るときに、旧 ClaudeMonitor の認証データ（Cookie と鍵だけ）を引き継ぐ（D1）。
	v.migrateLegacy()
	dir := v.dataDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		v.p.reportError("network_error", fmt.Sprintf("認証データフォルダを作れません: %v", err))
		return
	}
	aw := newWebView(webview2.WebViewOptions{
		Debug:    false,
		DataPath: dir,
		WindowOptions: webview2.WindowOptions{
			Title:  v.p.title,
			Width:  500,
			Height: 700,
			Center: false,
		},
	})
	if aw == nil {
		fmt.Fprintln(os.Stderr, "[aiusage]", v.p.id, "WebView 生成失敗")
		v.p.reportError("network_error", "認証用 WebView を生成できません")
		return
	}
	hwnd := uintptr(aw.Window())
	w := &authWin{v: v}
	authWins.Store(hwnd, w)
	w.orig, _, _ = procSetWindowLongPtr.Call(hwnd, uintptr(gwlpWndProc), authWndProcCallback)
	moveOffscreen(hwnd)

	v.p.bind(aw)
	aw.Init(v.p.script)
	aw.Navigate(v.p.usageURL)

	v.mu.Lock()
	v.inst, v.hwnd = aw, hwnd
	v.mu.Unlock()

	// 補助ページが読み込めず、初回取得が来ないまま止まることがある（init のまま）。
	// 通常の定期取得は 5 分後なので、それまで「取得中…」のままになる。一定時間たっても
	// init なら、こちらから取得を依頼する（応答が無ければ refresh のタイムアウトで再読み込みする）。
	time.AfterFunc(initStallCheckDelay, func() { v.refreshIfStillInit(aw) })

	// 生成中（Embed の入れ子メッセージループの間）に積まれた Dispatch は、次に誰かが
	// Dispatch を積むまで止まる（T0-1 で 433ms の遅延を確認）。空の Dispatch を 1 回積んで
	// 掃き出す。create は Dispatch の内側から呼ばれるので、積むだけで待たない。
	hostDispatch(func() {})
}

// destroy は補助 WebView を破棄する。UI スレッドで呼ぶこと。
// ブラウザプロセス一式は約 1 秒で終了し、メモリが解放される（T0-3）。
func (v *authView) destroy() {
	v.mu.Lock()
	inst, hwnd := v.inst, v.hwnd
	v.inst, v.hwnd = nil, 0
	v.mu.Unlock()
	if inst == nil {
		return
	}
	if val, ok := authWins.Load(hwnd); ok {
		val.(*authWin).destroying.Store(true)
	}
	v.visible.Store(false)
	inst.Destroy()
	v.signalDone() // 取得待ちの refresh を解放する（タイムアウトのエラーを出させない）
	v.p.reset()
	notifyChanged()
}

// showLogin はログイン画面を画面内に出す。既に出していれば前面にするだけ。
// どの goroutine からでも呼べる。
func (v *authView) showLogin() {
	if v.instance() == nil {
		return
	}
	wasHidden := v.visible.CompareAndSwap(false, true)
	if wasHidden {
		// ログインの完了を待つ。ログイン後の画面遷移ではページ内の取得スクリプトが再実行されず、
		// 通常の取得（既定 5 分ごと）まで未ログインのまま残るので、こちらから取得し直す。
		// 成功すれば applyUsagePayload が hideLogin を呼び、visible が false になって止まる。
		go watchLoop(loginWatchInterval, loginWatchMax, func() bool { return v.visible.Load() && v.instance() != nil }, v.refresh)
	}
	hostDispatch(func() {
		inst, hwnd := v.state()
		if inst == nil {
			return
		}
		if !wasHidden {
			procSetForegroundWindow.Call(hwnd)
			return
		}
		moveOnscreen(hwnd, v.p.loginPos[0], v.p.loginPos[1])
		inst.Navigate(v.p.loginURL)
	})
}

// ログイン画面を出している間に取得し直す間隔と、待つ最長の時間。
var (
	loginWatchInterval = 4 * time.Second
	loginWatchMax      = 10 * time.Minute
)

// watchLoop は interval ごとに step を呼ぶ。keepGoing が false になるか、max を過ぎたら止まる。
// 呼び出し側は goroutine で動かすこと（step は取得の完了を待つので長くかかる）。
func watchLoop(interval, max time.Duration, keepGoing func() bool, step func()) {
	deadline := time.Now().Add(max)
	for time.Now().Before(deadline) {
		time.Sleep(interval)
		if !keepGoing() {
			return
		}
		step()
	}
}

// hideLogin は取得に成功した後、ログイン画面を画面外へ戻す。どの goroutine からでも呼べる。
func (v *authView) hideLogin() {
	if !v.visible.CompareAndSwap(true, false) {
		return
	}
	hostDispatch(func() {
		if _, hwnd := v.state(); hwnd != 0 {
			moveOffscreen(hwnd)
		}
		if host != nil {
			host.OnAuthWindowHidden()
		}
	})
}

// setInterval はページ内のバックアップタイマーの間隔を差し替える。
func (v *authView) setInterval(ms int) {
	hostDispatch(func() {
		if inst := v.instance(); inst != nil {
			inst.Eval(fmt.Sprintf(v.p.intervalJS, ms))
		}
	})
}

// refreshTimeout は取得スクリプトのコールバックを待つ上限。
// ページ内の timedFetch の合計上限（Claude 12 秒 / Codex 14 秒）より長く取る。
// これより短いと Go 側のタイムアウトが先に走り、ページ内が特定した理由が表示に出ない。
const refreshTimeout = 20 * time.Second

// refresh はページ内の取得スクリプトを走らせ、Bind コールバックの完了まで待つ（最大 refreshTimeout）。
// 並行呼び出しは refreshMu で直列化される。
//
// UI スレッドから同期で呼んではならない（Dispatch した Eval の完了を待つのでデッドロックする）。
func (v *authView) refresh() {
	cfg := snapshotConfig()
	if !cfg.Enabled || !v.p.enabled(cfg) || v.instance() == nil {
		return
	}
	v.refreshMu.Lock()
	defer v.refreshMu.Unlock()

	ch := make(chan struct{}, 1)
	v.notifyMu.Lock()
	v.notify = ch
	v.notifyMu.Unlock()
	defer func() {
		v.notifyMu.Lock()
		v.notify = nil
		v.notifyMu.Unlock()
	}()

	hostDispatch(func() {
		if inst := v.instance(); inst != nil {
			inst.Eval(v.p.fetchJS)
		}
	})

	select {
	case <-ch:
	case <-time.After(refreshTimeout):
		// ページ内の取得は timedFetch で個別に打ち切り、理由付きで返るようになっている。
		// ここに落ちるのは取得スクリプトが注入されていない等、ページ側が応答できない場合。
		v.p.reportError("network_error", "fetch timeout (ページが応答しません)")
		// ページが応答しないままだと、以後の取得も同じ理由で失敗し続ける（Eval するだけで、
		// ページを読み込み直す経路が無かった）。使用量ページを開き直せば、初期化スクリプトが
		// 注入され直し、ページ側の初回取得（読み込み後に自動で走る）で復帰できる。
		if shouldReloadAfterTimeout(time.Now(), v.lastReload, v.visible.Load()) {
			v.lastReload = time.Now()
			notifyLog("%s: 取得がタイムアウトしました。補助ページを再読み込みします", v.p.id)
			hostDispatch(func() { v.reloadUsagePage() })
		}
	}
}

// reloadUsagePage は補助ページを使用量ページへ開き直す。UI スレッドで呼ぶこと。
// 開き直したら true を返す。
//
// 呼ぶ時点でもログイン画面の表示状態を確認する。hostDispatch は非同期なので、再読み込みを予約した
// 時点では出ていなくても、実行までの間に showLogin がログイン画面へ遷移させていることがある。
// 確認しないと、ログイン画面を出した直後に使用量ページへ遷移させて、操作中のページを奪ってしまう。
func (v *authView) reloadUsagePage() bool {
	if v.visible.Load() {
		return false
	}
	inst := v.instance()
	if inst == nil {
		return false
	}
	inst.Navigate(v.p.usageURL)
	return true
}

// initStallCheckDelay は、補助ページ生成から、初回取得が来ているか（init のままでないか）を
// 確認するまでの時間。通常は数秒で最初の取得が済むので、それより十分長く取る。
const initStallCheckDelay = 45 * time.Second

// refreshIfStillInit は、補助ページ inst の生成から時間がたっても認証状態が init のままなら、
// 取得を依頼する。その間にページが作り直された・破棄された場合や、状態が進んでいる場合は何もしない。
// どの goroutine からでも呼べる（refresh は待つので UI スレッドからは呼ばないこと）。
func (v *authView) refreshIfStillInit(inst webview2.WebView) {
	if v.instance() != inst || v.p.authState == nil || v.p.authState() != "init" {
		return
	}
	notifyLog("%s: 起動から %v たっても初回取得が来ないため、取得を依頼します", v.p.id, initStallCheckDelay)
	v.refresh()
}

// reloadMinInterval は、取得タイムアウト後の再読み込みの最小間隔。
const reloadMinInterval = time.Minute

// shouldReloadAfterTimeout は、取得がタイムアウトしたときに補助ページを再読み込みするかを返す。
// ログイン画面を出している間は、操作中のページを奪わないので再読み込みしない。
// 短時間に繰り返さない（ログイン待ちの監視などで refresh が続けて呼ばれても連打にならない）。
func shouldReloadAfterTimeout(now, last time.Time, loginVisible bool) bool {
	if loginVisible {
		return false
	}
	return last.IsZero() || now.Sub(last) >= reloadMinInterval
}

// signalDone は Bind コールバックの完了を refresh に伝える。
func (v *authView) signalDone() {
	v.notifyMu.Lock()
	ch := v.notify
	v.notifyMu.Unlock()
	if ch == nil {
		return
	}
	select {
	case ch <- struct{}{}:
	default:
	}
}

// logout は WebView 内の全データ（HttpOnly の Cookie を含む）を消す（D7）。
// プロセスは再起動しない: Destroy → 認証フォルダを削除 → 再作成。再作成後は未ログインになり、
// 次の取得で needs_login が報告される。どの goroutine からでも呼べる（待たずに戻る）。
func (v *authView) logout() {
	if !v.loggingOut.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer v.loggingOut.Store(false)

		done := make(chan struct{})
		hostDispatch(func() {
			v.destroy()
			close(done)
		})
		<-done

		// ブラウザプロセスが終了するまでフォルダはロックされている（T0-3: 約 1 秒）。
		dir := v.dataDir()
		var err error
		for i := 0; i < 50; i++ {
			if err = os.RemoveAll(dir); err == nil {
				break
			}
			time.Sleep(200 * time.Millisecond)
		}
		if err != nil {
			notifyLog("logout %s: 認証データを削除できません: %v", v.p.id, err)
		}

		hostDispatch(func() { v.sync(snapshotConfig()) })
	}()
}
