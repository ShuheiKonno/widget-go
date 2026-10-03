package main

import (
	"crypto/rand"
	"crypto/subtle"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"unsafe"

	"widget-go/aiusage"
	"widget-go/sysinfo"
)

// generateToken は起動毎のランダムなセッショントークンを生成する。
// フロントに埋め込み、全 /api 呼び出しのカスタムヘッダ検証に使う（CSRF 対策）。
func generateToken() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// 生成失敗はほぼ起こらないが、起きた場合は fail-closed（安全側）で停止する。
		log.Fatalf("セキュリティトークンの生成に失敗: %v", err)
	}
	return hex.EncodeToString(b)
}

// securityMiddleware はローカル HTTP API をブラウザ由来の攻撃から守る多層防御を適用する。
//   - Host 検証: 127.0.0.1:<port> 以外を 403（DNS rebinding 対策）
//   - "/"（UI）: トークン不要。CSP と nosniff を付与してここでトークンを配布する
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
		if r.URL.Path == "/" {
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

func startServer(ln net.Listener, token string) {
	mux := http.NewServeMux()

	// AI 使用量（Claude / Codex）。securityMiddleware の内側に置かれる。
	aiusage.RegisterHandlers(mux)

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(getHTML(token)))
	})

	mux.HandleFunc("/api/system", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(getSystemData())
	})

	mux.HandleFunc("/api/close", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		go func() {
			cleanupTray()
			os.Exit(0)
		}()
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

	// theme / coreBase / netUnit などフロント向け設定を保存する。
	// hide.gpu / hide.npu は Go 側の GPU/NPU 収集の ON/OFF にも使う。
	mux.HandleFunc("/api/setoption", func(w http.ResponseWriter, r *http.Request) {
		key := r.URL.Query().Get("key")
		val := r.URL.Query().Get("value")
		switch key {
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
		case "order":
			// 区画の表示順: value=cpu,memory,gpu,npu,network,ai を並べ替えたもの
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
				// GPU と NPU が両方非表示のときだけ収集を止める（Config.syncShowGPU）
				enable := snapshotConfig().ShowGPU
				showGPU.Store(enable)
				if !enable {
					// 次の /api/system が古い GPU/NPU を返して一瞬再表示されないよう即座に捨てる
					clearGPUCache()
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
			"coreBase":    c.CoreBase,
			"netUnit":     c.NetUnit,
			"showGPU":     showGPU.Load(),
			"collapsed":   c.Collapsed,
			"hidden":      c.Hidden,
			"order":       c.Order,
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

	port := ln.Addr().(*net.TCPAddr).Port
	if err := http.Serve(ln, securityMiddleware(mux, token, port)); err != nil {
		// http.Serve は正常時は戻らない。戻った＝致命的（リスナー閉塞など）。
		log.Fatalf("HTTP サーバーが停止しました: %v", err)
	}
}

// listenLocal は 127.0.0.1 の空きポートでリスナーを確保して返す。
// 旧 getAvailablePort のように一度 Close して再 Listen すると、その隙間に別プロセスへ
// ポートを奪われる TOCTOU があった。確保したリスナーをそのまま Serve に渡すことで回避する。
func listenLocal() (net.Listener, int) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatalf("ローカルポートの確保に失敗: %v", err)
	}
	return ln, ln.Addr().(*net.TCPAddr).Port
}

//go:embed ui/index.html
var indexHTML string

// getHTML は埋め込み済みの UI HTML を返す。__WIDGET_TOKEN__ を起動毎の
// セッショントークンに差し替える（<script> の CSP nonce と JS の WIDGET_TOKEN 定数）。
func getHTML(token string) string {
	return strings.ReplaceAll(indexHTML, "__WIDGET_TOKEN__", token)
}
