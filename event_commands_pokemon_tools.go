//go:build windows

package main

import (
	"fmt"
	"strings"
	"syscall"
	"unsafe"
)

const (
	pokemonToolsPaletteClass = "PLMStudioPokemonEventTools01"
	idPokemonToolGive        = 8700
	idPokemonToolRemove      = 8701
	idPokemonToolEgg         = 8702
	idPokemonToolHeal        = 8703
	idPokemonToolBack        = 8704
)

type pokemonEventTool int

const (
	pokemonToolNone pokemonEventTool = iota
	pokemonToolGive
	pokemonToolRemove
	pokemonToolEgg
	pokemonToolHeal
)

var pokemonToolsRegistered, pokemonToolsOpen, pokemonToolsAccepted bool
var pokemonToolsWindow syscall.Handle
var pokemonToolsResult pokemonEventTool

func pokemonToolsWndProcFn(hwnd syscall.Handle, msg uint32, w, l uintptr) uintptr {
	switch msg {
	case WM_COMMAND:
		switch int(loword(w)) {
		case idPokemonToolGive:
			pokemonToolsResult = pokemonToolGive
			pokemonToolsAccepted = true
			pDestroyWindow.Call(uintptr(hwnd))
			return 0
		case idPokemonToolRemove:
			pokemonToolsResult = pokemonToolRemove
			pokemonToolsAccepted = true
			pDestroyWindow.Call(uintptr(hwnd))
			return 0
		case idPokemonToolEgg:
			pokemonToolsResult = pokemonToolEgg
			pokemonToolsAccepted = true
			pDestroyWindow.Call(uintptr(hwnd))
			return 0
		case idPokemonToolHeal:
			pokemonToolsResult = pokemonToolHeal
			pokemonToolsAccepted = true
			pDestroyWindow.Call(uintptr(hwnd))
			return 0
		case idPokemonToolBack:
			pDestroyWindow.Call(uintptr(hwnd))
			return 0
		}
	case WM_CLOSE:
		pDestroyWindow.Call(uintptr(hwnd))
		return 0
	case WM_DESTROY:
		pokemonToolsOpen = false
		pokemonToolsWindow = 0
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(uintptr(hwnd), uintptr(msg), w, l)
	return r
}

var pokemonToolsWndProc = syscall.NewCallback(pokemonToolsWndProcFn)

func ensurePokemonToolsClass() error {
	if pokemonToolsRegistered {
		return nil
	}
	h, _, _ := pGetModuleHandleW.Call(0)
	hi := syscall.Handle(h)
	cur, _, _ := pLoadCursorW.Call(0, IDC_ARROW)
	brush, _, _ := pCreateSolidBrush.Call(rgb(244, 244, 244))
	wc := WNDCLASSEX{cbSize: uint32(unsafe.Sizeof(WNDCLASSEX{})), lpfnWndProc: pokemonToolsWndProc, hInstance: hi, hCursor: syscall.Handle(cur), hbrBackground: syscall.Handle(brush), lpszClassName: wstr(pokemonToolsPaletteClass)}
	r, _, err := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	if r == 0 {
		return fmt.Errorf("registrazione Pokémon evento: %v", err)
	}
	pokemonToolsRegistered = true
	return nil
}

func showPokemonToolsPalette(owner syscall.Handle) (pokemonEventTool, bool) {
	if pokemonToolsOpen {
		return pokemonToolNone, false
	}
	if err := ensurePokemonToolsClass(); err != nil {
		msgbox("PML Studio - Pokémon evento", err.Error(), MB_OK|MB_ICONERROR)
		return pokemonToolNone, false
	}
	const ww, wh int32 = 620, 390
	x, y := centerOwnedWindow(ww, wh)
	h, _, _ := pGetModuleHandleW.Call(0)
	hi := syscall.Handle(h)
	pokemonToolsOpen = true
	pokemonToolsAccepted = false
	pokemonToolsResult = pokemonToolNone
	pokemonToolsWindow = createWindow(pokemonToolsPaletteClass, "Pokémon evento", WS_OVERLAPPEDWINDOW|WS_VISIBLE|WS_CLIPCHILDREN, x, y, ww, wh, owner, 0, hi)
	if pokemonToolsWindow == 0 {
		pokemonToolsOpen = false
		return pokemonToolNone, false
	}
	setWindowIcon(pokemonToolsWindow)
	createWindow("STATIC", "Azioni Pokémon usate direttamente nella costruzione degli eventi. Le battaglie sono nella sezione Battaglie della palette principale.", WS_CHILD|WS_VISIBLE, 32, 24, 540, 44, pokemonToolsWindow, 8710, hi)
	createWindow("BUTTON", "Dai Pokémon", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 48, 86, 220, 58, pokemonToolsWindow, idPokemonToolGive, hi)
	createWindow("BUTTON", "Rimuovi Pokémon", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 330, 86, 220, 58, pokemonToolsWindow, idPokemonToolRemove, hi)
	createWindow("BUTTON", "Dai Uovo", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 48, 166, 220, 58, pokemonToolsWindow, idPokemonToolEgg, hi)
	createWindow("BUTTON", "Cura squadra", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 330, 166, 220, 58, pokemonToolsWindow, idPokemonToolHeal, hi)
	createWindow("BUTTON", "← Indietro", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 190, 262, 220, 46, pokemonToolsWindow, idPokemonToolBack, hi)
	pEnableWindow.Call(uintptr(owner), 0)
	modalLoop(&pokemonToolsOpen)
	pEnableWindow.Call(uintptr(owner), 1)
	pSetFocus.Call(uintptr(owner))
	return pokemonToolsResult, pokemonToolsAccepted
}

func showPokemonSpeciesEventDialog(owner syscall.Handle, title string, withLevel bool) (string, int, bool) {
	eventCommandDialogModeValue = eventCommandFixedPokemon
	eventCommandDialogCatalog = loadEventSpeciesChoices()
	eventCommandDialogFiltered = nil
	eventCommandDialogOneShot = 0
	eventCommandDialogLegendary = 0
	ok := runEventCommandDialog(owner, title, 760, 590, func(hInst syscall.Handle) {
		createWindow("STATIC", "Cerca specie:", WS_CHILD|WS_VISIBLE, 20, 18, 130, 24, eventCommandDialogWindow, 8720, hInst)
		eventCommandDialogSearch = createWindow("EDIT", "", WS_CHILD|WS_VISIBLE|WS_BORDER|WS_TABSTOP, 150, 12, 560, 30, eventCommandDialogWindow, idEventCmdSearch, hInst)
		eventCommandDialogList = createWindow("LISTBOX", "", WS_CHILD|WS_VISIBLE|WS_BORDER|WS_VSCROLL|WS_TABSTOP|LBS_NOTIFY, 20, 54, 690, 340, eventCommandDialogWindow, idEventCmdList, hInst)
		createWindow("STATIC", "Specie / ID:", WS_CHILD|WS_VISIBLE, 20, 414, 120, 24, eventCommandDialogWindow, 8721, hInst)
		eventCommandDialogID = createWindow("EDIT", "", WS_CHILD|WS_VISIBLE|WS_BORDER|WS_TABSTOP, 145, 408, 300, 30, eventCommandDialogWindow, idEventCmdID, hInst)
		if withLevel {
			createWindow("STATIC", "Livello:", WS_CHILD|WS_VISIBLE, 470, 414, 70, 24, eventCommandDialogWindow, 8722, hInst)
			eventCommandDialogValue = createWindow("EDIT", "5", WS_CHILD|WS_VISIBLE|WS_BORDER|WS_TABSTOP, 542, 408, 80, 30, eventCommandDialogWindow, idEventCmdValue, hInst)
		} else {
			eventCommandDialogValue = createWindow("EDIT", "1", WS_CHILD, 0, 0, 0, 0, eventCommandDialogWindow, idEventCmdValue, hInst)
		}
		createWindow("STATIC", "Seleziona una specie reale del progetto. Doppio clic = conferma.", WS_CHILD|WS_VISIBLE, 20, 454, 600, 28, eventCommandDialogWindow, 8723, hInst)
		createWindow("BUTTON", "Conferma", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 490, 502, 105, 36, eventCommandDialogWindow, idEventCmdOK, hInst)
		createWindow("BUTTON", "Annulla", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 605, 502, 105, 36, eventCommandDialogWindow, idEventCmdCancel, hInst)
		refreshEventCommandDialogFilter()
	})
	return strings.ToUpper(strings.TrimSpace(eventCommandDialogResultID)), eventCommandDialogResultVal, ok
}

func addPokemonEventToolsCommand() {
	tool, ok := showPokemonToolsPalette(eventEditorWindow)
	if !ok {
		return
	}
	switch tool {
	case pokemonToolGive:
		sp, lv, ok := showPokemonSpeciesEventDialog(eventEditorWindow, "Dai Pokémon", true)
		if !ok || sp == "" {
			return
		}
		if appendManagedEventCommand(plmManagedEventCommand{Type: "pokemon_give", Species: sp, Level: maxIntEventCmd(lv, 1)}) {
			setToolbarStatus("Comando evento aggiunto: Dai Pokémon " + sp + ".")
		}
	case pokemonToolRemove:
		sp, _, ok := showPokemonSpeciesEventDialog(eventEditorWindow, "Rimuovi Pokémon", false)
		if !ok || sp == "" {
			return
		}
		if appendManagedEventCommand(plmManagedEventCommand{Type: "pokemon_remove", Species: sp}) {
			setToolbarStatus("Comando evento aggiunto: Rimuovi Pokémon " + sp + ".")
		}
	case pokemonToolEgg:
		sp, _, ok := showPokemonSpeciesEventDialog(eventEditorWindow, "Dai Uovo", false)
		if !ok || sp == "" {
			return
		}
		if appendManagedEventCommand(plmManagedEventCommand{Type: "pokemon_egg", Species: sp}) {
			setToolbarStatus("Comando evento aggiunto: Dai Uovo " + sp + ".")
		}
	case pokemonToolHeal:
		if appendManagedEventCommand(plmManagedEventCommand{Type: "pokemon_heal"}) {
			setToolbarStatus("Comando evento aggiunto: Cura squadra.")
		}
	}
}
