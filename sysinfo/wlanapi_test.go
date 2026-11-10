package sysinfo

import (
	"encoding/binary"
	"testing"
	"unicode/utf16"
)

// buildInterfaceList は WLAN_INTERFACE_INFO_LIST の見本を作る。
func buildInterfaceList(items ...wlanInterface) []byte {
	b := make([]byte, 8+len(items)*wlanInterfaceInfoSize)
	binary.LittleEndian.PutUint32(b[0:], uint32(len(items)))
	for i, it := range items {
		off := 8 + i*wlanInterfaceInfoSize
		copy(b[off:], it.GUID[:])
		for j, c := range utf16.Encode([]rune(it.Description)) {
			binary.LittleEndian.PutUint16(b[off+16+j*2:], c)
		}
		binary.LittleEndian.PutUint32(b[off+16+512:], it.State)
	}
	return b
}

// buildConnection は WLAN_CONNECTION_ATTRIBUTES の見本を作る（オフセットは wlanapi.go の説明どおり）。
func buildConnection(ssid string, bssid [6]byte, phy uint32, signal uint32) []byte {
	b := make([]byte, 4+4+512+4+32+4+8+4+4+4+4+4+200) // 後ろのセキュリティ属性などは余白
	binary.LittleEndian.PutUint32(b[0:], wlanInterfaceStateConnected)
	const assoc = 4 + 4 + 512
	binary.LittleEndian.PutUint32(b[assoc:], uint32(len(ssid)))
	copy(b[assoc+4:], ssid)
	p := assoc + 4 + 32 + 4
	copy(b[p:], bssid[:])
	p += 8
	binary.LittleEndian.PutUint32(b[p:], phy)
	binary.LittleEndian.PutUint32(b[p+8:], signal)
	return b
}

func TestParseWlanInterfaces(t *testing.T) {
	g := [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	got := parseWlanInterfaces(buildInterfaceList(
		wlanInterface{GUID: g, Description: "Intel(R) Wi-Fi 6E AX211 160MHz", State: 1},
		wlanInterface{Description: "ワイヤレス アダプター", State: 4},
	))
	if len(got) != 2 {
		t.Fatalf("2 件のはず: %d", len(got))
	}
	if got[0].GUID != g || got[0].Description != "Intel(R) Wi-Fi 6E AX211 160MHz" || got[0].State != 1 {
		t.Errorf("1 件目: %+v", got[0])
	}
	if got[1].Description != "ワイヤレス アダプター" || got[1].State != 4 {
		t.Errorf("2 件目（日本語の説明）: %+v", got[1])
	}
}

func TestParseWlanInterfacesRejectsTruncated(t *testing.T) {
	full := buildInterfaceList(wlanInterface{Description: "x", State: 1}, wlanInterface{Description: "y", State: 1})
	if got := parseWlanInterfaces(full[:8+wlanInterfaceInfoSize+10]); len(got) != 1 {
		t.Errorf("途中で切れた 2 件目は読まない: %d 件", len(got))
	}
	if parseWlanInterfaces(nil) != nil || parseWlanInterfaces([]byte{1, 2}) != nil {
		t.Error("短すぎる入力は nil")
	}
}

func TestParseWlanConnection(t *testing.T) {
	bssid := [6]byte{0x2e, 0x2b, 0xa6, 0xa4, 0x48, 0xc4}
	c, ok := parseWlanConnection(buildConnection("SSID-A448BD", bssid, 10, 82))
	if !ok {
		t.Fatal("読めるはず")
	}
	if string(c.SSID) != "SSID-A448BD" || c.BSSID != bssid || c.PhyType != 10 || c.Signal != 82 {
		t.Errorf("%+v", c)
	}
	if formatMAC(c.BSSID) != "2e:2b:a6:a4:48:c4" {
		t.Errorf("netsh と同じ小文字のコロン区切りのはず: %s", formatMAC(c.BSSID))
	}
}

func TestParseWlanConnectionRejectsBadInput(t *testing.T) {
	if _, ok := parseWlanConnection([]byte{1, 2, 3}); ok {
		t.Error("短すぎる入力は拒否")
	}
	bad := buildConnection("x", [6]byte{}, 10, 1)
	binary.LittleEndian.PutUint32(bad[4+4+512:], 33) // SSID 長が 32 を超える
	if _, ok := parseWlanConnection(bad); ok {
		t.Error("SSID 長が不正なら拒否（範囲外を読まない）")
	}
}

func TestParseBssList(t *testing.T) {
	want := [6]byte{0x2e, 0x2b, 0xa6, 0xa4, 0x48, 0xc4}
	b := make([]byte, 8+2*wlanBssEntrySize)
	binary.LittleEndian.PutUint32(b[0:], uint32(len(b)))
	binary.LittleEndian.PutUint32(b[4:], 2)
	put := func(i int, bssid [6]byte, freq uint32) {
		off := 8 + i*wlanBssEntrySize
		copy(b[off+40:], bssid[:])
		binary.LittleEndian.PutUint32(b[off+92:], freq)
	}
	put(0, [6]byte{1, 2, 3, 4, 5, 6}, 2437000)
	put(1, want, 5500000)

	es := parseBssList(b)
	if len(es) != 2 || es[0].FreqKHz != 2437000 || es[1].BSSID != want || es[1].FreqKHz != 5500000 {
		t.Errorf("%+v", es)
	}
	if got := parseBssList(b[:8+wlanBssEntrySize+5]); len(got) != 1 {
		t.Errorf("途中で切れた 2 件目は読まない: %d 件", len(got))
	}
}

func TestPhyTypeNameMatchesNetshNotation(t *testing.T) {
	for phy, want := range map[uint32]string{4: "802.11a", 5: "802.11b", 6: "802.11g", 7: "802.11n", 8: "802.11ac", 10: "802.11ax", 11: "802.11be"} {
		if got := phyTypeName(phy); got != want {
			t.Errorf("phy %d = %q, want %q", phy, got, want)
		}
		// UI は GetWiFiStandard で表示名にするので、その対応表に載る表記であること
		if GetWiFiStandard(want) == "" {
			t.Errorf("%q が GetWiFiStandard に渡せない", want)
		}
	}
	if phyTypeName(0) != "" || phyTypeName(99) != "" {
		t.Error("不明な種別は空")
	}
}

func TestBandAndChannelFromFrequency(t *testing.T) {
	cases := []struct {
		kHz  uint32
		band string
		ch   int
	}{
		{2412000, "2.4GHz", 1},
		{2437000, "2.4GHz", 6},
		{2472000, "2.4GHz", 13},
		{2484000, "2.4GHz", 14},
		{5180000, "5GHz", 36},
		{5500000, "5GHz", 100},
		{5885000, "5GHz", 177},
		{5955000, "6GHz", 1},  // 6GHz のチャネル 1 は 2.4GHz のチャネル 1 と番号が被る。周波数なら区別できる
		{6415000, "6GHz", 93}, // 6GHz のチャネル 93
		{0, "", 0},
	}
	for _, c := range cases {
		if got := bandFromFreqKHz(c.kHz); got != c.band {
			t.Errorf("%d kHz: band=%q, want %q", c.kHz, got, c.band)
		}
		if got := channelFromFreqKHz(c.kHz); got != c.ch {
			t.Errorf("%d kHz: ch=%d, want %d", c.kHz, got, c.ch)
		}
	}
}

func TestBandFromChannel(t *testing.T) {
	for ch, want := range map[int]string{1: "2.4GHz", 14: "2.4GHz", 36: "5GHz", 177: "5GHz", 178: "6GHz", 0: "", 20: ""} {
		if got := bandFromChannel(ch); got != want {
			t.Errorf("ch %d = %q, want %q", ch, got, want)
		}
	}
}

// 実機に Wi-Fi がある環境では、wlanapi の結果が netsh と同じになる（無ければ何も確かめない）。
func TestLiveWlanAPIMatchesNetshWhenAvailable(t *testing.T) {
	api, err := getWiFiInfoWlanAPI()
	if err != nil || api == nil {
		t.Skip("wlanapi で Wi-Fi の接続を読めない環境")
	}
	ns, err := getWiFiInfoNetsh()
	if err != nil || ns == nil {
		t.Skip("netsh で読めない環境")
	}
	if api.SSID != ns.SSID || api.BSSID != ns.BSSID || api.Type != ns.Type || api.Channel != ns.Channel || api.Description != ns.Description {
		t.Errorf("wlanapi と netsh が食い違う:\n api=%+v\n netsh=%+v", *api, *ns)
	}
	// バンドは netsh がチャネルからの推定のことがある。出ているときだけ比べる
	if ns.Band != "" && api.Band != ns.Band {
		t.Errorf("バンド: api=%q netsh=%q", api.Band, ns.Band)
	}
}
