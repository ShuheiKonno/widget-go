package main

import (
	"encoding/json"
	"net/http"
	"os"

	"widget-go/shortcut"
)

// shortcutService は、設定画面のスタートメニュー / スタートアップのチェックに対応する HTTP ハンドラ。
// 保存先と実行ファイルのパスは差し替えられる（テスト用）。
type shortcutService struct {
	locations func() (shortcut.Locations, error)
	exe       func() (string, error)
}

func defaultShortcutService() *shortcutService {
	return &shortcutService{locations: shortcut.DefaultLocations, exe: os.Executable}
}

// shortcutsResponse は /api/shortcuts と /api/shortcuts/set の応答。
type shortcutsResponse struct {
	Exe       string         `json:"exe"`
	StartMenu shortcut.State `json:"startMenu"`
	Startup   shortcut.State `json:"startup"`
	Error     string         `json:"error,omitempty"`
}

func (s *shortcutService) status() (shortcutsResponse, error) {
	var res shortcutsResponse
	l, err := s.locations()
	if err != nil {
		return res, err
	}
	exe, err := s.exe()
	if err != nil {
		return res, err
	}
	res.Exe = exe
	if res.StartMenu, err = shortcut.Status(l, shortcut.StartMenu, exe); err != nil {
		return res, err
	}
	if res.Startup, err = shortcut.Status(l, shortcut.Startup, exe); err != nil {
		return res, err
	}
	return res, nil
}

func writeShortcutsJSON(w http.ResponseWriter, res shortcutsResponse) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(res)
}

// handleStatus は /api/shortcuts（GET）。ファイルの有無から現在の状態を返す。
func (s *shortcutService) handleStatus(w http.ResponseWriter, r *http.Request) {
	res, err := s.status()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeShortcutsJSON(w, res)
}

// handleSet は /api/shortcuts/set?kind=startmenu|startup&enable=true|false（POST）。
// true でショートカットを保存し（あれば上書き）、false で削除する。成功したら最新の状態を返す。
func (s *shortcutService) handleSet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	kind := shortcut.Kind(r.URL.Query().Get("kind"))
	if kind != shortcut.StartMenu && kind != shortcut.Startup {
		http.Error(w, "invalid kind", http.StatusBadRequest)
		return
	}
	var enable bool
	switch r.URL.Query().Get("enable") {
	case "true":
		enable = true
	case "false":
		enable = false
	default:
		http.Error(w, "invalid enable", http.StatusBadRequest)
		return
	}
	l, err := s.locations()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if enable {
		// 内側の := で err を隠すと、Enable のエラーが外の err に届かず、失敗が成功として返ってしまう。
		exe, exeErr := s.exe()
		if exeErr != nil {
			http.Error(w, exeErr.Error(), http.StatusInternalServerError)
			return
		}
		err = shortcut.Enable(l, kind, exe)
	} else {
		err = shortcut.Disable(l, kind)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	res, err := s.status()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeShortcutsJSON(w, res)
}
