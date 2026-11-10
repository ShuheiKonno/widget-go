// wifiprobe は Wi-Fi の取得結果を、wlanapi と netsh の両方で表示して見比べる確認用のコマンド。
package main

import (
	"fmt"

	"widget-go/sysinfo"
)

func main() {
	fmt.Println("GetWiFiInfo (wlanapi→netsh):", show(sysinfo.GetWiFiInfo()))
	fmt.Println("netsh のみ:                 ", show(sysinfo.GetWiFiInfoNetshForProbe()))
	fmt.Println("wlanapi のみ:               ", show(sysinfo.GetWiFiInfoWlanAPIForProbe()))
}

func show(i *sysinfo.WiFiInfo, err error) string {
	if err != nil {
		return "error: " + err.Error()
	}
	if i == nil {
		return "未接続(nil)"
	}
	return fmt.Sprintf("%+v", *i)
}
