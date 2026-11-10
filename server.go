package main

import (
	"crypto/rand"
	"crypto/subtle"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"unsafe"

	"widget-go/aiusage"
	"widget-go/ping"
	"widget-go/sysinfo"
)

// generateToken は起動毎のランダムなセッショントークンを生成する。
// フロントに埋め込み、全 /api 呼び出しのカスタムヘッダ検証に使う（CSRF 対策）。
func generateToken() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// 生成失敗はほぼ起こらないが、起きた場合は fail-closed（安全側）で停止する。
		fatalf("セキュリティトークンの生成に失敗しました: %v", err)
	}
	return hex.EncodeToString(b)
}

// pageAccessKey は、UI のページ（"/"・"/settings"・"/worktimer"）を取得するための、起動毎の秘密のキー。
// API トークンは HTML に埋め込まれるため、ページを誰でも取れると、同じ PC の別ユーザー（ループバックのポートは
// 全ユーザーから見える）がトークンを読めてしまう。ページの URL（?k=）にキーを要求して、WebView2 以外が
// HTML を取れないようにする。キーは URL に入るので、リロードでも壊れない。main が起動時に 1 回だけ設定する
// （空のままではすべて拒否する）。
var pageAccessKey string

// pageURL は UI のページを開く URL（キーつき）。
func pageURL(port int, path string) string {
	return fmt.Sprintf("http://127.0.0.1:%d%s?k=%s", port, path, pageAccessKey)
}

func validPageKey(k string) bool {
	return pageAccessKey != "" && subtle.ConstantTimeCompare([]byte(k), []byte(pageAccessKey)) == 1
}

// securityMiddleware はローカル HTTP API をブラウザ由来の攻撃から守る多層防御を適用する。
//   - Host 検証: 127.0.0.1:<port> 以外を 403（DNS rebinding 対策）
//   - "/"（UI）: ページ用のキー（?k=）が必要。CSP と nosniff を付与してここでトークンを配布する
//   - "/api/*": X-Widget-Token カスタムヘッダを検証（クロスサイトの単純リクエストは
//     カスタムヘッダを付けられず、付ければ preflight が発生して失敗するため CSRF を防ぐ）
func securityMiddleware(next http.Handler, token string, port int) http.Handler {
	expectedHost := fmt.Sprintf("127.0.0.1:%d", port)
	scriptCSP := "default-src 'none'; " +
		"script-src 'nonce-" + token + "'; " +
		"style-src 'unsafe-inline'; " +
		"connect-src 'self'; " +
		"img-src 'self' data:"
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != expectedHost {
			http.Error(w, "forbidden host", http.StatusForbidden)
			return
		}
		if r.URL.Path == "/" || r.URL.Path == "/settings" || r.URL.Path == "/worktimer" {
			if !validPageKey(r.URL.Query().Get("k")) {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			w.Header().Set("Content-Security-Policy", scriptCSP)
			w.Header().Set("X-Content-Type-Options", "nosniff")
			next.ServeHTTP(w, r)
			return
		}
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Widget-Token")), []byte(token)) != 1 {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// errPowerModeDisabled は電源モードの切替機能が設定で OFF のときに返す。
// OFF の間は Windows の設定・制御のままにするため、書き込みは行わない。
var errPowerModeDisabled = errors.New("電源モードの切替が設定で OFF になっています")

// setPowerModeIfEnabled は切替機能が ON のときだけ電源モードを切り替える。
func setPowerModeIfEnabled(mode string) error {
	if !snapshotConfig().PowerModeEnabled {
		return errPowerModeDisabled
	}
	return sysinfo.SetPowerMode(mode)
}

// powerModeHandler は /api/powermode のハンドラ。mode を許可リストで検証して電源モードを切り替え、
// 成功したら refresh が返す最新の状態を JSON で返す（取得できない環境では null）。
//
// 設定と再取得は引数で受け取る。テストで Win32 を呼ばず、状態コードの対応だけを検証するため。
//   - 400: mode が許可リスト外
//   - 403: 切替機能が設定で OFF
//   - 409: 電源プランがバランス以外（電源モードが効かない）
//   - 503: 電源モードの API が使えない
//   - 500: 設定に失敗（Win32 エラーコードを本文に含む）
func powerModeHandler(set func(mode string) error, refresh func() *sysinfo.PowerModeInfo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := set(r.URL.Query().Get("mode")); err != nil {
			switch {
			case errors.Is(err, sysinfo.ErrPowerModeInvalid):
				http.Error(w, "invalid mode", http.StatusBadRequest)
			case errors.Is(err, errPowerModeDisabled):
				http.Error(w, err.Error(), http.StatusForbidden)
			case errors.Is(err, sysinfo.ErrPowerPlanNotBalanced):
				http.Error(w, err.Error(), http.StatusConflict)
			case errors.Is(err, sysinfo.ErrPowerModeUnsupported):
				http.Error(w, err.Error(), http.StatusServiceUnavailable)
			default:
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(refresh())
	}
}

func newServerMux(token string) *http.ServeMux {
	mux := http.NewServeMux()

	// AI 使用量（Claude / Codex）。securityMiddleware の内側に置かれる。
	aiusage.RegisterHandlers(mux)
	skins.register(mux)

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(getHTML(token)))
	})

	// 設定ウィンドウ（別ウィンドウ）のページ。"/" と同じく、トークンなしで読める（トークンは HTML に入る）。
	mux.HandleFunc("/settings", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(getSettingsHTML(token)))
	})
	mux.HandleFunc("/api/opensettings", func(w http.ResponseWriter, r *http.Request) {
		openSettingsWindow()
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/api/closesettings", func(w http.ResponseWriter, r *http.Request) {
		closeSettingsWindow()
		w.WriteHeader(http.StatusOK)
	})
	// 設定ページの初回の描画が済んだ合図。非表示で作った設定ウィンドウを見せる。
	mux.HandleFunc("/api/settingsready", func(w http.ResponseWriter, r *http.Request) {
		settingsWin.reveal()
		w.WriteHeader(http.StatusOK)
	})
	// 設定ウィンドウで設定が変わった合図。ウィジェット本体に反映させる。
	mux.HandleFunc("/api/settingschanged", func(w http.ResponseWriter, r *http.Request) {
		notifyMainSettingsChanged()
		w.WriteHeader(http.StatusOK)
	})
	// 勤務時間の入力ウィンドウ（コンパクト表示から開く小さな別ウィンドウ）。ページは "/settings" と同じく、トークンなしで読める。
	mux.HandleFunc("/worktimer", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(getWorkTimerHTML(token)))
	})
	mux.HandleFunc("/api/openworktimer", func(w http.ResponseWriter, r *http.Request) {
		openWorkTimerWindow()
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/api/closeworktimer", func(w http.ResponseWriter, r *http.Request) {
		workWin.closeWindow()
		w.WriteHeader(http.StatusOK)
	})
	// 入力ウィンドウの高さを、ページが測った内容の高さ（CSS px）に合わせる。
	mux.HandleFunc("/api/resizeworktimer", func(w http.ResponseWriter, r *http.Request) {
		h, err := strconv.Atoi(r.URL.Query().Get("h"))
		if err != nil || h <= 0 || h > 4000 {
			http.Error(w, "invalid h", http.StatusBadRequest)
			return
		}
		resizeWorkWindow(int32(h))
		w.WriteHeader(http.StatusOK)
	})
	// 勤務時間ページの初回の描画が済んだ合図。非表示で作ったウィンドウを見せる。
	mux.HandleFunc("/api/worktimerready", func(w http.ResponseWriter, r *http.Request) {
		workWin.reveal()
		w.WriteHeader(http.StatusOK)
	})
	// スタートメニュー / スタートアップのショートカット（チェックで作成、外すと削除）。
	shortcuts := defaultShortcutService()
	mux.HandleFunc("/api/shortcuts", shortcuts.handleStatus)
	mux.HandleFunc("/api/shortcuts/set", shortcuts.handleSet)

	// 監視先ホスト一覧の更新（最大5件、JSON ボディ）。
	// 書式が正しくないホスト（IPv6 など）は 400 で断る。重複は 1 件にまとめて保存し、保存した一覧を返す。
	mux.HandleFunc("/api/ping/targets", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
		var targets []ping.TargetConfig
		if err := json.NewDecoder(r.Body).Decode(&targets); err != nil {
			http.Error(w, "invalid json: "+err.Error(), http.StatusBadRequest)
			return
		}
		if len(targets) > ping.MaxTargets {
			http.Error(w, fmt.Sprintf("監視先は最大 %d 件までです", ping.MaxTargets), http.StatusBadRequest)
			return
		}
		for _, t := range targets {
			if err := ping.ValidateHost(t.Host); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		}
		mutateConfig(func(c *Config) {
			c.Ping.Targets = targets
			c.Ping = ping.NormalizeConfig(c.Ping)
		})
		pingKick()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(snapshotConfig().Ping.Targets)
	})

	mux.HandleFunc("/api/system", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(getSystemData())
	})

	mux.HandleFunc("/api/close", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		go func() {
			pomodoroShutdown() // 隠した Teams / Outlook と止めた通知を戻してから終わる（以後は隠さない）
			cleanupTray()
			os.Exit(0)
		}()
	})

	// フロントの初回レイアウト（テーマ・高さ）が済んだ合図。非表示で作ったメインウィンドウを出す。
	mux.HandleFunc("/api/ready", func(w http.ResponseWriter, r *http.Request) {
		showMainWindowOnce("ready")
		w.WriteHeader(http.StatusOK)
	})

	mux.HandleFunc("/api/entertray", func(w http.ResponseWriter, r *http.Request) {
		hideWindowToTray()
		w.WriteHeader(http.StatusOK)
	})

	mux.HandleFunc("/api/setviewmode", func(w http.ResponseWriter, r *http.Request) {
		modeStr := r.URL.Query().Get("mode")
		m, err := strconv.Atoi(modeStr)
		if err != nil || m < 0 || m > 1 {
			http.Error(w, "invalid mode", http.StatusBadRequest)
			return
		}
		// 切替の途中は、並走する /api/persistwindow に位置を保存させない（viewModeSwitching を参照）。
		// モードを変える前に立て、ウィンドウを動かし終えてから下ろす。
		viewModeSwitching.Store(true)
		defer viewModeSwitching.Store(false)
		viewModeMu.Lock()
		prev := viewMode
		viewMode = m
		viewModeMu.Unlock()
		mutateConfig(func(c *Config) { c.ViewMode = m })
		// 幅と位置の切替はここで行う。フロントは class の付け外しと高さ再測定だけを担当する。
		applyViewModeGeometry(m, prev)
		w.WriteHeader(http.StatusOK)
	})

	// ドラッグ開始時に現在のカーソル位置とウィンドウ位置を返す
	// ドラッグ開始 — カーソルとウィンドウの位置を物理ピクセルで保存
	mux.HandleFunc("/api/dragstart", func(w http.ResponseWriter, r *http.Request) {
		dragMu.Lock()
		dragStartCurX, dragStartCurY = getCursorPos()
		dragStartWinX, dragStartWinY = getWindowPos()
		dragMu.Unlock()
		w.WriteHeader(http.StatusOK)
	})

	// ドラッグ中 — 現在のカーソル位置を Win32 API で取得し、
	// 開始時との差分でウィンドウを移動（すべて物理ピクセル）
	mux.HandleFunc("/api/dragmove", func(w http.ResponseWriter, r *http.Request) {
		cx, cy := getCursorPos()
		dragMu.Lock()
		newX := dragStartWinX + (cx - dragStartCurX)
		newY := dragStartWinY + (cy - dragStartCurY)
		dragMu.Unlock()
		moveWindow(newX, newY)
		w.WriteHeader(http.StatusOK)
	})

	mux.HandleFunc("/api/windowsize", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		wid, hei := getWindowSize()
		json.NewEncoder(w).Encode(map[string]int32{
			"width":  wid,
			"height": hei,
		})
	})

	mux.HandleFunc("/api/toggletopmost", func(w http.ResponseWriter, r *http.Request) {
		alwaysOnTopMu.Lock()
		alwaysOnTop = !alwaysOnTop
		current := alwaysOnTop
		alwaysOnTopMu.Unlock()
		setTopmost(current)
		mutateConfig(func(c *Config) { c.Topmost = current })
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"topmost": current})
	})

	// 常に前面表示を明示設定する (enable=true|false)
	mux.HandleFunc("/api/topmost", func(w http.ResponseWriter, r *http.Request) {
		v := r.URL.Query().Get("enable")
		enable := v == "true" || v == "1"
		alwaysOnTopMu.Lock()
		alwaysOnTop = enable
		alwaysOnTopMu.Unlock()
		setTopmost(enable)
		mutateConfig(func(c *Config) { c.Topmost = enable })
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"topmost": enable})
	})

	// ウィンドウ位置とサイズ (論理ピクセル) をまとめて返す
	mux.HandleFunc("/api/windowgeometry", func(w http.ResponseWriter, r *http.Request) {
		var x, y int32
		if windowHandle.Load() != 0 {
			var rect RECT
			procGetWindowRect.Call(windowHandle.Load(), uintptr(unsafe.Pointer(&rect)))
			scale := getWindowDpiScale()
			if scale <= 0 {
				scale = 1.0
			}
			x = int32(math.Round(float64(rect.Left) / scale))
			y = int32(math.Round(float64(rect.Top) / scale))
		}
		wid, hei := getWindowSize()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]int32{
			"x":      x,
			"y":      y,
			"width":  wid,
			"height": hei,
		})
	})

	// ウィンドウ位置とサイズを明示的に設定する (論理ピクセル)
	mux.HandleFunc("/api/setwindow", func(w http.ResponseWriter, r *http.Request) {
		if windowHandle.Load() == 0 {
			http.Error(w, "window not ready", http.StatusServiceUnavailable)
			return
		}
		q := r.URL.Query()
		xVal, xErr := strconv.Atoi(q.Get("x"))
		yVal, yErr := strconv.Atoi(q.Get("y"))
		wVal, _ := strconv.Atoi(q.Get("w"))
		hVal, _ := strconv.Atoi(q.Get("h"))

		if wVal > 0 && hVal > 0 {
			resizeWindow(int32(wVal), int32(hVal), false)
		}
		if xErr == nil && yErr == nil {
			scale := getWindowDpiScale()
			if scale <= 0 {
				scale = 1.0
			}
			// 画面外に完全に出ないよう、アクティブモニタの作業領域でクランプする
			hmon, _, _ := procMonitorFromWindow.Call(windowHandle.Load(), MONITOR_DEFAULTTONEAREST)
			if hmon != 0 {
				var mi MONITORINFO
				mi.CbSize = uint32(unsafe.Sizeof(mi))
				if ok, _, _ := procGetMonitorInfoW.Call(hmon, uintptr(unsafe.Pointer(&mi))); ok != 0 {
					var rect RECT
					procGetWindowRect.Call(windowHandle.Load(), uintptr(unsafe.Pointer(&rect)))
					ww := rect.Right - rect.Left
					hh := rect.Bottom - rect.Top
					physX := int32(math.Round(float64(xVal) * scale))
					physY := int32(math.Round(float64(yVal) * scale))
					minX := mi.RcWork.Left
					minY := mi.RcWork.Top
					maxX := mi.RcWork.Right - ww
					maxY := mi.RcWork.Bottom - hh
					if physX < minX {
						physX = minX
					}
					if physX > maxX {
						physX = maxX
					}
					if physY < minY {
						physY = minY
					}
					if physY > maxY {
						physY = maxY
					}
					moveWindow(physX, physY)
					persistCurrentWindowGeometry()
					w.WriteHeader(http.StatusOK)
					return
				}
			}
			// モニタ情報が取れない場合はそのまま適用
			physX := int32(math.Round(float64(xVal) * scale))
			physY := int32(math.Round(float64(yVal) * scale))
			moveWindow(physX, physY)
		}
		persistCurrentWindowGeometry()
		w.WriteHeader(http.StatusOK)
	})

	// 透過モードトグル (旧 UI 互換)。現行 UI は /api/transparent?enable= を使う。
	mux.HandleFunc("/api/toggletransparent", func(w http.ResponseWriter, r *http.Request) {
		transparentMu.Lock()
		transparent = !transparent
		current := transparent
		transparentMu.Unlock()
		setTransparent(current)
		mutateConfig(func(c *Config) { c.Transparent = current })
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"transparent": current})
	})

	// 透過モードを明示的に設定する (enable=true|false)
	mux.HandleFunc("/api/transparent", func(w http.ResponseWriter, r *http.Request) {
		v := r.URL.Query().Get("enable")
		enable := v == "true" || v == "1"
		transparentMu.Lock()
		transparent = enable
		transparentMu.Unlock()
		setTransparent(enable)
		mutateConfig(func(c *Config) { c.Transparent = enable })
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"transparent": enable})
	})

	// トレイ表示モード (icon|cpu) を設定する
	mux.HandleFunc("/api/traydisplay", func(w http.ResponseWriter, r *http.Request) {
		modeStr := r.URL.Query().Get("mode")
		var m int
		var label string
		switch modeStr {
		case "icon", "0":
			m = TRAY_DISPLAY_ICON
			label = "icon"
		case "cpu", "1":
			m = TRAY_DISPLAY_CPU
			label = "cpu"
		case "usage", "2":
			// 保存はするが、AI 使用量が OFF の間は実際の表示がアイコンのみになる（effectiveTrayDisplay）
			m = TRAY_DISPLAY_USAGE
			label = "usage"
		default:
			http.Error(w, "invalid mode", http.StatusBadRequest)
			return
		}
		trayDisplayMu.Lock()
		trayDisplayMode = m
		trayDisplayMu.Unlock()
		mutateConfig(func(c *Config) { c.TrayDisplay = label })
		kickTray() // 次の周期を待たずに反映する
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]int{"trayDisplay": m})
	})

	// 電源モード（設定アプリの「電源モード」）を切り替える (mode=saver|balanced|best)。
	// OS の状態を書き換えるが、config には保存しない（起動時に再適用して設定アプリでの変更を戻さないため）。
	// トレイアイコンの帯の色も電源モードで決まるので、切替後は次の周期を待たずに更新を促す。
	mux.HandleFunc("/api/powermode", powerModeHandler(setPowerModeIfEnabled, func() *sysinfo.PowerModeInfo {
		info := refreshPowerMode()
		kickTray()
		return info
	}))

	// 勤務時間タイマーの操作（開始・終了・リセット・開始時刻の変更・1 分調整）。応答は操作後のスナップショット。
	mux.HandleFunc("/api/worktimer", workTimerActionHandler)
	// ポモドーロの操作（開始・一時停止・再開・スキップ・リセット・一時的に表示）。応答は操作後のスナップショット。
	mux.HandleFunc("/api/pomodoro", pomodoroActionHandler)

	// theme / coreBase / netUnit などフロント向け設定を保存する。
	// hide.gpu / hide.npu は Go 側の GPU/NPU 収集の ON/OFF にも使う。
	mux.HandleFunc("/api/setoption", func(w http.ResponseWriter, r *http.Request) {
		key := r.URL.Query().Get("key")
		val := r.URL.Query().Get("value")
		switch key {
		case "skin":
			if r.Method != http.MethodPost {
				w.Header().Set("Allow", http.MethodPost)
				http.Error(w, "許可されていないメソッドです", http.StatusMethodNotAllowed)
				return
			}
			if err := skins.setActive(val); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		case "theme":
			if val != "dark" && val != "light" && val != "system" {
				http.Error(w, "invalid theme", http.StatusBadRequest)
				return
			}
			mutateConfig(func(c *Config) { c.Theme = val })
		case "coreBase":
			n, err := strconv.Atoi(val)
			if err != nil || (n != 0 && n != 1) {
				http.Error(w, "invalid coreBase", http.StatusBadRequest)
				return
			}
			mutateConfig(func(c *Config) { c.CoreBase = n })
		case "netUnit":
			if val != "byte" && val != "bit" {
				http.Error(w, "invalid netUnit", http.StatusBadRequest)
				return
			}
			mutateConfig(func(c *Config) { c.NetUnit = val })
		case "zoom":
			// 拡大率（%）。zoomSteps のどれか。保存して、ウィンドウの大きさと WebView のズームを変える
			n, err := strconv.Atoi(val)
			if err != nil || !isZoomStep(n) {
				http.Error(w, "invalid zoom", http.StatusBadRequest)
				return
			}
			setZoom(n)
		case "diskIoView":
			if val != "number" && val != "graph" {
				http.Error(w, "invalid diskIoView", http.StatusBadRequest)
				return
			}
			mutateConfig(func(c *Config) { c.DiskIOView = val })
		case "powerMode":
			// 電源モードの切替機能の ON/OFF。OFF にした直後に API 呼び出しと表示を止める
			// （次の周期を待たない）。ON にしたときも即座に現在の状態を取得する。
			if err := setPowerModeEnabled(val); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			refreshPowerMode()
			kickTray()
		case "workTarget", "workLunch", "workRed", "workFloor", "workLag":
			// 勤務時間タイマーの設定（worktimer.go の setWorkTimerOption に検証がある）
			if err := setWorkTimerOption(key, val); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		case "pomoFocus", "pomoShort", "pomoLong", "pomoEvery", "pomoAuto", "pomoFocusMode", "pomoHide", "pomoTargets":
			// ポモドーロの設定（pomodoro.go の setPomodoroOption に検証がある）
			restore, err := setPomodoroOption(key, val)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if restore {
				// 隠す方法や対象が変わったときは、隠したものをいったん戻してから、あらためて適用する
				go func() { pomodoroRestoreAll(); pomodoroKick() }()
			} else {
				pomodoroKick() // 集中モードの ON/OFF を、次の周期を待たずに反映する
			}
		case "pingNotify", "pingInterval":
			// Ping 監視の設定（監視先の一覧だけは、JSON ボディの /api/ping/targets で更新する）
			if err := setPingOption(key, val); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		case "order":
			// 区画の表示順: value=cpu,memory,disk,gpu,npu,network,ai,work,pomodoro,ping を並べ替えたもの
			if err := setOrder(val); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		default:
			// 区画の表示/非表示: key=hide.<name>&value=true|false（true が非表示）
			if name, ok := strings.CutPrefix(key, "hide."); ok {
				if err := setHidden(name, val); err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				pomodoroKick() // ポモドーロを非表示にしたら、隠したものを戻す（表示に戻したら、集中中なら再び隠す）
				pingKick()     // Ping を非表示にしたら監視を止め、表示に戻したら再開する
				// GPU と NPU が両方非表示のときだけ収集を止める（Config.syncShowGPU）
				enable := snapshotConfig().ShowGPU
				showGPU.Store(enable)
				if !enable {
					// 次の /api/system が古い GPU/NPU を返して一瞬再表示されないよう即座に捨てる
					clearGPUCache()
				}
				// Disk 区画も同じく、非表示の間は収集を止める。表示に戻したときは容量をすぐ取り直す。
				hideDisk := snapshotConfig().Hidden.Disk
				if showDisk.Swap(!hideDisk) == hideDisk { // 表示状態が変わった
					if hideDisk {
						clearDiskCache()
					} else {
						select {
						case diskKick <- struct{}{}:
						default:
						}
					}
				}
				break
			}
			// 区画の折りたたみ: key=collapse.<name>&value=true|false
			if name, ok := strings.CutPrefix(key, "collapse."); ok {
				if err := setCollapsed(name, val); err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				break
			}
			// 区画が出している面（表 / 裏）: key=flip.<name>&value=true|false（true が裏面）
			if name, ok := strings.CutPrefix(key, "flip."); ok {
				if err := setFlipped(name, val); err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				break
			}
			http.Error(w, "unknown key", http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	// 現在のサーバー側設定を一括取得する (フロント側の初期化で使用)
	mux.HandleFunc("/api/settings", func(w http.ResponseWriter, r *http.Request) {
		c := snapshotConfig()
		viewModeMu.RLock()
		vm := viewMode
		viewModeMu.RUnlock()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"transparent": c.Transparent,
			"topmost":     c.Topmost,
			"trayDisplay": c.TrayDisplay,
			"theme":       c.Theme,
			"skin":        skins.active(c.Skin),
			"coreBase":    c.CoreBase,
			"netUnit":     c.NetUnit,
			"diskIoView":  c.DiskIOView,
			"zoom":        normalizeZoom(c.Zoom),
			"zoomSteps":   zoomSteps,
			"showGPU":     showGPU.Load(),
			"powerMode":   c.PowerModeEnabled,
			"collapsed":   c.Collapsed,
			"flipped":     c.Flipped,
			"hidden":      c.Hidden,
			"order":       c.Order,
			"workTimer":   c.WorkTimer,
			"pomodoro":    pomodoroSettingsView(c.Pomodoro),
			"ping":        c.Ping,
			"viewMode":    vm,
			// CPU 名は起動後に変わらないので、2 秒周期の /api/system ではなく
			// 起動時 1 回の設定取得に載せる。
			"cpuName": sysinfo.GetCPUName(),
		})
	})

	// リサイズ実行。mousemove 中に連続して呼ばれるので、ここでは永続化しない。
	// 確定タイミング (mouseup) で /api/persistwindow が呼ばれる。
	mux.HandleFunc("/api/resize", func(w http.ResponseWriter, r *http.Request) {
		wStr := r.URL.Query().Get("w")
		hStr := r.URL.Query().Get("h")
		wVal, _ := strconv.Atoi(wStr)
		h, _ := strconv.Atoi(hStr)
		anchorTop := r.URL.Query().Get("anchor") == "top"
		resizeWindow(int32(wVal), int32(h), anchorTop)
		w.WriteHeader(http.StatusOK)
	})

	// フロント側のドラッグ/リサイズ確定時に現在位置を config に記録する
	mux.HandleFunc("/api/persistwindow", func(w http.ResponseWriter, r *http.Request) {
		persistCurrentWindowGeometry()
		w.WriteHeader(http.StatusOK)
	})

	return mux
}

func startServer(ln net.Listener, token string) {
	mux := newServerMux(token)
	port := ln.Addr().(*net.TCPAddr).Port
	if err := http.Serve(ln, securityMiddleware(mux, token, port)); err != nil {
		// http.Serve は正常時は戻らない。戻った＝致命的（リスナー閉塞など）。
		fatalf("内部の HTTP サーバーが停止しました: %v", err)
	}
}

// listenLocal は 127.0.0.1 の空きポートでリスナーを確保して返す。
// 旧 getAvailablePort のように一度 Close して再 Listen すると、その隙間に別プロセスへ
// ポートを奪われる TOCTOU があった。確保したリスナーをそのまま Serve に渡すことで回避する。
func listenLocal() (net.Listener, int) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fatalf("ローカルポートを確保できませんでした: %v", err)
	}
	return ln, ln.Addr().(*net.TCPAddr).Port
}

//go:embed ui/index.html
var indexHTMLRaw string

//go:embed ui/worktimer.html
var workTimerHTMLRaw string

// 勤務時間の区画（CSS・マークアップ・JS）は、本体（ui/index.html）と、コンパクト表示から開く入力ウィンドウ
// （ui/worktimer.html）で同じものを使う。2 か所にコピーすると片方だけ直してずれるため、断片にして差し込む。
//
//go:embed ui/work/section.css
var workSectionCSS string

//go:embed ui/work/section.html
var workSectionHTML string

//go:embed ui/work/section.js
var workSectionJS string

// expandWorkParts は page の __WORK_CSS__ / __WORK_HTML__ / __WORK_JS__ を断片に差し替える。
// hidden が true なら、区画を最初は非表示にする（本体。設定を読むまでの間に出さない）。
func expandWorkParts(page string, hidden bool) string {
	hiddenClass := ""
	if hidden {
		hiddenClass = " is-hidden"
	}
	html := strings.ReplaceAll(workSectionHTML, "__WORK_HIDDEN__", hiddenClass)
	page = strings.ReplaceAll(page, "__WORK_CSS__", workSectionCSS)
	page = strings.ReplaceAll(page, "__WORK_HTML__", html)
	return strings.ReplaceAll(page, "__WORK_JS__", workSectionJS)
}

// 差し込みは起動時に 1 回だけ。トークンの差し替えは、リクエストのたびに行う。
var (
	indexHTML     = sync.OnceValue(func() string { return expandWorkParts(indexHTMLRaw, true) })
	workTimerHTML = sync.OnceValue(func() string { return expandWorkParts(workTimerHTMLRaw, false) })
)

// getHTML は埋め込み済みの UI HTML を返す。__WIDGET_TOKEN__ を起動毎の
// セッショントークンに差し替える（<script> の CSP nonce と JS の WIDGET_TOKEN 定数）。
func getHTML(token string) string {
	return strings.ReplaceAll(indexHTML(), "__WIDGET_TOKEN__", token)
}

// getWorkTimerHTML は勤務時間の入力ウィンドウの HTML（/worktimer）。
func getWorkTimerHTML(token string) string {
	return strings.ReplaceAll(workTimerHTML(), "__WIDGET_TOKEN__", token)
}

//go:embed ui/settings.html
var settingsHTML string

// getSettingsHTML は設定ウィンドウの HTML（/settings）。トークンの差し替えは getHTML と同じ。
func getSettingsHTML(token string) string {
	return strings.ReplaceAll(settingsHTML, "__WIDGET_TOKEN__", token)
}

// setPingOption は Ping 監視の単一キー設定を検証・保存する。
func setPingOption(key, val string) error {
	switch key {
	case "pingNotify":
		if val != "true" && val != "false" {
			return fmt.Errorf("invalid pingNotify value %q", val)
		}
		mutateConfig(func(c *Config) { c.Ping.NotifyEnabled = val == "true" })
		pingKick()
		return nil
	case "pingInterval":
		sec, err := strconv.Atoi(val)
		if err != nil || sec < 1 || sec > 60 {
			return fmt.Errorf("invalid pingInterval value %q", val)
		}
		mutateConfig(func(c *Config) { c.Ping.IntervalSec = sec })
		pingKick()
		return nil
	}
	return fmt.Errorf("unknown ping option %q", key)
}
