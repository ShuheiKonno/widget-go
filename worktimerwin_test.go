package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPlaceBeside(t *testing.T) {
	work := RECT{Left: 0, Top: 0, Right: 1920, Bottom: 1080}
	cases := []struct {
		name          string
		anchor        RECT
		wantX, wantY  int32
		wantW, wantH  int32
		inW, inH, gap int32
		work          RECT
	}{
		{name: "左に余裕があれば、ウィジェットの左", anchor: RECT{Left: 1500, Top: 100, Right: 1730, Bottom: 700}, inW: 300, inH: 400, gap: 8, work: work, wantX: 1500 - 8 - 300, wantY: 100, wantW: 300, wantH: 400},
		{name: "左に入らなければ右", anchor: RECT{Left: 100, Top: 100, Right: 330, Bottom: 700}, inW: 300, inH: 400, gap: 8, work: work, wantX: 338, wantY: 100, wantW: 300, wantH: 400},
		{name: "左にちょうど入る", anchor: RECT{Left: 308, Top: 0, Right: 538, Bottom: 600}, inW: 300, inH: 400, gap: 8, work: work, wantX: 0, wantY: 0, wantW: 300, wantH: 400},
		{name: "どちらにも入らなければ、作業領域の端に寄せる", anchor: RECT{Left: 100, Top: 100, Right: 330, Bottom: 700}, inW: 300, inH: 400, gap: 8, work: RECT{Left: 0, Top: 0, Right: 500, Bottom: 1080}, wantX: 200, wantY: 100, wantW: 300, wantH: 400},
		{name: "縦は作業領域の下端に収める", anchor: RECT{Left: 1500, Top: 900, Right: 1730, Bottom: 1070}, inW: 300, inH: 400, gap: 8, work: work, wantX: 1192, wantY: 680, wantW: 300, wantH: 400},
		{name: "ウィジェットが作業領域の上にはみ出していても、上端に収める", anchor: RECT{Left: 1500, Top: -50, Right: 1730, Bottom: 300}, inW: 300, inH: 400, gap: 8, work: work, wantX: 1192, wantY: 0, wantW: 300, wantH: 400},
		{name: "作業領域より大きいときは縮める", anchor: RECT{Left: 100, Top: 100, Right: 330, Bottom: 700}, inW: 300, inH: 400, gap: 8, work: RECT{Left: 0, Top: 0, Right: 250, Bottom: 300}, wantX: 0, wantY: 0, wantW: 250, wantH: 300},
		{name: "作業領域が左上原点でなくても使える", anchor: RECT{Left: 2400, Top: 200, Right: 2630, Bottom: 800}, inW: 300, inH: 400, gap: 8, work: RECT{Left: 1920, Top: 40, Right: 3840, Bottom: 1120}, wantX: 2400 - 8 - 300, wantY: 200, wantW: 300, wantH: 400},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			x, y, w, h := placeBeside(c.work, c.anchor, c.inW, c.inH, c.gap)
			if x != c.wantX || y != c.wantY || w != c.wantW || h != c.wantH {
				t.Errorf("got (%d,%d) %dx%d, want (%d,%d) %dx%d", x, y, w, h, c.wantX, c.wantY, c.wantW, c.wantH)
			}
		})
	}
}

func TestFitWindowHeight(t *testing.T) {
	cases := []struct {
		name       string
		cssH       int32
		scale      float64
		ncH        int32
		minH, maxH int32
		want       int32
	}{
		{"内容 + 枠", 280, 1.0, 40, 160, 350, 320},
		{"拡大率をかける", 280, 1.25, 50, 200, 438, 400},
		{"上限を超えない", 400, 1.0, 40, 160, 350, 350},
		{"下限を割らない", 50, 1.0, 40, 160, 350, 160},
	}
	for _, c := range cases {
		if got := fitWindowHeight(c.cssH, c.scale, c.ncH, c.minH, c.maxH); got != c.want {
			t.Errorf("%s: got %d, want %d", c.name, got, c.want)
		}
	}
}

// 勤務時間の区画の断片は、本体と入力ウィンドウの両方に差し込まれ、置き換え前の目印が残らない。
func TestWorkPartsExpanded(t *testing.T) {
	const token = "tok123"
	pages := map[string]string{"本体": getHTML(token), "入力ウィンドウ": getWorkTimerHTML(token)}
	for name, html := range pages {
		for _, marker := range []string{"__WORK_", "__WIDGET_TOKEN__"} {
			if strings.Contains(html, marker) {
				t.Errorf("%s: %q が残っている", name, marker)
			}
		}
		for _, want := range []string{`id="workSection"`, `id="work-start-input"`, `window.__workChanged`, "function applyWorkTimer", ".work-fill.paused", "const WIDGET_TOKEN = '" + token + "'"} {
			if !strings.Contains(html, want) {
				t.Errorf("%s: %q が無い", name, want)
			}
		}
	}
	// 設定ウィンドウでのテーマ変更を、開いている入力ウィンドウにも反映する（Go 側が window.__settingsChanged を呼ぶ）
	if !strings.Contains(pages["入力ウィンドウ"], "window.__settingsChanged") {
		t.Error("入力ウィンドウに window.__settingsChanged が無い（テーマの変更が反映されない）")
	}
	// 本体は設定を読むまで区画を出さない。入力ウィンドウは最初から出す。
	if !strings.Contains(pages["本体"], `class="section work-section is-hidden"`) {
		t.Error("本体の区画は、最初は is-hidden のはず")
	}
	if strings.Contains(pages["入力ウィンドウ"], "is-hidden") {
		t.Error("入力ウィンドウの区画は、最初から出すはず")
	}
	// 断片に、本体のウィンドウ制御（ドラッグ・リサイズ・保存）を呼ぶ処理を入れない
	for _, bad := range []string{"/api/resize", "/api/persistwindow", "/api/dragstart", "/api/ready'"} {
		if strings.Contains(workSectionJS, bad) {
			t.Errorf("断片の JS が %q を呼んでいる", bad)
		}
	}
}

// /worktimer は "/" や /settings と同じく、トークンなしで読め、CSP が付く。/api/* はトークンが要る。
func TestWorkTimerPageServedLikeIndex(t *testing.T) {
	const token = "testtoken123"
	mux := http.NewServeMux()
	mux.HandleFunc("/worktimer", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(getWorkTimerHTML(token))) })
	mux.HandleFunc("/api/openworktimer", func(w http.ResponseWriter, r *http.Request) {})
	srv := securityMiddleware(mux, token, 4321)

	withPageKey(t)
	req := httptest.NewRequest(http.MethodGet, "/worktimer?k="+pageAccessKey, nil)
	req.Host = "127.0.0.1:4321"
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("/worktimer: code=%d", rec.Code)
	}
	if csp := rec.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "nonce-"+token) {
		t.Errorf("CSP に nonce が無い: %q", csp)
	}
	if !strings.Contains(rec.Body.String(), "const WIDGET_TOKEN = '"+token+"'") {
		t.Error("トークンが HTML に入っていない")
	}

	req = httptest.NewRequest(http.MethodGet, "/api/openworktimer", nil)
	req.Host = "127.0.0.1:4321"
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("トークン無しの /api/* は拒否されるはず: code=%d", rec.Code)
	}
}

// UI のない環境（テスト）では、通知は何もせずに戻る。
func TestNotifyWorkTimerChangedWithoutUI(t *testing.T) {
	notifyWorkTimerChanged()
}
