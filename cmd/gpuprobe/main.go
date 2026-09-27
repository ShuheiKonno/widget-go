// gpuprobe is a standalone, CGO-free Windows GPU/NPU API probe.
package main

import (
	"flag"
	"fmt"
	"math"
	"os"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

type luid struct {
	Low  uint32
	High int32
}

func (l luid) String() string { return fmt.Sprintf("0x%08X_0x%08X", uint32(l.High), l.Low) }

type adapterInfo struct {
	Handle  uint32
	LUID    luid
	Sources uint32
	Precise uint32
}
type enumAdapters struct {
	Filter   uint64
	Count    uint32
	_        uint32
	Adapters uintptr
}
type queryInfo struct {
	Handle uint32
	Type   uint32
	Data   uintptr
	Size   uint32
	_      uint32
}
type segmentSize struct {
	DedicatedVideo  uint64
	DedicatedSystem uint64
	SharedSystem    uint64
}
type adapter struct {
	LUID                  luid
	Flags                 uint32
	Name                  string
	Capacity              segmentSize
	NameStatus, CapStatus uint32
}
type fmtValue struct {
	Status uint32
	_      uint32
	Value  float64
}
type fmtItem struct {
	Name  *uint16
	Value fmtValue
}
type counter struct {
	query, handle uintptr
	label         string
	wildcard      bool
}
type sample struct {
	Name  string
	Value float64
}
type timings struct{ Collect, Format, Parse time.Duration }

var (
	gdi            = syscall.NewLazyDLL("gdi32.dll")
	enumProc       = gdi.NewProc("D3DKMTEnumAdapters3")
	queryProc      = gdi.NewProc("D3DKMTQueryAdapterInfo")
	closeProc      = gdi.NewProc("D3DKMTCloseAdapter")
	pdh            = syscall.NewLazyDLL("pdh.dll")
	openQuery      = pdh.NewProc("PdhOpenQueryW")
	closeQuery     = pdh.NewProc("PdhCloseQuery")
	addEnglish     = pdh.NewProc("PdhAddEnglishCounterW")
	collectData    = pdh.NewProc("PdhCollectQueryData")
	formattedArray = pdh.NewProc("PdhGetFormattedCounterArrayW")
	expandPath     = pdh.NewProc("PdhExpandWildCardPathW")
	instanceRE     = regexp.MustCompile(`(?i)^pid_\d+_luid_0x([0-9a-f]{8})_0x([0-9a-f]{8})_phys_(\d+)_eng_(\d+)_engtype_(.*)$`)
	memoryRE       = regexp.MustCompile(`(?i)^luid_0x([0-9a-f]{8})_0x([0-9a-f]{8})_phys_\d+$`)
)

func status(p *syscall.LazyProc, args ...uintptr) uint32 {
	r, _, _ := p.Call(args...)
	return uint32(r)
}
func query(h uint32, typ uint32, data unsafe.Pointer, size uintptr) uint32 {
	q := queryInfo{Handle: h, Type: typ, Data: uintptr(data), Size: uint32(size)}
	return status(queryProc, uintptr(unsafe.Pointer(&q)))
}
func utf16(p *uint16) string {
	if p == nil {
		return ""
	}
	n := 0
	for *(*uint16)(unsafe.Add(unsafe.Pointer(p), n*2)) != 0 && n < 32768 {
		n++
	}
	return syscall.UTF16ToString(unsafe.Slice(p, n))
}
func trim16(v []uint16) string {
	for i, x := range v {
		if x == 0 {
			return syscall.UTF16ToString(v[:i])
		}
	}
	return syscall.UTF16ToString(v)
}
func adapters() ([]adapter, error) {
	for _, p := range []*syscall.LazyProc{enumProc, queryProc, closeProc} {
		if e := p.Find(); e != nil {
			return nil, e
		}
	}
	e := enumAdapters{Filter: 1}
	s := status(enumProc, uintptr(unsafe.Pointer(&e)))
	if s != 0 {
		return nil, fmt.Errorf("EnumAdapters3 count NTSTATUS=0x%08X", s)
	}
	infos := make([]adapterInfo, e.Count)
	if len(infos) == 0 {
		return nil, nil
	}
	e.Adapters = uintptr(unsafe.Pointer(&infos[0]))
	e.Count = uint32(len(infos))
	s = status(enumProc, uintptr(unsafe.Pointer(&e)))
	if s != 0 {
		return nil, fmt.Errorf("EnumAdapters3 items NTSTATUS=0x%08X", s)
	}
	out := make([]adapter, 0, e.Count)
	for _, v := range infos[:e.Count] {
		a := adapter{LUID: v.LUID}
		func() {
			defer closeProc.Call(uintptr(unsafe.Pointer(&struct{ H uint32 }{v.Handle})))
			var name [4096]uint16
			a.NameStatus = query(v.Handle, 65, unsafe.Pointer(&name[0]), unsafe.Sizeof(name))
			if a.NameStatus == 0 {
				a.Name = trim16(name[:])
			}
			a.CapStatus = query(v.Handle, 3, unsafe.Pointer(&a.Capacity), unsafe.Sizeof(a.Capacity))
			_ = query(v.Handle, 15, unsafe.Pointer(&a.Flags), unsafe.Sizeof(a.Flags))
		}()
		out = append(out, a)
	}
	return out, nil
}
func flagNames(x uint32) string {
	names := []string{"RenderSupported", "DisplaySupported", "SoftwareDevice", "PostDevice", "HybridDiscrete", "HybridIntegrated", "IndirectDisplayDevice", "Paravirtualized", "ACGSupported", "SupportSetTimingsFromVidPn", "Detachable", "ComputeOnly", "Prototype", "RuntimePowerManagement"}
	var out []string
	for i, n := range names {
		if x&(1<<i) != 0 {
			out = append(out, n)
		}
	}
	return strings.Join(out, ",")
}
func openCounter(label, path string) (*counter, error) {
	var q, h uintptr
	if s := status(openQuery, 0, 0, uintptr(unsafe.Pointer(&q))); s != 0 {
		return nil, fmt.Errorf("open %s: 0x%X", label, s)
	}
	p := syscall.StringToUTF16Ptr(path)
	if s := status(addEnglish, q, uintptr(unsafe.Pointer(p)), 0, uintptr(unsafe.Pointer(&h))); s != 0 {
		closeQuery.Call(q)
		return nil, fmt.Errorf("add %s: 0x%X", label, s)
	}
	return &counter{query: q, handle: h, label: label, wildcard: true}, nil
}
func (c *counter) close()          { closeQuery.Call(c.query) }
func (c *counter) collect() uint32 { return status(collectData, c.query) }
func (c *counter) values() ([]sample, uint32) {
	var bytes, count uint32
	s := status(formattedArray, c.handle, 0x200, uintptr(unsafe.Pointer(&bytes)), uintptr(unsafe.Pointer(&count)), 0)
	if s != 0x800007D2 && s != 0 {
		return nil, s
	}
	if bytes == 0 {
		return nil, 0
	}
	buf := make([]byte, int(bytes)+int(unsafe.Alignof(fmtItem{})))
	base := unsafe.Pointer(&buf[0])
	offset := (8 - uintptr(base)%8) % 8
	ptr := unsafe.Add(base, offset)
	s = status(formattedArray, c.handle, 0x200, uintptr(unsafe.Pointer(&bytes)), uintptr(unsafe.Pointer(&count)), uintptr(ptr))
	if s != 0 {
		return nil, s
	}
	items := unsafe.Slice((*fmtItem)(ptr), count)
	out := make([]sample, 0, count)
	for _, v := range items {
		if (v.Value.Status == 0 || v.Value.Status == 1) && !math.IsNaN(v.Value.Value) && !math.IsInf(v.Value.Value, 0) {
			out = append(out, sample{utf16(v.Name), v.Value.Value})
		}
	}
	return out, 0
}
func expandEnglish(localizedPath string) ([]string, uint32) {
	p := syscall.StringToUTF16Ptr(localizedPath)
	var n uint32
	s := status(expandPath, 0, uintptr(unsafe.Pointer(p)), 0, uintptr(unsafe.Pointer(&n)), 0)
	if s != 0x800007D2 {
		return nil, s
	}
	b := make([]uint16, n)
	s = status(expandPath, 0, uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(&b[0])), uintptr(unsafe.Pointer(&n)), 0)
	if s != 0 {
		return nil, s
	}
	var out []string
	start := 0
	for i, v := range b {
		if v == 0 {
			if i == start {
				break
			}
			out = append(out, syscall.UTF16ToString(b[start:i]))
			start = i + 1
		}
	}
	return out, 0
}
func printLayout() {
	fmt.Printf("layout x64: LUID size=%d low=%d high=%d; ADAPTERINFO size=%d handle=%d luid=%d sources=%d precise=%d\n", unsafe.Sizeof(luid{}), unsafe.Offsetof(luid{}.Low), unsafe.Offsetof(luid{}.High), unsafe.Sizeof(adapterInfo{}), unsafe.Offsetof(adapterInfo{}.Handle), unsafe.Offsetof(adapterInfo{}.LUID), unsafe.Offsetof(adapterInfo{}.Sources), unsafe.Offsetof(adapterInfo{}.Precise))
	fmt.Printf("layout x64: ENUMADAPTERS3 size=%d filter=%d count=%d ptr=%d; QUERYADAPTERINFO size=%d handle=%d type=%d data=%d size=%d\n", unsafe.Sizeof(enumAdapters{}), unsafe.Offsetof(enumAdapters{}.Filter), unsafe.Offsetof(enumAdapters{}.Count), unsafe.Offsetof(enumAdapters{}.Adapters), unsafe.Sizeof(queryInfo{}), unsafe.Offsetof(queryInfo{}.Handle), unsafe.Offsetof(queryInfo{}.Type), unsafe.Offsetof(queryInfo{}.Data), unsafe.Offsetof(queryInfo{}.Size))
	fmt.Printf("layout x64: SEGMENTSIZEINFO size=%d dedicated=%d dedicatedSystem=%d shared=%d; PDH_FMT_COUNTERVALUE size=%d status=%d double=%d; PDH_FMT_COUNTERVALUE_ITEM_W size=%d name=%d fmt=%d\n", unsafe.Sizeof(segmentSize{}), unsafe.Offsetof(segmentSize{}.DedicatedVideo), unsafe.Offsetof(segmentSize{}.DedicatedSystem), unsafe.Offsetof(segmentSize{}.SharedSystem), unsafe.Sizeof(fmtValue{}), unsafe.Offsetof(fmtValue{}.Status), unsafe.Offsetof(fmtValue{}.Value), unsafe.Sizeof(fmtItem{}), unsafe.Offsetof(fmtItem{}.Name), unsafe.Offsetof(fmtItem{}.Value))
	fmt.Printf("layout x64: DXGI_ADAPTER_DESC1 size=%d description=%d dedicated=%d shared=%d luid=%d flags=%d\n", unsafe.Sizeof(dxgiDesc{}), unsafe.Offsetof(dxgiDesc{}.Description), unsafe.Offsetof(dxgiDesc{}.DedicatedVideoMemory), unsafe.Offsetof(dxgiDesc{}.SharedSystemMemory), unsafe.Offsetof(dxgiDesc{}.LUID), unsafe.Offsetof(dxgiDesc{}.Flags))
}
func main() {
	interval := flag.Duration("interval", time.Second, "sample interval")
	duration := flag.Duration("duration", 0, "total duration (0: until interrupted)")
	flag.Parse()
	if runtime.GOARCH != "amd64" {
		fmt.Fprintln(os.Stderr, "x64 only")
		os.Exit(2)
	}
	printLayout()
	aa, err := adapters()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
	for _, a := range aa {
		fmt.Printf("adapter %s flags=0x%X [%s] name=%q dedicated=%d shared=%d nameStatus=0x%X capStatus=0x%X\n", a.LUID, a.Flags, flagNames(a.Flags), a.Name, a.Capacity.DedicatedVideo, a.Capacity.SharedSystem, a.NameStatus, a.CapStatus)
	}
	printDXGI()
	printDXCore()
	paths := []string{`\GPU Engine(*)\Utilization Percentage`, `\GPU Adapter Memory(*)\Dedicated Usage`, `\GPU Adapter Memory(*)\Shared Usage`}
	labels := []string{"engine", "dedicated", "shared"}
	var cs []*counter
	for i, p := range paths {
		c, e := openCounter(labels[i], p)
		if e != nil {
			fmt.Fprintln(os.Stderr, e)
			continue
		}
		defer c.close()
		cs = append(cs, c)
	}
	if len(cs) == 0 {
		return
	}
	for _, c := range cs {
		c.collect()
	}
	time.Sleep(*interval)
	start := time.Now()
	count := 0
	var total timings
	for {
		t := time.Now()
		for _, c := range cs {
			if s := c.collect(); s != 0 {
				fmt.Printf("collect %s status=0x%X\n", c.label, s)
			}
		}
		dCollect := time.Since(t)
		t = time.Now()
		values := map[string][]sample{}
		for _, c := range cs {
			v, s := c.values()
			if s != 0 {
				fmt.Printf("array %s status=0x%X\n", c.label, s)
			}
			values[c.label] = v
		}
		dFormat := time.Since(t)
		if count == 0 && len(values["engine"]) == 0 {
			fmt.Println("wildcard returned no engine items; PdhExpandWildCardPathW fallback follows (localized path may be required)")
			for _, p := range paths {
				v, s := expandEnglish(p)
				fmt.Printf("expand %s status=0x%X count=%d\n", p, s, len(v))
			}
		}
		t = time.Now()
		output := reportSamples(values, count == 0)
		dParse := time.Since(t)
		fmt.Print(output)
		total.Collect += dCollect
		total.Format += dFormat
		total.Parse += dParse
		count++
		fmt.Printf("timing sample=%d collect=%s format=%s parse=%s avgCollect=%s avgFormat=%s avgParse=%s\n", count, dCollect, dFormat, dParse, total.Collect/time.Duration(count), total.Format/time.Duration(count), total.Parse/time.Duration(count))
		if *duration > 0 && time.Since(start) >= *duration {
			break
		}
		time.Sleep(*interval)
	}
}
func reportSamples(v map[string][]sample, detail bool) string {
	var b strings.Builder
	if detail {
		types := map[string]bool{}
		for _, x := range v["engine"] {
			fmt.Fprintf(&b, "instance %s %.3f%%\n", x.Name, x.Value)
			if m := instanceRE.FindStringSubmatch(x.Name); m != nil {
				types[m[5]] = true
			}
		}
		var keys []string
		for k := range types {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		fmt.Fprintf(&b, "engtypes=%q\n", keys)
	}
	type key struct{ l, phys, eng string }
	group := map[key]float64{}
	compute := map[key]bool{}
	malformed := 0
	for _, x := range v["engine"] {
		m := instanceRE.FindStringSubmatch(x.Name)
		if m == nil {
			malformed++
			continue
		}
		k := key{m[1] + "_" + m[2], m[3], m[4]}
		group[k] += x.Value
		low := strings.ToLower(m[5])
		compute[k] = compute[k] || strings.Contains(low, "compute") || strings.Contains(low, "cuda")
	}
	type usage struct {
		all, comp   float64
		has         bool
		ded, shared float64
	}
	u := map[string]usage{}
	for k, x := range group {
		a := u[k.l]
		if x > a.all {
			a.all = x
		}
		if compute[k] {
			a.has = true
			if x > a.comp {
				a.comp = x
			}
		}
		u[k.l] = a
	}
	for _, label := range []string{"dedicated", "shared"} {
		for _, x := range v[label] {
			m := memoryRE.FindStringSubmatch(x.Name)
			if m == nil {
				continue
			}
			k := m[1] + "_" + m[2]
			a := u[k]
			if label == "dedicated" {
				a.ded += x.Value
			} else {
				a.shared += x.Value
			}
			u[k] = a
		}
	}
	keys := make([]string, 0, len(u))
	for k := range u {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		a := u[k]
		comp := "---"
		if a.has {
			comp = fmt.Sprintf("%.3f%%", a.comp)
		}
		fmt.Fprintf(&b, "sample %s all=%.3f%% compute=%s dedicated=%.0f shared=%.0f malformed=%d\n", k, a.all, comp, a.ded, a.shared, malformed)
	}
	return b.String()
}
