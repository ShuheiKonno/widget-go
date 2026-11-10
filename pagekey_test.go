package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// withPageKey はページ用のキーをテスト用の値にする(終了時に元へ戻す)。
func withPageKey(t *testing.T) {
	t.Helper()
	old := pageAccessKey
	pageAccessKey = "test-page-key"
	t.Cleanup(func() { pageAccessKey = old })
}

func getPage(h http.Handler, target string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.Host = "127.0.0.1:4321"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// ページはキーが無ければ読めない。トークン(HTML に入る)を、ほかのユーザーや他のプロセスに渡さないため。
func TestPagesRequireAccessKey(t *testing.T) {
	withPageKey(t)
	const token = "page-key-token"
	h := securityMiddleware(newServerMux(token), token, 4321)

	for _, path := range []string{"/", "/settings", "/worktimer"} {
		for _, tc := range []struct {
			name, query string
			want        int
		}{
			{"キーなし", "", http.StatusForbidden},
			{"空のキー", "?k=", http.StatusForbidden},
			{"違うキー", "?k=wrong", http.StatusForbidden},
			{"正しいキー", "?k=" + pageAccessKey, http.StatusOK},
		} {
			rec := getPage(h, path+tc.query)
			if rec.Code != tc.want {
				t.Errorf("%s %s: code=%d, want %d", path, tc.name, rec.Code, tc.want)
			}
			if tc.want == http.StatusForbidden && strings.Contains(rec.Body.String(), token) {
				t.Errorf("%s %s: 拒否したのにトークンが本文に入っている", path, tc.name)
			}
		}
	}
}

// キーが未設定(空)のときは、空のキーでも通さない(設定し忘れで開放しない)。
func TestPagesDeniedWhenAccessKeyUnset(t *testing.T) {
	old := pageAccessKey
	pageAccessKey = ""
	t.Cleanup(func() { pageAccessKey = old })
	h := securityMiddleware(newServerMux("tok"), "tok", 4321)
	if rec := getPage(h, "/?k="); rec.Code != http.StatusForbidden {
		t.Errorf("キー未設定なのに通した: code=%d", rec.Code)
	}
}

// API は引き続きトークンのヘッダが要る。ページのキーでは通らない。
func TestAPIStillNeedsTokenNotPageKey(t *testing.T) {
	withPageKey(t)
	h := securityMiddleware(newServerMux("api-token"), "api-token", 4321)
	if rec := getPage(h, "/api/settings?k="+pageAccessKey); rec.Code != http.StatusForbidden {
		t.Errorf("ページのキーで API を通した: code=%d", rec.Code)
	}
}

func TestPageURLCarriesKey(t *testing.T) {
	withPageKey(t)
	if got := pageURL(1234, "/settings"); got != "http://127.0.0.1:1234/settings?k=test-page-key" {
		t.Errorf("pageURL = %q", got)
	}
}
