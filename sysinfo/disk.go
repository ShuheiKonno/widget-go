//go:build windows

package sysinfo

import (
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// DriveInfo は固定ドライブ 1 つの容量情報。
type DriveInfo struct {
	Letter      string // "C:" の形
	Total       uint64
	Used        uint64
	UsedPercent float64
}

// DiskIOInfo は物理ディスク全体の I/O。取得できない値は nil（UI は「---」にする）。
type DiskIOInfo struct {
	ActivePct *float64 // アクティブな時間（%）。タスクマネージャーの「アクティブな時間」に相当
	ReadBps   *float64 // 読み取り（バイト/秒）
	WriteBps  *float64 // 書き込み（バイト/秒）
}

// DiskInfo は /api/system の Disk に載せるスナップショット。
type DiskInfo struct {
	Drives []DriveInfo
	IO     *DiskIOInfo
}

// driveFromSpace は GetDiskFreeSpaceEx の結果から DriveInfo を組み立てる。
// 総容量が 0 のドライブ（メディアなし等）は ok=false。
func driveFromSpace(letter string, total, free uint64) (DriveInfo, bool) {
	if total == 0 {
		return DriveInfo{}, false
	}
	if free > total {
		free = total
	}
	used := total - free
	return DriveInfo{
		Letter:      letter,
		Total:       total,
		Used:        used,
		UsedPercent: float64(used) * 100 / float64(total),
	}, true
}

// GetDrives は固定ドライブ（DRIVE_FIXED）の容量を返す。
// 切断済みのネットワークドライブや、空の光学ドライブ・カードリーダーに容量を問い合わせると
// 待たされたりエラーになったりするため、種別で先に絞る。
func GetDrives() []DriveInfo {
	mask, err := windows.GetLogicalDrives()
	if err != nil {
		return nil
	}
	var drives []DriveInfo
	for i := 0; i < 26; i++ {
		if mask&(1<<uint(i)) == 0 {
			continue
		}
		letter := string(rune('A'+i)) + ":"
		root, err := windows.UTF16PtrFromString(letter + `\`)
		if err != nil {
			continue
		}
		if windows.GetDriveType(root) != windows.DRIVE_FIXED {
			continue
		}
		var avail, total, free uint64
		if err := windows.GetDiskFreeSpaceEx(root, &avail, &total, &free); err != nil {
			continue
		}
		if d, ok := driveFromSpace(letter, total, free); ok {
			drives = append(drives, d)
		}
	}
	return drives
}

// activeFromIdle は PhysicalDisk の "% Idle Time" からアクティブな時間（%）を求める。
// 100 - Idle で、取りうる範囲 0〜100 に収める。
func activeFromIdle(idle float64) float64 {
	a := 100 - idle
	if a < 0 {
		return 0
	}
	if a > 100 {
		return 100
	}
	return a
}

var (
	diskIOMu      sync.Mutex
	diskQuery     uintptr
	diskIdle      uintptr
	diskRead      uintptr
	diskWrite     uintptr
	diskIOReady   bool
	diskIOCounter = [3]struct {
		path string
		h    *uintptr
	}{
		{`\PhysicalDisk(_Total)\% Idle Time`, &diskIdle},
		{`\PhysicalDisk(_Total)\Disk Read Bytes/sec`, &diskRead},
		{`\PhysicalDisk(_Total)\Disk Write Bytes/sec`, &diskWrite},
	}
)

// InitDiskIO は Disk I/O 取得用の PDH クエリを開く。成功済みなら何もしない。
// カウンタ名はロケール依存のため、英語名で登録できる PdhAddEnglishCounterW を使う。
// 失敗しても次の GetDiskIO で再試行する。
func InitDiskIO() bool {
	diskIOMu.Lock()
	defer diskIOMu.Unlock()
	return initDiskIOLocked()
}

func initDiskIOLocked() bool {
	if diskIOReady {
		return true
	}
	for _, p := range []*syscall.LazyProc{procPdhOpenQuery, procPdhAddEnglishCounter, procPdhCollectQueryData, procPdhGetFormattedCounterValue, procPdhCloseQuery} {
		if p.Find() != nil {
			return false
		}
	}
	var query uintptr
	if r, _, _ := procPdhOpenQuery.Call(0, 0, uintptr(unsafe.Pointer(&query))); r != 0 {
		return false
	}
	for _, c := range diskIOCounter {
		path, err := syscall.UTF16PtrFromString(c.path)
		if err != nil {
			procPdhCloseQuery.Call(query)
			return false
		}
		if r, _, _ := procPdhAddEnglishCounter.Call(query, uintptr(unsafe.Pointer(path)), 0, uintptr(unsafe.Pointer(c.h))); r != 0 {
			procPdhCloseQuery.Call(query)
			return false
		}
	}
	// 差分カウンタなので、最初の 1 回はベースライン取得のために捨てる。
	procPdhCollectQueryData.Call(query)
	diskQuery = query
	diskIOReady = true
	return true
}

// CloseDiskIO は PDH クエリを閉じる。区画を非表示にして収集を止めるときに呼ぶ。
// 再開時は InitDiskIO が開き直し、最初の 1 回はベースラインとして捨てる。
func CloseDiskIO() {
	diskIOMu.Lock()
	defer diskIOMu.Unlock()
	if diskIOReady {
		procPdhCloseQuery.Call(diskQuery)
		diskQuery, diskIOReady = 0, false
	}
}

func diskCounterValue(h uintptr) *float64 {
	var value pdhFmtCounterValue
	var valueType uint32
	r, _, _ := procPdhGetFormattedCounterValue.Call(h, pdhFmtDouble, uintptr(unsafe.Pointer(&valueType)), uintptr(unsafe.Pointer(&value)))
	if r != 0 || value.CStatus != 0 {
		return nil
	}
	v := value.DoubleValue
	if v < 0 {
		v = 0
	}
	return &v
}

// GetDiskIO は物理ディスク全体の I/O を返す。取得できないときは nil。
func GetDiskIO() *DiskIOInfo {
	diskIOMu.Lock()
	defer diskIOMu.Unlock()
	if !initDiskIOLocked() {
		return nil
	}
	if r, _, _ := procPdhCollectQueryData.Call(diskQuery); r != 0 {
		procPdhCloseQuery.Call(diskQuery)
		diskQuery, diskIOReady = 0, false
		return nil
	}
	io := &DiskIOInfo{ReadBps: diskCounterValue(diskRead), WriteBps: diskCounterValue(diskWrite)}
	if idle := diskCounterValue(diskIdle); idle != nil {
		a := activeFromIdle(*idle)
		io.ActivePct = &a
	}
	return io
}
