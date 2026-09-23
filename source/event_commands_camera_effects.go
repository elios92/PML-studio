//go:build windows

package main

import (
	"fmt"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

// Camera / screen-effects editor. The PML_CMD marker is always the source of
// truth for the native Python runtime. Where RPG Maker XP has an equivalent
// command, eventCommandNodesForManaged also emits a compatible native/script
// command so imported Essentials projects keep sensible parity.

const (
	cameraEffectsPaletteClass = "PLMStudioCameraEffectsPalette01"
	cameraEffectDialogClass   = "PLMStudioCameraEffectDialog01"

	idCameraEffectButtonBase = 8100
	idCameraEffectsBack      = 8120

	idCameraEffectDirection = 8140
	idCameraEffectA         = 8141
	idCameraEffectB         = 8142
	idCameraEffectC         = 8143
	idCameraEffectD         = 8144
	idCameraEffectE         = 8145
	idCameraEffectWait      = 8146
	idCameraEffectOK        = 8147
	idCameraEffectCancel    = 8148
)

type cameraEffectChoice int

const (
	cameraEffectNone cameraEffectChoice = iota
	cameraEffectPan
	cameraEffectShake
	cameraEffectFlashWhite
	cameraEffectFlashBlack
	cameraEffectFlashCustom
	cameraEffectFadeBlack
	cameraEffectFadeIn
	cameraEffectTone
	cameraEffectZoom
	cameraEffectReset
)

type cameraEffectPaletteEntry struct {
	Title  string
	Choice cameraEffectChoice
}

var cameraEffectPaletteEntries = []cameraEffectPaletteEntry{
	{"Muovi camera", cameraEffectPan},
	{"Tremore camera", cameraEffectShake},
	{"Flash bianco", cameraEffectFlashWhite},
	{"Flash nero", cameraEffectFlashBlack},
	{"Flash colore", cameraEffectFlashCustom},
	{"Dissolvenza al nero", cameraEffectFadeBlack},
	{"Ritorno dal nero", cameraEffectFadeIn},
	{"Tinta schermo", cameraEffectTone},
	{"Zoom camera", cameraEffectZoom},
	{"Reset camera", cameraEffectReset},
}

var cameraEffectsPaletteRegistered bool
var cameraEffectsPaletteOpen bool
var cameraEffectsPaletteWindow syscall.Handle
var cameraEffectsPaletteAccepted bool
var cameraEffectsPaletteResult cameraEffectChoice

func cameraEffectsPaletteWndProcFn(hwnd syscall.Handle, msg uint32, w, l uintptr) uintptr {
	switch msg {
	case WM_COMMAND:
		id := int(loword(w))
		if id >= idCameraEffectButtonBase && id < idCameraEffectButtonBase+len(cameraEffectPaletteEntries) {
			cameraEffectsPaletteResult = cameraEffectPaletteEntries[id-idCameraEffectButtonBase].Choice
			cameraEffectsPaletteAccepted = true
			pDestroyWindow.Call(uintptr(hwnd))
			return 0
		}
		if id == idCameraEffectsBack {
			pDestroyWindow.Call(uintptr(hwnd))
			return 0
		}
	case WM_CLOSE:
		pDestroyWindow.Call(uintptr(hwnd))
		return 0
	case WM_DESTROY:
		cameraEffectsPaletteOpen = false
		cameraEffectsPaletteWindow = 0
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(uintptr(hwnd), uintptr(msg), w, l)
	return r
}

var cameraEffectsPaletteWndProc = syscall.NewCallback(cameraEffectsPaletteWndProcFn)

func ensureCameraEffectsPaletteClass() error {
	if cameraEffectsPaletteRegistered {
		return nil
	}
	h, _, _ := pGetModuleHandleW.Call(0)
	hi := syscall.Handle(h)
	cur, _, _ := pLoadCursorW.Call(0, IDC_ARROW)
	brush, _, _ := pCreateSolidBrush.Call(rgb(244, 244, 244))
	wc := WNDCLASSEX{cbSize: uint32(unsafe.Sizeof(WNDCLASSEX{})), lpfnWndProc: cameraEffectsPaletteWndProc, hInstance: hi, hCursor: syscall.Handle(cur), hbrBackground: syscall.Handle(brush), lpszClassName: wstr(cameraEffectsPaletteClass)}
	r, _, err := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	if r == 0 {
		return fmt.Errorf("registrazione Camera / Effetti: %v", err)
	}
	cameraEffectsPaletteRegistered = true
	return nil
}

func showCameraEffectsPalette(owner syscall.Handle) (cameraEffectChoice, bool) {
	if cameraEffectsPaletteOpen {
		return cameraEffectNone, false
	}
	if err := ensureCameraEffectsPaletteClass(); err != nil {
		msgbox("PML Studio - Camera / Effetti", err.Error(), MB_OK|MB_ICONERROR)
		return cameraEffectNone, false
	}
	const ww, wh int32 = 720, 530
	x, y := centerOwnedWindow(ww, wh)
	h, _, _ := pGetModuleHandleW.Call(0)
	hi := syscall.Handle(h)
	cameraEffectsPaletteAccepted = false
	cameraEffectsPaletteResult = cameraEffectNone
	cameraEffectsPaletteOpen = true
	cameraEffectsPaletteWindow = createWindow(cameraEffectsPaletteClass, "Camera / Effetti speciali", WS_OVERLAPPEDWINDOW|WS_VISIBLE|WS_CLIPCHILDREN, x, y, ww, wh, owner, 0, hi)
	if cameraEffectsPaletteWindow == 0 {
		cameraEffectsPaletteOpen = false
		return cameraEffectNone, false
	}
	setWindowIcon(cameraEffectsPaletteWindow)
	createWindow("STATIC", "Scegli l'effetto da inserire nella sequenza dell'evento.", WS_CHILD|WS_VISIBLE, 28, 22, 640, 26, cameraEffectsPaletteWindow, 8130, hi)
	const cols = 2
	const bw, bh int32 = 300, 54
	const gx, gy int32 = 24, 14
	const sx, sy int32 = 36, 62
	for i, entry := range cameraEffectPaletteEntries {
		col := int32(i % cols)
		row := int32(i / cols)
		createWindow("BUTTON", entry.Title, WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_PUSHBUTTON, sx+col*(bw+gx), sy+row*(bh+gy), bw, bh, cameraEffectsPaletteWindow, uintptr(idCameraEffectButtonBase+i), hi)
	}
	createWindow("BUTTON", "← Indietro", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 36, 425, 150, 40, cameraEffectsPaletteWindow, idCameraEffectsBack, hi)
	createWindow("STATIC", "Flash, tinta, tremore e scorrimento mantengono anche la compatibilità RPG Maker XP quando possibile. Zoom e Reset camera sono comandi PLM.", WS_CHILD|WS_VISIBLE, 210, 421, 455, 55, cameraEffectsPaletteWindow, 8131, hi)
	pEnableWindow.Call(uintptr(owner), 0)
	modalLoop(&cameraEffectsPaletteOpen)
	pEnableWindow.Call(uintptr(owner), 1)
	pSetFocus.Call(uintptr(owner))
	return cameraEffectsPaletteResult, cameraEffectsPaletteAccepted
}

var cameraEffectDialogRegistered bool
var cameraEffectDialogOpen bool
var cameraEffectDialogWindow syscall.Handle
var cameraEffectDialogAccepted bool
var cameraEffectDialogChoice cameraEffectChoice
var cameraEffectDirection syscall.Handle
var cameraEffectA, cameraEffectB, cameraEffectC, cameraEffectD, cameraEffectE syscall.Handle
var cameraEffectWait syscall.Handle
var cameraEffectResult plmManagedEventCommand

func effectInt(h syscall.Handle, fallback, min, max int) int {
	n, err := strconv.Atoi(strings.TrimSpace(getText(h)))
	if err != nil {
		n = fallback
	}
	if n < min {
		n = min
	}
	if n > max {
		n = max
	}
	return n
}

func cameraEffectDialogWndProcFn(hwnd syscall.Handle, msg uint32, w, l uintptr) uintptr {
	switch msg {
	case WM_COMMAND:
		switch int(loword(w)) {
		case idCameraEffectOK:
			c := plmManagedEventCommand{}
			switch cameraEffectDialogChoice {
			case cameraEffectPan:
				c.Type = "camera_pan"
				dirs := []int{2, 4, 6, 8}
				di := comboSel(cameraEffectDirection)
				if di < 0 || di >= len(dirs) {
					di = 0
				}
				c.Direction = dirs[di]
				c.Tiles = effectInt(cameraEffectA, 3, 1, 999)
				c.Speed = effectInt(cameraEffectB, 4, 1, 6)
				c.Wait = checked(cameraEffectWait)
			case cameraEffectShake:
				c.Type = "camera_shake"
				c.Power = effectInt(cameraEffectA, 5, 1, 9)
				c.Speed = effectInt(cameraEffectB, 5, 1, 9)
				c.Duration = effectInt(cameraEffectC, 30, 1, 9999)
				c.Wait = checked(cameraEffectWait)
			case cameraEffectFlashWhite, cameraEffectFlashBlack, cameraEffectFlashCustom:
				c.Type = "screen_flash"
				c.Red = effectInt(cameraEffectA, 255, 0, 255)
				c.Green = effectInt(cameraEffectB, 255, 0, 255)
				c.Blue = effectInt(cameraEffectC, 255, 0, 255)
				c.Alpha = effectInt(cameraEffectD, 255, 0, 255)
				c.Duration = effectInt(cameraEffectE, 20, 1, 9999)
				c.Wait = checked(cameraEffectWait)
			case cameraEffectFadeBlack:
				c.Type = "fade_black"
				c.Duration = effectInt(cameraEffectA, 30, 1, 9999)
				c.Wait = checked(cameraEffectWait)
			case cameraEffectFadeIn:
				c.Type = "fade_in"
				c.Duration = effectInt(cameraEffectA, 30, 1, 9999)
				c.Wait = checked(cameraEffectWait)
			case cameraEffectTone:
				c.Type = "screen_tone"
				c.Red = effectInt(cameraEffectA, 0, -255, 255)
				c.Green = effectInt(cameraEffectB, 0, -255, 255)
				c.Blue = effectInt(cameraEffectC, 0, -255, 255)
				c.Gray = effectInt(cameraEffectD, 0, 0, 255)
				c.Duration = effectInt(cameraEffectE, 30, 1, 9999)
				c.Wait = checked(cameraEffectWait)
			case cameraEffectZoom:
				c.Type = "camera_zoom"
				c.Zoom = effectInt(cameraEffectA, 125, 25, 400)
				c.Duration = effectInt(cameraEffectB, 30, 1, 9999)
				c.Wait = checked(cameraEffectWait)
			default:
				return 0
			}
			cameraEffectResult = c
			cameraEffectDialogAccepted = true
			pDestroyWindow.Call(uintptr(hwnd))
			return 0
		case idCameraEffectCancel:
			pDestroyWindow.Call(uintptr(hwnd))
			return 0
		}
	case WM_CLOSE:
		pDestroyWindow.Call(uintptr(hwnd))
		return 0
	case WM_DESTROY:
		cameraEffectDialogOpen = false
		cameraEffectDialogWindow = 0
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(uintptr(hwnd), uintptr(msg), w, l)
	return r
}

var cameraEffectDialogWndProc = syscall.NewCallback(cameraEffectDialogWndProcFn)

func ensureCameraEffectDialogClass() error {
	if cameraEffectDialogRegistered {
		return nil
	}
	h, _, _ := pGetModuleHandleW.Call(0)
	hi := syscall.Handle(h)
	cur, _, _ := pLoadCursorW.Call(0, IDC_ARROW)
	brush, _, _ := pCreateSolidBrush.Call(rgb(244, 244, 244))
	wc := WNDCLASSEX{cbSize: uint32(unsafe.Sizeof(WNDCLASSEX{})), lpfnWndProc: cameraEffectDialogWndProc, hInstance: hi, hCursor: syscall.Handle(cur), hbrBackground: syscall.Handle(brush), lpszClassName: wstr(cameraEffectDialogClass)}
	r, _, err := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	if r == 0 {
		return fmt.Errorf("registrazione configurazione effetto: %v", err)
	}
	cameraEffectDialogRegistered = true
	return nil
}

func addEffectEdit(hi syscall.Handle, label, value string, y int32, id int) syscall.Handle {
	createWindow("STATIC", label, WS_CHILD|WS_VISIBLE, 30, y+5, 180, 24, cameraEffectDialogWindow, uintptr(id+100), hi)
	return createWindow("EDIT", value, WS_CHILD|WS_VISIBLE|WS_BORDER|WS_TABSTOP, 220, y, 190, 31, cameraEffectDialogWindow, uintptr(id), hi)
}

func showCameraEffectConfig(owner syscall.Handle, choice cameraEffectChoice) (plmManagedEventCommand, bool) {
	if cameraEffectDialogOpen {
		return plmManagedEventCommand{}, false
	}
	if err := ensureCameraEffectDialogClass(); err != nil {
		msgbox("PML Studio - Camera / Effetti", err.Error(), MB_OK|MB_ICONERROR)
		return plmManagedEventCommand{}, false
	}
	title := "Configura effetto"
	switch choice {
	case cameraEffectPan:
		title = "Muovi camera"
	case cameraEffectShake:
		title = "Tremore camera"
	case cameraEffectFlashWhite:
		title = "Flash bianco"
	case cameraEffectFlashBlack:
		title = "Flash nero"
	case cameraEffectFlashCustom:
		title = "Flash colore"
	case cameraEffectFadeBlack:
		title = "Dissolvenza al nero"
	case cameraEffectFadeIn:
		title = "Ritorno dal nero"
	case cameraEffectTone:
		title = "Tinta schermo"
	case cameraEffectZoom:
		title = "Zoom camera"
	}
	const ww, wh int32 = 500, 465
	x, y := centerOwnedWindow(ww, wh)
	h, _, _ := pGetModuleHandleW.Call(0)
	hi := syscall.Handle(h)
	cameraEffectDialogAccepted = false
	cameraEffectDialogChoice = choice
	cameraEffectResult = plmManagedEventCommand{}
	cameraEffectA, cameraEffectB, cameraEffectC, cameraEffectD, cameraEffectE = 0, 0, 0, 0, 0
	cameraEffectDirection, cameraEffectWait = 0, 0
	cameraEffectDialogOpen = true
	cameraEffectDialogWindow = createWindow(cameraEffectDialogClass, title, WS_OVERLAPPEDWINDOW|WS_VISIBLE|WS_CLIPCHILDREN, x, y, ww, wh, owner, 0, hi)
	if cameraEffectDialogWindow == 0 {
		cameraEffectDialogOpen = false
		return plmManagedEventCommand{}, false
	}
	setWindowIcon(cameraEffectDialogWindow)
	createWindow("STATIC", "Parametri dell'effetto:", WS_CHILD|WS_VISIBLE, 30, 20, 410, 28, cameraEffectDialogWindow, 8150, hi)
	y0 := int32(62)
	switch choice {
	case cameraEffectPan:
		createWindow("STATIC", "Direzione:", WS_CHILD|WS_VISIBLE, 30, y0+5, 180, 24, cameraEffectDialogWindow, 8151, hi)
		cameraEffectDirection = createWindow("COMBOBOX", "", WS_CHILD|WS_VISIBLE|WS_TABSTOP|CBS_DROPDOWNLIST, 220, y0, 190, 160, cameraEffectDialogWindow, idCameraEffectDirection, hi)
		for _, s := range []string{"Giù", "Sinistra", "Destra", "Su"} {
			comboAdd(cameraEffectDirection, s)
		}
		comboSelectIndex(cameraEffectDirection, 0)
		cameraEffectA = addEffectEdit(hi, "Distanza (tile):", "3", y0+47, idCameraEffectA)
		cameraEffectB = addEffectEdit(hi, "Velocità (1-6):", "4", y0+94, idCameraEffectB)
	case cameraEffectShake:
		cameraEffectA = addEffectEdit(hi, "Forza (1-9):", "5", y0, idCameraEffectA)
		cameraEffectB = addEffectEdit(hi, "Velocità (1-9):", "5", y0+47, idCameraEffectB)
		cameraEffectC = addEffectEdit(hi, "Durata (frame):", "30", y0+94, idCameraEffectC)
	case cameraEffectFlashWhite, cameraEffectFlashBlack, cameraEffectFlashCustom:
		defaults := []string{"255", "255", "255", "255", "20"}
		if choice == cameraEffectFlashBlack {
			defaults[0], defaults[1], defaults[2] = "0", "0", "0"
		}
		cameraEffectA = addEffectEdit(hi, "Rosso (0-255):", defaults[0], y0, idCameraEffectA)
		cameraEffectB = addEffectEdit(hi, "Verde (0-255):", defaults[1], y0+47, idCameraEffectB)
		cameraEffectC = addEffectEdit(hi, "Blu (0-255):", defaults[2], y0+94, idCameraEffectC)
		cameraEffectD = addEffectEdit(hi, "Intensità (0-255):", defaults[3], y0+141, idCameraEffectD)
		cameraEffectE = addEffectEdit(hi, "Durata (frame):", defaults[4], y0+188, idCameraEffectE)
	case cameraEffectFadeBlack, cameraEffectFadeIn:
		cameraEffectA = addEffectEdit(hi, "Durata (frame):", "30", y0, idCameraEffectA)
		createWindow("STATIC", "La dissolvenza usa una tinta progressiva fino al nero / ritorno alla tinta normale, così resta compatibile anche con progetti Essentials importati.", WS_CHILD|WS_VISIBLE, 30, y0+55, 410, 70, cameraEffectDialogWindow, 8152, hi)
	case cameraEffectTone:
		cameraEffectA = addEffectEdit(hi, "Rosso (-255..255):", "0", y0, idCameraEffectA)
		cameraEffectB = addEffectEdit(hi, "Verde (-255..255):", "0", y0+47, idCameraEffectB)
		cameraEffectC = addEffectEdit(hi, "Blu (-255..255):", "0", y0+94, idCameraEffectC)
		cameraEffectD = addEffectEdit(hi, "Grigio (0-255):", "0", y0+141, idCameraEffectD)
		cameraEffectE = addEffectEdit(hi, "Durata (frame):", "30", y0+188, idCameraEffectE)
	case cameraEffectZoom:
		cameraEffectA = addEffectEdit(hi, "Zoom (%):", "125", y0, idCameraEffectA)
		cameraEffectB = addEffectEdit(hi, "Durata (frame):", "30", y0+47, idCameraEffectB)
		createWindow("STATIC", "Zoom è un comando PLM: il runtime Python lo eseguirà senza dipendere da script Ruby/RGSS.", WS_CHILD|WS_VISIBLE, 30, y0+105, 410, 55, cameraEffectDialogWindow, 8153, hi)
	}
	cameraEffectWait = createWindow("BUTTON", "Attendi la fine dell'effetto prima del comando successivo", WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_AUTOCHECKBOX, 30, 330, 410, 28, cameraEffectDialogWindow, idCameraEffectWait, hi)
	pSendMessageW.Call(uintptr(cameraEffectWait), BM_SETCHECK, BST_CHECKED, 0)
	createWindow("BUTTON", "Inserisci", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 225, 375, 100, 36, cameraEffectDialogWindow, idCameraEffectOK, hi)
	createWindow("BUTTON", "Annulla", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 340, 375, 100, 36, cameraEffectDialogWindow, idCameraEffectCancel, hi)
	pEnableWindow.Call(uintptr(owner), 0)
	modalLoop(&cameraEffectDialogOpen)
	pEnableWindow.Call(uintptr(owner), 1)
	pSetFocus.Call(uintptr(owner))
	return cameraEffectResult, cameraEffectDialogAccepted
}

func addCameraEffectsEventCommand() {
	owner := eventEditorWindow
	for {
		choice, ok := showCameraEffectsPalette(owner)
		if !ok || choice == cameraEffectNone {
			return
		}
		if choice == cameraEffectReset {
			appendManagedEventCommand(plmManagedEventCommand{Type: "camera_reset"})
			setToolbarStatus("Comando evento aggiunto: Reset camera.")
			continue
		}
		c, ok := showCameraEffectConfig(owner, choice)
		if !ok {
			continue
		}
		appendManagedEventCommand(c)
		setToolbarStatus("Comando Camera / Effetti aggiunto: " + managedEventCommandLabel(c))
	}
}
