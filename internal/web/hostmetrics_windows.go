package web

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

var metricsKernel32 = windows.NewLazySystemDLL("kernel32.dll")
var metricsGetSystemTimes = metricsKernel32.NewProc("GetSystemTimes")
var metricsGlobalMemoryStatusEx = metricsKernel32.NewProc("GlobalMemoryStatusEx")

type metricsFiletime struct{ low, high uint32 }

func (value metricsFiletime) ticks() uint64 { return uint64(value.high)<<32 | uint64(value.low) }

func readCPUTicks() (idle, total uint64, ok bool) {
	var idleTime, kernelTime, userTime metricsFiletime
	result, _, _ := metricsGetSystemTimes.Call(uintptr(unsafe.Pointer(&idleTime)), uintptr(unsafe.Pointer(&kernelTime)), uintptr(unsafe.Pointer(&userTime)))
	if result == 0 {
		return 0, 0, false
	}
	return idleTime.ticks(), kernelTime.ticks() + userTime.ticks(), true
}

type metricsMemoryStatus struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

func readSystemMemory() (used, total uint64, ok bool) {
	status := metricsMemoryStatus{Length: uint32(unsafe.Sizeof(metricsMemoryStatus{}))}
	result, _, _ := metricsGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&status)))
	if result == 0 || status.TotalPhys == 0 || status.AvailPhys > status.TotalPhys {
		return 0, 0, false
	}
	return status.TotalPhys - status.AvailPhys, status.TotalPhys, true
}
