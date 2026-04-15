package sysinfo

import (
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"syscall"
)

type WiFiInfo struct {
	SSID      string
	BSSID     string
	Band      string
	Type      string
	Signal    int
	Channel   int
	Connected bool
}

func GetWiFiInfo() (*WiFiInfo, error) {
	cmd := exec.Command("netsh", "wlan", "show", "interfaces")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

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
		if strings.HasPrefix(trimmed, "BSSID") {
			if info.BSSID == "" {
				if match := bssidRe.FindStringSubmatch(trimmed); len(match) > 1 {
					info.BSSID = strings.TrimSpace(match[1])
				}
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
	}

	// State を取得できた場合は State を優先して切断を判定する。
	// State が取得できない環境向けに SSID 空フォールバックも残す。
	if stateFound && !info.Connected {
		return nil, nil
	}
	if info.SSID == "" {
		return nil, nil
	}
	info.Connected = true

	// チャネルから帯域を判定
	if info.Channel >= 36 {
		info.Band = "5GHz"
	} else {
		info.Band = "2.4GHz"
	}

	return info, nil
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
