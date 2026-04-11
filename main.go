package main

import (
	"encoding/json"
	"fmt"
	"math"
	"net"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"widget-go/sysinfo"

	"github.com/jchv/go-webview2"
)

var (
	user32            = syscall.NewLazyDLL("user32.dll")
	procFindWindow    = user32.NewProc("FindWindowW")
	procGetWindowLong = user32.NewProc("GetWindowLongW")
	procSetWindowLong = user32.NewProc("SetWindowLongW")
	procSetWindowPos  = user32.NewProc("SetWindowPos")
	procGetWindowRect = user32.NewProc("GetWindowRect")
	procGetCursorPos  = user32.NewProc("GetCursorPos")
	procGetDpiForWindow = user32.NewProc("GetDpiForWindow")
)

const (
	WS_CAPTION       = 0x00C00000
	WS_THICKFRAME    = 0x00040000
	WS_SYSMENU       = 0x00080000
	SWP_FRAMECHANGED = 0x0020
	SWP_NOMOVE       = 0x0002
	SWP_NOSIZE       = 0x0001
	SWP_NOZORDER     = 0x0004

	windowWidth  = 210
	windowHeight = 400
)

var windowHandle uintptr

type POINT struct {
	X, Y int32
}

type RECT struct {
	Left, Top, Right, Bottom int32
}

type SystemData struct {
	CPU    *sysinfo.CPUInfo    `json:"CPU"`
	Memory *sysinfo.MemoryInfo `json:"Memory"`
	WiFi   *sysinfo.WiFiInfo   `json:"WiFi"`
}

// バックグラウンドで更新されるスナップショット。
// /api/system はこのキャッシュを読むだけで、その場での計測は行わない。
var (
	cacheMu    sync.RWMutex
	cachedCPU  *sysinfo.CPUInfo
	cachedMem  *sysinfo.MemoryInfo
	cachedWiFi *sysinfo.WiFiInfo
)

// ドラッグ開始時の状態（物理ピクセル）。
// ドラッグ中の差分計算をすべてサーバー側で行い、
// ブラウザの論理ピクセルと Win32 の物理ピクセルの不一致を回避する。
var (
	dragMu        sync.Mutex
	dragStartCurX int32
	dragStartCurY int32
	dragStartWinX int32
	dragStartWinY int32
)

func getSystemData() SystemData {
	cacheMu.RLock()
	defer cacheMu.RUnlock()
	return SystemData{
		CPU:    cachedCPU,
		Memory: cachedMem,
		WiFi:   cachedWiFi,
	}
}

func refreshCPUAndMem() {
	cpuInfo, cpuErr := sysinfo.GetCPUInfo()
	memInfo, memErr := sysinfo.GetMemoryInfo()

	cacheMu.Lock()
	if cpuErr == nil {
		cachedCPU = cpuInfo
	}
	if memErr == nil {
		cachedMem = memInfo
	}
	cacheMu.Unlock()
}

func refreshWiFi() {
	wifiInfo, err := sysinfo.GetWiFiInfo()
	if err != nil {
		// netsh が失敗した場合は前回のキャッシュを保持
		return
	}
	// 未接続（wifiInfo == nil）の場合もそのまま反映する
	cacheMu.Lock()
	cachedWiFi = wifiInfo
	cacheMu.Unlock()
}

// startCollector はバックグラウンドで周期的にシステム情報を取得し、
// キャッシュを更新する。CPU/メモリは 2 秒ごと、Wi-Fi は 15 秒ごと
// （netsh の呼び出しコストが高いため）。
func startCollector() {
	sysinfo.PrimeCPU()
	refreshCPUAndMem()
	refreshWiFi()

	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			refreshCPUAndMem()
		}
	}()

	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			refreshWiFi()
		}
	}()
}

func getCursorPos() (int32, int32) {
	var pt POINT
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	return pt.X, pt.Y
}

func getWindowPos() (int32, int32) {
	var rect RECT
	if windowHandle != 0 {
		procGetWindowRect.Call(windowHandle, uintptr(unsafe.Pointer(&rect)))
	}
	return rect.Left, rect.Top
}

func moveWindow(x, y int32) {
	if windowHandle != 0 {
		procSetWindowPos.Call(windowHandle, 0, uintptr(x), uintptr(y), 0, 0, SWP_NOSIZE|SWP_NOZORDER)
	}
}

func getWindowSize() (int32, int32) {
	if windowHandle == 0 {
		return windowWidth, windowHeight
	}
	var rect RECT
	procGetWindowRect.Call(windowHandle, uintptr(unsafe.Pointer(&rect)))
	width := rect.Right - rect.Left
	height := rect.Bottom - rect.Top
	scale := getWindowDpiScale()
	if scale <= 0 {
		scale = 1.0
	}
	logicalW := int32(math.Round(float64(width) / scale))
	logicalH := int32(math.Round(float64(height) / scale))
	if logicalW <= 0 {
		logicalW = windowWidth
	}
	if logicalH <= 0 {
		logicalH = windowHeight
	}
	return logicalW, logicalH
}

func getWindowDpiScale() float64 {
	if windowHandle == 0 {
		return 1.0
	}
	dpi, _, _ := procGetDpiForWindow.Call(windowHandle)
	if dpi == 0 {
		return 1.0
	}
	return float64(dpi) / 96.0
}

func resizeWindow(width, height int32) {
	if windowHandle == 0 {
		return
	}
	if width <= 0 || height <= 0 {
		curW, curH := getWindowSize()
		if width <= 0 {
			width = curW
		}
		if height <= 0 {
			height = curH
		}
	}
	if width < 200 {
		width = 200
	}
	if width > 600 {
		width = 600
	}
	if height < 200 {
		height = 200
	}
	if height > 1000 {
		height = 1000
	}
	scale := getWindowDpiScale()
	scaledW := int32(math.Round(float64(width) * scale))
	scaledH := int32(math.Round(float64(height) * scale))
	procSetWindowPos.Call(windowHandle, 0, 0, 0, uintptr(scaledW), uintptr(scaledH), SWP_NOMOVE|SWP_NOZORDER)
}

func startServer(port int) {
	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(getHTML()))
	})

	mux.HandleFunc("/api/system", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(getSystemData())
	})

	mux.HandleFunc("/api/close", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		go func() { os.Exit(0) }()
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

	mux.HandleFunc("/api/resize", func(w http.ResponseWriter, r *http.Request) {
		wStr := r.URL.Query().Get("w")
		hStr := r.URL.Query().Get("h")
		wVal, _ := strconv.Atoi(wStr)
		h, _ := strconv.Atoi(hStr)
		resizeWindow(int32(wVal), int32(h))
		w.WriteHeader(http.StatusOK)
	})

	http.ListenAndServe(fmt.Sprintf("127.0.0.1:%d", port), mux)
}

func getAvailablePort() int {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 18080
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	return port
}

func removeWindowFrame(title string) {
	titlePtr, _ := syscall.UTF16PtrFromString(title)
	hwnd, _, _ := procFindWindow.Call(0, uintptr(unsafe.Pointer(titlePtr)))
	if hwnd == 0 {
		return
	}
	windowHandle = hwnd

	gwlStyle := int32(-16)
	style, _, _ := procGetWindowLong.Call(hwnd, uintptr(gwlStyle))
	newStyle := style &^ (WS_CAPTION | WS_THICKFRAME | WS_SYSMENU)
	procSetWindowLong.Call(hwnd, uintptr(gwlStyle), newStyle)
	procSetWindowPos.Call(hwnd, 0, 0, 0, 0, 0, SWP_FRAMECHANGED|SWP_NOMOVE|SWP_NOZORDER|SWP_NOSIZE)
}

func main() {
	runtime.LockOSThread()

	port := getAvailablePort()
	startCollector()
	go startServer(port)

	windowTitle := "システムモニター"

	w := webview2.NewWithOptions(webview2.WebViewOptions{
		Debug:     false,
		AutoFocus: true,
		WindowOptions: webview2.WindowOptions{
			Title:  windowTitle,
			Width:  windowWidth,
			Height: windowHeight,
			Center: true,
		},
	})
	if w == nil {
		panic("Failed to create webview")
	}
	defer w.Destroy()

	go func() {
		time.Sleep(200 * time.Millisecond)
		removeWindowFrame(windowTitle)
	}()

	w.SetSize(windowWidth, windowHeight, webview2.HintFixed)
	w.Navigate(fmt.Sprintf("http://127.0.0.1:%d", port))

	w.Run()
}

func getHTML() string {
	return `<!DOCTYPE html>
<html lang="ja">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>システムモニター</title>
    <style>
        * { margin: 0; padding: 0; box-sizing: border-box; }
        html, body { width: 100%; height: 100%; overflow: hidden; }
        body { font-family: 'Segoe UI', 'Yu Gothic UI', sans-serif; background: linear-gradient(135deg, #3b3b6d 0%, #2e4a7a 100%); color: #fff; font-size: 13px; user-select: none; }
        .container { width: 100%; height: 100%; background: rgba(60,60,95,0.96); padding: 10px; position: relative; cursor: grab; }
        .container.dragging { cursor: grabbing; }
        .close-btn { position: absolute; top: 8px; right: 8px; width: 18px; height: 18px; border-radius: 50%; background: #ff5f57; border: none; cursor: pointer; display: flex; align-items: center; justify-content: center; font-size: 10px; color: transparent; transition: all 0.2s; z-index: 100; }
        .close-btn:hover { background: #ff3b30; color: rgba(0,0,0,0.6); }
        .header { text-align: center; margin-bottom: 10px; padding-bottom: 8px; border-bottom: 1px solid rgba(255,255,255,0.18); }
        .header h1 { font-size: 15px; font-weight: 600; color: #f5f5ff; }
        .section { background: rgba(255,255,255,0.09); border-radius: 8px; padding: 8px 10px; margin-bottom: 8px; border: 1px solid rgba(255,255,255,0.16); }
        .section:last-child { margin-bottom: 0; }
        .section-header { display: flex; align-items: center; margin-bottom: 6px; }
        .section-icon { font-size: 14px; margin-right: 6px; }
        .section-title { font-size: 12px; font-weight: 600; flex-grow: 1; color: #e8e8ff; }
        .section-value { color: #7bffe0; font-size: 13px; font-weight: 700; font-family: 'Consolas', monospace; }
        .progress-bar { height: 5px; background: rgba(255,255,255,0.18); border-radius: 3px; overflow: hidden; margin-bottom: 6px; }
        .progress-fill { height: 100%; border-radius: 3px; transition: width 0.3s ease; }
        .cpu-fill { background: linear-gradient(90deg, #8a9cff, #b07cff); }
        .memory-fill { background: linear-gradient(90deg, #ffa3ff, #ff7a8a); }
        .cores-container { display: grid; grid-template-columns: repeat(4,1fr); gap: 3px; margin-top: 5px; }
        .core-item { text-align: center; }
        .core-label { color:#c5c5e0; font-size:8px; display:block; margin-bottom:2px; }
        .core-bar-container { height:24px; background:rgba(255,255,255,0.16); border-radius:3px; position:relative; overflow:hidden; }
        .core-bar { position:absolute; bottom:0; width:100%; background:linear-gradient(180deg,#8a9cff,#b07cff); border-radius:3px; transition:height 0.3s ease; }
        .core-value { position:absolute; width:100%; text-align:center; color:#fff; font-size:9px; font-weight:600; top:50%; transform:translateY(-50%); text-shadow:0 1px 2px rgba(0,0,0,0.6); }
        .memory-details { text-align:right; font-size:11px; color:#c5c5e0; font-family:'Consolas', monospace; }
        .memory-details .used { color:#ffa3ff; font-weight:600; }
        .wifi-info { display:flex; flex-direction:column; gap:4px; }
        .wifi-row { display:flex; justify-content:space-between; align-items:center; }
        .wifi-label { color:#c5c5e0; font-size:11px; }
        .wifi-value { color:#fff; font-size:11px; font-weight:600; font-family:'Consolas', monospace; }
        .wifi-connected { color:#7fffaa; }
        .wifi-disconnected { color:#ff8a8a; }
    </style>
</head>
<body>
    <div class="container" id="dragArea">
        <button class="close-btn" id="closeBtn" title="閉じる">✕</button>
        <div class="header"><h1>システムモニター</h1></div>
        <div class="section">
            <div class="section-header"><span class="section-icon">💻</span><span class="section-title">CPU</span><span class="section-value" id="cpu-overall">0%</span></div>
            <div class="progress-bar"><div class="progress-fill cpu-fill" id="cpu-bar" style="width:0%"></div></div>
            <div class="cores-container" id="cores-container"></div>
        </div>
        <div class="section">
            <div class="section-header"><span class="section-icon">🧠</span><span class="section-title">メモリ</span><span class="section-value" id="memory-percent">0%</span></div>
            <div class="progress-bar"><div class="progress-fill memory-fill" id="memory-bar" style="width:0%"></div></div>
            <div class="memory-details"><span class="used" id="memory-used">0 GB</span><span id="memory-total">/ 0 GB</span></div>
        </div>
        <div class="section">
            <div class="section-header"><span class="section-icon">📶</span><span class="section-title">Wi-Fi</span></div>
            <div class="wifi-info">
                <div class="wifi-row"><span class="wifi-label">SSID</span><span class="wifi-value" id="wifi-ssid">---</span></div>
                <div class="wifi-row"><span class="wifi-label">帯域</span><span class="wifi-value" id="wifi-band">---</span></div>
                <div class="wifi-row"><span class="wifi-label">規格</span><span class="wifi-value" id="wifi-type">---</span></div>
                <div class="wifi-row"><span class="wifi-label">信号強度</span><span class="wifi-value" id="wifi-signal">---</span></div>
            </div>
        </div>
    </div>
    <script>
        const wifiStandards = { '802.11a':'802.11a','802.11b':'802.11b','802.11g':'802.11g','802.11n':'802.11n (Wi-Fi 4)','802.11ac':'802.11ac (Wi-Fi 5)','802.11ax':'802.11ax (Wi-Fi 6)','802.11be':'802.11be (Wi-Fi 7)' };
        function formatBytes(b){return (b/(1024*1024*1024)).toFixed(1)+' GB';}

        // Close button
        document.getElementById('closeBtn').addEventListener('click', function(e) {
            e.stopPropagation();
            fetch('/api/close').catch(()=>{});
        });

        // Drag functionality
        let isDragging = false;
        let isResizing = false;
        let resizeDir = '';
        let startCursorX, startCursorY;
        let startWinW = 0, startWinH = 0;
        const dragArea = document.getElementById('dragArea');
        const resizeMargin = 6;

        dragArea.addEventListener('mousemove', function(e) {
            if (isDragging || isResizing) return;
            const rect = dragArea.getBoundingClientRect();
            const onRight = (rect.right - e.clientX) <= resizeMargin;
            const onBottom = (rect.bottom - e.clientY) <= resizeMargin;
            if (onRight && onBottom) {
                dragArea.style.cursor = 'nwse-resize';
            } else if (onRight) {
                dragArea.style.cursor = 'ew-resize';
            } else if (onBottom) {
                dragArea.style.cursor = 'ns-resize';
            } else {
                dragArea.style.cursor = 'grab';
            }
        });

        dragArea.addEventListener('mouseleave', function() {
            if (!isDragging && !isResizing) {
                dragArea.style.cursor = 'grab';
            }
        });

        dragArea.addEventListener('mousedown', async function(e) {
            if (e.target.id === 'closeBtn') return;
            const rect = dragArea.getBoundingClientRect();
            const onRight = (rect.right - e.clientX) <= resizeMargin;
            const onBottom = (rect.bottom - e.clientY) <= resizeMargin;
            if (onRight || onBottom) {
                isResizing = true;
                resizeDir = (onRight ? 'r' : '') + (onBottom ? 'b' : '');
                dragArea.classList.add('resizing');
                startCursorX = e.screenX;
                startCursorY = e.screenY;
                try {
                    const res = await fetch('/api/windowsize');
                    const data = await res.json();
                    startWinW = data.width;
                    startWinH = data.height;
                } catch(err) {
                    isResizing = false;
                    dragArea.classList.remove('resizing');
                }
                e.preventDefault();
                return;
            }
            isDragging = true;
            dragArea.classList.add('dragging');
            fetch('/api/dragstart').catch(()=>{ isDragging = false; dragArea.classList.remove('dragging'); });
        });

        document.addEventListener('mousemove', function(e) {
            if (isResizing) {
                const dx = e.screenX - startCursorX;
                const dy = e.screenY - startCursorY;
                let newW = startWinW;
                let newH = startWinH;
                if (resizeDir.indexOf('r') !== -1) {
                    newW = startWinW + dx;
                }
                if (resizeDir.indexOf('b') !== -1) {
                    newH = startWinH + dy;
                }
                fetch('/api/resize?w=' + Math.round(newW) + '&h=' + Math.round(newH)).catch(()=>{});
                return;
            }
            if (!isDragging) return;
            fetch('/api/dragmove').catch(()=>{});
        });

        document.addEventListener('mouseup', function() {
            if (isResizing) {
                isResizing = false;
                dragArea.classList.remove('resizing');
            }
            isDragging = false;
            dragArea.classList.remove('dragging');
        });

        function adjustWindowHeight() {
            // height:100% / overflow:hidden の影響を避けるため
            // 一時的に auto にしてコンテンツの自然な高さを測定する
            const el = document.documentElement;
            const bd = document.body;
            const ct = document.getElementById('dragArea');
            el.style.height = 'auto'; bd.style.height = 'auto'; ct.style.height = 'auto';
            const h = ct.scrollHeight;
            el.style.height = '100%'; bd.style.height = '100%'; ct.style.height = '100%';
            fetch('/api/resize?h=' + h).catch(()=>{});
        }

        async function updateData(){
            try{ const res=await fetch('/api/system'); const data=await res.json();
                if(data.CPU){ const o=Math.round(data.CPU.Overall); document.getElementById('cpu-overall').textContent=o+'%'; document.getElementById('cpu-bar').style.width=o+'%'; const c=document.getElementById('cores-container'); if(c.children.length===0 && data.CPU.Cores){ data.CPU.Cores.forEach((_,i)=>{ const d=document.createElement('div'); d.className='core-item'; d.innerHTML='<span class="core-label">Core '+i+'</span><div class="core-bar-container"><div class="core-bar" id="core-bar-'+i+'"></div><span class="core-value" id="core-value-'+i+'">0%</span></div>'; c.appendChild(d);}); }
                    if(data.CPU.Cores){ data.CPU.Cores.forEach((load,i)=>{ const bar=document.getElementById('core-bar-'+i); const val=document.getElementById('core-value-'+i); if(bar&&val){ const r=Math.round(load); bar.style.height=r+'%'; val.textContent=r+'%'; } }); }}
                if(data.Memory){ const p=Math.round(data.Memory.UsedPercent); document.getElementById('memory-percent').textContent=p+'%'; document.getElementById('memory-bar').style.width=p+'%'; document.getElementById('memory-used').textContent=formatBytes(data.Memory.Used); document.getElementById('memory-total').textContent='/ '+formatBytes(data.Memory.Total); }
                const ssid=document.getElementById('wifi-ssid'); if(data.WiFi && data.WiFi.SSID){ ssid.textContent=data.WiFi.SSID; ssid.className='wifi-value wifi-connected'; let band=data.WiFi.Band; if(data.WiFi.Channel>0){ band+=' (Ch '+data.WiFi.Channel+')'; } document.getElementById('wifi-band').textContent=band; document.getElementById('wifi-type').textContent=wifiStandards[data.WiFi.Type]||data.WiFi.Type; document.getElementById('wifi-signal').textContent=data.WiFi.Signal+'%'; } else { ssid.textContent='未接続'; ssid.className='wifi-value wifi-disconnected'; document.getElementById('wifi-band').textContent='---'; document.getElementById('wifi-type').textContent='---'; document.getElementById('wifi-signal').textContent='---'; }
            }catch(e){ console.error('Update error:',e); }
        }
        let heightAdjusted = false;
        updateData().then(()=>{ adjustWindowHeight(); setTimeout(adjustWindowHeight, 300); heightAdjusted = true; });
        setInterval(updateData,2000);
        window.addEventListener('load', function() {
            if(!heightAdjusted){ setTimeout(adjustWindowHeight, 350); setTimeout(adjustWindowHeight, 1000); }
        });
    </script>
</body>
</html>`
}
