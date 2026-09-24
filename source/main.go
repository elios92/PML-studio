//go:build windows

package main

import (
	"runtime"
	"syscall"
	"unsafe"
)

func main() {
	if runEmbeddedGameHost() {
		return
	}
	if runUpdateHelperFromArgs() {
		return
	}
	go cleanupUpdateArtifacts()
	// Win32 windows, their message queue and COM apartment belong to one
	// OS thread. Go must not move this goroutine between native threads.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	diagInit()
	defer diagShutdown()
	diagLogf("[BOOT] main() START")
	pCoInitializeEx.Call(0, COINIT_APARTMENTTHREADED)
	defer pCoUninitialize.Call()

	loadSettings()
	gridEnabled = settings.GridDefault
	applyDefaultEditorTool()

	// Common Controls v6 + Segoe UI devono essere attivi PRIMA di creare una
	// qualsiasi finestra/controllo. Il vecchio percorso usava DEFAULT_GUI_FONT,
	// facendo apparire l'editor come una UI Win9x/Windows 98.
	activateModernVisualStyles()
	defer deactivateModernVisualStyles()
	initModernThemeResources()
	defer releaseModernThemeResources()

	hi, _, _ := pGetModuleHandleW.Call(0)
	hInst := syscall.Handle(hi)
	cur, _, _ := pLoadCursorW.Call(0, IDC_ARROW)
	cursor := syscall.Handle(cur)

	appIconBig, appIconSmall = loadWindowIcons(hInst)
	defer releaseWindowIcons()
	defer releaseToolbarBitmaps()

	cn := wstr("PLMStudioWindowClass03")
	wc := WNDCLASSEX{
		cbSize:        uint32(unsafe.Sizeof(WNDCLASSEX{})),
		lpfnWndProc:   syscall.NewCallback(wndProc),
		hInstance:     hInst,
		hIcon:         appIconBig,
		hCursor:       cursor,
		hbrBackground: themeWindowBackgroundBrush(),
		lpszClassName: cn,
		hIconSm:       appIconSmall,
	}
	pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))

	ccn := wstr("PLMStudioCanvas03")
	wc2 := WNDCLASSEX{
		cbSize:        uint32(unsafe.Sizeof(WNDCLASSEX{})),
		lpfnWndProc:   syscall.NewCallback(canvasWndProc),
		hInstance:     hInst,
		hCursor:       cursor,
		hbrBackground: themeWindowBackgroundBrush(),
		lpszClassName: ccn,
	}
	pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc2)))

	pcn := wstr("PLMStudioPalette03")
	wc3 := WNDCLASSEX{
		cbSize:        uint32(unsafe.Sizeof(WNDCLASSEX{})),
		lpfnWndProc:   syscall.NewCallback(paletteWndProc),
		hInstance:     hInst,
		hCursor:       cursor,
		hbrBackground: themeWindowBackgroundBrush(),
		lpszClassName: pcn,
	}
	pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc3)))

	registerUISettingsWindowClass(hInst, cursor)

	hwndMain = createWindow("PLMStudioWindowClass03", "PML Studio", WS_OVERLAPPEDWINDOW|WS_VISIBLE|WS_CLIPCHILDREN, -2147483648, -2147483648, 1500, 860, 0, 0, hInst)
	if hwndMain == 0 {
		panic("Impossibile creare finestra")
	}
	setWindowIcon(hwndMain)
	applyModernWindowFrame(hwndMain)
	createMainControls(hInst)
	updateRecent()
	updateInspector()
	layout(hwndMain)
	pShowWindow.Call(uintptr(hwndMain), SW_SHOW)
	pUpdateWindow.Call(uintptr(hwndMain))

	// Il controllo release e asincrono: non deve mai bloccare il thread Win32.
	beginAutomaticUpdateCheck()

	configureEditorAutosave()
	switch settings.Startup {
	case "last":
		if isDir(settings.LastProject) {
			reopen()
		}
	case "open":
		openProject()
	}
	diagCurrentUIOperation.Store("message-loop")
	diagLogf("[BOOT] enter message loop")
	var m MSG
	for {
		r, _, callErr := pGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) == -1 {
			diagLogf("[ERROR] GetMessageW returned -1: %v", callErr)
			break
		}
		if r == 0 {
			diagLogf("[BOOT] GetMessageW returned 0 (WM_QUIT)")
			break
		}
		diagUIHeartbeat("dispatch-message")
		if hwndUISettings != 0 {
			if handled, _, _ := user32.NewProc("IsDialogMessageW").Call(uintptr(hwndUISettings), uintptr(unsafe.Pointer(&m))); handled != 0 {
				continue
			}
		}
		diagBeforeDispatch(&m)
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		pDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
		diagAfterDispatch(&m)
	}
	diagLogf("[BOOT] exit message loop")
}
