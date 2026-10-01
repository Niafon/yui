//go:build windows

package inference

import (
	"path/filepath"
	"syscall"
	"unsafe"
)

type filetime struct{ Low, High uint32 }
type memoryStatusEx struct {
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

type platformSampler struct{ prevIdle, prevKernel, prevUser uint64 }

var (
	kernel32                   = syscall.NewLazyDLL("kernel32.dll")
	user32                     = syscall.NewLazyDLL("user32.dll")
	getSystemTimes             = kernel32.NewProc("GetSystemTimes")
	globalMemoryStatusEx       = kernel32.NewProc("GlobalMemoryStatusEx")
	openProcess                = kernel32.NewProc("OpenProcess")
	closeHandle                = kernel32.NewProc("CloseHandle")
	queryFullProcessImageNameW = kernel32.NewProc("QueryFullProcessImageNameW")
	getForegroundWindow        = user32.NewProc("GetForegroundWindow")
	getWindowThreadProcessId   = user32.NewProc("GetWindowThreadProcessId")
)

func newPlatformSampler() platformSampler { return platformSampler{} }
func ft(v filetime) uint64                { return uint64(v.High)<<32 | uint64(v.Low) }

func (p *platformSampler) sample() Snapshot {
	s := Snapshot{}
	var idle, kernel, user filetime
	if ok, _, _ := getSystemTimes.Call(uintptr(unsafe.Pointer(&idle)), uintptr(unsafe.Pointer(&kernel)), uintptr(unsafe.Pointer(&user))); ok != 0 {
		i, k, u := ft(idle), ft(kernel), ft(user)
		if p.prevKernel > 0 {
			dIdle, dKernel, dUser := i-p.prevIdle, k-p.prevKernel, u-p.prevUser
			total := dKernel + dUser
			if total > 0 {
				s.CPUPercent = 100 * float64(total-dIdle) / float64(total)
			}
		}
		p.prevIdle, p.prevKernel, p.prevUser = i, k, u
	}
	mem := memoryStatusEx{Length: uint32(unsafe.Sizeof(memoryStatusEx{}))}
	if ok, _, _ := globalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&mem))); ok != 0 {
		s.RAMUsedMB = int((mem.TotalPhys - mem.AvailPhys) / 1024 / 1024)
		s.RAMFreeMB = int(mem.AvailPhys / 1024 / 1024)
	}
	s.ForegroundProcess = foregroundProcess()
	return s
}

func foregroundProcess() string {
	hwnd, _, _ := getForegroundWindow.Call()
	if hwnd == 0 {
		return ""
	}
	var pid uint32
	getWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	if pid == 0 {
		return ""
	}
	const processQueryLimitedInformation = 0x1000
	h, _, _ := openProcess.Call(processQueryLimitedInformation, 0, uintptr(pid))
	if h == 0 {
		return ""
	}
	defer closeHandle.Call(h)
	buf := make([]uint16, 1024)
	sz := uint32(len(buf))
	ok, _, _ := queryFullProcessImageNameW.Call(h, 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&sz)))
	if ok == 0 || sz == 0 {
		return ""
	}
	return filepath.Base(syscall.UTF16ToString(buf[:sz]))
}
