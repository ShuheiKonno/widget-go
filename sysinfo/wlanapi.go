package sysinfo

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Wi-Fi の状態を Native Wifi API（wlanapi.dll）から読む。
// 以前は `netsh wlan show interfaces` を 3 秒ごとに起動していた（1 日あたり約 28,800 回のプロセス生成と、
// OS の言語・ビルドごとに違う出力の解析）。API なら、プロセスを作らず、言語にも依らない。
// 読めなかったときは wifi.go の netsh に戻る（GetWiFiInfo）。
//
// 構造体は、Windows SDK の wlanapi.h / wlantypes.h / windot11.h のレイアウトを、バイト列のオフセットで読む。
// Go の構造体にしないのは、API が確保した領域を読むだけで、パディングの食い違いを避けたいため。

var (
	// 検索パスを System32 に限る（カレントディレクトリなどから DLL を差し込まれないように）。
	wlanapi                   = windows.NewLazySystemDLL("wlanapi.dll")
	procWlanOpenHandle        = wlanapi.NewProc("WlanOpenHandle")
	procWlanCloseHandle       = wlanapi.NewProc("WlanCloseHandle")
	procWlanEnumInterfaces    = wlanapi.NewProc("WlanEnumInterfaces")
	procWlanQueryInterface    = wlanapi.NewProc("WlanQueryInterface")
	procWlanGetNetworkBssList = wlanapi.NewProc("WlanGetNetworkBssList")
	procWlanFreeMemory        = wlanapi.NewProc("WlanFreeMemory")
)

const (
	wlanClientVersion = 2 // Vista 以降

	errorServiceNotActive = 1062 // WLAN AutoConfig サービスが動いていない（Wi-Fi の無い PC など）
	errorInvalidState     = 5023 // 接続していないインターフェースに、接続の情報を求めた

	wlanIntfOpcodeCurrentConnection = 7
	wlanIntfOpcodeChannelNumber     = 8

	wlanInterfaceStateConnected = 1
	dot11BSSTypeAny             = 3

	// WLAN_INTERFACE_INFO: GUID(16) + 説明(WCHAR 256 = 512) + 状態(4)
	wlanInterfaceInfoSize = 16 + 512 + 4
	// WLAN_BSS_ENTRY の大きさ。配列の先頭は WLAN_BSS_LIST の 8 バイト目（ULONGLONG の境界）
	wlanBssEntrySize = 360
)

// errWlanUnavailable は wlanapi を使えない（DLL が無い、呼び出しが想定外に失敗した）ことを表す。netsh に戻る合図。
var errWlanUnavailable = errors.New("wlanapi を使えません")

// wlanInterface は WLAN_INTERFACE_INFO のうち使う部分。
type wlanInterface struct {
	GUID        [16]byte
	Description string
	State       uint32
}

// wlanConnection は WLAN_CONNECTION_ATTRIBUTES のうち使う部分。
type wlanConnection struct {
	SSID    []byte
	BSSID   [6]byte
	PhyType uint32
	Signal  int // 0〜100（%）
}

// bssEntry は WLAN_BSS_ENTRY のうち使う部分。
type bssEntry struct {
	BSSID   [6]byte
	FreqKHz uint32
}

// parseWlanInterfaces は WlanEnumInterfaces が返す WLAN_INTERFACE_INFO_LIST を読む。
func parseWlanInterfaces(b []byte) []wlanInterface {
	if len(b) < 8 {
		return nil
	}
	n := int(binary.LittleEndian.Uint32(b[0:4]))
	var out []wlanInterface
	for i := 0; i < n; i++ {
		off := 8 + i*wlanInterfaceInfoSize
		if off+wlanInterfaceInfoSize > len(b) {
			break
		}
		it := wlanInterface{State: binary.LittleEndian.Uint32(b[off+16+512:])}
		copy(it.GUID[:], b[off:off+16])
		it.Description = utf16String(b[off+16 : off+16+512])
		out = append(out, it)
	}
	return out
}

// utf16String は NUL 終端の UTF-16LE のバイト列を文字列にする。
func utf16String(b []byte) string {
	u := make([]uint16, 0, len(b)/2)
	for i := 0; i+1 < len(b); i += 2 {
		c := binary.LittleEndian.Uint16(b[i:])
		if c == 0 {
			break
		}
		u = append(u, c)
	}
	return string(utf16.Decode(u))
}

// parseWlanConnection は WLAN_CONNECTION_ATTRIBUTES を読む。
// 先頭から: isState(4) mode(4) プロファイル名(WCHAR 256 = 512)、そのあとに WLAN_ASSOCIATION_ATTRIBUTES:
// SSID 長(4) SSID(32) BSS 種別(4) BSSID(6, 2 バイトのパディング) PHY 種別(4) PHY 番号(4) 信号品質(4) …
func parseWlanConnection(b []byte) (wlanConnection, bool) {
	const assoc = 4 + 4 + 512
	if len(b) < assoc+4+32+4+8+4+4+4 {
		return wlanConnection{}, false
	}
	var c wlanConnection
	ssidLen := int(binary.LittleEndian.Uint32(b[assoc:]))
	if ssidLen > 32 {
		return wlanConnection{}, false
	}
	c.SSID = append([]byte(nil), b[assoc+4:assoc+4+ssidLen]...)
	p := assoc + 4 + 32 + 4 // BSS 種別の次
	copy(c.BSSID[:], b[p:p+6])
	p += 8
	c.PhyType = binary.LittleEndian.Uint32(b[p:])
	c.Signal = int(binary.LittleEndian.Uint32(b[p+8:]))
	return c, true
}

// parseBssList は WlanGetNetworkBssList が返す WLAN_BSS_LIST を読む（先頭: 全体の大きさ(4) 件数(4)）。
func parseBssList(b []byte) []bssEntry {
	if len(b) < 8 {
		return nil
	}
	n := int(binary.LittleEndian.Uint32(b[4:8]))
	var out []bssEntry
	for i := 0; i < n; i++ {
		off := 8 + i*wlanBssEntrySize
		if off+wlanBssEntrySize > len(b) {
			break
		}
		var e bssEntry
		copy(e.BSSID[:], b[off+40:off+46])
		e.FreqKHz = binary.LittleEndian.Uint32(b[off+92:])
		out = append(out, e)
	}
	return out
}

// phyTypeName は DOT11_PHY_TYPE を、netsh の「無線の種類」と同じ表記にする。
func phyTypeName(t uint32) string {
	switch t {
	case 4:
		return "802.11a" // OFDM
	case 5:
		return "802.11b" // HR/DSSS
	case 6:
		return "802.11g" // ERP
	case 7:
		return "802.11n" // HT
	case 8:
		return "802.11ac" // VHT
	case 9:
		return "802.11ad" // DMG
	case 10:
		return "802.11ax" // HE
	case 11:
		return "802.11be" // EHT
	}
	return ""
}

// bandFromFreqKHz は中心周波数（kHz）から周波数帯を返す。6GHz は 5925MHz から。
func bandFromFreqKHz(f uint32) string {
	switch {
	case f == 0:
		return ""
	case f < 3_000_000:
		return "2.4GHz"
	case f < 5_925_000:
		return "5GHz"
	default:
		return "6GHz"
	}
}

// channelFromFreqKHz は中心周波数（kHz）からチャネル番号を求める（API でチャネルを取れなかったときの代わり）。
func channelFromFreqKHz(f uint32) int {
	switch {
	case f == 2_484_000:
		return 14
	case f >= 2_412_000 && f <= 2_472_000:
		return int(f-2_407_000) / 5000
	case f >= 5_160_000 && f < 5_925_000:
		return int(f-5_000_000) / 5000
	case f >= 5_925_000 && f <= 7_125_000:
		return int(f-5_950_000) / 5000 // 6GHz は 5955MHz がチャネル 1
	}
	return 0
}

// wlanCall は wlanapi の呼び出しを、エラーコード（0 が成功）で返す。
func wlanCall(p *windows.LazyProc, args ...uintptr) uintptr {
	r, _, _ := p.Call(args...)
	return r
}

// copyWlanMemory は API が確保した領域を n バイトだけコピーして解放する。
func copyWlanMemory(p unsafe.Pointer, n int) []byte {
	if p == nil {
		return nil
	}
	b := append([]byte(nil), unsafe.Slice((*byte)(p), n)...)
	wlanCall(procWlanFreeMemory, uintptr(p))
	return b
}

// getWiFiInfoWlanAPI は Wi-Fi の状態を wlanapi から読む。接続していなければ (nil, nil)。
// 想定外の失敗は errWlanUnavailable で返し、呼び出し側は netsh に戻る。
func getWiFiInfoWlanAPI() (*WiFiInfo, error) {
	if err := wlanapi.Load(); err != nil {
		return nil, errWlanUnavailable
	}
	var negotiated uint32
	var h uintptr
	switch r := wlanCall(procWlanOpenHandle, wlanClientVersion, 0, uintptr(unsafe.Pointer(&negotiated)), uintptr(unsafe.Pointer(&h))); r {
	case 0:
	case errorServiceNotActive:
		return nil, nil // Wi-Fi の無い PC。netsh でも読めない
	default:
		return nil, fmt.Errorf("%w: WlanOpenHandle=%d", errWlanUnavailable, r)
	}
	defer wlanCall(procWlanCloseHandle, h, 0)

	var list unsafe.Pointer
	if r := wlanCall(procWlanEnumInterfaces, h, 0, uintptr(unsafe.Pointer(&list))); r != 0 {
		return nil, fmt.Errorf("%w: WlanEnumInterfaces=%d", errWlanUnavailable, r)
	}
	var ifaces []wlanInterface
	if list != nil {
		n := int(*(*uint32)(list))
		ifaces = parseWlanInterfaces(copyWlanMemory(list, 8+n*wlanInterfaceInfoSize))
	}

	for i := range ifaces {
		it := &ifaces[i]
		if it.State != wlanInterfaceStateConnected {
			continue
		}
		info, err := queryWiFiConnection(h, it)
		if err != nil {
			return nil, err
		}
		if info != nil {
			return info, nil
		}
	}
	return nil, nil
}

// queryWlan は WlanQueryInterface の結果をコピーして返す。失敗時はエラーコード。
func queryWlan(h uintptr, guid *[16]byte, opcode uintptr) ([]byte, uintptr) {
	var size uint32
	var data unsafe.Pointer
	var valueType uint32
	r := wlanCall(procWlanQueryInterface, h, uintptr(unsafe.Pointer(guid)), opcode, 0,
		uintptr(unsafe.Pointer(&size)), uintptr(unsafe.Pointer(&data)), uintptr(unsafe.Pointer(&valueType)))
	if r != 0 {
		return nil, r
	}
	return copyWlanMemory(data, int(size)), 0
}

func queryWiFiConnection(h uintptr, it *wlanInterface) (*WiFiInfo, error) {
	raw, r := queryWlan(h, &it.GUID, wlanIntfOpcodeCurrentConnection)
	if r == errorInvalidState {
		return nil, nil // 接続の途中で切れた
	}
	if r != 0 {
		return nil, fmt.Errorf("%w: WlanQueryInterface(connection)=%d", errWlanUnavailable, r)
	}
	conn, ok := parseWlanConnection(raw)
	if !ok {
		return nil, fmt.Errorf("%w: 接続情報の形式が不正", errWlanUnavailable)
	}
	ssid := strings.ToValidUTF8(string(conn.SSID), "�")
	if ssid == "" {
		return nil, nil
	}

	info := &WiFiInfo{
		SSID:        ssid,
		BSSID:       formatMAC(conn.BSSID),
		Type:        phyTypeName(conn.PhyType),
		Description: it.Description,
		Signal:      conn.Signal,
		Connected:   true,
	}

	// 周波数帯とチャネル。接続中の AP（BSSID が同じ）の中心周波数から求める。
	// netsh は新しい Windows 11 でだけバンドを出していた。周波数なら、6GHz と 2.4GHz のチャネル番号の重なりも区別できる。
	var freq uint32
	var bss unsafe.Pointer
	if r := wlanCall(procWlanGetNetworkBssList, h, uintptr(unsafe.Pointer(&it.GUID)), 0, dot11BSSTypeAny, 0, 0, uintptr(unsafe.Pointer(&bss))); r == 0 && bss != nil {
		total := int(*(*uint32)(bss))
		for _, e := range parseBssList(copyWlanMemory(bss, total)) {
			if e.BSSID == conn.BSSID {
				freq = e.FreqKHz
				break
			}
		}
	}
	info.Band = bandFromFreqKHz(freq)

	if raw, r := queryWlan(h, &it.GUID, wlanIntfOpcodeChannelNumber); r == 0 && len(raw) >= 4 {
		info.Channel = int(binary.LittleEndian.Uint32(raw))
	}
	if info.Channel == 0 {
		info.Channel = channelFromFreqKHz(freq)
	}
	// 周波数が分からないときは、チャネルから推定する（wifi.go の netsh と同じ割り切り）。
	if info.Band == "" {
		info.Band = bandFromChannel(info.Channel)
	}
	return info, nil
}

func formatMAC(m [6]byte) string {
	return fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x", m[0], m[1], m[2], m[3], m[4], m[5])
}

// 確認用コマンド（cmd/wifiprobe）から、経路を指定して呼ぶための窓口。
func GetWiFiInfoNetshForProbe() (*WiFiInfo, error)   { return getWiFiInfoNetsh() }
func GetWiFiInfoWlanAPIForProbe() (*WiFiInfo, error) { return getWiFiInfoWlanAPI() }
