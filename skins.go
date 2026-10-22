package main

import (
	"archive/zip"
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"golang.org/x/sys/windows"
)

//go:embed all:skins/builtin
var builtinSkinFiles embed.FS

const (
	skinJSONLimit = 16 * 1024
	skinCSSLimit  = 256 * 1024
	skinZIPLimit  = 1024 * 1024
)

var (
	skinIDPattern      = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)
	skinCSSNamePattern = regexp.MustCompile(`^[A-Za-z0-9._-]+\.css$`)
	skinColorPattern   = regexp.MustCompile(`^#[A-Fa-f0-9]{6}$`)
	skinComments       = regexp.MustCompile(`(?s)/\*.*?\*/`)
	skinURLs           = regexp.MustCompile(`(?i)url\s*\(`)
	skinForbidden      = regexp.MustCompile(`(?i)</style|@import|expression\s*\(|javascript\s*:|-moz-binding|(?:^|[^a-z0-9_-])behavior\s*:`)
	builtinSkinIDs     = []string{"shukei", "fieldnote", "orbital", "paper-tape"}
	errSkinConflict    = errors.New("同じスキンが既にあります")
)

type skinManifest struct {
	Schema            int      `json:"schema"`
	ID                string   `json:"id"`
	Name              string   `json:"name"`
	Author            string   `json:"author,omitempty"`
	Version           string   `json:"version,omitempty"`
	Description       string   `json:"description,omitempty"`
	CSS               string   `json:"css,omitempty"`
	Swatch            []string `json:"swatch,omitempty"`
	StartupBackground struct {
		Dark  string `json:"dark,omitempty"`
		Light string `json:"light,omitempty"`
	} `json:"startupBackground,omitempty"`
}

type skinInfo struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Builtin     bool     `json:"builtin"`
	Author      string   `json:"author"`
	Version     string   `json:"version"`
	Description string   `json:"description"`
	Swatch      []string `json:"swatch"`
}

type loadedSkin struct {
	manifest skinManifest
	css      []byte
	folder   string
	builtin  bool
}

func (s loadedSkin) info() skinInfo {
	swatch := append([]string{}, s.manifest.Swatch...)
	return skinInfo{s.manifest.ID, s.manifest.Name, s.builtin, s.manifest.Author, s.manifest.Version, s.manifest.Description, swatch}
}

type skinLoadError struct {
	Folder  string `json:"folder"`
	Message string `json:"message"`
}

type skinCatalog struct {
	skins        []loadedSkin
	errors       []skinLoadError
	duplicateIDs map[string]bool
}

func (c skinCatalog) find(id string) (loadedSkin, bool) {
	for _, s := range c.skins {
		if s.manifest.ID == id {
			return s, true
		}
	}
	return loadedSkin{}, false
}

func (c skinCatalog) active(id string) string {
	if _, ok := c.find(id); ok {
		return id
	}
	return "default"
}

type skinStore struct {
	mu      sync.Mutex // スキャンと import/delete/copy を直列化する。
	builtin fs.FS
	dir     func() string
	openDir func(string) error
}

func skinDirectory() string {
	configMu.Lock()
	p := configPath
	configMu.Unlock()
	if p != "" {
		return filepath.Join(filepath.Dir(p), "skins")
	}
	return filepath.Join(appConfigDirectory(), "skins")
}

func embeddedSkinFS() fs.FS {
	f, err := fs.Sub(builtinSkinFiles, "skins/builtin")
	if err != nil {
		panic(err)
	}
	return f
}

var skins = &skinStore{builtin: embeddedSkinFS(), dir: skinDirectory, openDir: openSkinDirectory}

// openSkinDirectory はスキンフォルダをエクスプローラで開く。
// explorer.exe を HideWindow 付きで起動すると、開いたウィンドウまで非表示になる
// (押すたびに見えないウィンドウが溜まる)。ShellExecute で通常のウィンドウとして開く。
func openSkinDirectory(dir string) error {
	verb, err := windows.UTF16PtrFromString("open")
	if err != nil {
		return err
	}
	target, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return err
	}
	return windows.ShellExecute(0, verb, target, nil, nil, windows.SW_SHOWNORMAL)
}

func reservedSkinID(id string) bool {
	if id == "default" {
		return true
	}
	for _, b := range builtinSkinIDs {
		if id == b {
			return true
		}
	}
	return false
}

func validateSkinManifest(data []byte) (skinManifest, error) {
	var m skinManifest
	if len(data) > skinJSONLimit || !utf8.Valid(data) {
		return m, errors.New("skin.json のサイズまたは文字コードが不正です")
	}
	if err := json.Unmarshal(bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf}), &m); err != nil {
		return m, errors.New("skin.json を読み取れません")
	}
	if m.Schema != 1 {
		return m, errors.New("skin.json の schema は 1 にしてください")
	}
	if !skinIDPattern.MatchString(m.ID) || m.ID == "default" {
		return m, errors.New("skin.json の id が不正です")
	}
	for _, f := range []struct {
		value    string
		min, max int
	}{{m.Name, 1, 40}, {m.Author, 0, 100}, {m.Version, 0, 20}, {m.Description, 0, 200}} {
		n := utf8.RuneCountInString(f.value)
		if n < f.min || n > f.max {
			return m, errors.New("skin.json の文字数が不正です")
		}
	}
	if m.CSS == "" {
		m.CSS = "skin.css"
	}
	if !skinCSSNamePattern.MatchString(m.CSS) || filepath.Base(filepath.Clean(m.CSS)) != m.CSS {
		return m, errors.New("CSS ファイル名が不正です")
	}
	if m.Swatch != nil && len(m.Swatch) != 3 {
		return m, errors.New("swatch は色を 3 つ指定してください")
	}
	for _, color := range append(append([]string{}, m.Swatch...), m.StartupBackground.Dark, m.StartupBackground.Light) {
		if color != "" && !skinColorPattern.MatchString(color) {
			return m, errors.New("色は #RRGGBB で指定してください")
		}
	}
	for _, color := range m.Swatch {
		if color == "" {
			return m, errors.New("swatch の色が不正です")
		}
	}
	return m, nil
}

// CSS エスケープを検査用に復元し、コメント分割やエスケープによる回避も拒否する。
func decodeSkinCSSEscapes(s string) string {
	var out strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' {
			out.WriteByte(s[i])
			continue
		}
		i++
		if i >= len(s) {
			break
		}
		if s[i] == '\n' || s[i] == '\r' || s[i] == '\f' {
			continue
		}
		start := i
		for i < len(s) && i-start < 6 && strings.ContainsRune("0123456789abcdefABCDEF", rune(s[i])) {
			i++
		}
		if i > start {
			n, _ := strconv.ParseInt(s[start:i], 16, 32)
			out.WriteRune(rune(n))
			if i < len(s) && strings.ContainsRune(" \t\n\r\f", rune(s[i])) {
				continue
			}
			i--
		} else {
			out.WriteByte(s[i])
		}
	}
	return out.String()
}

func validateSkinCSS(data []byte) ([]byte, error) {
	if len(data) > skinCSSLimit || !utf8.Valid(data) {
		return nil, errors.New("CSS のサイズまたは文字コードが不正です")
	}
	data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})
	raw := string(data)
	for _, s := range []string{raw, skinComments.ReplaceAllString(raw, ""), skinComments.ReplaceAllString(decodeSkinCSSEscapes(raw), "")} {
		if skinForbidden.MatchString(s) {
			return nil, errors.New("CSS に許可されていない記述があります")
		}
		for _, loc := range skinURLs.FindAllStringIndex(s, -1) {
			tail := strings.TrimLeft(s[loc[1]:], " \t\n\r\f\"'")
			if !strings.HasPrefix(strings.ToLower(tail), "data:image/") {
				return nil, errors.New("CSS の url は data:image/ のみ使用できます")
			}
		}
	}
	return data, nil
}

func readSkinFile(fsys fs.FS, name string, limit int) ([]byte, error) {
	f, err := fsys.Open(name)
	if err != nil {
		return nil, errors.New("スキンのファイルを開けません")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > int64(limit) {
		return nil, errors.New("スキンのファイルサイズまたは形式が不正です")
	}
	b, err := io.ReadAll(io.LimitReader(f, int64(limit)+1))
	if err != nil || len(b) > limit {
		return nil, errors.New("スキンのファイルを読み取れません")
	}
	return b, nil
}

func loadSkin(fsys fs.FS) (loadedSkin, error) {
	var s loadedSkin
	b, err := readSkinFile(fsys, "skin.json", skinJSONLimit)
	if err != nil {
		return s, err
	}
	s.manifest, err = validateSkinManifest(b)
	if err != nil {
		return s, err
	}
	b, err = readSkinFile(fsys, s.manifest.CSS, skinCSSLimit)
	if err != nil {
		return s, err
	}
	s.css, err = validateSkinCSS(b)
	return s, err
}

// folder は ReadDir の直接の子だけを許可し、パス区切り・特殊パスを拒否する。
func safeSkinFolder(folder string) bool {
	return folder != "" && folder != "." && folder != ".." && !strings.ContainsAny(folder, `/\:`) && filepath.Base(filepath.Clean(folder)) == folder && !strings.HasSuffix(folder, ".") && !strings.HasSuffix(folder, " ")
}

// scan は mu を保持した状態で呼ぶ。重複した利用者 id は両方を一覧から除く。
func (s *skinStore) scan() skinCatalog {
	c := skinCatalog{errors: []skinLoadError{}, duplicateIDs: map[string]bool{}}
	for _, id := range builtinSkinIDs {
		f, err := fs.Sub(s.builtin, id)
		if err != nil {
			c.errors = append(c.errors, skinLoadError{id, "組み込みスキンを読み取れません"})
			continue
		}
		v, err := loadSkin(f)
		if err != nil || v.manifest.ID != id {
			msg := "組み込みスキンの id が不正です"
			if err != nil {
				msg = err.Error()
			}
			c.errors = append(c.errors, skinLoadError{id, msg})
			continue
		}
		v.builtin = true
		c.skins = append(c.skins, v)
	}
	entries, err := os.ReadDir(s.dir())
	if err != nil {
		if !os.IsNotExist(err) {
			c.errors = append(c.errors, skinLoadError{"skins", "スキンフォルダを読み取れません"})
		}
		return c
	}
	users := []loadedSkin{}
	counts := map[string]int{}
	for _, e := range entries {
		if e.Type()&os.ModeSymlink != 0 {
			c.errors = append(c.errors, skinLoadError{e.Name(), "リンクは使用できません"})
			continue
		}
		if !e.IsDir() {
			continue
		}
		if !safeSkinFolder(e.Name()) {
			c.errors = append(c.errors, skinLoadError{e.Name(), "フォルダ名が不正です"})
			continue
		}
		// os.Root は CSS のリンクがフォルダ外を指す場合も拒否する。
		root, err := os.OpenRoot(filepath.Join(s.dir(), filepath.Clean(e.Name())))
		if err != nil {
			c.errors = append(c.errors, skinLoadError{e.Name(), "フォルダを開けません"})
			continue
		}
		v, err := loadSkin(root.FS())
		_ = root.Close()
		if err == nil && reservedSkinID(v.manifest.ID) {
			err = errors.New("予約済みの id は使用できません")
		}
		if err != nil {
			c.errors = append(c.errors, skinLoadError{e.Name(), err.Error()})
			continue
		}
		v.folder = e.Name()
		users = append(users, v)
		counts[v.manifest.ID]++
	}
	sort.Slice(users, func(i, j int) bool { return users[i].manifest.ID < users[j].manifest.ID })
	for _, v := range users {
		if counts[v.manifest.ID] > 1 {
			c.duplicateIDs[v.manifest.ID] = true
			c.errors = append(c.errors, skinLoadError{v.folder, "skin.json の id が重複しています"})
			continue
		}
		c.skins = append(c.skins, v)
	}
	return c
}

func (s *skinStore) active(id string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.scan().active(id)
}

func (s *skinStore) setActive(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !skinIDPattern.MatchString(id) {
		return errors.New("スキンの id が不正です")
	}
	if id != "default" {
		if _, ok := s.scan().find(id); !ok {
			return errors.New("スキンが見つかりません")
		}
	}
	mutateConfig(func(c *Config) { c.Skin = id })
	return nil
}

func readSkinZIP(data []byte) (loadedSkin, error) {
	var empty loadedSkin
	if len(data) > skinZIPLimit {
		return empty, errors.New("zip は 1 MiB 以下にしてください")
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil || len(zr.File) > 20 {
		return empty, errors.New("zip の形式またはエントリ数が不正です")
	}
	files := map[string][]byte{}
	total := 0
	for _, f := range zr.File {
		name := f.Name
		if strings.ContainsAny(name, `\:`) || strings.HasPrefix(name, "/") || filepath.IsAbs(name) {
			return empty, errors.New("zip のパスが不正です")
		}
		for _, part := range strings.Split(name, "/") {
			if part == ".." || part == "." {
				return empty, errors.New("zip のパスが不正です")
			}
		}
		if name == "" || path.Clean(name) != strings.TrimSuffix(name, "/") || f.Mode()&os.ModeSymlink != 0 {
			return empty, errors.New("zip のエントリが不正です")
		}
		if f.UncompressedSize64 > uint64(skinZIPLimit-total) {
			return empty, errors.New("zip の展開サイズが大きすぎます")
		}
		if _, ok := files[name]; ok {
			return empty, errors.New("zip 内のファイル名が重複しています")
		}
		r, err := f.Open()
		if err != nil {
			return empty, errors.New("zip を読み取れません")
		}
		b, readErr := io.ReadAll(io.LimitReader(r, int64(skinZIPLimit-total)+1))
		_ = r.Close()
		if readErr != nil || len(b) > skinZIPLimit-total {
			return empty, errors.New("zip の展開サイズまたは内容が不正です")
		}
		total += len(b)
		files[name] = b
	}
	prefix := ""
	if _, ok := files["skin.json"]; !ok {
		for name := range files {
			parts := strings.Split(strings.TrimSuffix(name, "/"), "/")
			if len(parts) == 0 || parts[0] == "" {
				return empty, errors.New("zip の構成が不正です")
			}
			p := parts[0] + "/"
			if prefix != "" && prefix != p {
				return empty, errors.New("トップレベルフォルダは 1 つにしてください")
			}
			prefix = p
		}
	}
	m, err := validateSkinManifest(files[prefix+"skin.json"])
	if err != nil {
		return empty, err
	}
	css, ok := files[prefix+m.CSS]
	if !ok {
		return empty, errors.New("CSS ファイルがありません")
	}
	css, err = validateSkinCSS(css)
	return loadedSkin{manifest: m, css: css}, err
}

// save は検証済みの二つのファイルだけを書き込む。既存フォルダは一旦退避し、失敗時に戻す。
func (s *skinStore) save(v loadedSkin, folder string, overwrite bool) error {
	if !skinIDPattern.MatchString(v.manifest.ID) || !safeSkinFolder(folder) {
		return errors.New("スキンの保存先が不正です")
	}
	dir := filepath.Clean(s.dir())
	if err := os.MkdirAll(dir, 0755); err != nil {
		return errors.New("スキンフォルダを作成できません")
	}
	dest := filepath.Join(dir, filepath.Clean(folder))
	if rel, err := filepath.Rel(dir, dest); err != nil || rel != folder {
		return errors.New("スキンの保存先が不正です")
	}
	tmp, err := os.MkdirTemp(dir, ".import-")
	if err != nil {
		return errors.New("スキンの一時フォルダを作成できません")
	}
	defer os.RemoveAll(tmp)
	b, err := json.MarshalIndent(v.manifest, "", "  ")
	if err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(tmp, "skin.json"), b, 0644); err != nil {
		return errors.New("skin.json を保存できません")
	}
	if err = os.WriteFile(filepath.Join(tmp, v.manifest.CSS), v.css, 0644); err != nil {
		return errors.New("CSS を保存できません")
	}
	backup := ""
	if _, err := os.Lstat(dest); err == nil {
		if !overwrite {
			return errSkinConflict
		}
		backup, err = os.MkdirTemp(dir, ".backup-")
		if err != nil {
			return errors.New("スキンを退避できません")
		}
		_ = os.Remove(backup)
		if err := os.Rename(dest, backup); err != nil {
			return errors.New("スキンを退避できません")
		}
	} else if !os.IsNotExist(err) {
		return errors.New("保存先を確認できません")
	}
	if err := os.Rename(tmp, dest); err != nil {
		if backup != "" {
			_ = os.Rename(backup, dest)
		}
		return errors.New("スキンを保存できません")
	}
	if backup != "" {
		_ = os.RemoveAll(backup)
	}
	return nil
}

func (s *skinStore) importZIP(data []byte, overwrite bool) (loadedSkin, error) {
	v, err := readSkinZIP(data)
	if err != nil {
		return v, err
	}
	if reservedSkinID(v.manifest.ID) {
		return v, errors.New("予約済みの id は使用できません")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	folder := v.manifest.ID
	catalog := s.scan()
	canOverwrite := false
	if catalog.duplicateIDs[v.manifest.ID] {
		return v, errors.New("重複したスキンの id を修正してください")
	}
	if old, ok := catalog.find(v.manifest.ID); ok {
		if !overwrite {
			return v, errSkinConflict
		}
		folder = old.folder
		canOverwrite = true
	} else if overwrite {
		// 読み込みに失敗している(errors 側の)同名フォルダは、一覧に出ないため id では指せない。
		// 上書きで直せるようにする(でないと 409 のまま手作業で消すしかなくなる)。
		for _, e := range catalog.errors {
			if e.Folder == folder {
				canOverwrite = true
			}
		}
	}
	return v, s.save(v, folder, canOverwrite)
}

func (s *skinStore) copyBuiltin(id string) (loadedSkin, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.scan().find(id)
	if !ok || !v.builtin {
		return v, errors.New("組み込みスキンを指定してください")
	}
	oldID := v.manifest.ID
	v.manifest.ID += "-copy"
	v.manifest.Name += " (コピー)"
	v.builtin = false
	if !skinIDPattern.MatchString(v.manifest.ID) || utf8.RuneCountInString(v.manifest.Name) > 40 {
		return v, errors.New("複製後の id または名前が長すぎます")
	}
	catalog := s.scan()
	if _, ok := catalog.find(v.manifest.ID); ok || catalog.duplicateIDs[v.manifest.ID] {
		return v, errSkinConflict
	}
	v.css = bytes.ReplaceAll(v.css, []byte(`data-skin="`+oldID+`"`), []byte(`data-skin="`+v.manifest.ID+`"`))
	return v, s.save(v, v.manifest.ID, false)
}

func (s *skinStore) delete(id string) error {
	if !skinIDPattern.MatchString(id) || reservedSkinID(id) {
		return errors.New("利用者スキンを指定してください")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.scan().find(id)
	if !ok || v.builtin || !safeSkinFolder(v.folder) {
		return errors.New("利用者スキンが見つかりません")
	}
	dir := filepath.Clean(s.dir())
	target := filepath.Join(dir, filepath.Clean(v.folder))
	if rel, err := filepath.Rel(dir, target); err != nil || rel != v.folder {
		return errors.New("削除先が不正です")
	}
	if err := os.RemoveAll(target); err != nil {
		return errors.New("スキンを削除できません")
	}
	mutateConfig(func(c *Config) {
		if c.Skin == id {
			c.Skin = "default"
		}
	})
	return nil
}

func skinJSONResponse(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(value)
}

func skinResult(w http.ResponseWriter, v loadedSkin, err error) {
	if err != nil {
		code := http.StatusBadRequest
		if errors.Is(err, errSkinConflict) {
			code = http.StatusConflict
		}
		http.Error(w, err.Error(), code)
		return
	}
	skinJSONResponse(w, struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}{v.manifest.ID, v.manifest.Name})
}

func (s *skinStore) register(mux *http.ServeMux) {
	register := func(method, url string, handler http.HandlerFunc) {
		mux.HandleFunc(url, func(w http.ResponseWriter, r *http.Request) {
			if r.Method != method {
				w.Header().Set("Allow", method)
				http.Error(w, "許可されていないメソッドです", http.StatusMethodNotAllowed)
				return
			}
			handler(w, r)
		})
	}
	register(http.MethodGet, "/api/skins", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		c := s.scan()
		list := []skinInfo{{ID: "default", Name: "デフォルト", Builtin: true, Description: "現行のデザイン", Swatch: []string{}}}
		for _, v := range c.skins {
			list = append(list, v.info())
		}
		skinJSONResponse(w, struct {
			Active string          `json:"active"`
			Dir    string          `json:"dir"`
			Skins  []skinInfo      `json:"skins"`
			Errors []skinLoadError `json:"errors"`
		}{c.active(snapshotConfig().Skin), s.dir(), list, c.errors})
	})
	register(http.MethodGet, "/api/skin/css", func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		if !skinIDPattern.MatchString(id) {
			http.Error(w, "スキンが見つかりません", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		if id == "default" {
			return
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		v, ok := s.scan().find(id)
		if !ok {
			http.Error(w, "スキンが見つかりません", http.StatusNotFound)
			return
		}
		_, _ = w.Write(v.css)
	})
	register(http.MethodPost, "/api/skins/import", func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(http.MaxBytesReader(w, r.Body, skinZIPLimit))
		if err != nil {
			http.Error(w, "zip は 1 MiB 以下にしてください", http.StatusBadRequest)
			return
		}
		v, err := s.importZIP(b, r.URL.Query().Get("overwrite") == "1")
		skinResult(w, v, err)
	})
	register(http.MethodPost, "/api/skins/delete", func(w http.ResponseWriter, r *http.Request) {
		if err := s.delete(r.URL.Query().Get("id")); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	register(http.MethodPost, "/api/skins/copy", func(w http.ResponseWriter, r *http.Request) {
		v, err := s.copyBuiltin(r.URL.Query().Get("id"))
		skinResult(w, v, err)
	})
	register(http.MethodPost, "/api/skins/opendir", func(w http.ResponseWriter, r *http.Request) {
		if err := os.MkdirAll(s.dir(), 0755); err != nil {
			http.Error(w, "スキンフォルダを作成できません", http.StatusInternalServerError)
			return
		}
		if err := s.openDir(s.dir()); err != nil {
			http.Error(w, "スキンフォルダを開けません", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
}

func (s *skinStore) startupBackground(theme, id string, systemLight bool) rgb {
	base := startupBackground(theme, systemLight)
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.scan().find(id)
	if !ok {
		return base
	}
	color := v.manifest.StartupBackground.Dark
	if base == startupBgLight {
		color = v.manifest.StartupBackground.Light
	}
	if !skinColorPattern.MatchString(color) {
		return base
	}
	n, err := strconv.ParseUint(color[1:], 16, 24)
	if err != nil {
		return base
	}
	return rgb{uint8(n >> 16), uint8(n >> 8), uint8(n)}
}

func widgetStartupBackground(theme, id string, systemLight bool) rgb {
	return skins.startupBackground(theme, id, systemLight)
}
