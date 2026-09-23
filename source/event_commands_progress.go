//go:build windows

package main

import (
	"fmt"
	"strings"
	"syscall"
	"unsafe"
)

const (
	progressPaletteClass = "PLMStudioProgressPalette01"
	idProgressBadge      = 9800
	idProgressPokemon    = 9801
	idProgressPokedex    = 9802
	idProgressGym        = 9803
	idProgressQuest      = 9804
	idProgressMission    = 9805
	idProgressSetGym     = 9806
	idProgressBack       = 9807
)

var progressPaletteRegistered, progressPaletteOpen, progressPaletteAccepted bool
var progressPaletteWindow syscall.Handle
var progressPaletteChoice int

func progressPaletteWndProcFn(hwnd syscall.Handle, msg uint32, w, l uintptr) uintptr {
	if msg == WM_COMMAND {
		id := int(loword(w))
		if id >= idProgressBadge && id <= idProgressSetGym {
			progressPaletteChoice = id
			progressPaletteAccepted = true
			pDestroyWindow.Call(uintptr(hwnd))
			return 0
		}
		if id == idProgressBack {
			pDestroyWindow.Call(uintptr(hwnd))
			return 0
		}
	}
	if msg == WM_CLOSE {
		pDestroyWindow.Call(uintptr(hwnd))
		return 0
	}
	if msg == WM_DESTROY {
		progressPaletteOpen = false
		progressPaletteWindow = 0
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(uintptr(hwnd), uintptr(msg), w, l)
	return r
}

var progressPaletteWndProc = syscall.NewCallback(progressPaletteWndProcFn)

func ensureProgressPaletteClass() error {
	if progressPaletteRegistered {
		return nil
	}
	h, _, _ := pGetModuleHandleW.Call(0)
	hi := syscall.Handle(h)
	cur, _, _ := pLoadCursorW.Call(0, IDC_ARROW)
	br, _, _ := pCreateSolidBrush.Call(rgb(244, 244, 244))
	wc := WNDCLASSEX{cbSize: uint32(unsafe.Sizeof(WNDCLASSEX{})), lpfnWndProc: progressPaletteWndProc, hInstance: hi, hCursor: syscall.Handle(cur), hbrBackground: syscall.Handle(br), lpszClassName: wstr(progressPaletteClass)}
	r, _, e := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	if r == 0 {
		return fmt.Errorf("registrazione Condizioni progresso: %v", e)
	}
	progressPaletteRegistered = true
	return nil
}
func showProgressPalette(owner syscall.Handle) (int, bool) {
	if progressPaletteOpen {
		return 0, false
	}
	if err := ensureProgressPaletteClass(); err != nil {
		msgbox("PML Studio - Condizioni", err.Error(), MB_OK|MB_ICONERROR)
		return 0, false
	}
	const ww, wh int32 = 720, 470
	x, y := centerOwnedWindow(ww, wh)
	h, _, _ := pGetModuleHandleW.Call(0)
	hi := syscall.Handle(h)
	progressPaletteOpen = true
	progressPaletteAccepted = false
	progressPaletteChoice = 0
	progressPaletteWindow = createWindow(progressPaletteClass, "Condizioni di progresso", WS_OVERLAPPEDWINDOW|WS_VISIBLE|WS_CLIPCHILDREN, x, y, ww, wh, owner, 0, hi)
	if progressPaletteWindow == 0 {
		progressPaletteOpen = false
		return 0, false
	}
	setWindowIcon(progressPaletteWindow)
	createWindow("STATIC", "Questi comandi fanno da cancello: se la condizione non è soddisfatta, mostrano il messaggio e fermano il resto dell'evento.", WS_CHILD|WS_VISIBLE, 28, 22, 650, 42, progressPaletteWindow, 9810, hi)
	bs := []struct {
		id   int
		t    string
		x, y int32
	}{{idProgressBadge, "Richiedi medaglia", 45, 85}, {idProgressPokemon, "Richiedi Pokémon", 265, 85}, {idProgressPokedex, "Pokémon registrato", 485, 85}, {idProgressGym, "Ginnasio risolto", 45, 170}, {idProgressQuest, "Quest completata", 265, 170}, {idProgressMission, "Missione completata", 485, 170}, {idProgressSetGym, "Segna ginnasio risolto", 45, 255}, {idProgressBack, "← Indietro", 485, 300}}
	for _, b := range bs {
		createWindow("BUTTON", b.t, WS_CHILD|WS_VISIBLE|WS_TABSTOP, b.x, b.y, 190, 58, progressPaletteWindow, uintptr(b.id), hi)
	}
	createWindow("STATIC", "Usali prima di Warp, dialoghi, ricompense, NPC-gate o altre sequenze evento.", WS_CHILD|WS_VISIBLE, 45, 275, 390, 52, progressPaletteWindow, 9811, hi)
	pEnableWindow.Call(uintptr(owner), 0)
	modalLoop(&progressPaletteOpen)
	pEnableWindow.Call(uintptr(owner), 1)
	pSetFocus.Call(uintptr(owner))
	return progressPaletteChoice, progressPaletteAccepted
}
func speciesOptionStrings() []string {
	src := loadEventSpeciesChoices()
	out := make([]string, 0, len(src))
	for _, s := range src {
		out = append(out, s.ID+" — "+s.Name)
	}
	if len(out) == 0 {
		out = []string{"PIKACHU"}
	}
	return out
}
func narrativeMissionOptions(kindContains string) []string {
	d := loadStoryDoc()
	out := []string{}
	for _, m := range d.Missions {
		if kindContains == "" || strings.Contains(strings.ToLower(m.Kind), strings.ToLower(kindContains)) {
			out = append(out, m.ID+" — "+m.Name)
		}
	}
	if len(out) == 0 {
		out = []string{"(nessuna definizione)"}
	}
	return out
}
func configureProgressCondition(choice int) (plmManagedEventCommand, bool) {
	kind := ""
	label := "ID / valore"
	opts := []string(nil)
	initialFail := "Requisito non soddisfatto."
	switch choice {
	case idProgressBadge:
		kind = "badge"
		label = "Medaglia (ID/nome)"
		initialFail = "Non hai ancora la medaglia necessaria."
	case idProgressPokemon:
		kind = "pokemon_owned"
		label = "Pokémon richiesto"
		opts = speciesOptionStrings()
		initialFail = "Non hai il Pokémon richiesto."
	case idProgressPokedex:
		kind = "pokedex_registered"
		label = "Pokémon registrato"
		opts = speciesOptionStrings()
		initialFail = "Questo Pokémon non è ancora registrato nel Pokédex."
	case idProgressGym:
		kind = "gym_completed"
		label = "ID ginnasio"
		initialFail = "Devi prima completare il ginnasio."
	case idProgressQuest:
		kind = "quest_completed"
		label = "Quest"
		opts = narrativeMissionOptions("secondaria")
		initialFail = "Devi prima completare questa quest."
	case idProgressMission:
		kind = "mission_completed"
		label = "Missione"
		opts = narrativeMissionOptions("")
		initialFail = "Devi prima completare questa missione."
	default:
		return plmManagedEventCommand{}, false
	}
	fields := []simpleFormField{}
	if len(opts) > 0 {
		fields = append(fields, simpleFormField{Key: "id", Label: label, Kind: simpleFormCombo, Options: opts})
	} else {
		fields = append(fields, simpleFormField{Key: "id", Label: label, Kind: simpleFormText})
	}
	fields = append(fields, simpleFormField{Key: "fail", Label: "Messaggio se bloccato", Kind: simpleFormMultiline, Initial: initialFail})
	v, ok := showSimpleCommandForm(eventEditorWindow, "Condizione progresso", "Se la condizione fallisce, l'esecuzione dell'evento si ferma qui.", fields)
	if !ok {
		return plmManagedEventCommand{}, false
	}
	id := strings.TrimSpace(v["id"])
	if k := strings.Index(id, " — "); k >= 0 {
		id = id[:k]
	}
	if strings.HasPrefix(id, "(") {
		return plmManagedEventCommand{}, false
	}
	return plmManagedEventCommand{Type: "progress_condition", RequirementType: kind, RequirementID: storyID(id), FailText: strings.TrimSpace(v["fail"])}, true
}
func addProgressConditionEventCommand() {
	for {
		choice, ok := showProgressPalette(eventEditorWindow)
		if !ok {
			return
		}
		if choice == idProgressSetGym {
			v, ok := showSimpleCommandForm(eventEditorWindow, "Segna ginnasio risolto", "Registra il completamento del ginnasio nel salvataggio. Le condizioni 'Ginnasio risolto' leggeranno questo stesso stato.", []simpleFormField{{Key: "id", Label: "ID ginnasio", Kind: simpleFormText}, {Key: "action", Label: "Stato", Kind: simpleFormCombo, Options: []string{"COMPLETE", "RESET"}}})
			if ok {
				id := storyID(v["id"])
				if id != "" {
					c := plmManagedEventCommand{Type: "story_state", StoryType: "gym", StoryID: id, StoryName: id, Action: strings.ToLower(v["action"])}
					if appendManagedEventCommand(c) {
						setToolbarStatus("Stato ginnasio aggiunto: " + id)
					}
				}
			}
			continue
		}
		c, ok := configureProgressCondition(choice)
		if ok && appendManagedEventCommand(c) {
			setToolbarStatus("Condizione progresso aggiunta: " + managedEventCommandLabel(c))
		}
	}
}
