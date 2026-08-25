package windows

import (
	"fmt"
	"syscall"
	"unsafe"
)

var (
	kernel32        = syscall.NewLazyDLL("kernel32.dll")
	createMutexProc = kernel32.NewProc("CreateMutexW")
	closeHandleProc = kernel32.NewProc("CloseHandle")
)

type InstanceLock struct{ handle uintptr }

func AcquireInstance(name string) (*InstanceLock, bool, error) {
	wide, _ := syscall.UTF16PtrFromString(`Local\` + name)
	handle, _, callErr := createMutexProc.Call(0, 1, uintptr(unsafe.Pointer(wide)))
	if handle == 0 {
		return nil, false, fmt.Errorf("create instance mutex: %w", callErr)
	}
	alreadyExists := callErr == syscall.Errno(183)
	return &InstanceLock{handle: handle}, alreadyExists, nil
}

func (l *InstanceLock) Close() error {
	if l == nil || l.handle == 0 {
		return nil
	}
	ok, _, err := closeHandleProc.Call(l.handle)
	l.handle = 0
	if ok == 0 {
		return err
	}
	return nil
}
