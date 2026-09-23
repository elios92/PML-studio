//go:build windows

package main

import (
	"fmt"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

func parseThemeColor(value string) (r, g, b byte, err error) {
	value = strings.TrimPrefix(strings.TrimSpace(value), "#")
	if len(value) != 6 {
		return 0, 0, 0, fmt.Errorf("colore non valido: usa #RRGGBB")
	}
	n, err := strconv.ParseUint(value, 16, 24)
	return byte(n >> 16), byte(n >> 8), byte(n), err
}

func editorDarkTheme() bool {
	if settings.Theme == "dark" {
		return true
	}
	if settings.Theme != "system" {
		return false
	}
	var key syscall.Handle
	if syscall.RegOpenKeyEx(syscall.HKEY_CURRENT_USER, wstr(`Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`), 0, syscall.KEY_READ, &key) != nil {
		return false
	}
	defer syscall.RegCloseKey(key)
	var value, kind uint32
	size := uint32(4)
	if syscall.RegQueryValueEx(key, wstr("AppsUseLightTheme"), nil, &kind, (*byte)(unsafe.Pointer(&value)), &size) != nil {
		return false
	}
	return kind == syscall.REG_DWORD && value == 0
}

func applyPreferencePalette() {
	if settings.UISkin == "blue" {
		themeWindowR, themeWindowG, themeWindowB = 230, 239, 250
		themeToolbarR, themeToolbarG, themeToolbarB = 220, 233, 250
		themeAccentR, themeAccentG, themeAccentB = 0, 95, 184
	}
	if editorDarkTheme() {
		themeWindowR, themeWindowG, themeWindowB = 30, 32, 36
		themePanelR, themePanelG, themePanelB = 38, 41, 46
		themeToolbarR, themeToolbarG, themeToolbarB = 46, 50, 57
		themeStatusR, themeStatusG, themeStatusB = 30, 32, 36
		themeTextR, themeTextG, themeTextB = 240, 242, 247
		themeMutedR, themeMutedG, themeMutedB = 182, 193, 207
		themeBorderR, themeBorderG, themeBorderB = 90, 99, 112
		themeAccentR, themeAccentG, themeAccentB = 99, 164, 255
	}
	set := func(value string, r, g, b *byte) {
		if x, y, z, e := parseThemeColor(value); e == nil {
			*r, *g, *b = x, y, z
		}
	}
	set(settings.AccentColor, &themeAccentR, &themeAccentG, &themeAccentB)
	set(settings.SecondaryColor, &themeToolbarR, &themeToolbarG, &themeToolbarB)
	set(settings.PanelColor, &themePanelR, &themePanelG, &themePanelB)
	set(settings.BorderColor, &themeBorderR, &themeBorderG, &themeBorderB)
	themeSelectionR, themeSelectionG, themeSelectionB = themeAccentR, themeAccentG, themeAccentB
	set(settings.SelectionColor, &themeSelectionR, &themeSelectionG, &themeSelectionB)
	// Keep custom panel text readable automatically.
	if settings.PanelColor != "" {
		if int(themePanelR)*299+int(themePanelG)*587+int(themePanelB)*114 < 128000 {
			themeTextR, themeTextG, themeTextB = 240, 242, 247
		} else {
			themeTextR, themeTextG, themeTextB = 31, 41, 55
		}
	}
}

func applyLiveThemeWindow(h uintptr) {
	var name [128]uint16
	pGetClassNameW.Call(h, uintptr(unsafe.Pointer(&name[0])), uintptr(len(name)))
	class := syscall.UTF16ToString(name[:])
	applyModernControlTheme(syscall.Handle(h), class)
	if strings.HasPrefix(class, "PLMStudio") || strings.HasPrefix(class, "PMLStudio") {
		index := int32(-10) // GCLP_HBRBACKGROUND: registered custom classes only.
		user32.NewProc("SetClassLongPtrW").Call(h, uintptr(index), uintptr(themeBrushWindow))
		applyModernWindowFrame(syscall.Handle(h))
	}
	if strings.EqualFold(class, "SysTreeView32") {
		applyModernTreePalette(syscall.Handle(h))
	}
	pRedrawWindow.Call(h, 0, 0, RDW_INVALIDATE|RDW_ALLCHILDREN|0x0004)
}
func applyLiveEditorTheme() {
	oldFont := themeUIFont
	initModernThemeResources()
	thread, _, _ := syscall.NewLazyDLL("kernel32.dll").NewProc("GetCurrentThreadId").Call()
	user32.NewProc("EnumThreadWindows").Call(thread, liveThemeTopCallback, 0)
	if oldFont != 0 {
		pDeleteObject.Call(uintptr(oldFont))
	}
	if hwndMain != 0 {
		layout(hwndMain)
	}
}

var liveThemeChildCallback = syscall.NewCallback(func(h, l uintptr) uintptr { applyLiveThemeWindow(h); return 1 })
var liveThemeTopCallback = syscall.NewCallback(func(h, l uintptr) uintptr {
	applyLiveThemeWindow(h)
	user32.NewProc("EnumChildWindows").Call(h, liveThemeChildCallback, 0)
	return 1
})
var themeBrushCache = map[uintptr]syscall.Handle{}

var themeSelectionR, themeSelectionG, themeSelectionB byte

func resetAppearance(s *Settings) {
	s.UISkin, s.Theme, s.UIFontSize, s.UIScale = "windows10", "light", 16, 100
	s.AccentColor, s.SecondaryColor, s.PanelColor, s.BorderColor, s.SelectionColor = "", "", "", "", ""
}

const MB_YESNO = 0x00000004

type preferenceCustomDraw struct {
	Header nmhdr
	Stage  uint32
	DC     syscall.Handle
	Rect   RECT
	Item   uintptr
	State  uint32
	Param  uintptr
}

func themeButtonNotify(l uintptr) (bool, uintptr) {
	if l == 0 {
		return false, 0
	}
	d := (*preferenceCustomDraw)(unsafe.Pointer(l))
	if int32(d.Header.Code) != -12 {
		return false, 0
	}
	var class [32]uint16
	pGetClassNameW.Call(uintptr(d.Header.HwndFrom), uintptr(unsafe.Pointer(&class[0])), 32)
	if syscall.UTF16ToString(class[:]) != "Button" {
		return false, 0
	}
	if d.Stage == 1 {
		return true, 0x10
	}
	if d.Stage != 2 {
		return false, 0
	}
	id := d.Header.IDFrom
	active := false
	if id >= idToolSelect && id <= idToolEraser {
		active = int(id-idToolSelect) == int(activeMapTool)
	}
	if id >= idLayer1 && id <= idLayer3 {
		active = int(id-idLayer1) == activeLayer
	}
	views := map[uintptr]string{idViewMap: "map", idViewPermissions: "permissions", idViewEvents: "events", idViewEncounters: "encounters", idViewHeader: "header", idViewConnections: "connections", idViewAnimations: "animations", idViewDatabase: "database"}
	if v, ok := views[id]; ok {
		active = v == mode
	}
	if active || d.State&1 != 0 {
		r := d.Rect
		r.Top = r.Bottom - 4
		color := rgb(themeAccentR, themeAccentG, themeAccentB)
		if active {
			color = rgb(themeSelectionR, themeSelectionG, themeSelectionB)
		}
		fill(d.DC, r, color)
	}
	return true, 0
}
