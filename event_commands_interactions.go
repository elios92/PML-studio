//go:build windows

package main

import (
	"fmt"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

const (
	interactionPaletteClass = "PLMStudioMapInteractions01"
	idInteractLever         = 9900
	idInteractButton        = 9901
	idInteractElevator      = 9902
	idInteractStairs        = 9903
	idInteractGate          = 9904
	idInteractBack          = 9905
)

var interactionPaletteRegistered, interactionPaletteOpen, interactionPaletteAccepted bool
var interactionPaletteWindow syscall.Handle
var interactionPaletteChoice int

func interactionPaletteWndProcFn(hwnd syscall.Handle, msg uint32, w, l uintptr) uintptr {
	if msg == WM_COMMAND {
		id := int(loword(w))
		if id >= idInteractLever && id <= idInteractGate {
			interactionPaletteChoice = id
			interactionPaletteAccepted = true
			pDestroyWindow.Call(uintptr(hwnd))
			return 0
		}
		if id == idInteractBack {
			pDestroyWindow.Call(uintptr(hwnd))
			return 0
		}
	}
	if msg == WM_CLOSE {
		pDestroyWindow.Call(uintptr(hwnd))
		return 0
	}
	if msg == WM_DESTROY {
		interactionPaletteOpen = false
		interactionPaletteWindow = 0
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(uintptr(hwnd), uintptr(msg), w, l)
	return r
}

var interactionPaletteWndProc = syscall.NewCallback(interactionPaletteWndProcFn)

func ensureInteractionPaletteClass() error {
	if interactionPaletteRegistered {
		return nil
	}
	h, _, _ := pGetModuleHandleW.Call(0)
	hi := syscall.Handle(h)
	cur, _, _ := pLoadCursorW.Call(0, IDC_ARROW)
	br, _, _ := pCreateSolidBrush.Call(rgb(244, 244, 244))
	wc := WNDCLASSEX{cbSize: uint32(unsafe.Sizeof(WNDCLASSEX{})), lpfnWndProc: interactionPaletteWndProc, hInstance: hi, hCursor: syscall.Handle(cur), hbrBackground: syscall.Handle(br), lpszClassName: wstr(interactionPaletteClass)}
	r, _, e := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	if r == 0 {
		return fmt.Errorf("registrazione Interazioni mappa: %v", e)
	}
	interactionPaletteRegistered = true
	return nil
}
func showInteractionPalette(owner syscall.Handle) (int, bool) {
	if interactionPaletteOpen {
		return 0, false
	}
	if err := ensureInteractionPaletteClass(); err != nil {
		msgbox("PML Studio - Interazioni", err.Error(), MB_OK|MB_ICONERROR)
		return 0, false
	}
	const ww, wh int32 = 700, 430
	x, y := centerOwnedWindow(ww, wh)
	h, _, _ := pGetModuleHandleW.Call(0)
	hi := syscall.Handle(h)
	interactionPaletteOpen = true
	interactionPaletteAccepted = false
	interactionPaletteChoice = 0
	interactionPaletteWindow = createWindow(interactionPaletteClass, "Interazioni mappa", WS_OVERLAPPEDWINDOW|WS_VISIBLE|WS_CLIPCHILDREN, x, y, ww, wh, owner, 0, hi)
	if interactionPaletteWindow == 0 {
		interactionPaletteOpen = false
		return 0, false
	}
	setWindowIcon(interactionPaletteWindow)
	createWindow("STATIC", "Preset specifici che assemblano comandi evento esistenti senza duplicare il sistema Warp/Movimento.", WS_CHILD|WS_VISIBLE, 28, 22, 640, 42, interactionPaletteWindow, 9910, hi)
	bs := []struct {
		id   int
		t    string
		x, y int32
	}{{idInteractLever, "Leva", 45, 85}, {idInteractButton, "Pulsante", 260, 85}, {idInteractElevator, "Ascensore", 475, 85}, {idInteractStairs, "Scala / Cambio piano", 45, 175}, {idInteractGate, "NPC blocca passaggio", 260, 175}, {idInteractBack, "← Indietro", 475, 285}}
	for _, b := range bs {
		createWindow("BUTTON", b.t, WS_CHILD|WS_VISIBLE|WS_TABSTOP, b.x, b.y, 185, 60, interactionPaletteWindow, uintptr(b.id), hi)
	}
	createWindow("STATIC", "NPC blocca passaggio usa le stesse Condizioni progresso, poi apre il passaggio con Self Switch A.", WS_CHILD|WS_VISIBLE, 45, 270, 390, 58, interactionPaletteWindow, 9911, hi)
	pEnableWindow.Call(uintptr(owner), 0)
	modalLoop(&interactionPaletteOpen)
	pEnableWindow.Call(uintptr(owner), 1)
	pSetFocus.Call(uintptr(owner))
	return interactionPaletteChoice, interactionPaletteAccepted
}
func addLeverOrButton(kind string) {
	v, ok := showSimpleCommandForm(eventEditorWindow, strings.Title(kind), "Il comando può impostare o invertire un Self Switch. Il runtime esegue l'azione prima di proseguire.", []simpleFormField{{Key: "switch", Label: "Self Switch", Kind: simpleFormCombo, Options: []string{"A", "B", "C", "D"}}, {Key: "action", Label: "Azione", Kind: simpleFormCombo, Options: []string{"TOGGLE", "ON", "OFF"}}, {Key: "se", Label: "SE opzionale", Kind: simpleFormText, Initial: ""}})
	if !ok {
		return
	}
	c := plmManagedEventCommand{Type: "map_interaction", Interaction: kind, SelfSwitch: v["switch"], Action: strings.ToLower(v["action"]), AudioName: strings.TrimSpace(v["se"])}
	if appendManagedEventCommand(c) {
		setToolbarStatus("Interazione aggiunta: " + kind)
	}
}
func parseElevatorDestinations(text string) ([]plmDestination, error) {
	out := []plmDestination{}
	for i, raw := range strings.Split(strings.ReplaceAll(text, "\r", ""), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		p := strings.Split(line, "|")
		if len(p) < 4 {
			return nil, fmt.Errorf("riga %d: usa Nome|MapID|X|Y", i+1)
		}
		mid, e1 := strconv.Atoi(strings.TrimSpace(p[1]))
		x, e2 := strconv.Atoi(strings.TrimSpace(p[2]))
		y, e3 := strconv.Atoi(strings.TrimSpace(p[3]))
		if e1 != nil || e2 != nil || e3 != nil || mid <= 0 {
			return nil, fmt.Errorf("riga %d: coordinate/MapID non validi", i+1)
		}
		out = append(out, plmDestination{Name: strings.TrimSpace(p[0]), MapID: mid, X: x, Y: y})
	}
	if len(out) < 2 {
		return nil, fmt.Errorf("inserisci almeno 2 piani")
	}
	return out, nil
}
func addElevatorInteraction() {
	initial := "Piano Terra|1|10|10\r\nPiano 1|2|5|8"
	v, ok := showSimpleCommandForm(eventEditorWindow, "Ascensore", "Un piano per riga: Nome|MapID|X|Y. Durante il gioco viene mostrata la scelta e poi eseguito il trasferimento selezionato.", []simpleFormField{{Key: "dest", Label: "Piani / Destinazioni", Kind: simpleFormMultiline, Initial: initial}})
	if !ok {
		return
	}
	dest, err := parseElevatorDestinations(v["dest"])
	if err != nil {
		msgbox("PML Studio - Ascensore", err.Error(), MB_OK|MB_ICONERROR)
		return
	}
	c := plmManagedEventCommand{Type: "map_interaction", Interaction: "elevator", Destinations: dest}
	if appendManagedEventCommand(c) {
		setToolbarStatus(fmt.Sprintf("Ascensore aggiunto: %d piani.", len(dest)))
	}
}
func addStairsInteraction() {
	addWarpEventCommand()
	setToolbarStatus("Scala/Cambio piano: usa il Warp reale e la casella Movimento 00 già implementata.")
}
func addNPCGateInteraction() {
	choice, ok := showProgressPalette(eventEditorWindow)
	if !ok {
		return
	}
	cond, ok := configureProgressCondition(choice)
	if !ok {
		return
	}
	if !appendManagedEventCommand(cond) {
		return
	}
	appendManagedEventCommand(plmManagedEventCommand{Type: "self_switch", SelfSwitch: "A", State: "ON"})
	ensureSimpleConsumedBlankPage()
	if eventEditorPageIndex >= 0 && eventEditorPageIndex < len(eventEditorPages) {
		eventEditorPages[eventEditorPageIndex].Trigger = 0
		eventEditorPages[eventEditorPageIndex].Through = false
		loadEventEditorPageToControls()
	}
	setToolbarStatus("NPC blocca passaggio: condizione + apertura Self Switch A create.")
}
func addMapInteractionsEventCommand() {
	for {
		choice, ok := showInteractionPalette(eventEditorWindow)
		if !ok {
			return
		}
		switch choice {
		case idInteractLever:
			addLeverOrButton("lever")
		case idInteractButton:
			addLeverOrButton("button")
		case idInteractElevator:
			addElevatorInteraction()
		case idInteractStairs:
			addStairsInteraction()
		case idInteractGate:
			addNPCGateInteraction()
		}
	}
}
