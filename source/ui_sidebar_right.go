//go:build windows

package main

import "syscall"

const idPaletteModeCombo = 1540

var hwndPaletteModeCombo syscall.Handle

func createRightSidebar(hInst syscall.Handle) {
	hwndInspector = createWindow("STATIC", "", WS_CHILD|WS_VISIBLE, 0, 0, 0, 0, hwndMain, 1200, hInst)

	// Selettore reale del tileset della mappa corrente. Le righe vengono
	// popolate dal catalogo dei tileset/pallette numerate del progetto.
	hwndPaletteModeCombo = createWindow("COMBOBOX", "", WS_CHILD|WS_VISIBLE|WS_TABSTOP|CBS_DROPDOWNLIST|WS_VSCROLL, 0, 0, 0, 240, hwndMain, idPaletteModeCombo, hInst)
	comboAdd(hwndPaletteModeCombo, "Tileset")
	pSendMessageW.Call(uintptr(hwndPaletteModeCombo), CB_SETCURSEL, 0, 0)

	hwndPaletteBorders = createWindow("BUTTON", "Blocco bordi", WS_CHILD|WS_VISIBLE|BS_GROUPBOX, 0, 0, 0, 0, hwndMain, 1520, hInst)
	hwndPaletteBorderArea = createWindow("PLMStudioPalette03", "", WS_CHILD|WS_VISIBLE|WS_BORDER, 0, 0, 0, 0, hwndMain, 1521, hInst)

	hwndPaletteAutotile = createWindow("BUTTON", "Autotile", WS_CHILD|WS_VISIBLE|BS_GROUPBOX, 0, 0, 0, 0, hwndMain, 1510, hInst)
	hwndPaletteAutotileList = createWindow("PLMStudioPalette03", "", WS_CHILD|WS_VISIBLE|WS_BORDER, 0, 0, 0, 0, hwndMain, 1511, hInst)

	hwndPaletteTilesets = createWindow("BUTTON", "Tiles", WS_CHILD|WS_VISIBLE|BS_GROUPBOX, 0, 0, 0, 0, hwndMain, 1500, hInst)
	hwndPaletteTilesetList = createWindow("PLMStudioPalette03", "", WS_CHILD|WS_VISIBLE|WS_BORDER|WS_VSCROLL, 0, 0, 0, 0, hwndMain, 1501, hInst)

	hwndViewPlaceholder = createWindow("STATIC", "", WS_CHILD, 0, 0, 0, 0, hwndMain, 1530, hInst)
}
