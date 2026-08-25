package windows

func EnableDPIAwareness() {
	setContext := user32.NewProc("SetProcessDpiAwarenessContext")
	// DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 is the signed pseudo-handle -4.
	if result, _, _ := setContext.Call(^uintptr(3)); result != 0 {
		return
	}
	user32.NewProc("SetProcessDPIAware").Call()
}
