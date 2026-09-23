//go:build windows

package main

import (
	"fmt"
	"strings"
	"syscall"
	"unsafe"
)

const (
	screenGraphicsPaletteClass = "PLMStudioScreenGraphicsPalette01"
	screenGraphicsDialogClass  = "PLMStudioScreenGraphicsDialog01"
	idScreenGraphicsShow       = 8600
	idScreenGraphicsMove       = 8601
	idScreenGraphicsHide       = 8602
	idScreenGraphicsBack       = 8603
	idScreenGraphicSlot        = 8610
	idScreenGraphicImage       = 8611
	idScreenGraphicX           = 8612
	idScreenGraphicY           = 8613
	idScreenGraphicScaleX      = 8614
	idScreenGraphicScaleY      = 8615
	idScreenGraphicOpacity     = 8616
	idScreenGraphicBlend       = 8617
	idScreenGraphicDuration    = 8618
	idScreenGraphicWait        = 8619
	idScreenGraphicOK          = 8620
	idScreenGraphicCancel      = 8621
)

type screenGraphicAction int

const (
	screenGraphicNone screenGraphicAction = iota
	screenGraphicShow
	screenGraphicMove
	screenGraphicHide
)

var screenGraphicsPaletteRegistered, screenGraphicsPaletteOpen, screenGraphicsPaletteAccepted bool
var screenGraphicsPaletteWindow syscall.Handle
var screenGraphicsPaletteResult screenGraphicAction

func screenGraphicsPaletteWndProcFn(hwnd syscall.Handle, msg uint32, w, l uintptr) uintptr {
	switch msg {
	case WM_COMMAND:
		switch int(loword(w)) {
		case idScreenGraphicsShow:
			screenGraphicsPaletteResult = screenGraphicShow
			screenGraphicsPaletteAccepted = true
			pDestroyWindow.Call(uintptr(hwnd))
			return 0
		case idScreenGraphicsMove:
			screenGraphicsPaletteResult = screenGraphicMove
			screenGraphicsPaletteAccepted = true
			pDestroyWindow.Call(uintptr(hwnd))
			return 0
		case idScreenGraphicsHide:
			screenGraphicsPaletteResult = screenGraphicHide
			screenGraphicsPaletteAccepted = true
			pDestroyWindow.Call(uintptr(hwnd))
			return 0
		case idScreenGraphicsBack:
			pDestroyWindow.Call(uintptr(hwnd))
			return 0
		}
	case WM_CLOSE:
		pDestroyWindow.Call(uintptr(hwnd))
		return 0
	case WM_DESTROY:
		screenGraphicsPaletteOpen = false
		screenGraphicsPaletteWindow = 0
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(uintptr(hwnd), uintptr(msg), w, l)
	return r
}

var screenGraphicsPaletteWndProc = syscall.NewCallback(screenGraphicsPaletteWndProcFn)

func ensureScreenGraphicsPaletteClass() error {
	if screenGraphicsPaletteRegistered {
		return nil
	}
	h, _, _ := pGetModuleHandleW.Call(0)
	hi := syscall.Handle(h)
	cur, _, _ := pLoadCursorW.Call(0, IDC_ARROW)
	brush, _, _ := pCreateSolidBrush.Call(rgb(244, 244, 244))
	wc := WNDCLASSEX{cbSize: uint32(unsafe.Sizeof(WNDCLASSEX{})), lpfnWndProc: screenGraphicsPaletteWndProc, hInstance: hi, hCursor: syscall.Handle(cur), hbrBackground: syscall.Handle(brush), lpszClassName: wstr(screenGraphicsPaletteClass)}
	r, _, err := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	if r == 0 {
		return fmt.Errorf("registrazione Grafica schermo: %v", err)
	}
	screenGraphicsPaletteRegistered = true
	return nil
}

func showScreenGraphicsPalette(owner syscall.Handle) (screenGraphicAction, bool) {
	if screenGraphicsPaletteOpen {
		return screenGraphicNone, false
	}
	if err := ensureScreenGraphicsPaletteClass(); err != nil {
		msgbox("PML Studio - Grafica schermo", err.Error(), MB_OK|MB_ICONERROR)
		return screenGraphicNone, false
	}
	const ww, wh int32 = 560, 360
	x, y := centerOwnedWindow(ww, wh)
	h, _, _ := pGetModuleHandleW.Call(0)
	hi := syscall.Handle(h)
	screenGraphicsPaletteAccepted = false
	screenGraphicsPaletteResult = screenGraphicNone
	screenGraphicsPaletteOpen = true
	screenGraphicsPaletteWindow = createWindow(screenGraphicsPaletteClass, "Grafica schermo", WS_OVERLAPPEDWINDOW|WS_VISIBLE|WS_CLIPCHILDREN, x, y, ww, wh, owner, 0, hi)
	if screenGraphicsPaletteWindow == 0 {
		screenGraphicsPaletteOpen = false
		return screenGraphicNone, false
	}
	setWindowIcon(screenGraphicsPaletteWindow)
	createWindow("STATIC", "Immagini per cutscene/eventi. Ogni immagine usa uno slot numerico.", WS_CHILD|WS_VISIBLE, 28, 24, 480, 28, screenGraphicsPaletteWindow, 8630, hi)
	createWindow("BUTTON", "Mostra immagine", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 55, 78, 190, 62, screenGraphicsPaletteWindow, idScreenGraphicsShow, hi)
	createWindow("BUTTON", "Muovi immagine", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 285, 78, 190, 62, screenGraphicsPaletteWindow, idScreenGraphicsMove, hi)
	createWindow("BUTTON", "Nascondi immagine", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 55, 158, 190, 62, screenGraphicsPaletteWindow, idScreenGraphicsHide, hi)
	createWindow("BUTTON", "← Indietro", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 285, 158, 190, 62, screenGraphicsPaletteWindow, idScreenGraphicsBack, hi)
	createWindow("STATIC", "Mostra crea lo slot; Muovi cambia posizione/scala/opacità; Nascondi elimina lo slot.", WS_CHILD|WS_VISIBLE, 55, 244, 420, 48, screenGraphicsPaletteWindow, 8631, hi)
	pEnableWindow.Call(uintptr(owner), 0)
	modalLoop(&screenGraphicsPaletteOpen)
	pEnableWindow.Call(uintptr(owner), 1)
	pSetFocus.Call(uintptr(owner))
	return screenGraphicsPaletteResult, screenGraphicsPaletteAccepted
}

var screenGraphicDialogRegistered, screenGraphicDialogOpen, screenGraphicDialogAccepted bool
var screenGraphicDialogWindow syscall.Handle
var screenGraphicSlot, screenGraphicImage, screenGraphicX, screenGraphicY, screenGraphicScaleX, screenGraphicScaleY syscall.Handle
var screenGraphicOpacity, screenGraphicBlend, screenGraphicDuration, screenGraphicWait syscall.Handle
var screenGraphicDialogAction screenGraphicAction
var screenGraphicDialogResult plmManagedEventCommand

func screenGraphicDialogWndProcFn(hwnd syscall.Handle, msg uint32, w, l uintptr) uintptr {
	switch msg {
	case WM_COMMAND:
		switch int(loword(w)) {
		case idScreenGraphicOK:
			slot := intField(screenGraphicSlot, 1)
			if slot < 1 {
				slot = 1
			}
			if slot > 999 {
				slot = 999
			}
			c := plmManagedEventCommand{PictureSlot: slot}
			switch screenGraphicDialogAction {
			case screenGraphicShow:
				graphic := strings.TrimSpace(getText(screenGraphicImage))
				if graphic == "" {
					msgbox("PML Studio - Grafica schermo", "Seleziona un'immagine.", MB_OK|MB_ICONINFORMATION)
					return 0
				}
				c.Type = "picture_show"
				c.Graphic = graphic
				c.X = intField(screenGraphicX, 0)
				c.Y = intField(screenGraphicY, 0)
				c.ScaleX = intField(screenGraphicScaleX, 100)
				c.ScaleY = intField(screenGraphicScaleY, 100)
				c.Opacity = intField(screenGraphicOpacity, 255)
				c.BlendMode = comboSel(screenGraphicBlend)
			case screenGraphicMove:
				c.Type = "picture_move"
				c.X = intField(screenGraphicX, 0)
				c.Y = intField(screenGraphicY, 0)
				c.ScaleX = intField(screenGraphicScaleX, 100)
				c.ScaleY = intField(screenGraphicScaleY, 100)
				c.Opacity = intField(screenGraphicOpacity, 255)
				c.BlendMode = comboSel(screenGraphicBlend)
				c.Duration = intField(screenGraphicDuration, 30)
				if c.Duration < 1 {
					c.Duration = 1
				}
				c.Wait = checked(screenGraphicWait)
			case screenGraphicHide:
				c.Type = "picture_hide"
			default:
				return 0
			}
			if c.ScaleX < 1 {
				c.ScaleX = 1
			}
			if c.ScaleY < 1 {
				c.ScaleY = 1
			}
			if c.Opacity < 0 {
				c.Opacity = 0
			}
			if c.Opacity > 255 {
				c.Opacity = 255
			}
			screenGraphicDialogResult = c
			screenGraphicDialogAccepted = true
			pDestroyWindow.Call(uintptr(hwnd))
			return 0
		case idScreenGraphicCancel:
			pDestroyWindow.Call(uintptr(hwnd))
			return 0
		}
	case WM_CLOSE:
		pDestroyWindow.Call(uintptr(hwnd))
		return 0
	case WM_DESTROY:
		screenGraphicDialogOpen = false
		screenGraphicDialogWindow = 0
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(uintptr(hwnd), uintptr(msg), w, l)
	return r
}

var screenGraphicDialogWndProc = syscall.NewCallback(screenGraphicDialogWndProcFn)

func ensureScreenGraphicDialogClass() error {
	if screenGraphicDialogRegistered {
		return nil
	}
	h, _, _ := pGetModuleHandleW.Call(0)
	hi := syscall.Handle(h)
	cur, _, _ := pLoadCursorW.Call(0, IDC_ARROW)
	brush, _, _ := pCreateSolidBrush.Call(rgb(244, 244, 244))
	wc := WNDCLASSEX{cbSize: uint32(unsafe.Sizeof(WNDCLASSEX{})), lpfnWndProc: screenGraphicDialogWndProc, hInstance: hi, hCursor: syscall.Handle(cur), hbrBackground: syscall.Handle(brush), lpszClassName: wstr(screenGraphicsDialogClass)}
	r, _, err := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	if r == 0 {
		return fmt.Errorf("registrazione editor immagine: %v", err)
	}
	screenGraphicDialogRegistered = true
	return nil
}

func showScreenGraphicDialog(owner syscall.Handle, action screenGraphicAction) (plmManagedEventCommand, bool) {
	if screenGraphicDialogOpen {
		return plmManagedEventCommand{}, false
	}
	if err := ensureScreenGraphicDialogClass(); err != nil {
		msgbox("PML Studio - Grafica schermo", err.Error(), MB_OK|MB_ICONERROR)
		return plmManagedEventCommand{}, false
	}
	const ww, wh int32 = 700, 600
	x, y := centerOwnedWindow(ww, wh)
	h, _, _ := pGetModuleHandleW.Call(0)
	hi := syscall.Handle(h)
	screenGraphicDialogOpen = true
	screenGraphicDialogAccepted = false
	screenGraphicDialogAction = action
	screenGraphicDialogResult = plmManagedEventCommand{}
	title := "Mostra immagine"
	if action == screenGraphicMove {
		title = "Muovi immagine"
	}
	if action == screenGraphicHide {
		title = "Nascondi immagine"
	}
	screenGraphicDialogWindow = createWindow(screenGraphicsDialogClass, title, WS_OVERLAPPEDWINDOW|WS_VISIBLE|WS_CLIPCHILDREN, x, y, ww, wh, owner, 0, hi)
	if screenGraphicDialogWindow == 0 {
		screenGraphicDialogOpen = false
		return plmManagedEventCommand{}, false
	}
	setWindowIcon(screenGraphicDialogWindow)
	createWindow("STATIC", "Slot immagine:", WS_CHILD|WS_VISIBLE, 28, 30, 140, 24, screenGraphicDialogWindow, 8640, hi)
	screenGraphicSlot = createWindow("EDIT", "1", WS_CHILD|WS_VISIBLE|WS_BORDER|WS_TABSTOP, 175, 24, 90, 32, screenGraphicDialogWindow, idScreenGraphicSlot, hi)
	y0 := int32(78)
	if action == screenGraphicShow {
		createWindow("STATIC", "Immagine (Graphics/Pictures):", WS_CHILD|WS_VISIBLE, 28, y0, 190, 24, screenGraphicDialogWindow, 8641, hi)
		screenGraphicImage = createWindow("COMBOBOX", "", WS_CHILD|WS_VISIBLE|WS_TABSTOP|0x0002|WS_VSCROLL, 225, y0-6, 410, 260, screenGraphicDialogWindow, idScreenGraphicImage, hi)
		setComboFromStrings(screenGraphicImage, mugshotAssetCatalog(currentProject), 0)
		y0 += 56
	} else {
		screenGraphicImage = 0
	}
	if action != screenGraphicHide {
		createWindow("STATIC", "X:", WS_CHILD|WS_VISIBLE, 28, y0, 35, 24, screenGraphicDialogWindow, 8642, hi)
		screenGraphicX = createWindow("EDIT", "0", WS_CHILD|WS_VISIBLE|WS_BORDER|WS_TABSTOP, 64, y0-6, 80, 30, screenGraphicDialogWindow, idScreenGraphicX, hi)
		createWindow("STATIC", "Y:", WS_CHILD|WS_VISIBLE, 170, y0, 35, 24, screenGraphicDialogWindow, 8643, hi)
		screenGraphicY = createWindow("EDIT", "0", WS_CHILD|WS_VISIBLE|WS_BORDER|WS_TABSTOP, 206, y0-6, 80, 30, screenGraphicDialogWindow, idScreenGraphicY, hi)
		y0 += 48
		createWindow("STATIC", "Scala X %:", WS_CHILD|WS_VISIBLE, 28, y0, 85, 24, screenGraphicDialogWindow, 8644, hi)
		screenGraphicScaleX = createWindow("EDIT", "100", WS_CHILD|WS_VISIBLE|WS_BORDER|WS_TABSTOP, 116, y0-6, 70, 30, screenGraphicDialogWindow, idScreenGraphicScaleX, hi)
		createWindow("STATIC", "Scala Y %:", WS_CHILD|WS_VISIBLE, 208, y0, 85, 24, screenGraphicDialogWindow, 8645, hi)
		screenGraphicScaleY = createWindow("EDIT", "100", WS_CHILD|WS_VISIBLE|WS_BORDER|WS_TABSTOP, 296, y0-6, 70, 30, screenGraphicDialogWindow, idScreenGraphicScaleY, hi)
		y0 += 48
		createWindow("STATIC", "Opacità:", WS_CHILD|WS_VISIBLE, 28, y0, 85, 24, screenGraphicDialogWindow, 8646, hi)
		screenGraphicOpacity = createWindow("EDIT", "255", WS_CHILD|WS_VISIBLE|WS_BORDER|WS_TABSTOP, 116, y0-6, 70, 30, screenGraphicDialogWindow, idScreenGraphicOpacity, hi)
		createWindow("STATIC", "Fusione:", WS_CHILD|WS_VISIBLE, 208, y0, 70, 24, screenGraphicDialogWindow, 8647, hi)
		screenGraphicBlend = createWindow("COMBOBOX", "", WS_CHILD|WS_VISIBLE|WS_TABSTOP|CBS_DROPDOWNLIST, 282, y0-6, 180, 140, screenGraphicDialogWindow, idScreenGraphicBlend, hi)
		setComboFromStrings(screenGraphicBlend, []string{"Normale", "Additiva", "Sottrattiva"}, 0)
		y0 += 52
		if action == screenGraphicMove {
			createWindow("STATIC", "Durata (frame):", WS_CHILD|WS_VISIBLE, 28, y0, 120, 24, screenGraphicDialogWindow, 8648, hi)
			screenGraphicDuration = createWindow("EDIT", "30", WS_CHILD|WS_VISIBLE|WS_BORDER|WS_TABSTOP, 152, y0-6, 80, 30, screenGraphicDialogWindow, idScreenGraphicDuration, hi)
			screenGraphicWait = createWindow("BUTTON", "Attendi fine movimento immagine", WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_AUTOCHECKBOX, 280, y0-6, 260, 30, screenGraphicDialogWindow, idScreenGraphicWait, hi)
			pSendMessageW.Call(uintptr(screenGraphicWait), BM_SETCHECK, BST_CHECKED, 0)
		}
	}
	info := "Il comando viene eseguito in sequenza nell'evento. Muovi immagine può bloccare il comando successivo fino alla fine dell'animazione."
	createWindow("STATIC", info, WS_CHILD|WS_VISIBLE, 28, 430, 620, 48, screenGraphicDialogWindow, 8649, hi)
	createWindow("BUTTON", "Conferma", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 430, 500, 105, 38, screenGraphicDialogWindow, idScreenGraphicOK, hi)
	createWindow("BUTTON", "Annulla", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 548, 500, 105, 38, screenGraphicDialogWindow, idScreenGraphicCancel, hi)
	pEnableWindow.Call(uintptr(owner), 0)
	modalLoop(&screenGraphicDialogOpen)
	pEnableWindow.Call(uintptr(owner), 1)
	pSetFocus.Call(uintptr(owner))
	return screenGraphicDialogResult, screenGraphicDialogAccepted
}

func addScreenGraphicsEventCommand() {
	action, ok := showScreenGraphicsPalette(eventEditorWindow)
	if !ok {
		return
	}
	c, ok := showScreenGraphicDialog(eventEditorWindow, action)
	if !ok {
		return
	}
	if appendManagedEventCommand(c) {
		setToolbarStatus("Comando evento aggiunto: Grafica schermo.")
	}
}
