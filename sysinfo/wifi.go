package sysinfo

import (
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"syscall"
)

type WiFiInfo struct {
	SSID    string
	Band    string
	Type    string
	Signal  int
	Channel int
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
	signalRe := regexp.MustCompile(`(\d+)%`)
	radioRe := regexp.MustCompile(`:\s*(.+)`)
	channelRe := regexp.MustCompile(`:\s*(\d+)`)

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// SSID
		if strings.HasPrefix(trimmed, "SSID") && !strings.Contains(trimmed, "BSSID") {
			if match := ssidRe.FindStringSubmatch(trimmed); len(match) > 1 {
				info.SSID = strings.TrimSpace(match[1])
			}
		}

		// シグナル (Signal)
		if strings.HasPrefix(trimmed, "シグナル") || strings.Contains(trimmed, "Signal") {
			if match := signalRe.FindStringSubmatch(trimmed); len(match) > 1 {
				info.Signal, _ = strconv.Atoi(match[1])
			}
		}

		// 無線の種類 (Radio type)
		if strings.HasPrefix(trimmed, "無線の種類") || strings.Contains(trimmed, "Radio type") {
			if match := radioRe.FindStringSubmatch(trimmed); len(match) > 1 {
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

	if info.SSID == "" {
		return nil, nil
	}

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
