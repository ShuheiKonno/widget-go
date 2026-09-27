package sysinfo

import (
	"math"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"unsafe"
)

type GPUInfo struct {
	Name                          string
	TotalPct, ComputePct          *float64
	DedicatedUsed, DedicatedTotal uint64
	SharedUsed, SharedTotal       uint64
}

type NPUInfo struct {
	Name     string
	UsagePct *float64
	MemUsed  uint64
}

type gpuLUID struct {
	Low  uint32
	High int32
}
type gpuAdapterInfo struct {
	Handle           uint32
	LUID             gpuLUID
	Sources, Precise uint32
}
type gpuEnum struct {
	Filter   uint64
	Count    uint32
	_        uint32
	Adapters uintptr
}
type gpuQuery struct {
	Handle, Type uint32
	Data         uintptr
	Size         uint32
	_            uint32
}
type gpuSegments struct{ DedicatedVideo, DedicatedSystem, SharedSystem uint64 }
type gpuAdapter struct {
	LUID     gpuLUID
	Flags    uint32
	Name     string
	Segments gpuSegments
}
type gpuValue struct {
	Status uint32
	_      uint32
	Value  float64
}
type gpuItem struct {
	Name  *uint16
	Value gpuValue
}
type gpuSample struct {
	Name  string
	Value float64
}
type gpuInstance struct {
	LUID      gpuLUID
	Phys, Eng uint32
	Type      string
}
type gpuEngineKey struct {
	LUID      gpuLUID
	Phys, Eng uint32
}
type gpuUsage struct{ Total, Compute *float64 }

var (
	gpuGDI                   = syscall.NewLazyDLL("gdi32.dll")
	gpuEnumProc              = gpuGDI.NewProc("D3DKMTEnumAdapters3")
	gpuQueryProc             = gpuGDI.NewProc("D3DKMTQueryAdapterInfo")
	gpuCloseProc             = gpuGDI.NewProc("D3DKMTCloseAdapter")
	gpuDXCore                = syscall.NewLazyDLL("dxcore.dll").NewProc("DXCoreCreateAdapterFactory")
	gpuInstanceRE            = regexp.MustCompile(`(?i)^pid_\d+_luid_0x([0-9a-f]{8})_0x([0-9a-f]{8})_phys_(\d+)_eng_(\d+)_engtype_(.*)$`)
	gpuMemoryRE              = regexp.MustCompile(`(?i)^luid_0x([0-9a-f]{8})_0x([0-9a-f]{8})_phys_\d+$`)
	gpuMu                    sync.Mutex
	gpuReady                 bool
	gpuSupported             bool
	gpuSelected, npuSelected *gpuAdapter
	gpuKnown                 map[gpuLUID]bool
	gpuQueryHandle           uintptr
	gpuCounters              [3]uintptr
	gpuPrimed                bool
	gpuMissing               int
	gpuMalformed             int // diagnostic count of malformed engine names in the latest sample
)

const gpuMissingLimit = 5

func gpuCall(p *syscall.LazyProc, args ...uintptr) uint32 {
	r, _, _ := p.Call(args...)
	return uint32(r)
}

func gpuQueryAdapter(handle, kind uint32, data unsafe.Pointer, size uintptr) uint32 {
	q := gpuQuery{Handle: handle, Type: kind, Data: uintptr(data), Size: uint32(size)}
	return gpuCall(gpuQueryProc, uintptr(unsafe.Pointer(&q)))
}

func gpuString(v []uint16) string {
	for i, x := range v {
		if x == 0 {
			return syscall.UTF16ToString(v[:i])
		}
	}
	return syscall.UTF16ToString(v)
}

func gpuPointerString(p *uint16) string {
	if p == nil {
		return ""
	}
	n := 0
	for n < 32768 && *(*uint16)(unsafe.Add(unsafe.Pointer(p), n*2)) != 0 {
		n++
	}
	return syscall.UTF16ToString(unsafe.Slice(p, n))
}

func gpuEnumerate() ([]gpuAdapter, bool) {
	for _, p := range []*syscall.LazyProc{gpuEnumProc, gpuQueryProc, gpuCloseProc} {
		if p.Find() != nil {
			return nil, false
		}
	}
	e := gpuEnum{Filter: 1}
	if gpuCall(gpuEnumProc, uintptr(unsafe.Pointer(&e))) != 0 {
		return nil, false
	}
	if e.Count == 0 {
		return nil, true
	}
	if e.Count > 256 {
		return nil, false
	}
	items := make([]gpuAdapterInfo, e.Count)
	e.Adapters = uintptr(unsafe.Pointer(&items[0]))
	if gpuCall(gpuEnumProc, uintptr(unsafe.Pointer(&e))) != 0 || e.Count > uint32(len(items)) {
		return nil, false
	}
	result := make([]gpuAdapter, 0, e.Count)
	for _, item := range items[:e.Count] {
		a := gpuAdapter{LUID: item.LUID}
		func() {
			defer gpuCall(gpuCloseProc, uintptr(unsafe.Pointer(&struct{ Handle uint32 }{item.Handle})))
			var name [4096]uint16
			if gpuQueryAdapter(item.Handle, 65, unsafe.Pointer(&name[0]), unsafe.Sizeof(name)) == 0 {
				a.Name = gpuString(name[:])
			}
			_ = gpuQueryAdapter(item.Handle, 3, unsafe.Pointer(&a.Segments), unsafe.Sizeof(a.Segments))
			_ = gpuQueryAdapter(item.Handle, 15, unsafe.Pointer(&a.Flags), unsafe.Sizeof(a.Flags))
		}()
		result = append(result, a)
	}
	return result, true
}

type gpuGUID struct {
	A    uint32
	B, C uint16
	D    [8]byte
}

func gpuMethod(obj unsafe.Pointer, index uintptr, args ...uintptr) uintptr {
	vt := *(*unsafe.Pointer)(obj)
	fn := *(*uintptr)(unsafe.Add(vt, index*unsafe.Sizeof(uintptr(0))))
	r, _, _ := syscall.SyscallN(fn, append([]uintptr{uintptr(obj)}, args...)...)
	return r
}
func gpuRelease(obj unsafe.Pointer) {
	if obj != nil {
		gpuMethod(obj, 2)
	}
}

// DXCore objects are acquired and released on this goroutine; no COM apartment is touched.
func gpuNPULUIDs() map[gpuLUID]bool {
	result := map[gpuLUID]bool{}
	if gpuDXCore.Find() != nil {
		return result
	}
	factoryIID := gpuGUID{0x78ee5945, 0xc36e, 0x4b13, [8]byte{0xa6, 0x69, 0, 0x5d, 0xd1, 0x1c, 0x0f, 0x06}}
	listIID := gpuGUID{0x526c7776, 0x40e9, 0x459b, [8]byte{0xb7, 0x11, 0xf3, 0x2a, 0xd7, 0x6d, 0xfc, 0x28}}
	adapterIID := gpuGUID{0xf0db4c7f, 0xfe5a, 0x42a2, [8]byte{0xbd, 0x62, 0xf2, 0xa6, 0xcf, 0x6f, 0xc8, 0x3e}}
	npuAttribute := gpuGUID{0xd46140c4, 0xadd7, 0x451b, [8]byte{0x9e, 0x56, 0x06, 0xfe, 0x8c, 0x3b, 0x58, 0xed}}
	var factory unsafe.Pointer
	if int32(gpuCall(gpuDXCore, uintptr(unsafe.Pointer(&factoryIID)), uintptr(unsafe.Pointer(&factory)))) < 0 || factory == nil {
		return result
	}
	defer gpuRelease(factory)
	var list unsafe.Pointer
	if int32(gpuMethod(factory, 3, 1, uintptr(unsafe.Pointer(&npuAttribute)), uintptr(unsafe.Pointer(&listIID)), uintptr(unsafe.Pointer(&list)))) < 0 || list == nil {
		return result
	}
	defer gpuRelease(list)
	count := gpuMethod(list, 4)
	if count > 256 {
		return result
	}
	for i := uintptr(0); i < count; i++ {
		var obj unsafe.Pointer
		if int32(gpuMethod(list, 3, i, uintptr(unsafe.Pointer(&adapterIID)), uintptr(unsafe.Pointer(&obj)))) < 0 || obj == nil {
			continue
		}
		var l gpuLUID
		if int32(gpuMethod(obj, 6, 0, unsafe.Sizeof(l), uintptr(unsafe.Pointer(&l)))) >= 0 {
			result[l] = true
		}
		gpuRelease(obj)
	}
	return result
}

func gpuKind(a gpuAdapter, npus map[gpuLUID]bool) string {
	if a.Flags&(1<<2|1<<6) != 0 {
		return ""
	}
	if npus[a.LUID] {
		return "npu"
	}
	if a.Flags&1 != 0 && a.Flags&(1<<11) == 0 {
		return "gpu"
	}
	return ""
}

func gpuChoose(adapters []gpuAdapter, npus map[gpuLUID]bool) (*gpuAdapter, *gpuAdapter) {
	var gpu, npu *gpuAdapter
	for i := range adapters {
		a := &adapters[i]
		switch gpuKind(*a, npus) {
		case "npu":
			if npu == nil {
				npu = a
			}
		case "gpu":
			if gpu == nil || (a.Segments.DedicatedVideo >= 512<<20 && a.Segments.DedicatedVideo > gpu.Segments.DedicatedVideo) {
				gpu = a
			}
		}
	}
	return gpu, npu
}

func gpuParseLUID(hi, lo string) (gpuLUID, bool) {
	h, e1 := strconv.ParseUint(hi, 16, 32)
	l, e2 := strconv.ParseUint(lo, 16, 32)
	return gpuLUID{uint32(l), int32(h)}, e1 == nil && e2 == nil
}
func gpuParseInstance(name string) (gpuInstance, bool) {
	m := gpuInstanceRE.FindStringSubmatch(name)
	if m == nil {
		return gpuInstance{}, false
	}
	l, ok := gpuParseLUID(m[1], m[2])
	p, e1 := strconv.ParseUint(m[3], 10, 32)
	e, e2 := strconv.ParseUint(m[4], 10, 32)
	return gpuInstance{l, uint32(p), uint32(e), m[5]}, ok && e1 == nil && e2 == nil
}
func gpuComputeType(s string) bool {
	s = strings.ToLower(s)
	return strings.Contains(s, "compute") || strings.Contains(s, "cuda")
}
func gpuAggregate(samples []gpuSample) (map[gpuLUID]gpuUsage, int) {
	sums := map[gpuEngineKey]float64{}
	compute := map[gpuEngineKey]bool{}
	bad := 0
	for _, s := range samples {
		v, ok := gpuParseInstance(s.Name)
		if !ok {
			bad++
			continue
		}
		if math.IsNaN(s.Value) || math.IsInf(s.Value, 0) {
			continue
		}
		k := gpuEngineKey{v.LUID, v.Phys, v.Eng}
		sums[k] += s.Value
		compute[k] = compute[k] || gpuComputeType(v.Type)
	}
	usage := map[gpuLUID]gpuUsage{}
	for k, v := range sums {
		u := usage[k.LUID]
		if u.Total == nil || v > *u.Total {
			x := v
			u.Total = &x
		}
		if compute[k] && (u.Compute == nil || v > *u.Compute) {
			x := v
			u.Compute = &x
		}
		usage[k.LUID] = u
	}
	return usage, bad
}

func gpuMemory(samples []gpuSample) map[gpuLUID]uint64 {
	result := map[gpuLUID]uint64{}
	for _, s := range samples {
		m := gpuMemoryRE.FindStringSubmatch(s.Name)
		if m == nil || math.IsNaN(s.Value) || math.IsInf(s.Value, 0) || s.Value < 0 {
			continue
		}
		l, ok := gpuParseLUID(m[1], m[2])
		if !ok {
			continue
		}
		result[l] += uint64(s.Value)
	}
	return result
}

func gpuClosePDH() {
	if gpuQueryHandle != 0 {
		gpuCall(procPdhCloseQuery, gpuQueryHandle)
	}
	gpuQueryHandle = 0
	gpuCounters = [3]uintptr{}
	gpuPrimed = false
}
func gpuOpenPDH() bool {
	for _, p := range []*syscall.LazyProc{procPdhOpenQuery, procPdhAddEnglishCounter, procPdhCollectQueryData, procPdhGetFormattedCounterArray, procPdhCloseQuery} {
		if p.Find() != nil {
			return false
		}
	}
	if gpuCall(procPdhOpenQuery, 0, 0, uintptr(unsafe.Pointer(&gpuQueryHandle))) != 0 {
		return false
	}
	paths := []string{`\GPU Engine(*)\Utilization Percentage`, `\GPU Adapter Memory(*)\Dedicated Usage`, `\GPU Adapter Memory(*)\Shared Usage`}
	for i, path := range paths {
		p := syscall.StringToUTF16Ptr(path)
		if gpuCall(procPdhAddEnglishCounter, gpuQueryHandle, uintptr(unsafe.Pointer(p)), 0, uintptr(unsafe.Pointer(&gpuCounters[i]))) != 0 {
			gpuClosePDH()
			return false
		}
	}
	_ = gpuCall(procPdhCollectQueryData, gpuQueryHandle) // discard first sample
	gpuPrimed = true
	return true
}
func gpuValues(handle uintptr) ([]gpuSample, bool) {
	var size, count uint32
	s := gpuCall(procPdhGetFormattedCounterArray, handle, pdhFmtDouble, uintptr(unsafe.Pointer(&size)), uintptr(unsafe.Pointer(&count)), 0)
	if s != 0x800007D2 && s != 0 {
		return nil, false
	}
	if size == 0 {
		return nil, true
	}
	if size > 64<<20 {
		return nil, false
	}
	buf := make([]byte, int(size)+8)
	base := unsafe.Pointer(&buf[0])
	aligned := unsafe.Add(base, (8-uintptr(base)%8)%8)
	s = gpuCall(procPdhGetFormattedCounterArray, handle, pdhFmtDouble, uintptr(unsafe.Pointer(&size)), uintptr(unsafe.Pointer(&count)), uintptr(aligned))
	if s != 0 || uint64(count)*uint64(unsafe.Sizeof(gpuItem{})) > uint64(len(buf)) {
		return nil, false
	}
	out := make([]gpuSample, 0, count)
	for _, item := range unsafe.Slice((*gpuItem)(aligned), count) {
		if (item.Value.Status == 0 || item.Value.Status == 1) && !math.IsNaN(item.Value.Value) && !math.IsInf(item.Value.Value, 0) {
			out = append(out, gpuSample{gpuPointerString(item.Name), item.Value.Value})
		}
	}
	return out, true
}

func gpuRediscover() bool {
	adapters, ok := gpuEnumerate()
	if !ok {
		gpuSelected = nil
		npuSelected = nil
		gpuReady = false
		gpuSupported = false
		gpuClosePDH()
		return false
	}
	gpuSupported = true
	gpuSelected, npuSelected = gpuChoose(adapters, gpuNPULUIDs())
	gpuKnown = make(map[gpuLUID]bool, len(adapters))
	for _, a := range adapters {
		gpuKnown[a.LUID] = true
	}
	gpuMissing = 0
	return true
}

// InitGPU initializes adapter classification and PDH sampling. It does not initialize COM.
func InitGPU() {
	gpuMu.Lock()
	defer gpuMu.Unlock()
	gpuClosePDH()
	gpuReady = false
	gpuSupported = false
	gpuSelected = nil
	npuSelected = nil
	if !gpuRediscover() {
		return
	}
	// Device presence remains known if PDH is temporarily unavailable.
	if gpuOpenPDH() {
		gpuReady = true
	}
}

func gpuResult() (*GPUInfo, *NPUInfo) {
	var g *GPUInfo
	var n *NPUInfo
	if gpuSelected != nil {
		a := gpuSelected
		g = &GPUInfo{Name: a.Name, DedicatedTotal: a.Segments.DedicatedVideo, SharedTotal: a.Segments.SharedSystem}
	}
	if npuSelected != nil {
		n = &NPUInfo{Name: npuSelected.Name}
	}
	return g, n
}

// GetGPUInfo returns fresh snapshots. A temporary PDH failure clears only measurements.
func GetGPUInfo() (*GPUInfo, *NPUInfo) {
	gpuMu.Lock()
	defer gpuMu.Unlock()
	g, n := gpuResult()
	if !gpuSupported {
		return nil, nil
	}
	if !gpuReady {
		if !gpuOpenPDH() {
			return g, n
		}
		gpuReady = true
		return g, n
	}
	if !gpuPrimed || gpuQueryHandle == 0 {
		return g, n
	}
	if gpuCall(procPdhCollectQueryData, gpuQueryHandle) != 0 {
		return g, n
	}
	var values [3][]gpuSample
	for i, h := range gpuCounters {
		var ok bool
		values[i], ok = gpuValues(h)
		if !ok {
			return g, n
		}
	}
	usage, bad := gpuAggregate(values[0])
	gpuMalformed = bad
	ded := gpuMemory(values[1])
	shared := gpuMemory(values[2])
	unknown := false
	for l := range usage {
		if !gpuKnown[l] {
			unknown = true
			break
		}
	}
	for l := range ded {
		if !gpuKnown[l] {
			unknown = true
			break
		}
	}
	for l := range shared {
		if !gpuKnown[l] {
			unknown = true
			break
		}
	}
	if gpuSelected != nil {
		if _, ok := usage[gpuSelected.LUID]; ok {
			gpuMissing = 0
		} else {
			gpuMissing++
		}
	}
	if unknown || gpuMissing >= gpuMissingLimit {
		if !gpuRediscover() {
			return nil, nil
		}
		g, n = gpuResult()
	}
	if g != nil {
		l := gpuSelected.LUID
		g.TotalPct = usage[l].Total
		g.ComputePct = usage[l].Compute
		g.DedicatedUsed = ded[l]
		g.SharedUsed = shared[l]
	}
	if n != nil {
		l := npuSelected.LUID
		n.UsagePct = usage[l].Total
		n.MemUsed = shared[l] + ded[l]
	}
	runtime.KeepAlive(values)
	return g, n
}
