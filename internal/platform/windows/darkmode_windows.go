package windows

import (
	"syscall"
	"unsafe"
)

var (
	dwmapi                    = syscall.NewLazyDLL("dwmapi.dll")
	dwmSetWindowAttributeProc = dwmapi.NewProc("DwmSetWindowAttribute")
)

// ApplyDarkTitleBar asks the Desktop Window Manager to use the system dark
// title-bar treatment while leaving the FLTK client area under our theme.
// Attribute 20 is supported by current Windows 10/11; 19 is the older name
// used by some Windows 10 builds.
func ApplyDarkTitleBar(hwnd uintptr) bool {
	if hwnd == 0 {
		return false
	}
	useDark := uint32(1)
	for _, attribute := range []uint32{20, 19} {
		result, _, _ := dwmSetWindowAttributeProc.Call(hwnd, uintptr(attribute), uintptr(unsafe.Pointer(&useDark)), unsafe.Sizeof(useDark))
		if result == 0 {
			return true
		}
	}
	return false
}
