package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"widget-go/sysinfo"
)

func TestPowerModeHandler(t *testing.T) {
	win32Err := fmt.Errorf("PowerSetActiveOverlayScheme: %w", errors.New("この要求はサポートされていません。"))
	newState := &sysinfo.PowerModeInfo{PlanOK: true, Active: sysinfo.PowerModeBest}

	for _, tc := range []struct {
		name        string
		query       string
		setErr      error
		wantStatus  int
		wantRefresh bool
		wantBody    string // 部分一致
	}{
		{"成功", "?mode=best", nil, http.StatusOK, true, `"active":"best"`},
		{"許可リスト外", "?mode=ultimate", fmt.Errorf("%w: %q", sysinfo.ErrPowerModeInvalid, "ultimate"), http.StatusBadRequest, false, "invalid mode"},
		{"mode なし", "", fmt.Errorf("%w: %q", sysinfo.ErrPowerModeInvalid, ""), http.StatusBadRequest, false, "invalid mode"},
		{"設定で OFF", "?mode=best", errPowerModeDisabled, http.StatusForbidden, false, "OFF"},
		{"バランス以外のプラン", "?mode=best", sysinfo.ErrPowerPlanNotBalanced, http.StatusConflict, false, "バランス"},
		{"API が使えない", "?mode=best", sysinfo.ErrPowerModeUnsupported, http.StatusServiceUnavailable, false, "利用できません"},
		{"設定に失敗", "?mode=best", win32Err, http.StatusInternalServerError, false, "PowerSetActiveOverlayScheme"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var gotMode string
			refreshed := false
			h := powerModeHandler(
				func(mode string) error { gotMode = mode; return tc.setErr },
				func() *sysinfo.PowerModeInfo { refreshed = true; return newState },
			)
			rec := httptest.NewRecorder()
			h(rec, httptest.NewRequest(http.MethodGet, "/api/powermode"+tc.query, nil))

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d (body %q)", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if refreshed != tc.wantRefresh {
				t.Fatalf("refresh 呼び出し = %v, want %v", refreshed, tc.wantRefresh)
			}
			if !strings.Contains(rec.Body.String(), tc.wantBody) {
				t.Fatalf("body %q に %q が含まれない", rec.Body.String(), tc.wantBody)
			}
			// クエリの mode は加工せずそのまま検証側へ渡す（許可リストは SetPowerMode 側）。
			if want := strings.TrimPrefix(tc.query, "?mode="); gotMode != want {
				t.Fatalf("set に渡った mode = %q, want %q", gotMode, want)
			}
		})
	}
}

// 電源モードの切替は既定 OFF。OS の電源状態を書き換える機能なので、利用者が ON にするまで何もしない。
// 設定ファイルが無い場合も、キーが無い旧形式（v2.1.1 以前）の config.json でも OFF のまま。
func TestPowerModeDefaultsOff(t *testing.T) {
	if defaultConfig().PowerModeEnabled {
		t.Error("defaultConfig().PowerModeEnabled = true, want false")
	}
	withConfigFile(t, "")
	if snapshotConfig().PowerModeEnabled {
		t.Error("設定ファイルが無いのに ON")
	}
	withConfigFile(t, configV180)
	if snapshotConfig().PowerModeEnabled {
		t.Error("キーが無い旧形式の config.json で ON になった")
	}
}

func TestLoadConfigPowerModeExplicitValuesAreKept(t *testing.T) {
	withConfigFile(t, `{"theme": "dark", "powerModeEnabled": true}`)
	if !snapshotConfig().PowerModeEnabled {
		t.Error("PowerModeEnabled = false, want true (明示的な true を尊重する)")
	}
	withConfigFile(t, `{"theme": "dark", "powerModeEnabled": false}`)
	if snapshotConfig().PowerModeEnabled {
		t.Error("PowerModeEnabled = true, want false (明示的な false を尊重する)")
	}
}

func TestSetPowerModeEnabled(t *testing.T) {
	path := withConfigFile(t, "")
	if snapshotConfig().PowerModeEnabled {
		t.Fatal("既定が ON になっている")
	}
	if err := setPowerModeEnabled("true"); err != nil {
		t.Fatal(err)
	}
	if !snapshotConfig().PowerModeEnabled {
		t.Fatal("true を保存したのに OFF のまま")
	}
	// 保存された JSON にキーが書かれ、再読み込みしても ON のまま
	data, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(data), `"powerModeEnabled": true`) {
		t.Fatalf("config.json に powerModeEnabled=true が無い: %v\n%s", err, data)
	}
	loadConfig()
	if !snapshotConfig().PowerModeEnabled {
		t.Fatal("再読み込みで OFF に戻った")
	}
	// 不正な値は拒否し、設定は変えない
	for _, v := range []string{"", "1", "TRUE", "yes", "on"} {
		if err := setPowerModeEnabled(v); err == nil {
			t.Errorf("setPowerModeEnabled(%q) がエラーにならない", v)
		}
	}
	if !snapshotConfig().PowerModeEnabled {
		t.Error("不正な値で設定が変わった")
	}
	// OFF に戻せる
	if err := setPowerModeEnabled("false"); err != nil || snapshotConfig().PowerModeEnabled {
		t.Fatalf("false を保存できない: %v", err)
	}
}

// OFF の間は、電源モードに書き込まず・読み取りもしない（Windows の設定・制御のまま）。
func TestPowerModeOffDoesNotTouchPower(t *testing.T) {
	withConfigFile(t, `{"powerModeEnabled": false}`)

	// 切替は Win32 を呼ぶ前に拒否する（許可リスト内の mode でも）
	if err := setPowerModeIfEnabled(sysinfo.PowerModeBest); !errors.Is(err, errPowerModeDisabled) {
		t.Fatalf("err = %v, want errPowerModeDisabled", err)
	}

	// 状態の取得も行わず、古い状態は捨てる（UI がスライダーを隠せる）
	cacheMu.Lock()
	old := cachedPower
	cachedPower = &sysinfo.PowerModeInfo{PlanOK: true, Active: sysinfo.PowerModeBest}
	cacheMu.Unlock()
	t.Cleanup(func() { cacheMu.Lock(); cachedPower = old; cacheMu.Unlock() })

	if got := refreshPowerMode(); got != nil {
		t.Fatalf("refreshPowerMode() = %+v, want nil", *got)
	}
	cacheMu.RLock()
	defer cacheMu.RUnlock()
	if cachedPower != nil {
		t.Fatal("OFF なのにキャッシュに電源モードが残っている")
	}
}

// 取得できない環境では null を返し、UI が行を隠せるようにする。
func TestPowerModeHandlerNullState(t *testing.T) {
	h := powerModeHandler(
		func(string) error { return nil },
		func() *sysinfo.PowerModeInfo { return nil },
	)
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodGet, "/api/powermode?mode=best", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var v *sysinfo.PowerModeInfo
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil || v != nil {
		t.Fatalf("body %q は null のはず (err=%v)", rec.Body.String(), err)
	}
}

// /api/powermode は他の /api と同じくセッショントークン無しでは到達できない（OS の状態を書き換えるため）。
func TestPowerModeRequiresToken(t *testing.T) {
	const token = "test-token"
	const port = 12345
	called := false
	mux := http.NewServeMux()
	mux.HandleFunc("/api/powermode", powerModeHandler(
		func(string) error { called = true; return nil },
		func() *sysinfo.PowerModeInfo { return nil },
	))
	srv := securityMiddleware(mux, token, port)
	host := fmt.Sprintf("127.0.0.1:%d", port)

	for _, tc := range []struct {
		name       string
		host       string
		token      string
		wantStatus int
		wantCalled bool
	}{
		{"トークン無し", host, "", http.StatusForbidden, false},
		{"誤ったトークン", host, "wrong", http.StatusForbidden, false},
		{"Host が不一致（DNS rebinding）", "evil.example:12345", token, http.StatusForbidden, false},
		{"正しいトークン", host, token, http.StatusOK, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			called = false
			req := httptest.NewRequest(http.MethodGet, "/api/powermode?mode=best", nil)
			req.Host = tc.host
			if tc.token != "" {
				req.Header.Set("X-Widget-Token", tc.token)
			}
			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, req)
			if rec.Code != tc.wantStatus || called != tc.wantCalled {
				t.Fatalf("status=%d called=%v, want %d / %v", rec.Code, called, tc.wantStatus, tc.wantCalled)
			}
		})
	}
}
