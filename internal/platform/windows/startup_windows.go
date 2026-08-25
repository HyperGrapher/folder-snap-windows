package windows

import (
	"fmt"
	"syscall"
	"unsafe"
)

const runKeyPath = `Software\Microsoft\Windows\CurrentVersion\Run`

func SetLaunchAtStartup(enabled bool) error {
	keyPath, _ := syscall.UTF16PtrFromString(runKeyPath)
	var key syscall.Handle
	if err := syscall.RegOpenKeyEx(syscall.HKEY_CURRENT_USER, keyPath, 0, syscall.KEY_SET_VALUE, &key); err != nil {
		return err
	}
	defer syscall.RegCloseKey(key)
	name, _ := syscall.UTF16PtrFromString("FolderSnap")
	if !enabled {
		status, _, _ := advapiRegDeleteValue.Call(uintptr(key), uintptr(unsafe.Pointer(name)))
		if status == uintptr(syscall.ERROR_FILE_NOT_FOUND) {
			return nil
		}
		if status != 0 {
			return syscall.Errno(status)
		}
		return nil
	}
	value := `"` + ExecutablePath() + `" --background`
	wide, err := syscall.UTF16FromString(value)
	if err != nil {
		return err
	}
	status, _, callErr := advapiRegSetValueEx.Call(
		uintptr(key), uintptr(unsafe.Pointer(name)), 0, syscall.REG_SZ,
		uintptr(unsafe.Pointer(&wide[0])), uintptr(len(wide)*2),
	)
	if status != 0 {
		return fmt.Errorf("write startup setting: %w", syscall.Errno(status))
	}
	if callErr != syscall.Errno(0) {
		return callErr
	}
	return nil
}

var (
	advapiRegSetValueEx  = syscall.NewLazyDLL("advapi32.dll").NewProc("RegSetValueExW")
	advapiRegDeleteValue = syscall.NewLazyDLL("advapi32.dll").NewProc("RegDeleteValueW")
)
