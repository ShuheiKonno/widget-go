package sysinfo

import (
	"errors"
	"runtime"
	"sync"
	"syscall"
	"unsafe"
)

// CPUFreqInfo は CPU の動作クロック情報（MHz）。
type CPUFreqInfo struct {
	CurrentMHz float64 // 現在の実効クロック（ターボ含む）
	BaseMHz    float64 // 定格（ベース）クロック
}

var (
	powrprof                   = syscall.NewLazyDLL("powrprof.dll")
	procCallNtPowerInformation = powrprof.NewProc("CallNtPowerInformation")

	pdh                             = syscall.NewLazyDLL("pdh.dll")
	procPdhOpenQuery                = pdh.NewProc("PdhOpenQueryW")
	procPdhAddEnglishCounter        = pdh.NewProc("PdhAddEnglishCounterW")
	procPdhCollectQueryData         = pdh.NewProc("PdhCollectQueryData")
	procPdhGetFormattedCounterValue = pdh.NewProc("PdhGetFormattedCounterValue")
)

const (
	// CallNtPowerInformation の InformationLevel: ProcessorInformation
	processorInformation = 11
	pdhFmtDouble         = 0x00000200
)

// processorPowerInformation は CallNtPowerInformation(ProcessorInformation) が
// 論理プロセッサごとに返す構造体（PROCESSOR_POWER_INFORMATION）。
type processorPowerInformation struct {
	Number           uint32
	MaxMhz           uint32
	CurrentMhz       uint32
	MhzLimit         uint32
	MaxIdleState     uint32
	CurrentIdleState uint32
}

// pdhFmtCounterValue は PDH_FMT_COUNTERVALUE（doubleFormat 用）。
// CStatus の後ろに union のアライメント用パディングが入る。
type pdhFmtCounterValue struct {
	CStatus     uint32
	_           uint32
	DoubleValue float64
}

var (
	freqMu      sync.Mutex
	freqQuery   uintptr
	freqCounter uintptr
	freqBaseMHz float64
	freqReady   bool
)

// baseClockMHz はベースクロックを CallNtPowerInformation の MaxMhz から取得する。
// 同 API の CurrentMhz は多くの Windows 10/11 環境でベース値に固定されたまま
// 変動しないため、現在クロックの取得には使わない（MaxMhz は信頼できる）。
func baseClockMHz() (float64, error) {
	n := runtime.NumCPU()
	if n <= 0 {
		return 0, errors.New("論理プロセッサ数が取得できません")
	}
	buf := make([]processorPowerInformation, n)
	size := uintptr(n) * unsafe.Sizeof(processorPowerInformation{})
	status, _, _ := procCallNtPowerInformation.Call(
		processorInformation, 0, 0,
		uintptr(unsafe.Pointer(&buf[0])), size,
	)
	if status != 0 {
		return 0, errors.New("CallNtPowerInformation に失敗しました")
	}
	if buf[0].MaxMhz == 0 {
		return 0, errors.New("ベースクロックが 0 です")
	}
	return float64(buf[0].MaxMhz), nil
}

// InitCPUFreq は現在クロック取得用の PDH クエリを初期化する。アプリ起動時に一度だけ呼ぶ。
// カウンタは "% Processor Performance"（ベースクロックに対する実効クロックの割合）で、
// タスクマネージャーの「速度」と同じ算出方法。ターボ時は 100% を超える。
// カウンタ名はロケール依存のため、日本語 Windows でも英語名で登録できる
// PdhAddEnglishCounterW を使う。
func InitCPUFreq() {
	freqMu.Lock()
	defer freqMu.Unlock()
	if freqReady {
		return
	}
	base, err := baseClockMHz()
	if err != nil {
		return
	}

	var query uintptr
	if r, _, _ := procPdhOpenQuery.Call(0, 0, uintptr(unsafe.Pointer(&query))); r != 0 {
		return
	}
	path, err := syscall.UTF16PtrFromString(`\Processor Information(_Total)\% Processor Performance`)
	if err != nil {
		return
	}
	var counter uintptr
	if r, _, _ := procPdhAddEnglishCounter.Call(query, uintptr(unsafe.Pointer(path)), 0, uintptr(unsafe.Pointer(&counter))); r != 0 {
		return
	}
	// 差分カウンタなので、最初の 1 回はベースライン取得のために捨てる。
	procPdhCollectQueryData.Call(query)

	freqQuery = query
	freqCounter = counter
	freqBaseMHz = base
	freqReady = true
}

// GetCPUFreq は現在の実効クロックを返す。InitCPUFreq が成功していない場合や
// 計測に失敗した場合は nil を返す（UI 側は「---」表示にフォールバックする）。
func GetCPUFreq() *CPUFreqInfo {
	freqMu.Lock()
	defer freqMu.Unlock()
	if !freqReady {
		return nil
	}
	if r, _, _ := procPdhCollectQueryData.Call(freqQuery); r != 0 {
		return nil
	}
	var value pdhFmtCounterValue
	var valueType uint32
	r, _, _ := procPdhGetFormattedCounterValue.Call(
		freqCounter, pdhFmtDouble,
		uintptr(unsafe.Pointer(&valueType)),
		uintptr(unsafe.Pointer(&value)),
	)
	if r != 0 {
		return nil
	}
	percent := value.DoubleValue
	if percent < 0 {
		percent = 0
	}
	// ターボ時は 100% を超えるのが正常なので上限クランプはしない。
	return &CPUFreqInfo{
		CurrentMHz: freqBaseMHz * percent / 100,
		BaseMHz:    freqBaseMHz,
	}
}
