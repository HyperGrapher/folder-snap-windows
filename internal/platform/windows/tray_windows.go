package windows

import (
	"fmt"
	"runtime"
	"sync"
	"syscall"
	"unsafe"
)

type TrayEvent int

const (
	TrayOpen TrayEvent = iota + 1
	TraySnapshot
	TraySettings
	TrayQuit
)

const (
	wmTray         = 0x8000 + 41
	wmDestroy      = 0x0002
	wmClose        = 0x0010
	wmLButtonUp    = 0x0202
	wmRButtonUp    = 0x0205
	nimAdd         = 0
	nimModify      = 1
	nimDelete      = 2
	nifMessage     = 0x1
	nifIcon        = 0x2
	nifTip         = 0x4
	nifInfo        = 0x10
	niifInfo       = 0x1
	mfString       = 0
	mfSeparator    = 0x800
	tpmRightButton = 0x2
	tpmReturnCmd   = 0x100
)

type point struct{ X, Y int32 }
type message struct {
	Hwnd           uintptr
	Message        uint32
	WParam, LParam uintptr
	Time           uint32
	Pt             point
	Private        uint32
}
type windowClassEx struct {
	Size                   uint32
	Style                  uint32
	WndProc                uintptr
	ClsExtra, WndExtra     int32
	Instance, Icon, Cursor uintptr
	Background             uintptr
	MenuName, ClassName    *uint16
	IconSmall              uintptr
}
type notifyIconData struct {
	Size            uint32
	Hwnd            uintptr
	ID              uint32
	Flags           uint32
	CallbackMessage uint32
	Icon            uintptr
	Tip             [128]uint16
	State           uint32
	StateMask       uint32
	Info            [256]uint16
	Version         uint32
	InfoTitle       [64]uint16
	InfoFlags       uint32
	GUID            [16]byte
	BalloonIcon     uintptr
}

var (
	registerClassExProc  = user32.NewProc("RegisterClassExW")
	createWindowExProc   = user32.NewProc("CreateWindowExW")
	defWindowProc        = user32.NewProc("DefWindowProcW")
	destroyWindowProc    = user32.NewProc("DestroyWindow")
	postQuitMessageProc  = user32.NewProc("PostQuitMessage")
	postMessageProc      = user32.NewProc("PostMessageW")
	getMessageProc       = user32.NewProc("GetMessageW")
	translateMessageProc = user32.NewProc("TranslateMessage")
	dispatchMessageProc  = user32.NewProc("DispatchMessageW")
	loadIconProc         = user32.NewProc("LoadIconW")
	createPopupMenuProc  = user32.NewProc("CreatePopupMenu")
	appendMenuProc       = user32.NewProc("AppendMenuW")
	trackPopupMenuProc   = user32.NewProc("TrackPopupMenu")
	destroyMenuProc      = user32.NewProc("DestroyMenu")
	getCursorPosProc     = user32.NewProc("GetCursorPos")
	shellNotifyIconProc  = shell32.NewProc("Shell_NotifyIconW")
	trayRegistry         sync.Map
)

type Tray struct {
	hwnd   uintptr
	events chan TrayEvent
	ready  chan error
	done   chan struct{}
}

func StartTray() (*Tray, error) {
	tray := &Tray{events: make(chan TrayEvent, 16), ready: make(chan error, 1), done: make(chan struct{})}
	go tray.loop()
	if err := <-tray.ready; err != nil {
		return nil, err
	}
	return tray, nil
}

func (t *Tray) Events() <-chan TrayEvent { return t.events }

func (t *Tray) Close() {
	if t == nil || t.hwnd == 0 {
		return
	}
	postMessageProc.Call(t.hwnd, wmClose, 0, 0)
	<-t.done
}

func (t *Tray) Notify(title, body string) {
	if t == nil || t.hwnd == 0 {
		return
	}
	data := t.iconData(nifInfo)
	copyUTF16(data.InfoTitle[:], title)
	copyUTF16(data.Info[:], body)
	data.InfoFlags = niifInfo
	shellNotifyIconProc.Call(nimModify, uintptr(unsafe.Pointer(&data)))
}

func (t *Tray) loop() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	className, _ := syscall.UTF16PtrFromString("FolderSnapTrayWindow")
	instance, _, _ := kernel32.NewProc("GetModuleHandleW").Call(0)
	class := windowClassEx{Size: uint32(unsafe.Sizeof(windowClassEx{})), WndProc: syscall.NewCallback(trayWindowProc), Instance: instance, ClassName: className}
	if atom, _, err := registerClassExProc.Call(uintptr(unsafe.Pointer(&class))); atom == 0 {
		t.ready <- fmt.Errorf("register tray window: %w", err)
		close(t.done)
		return
	}
	hwnd, _, err := createWindowExProc.Call(0, uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(className)), 0, 0, 0, 0, 0, 0, 0, instance, 0)
	if hwnd == 0 {
		t.ready <- fmt.Errorf("create tray window: %w", err)
		close(t.done)
		return
	}
	t.hwnd = hwnd
	trayRegistry.Store(hwnd, t)
	data := t.iconData(nifMessage | nifIcon | nifTip)
	copyUTF16(data.Tip[:], "FolderSnap")
	if ok, _, callErr := shellNotifyIconProc.Call(nimAdd, uintptr(unsafe.Pointer(&data))); ok == 0 {
		trayRegistry.Delete(hwnd)
		destroyWindowProc.Call(hwnd)
		t.ready <- fmt.Errorf("add tray icon: %w", callErr)
		close(t.done)
		return
	}
	t.ready <- nil
	var msg message
	for {
		result, _, _ := getMessageProc.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(result) <= 0 {
			break
		}
		translateMessageProc.Call(uintptr(unsafe.Pointer(&msg)))
		dispatchMessageProc.Call(uintptr(unsafe.Pointer(&msg)))
	}
	data = t.iconData(0)
	shellNotifyIconProc.Call(nimDelete, uintptr(unsafe.Pointer(&data)))
	trayRegistry.Delete(hwnd)
	t.hwnd = 0
	close(t.events)
	close(t.done)
}

func (t *Tray) iconData(flags uint32) notifyIconData {
	icon, _, _ := loadIconProc.Call(0, 32512)
	return notifyIconData{Size: uint32(unsafe.Sizeof(notifyIconData{})), Hwnd: t.hwnd, ID: 1, Flags: flags, CallbackMessage: wmTray, Icon: icon}
}

func trayWindowProc(hwnd uintptr, msg uint32, wParam, lParam uintptr) uintptr {
	value, ok := trayRegistry.Load(hwnd)
	if ok {
		tray := value.(*Tray)
		switch msg {
		case wmTray:
			switch uint32(lParam) {
			case wmLButtonUp:
				tray.send(TrayOpen)
			case wmRButtonUp:
				tray.popup()
			}
			return 0
		case wmClose:
			destroyWindowProc.Call(hwnd)
			return 0
		case wmDestroy:
			postQuitMessageProc.Call(0)
			return 0
		}
	}
	result, _, _ := defWindowProc.Call(hwnd, uintptr(msg), wParam, lParam)
	return result
}

func (t *Tray) popup() {
	menu, _, _ := createPopupMenuProc.Call()
	if menu == 0 {
		return
	}
	defer destroyMenuProc.Call(menu)
	appendMenu(menu, mfString, 1, "Open FolderSnap")
	appendMenu(menu, mfString, 2, "Take Snapshot Now")
	appendMenu(menu, mfString, 3, "Settings")
	appendMenu(menu, mfSeparator, 0, "")
	appendMenu(menu, mfString, 4, "Quit FolderSnap")
	var cursor point
	getCursorPosProc.Call(uintptr(unsafe.Pointer(&cursor)))
	foregroundProc.Call(t.hwnd)
	command, _, _ := trackPopupMenuProc.Call(menu, tpmRightButton|tpmReturnCmd, uintptr(cursor.X), uintptr(cursor.Y), 0, t.hwnd, 0)
	switch command {
	case 1:
		t.send(TrayOpen)
	case 2:
		t.send(TraySnapshot)
	case 3:
		t.send(TraySettings)
	case 4:
		t.send(TrayQuit)
	}
}

func (t *Tray) send(event TrayEvent) {
	select {
	case t.events <- event:
	default:
	}
}

func appendMenu(menu uintptr, flags, id uintptr, text string) {
	wide, _ := syscall.UTF16PtrFromString(text)
	appendMenuProc.Call(menu, flags, id, uintptr(unsafe.Pointer(wide)))
}

func copyUTF16(destination []uint16, value string) {
	encoded, _ := syscall.UTF16FromString(value)
	if len(encoded) > len(destination) {
		encoded = encoded[:len(destination)]
		encoded[len(encoded)-1] = 0
	}
	copy(destination, encoded)
}
