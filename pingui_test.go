package main

import (
	"os"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"widget-go/ping"
)

// jsonTags は構造体の JSON タグ名の集合を返す。
func jsonTags(v any) map[string]bool {
	tags := make(map[string]bool)
	rt := reflect.TypeOf(v)
	for i := 0; i < rt.NumField(); i++ {
		name, _, _ := strings.Cut(rt.Field(i).Tag.Get("json"), ",")
		if name != "" && name != "-" {
			tags[name] = true
		}
	}
	return tags
}

// jsFunctionBody は src から `function name(...){ ... }` の本体を、波かっこの対応で切り出す。
// 文字列やコメントの中の波かっこは考えない（applyPing には含まれない前提。崩れたら下の必須キーの確認で落ちる）。
func jsFunctionBody(t *testing.T, src, name string) string {
	t.Helper()
	head := "function " + name + "("
	i := strings.Index(src, head)
	if i < 0 {
		t.Fatalf("ui/index.html に %s が見つからない", head)
	}
	open := strings.Index(src[i:], "{")
	if open < 0 {
		t.Fatalf("%s の本体が見つからない", name)
	}
	depth := 0
	for j := i + open; j < len(src); j++ {
		switch src[j] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return src[i+open : j+1]
			}
		}
	}
	t.Fatalf("%s の本体が閉じていない", name)
	return ""
}

// propRefs は body の中の `obj.prop` の prop を集める（メソッド呼び出し `obj.prop(` は除く）。
func propRefs(body, obj string) map[string]bool {
	re := regexp.MustCompile(`\b` + regexp.QuoteMeta(obj) + `\.([A-Za-z_$][A-Za-z0-9_$]*)\b(\s*\()?`)
	refs := make(map[string]bool)
	for _, m := range re.FindAllStringSubmatch(body, -1) {
		if m[2] != "" {
			continue
		}
		refs[m[1]] = true
	}
	return refs
}

func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// #56: ui/index.html の applyPing が読むフィールド名が、ping パッケージの JSON タグに実在するかを確かめる。
// JavaScript は大文字小文字を区別し、存在しないキーは黙って undefined になる（isWarnRtt と isWarnRTT の取り違えで、
// RTT の警告が画面に出なかった）。Go 側のマーシャルだけを見るテストでは、この取り違えは見つからない。
func TestPingUIFieldNamesMatchJSONTags(t *testing.T) {
	data, err := os.ReadFile("ui/index.html")
	if err != nil {
		t.Fatal(err)
	}
	body := jsFunctionBody(t, string(data), "applyPing")

	checks := []struct {
		obj      string          // applyPing の中の変数名
		tags     map[string]bool // その変数が指す構造体の JSON タグ
		required []string        // 抽出が空回りしていないことの確認用（必ず参照しているはずのキー）
	}{
		{"info", jsonTags(ping.Info{}), []string{"targets", "summaryRtt"}},
		{"t", jsonTags(ping.TargetInfo{}), []string{"host", "targetIP", "status", "rttMs", "isWarnRTT", "isWarnLoss", "lossReady", "lossCount", "sentCount", "resolveFail"}},
	}
	for _, c := range checks {
		refs := propRefs(body, c.obj)
		for _, k := range sortedKeys(refs) {
			if !c.tags[k] {
				t.Errorf("applyPing が %s.%s を読んでいるが、JSON タグに %q はない（タグ: %v）", c.obj, k, k, sortedKeys(c.tags))
			}
		}
		for _, k := range c.required {
			if !refs[k] {
				t.Errorf("applyPing の中に %s.%s の参照が見つからない（変数名・関数名が変わってテストが空回りしていないか確認）", c.obj, k)
			}
		}
	}
}

// UI の警告の判定（PING_ALERT_STATUS）は、Go 側の ping.IsAlertStatus と同じ status の集合にする。
func TestPingUIAlertStatusMatchesGo(t *testing.T) {
	data, err := os.ReadFile("ui/index.html")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`const PING_ALERT_STATUS = \{([^}]*)\}`).FindStringSubmatch(string(data))
	if m == nil {
		t.Fatal("ui/index.html に PING_ALERT_STATUS が見つからない")
	}
	ui := make(map[string]bool)
	for _, kv := range regexp.MustCompile(`(\w+)\s*:\s*true`).FindAllStringSubmatch(m[1], -1) {
		ui[kv[1]] = true
	}
	all := []string{ping.StatusIdle, ping.StatusOK, ping.StatusWarn, ping.StatusError, ping.StatusUnresolved}
	for _, st := range all {
		if ui[st] != ping.IsAlertStatus(st) {
			t.Errorf("status %q: UI=%v, Go=%v", st, ui[st], ping.IsAlertStatus(st))
		}
	}
	if len(ui) == 0 {
		t.Error("PING_ALERT_STATUS が空")
	}
}
