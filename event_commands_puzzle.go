//go:build windows

package main

import (
	"fmt"
	"strings"
	"syscall"
	"unsafe"
)

const (
	puzzlePaletteClass    = "PLMStudioPuzzlePalette01"
	idPuzzlePath          = 10200
	idPuzzleSequenceInput = 10201
	idPuzzleCondition     = 10202
	idPuzzleTimer         = 10203
	idPuzzleCounter       = 10204
	idPuzzleBack          = 10205
)

var puzzlePaletteRegistered, puzzlePaletteOpen, puzzlePaletteAccepted bool
var puzzlePaletteWindow syscall.Handle
var puzzlePaletteChoice int

// Mouse path capture lives on the real map canvas. It is intentionally editor-only:
// no Terrain Tag or movement permission is modified while drawing the puzzle path.
var puzzlePathCaptureActive bool
var puzzlePathCaptureCancelled bool
var puzzlePathCapturePoints []plmMapPoint
var puzzlePathCaptureOldMode string

func puzzlePaletteWndProcFn(hwnd syscall.Handle, msg uint32, w, l uintptr) uintptr {
	if msg == WM_COMMAND {
		id := int(loword(w))
		if id >= idPuzzlePath && id <= idPuzzleCounter {
			puzzlePaletteChoice = id
			puzzlePaletteAccepted = true
			pDestroyWindow.Call(uintptr(hwnd))
			return 0
		}
		if id == idPuzzleBack {
			pDestroyWindow.Call(uintptr(hwnd))
			return 0
		}
	}
	if msg == WM_CLOSE {
		pDestroyWindow.Call(uintptr(hwnd))
		return 0
	}
	if msg == WM_DESTROY {
		puzzlePaletteOpen = false
		puzzlePaletteWindow = 0
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(uintptr(hwnd), uintptr(msg), w, l)
	return r
}

var puzzlePaletteWndProc = syscall.NewCallback(puzzlePaletteWndProcFn)

func ensurePuzzlePaletteClass() error {
	if puzzlePaletteRegistered {
		return nil
	}
	h, _, _ := pGetModuleHandleW.Call(0)
	hi := syscall.Handle(h)
	cur, _, _ := pLoadCursorW.Call(0, IDC_ARROW)
	br, _, _ := pCreateSolidBrush.Call(rgb(244, 244, 244))
	wc := WNDCLASSEX{cbSize: uint32(unsafe.Sizeof(WNDCLASSEX{})), lpfnWndProc: puzzlePaletteWndProc, hInstance: hi, hCursor: syscall.Handle(cur), hbrBackground: syscall.Handle(br), lpszClassName: wstr(puzzlePaletteClass)}
	r, _, e := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	if r == 0 {
		return fmt.Errorf("registrazione Puzzle: %v", e)
	}
	puzzlePaletteRegistered = true
	return nil
}
func showPuzzlePalette(owner syscall.Handle) (int, bool) {
	if puzzlePaletteOpen {
		return 0, false
	}
	if err := ensurePuzzlePaletteClass(); err != nil {
		msgbox("PML Studio - Puzzle", err.Error(), MB_OK|MB_ICONERROR)
		return 0, false
	}
	const ww, wh int32 = 720, 460
	x, y := centerOwnedWindow(ww, wh)
	h, _, _ := pGetModuleHandleW.Call(0)
	hi := syscall.Handle(h)
	puzzlePaletteOpen = true
	puzzlePaletteAccepted = false
	puzzlePaletteChoice = 0
	puzzlePaletteWindow = createWindow(puzzlePaletteClass, "Puzzle evento", WS_OVERLAPPEDWINDOW|WS_VISIBLE|WS_CLIPCHILDREN, x, y, ww, wh, owner, 0, hi)
	if puzzlePaletteWindow == 0 {
		puzzlePaletteOpen = false
		return 0, false
	}
	setWindowIcon(puzzlePaletteWindow)
	createWindow("STATIC", "Puzzle costruiti con eventi: percorsi, sequenze di pulsanti, timer, contatori e porte collegate allo stesso Puzzle ID.", WS_CHILD|WS_VISIBLE, 28, 20, 650, 44, puzzlePaletteWindow, 10220, hi)
	bs := []struct {
		id   int
		t    string
		x, y int32
	}{{idPuzzlePath, "Percorso sulla mappa", 40, 85}, {idPuzzleSequenceInput, "Input sequenza", 260, 85}, {idPuzzleCondition, "Porta / verifica", 480, 85}, {idPuzzleTimer, "Timer puzzle", 40, 190}, {idPuzzleCounter, "Contatore", 260, 190}, {idPuzzleBack, "← Indietro", 480, 300}}
	for _, b := range bs {
		createWindow("BUTTON", b.t, WS_CHILD|WS_VISIBLE|WS_TABSTOP, b.x, b.y, 190, 62, puzzlePaletteWindow, uintptr(b.id), hi)
	}
	createWindow("STATIC", "Il percorso si disegna direttamente sulla mappa: clic sinistro aggiunge, destro annulla l'ultima casella, Invio conferma, Esc annulla.", WS_CHILD|WS_VISIBLE, 40, 275, 400, 72, puzzlePaletteWindow, 10221, hi)
	pEnableWindow.Call(uintptr(owner), 0)
	modalLoop(&puzzlePaletteOpen)
	pEnableWindow.Call(uintptr(owner), 1)
	pSetFocus.Call(uintptr(owner))
	return puzzlePaletteChoice, puzzlePaletteAccepted
}

func capturePuzzlePathOnMap() ([]plmMapPoint, bool) {
	if hwndCanvas == 0 || currentMapDoc == nil || eventEditorWindow == 0 {
		return nil, false
	}
	puzzlePathCapturePoints = nil
	puzzlePathCaptureCancelled = false
	puzzlePathCaptureOldMode = mode
	puzzlePathCaptureActive = true
	pShowWindow.Call(uintptr(eventEditorWindow), SW_HIDE)
	pEnableWindow.Call(uintptr(hwndMain), 1)
	mode = "events"
	setToolbarStatus("Puzzle percorso: clic sinistro aggiunge caselle, destro rimuove, Invio conferma, Esc annulla.")
	pSetFocus.Call(uintptr(hwndCanvas))
	invalidate(hwndCanvas)
	modalLoop(&puzzlePathCaptureActive)
	mode = puzzlePathCaptureOldMode
	pEnableWindow.Call(uintptr(hwndMain), 0)
	pShowWindow.Call(uintptr(eventEditorWindow), SW_SHOW)
	pSetFocus.Call(uintptr(eventEditorWindow))
	invalidate(hwndCanvas)
	if puzzlePathCaptureCancelled || len(puzzlePathCapturePoints) < 2 {
		return nil, false
	}
	out := append([]plmMapPoint(nil), puzzlePathCapturePoints...)
	return out, true
}

func puzzlePathCanvasClick(x, y int, right bool) bool {
	if !puzzlePathCaptureActive {
		return false
	}
	if right {
		if len(puzzlePathCapturePoints) > 0 {
			puzzlePathCapturePoints = puzzlePathCapturePoints[:len(puzzlePathCapturePoints)-1]
		}
		invalidate(hwndCanvas)
		return true
	}
	if len(puzzlePathCapturePoints) > 0 {
		last := puzzlePathCapturePoints[len(puzzlePathCapturePoints)-1]
		if last.X == x && last.Y == y {
			return true
		}
		dx := last.X - x
		if dx < 0 {
			dx = -dx
		}
		dy := last.Y - y
		if dy < 0 {
			dy = -dy
		}
		if dx+dy != 1 {
			setToolbarStatus("Percorso puzzle: scegli una casella adiacente alla precedente.")
			return true
		}
	}
	puzzlePathCapturePoints = append(puzzlePathCapturePoints, plmMapPoint{X: x, Y: y})
	invalidate(hwndCanvas)
	return true
}
func puzzlePathCanvasKey(vk uintptr) bool {
	if !puzzlePathCaptureActive {
		return false
	}
	switch vk {
	case 13: // Enter
		if len(puzzlePathCapturePoints) >= 2 {
			puzzlePathCaptureActive = false
		} else {
			setToolbarStatus("Percorso puzzle: servono almeno 2 caselle.")
		}
		return true
	case 27: // Escape
		puzzlePathCaptureCancelled = true
		puzzlePathCaptureActive = false
		return true
	}
	return false
}
func drawPuzzlePathCaptureOverlay(hdc syscall.Handle) {
	if !puzzlePathCaptureActive || hdc == 0 {
		return
	}
	tile := mapDisplayTileSize()
	if tile <= 0 {
		return
	}
	for i, p := range puzzlePathCapturePoints {
		dx := int32(p.X*tile - mapScrollX)
		dy := gridOriginY + int32(p.Y*tile-mapScrollY)
		r := RECT{Left: dx + 2, Top: dy + 2, Right: dx + int32(tile) - 2, Bottom: dy + int32(tile) - 2}
		br := createSolidBrush(rgb(255, 235, 120))
		if br != 0 {
			fillWithBrush(hdc, r, br)
			deleteGDIObject(br)
		}
		text(hdc, dx+5, dy+4, fmt.Sprintf("%d", i+1), rgb(20, 20, 20))
	}
}

func addPuzzlePathCommand() {
	v, ok := showSimpleCommandForm(eventEditorWindow, "Puzzle percorso", "Assegna un ID stabile; dopo Conferma disegnerai il percorso direttamente sulla mappa.", []simpleFormField{{Key: "id", Label: "Puzzle ID", Kind: simpleFormText}, {Key: "reset", Label: "Reset se il giocatore sbaglia casella", Kind: simpleFormCheck, Initial: "true"}})
	if !ok {
		return
	}
	id := storyID(v["id"])
	if id == "" {
		return
	}
	path, ok := capturePuzzlePathOnMap()
	if !ok {
		return
	}
	c := plmManagedEventCommand{Type: "puzzle_path", PuzzleID: id, Path: path, Action: "track"}
	if formBool(v, "reset") {
		c.State = "reset_on_wrong"
	}
	if eventEditorPageIndex >= 0 && eventEditorPageIndex < len(eventEditorPages) {
		eventEditorPages[eventEditorPageIndex].Trigger = 4
	}
	if prependManagedEventCommand(c) {
		setToolbarStatus(fmt.Sprintf("Puzzle percorso %s: %d caselle.", id, len(path)))
	}
}
func addPuzzleSequenceInputCommand() {
	v, ok := showSimpleCommandForm(eventEditorWindow, "Puzzle sequenza", "Metti questo comando sui pulsanti/le leve del puzzle. Tutti usano lo stesso Puzzle ID e la stessa sequenza attesa; cambia solo Token input.", []simpleFormField{{Key: "id", Label: "Puzzle ID", Kind: simpleFormText}, {Key: "sequence", Label: "Sequenza attesa (es. A,B,C,A)", Kind: simpleFormText}, {Key: "token", Label: "Token di questo pulsante", Kind: simpleFormText}})
	if !ok {
		return
	}
	id := storyID(v["id"])
	tok := strings.ToUpper(strings.TrimSpace(v["token"]))
	if id == "" || tok == "" {
		return
	}
	seq := []string{}
	for _, x := range strings.Split(v["sequence"], ",") {
		x = strings.ToUpper(strings.TrimSpace(x))
		if x != "" {
			seq = append(seq, x)
		}
	}
	if len(seq) == 0 {
		return
	}
	if appendManagedEventCommand(plmManagedEventCommand{Type: "puzzle_sequence_input", PuzzleID: id, Sequence: seq, Token: tok}) {
		setToolbarStatus("Input puzzle aggiunto: " + tok)
	}
}
func addPuzzleConditionCommand() {
	v, ok := showSimpleCommandForm(eventEditorWindow, "Porta / verifica puzzle", "Usa lo stesso Puzzle ID su una o più porte. Finché non è completato, i comandi successivi dell'evento non partono.", []simpleFormField{{Key: "id", Label: "Puzzle ID", Kind: simpleFormText}, {Key: "fail", Label: "Messaggio se non completato", Kind: simpleFormMultiline, Initial: "Il meccanismo non è ancora sbloccato."}})
	if !ok {
		return
	}
	id := storyID(v["id"])
	if id == "" {
		return
	}
	if appendManagedEventCommand(plmManagedEventCommand{Type: "puzzle_condition", PuzzleID: id, FailText: strings.TrimSpace(v["fail"])}) {
		setToolbarStatus("Verifica puzzle aggiunta: " + id)
	}
}
func addPuzzleTimerCommand() {
	v, ok := showSimpleCommandForm(eventEditorWindow, "Timer puzzle", "START avvia il timer; CHECK blocca i comandi successivi se il tempo è scaduto; RESET azzera il timer.", []simpleFormField{{Key: "id", Label: "Puzzle ID", Kind: simpleFormText}, {Key: "action", Label: "Azione", Kind: simpleFormCombo, Options: []string{"START", "CHECK", "RESET"}}, {Key: "seconds", Label: "Secondi", Kind: simpleFormNumber, Initial: "30"}, {Key: "fail", Label: "Messaggio tempo scaduto", Kind: simpleFormMultiline, Initial: "Tempo scaduto."}})
	if !ok {
		return
	}
	id := storyID(v["id"])
	if id == "" {
		return
	}
	c := plmManagedEventCommand{Type: "puzzle_timer", PuzzleID: id, Action: strings.ToLower(v["action"]), Duration: maxIntEventCmd(formInt(v, "seconds", 30), 1), FailText: strings.TrimSpace(v["fail"])}
	if appendManagedEventCommand(c) {
		setToolbarStatus("Timer puzzle aggiunto: " + id)
	}
}
func addPuzzleCounterCommand() {
	v, ok := showSimpleCommandForm(eventEditorWindow, "Contatore puzzle", "ADD incrementa; CHECK richiede la soglia; RESET azzera. Utile per più leve/porte collegate.", []simpleFormField{{Key: "id", Label: "Puzzle ID", Kind: simpleFormText}, {Key: "action", Label: "Azione", Kind: simpleFormCombo, Options: []string{"ADD", "CHECK", "RESET"}}, {Key: "threshold", Label: "Soglia", Kind: simpleFormNumber, Initial: "3"}, {Key: "fail", Label: "Messaggio se soglia non raggiunta", Kind: simpleFormMultiline, Initial: "Manca ancora qualcosa."}})
	if !ok {
		return
	}
	id := storyID(v["id"])
	if id == "" {
		return
	}
	c := plmManagedEventCommand{Type: "puzzle_counter", PuzzleID: id, Action: strings.ToLower(v["action"]), Threshold: maxIntEventCmd(formInt(v, "threshold", 3), 1), FailText: strings.TrimSpace(v["fail"])}
	if appendManagedEventCommand(c) {
		setToolbarStatus("Contatore puzzle aggiunto: " + id)
	}
}
func addPuzzleEventCommand() {
	for {
		ch, ok := showPuzzlePalette(eventEditorWindow)
		if !ok {
			return
		}
		switch ch {
		case idPuzzlePath:
			addPuzzlePathCommand()
		case idPuzzleSequenceInput:
			addPuzzleSequenceInputCommand()
		case idPuzzleCondition:
			addPuzzleConditionCommand()
		case idPuzzleTimer:
			addPuzzleTimerCommand()
		case idPuzzleCounter:
			addPuzzleCounterCommand()
		}
	}
}
