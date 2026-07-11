package sysinfo

import "testing"

// netsh wlan show interfaces の出力サンプル（実機の書式に準拠）。

const netshJaConnected = `
システム上に 1 インターフェイスがあります:

    名前                   : Wi-Fi
    説明                   : Intel(R) Wi-Fi 6E AX211 160MHz
    GUID                   : 00000000-0000-0000-0000-000000000000
    物理アドレス           : 11:22:33:44:55:66
    状態                   : 接続されました
    SSID                   : MyHomeNet
    AP BSSID               : aa:bb:cc:dd:ee:ff
    バンド                 : 5 GHz
    チャネル               : 36
    受信速度 (Mbps)        : 1200
    送信速度 (Mbps)        : 1200
    シグナル               : 90%
    無線の種類             : 802.11ax
    認証                   : WPA2-Personal
`

const netshEnConnected = `
There is 1 interface on the system:

    Name                   : Wi-Fi
    Description            : Intel(R) Wi-Fi 6E AX211 160MHz
    GUID                   : 00000000-0000-0000-0000-000000000000
    Physical address       : 11:22:33:44:55:66
    State                  : connected
    SSID                   : MyHomeNet
    AP BSSID               : aa:bb:cc:dd:ee:ff
    Band                   : 5 GHz
    Channel                : 36
    Receive rate (Mbps)    : 1200
    Transmit rate (Mbps)   : 1200
    Signal                 : 90%
    Radio type             : 802.11ax
    Authentication         : WPA2-Personal
`

// バンド行が無い（古い Windows）ケース。チャネルから帯域を推定する。
const netshEnNoBandCh149 = `
    Name                   : Wi-Fi
    State                  : connected
    SSID                   : LegacyAP
    BSSID                  : aa:bb:cc:dd:ee:ff
    Channel                : 149
    Signal                 : 75%
    Radio type             : 802.11ac
`

const netshJaDisconnected = `
    名前                   : Wi-Fi
    状態                   : 切断されました
    ソフトウェアのオフ スイッチ : オフ
`

func TestParseWiFiInfo(t *testing.T) {
	cases := []struct {
		name       string
		output     string
		wantNil    bool
		wantSSID   string
		wantBand   string
		wantCh     int
		wantSignal int
		wantType   string
	}{
		{
			name:       "日本語ロケール・接続",
			output:     netshJaConnected,
			wantSSID:   "MyHomeNet",
			wantBand:   "5GHz",
			wantCh:     36,
			wantSignal: 90,
			wantType:   "802.11ax",
		},
		{
			name:       "英語ロケール・接続",
			output:     netshEnConnected,
			wantSSID:   "MyHomeNet",
			wantBand:   "5GHz",
			wantCh:     36,
			wantSignal: 90,
			wantType:   "802.11ax",
		},
		{
			name:       "バンド行なし・チャネル149から5GHzを推定",
			output:     netshEnNoBandCh149,
			wantSSID:   "LegacyAP",
			wantBand:   "5GHz",
			wantCh:     149,
			wantSignal: 75,
			wantType:   "802.11ac",
		},
		{
			name:    "日本語ロケール・切断はnil",
			output:  netshJaDisconnected,
			wantNil: true,
		},
		{
			name:    "空出力はnil",
			output:  "",
			wantNil: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseWiFiInfo([]byte(tc.output))
			if tc.wantNil {
				if got != nil {
					t.Fatalf("parseWiFiInfo = %+v, want nil", got)
				}
				return
			}
			if got == nil {
				t.Fatal("parseWiFiInfo = nil, want non-nil")
			}
			if got.SSID != tc.wantSSID {
				t.Errorf("SSID = %q, want %q", got.SSID, tc.wantSSID)
			}
			if got.Band != tc.wantBand {
				t.Errorf("Band = %q, want %q", got.Band, tc.wantBand)
			}
			if got.Channel != tc.wantCh {
				t.Errorf("Channel = %d, want %d", got.Channel, tc.wantCh)
			}
			if got.Signal != tc.wantSignal {
				t.Errorf("Signal = %d, want %d", got.Signal, tc.wantSignal)
			}
			if got.Type != tc.wantType {
				t.Errorf("Type = %q, want %q", got.Type, tc.wantType)
			}
			if !got.Connected {
				t.Error("Connected = false, want true")
			}
		})
	}
}

// normalizeBand の表記揺れ吸収を検証する。
func TestNormalizeBand(t *testing.T) {
	cases := []struct{ in, want string }{
		{"5 GHz", "5GHz"},
		{"2.4 GHz", "2.4GHz"},
		{"6 GHz", "6GHz"},
		{"5GHz", "5GHz"},
		{"不明な値", "不明な値"},
	}
	for _, tc := range cases {
		if got := normalizeBand(tc.in); got != tc.want {
			t.Errorf("normalizeBand(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// GetWiFiStandard の規格名マッピングを検証する。
func TestGetWiFiStandard(t *testing.T) {
	cases := []struct{ in, want string }{
		{"802.11ax", "802.11ax (Wi-Fi 6)"},
		{"802.11be", "802.11be (Wi-Fi 7)"},
		{"802.11ac", "802.11ac (Wi-Fi 5)"},
		{"802.11n", "802.11n (Wi-Fi 4)"},
		{"unknown", "unknown"},
	}
	for _, tc := range cases {
		if got := GetWiFiStandard(tc.in); got != tc.want {
			t.Errorf("GetWiFiStandard(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
