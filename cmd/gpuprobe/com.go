package main

import (
	"fmt"
	"syscall"
	"unsafe"
)

type guid struct {
	A    uint32
	B, C uint16
	D    [8]byte
}

func method(obj unsafe.Pointer, n uintptr, args ...uintptr) uintptr {
	vtable := *(*unsafe.Pointer)(obj)
	fn := *(*uintptr)(unsafe.Add(vtable, n*unsafe.Sizeof(uintptr(0))))
	r, _, _ := syscall.SyscallN(fn, append([]uintptr{uintptr(obj)}, args...)...)
	return r
}
func failed(hr uintptr) bool { return int32(hr) < 0 }
func release(obj unsafe.Pointer) {
	if obj != nil {
		method(obj, 2)
	}
}

// IDXGIFactory1 vtable: IUnknown 0..2, IDXGIObject 3..6, IDXGIFactory 7..11,
// IDXGIFactory1::EnumAdapters1 12. IDXGIAdapter1::GetDesc1 is index 10.
type dxgiDesc struct {
	Description                                                     [128]uint16
	VendorID, DeviceID, SubSysID, Revision                          uint32
	DedicatedVideoMemory, DedicatedSystemMemory, SharedSystemMemory uint64
	LUID                                                            luid
	Flags                                                           uint32
}

func printDXGI() {
	dll := syscall.NewLazyDLL("dxgi.dll")
	p := dll.NewProc("CreateDXGIFactory1")
	if e := p.Find(); e != nil {
		fmt.Printf("DXGI unavailable: %v\n", e)
		return
	}
	iid := guid{0x770aae78, 0xf26f, 0x4dba, [8]byte{0xa8, 0x29, 0x25, 0x3c, 0x83, 0xd1, 0xb3, 0x87}}
	var factory unsafe.Pointer
	if h := status(p, uintptr(unsafe.Pointer(&iid)), uintptr(unsafe.Pointer(&factory))); int32(h) < 0 {
		fmt.Printf("DXGI factory HRESULT=0x%X\n", h)
		return
	}
	defer release(factory)
	for i := uintptr(0); i < 64; i++ {
		var obj unsafe.Pointer
		h := method(factory, 12, i, uintptr(unsafe.Pointer(&obj)))
		if uint32(h) == 0x887A0002 {
			break
		}
		if failed(h) {
			fmt.Printf("DXGI EnumAdapters1[%d] HRESULT=0x%X\n", i, h)
			break
		}
		var d dxgiDesc
		h = method(obj, 10, uintptr(unsafe.Pointer(&d)))
		release(obj)
		if failed(h) {
			fmt.Printf("DXGI GetDesc1[%d] HRESULT=0x%X\n", i, h)
			continue
		}
		fmt.Printf("DXGI %s name=%q dedicated=%d shared=%d flags=0x%X\n", d.LUID, trim16(d.Description[:]), d.DedicatedVideoMemory, d.SharedSystemMemory, d.Flags)
	}
}

func printDXCore() {
	dll := syscall.NewLazyDLL("dxcore.dll")
	p := dll.NewProc("DXCoreCreateAdapterFactory")
	if e := p.Find(); e != nil {
		fmt.Printf("DXCore unavailable: %v\n", e)
		return
	}
	factoryIID := guid{0x78ee5945, 0xc36e, 0x4b13, [8]byte{0xa6, 0x69, 0, 0x5d, 0xd1, 0x1c, 0x0f, 0x06}}
	listIID := guid{0x526c7776, 0x40e9, 0x459b, [8]byte{0xb7, 0x11, 0xf3, 0x2a, 0xd7, 0x6d, 0xfc, 0x28}}
	adapterIID := guid{0xf0db4c7f, 0xfe5a, 0x42a2, [8]byte{0xbd, 0x62, 0xf2, 0xa6, 0xcf, 0x6f, 0xc8, 0x3e}}
	npu := guid{0xd46140c4, 0xadd7, 0x451b, [8]byte{0x9e, 0x56, 0x06, 0xfe, 0x8c, 0x3b, 0x58, 0xed}}
	compute := guid{0xe0b195da, 0x58ef, 0x4a22, [8]byte{0x90, 0xf1, 0x1f, 0x28, 0x16, 0x9c, 0xab, 0x8d}}
	graphics := guid{0xb69eb219, 0x3ded, 0x4464, [8]byte{0x97, 0x9f, 0xa0, 0x0b, 0xd4, 0x68, 0x70, 0x06}}
	var factory unsafe.Pointer
	if h := status(p, uintptr(unsafe.Pointer(&factoryIID)), uintptr(unsafe.Pointer(&factory))); int32(h) < 0 {
		fmt.Printf("DXCore factory HRESULT=0x%X\n", h)
		return
	}
	defer release(factory)
	// CreateAdapterList is factory vtable index 3. Listing three categories
	// independently also works on systems where the NPU attribute is unavailable.
	for _, kind := range []struct {
		name string
		g    guid
	}{{"NPU", npu}, {"ComputeAccelerator", compute}, {"GPU", graphics}} {
		var list unsafe.Pointer
		h := method(factory, 3, 1, uintptr(unsafe.Pointer(&kind.g)), uintptr(unsafe.Pointer(&listIID)), uintptr(unsafe.Pointer(&list)))
		if failed(h) {
			fmt.Printf("DXCore %s list HRESULT=0x%X\n", kind.name, h)
			continue
		}
		count := method(list, 4)
		fmt.Printf("DXCore %s count=%d\n", kind.name, count)
		for i := uintptr(0); i < count; i++ {
			var obj unsafe.Pointer
			h = method(list, 3, i, uintptr(unsafe.Pointer(&adapterIID)), uintptr(unsafe.Pointer(&obj)))
			if failed(h) {
				fmt.Printf("DXCore GetAdapter HRESULT=0x%X\n", h)
				continue
			}
			var l luid
			h = method(obj, 6, 0, unsafe.Sizeof(l), uintptr(unsafe.Pointer(&l)))
			if failed(h) {
				fmt.Printf("DXCore InstanceLuid HRESULT=0x%X\n", h)
			} else {
				fmt.Printf("DXCore %s LUID=%s\n", kind.name, l)
			}
			release(obj)
		}
		release(list)
	}
}
