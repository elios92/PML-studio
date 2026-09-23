//go:build windows

package main

import (
	"fmt"
	"syscall"
	"unsafe"
)

const (
	cutscenePaletteClass = "PLMStudioCutscenePalette01"
	idCutsceneBegin      = 9600
	idCutsceneEnd        = 9601
	idCutsceneScript     = 9602
	idCutsceneBack       = 9603
)

var cutscenePaletteRegistered, cutscenePaletteOpen, cutscenePaletteAccepted bool
var cutscenePaletteWindow syscall.Handle
var cutscenePaletteChoice int

func cutscenePaletteWndProcFn(hwnd syscall.Handle, msg uint32, w, l uintptr) uintptr {
	if msg == WM_COMMAND {
		id := int(loword(w))
		if id >= idCutsceneBegin && id <= idCutsceneScript {
			cutscenePaletteChoice = id
			cutscenePaletteAccepted = true
			pDestroyWindow.Call(uintptr(hwnd))
			return 0
		}
		if id == idCutsceneBack {
			pDestroyWindow.Call(uintptr(hwnd))
			return 0
		}
	}
	if msg == WM_CLOSE {
		pDestroyWindow.Call(uintptr(hwnd))
		return 0
	}
	if msg == WM_DESTROY {
		cutscenePaletteOpen = false
		cutscenePaletteWindow = 0
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(uintptr(hwnd), uintptr(msg), w, l)
	return r
}

var cutscenePaletteWndProc = syscall.NewCallback(cutscenePaletteWndProcFn)

func ensureCutscenePaletteClass() error {
	if cutscenePaletteRegistered {
		return nil
	}
	h, _, _ := pGetModuleHandleW.Call(0)
	hi := syscall.Handle(h)
	cur, _, _ := pLoadCursorW.Call(0, IDC_ARROW)
	br, _, _ := pCreateSolidBrush.Call(rgb(244, 244, 244))
	wc := WNDCLASSEX{cbSize: uint32(unsafe.Sizeof(WNDCLASSEX{})), lpfnWndProc: cutscenePaletteWndProc, hInstance: hi, hCursor: syscall.Handle(cur), hbrBackground: syscall.Handle(br), lpszClassName: wstr(cutscenePaletteClass)}
	r, _, e := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	if r == 0 {
		return fmt.Errorf("registrazione Cutscene: %v", e)
	}
	cutscenePaletteRegistered = true
	return nil
}
func showCutscenePalette(owner syscall.Handle) (int, bool) {
	if cutscenePaletteOpen {
		return 0, false
	}
	if err := ensureCutscenePaletteClass(); err != nil {
		msgbox("PML Studio - Cutscene", err.Error(), MB_OK|MB_ICONERROR)
		return 0, false
	}
	const ww, wh int32 = 640, 350
	x, y := centerOwnedWindow(ww, wh)
	h, _, _ := pGetModuleHandleW.Call(0)
	hi := syscall.Handle(h)
	cutscenePaletteOpen = true
	cutscenePaletteAccepted = false
	cutscenePaletteChoice = 0
	cutscenePaletteWindow = createWindow(cutscenePaletteClass, "Cutscene / Scene", WS_OVERLAPPEDWINDOW|WS_VISIBLE|WS_CLIPCHILDREN, x, y, ww, wh, owner, 0, hi)
	if cutscenePaletteWindow == 0 {
		cutscenePaletteOpen = false
		return 0, false
	}
	setWindowIcon(cutscenePaletteWindow)
	createWindow("STATIC", "Inizio/Fine cutscene proteggono la sequenza; Scena da sceneggiatura genera dialoghi e comandi in ordine.", WS_CHILD|WS_VISIBLE, 28, 22, 570, 42, cutscenePaletteWindow, 9610, hi)
	createWindow("BUTTON", "Inizio cutscene", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 45, 85, 160, 58, cutscenePaletteWindow, idCutsceneBegin, hi)
	createWindow("BUTTON", "Fine cutscene", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 238, 85, 160, 58, cutscenePaletteWindow, idCutsceneEnd, hi)
	createWindow("BUTTON", "Scena da sceneggiatura", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 431, 85, 160, 58, cutscenePaletteWindow, idCutsceneScript, hi)
	createWindow("BUTTON", "← Indietro", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 431, 220, 160, 52, cutscenePaletteWindow, idCutsceneBack, hi)
	createWindow("STATIC", "La scena compilata usa Movimento, Audio, Flash, Attese e stati Missione/Capitolo già presenti nell'editor.", WS_CHILD|WS_VISIBLE, 45, 170, 360, 72, cutscenePaletteWindow, 9611, hi)
	pEnableWindow.Call(uintptr(owner), 0)
	modalLoop(&cutscenePaletteOpen)
	pEnableWindow.Call(uintptr(owner), 1)
	pSetFocus.Call(uintptr(owner))
	return cutscenePaletteChoice, cutscenePaletteAccepted
}
func addCutsceneEventCommand() {
	for {
		choice, ok := showCutscenePalette(eventEditorWindow)
		if !ok {
			return
		}
		switch choice {
		case idCutsceneBegin:
			if appendManagedEventCommand(plmManagedEventCommand{Type: "cutscene_begin"}) {
				setToolbarStatus("Inizio cutscene aggiunto.")
			}
		case idCutsceneEnd:
			if appendManagedEventCommand(plmManagedEventCommand{Type: "cutscene_end"}) {
				setToolbarStatus("Fine cutscene aggiunta.")
			}
		case idCutsceneScript:
			createStoryScene()
		}
	}
}
