package windows

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

var (
	shell32          = syscall.NewLazyDLL("shell32.dll")
	shellExecuteProc = shell32.NewProc("ShellExecuteW")
	user32           = syscall.NewLazyDLL("user32.dll")
	findWindowProc   = user32.NewProc("FindWindowW")
	showWindowProc   = user32.NewProc("ShowWindow")
	foregroundProc   = user32.NewProc("SetForegroundWindow")
)

func OpenInExplorer(path string) error {
	verb, _ := syscall.UTF16PtrFromString("open")
	target, _ := syscall.UTF16PtrFromString(path)
	result, _, callErr := shellExecuteProc.Call(0, uintptr(unsafe.Pointer(verb)), uintptr(unsafe.Pointer(target)), 0, 0, 1)
	if result <= 32 {
		return fmt.Errorf("open Explorer: %w", callErr)
	}
	return nil
}

func ActivateWindow(title string) bool {
	wide, _ := syscall.UTF16PtrFromString(title)
	hwnd, _, _ := findWindowProc.Call(0, uintptr(unsafe.Pointer(wide)))
	if hwnd == 0 {
		return false
	}
	showWindowProc.Call(hwnd, 9)
	foregroundProc.Call(hwnd)
	return true
}

func ExecutablePath() string {
	path, err := os.Executable()
	if err != nil {
		return "FolderSnap.exe"
	}
	path, _ = filepath.Abs(path)
	return path
}
