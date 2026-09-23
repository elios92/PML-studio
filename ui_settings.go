//go:build windows

package main

import (
	"fmt"
	"syscall"
	"unsafe"
)

const uiSettingsClassName = "PMLStudioUISettings03"

var hwndUISettings syscall.Handle
var skinColorEdits [5]syscall.Handle
var skinOriginal Settings
var skinInfo syscall.Handle
var skinDialogReady, skinAccepted bool

func registerUISettingsWindowClass(hInst syscall.Handle, cursor syscall.Handle) {
	wc := WNDCLASSEX{cbSize: uint32(unsafe.Sizeof(WNDCLASSEX{})), lpfnWndProc: syscall.NewCallback(uiSettingsWndProc), hInstance: hInst, hIcon: appIconBig, hCursor: cursor, hbrBackground: themeWindowBackgroundBrush(), lpszClassName: wstr(uiSettingsClassName), hIconSm: appIconSmall}
	pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
}

func showUISettingsDialog() {
	if hwndUISettings != 0 {
		pSetFocus.Call(uintptr(hwndUISettings))
		return
	}
	skinOriginal, skinAccepted, skinDialogReady = settings, false, false
	hi, _, _ := pGetModuleHandleW.Call(0)
	var owner RECT
	pGetWindowRect.Call(uintptr(hwndMain), uintptr(unsafe.Pointer(&owner)))
	hwndUISettings = createWindow(uiSettingsClassName, "Personalizza skin", 0x00C80000|WS_VISIBLE, owner.Left+70, owner.Top+70, 630*int32(max(100, settings.UIScale))/100, 445*int32(max(100, settings.UIScale))/100, hwndMain, 0, syscall.Handle(hi))
	if hwndUISettings == 0 {
		return
	}
	pEnableWindow.Call(uintptr(hwndMain), 0)
	labels := []string{"Colore principale", "Secondario / barra strumenti", "Sfondo pannelli", "Bordi", "Selezione e strumenti attivi"}
	values := []string{settings.AccentColor, settings.SecondaryColor, settings.PanelColor, settings.BorderColor, settings.SelectionColor}
	for i, label := range labels {
		y := int32(24 + i*45)
		createSkinControl("STATIC", label, WS_CHILD|WS_VISIBLE, 20, y+3, 290, 28, hwndUISettings, 0, syscall.Handle(hi))
		skinColorEdits[i] = createSkinControl("EDIT", values[i], WS_CHILD|WS_VISIBLE|WS_TABSTOP|0x00800000|0x0080, 325, y, 140, 30, hwndUISettings, uintptr(2660+i), syscall.Handle(hi))
		createSkinControl("BUTTON", "Scegli...", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 474, y, 102, 30, hwndUISettings, uintptr(2680+i), syscall.Handle(hi))
	}
	skinInfo = createSkinControl("STATIC", "Colori #RRGGBB; campo vuoto = colore della skin. Anteprima immediata; Annulla ripristina l'aspetto precedente.", WS_CHILD|WS_VISIBLE, 20, 256, 565, 64, hwndUISettings, 0, syscall.Handle(hi))
	createSkinControl("BUTTON", "Skin ufficiale", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 20, 337, 180, 38, hwndUISettings, 2670, syscall.Handle(hi))
	createSkinControl("BUTTON", "Salva", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 310, 337, 128, 38, hwndUISettings, 2671, syscall.Handle(hi))
	createSkinControl("BUTTON", "Annulla", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 451, 337, 128, 38, hwndUISettings, 2672, syscall.Handle(hi))
	setWindowIcon(hwndUISettings)
	applyModernWindowFrame(hwndUISettings)
	skinDialogReady = true
}

func previewSkinFields() error {
	values := make([]string, 5)
	for i, h := range skinColorEdits {
		values[i] = getText(h)
		if values[i] != "" {
			if _, _, _, err := parseThemeColor(values[i]); err != nil {
				return fmt.Errorf("campo %d: %w", i+1, err)
			}
		}
	}
	settings.AccentColor, settings.SecondaryColor, settings.PanelColor, settings.BorderColor, settings.SelectionColor = values[0], values[1], values[2], values[3], values[4]
	applyLiveEditorTheme()
	return nil
}

func resetResizableUILayout() {
	changeEditorPreferences(func(s *Settings) {
		s.LeftPanelWidth = 194
		s.RightPanelWidth = 188
		s.PermissionPanelWidth = 344
		s.LeftDetailsHeight = 103
		s.PaletteBorderHeight = 98
		s.PaletteAutotileHeight = 122
	})
	layout(hwndMain)
}

func uiSettingsWndProc(hwnd syscall.Handle, msg uint32, w, l uintptr) uintptr {
	switch msg {
	case WM_CTLCOLORSTATIC, WM_CTLCOLOREDIT, WM_CTLCOLORLISTBOX, WM_CTLCOLORBTN:
		return modernCtlColor(msg, w, syscall.Handle(l))
	case WM_ERASEBKGND:
		var r RECT
		pGetClientRect.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&r)))
		pFillRect.Call(w, uintptr(unsafe.Pointer(&r)), uintptr(themeBrushPanel))
		return 1
	case WM_COMMAND:
		id := loword(w)
		if id >= 2680 && id <= 2684 {
			chooseSkinColor(int(id - 2680))
			return 0
		}
		if id >= 2660 && id <= 2664 && hiword(w) == EN_CHANGE && skinDialogReady {
			if e := previewSkinFields(); e != nil {
				setText(skinInfo, e.Error())
			} else {
				setText(skinInfo, "Anteprima applicata. Salva per mantenerla, Annulla per ripristinare.")
			}
			return 0
		}
		switch id {
		case 2670:
			skinDialogReady = false
			resetAppearance(&settings)
			for _, h := range skinColorEdits {
				setText(h, "")
			}
			skinDialogReady = true
			applyLiveEditorTheme()
			setText(skinInfo, "Skin ufficiale in anteprima. Premi Salva per confermare.")
		case 2671:
			for _, h := range skinColorEdits {
				v := getText(h)
				if v != "" {
					if _, _, _, e := parseThemeColor(v); e != nil {
						setText(skinInfo, e.Error())
						return 0
					}
				}
			}
			if e := persistEditorPreferences(settings); e != nil {
				setText(skinInfo, "Salvataggio fallito: "+e.Error())
				return 0
			}
			skinAccepted = true
			pDestroyWindow.Call(uintptr(hwnd))
		case 2672, 2: // IDCANCEL from IsDialogMessage/Escape.
			pDestroyWindow.Call(uintptr(hwnd))
		}
		return 0
	case WM_CLOSE:
		pDestroyWindow.Call(uintptr(hwnd))
		return 0
	case WM_DESTROY:
		skinDialogReady = false
		hwndUISettings = 0
		if !skinAccepted {
			settings = skinOriginal
			applyLiveEditorTheme()
		}
		pEnableWindow.Call(uintptr(hwndMain), 1)
		createMainMenu(hwndMain)
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(uintptr(hwnd), uintptr(msg), w, l)
	return r
}

type skinChooseColor struct {
	Size     uint32
	Owner    syscall.Handle
	Instance syscall.Handle
	Result   uint32
	Custom   *uint32
	Flags    uint32
	Data     uintptr
	Hook     uintptr
	Template *uint16
}

var skinCustomColors [16]uint32

func chooseSkinColor(index int) {
	r, g, b, err := parseThemeColor(getText(skinColorEdits[index]))
	if err != nil {
		r, g, b = themeAccentR, themeAccentG, themeAccentB
	}
	dialog := skinChooseColor{Size: uint32(unsafe.Sizeof(skinChooseColor{})), Owner: hwndUISettings, Result: uint32(rgb(r, g, b)), Custom: &skinCustomColors[0], Flags: 3}
	ok, _, _ := syscall.NewLazyDLL("comdlg32.dll").NewProc("ChooseColorW").Call(uintptr(unsafe.Pointer(&dialog)))
	if ok != 0 {
		setText(skinColorEdits[index], fmt.Sprintf("#%02X%02X%02X", byte(dialog.Result), byte(dialog.Result>>8), byte(dialog.Result>>16)))
	}
}

func createSkinControl(class, title string, style uint32, x, y, w, h int32, parent syscall.Handle, id uintptr, instance syscall.Handle) syscall.Handle {
	scale := int32(max(100, settings.UIScale))
	return createWindow(class, title, style, x*scale/100, y*scale/100, w*scale/100, h*scale/100, parent, id, instance)
}
