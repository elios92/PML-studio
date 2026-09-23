//go:build windows

package main

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

// Palette corrente. Rimangono variabili package-level perché renderer di
// splitter e icone le usano direttamente. Tutte le skin qui definite restano
// coerenti con Common Controls v6/Segoe UI: nessuna riattiva lo stile classico.
var (
	themeWindowR  byte = 243
	themeWindowG  byte = 246
	themeWindowB  byte = 250
	themePanelR   byte = 255
	themePanelG   byte = 255
	themePanelB   byte = 255
	themeToolbarR byte = 247
	themeToolbarG byte = 249
	themeToolbarB byte = 252
	themeStatusR  byte = 235
	themeStatusG  byte = 240
	themeStatusB  byte = 247
	themeTextR    byte = 31
	themeTextG    byte = 41
	themeTextB    byte = 55
	themeMutedR   byte = 90
	themeMutedG   byte = 103
	themeMutedB   byte = 120
	themeBorderR  byte = 211
	themeBorderG  byte = 218
	themeBorderB  byte = 228
	themeAccentR  byte = 47
	themeAccentG  byte = 111
	themeAccentB  byte = 237
)

var (
	themeBrushWindow  syscall.Handle
	themeBrushPanel   syscall.Handle
	themeBrushToolbar syscall.Handle
	themeBrushStatus  syscall.Handle
	themeUIFont       syscall.Handle

	visualStylesActCtx syscall.Handle
	visualStylesCookie uintptr
)

const modernVisualStylesManifest = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<assembly xmlns="urn:schemas-microsoft-com:asm.v1" manifestVersion="1.0">
  <assemblyIdentity version="1.0.0.0" processorArchitecture="*" name="PMLStudio.ModernUI" type="win32"/>
  <dependency>
    <dependentAssembly>
      <assemblyIdentity type="win32" name="Microsoft.Windows.Common-Controls" version="6.0.0.0" processorArchitecture="*" publicKeyToken="6595b64144ccf1df" language="*"/>
    </dependentAssembly>
  </dependency>
</assembly>`

func selectThemePalette() {
	defer applyPreferencePalette()
	switch settings.UISkin {
	case "windows10_gray":
		themeWindowR, themeWindowG, themeWindowB = 240, 242, 245
		themePanelR, themePanelG, themePanelB = 252, 252, 253
		themeToolbarR, themeToolbarG, themeToolbarB = 245, 246, 248
		themeStatusR, themeStatusG, themeStatusB = 232, 235, 239
		themeTextR, themeTextG, themeTextB = 35, 39, 45
		themeMutedR, themeMutedG, themeMutedB = 96, 101, 109
		themeBorderR, themeBorderG, themeBorderB = 202, 206, 212
		themeAccentR, themeAccentG, themeAccentB = 72, 92, 118
	case "windows10_soft":
		themeWindowR, themeWindowG, themeWindowB = 242, 248, 247
		themePanelR, themePanelG, themePanelB = 253, 255, 255
		themeToolbarR, themeToolbarG, themeToolbarB = 244, 250, 249
		themeStatusR, themeStatusG, themeStatusB = 232, 242, 240
		themeTextR, themeTextG, themeTextB = 29, 47, 46
		themeMutedR, themeMutedG, themeMutedB = 84, 108, 105
		themeBorderR, themeBorderG, themeBorderB = 201, 220, 216
		themeAccentR, themeAccentG, themeAccentB = 26, 137, 124
	default:
		themeWindowR, themeWindowG, themeWindowB = 243, 246, 250
		themePanelR, themePanelG, themePanelB = 255, 255, 255
		themeToolbarR, themeToolbarG, themeToolbarB = 247, 249, 252
		themeStatusR, themeStatusG, themeStatusB = 235, 240, 247
		themeTextR, themeTextG, themeTextB = 31, 41, 55
		themeMutedR, themeMutedG, themeMutedB = 90, 103, 120
		themeBorderR, themeBorderG, themeBorderB = 211, 218, 228
		themeAccentR, themeAccentG, themeAccentB = 47, 111, 237
	}
}

func activateModernVisualStyles() {
	// L'eseguibile storico contiene soltanto le risorse icona. Creiamo un
	// activation context in runtime in modo che BUTTON/COMBO/TREEVIEW usino
	// Common Controls v6 senza cambiare framework o introdurre dipendenze.
	dir := appDataDir()
	_ = os.MkdirAll(dir, 0755)
	path := filepath.Join(dir, "PMLStudio.visualstyles.manifest")
	_ = os.WriteFile(path, []byte(modernVisualStylesManifest), 0644)

	src, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return
	}
	act := ACTCTXW{CbSize: uint32(unsafe.Sizeof(ACTCTXW{})), LpSource: src}
	h, _, _ := pCreateActCtxW.Call(uintptr(unsafe.Pointer(&act)))
	if h == 0 || h == ^uintptr(0) {
		return
	}
	var cookie uintptr
	ok, _, _ := pActivateActCtx.Call(h, uintptr(unsafe.Pointer(&cookie)))
	if ok == 0 {
		pReleaseActCtx.Call(h)
		return
	}
	visualStylesActCtx = syscall.Handle(h)
	visualStylesCookie = cookie
}

func deactivateModernVisualStyles() {
	if visualStylesCookie != 0 {
		pDeactivateActCtx.Call(0, visualStylesCookie)
		visualStylesCookie = 0
	}
	if visualStylesActCtx != 0 {
		pReleaseActCtx.Call(uintptr(visualStylesActCtx))
		visualStylesActCtx = 0
	}
}

func initModernThemeResources() {
	selectThemePalette()
	mk := func(r, g, b byte) syscall.Handle {
		key := rgb(r, g, b)
		if h := themeBrushCache[key]; h != 0 {
			return h
		}
		h, _, _ := pCreateSolidBrush.Call(uintptr(key))
		themeBrushCache[key] = syscall.Handle(h)
		return syscall.Handle(h)
	}
	themeBrushWindow = mk(themeWindowR, themeWindowG, themeWindowB)
	themeBrushPanel = mk(themePanelR, themePanelG, themePanelB)
	themeBrushToolbar = mk(themeToolbarR, themeToolbarG, themeToolbarB)
	themeBrushStatus = mk(themeStatusR, themeStatusG, themeStatusB)

	fontHeight := int32(-settings.UIFontSize * max(100, settings.UIScale) / 100)
	if fontHeight == 0 {
		fontHeight = -16
	}
	f, _, _ := pCreateFontW.Call(
		uintptr(fontHeight), 0, 0, 0, 400, 0, 0, 0,
		1, 0, 0, 5, 0,
		uintptr(unsafe.Pointer(wstr("Segoe UI"))),
	)
	themeUIFont = syscall.Handle(f)
}

func releaseModernThemeResources() {
	for _, h := range themeBrushCache {
		if h != 0 {
			pDeleteObject.Call(uintptr(h))
		}
	}
	themeBrushCache = map[uintptr]syscall.Handle{}
	if themeUIFont != 0 {
		pDeleteObject.Call(uintptr(themeUIFont))
	}
	themeBrushWindow, themeBrushPanel, themeBrushToolbar, themeBrushStatus, themeUIFont = 0, 0, 0, 0, 0
}
func themeWindowBackgroundBrush() syscall.Handle {
	if themeBrushWindow != 0 {
		return themeBrushWindow
	}
	return themeBrushPanel
}

func applyModernWindowFrame(hwnd syscall.Handle) {
	if hwnd == 0 {
		return
	}
	// Aggiorna la title bar secondo il tema corrente.

	dark := int32(0)
	if editorDarkTheme() {
		dark = 1
	}
	if pDwmSetWindowAttribute.Find() == nil {
		pDwmSetWindowAttribute.Call(uintptr(hwnd), 20, uintptr(unsafe.Pointer(&dark)), unsafe.Sizeof(dark))
		pDwmSetWindowAttribute.Call(uintptr(hwnd), 19, uintptr(unsafe.Pointer(&dark)), unsafe.Sizeof(dark))
	}
}

func applyModernControlTheme(hwnd syscall.Handle, class string) {
	if hwnd == 0 {
		return
	}
	if themeUIFont != 0 {
		pSendMessageW.Call(uintptr(hwnd), WM_SETFONT, uintptr(themeUIFont), 1)
	}

	if pSetWindowTheme.Find() == nil {
		switch strings.ToUpper(class) {
		case "SYSTREEVIEW32", "SYSLISTVIEW32", "LISTBOX", "EDIT", "COMBOBOX", "BUTTON":
			name := "Explorer"
			if editorDarkTheme() {
				name = "DarkMode_Explorer"
			}
			pSetWindowTheme.Call(uintptr(hwnd), uintptr(unsafe.Pointer(wstr(name))), 0)
		}
	}
}

func applyModernTreePalette(hwnd syscall.Handle) {
	if hwnd == 0 {
		return
	}
	pSendMessageW.Call(uintptr(hwnd), TVM_SETBKCOLOR, 0, uintptr(rgb(themePanelR, themePanelG, themePanelB)))
	pSendMessageW.Call(uintptr(hwnd), TVM_SETTEXTCOLOR, 0, uintptr(rgb(themeTextR, themeTextG, themeTextB)))
	pSendMessageW.Call(uintptr(hwnd), TVM_SETLINECOLOR, 0, uintptr(rgb(themeBorderR, themeBorderG, themeBorderB)))
}

func modernCtlColor(msg uint32, hdc uintptr, child syscall.Handle) uintptr {
	if hdc == 0 {
		return 0
	}
	pSetTextColor.Call(hdc, uintptr(rgb(themeTextR, themeTextG, themeTextB)))
	pSetBkMode.Call(hdc, TRANSPARENT)

	switch msg {
	case WM_CTLCOLOREDIT, WM_CTLCOLORLISTBOX:
		pSetBkMode.Call(hdc, OPAQUE)
		pSetBkColor.Call(hdc, uintptr(rgb(themePanelR, themePanelG, themePanelB)))
		return uintptr(themeBrushPanel)
	case WM_CTLCOLORSTATIC:
		switch child {
		case hwndToolbarStrip, hwndTabsStrip:
			return uintptr(themeBrushToolbar)
		case hwndStatus:
			pSetTextColor.Call(hdc, uintptr(rgb(themeMutedR, themeMutedG, themeMutedB)))
			return uintptr(themeBrushStatus)
		default:
			return uintptr(themeBrushPanel)
		}
	case WM_CTLCOLORBTN:
		return uintptr(themeBrushPanel)
	}
	return uintptr(themeBrushWindow)
}
