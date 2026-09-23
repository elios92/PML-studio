//go:build windows

package main

import (
	"fmt"
	"syscall"
	"unsafe"
)

const (
	movementDialogClass = "PLMStudioMovementRoute01"
	idMoveList          = 8500
	idMoveUp            = 8501
	idMoveDown          = 8502
	idMoveLeft          = 8503
	idMoveRight         = 8504
	idTurnUp            = 8505
	idTurnDown          = 8506
	idTurnLeft          = 8507
	idTurnRight         = 8508
	idTowardPlayer      = 8509
	idAwayPlayer        = 8510
	idMoveSpecial       = 8511
	idMoveA             = 8512
	idMoveB             = 8513
	idMoveAddSpecial    = 8514
	idMoveDelete        = 8515
	idMoveShiftUp       = 8516
	idMoveShiftDown     = 8517
	idMoveRepeat        = 8518
	idMoveTarget        = 8519
	idMoveOK            = 8520
	idMoveCancel        = 8521
)

var movementDialogRegistered bool
var movementDialogOpen bool
var movementDialogWindow syscall.Handle
var movementDialogList syscall.Handle
var movementDialogSpecial syscall.Handle
var movementDialogA syscall.Handle
var movementDialogB syscall.Handle
var movementDialogRepeat syscall.Handle
var movementDialogTarget syscall.Handle
var movementDialogNPC bool
var movementDialogSteps []plmMovementStep
var movementDialogAccepted bool
var movementDialogResult plmManagedEventCommand

func movementStepLabel(s plmMovementStep) string {
	switch s.Op {
	case "down":
		return "Muovi giù"
	case "left":
		return "Muovi sinistra"
	case "right":
		return "Muovi destra"
	case "up":
		return "Muovi su"
	case "turn_down":
		return "Gira giù"
	case "turn_left":
		return "Gira sinistra"
	case "turn_right":
		return "Gira destra"
	case "turn_up":
		return "Gira su"
	case "toward_player":
		return "Muovi verso il giocatore"
	case "away_player":
		return "Muovi lontano dal giocatore"
	case "wait":
		return fmt.Sprintf("Attendi %d frame", maxIntEventCmd(s.A, 1))
	case "jump":
		return fmt.Sprintf("Salta ΔX=%d ΔY=%d", s.A, s.B)
	case "speed":
		return fmt.Sprintf("Velocità = %d", maxIntEventCmd(s.A, 1))
	case "frequency":
		return fmt.Sprintf("Frequenza = %d", maxIntEventCmd(s.A, 1))
	case "through_on":
		return "Attraversabile = ON"
	case "through_off":
		return "Attraversabile = OFF"
	case "opacity":
		return fmt.Sprintf("Opacità = %d", s.A)
	}
	return s.Op
}

func refreshMovementDialogList(selectIndex int) {
	clearList(movementDialogList)
	for i, st := range movementDialogSteps {
		addList(movementDialogList, fmt.Sprintf("%02d. %s", i+1, movementStepLabel(st)))
	}
	if len(movementDialogSteps) == 0 {
		return
	}
	if selectIndex < 0 {
		selectIndex = len(movementDialogSteps) - 1
	}
	if selectIndex >= len(movementDialogSteps) {
		selectIndex = len(movementDialogSteps) - 1
	}
	pSendMessageW.Call(uintptr(movementDialogList), LB_SETCURSEL, uintptr(selectIndex), 0)
}

func selectedMovementIndex() int {
	if movementDialogList == 0 {
		return -1
	}
	r, _, _ := pSendMessageW.Call(uintptr(movementDialogList), LB_GETCURSEL, 0, 0)
	if int32(r) < 0 {
		return -1
	}
	return int(r)
}

func appendMovementStep(op string, a, b int) {
	movementDialogSteps = append(movementDialogSteps, plmMovementStep{Op: op, A: a, B: b})
	refreshMovementDialogList(len(movementDialogSteps) - 1)
}

func movementSpecialStep() (plmMovementStep, bool) {
	idx := comboSel(movementDialogSpecial)
	a := intField(movementDialogA, 0)
	b := intField(movementDialogB, 0)
	switch idx {
	case 0:
		if a <= 0 {
			a = 15
		}
		return plmMovementStep{Op: "wait", A: a}, true
	case 1:
		return plmMovementStep{Op: "jump", A: a, B: b}, true
	case 2:
		if a < 1 {
			a = 1
		}
		if a > 6 {
			a = 6
		}
		return plmMovementStep{Op: "speed", A: a}, true
	case 3:
		if a < 1 {
			a = 1
		}
		if a > 6 {
			a = 6
		}
		return plmMovementStep{Op: "frequency", A: a}, true
	case 4:
		return plmMovementStep{Op: "through_on"}, true
	case 5:
		return plmMovementStep{Op: "through_off"}, true
	case 6:
		if a < 0 {
			a = 0
		}
		if a > 255 {
			a = 255
		}
		return plmMovementStep{Op: "opacity", A: a}, true
	}
	return plmMovementStep{}, false
}

func movementDialogWndProcFn(hwnd syscall.Handle, msg uint32, w, l uintptr) uintptr {
	switch msg {
	case WM_COMMAND:
		switch int(loword(w)) {
		case idMoveUp:
			appendMovementStep("up", 0, 0)
		case idMoveDown:
			appendMovementStep("down", 0, 0)
		case idMoveLeft:
			appendMovementStep("left", 0, 0)
		case idMoveRight:
			appendMovementStep("right", 0, 0)
		case idTurnUp:
			appendMovementStep("turn_up", 0, 0)
		case idTurnDown:
			appendMovementStep("turn_down", 0, 0)
		case idTurnLeft:
			appendMovementStep("turn_left", 0, 0)
		case idTurnRight:
			appendMovementStep("turn_right", 0, 0)
		case idTowardPlayer:
			appendMovementStep("toward_player", 0, 0)
		case idAwayPlayer:
			appendMovementStep("away_player", 0, 0)
		case idMoveAddSpecial:
			if st, ok := movementSpecialStep(); ok {
				movementDialogSteps = append(movementDialogSteps, st)
				refreshMovementDialogList(len(movementDialogSteps) - 1)
			}
		case idMoveDelete:
			idx := selectedMovementIndex()
			if idx >= 0 && idx < len(movementDialogSteps) {
				movementDialogSteps = append(movementDialogSteps[:idx], movementDialogSteps[idx+1:]...)
				refreshMovementDialogList(idx)
			}
		case idMoveShiftUp:
			idx := selectedMovementIndex()
			if idx > 0 && idx < len(movementDialogSteps) {
				movementDialogSteps[idx-1], movementDialogSteps[idx] = movementDialogSteps[idx], movementDialogSteps[idx-1]
				refreshMovementDialogList(idx - 1)
			}
		case idMoveShiftDown:
			idx := selectedMovementIndex()
			if idx >= 0 && idx+1 < len(movementDialogSteps) {
				movementDialogSteps[idx+1], movementDialogSteps[idx] = movementDialogSteps[idx], movementDialogSteps[idx+1]
				refreshMovementDialogList(idx + 1)
			}
		case idMoveOK:
			if len(movementDialogSteps) == 0 {
				msgbox("PML Studio - Movimento", "Inserisci almeno un comando di movimento.", MB_OK|MB_ICONINFORMATION)
				return 0
			}
			target := "player"
			targetID := -1
			if movementDialogNPC {
				target = "event"
				targetID = intField(movementDialogTarget, 0) // 0 = evento corrente
				if targetID < 0 {
					targetID = 0
				}
			}
			reps := intField(movementDialogRepeat, 1)
			if reps < 1 {
				reps = 1
			}
			if reps > 999 {
				reps = 999
			}
			movementDialogResult = plmManagedEventCommand{
				Type: "movement_route", Target: target, TargetEventID: targetID,
				Moves: append([]plmMovementStep(nil), movementDialogSteps...), RepeatCount: reps,
				Wait: true,
			}
			movementDialogAccepted = true
			pDestroyWindow.Call(uintptr(hwnd))
			return 0
		case idMoveCancel:
			pDestroyWindow.Call(uintptr(hwnd))
			return 0
		}
	case WM_CLOSE:
		pDestroyWindow.Call(uintptr(hwnd))
		return 0
	case WM_DESTROY:
		movementDialogOpen = false
		movementDialogWindow = 0
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(uintptr(hwnd), uintptr(msg), w, l)
	return r
}

var movementDialogWndProc = syscall.NewCallback(movementDialogWndProcFn)

func ensureMovementDialogClass() error {
	if movementDialogRegistered {
		return nil
	}
	h, _, _ := pGetModuleHandleW.Call(0)
	hi := syscall.Handle(h)
	cur, _, _ := pLoadCursorW.Call(0, IDC_ARROW)
	brush, _, _ := pCreateSolidBrush.Call(rgb(244, 244, 244))
	wc := WNDCLASSEX{cbSize: uint32(unsafe.Sizeof(WNDCLASSEX{})), lpfnWndProc: movementDialogWndProc, hInstance: hi, hCursor: syscall.Handle(cur), hbrBackground: syscall.Handle(brush), lpszClassName: wstr(movementDialogClass)}
	r, _, err := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	if r == 0 {
		return fmt.Errorf("registrazione editor movimento: %v", err)
	}
	movementDialogRegistered = true
	return nil
}

func showMovementRouteDialog(owner syscall.Handle, npc bool) (plmManagedEventCommand, bool) {
	if movementDialogOpen {
		return plmManagedEventCommand{}, false
	}
	if err := ensureMovementDialogClass(); err != nil {
		msgbox("PML Studio - Movimento", err.Error(), MB_OK|MB_ICONERROR)
		return plmManagedEventCommand{}, false
	}
	const ww, wh int32 = 980, 720
	x, y := centerOwnedWindow(ww, wh)
	h, _, _ := pGetModuleHandleW.Call(0)
	hi := syscall.Handle(h)
	movementDialogOpen = true
	movementDialogAccepted = false
	movementDialogNPC = npc
	movementDialogSteps = nil
	movementDialogResult = plmManagedEventCommand{}
	title := "Movimento giocatore"
	if npc {
		title = "Movimento NPC / Evento"
	}
	movementDialogWindow = createWindow(movementDialogClass, title, WS_OVERLAPPEDWINDOW|WS_VISIBLE|WS_CLIPCHILDREN, x, y, ww, wh, owner, 0, hi)
	if movementDialogWindow == 0 {
		movementDialogOpen = false
		return plmManagedEventCommand{}, false
	}
	setWindowIcon(movementDialogWindow)

	createWindow("STATIC", "Sequenza: viene eseguita dall'alto verso il basso. PLM attende SEMPRE la fine completa prima del comando evento successivo.", WS_CHILD|WS_VISIBLE, 24, 18, 900, 34, movementDialogWindow, 8530, hi)
	movementDialogList = createWindow("LISTBOX", "", WS_CHILD|WS_VISIBLE|WS_BORDER|WS_VSCROLL|LBS_NOTIFY, 24, 58, 520, 460, movementDialogWindow, idMoveList, hi)

	bx := int32(570)
	by := int32(58)
	bw := int32(82)
	bh := int32(36)
	gap := int32(8)
	createWindow("BUTTON", "↑", WS_CHILD|WS_VISIBLE|WS_TABSTOP, bx+86, by, bw, bh, movementDialogWindow, idMoveUp, hi)
	createWindow("BUTTON", "←", WS_CHILD|WS_VISIBLE|WS_TABSTOP, bx, by+44, bw, bh, movementDialogWindow, idMoveLeft, hi)
	createWindow("BUTTON", "↓", WS_CHILD|WS_VISIBLE|WS_TABSTOP, bx+86, by+44, bw, bh, movementDialogWindow, idMoveDown, hi)
	createWindow("BUTTON", "→", WS_CHILD|WS_VISIBLE|WS_TABSTOP, bx+172, by+44, bw, bh, movementDialogWindow, idMoveRight, hi)
	createWindow("STATIC", "Gira:", WS_CHILD|WS_VISIBLE, bx, by+98, 60, 24, movementDialogWindow, 8531, hi)
	createWindow("BUTTON", "↑", WS_CHILD|WS_VISIBLE|WS_TABSTOP, bx+62, by+92, 58, bh, movementDialogWindow, idTurnUp, hi)
	createWindow("BUTTON", "↓", WS_CHILD|WS_VISIBLE|WS_TABSTOP, bx+124, by+92, 58, bh, movementDialogWindow, idTurnDown, hi)
	createWindow("BUTTON", "←", WS_CHILD|WS_VISIBLE|WS_TABSTOP, bx+186, by+92, 58, bh, movementDialogWindow, idTurnLeft, hi)
	createWindow("BUTTON", "→", WS_CHILD|WS_VISIBLE|WS_TABSTOP, bx+248, by+92, 58, bh, movementDialogWindow, idTurnRight, hi)
	createWindow("BUTTON", "Verso giocatore", WS_CHILD|WS_VISIBLE|WS_TABSTOP, bx, by+138, 146, bh, movementDialogWindow, idTowardPlayer, hi)
	createWindow("BUTTON", "Lontano giocatore", WS_CHILD|WS_VISIBLE|WS_TABSTOP, bx+154, by+138, 152, bh, movementDialogWindow, idAwayPlayer, hi)

	createWindow("STATIC", "Comando speciale:", WS_CHILD|WS_VISIBLE, bx, by+194, 130, 24, movementDialogWindow, 8532, hi)
	movementDialogSpecial = createWindow("COMBOBOX", "", WS_CHILD|WS_VISIBLE|WS_TABSTOP|CBS_DROPDOWNLIST, bx, by+220, 306, 220, movementDialogWindow, idMoveSpecial, hi)
	setComboFromStrings(movementDialogSpecial, []string{"Attendi (frame)", "Salta (ΔX, ΔY)", "Velocità 1..6", "Frequenza 1..6", "Attraversabile ON", "Attraversabile OFF", "Opacità 0..255"}, 0)
	createWindow("STATIC", "A:", WS_CHILD|WS_VISIBLE, bx, by+262, 24, 24, movementDialogWindow, 8533, hi)
	movementDialogA = createWindow("EDIT", "15", WS_CHILD|WS_VISIBLE|WS_BORDER|WS_TABSTOP, bx+28, by+256, 90, 30, movementDialogWindow, idMoveA, hi)
	createWindow("STATIC", "B:", WS_CHILD|WS_VISIBLE, bx+132, by+262, 24, 24, movementDialogWindow, 8534, hi)
	movementDialogB = createWindow("EDIT", "0", WS_CHILD|WS_VISIBLE|WS_BORDER|WS_TABSTOP, bx+160, by+256, 90, 30, movementDialogWindow, idMoveB, hi)
	createWindow("BUTTON", "Aggiungi", WS_CHILD|WS_VISIBLE|WS_TABSTOP, bx, by+298, 120, bh, movementDialogWindow, idMoveAddSpecial, hi)

	createWindow("BUTTON", "Elimina", WS_CHILD|WS_VISIBLE|WS_TABSTOP, bx, by+354, 92, bh, movementDialogWindow, idMoveDelete, hi)
	createWindow("BUTTON", "Sposta ↑", WS_CHILD|WS_VISIBLE|WS_TABSTOP, bx+100, by+354, 98, bh, movementDialogWindow, idMoveShiftUp, hi)
	createWindow("BUTTON", "Sposta ↓", WS_CHILD|WS_VISIBLE|WS_TABSTOP, bx+206, by+354, 98, bh, movementDialogWindow, idMoveShiftDown, hi)

	createWindow("STATIC", "Ripetizioni finite:", WS_CHILD|WS_VISIBLE, 24, 542, 150, 24, movementDialogWindow, 8535, hi)
	movementDialogRepeat = createWindow("EDIT", "1", WS_CHILD|WS_VISIBLE|WS_BORDER|WS_TABSTOP, 178, 536, 70, 30, movementDialogWindow, idMoveRepeat, hi)
	if npc {
		createWindow("STATIC", "ID NPC/Evento (0 = evento corrente):", WS_CHILD|WS_VISIBLE, 280, 542, 250, 24, movementDialogWindow, 8536, hi)
		movementDialogTarget = createWindow("EDIT", "0", WS_CHILD|WS_VISIBLE|WS_BORDER|WS_TABSTOP, 532, 536, 76, 30, movementDialogWindow, idMoveTarget, hi)
	} else {
		movementDialogTarget = 0
		createWindow("STATIC", "Target: giocatore", WS_CHILD|WS_VISIBLE, 280, 542, 250, 24, movementDialogWindow, 8537, hi)
	}
	createWindow("STATIC", "Sicurezza: percorso non skippabile; ogni step viene letto in ordine. Il runtime inserisce una barriera di completamento prima di proseguire.", WS_CHILD|WS_VISIBLE, 24, 580, 900, 42, movementDialogWindow, 8538, hi)
	createWindow("BUTTON", "Salva movimento", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 692, 628, 135, 40, movementDialogWindow, idMoveOK, hi)
	createWindow("BUTTON", "Annulla", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 838, 628, 100, 40, movementDialogWindow, idMoveCancel, hi)
	_ = gap

	pEnableWindow.Call(uintptr(owner), 0)
	modalLoop(&movementDialogOpen)
	pEnableWindow.Call(uintptr(owner), 1)
	pSetFocus.Call(uintptr(owner))
	return movementDialogResult, movementDialogAccepted
}

func addMovementRouteEventCommand(npc bool) {
	c, ok := showMovementRouteDialog(eventEditorWindow, npc)
	if !ok {
		return
	}
	if appendManagedEventCommand(c) {
		target := "giocatore"
		if npc {
			target = "NPC/evento"
		}
		setToolbarStatus("Comando evento aggiunto: movimento " + target + " completo e bloccante.")
	}
}
