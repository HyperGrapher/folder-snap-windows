package main

import (
	"flag"
	"log"
	"runtime"

	"foldersnap/internal/app"
	configpkg "foldersnap/internal/config"
	loggingpkg "foldersnap/internal/logging"
	platform "foldersnap/internal/platform/windows"
	"foldersnap/internal/ui"

	"github.com/pwiecz/go-fltk"
)

func main() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	background := flag.Bool("background", false, "start in the system tray")
	flag.Parse()

	instance, exists, err := platform.AcquireInstance("FolderSnap.SingleInstance")
	if err != nil {
		log.Fatal(err)
	}
	if exists {
		_ = platform.ActivateWindow(ui.WindowTitle)
		_ = instance.Close()
		return
	}
	defer instance.Close()
	platform.EnableDPIAwareness()

	dataDir, err := configpkg.LocalDataDir()
	if err != nil {
		log.Fatal(err)
	}
	logger, closer, err := loggingpkg.New(dataDir)
	if err != nil {
		log.Fatal(err)
	}
	defer closer.Close()
	logger.Printf("FolderSnap starting background=%v", *background)

	service, err := app.New(dataDir, logger)
	if err != nil {
		logger.Printf("startup failed: %v", err)
		return
	}
	defer service.Close()
	if service.Config().LaunchAtStartup {
		if err := platform.SetLaunchAtStartup(true); err != nil {
			logger.Printf("repair startup registration: %v", err)
		}
	}
	desktop := ui.New(service, *background)
	tray, err := platform.StartTray()
	if err != nil {
		logger.Printf("tray feasibility gate failed: %v", err)
		return
	}
	defer tray.Close()
	desktop.SetNotifier(tray.Notify)
	service.StartScheduler()

	go func() {
		for event := range tray.Events() {
			event := event
			fltk.Awake(func() {
				switch event {
				case platform.TrayOpen:
					desktop.Show()
				case platform.TraySnapshot:
					desktop.TriggerSnapshot()
				case platform.TraySettings:
					desktop.ShowSettings()
				case platform.TrayQuit:
					desktop.Quit()
				}
			})
		}
	}()

	exitCode := desktop.Run()
	logger.Printf("FolderSnap stopping exit=%d", exitCode)
}
