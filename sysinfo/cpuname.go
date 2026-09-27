package sysinfo

import (
	"sync"

	"golang.org/x/sys/windows/registry"
)

// CPU 名は起動後に変わらない静的な情報なので、一度だけ読んでキャッシュする。
var (
	cpuNameOnce  sync.Once
	cpuNameValue string
)

// GetCPUName は CPU のモデル名（例: "Intel(R) Core(TM) Ultra 7 240H"）を返す。
// 取得できない場合は空文字を返す。
//
// WMI (Win32_Processor) でも同じ値が取れるが、初回クエリに数百 ms かかるうえ
// COM の初期化状態に依存する。ここが参照しているレジストリ値は WMI と同じ
// ソース（ファームウェアのブランド文字列）で、読み取りは数十 µs で終わる。
func GetCPUName() string {
	cpuNameOnce.Do(func() {
		k, err := registry.OpenKey(registry.LOCAL_MACHINE,
			`HARDWARE\DESCRIPTION\System\CentralProcessor\0`, registry.QUERY_VALUE)
		if err != nil {
			return
		}
		defer k.Close()
		s, _, err := k.GetStringValue("ProcessorNameString")
		if err != nil {
			return
		}
		cpuNameValue = s
	})
	return cpuNameValue
}
