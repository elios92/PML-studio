//go:build windows

package main

import (
	"fmt"
	"strings"
	"syscall"
	"unsafe"
)

const (
	logicPaletteClass   = "PLMStudioEventLogicPalette01"
	idLogicIf           = 10100
	idLogicElse         = 10101
	idLogicEnd          = 10102
	idLogicGlobalSwitch = 10103
	idLogicAreaTrigger  = 10104
	idLogicPathfind     = 10105
	idLogicWaitZone     = 10106
	idLogicBack         = 10107
)

var logicPaletteRegistered, logicPaletteOpen, logicPaletteAccepted bool
var logicPaletteWindow syscall.Handle
var logicPaletteChoice int

func logicPaletteWndProcFn(hwnd syscall.Handle, msg uint32, w, l uintptr) uintptr {
	if msg == WM_COMMAND {
		id := int(loword(w))
		if id >= idLogicIf && id <= idLogicWaitZone {
			logicPaletteChoice = id
			logicPaletteAccepted = true
			pDestroyWindow.Call(uintptr(hwnd))
			return 0
		}
		if id == idLogicBack {
			pDestroyWindow.Call(uintptr(hwnd))
			return 0
		}
	}
	if msg == WM_CLOSE {
		pDestroyWindow.Call(uintptr(hwnd))
		return 0
	}
	if msg == WM_DESTROY {
		logicPaletteOpen = false
		logicPaletteWindow = 0
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(uintptr(hwnd), uintptr(msg), w, l)
	return r
}

var logicPaletteWndProc = syscall.NewCallback(logicPaletteWndProcFn)

func ensureLogicPaletteClass() error {
	if logicPaletteRegistered {
		return nil
	}
	h, _, _ := pGetModuleHandleW.Call(0)
	hi := syscall.Handle(h)
	cur, _, _ := pLoadCursorW.Call(0, IDC_ARROW)
	br, _, _ := pCreateSolidBrush.Call(rgb(244, 244, 244))
	wc := WNDCLASSEX{cbSize: uint32(unsafe.Sizeof(WNDCLASSEX{})), lpfnWndProc: logicPaletteWndProc, hInstance: hi, hCursor: syscall.Handle(cur), hbrBackground: syscall.Handle(br), lpszClassName: wstr(logicPaletteClass)}
	r, _, e := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	if r == 0 {
		return fmt.Errorf("registrazione Logica/Trigger: %v", e)
	}
	logicPaletteRegistered = true
	return nil
}
func showLogicPalette(owner syscall.Handle) (int, bool) {
	if logicPaletteOpen {
		return 0, false
	}
	if err := ensureLogicPaletteClass(); err != nil {
		msgbox("PML Studio - Logica", err.Error(), MB_OK|MB_ICONERROR)
		return 0, false
	}
	const ww, wh int32 = 760, 500
	x, y := centerOwnedWindow(ww, wh)
	h, _, _ := pGetModuleHandleW.Call(0)
	hi := syscall.Handle(h)
	logicPaletteOpen = true
	logicPaletteAccepted = false
	logicPaletteChoice = 0
	logicPaletteWindow = createWindow(logicPaletteClass, "Logica / Trigger evento", WS_OVERLAPPEDWINDOW|WS_VISIBLE|WS_CLIPCHILDREN, x, y, ww, wh, owner, 0, hi)
	if logicPaletteWindow == 0 {
		logicPaletteOpen = false
		return 0, false
	}
	setWindowIcon(logicPaletteWindow)
	createWindow("STATIC", "Blocchi logici e sincronizzazione per costruire eventi complessi senza script manuali.", WS_CHILD|WS_VISIBLE, 28, 20, 690, 36, logicPaletteWindow, 10120, hi)
	bs := []struct {
		id   int
		t    string
		x, y int32
	}{
		{idLogicIf, "SE / condizione", 35, 75}, {idLogicElse, "ALTRIMENTI", 205, 75}, {idLogicEnd, "FINE SE", 375, 75}, {idLogicGlobalSwitch, "Switch globale", 545, 75},
		{idLogicAreaTrigger, "Trigger per area", 35, 170}, {idLogicPathfind, "Path NPC → punto", 205, 170}, {idLogicWaitZone, "Attendi arrivo in zona", 375, 170}, {idLogicBack, "← Indietro", 545, 300},
	}
	for _, b := range bs {
		createWindow("BUTTON", b.t, WS_CHILD|WS_VISIBLE|WS_TABSTOP, b.x, b.y, 150, 60, logicPaletteWindow, uintptr(b.id), hi)
	}
	createWindow("STATIC", "SE/ALTRIMENTI/FINE SE possono essere annidati. Path e attesa non proseguono finché la condizione di movimento non è conclusa.", WS_CHILD|WS_VISIBLE, 35, 260, 480, 70, logicPaletteWindow, 10121, hi)
	pEnableWindow.Call(uintptr(owner), 0)
	modalLoop(&logicPaletteOpen)
	pEnableWindow.Call(uintptr(owner), 1)
	pSetFocus.Call(uintptr(owner))
	return logicPaletteChoice, logicPaletteAccepted
}

func addLogicIfCommand() {
	v, ok := showSimpleCommandForm(eventEditorWindow, "SE / Condizione", "Inserisce l'inizio di un blocco condizionale. Aggiungi poi ALTRIMENTI (opzionale) e FINE SE.", []simpleFormField{
		{Key: "kind", Label: "Condizione", Kind: simpleFormCombo, Options: []string{"Switch globale ON", "Switch globale OFF", "Self Switch classica ON", "Self Switch classica OFF", "Unlimited Self Switch ON", "Unlimited Self Switch OFF", "Variabile >= valore", "Variabile == valore", "Missione completata", "Puzzle completato"}},
		{Key: "id", Label: "ID / Nome / Variabile", Kind: simpleFormText},
		{Key: "value", Label: "Valore numerico (se serve)", Kind: simpleFormNumber, Initial: "0"},
	})
	if !ok {
		return
	}
	kind := strings.ToLower(strings.TrimSpace(v["kind"]))
	ct := ""
	cmp := ""
	state := ""
	switch {
	case strings.HasPrefix(kind, "switch globale on"):
		ct = "global_switch"
		state = "on"
	case strings.HasPrefix(kind, "switch globale off"):
		ct = "global_switch"
		state = "off"
	case strings.HasPrefix(kind, "self switch classica on"):
		ct = "self_switch"
		state = "on"
	case strings.HasPrefix(kind, "self switch classica off"):
		ct = "self_switch"
		state = "off"
	case strings.HasPrefix(kind, "unlimited self switch on"):
		ct = "unlimited_self_switch"
		state = "on"
	case strings.HasPrefix(kind, "unlimited self switch off"):
		ct = "unlimited_self_switch"
		state = "off"
	case strings.HasPrefix(kind, "variabile >="):
		ct = "variable"
		cmp = ">="
	case strings.HasPrefix(kind, "variabile =="):
		ct = "variable"
		cmp = "=="
	case strings.HasPrefix(kind, "missione"):
		ct = "mission_completed"
	case strings.HasPrefix(kind, "puzzle"):
		ct = "puzzle_completed"
	}
	id := strings.TrimSpace(v["id"])
	if id == "" {
		return
	}
	c := plmManagedEventCommand{Type: "logic_if", ConditionType: ct, RequirementID: storyID(id), Compare: cmp, Value: formInt(v, "value", 0), State: state, SelfSwitch: strings.ToUpper(id)}
	if appendManagedEventCommand(c) {
		setToolbarStatus("Blocco SE aggiunto.")
	}
}

func addAreaTriggerCommand() {
	v, ok := showSimpleCommandForm(eventEditorWindow, "Trigger per area", "La pagina viene eseguita in parallelo ma i comandi successivi partono solo quando il giocatore entra nell'area.", []simpleFormField{
		{Key: "mode", Label: "Area", Kind: simpleFormCombo, Options: []string{"Raggio attorno all'evento", "Rettangolo assoluto"}},
		{Key: "radius", Label: "Raggio (tile)", Kind: simpleFormNumber, Initial: "2"},
		{Key: "x", Label: "X rettangolo", Kind: simpleFormNumber, Initial: "0"}, {Key: "y", Label: "Y rettangolo", Kind: simpleFormNumber, Initial: "0"},
		{Key: "w", Label: "Larghezza", Kind: simpleFormNumber, Initial: "1"}, {Key: "h", Label: "Altezza", Kind: simpleFormNumber, Initial: "1"},
	})
	if !ok {
		return
	}
	c := plmManagedEventCommand{Type: "area_trigger", Radius: maxIntEventCmd(formInt(v, "radius", 2), 0), X: formInt(v, "x", 0), Y: formInt(v, "y", 0), Width: maxIntEventCmd(formInt(v, "w", 1), 1), Height: maxIntEventCmd(formInt(v, "h", 1), 1)}
	if strings.HasPrefix(strings.ToLower(v["mode"]), "rettangolo") {
		c.Radius = 0
	}
	if eventEditorPageIndex >= 0 && eventEditorPageIndex < len(eventEditorPages) {
		eventEditorPages[eventEditorPageIndex].Trigger = 4
	}
	if appendManagedEventCommand(c) {
		setToolbarStatus("Trigger per area aggiunto (Processo parallelo).")
	}
}

func addPathfindCommand() {
	v, ok := showSimpleCommandForm(eventEditorWindow, "Path NPC verso coordinate", "Il runtime calcola un percorso passabile e attende che l'NPC arrivi prima di proseguire. Se non trova un percorso, segnala errore invece di saltarlo.", []simpleFormField{
		{Key: "event", Label: "ID NPC / Evento", Kind: simpleFormNumber, Initial: "0"}, {Key: "x", Label: "Destinazione X", Kind: simpleFormNumber, Initial: "0"}, {Key: "y", Label: "Destinazione Y", Kind: simpleFormNumber, Initial: "0"},
	})
	if !ok {
		return
	}
	c := plmManagedEventCommand{Type: "pathfind_move", Target: "event", TargetEventID: formInt(v, "event", 0), X: formInt(v, "x", 0), Y: formInt(v, "y", 0), Wait: true}
	if appendManagedEventCommand(c) {
		setToolbarStatus("Path NPC bloccante aggiunto.")
	}
}

func addWaitZoneCommand() {
	v, ok := showSimpleCommandForm(eventEditorWindow, "Attendi arrivo in zona", "L'evento resta sincronizzato finché il bersaglio non entra nel rettangolo indicato. Timeout 0 = nessun timeout.", []simpleFormField{
		{Key: "target", Label: "Bersaglio", Kind: simpleFormCombo, Options: []string{"Giocatore", "NPC / Evento"}}, {Key: "event", Label: "ID NPC (se serve)", Kind: simpleFormNumber, Initial: "0"},
		{Key: "x", Label: "Zona X", Kind: simpleFormNumber, Initial: "0"}, {Key: "y", Label: "Zona Y", Kind: simpleFormNumber, Initial: "0"}, {Key: "w", Label: "Larghezza", Kind: simpleFormNumber, Initial: "1"}, {Key: "h", Label: "Altezza", Kind: simpleFormNumber, Initial: "1"}, {Key: "timeout", Label: "Timeout (frame, 0 = infinito)", Kind: simpleFormNumber, Initial: "0"},
	})
	if !ok {
		return
	}
	target := "player"
	if strings.HasPrefix(strings.ToLower(v["target"]), "npc") {
		target = "event"
	}
	c := plmManagedEventCommand{Type: "wait_until_zone", Target: target, TargetEventID: formInt(v, "event", 0), X: formInt(v, "x", 0), Y: formInt(v, "y", 0), Width: maxIntEventCmd(formInt(v, "w", 1), 1), Height: maxIntEventCmd(formInt(v, "h", 1), 1), Timeout: maxIntEventCmd(formInt(v, "timeout", 0), 0), Wait: true}
	if appendManagedEventCommand(c) {
		setToolbarStatus("Attesa arrivo in zona aggiunta.")
	}
}

func addEventLogicCommand() {
	for {
		ch, ok := showLogicPalette(eventEditorWindow)
		if !ok {
			return
		}
		switch ch {
		case idLogicIf:
			addLogicIfCommand()
		case idLogicElse:
			if appendManagedEventCommand(plmManagedEventCommand{Type: "logic_else"}) {
				setToolbarStatus("ALTRIMENTI aggiunto.")
			}
		case idLogicEnd:
			if appendManagedEventCommand(plmManagedEventCommand{Type: "logic_end"}) {
				setToolbarStatus("FINE SE aggiunto.")
			}
		case idLogicGlobalSwitch:
			addGlobalFlagCommand()
		case idLogicAreaTrigger:
			addAreaTriggerCommand()
		case idLogicPathfind:
			addPathfindCommand()
		case idLogicWaitZone:
			addWaitZoneCommand()
		}
	}
}
