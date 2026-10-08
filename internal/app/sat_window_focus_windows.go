//go:build windows

package app

import (
	"syscall"
	"unsafe"
)

var (
	user32            = syscall.NewLazyDLL("user32.dll")
	procFindWindowW   = user32.NewProc("FindWindowW")
	procShowWindow    = user32.NewProc("ShowWindow")
	procSetForeground = user32.NewProc("SetForegroundWindow")
)

const swRestore = 9

// focusSatelliteWindow brings the existing SAT top-level window to the foreground
// without changing its persistent Always-On-Top state.
func focusSatelliteWindow() bool {
	title, err := syscall.UTF16PtrFromString("IC-9700 SAT")
	if err != nil {
		return false
	}
	hwnd, _, _ := procFindWindowW.Call(0, uintptr(unsafe.Pointer(title)))
	if hwnd == 0 {
		return false
	}
	_, _, _ = procShowWindow.Call(hwnd, swRestore)
	ret, _, _ := procSetForeground.Call(hwnd)
	return ret != 0
}
