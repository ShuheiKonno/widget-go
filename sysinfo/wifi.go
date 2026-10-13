package sysinfo

import (
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"syscall"
)

type WiFiInfo struct {
	SSID        string
	BSSID       string
	Band        string
	Type        string
	Description string // アダプタのモデル名 (netsh の "説明" / "Description" 行)
	Signal      int
	Channel     int
	Connected   bool
}

func GetWiFiInfo() (*WiFiInfo, error) {
	cmd := exec.Command("netsh", "wlan", "show", "interfaces")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	return parseWiFiInfo(output), nil
}

// parseWiFiInfo は `netsh wlan show interfaces` の出力を解析して WiFiInfo を返す。
// 未接続（切断状態、または SSID 無し）の場合は nil を返す。
// exec と分離した純粋関数にすることで、日英ロケールの出力をテストで検証できる。
func parseWiFiInfo(output []byte) *WiFiInfo {
	lines := strings.Split(string(output), "\n")
	info := &WiFiInfo{}

	ssidRe := regexp.MustCompile(`SSID\s*:\s*(.+)`)
	bssidRe := regexp.MustCompile(`BSSID\s*:\s*(.+)`)
	colonRe := regexp.MustCompile(`:\s*(.+)`) // State / Radio type など "key : value" 共通
	signalRe := regexp.MustCompile(`(\d+)%`)
	channelRe := regexp.MustCompile(`:\s*(\d+)`)

	stateFound := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// 状態 / State （接続/切断 を判定）
		// ※ SSID / BSSID より先に評価。"State" を含む別行（"Radio state" 等）と混同しないよう
		//    行頭が "状態" または "State" で始まるものだけを対象にする。
		// ※ 日本語版 Windows の netsh は "接続されました" / "切断されました" を出すため、
		//    "接続済み" 等の固定文字列ではマッチしない。負の条件（切断系のみ false）で判定する。
		if !stateFound && (strings.HasPrefix(trimmed, "状態") || strings.HasPrefix(trimmed, "State")) {
			if match := colonRe.FindStringSubmatch(trimmed); len(match) > 1 {
				val := strings.TrimSpace(match[1])
				lower := strings.ToLower(val)
				// "disconnected" または "切断" を含む場合のみ未接続とみなす。
				// それ以外（"connected" / "接続されました" / "authenticating" 等の過渡状態）は
				// 接続中扱い（過渡状態でも SSID/Channel が出ていれば表示する方が UX が良い）。
				if strings.Contains(lower, "disconnected") || strings.Contains(val, "切断") {
					info.Connected = false
				} else {
					info.Connected = true
				}
				stateFound = true
			}
		}

		// BSSID （SSID より先に判定しないと SSID の HasPrefix に吸われる）
		// Windows 11 26200 では "AP BSSID" 表記に変わっているため Contains で拾う。
		// ("SSID" 行には "BSSID" が含まれないので誤マッチはしない)
		if strings.Contains(trimmed, "BSSID") {
			if info.BSSID == "" {
				if match := bssidRe.FindStringSubmatch(trimmed); len(match) > 1 {
					info.BSSID = strings.TrimSpace(match[1])
				}
			}
			continue
		}

		// 説明 (Description) — アダプタのモデル名。
		if strings.HasPrefix(trimmed, "説明") || strings.HasPrefix(trimmed, "Description") {
			if match := colonRe.FindStringSubmatch(trimmed); len(match) > 1 {
				info.Description = strings.TrimSpace(match[1])
			}
			continue
		}

		// SSID
		if strings.HasPrefix(trimmed, "SSID") {
			if match := ssidRe.FindStringSubmatch(trimmed); len(match) > 1 {
				info.SSID = strings.TrimSpace(match[1])
			}
			continue
		}

		// シグナル (Signal)
		if strings.HasPrefix(trimmed, "シグナル") || strings.Contains(trimmed, "Signal") {
			if match := signalRe.FindStringSubmatch(trimmed); len(match) > 1 {
				info.Signal, _ = strconv.Atoi(match[1])
			}
		}

		// 無線の種類 (Radio type)
		if strings.HasPrefix(trimmed, "無線の種類") || strings.Contains(trimmed, "Radio type") {
			if match := colonRe.FindStringSubmatch(trimmed); len(match) > 1 {
				info.Type = strings.TrimSpace(match[1])
			}
		}

		// チャネル (Channel)
		if strings.HasPrefix(trimmed, "チャネル") || strings.Contains(trimmed, "Channel") {
			if match := channelRe.FindStringSubmatch(trimmed); len(match) > 1 {
				info.Channel, _ = strconv.Atoi(match[1])
			}
		}

		// バンド / 帯域 (Band) — Windows 11 22H2 以降で netsh が出力するようになった。
		// 6GHz と 2.4GHz はチャネル番号が被る (どちらも 1〜) ため、
		// この行があれば最優先で採用する。
		// 日本語ロケール実機 (Win11 26200) では "バンド" だった。ビルド/バージョン依存で
		// "帯域" と表記される環境もありうるのでどちらも受け付ける。
		// 注意:
		//   ja: "帯域" と "帯域幅" (channel bandwidth) を区別する
		//   en: "Band" と "Bandwidth" を区別する
		if strings.HasPrefix(trimmed, "バンド") ||
			(strings.HasPrefix(trimmed, "帯域") && !strings.HasPrefix(trimmed, "帯域幅")) ||
			(strings.HasPrefix(trimmed, "Band") && !strings.HasPrefix(trimmed, "Bandwidth")) {
			if match := colonRe.FindStringSubmatch(trimmed); len(match) > 1 {
				info.Band = normalizeBand(strings.TrimSpace(match[1]))
			}
		}
	}

	// State を取得できた場合は State を優先して切断を判定する。
	// State が取得できない環境向けに SSID 空フォールバックも残す。
	if stateFound && !info.Connected {
		return nil
	}
	if info.SSID == "" {
		return nil
	}
	info.Connected = true

	// netsh が "バンド"/"帯域" 行を出してくれた場合は info.Band は既に埋まっている。
	// 出さない環境 (Windows 10 / 古い Win11) 向けにチャネルから推定する。
	// ※ 6GHz と 2.4GHz はチャネル番号が被る (どちらも 1〜) ので、バンド行がない場合の
	//   ch 1-14 は 2.4GHz 扱いで割り切る (Wi-Fi 7 実機は通常バンド行を出す Win11 新ビルド)。
	//   ch >= 178 は 6GHz 専用帯 (UNII-8 上位) なので 6GHz と確定できる。
	if info.Band == "" {
		switch {
		case info.Channel >= 178:
			info.Band = "6GHz"
		case info.Channel >= 36 && info.Channel <= 177:
			info.Band = "5GHz"
		case info.Channel >= 1 && info.Channel <= 14:
			info.Band = "2.4GHz"
		}
	}

	return info
}

// bandValueRe は netsh の "バンド"/"Band" 行の値から周波数帯を抽出する。
// "5 GHz", "2.4 GHz", "6 GHz" 等の表記揺れに対応。
var bandValueRe = regexp.MustCompile(`(?i)(2\.4|5|6)\s*GHz`)

// normalizeBand は "5 GHz" のような値を "5GHz" 形式に正規化する。
// UI 側は Band 値をそのまま表示するため、2.4/5/6 の 3 値で揃える。
func normalizeBand(raw string) string {
	if m := bandValueRe.FindStringSubmatch(raw); len(m) > 1 {
		return m[1] + "GHz"
	}
	return raw
}

// GetWiFiStandard はWi-Fi規格名を返す
func GetWiFiStandard(radioType string) string {
	standards := map[string]string{
		"802.11a":  "802.11a",
		"802.11b":  "802.11b",
		"802.11g":  "802.11g",
		"802.11n":  "802.11n (Wi-Fi 4)",
		"802.11ac": "802.11ac (Wi-Fi 5)",
		"802.11ax": "802.11ax (Wi-Fi 6)",
		"802.11be": "802.11be (Wi-Fi 7)",
	}

	if standard, ok := standards[radioType]; ok {
		return standard
	}
	return radioType
}
