package sysinfo

import (
	"net"
	"strings"
	"syscall"
	"unsafe"
)

type EthernetInfo struct {
	Name         string
	Description  string // アダプタのモデル名 (MIB_IF_ROW2.Description)
	LinkSpeedBps uint64 // TransmitLinkSpeed (bits/sec). 取得失敗時は 0。
	Connected    bool
}

// excludeNameHints はイーサネット判定から除外するインターフェース名の部分文字列。
// Wi-Fi 系・仮想 NIC・VPN・ループバックなどは有線接続ではないので弾く。
// 小文字で比較する。
var excludeNameHints = []string{
	"wi-fi", "wifi", "wireless", "wlan",
	"bluetooth",
	"virtual", "vmware", "vethernet", "hyper-v", "wsl",
	"loopback",
	"tailscale", "tap-", "tun", "vpn", "zerotier",
	"npcap",
}

var (
	iphlpapi        = syscall.NewLazyDLL("iphlpapi.dll")
	procGetIfEntry2 = iphlpapi.NewProc("GetIfEntry2")
)

// mibIfRow2 は Windows SDK netioapi.h の MIB_IF_ROW2 に対応する。
// GetIfEntry2 は InterfaceLuid または InterfaceIndex を入力として行全体を埋める。
// Windows 側が構造体末尾まで書き込むためサイズを一致させる必要があり、
// 使わない統計フィールドも全部定義しておく。
// レイアウト上の注意:
//   - InterfaceAndOperStatusFlags (uint8) の後に 3byte のパディングが入る (Go が自動)
//   - ConnectionType (uint32) の後に 4byte のパディングが入る (Go が自動)
type mibIfRow2 struct {
	InterfaceLuid               uint64
	InterfaceIndex              uint32
	InterfaceGuid               [16]byte
	Alias                       [257]uint16
	Description                 [257]uint16
	PhysicalAddressLength       uint32
	PhysicalAddress             [32]byte
	PermanentPhysicalAddress    [32]byte
	Mtu                         uint32
	Type                        uint32
	TunnelType                  uint32
	MediaType                   uint32
	PhysicalMediumType          uint32
	AccessType                  uint32
	DirectionType               uint32
	InterfaceAndOperStatusFlags uint8
	OperStatus                  uint32
	AdminStatus                 uint32
	MediaConnectState           uint32
	NetworkGuid                 [16]byte
	ConnectionType              uint32
	TransmitLinkSpeed           uint64
	ReceiveLinkSpeed            uint64
	InOctets                    uint64
	InUcastPkts                 uint64
	InNUcastPkts                uint64
	InDiscards                  uint64
	InErrors                    uint64
	InUnknownProtos             uint64
	InUcastOctets               uint64
	InMulticastOctets           uint64
	InBroadcastOctets           uint64
	OutOctets                   uint64
	OutUcastPkts                uint64
	OutNUcastPkts               uint64
	OutDiscards                 uint64
	OutErrors                   uint64
	OutUcastOctets              uint64
	OutMulticastOctets          uint64
	OutBroadcastOctets          uint64
	OutQLen                     uint64
}

// ifRowInfo は MIB_IF_ROW2 から物理 NIC 判定に必要な情報を取り出した結果。
type ifRowInfo struct {
	LinkSpeedBps      uint64
	Description       string
	HardwareInterface bool
	TunnelType        uint32
}

// getIfRowInfo は InterfaceIndex から送信リンク速度・Description・物理 NIC フラグ等を返す。
// 取得失敗時は ok=false。DWORD(32bit) の GetIfEntry と違い、>4Gbps も正しく扱える。
//
// HardwareInterface ビット: InterfaceAndOperStatusFlags の bit 0。
// 物理 NIC のみ 1 になり、VPN/仮想/ループバック等は 0。
func getIfRowInfo(ifIndex int) (ifRowInfo, bool) {
	var row mibIfRow2
	row.InterfaceIndex = uint32(ifIndex)
	ret, _, _ := procGetIfEntry2.Call(uintptr(unsafe.Pointer(&row)))
	if ret != 0 { // NO_ERROR = 0
		return ifRowInfo{}, false
	}
	return ifRowInfo{
		LinkSpeedBps:      row.TransmitLinkSpeed,
		Description:       syscall.UTF16ToString(row.Description[:]),
		HardwareInterface: (row.InterfaceAndOperStatusFlags & 0x01) != 0,
		TunnelType:        row.TunnelType,
	}, true
}

// GetEthernetInfo は有線 NIC のうち UP 状態かつ有効な IPv4 を持つものを返す。
// 見つからなければ nil, nil。
//
// 厳密には「現在 default route が通っている NIC」を返すべきだが、
// 用途（Wi-Fi 未接続時に "Ethernet" と表示したい）にはこの簡易判定で十分。
func GetEthernetInfo() (*EthernetInfo, error) {
	ifs, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	for _, ifc := range ifs {
		if ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		if ifc.Flags&net.FlagUp == 0 {
			continue
		}
		info, ok := getIfRowInfo(ifc.Index)
		if !ok {
			continue
		}
		// 物理 NIC 以外（VPN・仮想・トンネル）は除外。
		// VPN クライアント (Cato Networks Tunnel 等) は HardwareInterface=0 になる。
		if !info.HardwareInterface || info.TunnelType != 0 {
			continue
		}
		// Alias（ifc.Name）と Description の両方を名前ヒントで弾く。
		// Alias はユーザが自由に変更できるため Description も併用する。
		lowerName := strings.ToLower(ifc.Name)
		lowerDesc := strings.ToLower(info.Description)
		excluded := false
		for _, hint := range excludeNameHints {
			if strings.Contains(lowerName, hint) || strings.Contains(lowerDesc, hint) {
				excluded = true
				break
			}
		}
		if excluded {
			continue
		}
		addrs, err := ifc.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip4 := ipnet.IP.To4()
			if ip4 == nil {
				continue
			}
			if ip4.IsLoopback() || ip4.IsLinkLocalUnicast() || ip4.IsUnspecified() {
				continue
			}
			return &EthernetInfo{
				Name:         ifc.Name,
				Description:  info.Description,
				LinkSpeedBps: info.LinkSpeedBps,
				Connected:    true,
			}, nil
		}
	}
	return nil, nil
}
