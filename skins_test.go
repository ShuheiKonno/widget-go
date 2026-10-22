package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
)

func skinTestManifest(id string) skinManifest {
	m := skinManifest{Schema: 1, ID: id, Name: "テスト", CSS: "skin.css"}
	m.StartupBackground.Dark = "#123456"
	m.StartupBackground.Light = "#abcdef"
	return m
}

func skinTestJSON(t *testing.T, m skinManifest) []byte {
	t.Helper()
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func newTestSkinStore(t *testing.T) *skinStore {
	t.Helper()
	fixture := fstest.MapFS{}
	for _, id := range builtinSkinIDs {
		fixture[id+"/skin.json"] = &fstest.MapFile{Data: skinTestJSON(t, skinTestManifest(id))}
		fixture[id+"/skin.css"] = &fstest.MapFile{Data: []byte(`body[data-skin="` + id + `"] {color:red} body[data-skin="` + id + `"].theme-light {color:blue}`)}
	}
	dir := filepath.Join(t.TempDir(), "skins")
	return &skinStore{builtin: fixture, dir: func() string { return dir }, openDir: func(string) error { return nil }}
}

func writeTestSkin(t *testing.T, s *skinStore, folder, id string) {
	t.Helper()
	dir := filepath.Join(s.dir(), folder)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "skin.json"), skinTestJSON(t, skinTestManifest(id)), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "skin.css"), []byte(`body[data-skin="`+id+`"] {color:red}`), 0644); err != nil {
		t.Fatal(err)
	}
}

func testSkinZIP(t *testing.T, entries map[string][]byte) []byte {
	t.Helper()
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	for name, data := range entries {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func testSkinArchive(t *testing.T, id, prefix string) []byte {
	t.Helper()
	return testSkinZIP(t, map[string][]byte{prefix + "skin.json": skinTestJSON(t, skinTestManifest(id)), prefix + "skin.css": []byte(`body[data-skin="` + id + `"] {color:red}`)})
}

func TestSkinManifestValidation(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*skinManifest)
	}{
		{"schema", func(m *skinManifest) { m.Schema = 2 }},
		{"id traversal", func(m *skinManifest) { m.ID = "../escape" }},
		{"id uppercase", func(m *skinManifest) { m.ID = "Upper" }},
		{"id length", func(m *skinManifest) { m.ID = strings.Repeat("a", 33) }},
		{"reserved default", func(m *skinManifest) { m.ID = "default" }},
		{"name empty", func(m *skinManifest) { m.Name = "" }},
		{"name unicode length", func(m *skinManifest) { m.Name = strings.Repeat("猫", 41) }},
		{"author", func(m *skinManifest) { m.Author = strings.Repeat("a", 101) }},
		{"version", func(m *skinManifest) { m.Version = strings.Repeat("a", 21) }},
		{"description", func(m *skinManifest) { m.Description = strings.Repeat("a", 201) }},
		{"css traversal", func(m *skinManifest) { m.CSS = "../skin.css" }},
		{"css drive", func(m *skinManifest) { m.CSS = "C:skin.css" }},
		{"css extension", func(m *skinManifest) { m.CSS = "skin.html" }},
		{"swatch count", func(m *skinManifest) { m.Swatch = []string{"#123456"} }},
		{"swatch empty", func(m *skinManifest) { m.Swatch = []string{"#123456", "", "#123456"} }},
		{"swatch color", func(m *skinManifest) { m.Swatch = []string{"#123456", "red", "#123456"} }},
		{"startup color", func(m *skinManifest) { m.StartupBackground.Dark = "#123" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := skinTestManifest("valid")
			tc.mutate(&m)
			if _, err := validateSkinManifest(skinTestJSON(t, m)); err == nil {
				t.Fatal("不正な manifest を受理した")
			}
		})
	}
	m := skinTestManifest(strings.Repeat("a", 32))
	m.Name = strings.Repeat("猫", 40)
	m.CSS = ""
	got, err := validateSkinManifest(skinTestJSON(t, m))
	if err != nil || got.CSS != "skin.css" {
		t.Fatalf("有効な境界値: %+v, %v", got, err)
	}
	for _, b := range [][]byte{bytes.Repeat([]byte(" "), skinJSONLimit+1), {0xff}, []byte(`{"schema":1.5}`), []byte(`{"schema":1} trailing`)} {
		if _, err := validateSkinManifest(b); err == nil {
			t.Error("不正な JSON を受理した")
		}
	}
}

func TestSkinCSSValidation(t *testing.T) {
	for _, css := range []string{
		`@import "x.css";`, `@IMPORT url(data:image/png;base64,abc)`,
		`a{background:url(https://example.com/x)}`, `a{background:URL('file:///x')}`,
		`</StYlE><script>`, `a{width:expression(alert(1))}`, `javascript:alert(1)`,
		`a{-moz-binding:none}`, `a{behavior:url(x)}`,
		`@im/**/port "x";`, `u/**/rl(https://example.com)`,
		`@\69mport "x"`, `a{background:\75rl(https://example.com)}`,
		`a{beha/**/vior: none}`, `a{width:expre/**/ssion(1)}`,
		`a{color:red;behavior : url(x)}`, `behavior:url(x)`,
	} {
		t.Run(css, func(t *testing.T) {
			if _, err := validateSkinCSS([]byte(css)); err == nil {
				t.Fatal("不正な CSS を受理した")
			}
		})
	}
	for _, css := range []string{`body[data-skin="test"] {color:red}`, `a{background:url("data:image/svg+xml;base64,abc")}`, `a{background:url(data:image/png;base64,abc)}`, `/* normal comment */ a{color:red}`, `html{scroll-behavior:smooth}`, `a{overscroll-behavior:contain}`} {
		if _, err := validateSkinCSS([]byte(css)); err != nil {
			t.Errorf("有効な CSS: %v", err)
		}
	}
	bom := append([]byte{0xef, 0xbb, 0xbf}, []byte("a{}")...)
	if b, err := validateSkinCSS(bom); err != nil || string(b) != "a{}" {
		t.Fatalf("BOM 除去: %q %v", b, err)
	}
	for _, b := range [][]byte{bytes.Repeat([]byte(" "), skinCSSLimit+1), {0xff}} {
		if _, err := validateSkinCSS(b); err == nil {
			t.Fatal("サイズ/文字コードの制限がない")
		}
	}
	if _, err := validateSkinCSS(bytes.Repeat([]byte(" "), skinCSSLimit)); err != nil {
		t.Fatal(err)
	}
}

func TestSkinZIPValidation(t *testing.T) {
	for _, prefix := range []string{"", "package/"} {
		if v, err := readSkinZIP(testSkinArchive(t, "custom", prefix)); err != nil || v.manifest.ID != "custom" {
			t.Fatalf("prefix=%q: %+v %v", prefix, v, err)
		}
	}
	base := func() map[string][]byte {
		return map[string][]byte{"skin.json": skinTestJSON(t, skinTestManifest("custom")), "skin.css": []byte("a{}")}
	}
	for _, name := range []string{"../evil", "/absolute", `C:/evil`, `x\..\evil`, `x/../evil`, `./evil`, `x//evil`, `//host/share`} {
		e := base()
		e[name] = []byte("ignored")
		if _, err := readSkinZIP(testSkinZIP(t, e)); err == nil {
			t.Errorf("危険なパスを受理: %s", name)
		}
	}
	e := base()
	e["ignored.bin"] = bytes.Repeat([]byte("a"), skinZIPLimit)
	if _, err := readSkinZIP(testSkinZIP(t, e)); err == nil {
		t.Error("展開サイズ制限がない")
	}
	e = base()
	for i := 0; i < 19; i++ {
		e[fmt.Sprintf("ignored%d", i)] = nil
	}
	if _, err := readSkinZIP(testSkinZIP(t, e)); err == nil {
		t.Error("エントリ数制限がない")
	}
	e = base()
	e["skin.css"] = bytes.Repeat([]byte("a"), skinCSSLimit+1)
	if _, err := readSkinZIP(testSkinZIP(t, e)); err == nil {
		t.Error("巨大 CSS を受理した")
	}
	e = base()
	e["skin.json"] = bytes.Repeat([]byte(" "), skinJSONLimit+1)
	if _, err := readSkinZIP(testSkinZIP(t, e)); err == nil {
		t.Error("巨大 JSON を受理した")
	}
	e = map[string][]byte{"one/skin.json": skinTestJSON(t, skinTestManifest("custom")), "one/skin.css": []byte("a{}"), "two/readme": nil}
	if _, err := readSkinZIP(testSkinZIP(t, e)); err == nil {
		t.Error("トップレベル 2 フォルダを受理した")
	}
	for _, css := range []string{`@import "external"`, `a{background:url(https://example.com)}`, `</style>`} {
		e = base()
		e["skin.css"] = []byte(css)
		if _, err := readSkinZIP(testSkinZIP(t, e)); err == nil {
			t.Errorf("不正 CSS の ZIP を受理: %s", css)
		}
	}
	for _, data := range [][]byte{[]byte("not zip"), bytes.Repeat([]byte("x"), skinZIPLimit+1)} {
		if _, err := readSkinZIP(data); err == nil {
			t.Error("不正/巨大 ZIP を受理した")
		}
	}
	// 不要ファイルは保存されず、フォルダのエントリは許可される。
	e = map[string][]byte{"package/": nil, "package/skin.json": skinTestJSON(t, skinTestManifest("custom")), "package/skin.css": []byte("a{}"), "package/readme.txt": []byte("ignored")}
	s := newTestSkinStore(t)
	if _, err := s.importZIP(testSkinZIP(t, e), false); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(s.dir(), "custom"))
	if err != nil || len(entries) != 2 {
		t.Fatalf("必要な 2 ファイルのみ保存: %v %v", entries, err)
	}
}

func TestSkinZIPDuplicateAndSymlink(t *testing.T) {
	for _, symlink := range []bool{false, true} {
		var b bytes.Buffer
		w := zip.NewWriter(&b)
		for i := 0; i < 2; i++ {
			h := &zip.FileHeader{Name: "skin.json", Method: zip.Deflate}
			if symlink {
				h.SetMode(os.ModeSymlink | 0644)
			}
			f, err := w.CreateHeader(h)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = f.Write(skinTestJSON(t, skinTestManifest("custom"))); err != nil {
				t.Fatal(err)
			}
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		if _, err := readSkinZIP(b.Bytes()); err == nil {
			t.Fatalf("duplicate/symlink=%v を受理した", symlink)
		}
	}
}

func TestSkinCatalogDuplicatesAndRescan(t *testing.T) {
	s := newTestSkinStore(t)
	writeTestSkin(t, s, "different folder", "z-last")
	writeTestSkin(t, s, "first", "a-first")
	writeTestSkin(t, s, "dup1", "duplicate")
	writeTestSkin(t, s, "dup2", "duplicate")
	writeTestSkin(t, s, "builtin-conflict", "shukei")
	writeTestSkin(t, s, "broken", "bad")
	if err := os.WriteFile(filepath.Join(s.dir(), "broken", "skin.css"), []byte("@import 'x'"), 0644); err != nil {
		t.Fatal(err)
	}
	c := s.scan()
	if len(c.skins) != 6 || len(c.errors) != 4 {
		t.Fatalf("skins=%d errors=%+v", len(c.skins), c.errors)
	}
	for i, id := range append(append([]string{}, builtinSkinIDs...), "a-first", "z-last") {
		if c.skins[i].manifest.ID != id {
			t.Errorf("順序[%d]=%s want %s", i, c.skins[i].manifest.ID, id)
		}
	}
	if _, ok := c.find("duplicate"); ok {
		t.Fatal("重複 id がカタログに残っている")
	}
	if s.active("missing") != "default" || s.active("") != "default" {
		t.Fatal("不明 id が default に戻らない")
	}
	if _, err := s.importZIP(testSkinArchive(t, "duplicate", ""), true); err == nil {
		t.Fatal("重複 id を上書きした")
	}
	if _, err := s.importZIP(testSkinArchive(t, "unrelated", ""), false); err != nil {
		t.Fatalf("別 id の重複が新規 import を妨げた: %v", err)
	}
	// 外部編集を次のスキャンで反映する。
	if err := os.WriteFile(filepath.Join(s.dir(), "first", "skin.css"), []byte("a{color:blue}"), 0644); err != nil {
		t.Fatal(err)
	}
	v, ok := s.scan().find("a-first")
	if !ok || string(v.css) != "a{color:blue}" {
		t.Fatal("再スキャンされていない")
	}
}

func skinTestRequest(t *testing.T, h http.Handler, method, url string, body []byte, token bool) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, url, bytes.NewReader(body))
	r.Host = "127.0.0.1:4321"
	if token {
		r.Header.Set("X-Widget-Token", "skin-test-token")
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestSkinHTTPAndConfigIntegration(t *testing.T) {
	withConfigFile(t, configV180)
	s := newTestSkinStore(t)
	old := skins
	skins = s
	t.Cleanup(func() { skins = old })
	h := securityMiddleware(newServerMux("skin-test-token"), "skin-test-token", 4321)
	request := func(method, url string, body []byte, want int) *httptest.ResponseRecorder {
		t.Helper()
		w := skinTestRequest(t, h, method, url, body, true)
		if w.Code != want {
			t.Fatalf("%s %s: %d want %d: %s", method, url, w.Code, want, w.Body.String())
		}
		return w
	}
	for _, url := range []string{"/api/skins", "/api/skin/css?id=shukei", "/api/skins/import", "/api/skins/delete?id=custom", "/api/skins/copy?id=shukei", "/api/skins/opendir"} {
		if w := skinTestRequest(t, h, http.MethodPost, url, nil, false); w.Code != 403 {
			t.Fatalf("トークン無しを許可: %s", url)
		}
	}
	request("GET", "/api/skins/delete?id=custom", nil, 405)
	request("POST", "/api/skins", nil, 405)
	request("GET", "/api/setoption?key=skin&value=shukei", nil, 405)
	request("POST", "/api/setoption?key=skin&value=missing", nil, 400)
	request("POST", "/api/setoption?key=skin&value=../bad", nil, 400)
	request("GET", "/api/skin/css?id=missing", nil, 404)
	request("GET", "/api/skin/css?id=../bad", nil, 404)
	w := request("GET", "/api/skin/css?id=default", nil, 200)
	if w.Body.Len() != 0 || w.Header().Get("Content-Type") != "text/css; charset=utf-8" {
		t.Fatal("default CSS の応答が不正")
	}
	w = request("GET", "/api/settings", nil, 200)
	if !strings.Contains(w.Body.String(), `"skin":"default"`) {
		t.Fatal(w.Body.String())
	}
	request("POST", "/api/setoption?key=skin&value=shukei", nil, 200)
	loadConfig()
	if snapshotConfig().Skin != "shukei" {
		t.Fatal("選択が永続化されていない")
	}
	request("POST", "/api/skins/import", testSkinArchive(t, "custom", ""), 200)
	if snapshotConfig().Skin != "shukei" {
		t.Fatal("import が勝手に有効化された")
	}
	request("POST", "/api/skins/import", testSkinArchive(t, "custom", ""), 409)
	request("POST", "/api/skins/import?overwrite=1", testSkinArchive(t, "custom", "package/"), 200)
	for _, id := range []string{"default", "shukei", "fieldnote", "orbital", "paper-tape"} {
		request("POST", "/api/skins/import?overwrite=1", testSkinArchive(t, id, ""), 400)
		request("POST", "/api/skins/delete?id="+id, nil, 400)
	}
	request("POST", "/api/skins/import", bytes.Repeat([]byte("x"), skinZIPLimit+1), 400)
	request("POST", "/api/skins/copy?id=custom", nil, 400)
	request("POST", "/api/skins/copy?id=default", nil, 400)
	w = request("POST", "/api/skins/copy?id=shukei", nil, 200)
	if !strings.Contains(w.Body.String(), `"id":"shukei-copy"`) || !strings.Contains(w.Body.String(), "(コピー)") {
		t.Fatal(w.Body.String())
	}
	request("POST", "/api/skins/copy?id=shukei", nil, 409)
	w = request("GET", "/api/skin/css?id=shukei-copy", nil, 200)
	if strings.Contains(w.Body.String(), `data-skin="shukei"`) || strings.Count(w.Body.String(), `data-skin="shukei-copy"`) != 2 {
		t.Fatal("複製 CSS のセレクタが不正: " + w.Body.String())
	}
	request("POST", "/api/setoption?key=skin&value=custom", nil, 200)
	w = request("GET", "/api/settings", nil, 200)
	if !strings.Contains(w.Body.String(), `"skin":"custom"`) {
		t.Fatal(w.Body.String())
	}
	w = request("GET", "/api/skins", nil, 200)
	var catalog struct {
		Active string
		Dir    string
		Skins  []skinInfo
		Errors []skinLoadError
	}
	if err := json.Unmarshal(w.Body.Bytes(), &catalog); err != nil {
		t.Fatal(err)
	}
	if catalog.Active != "custom" || catalog.Dir != s.dir() || len(catalog.Skins) != 7 || len(catalog.Errors) != 0 || catalog.Skins[0].ID != "default" || catalog.Skins[0].Swatch == nil {
		t.Fatalf("catalog=%+v", catalog)
	}
	request("POST", "/api/skins/delete?id=custom", nil, 200)
	loadConfig()
	if snapshotConfig().Skin != "default" {
		t.Fatal("削除時の default が永続化されていない")
	}
	request("GET", "/api/skin/css?id=custom", nil, 404)
	request("POST", "/api/skins/delete?id=custom", nil, 400)
	request("POST", "/api/skins/delete?id=../bad", nil, 400)
	opened := ""
	s.openDir = func(dir string) error { opened = dir; return nil }
	request("POST", "/api/skins/opendir", nil, 200)
	if opened != s.dir() {
		t.Fatal("opendir の対象が違う")
	}
	request("POST", "/api/setoption?key=skin&value=default", nil, 200)
	mutateConfig(func(c *Config) { c.Skin = "missing" })
	w = request("GET", "/api/settings", nil, 200)
	if !strings.Contains(w.Body.String(), `"skin":"default"`) {
		t.Fatal("不明 id の settings が default でない")
	}
	w = request("GET", "/api/skins", nil, 200)
	if !strings.Contains(w.Body.String(), `"active":"default"`) {
		t.Fatal("不明 id の一覧が default でない")
	}
}

func TestSkinImportOverwriteDifferentFolderAndDelete(t *testing.T) {
	withConfigFile(t, "")
	s := newTestSkinStore(t)
	writeTestSkin(t, s, "My Custom Folder", "custom")
	if _, err := s.importZIP(testSkinArchive(t, "custom", ""), false); err != errSkinConflict {
		t.Fatalf("id 衝突: %v", err)
	}
	if _, err := s.importZIP(testSkinArchive(t, "custom", ""), true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.dir(), "custom")); !os.IsNotExist(err) {
		t.Fatal("id 名の別フォルダが作られた")
	}
	if err := s.setActive("custom"); err != nil {
		t.Fatal(err)
	}
	if err := s.delete("custom"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.dir(), "My Custom Folder")); !os.IsNotExist(err) {
		t.Fatal("実フォルダが削除されていない")
	}
	// id に一致するフォルダが別 id のスキンなら overwrite=1 でも消さない。
	writeTestSkin(t, s, "custom", "another")
	if _, err := s.importZIP(testSkinArchive(t, "custom", ""), true); err != errSkinConflict {
		t.Fatalf("別 id を上書き: %v", err)
	}
	if _, ok := s.scan().find("another"); !ok {
		t.Fatal("別 id のスキンを失った")
	}
}

// 読み込みに失敗している(一覧に出ない)同名フォルダは、overwrite=1 のインポートで直せる。
func TestSkinImportOverwriteBrokenFolder(t *testing.T) {
	withConfigFile(t, "")
	s := newTestSkinStore(t)
	broken := filepath.Join(s.dir(), "custom")
	if err := os.MkdirAll(broken, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(broken, "skin.json"), []byte("{broken"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.scan().find("custom"); ok {
		t.Fatal("壊れたスキンが一覧に出ている")
	}
	if _, err := s.importZIP(testSkinArchive(t, "custom", ""), false); err != errSkinConflict {
		t.Fatalf("overwrite なし: %v", err)
	}
	if _, err := s.importZIP(testSkinArchive(t, "custom", ""), true); err != nil {
		t.Fatalf("overwrite あり: %v", err)
	}
	if _, ok := s.scan().find("custom"); !ok {
		t.Fatal("上書き後も読み込めない")
	}
}

func TestSkinConcurrentImport(t *testing.T) {
	s := newTestSkinStore(t)
	data := testSkinArchive(t, "custom", "")
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := s.importZIP(data, false); results <- err }()
	}
	wg.Wait()
	close(results)
	success, conflict := 0, 0
	for err := range results {
		switch err {
		case nil:
			success++
		case errSkinConflict:
			conflict++
		default:
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("success=%d conflict=%d", success, conflict)
	}
}

// 実際の組み込みファイルの検証は、他担当の作成完了後に最後に実行する。
func TestBuiltinSkinsLoad(t *testing.T) {
	s := &skinStore{builtin: embeddedSkinFS(), dir: func() string { return filepath.Join(t.TempDir(), "skins") }}
	c := s.scan()
	if len(c.errors) != 0 {
		t.Fatalf("組み込みスキンの検証エラー: %+v", c.errors)
	}
	if len(c.skins) != 4 {
		t.Fatalf("組み込み数=%d want 4", len(c.skins))
	}
	for i, id := range builtinSkinIDs {
		if c.skins[i].manifest.ID != id || !c.skins[i].builtin {
			t.Errorf("組み込み[%d]=%+v", i, c.skins[i].info())
		}
	}
}
