//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

// Extended high-level event commands. Native RPG Maker/Essentials commands are
// emitted when they have a stable equivalent. PLM-only behavior is kept in the
// PML_CMD marker so the Python runtime can execute it without fake Ruby glue.

func compactEventCommandText(s string, max int) string {
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.Join(strings.Fields(s), " ")
	if max > 3 && len([]rune(s)) > max {
		r := []rune(s)
		return string(r[:max-3]) + "..."
	}
	return s
}

func variableOperationCode(op string) int {
	switch strings.ToLower(strings.TrimSpace(op)) {
	case "add", "+", "+=":
		return 1
	case "sub", "-", "-=":
		return 2
	case "mul", "*", "*=":
		return 3
	case "div", "/", "/=":
		return 4
	case "mod", "%", "%=":
		return 5
	default:
		return 0
	}
}

func variableOperationSymbol(op string) string {
	switch variableOperationCode(op) {
	case 1:
		return "+="
	case 2:
		return "-="
	case 3:
		return "*="
	case 4:
		return "/="
	case 5:
		return "%="
	default:
		return "="
	}
}

func addSelfSwitchEventCommand() {
	addClassicSelfSwitchEventCommand()
}

func addVariableEventCommand() {
	id, name, op, value, ok := showVariableCommandDialog(eventEditorWindow)
	if !ok {
		return
	}
	if appendManagedEventCommand(plmManagedEventCommand{Type: "variable", VariableID: id, VariableName: name, Operation: op, Value: value}) {
		setToolbarStatus(fmt.Sprintf("Comando evento aggiunto: variabile %04d %s %d.", id, variableOperationSymbol(op), value))
	}
}

func addChoicesEventCommand() {
	text, ok := showExtendedMultilineDialog(eventEditorWindow, "Crea scelte", "Una scelta per riga:", "Inserisci da 2 a 8 opzioni. PLM crea la struttura Scelte nativa e la mantiene riconoscibile nell'editor.", "Sì\r\nNo")
	if !ok {
		return
	}
	choices := []string{}
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r", ""), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			choices = append(choices, line)
		}
	}
	if len(choices) < 2 {
		msgbox("PML Studio - Scelte", "Inserisci almeno 2 scelte, una per riga.", MB_OK|MB_ICONINFORMATION)
		return
	}
	if len(choices) > 8 {
		choices = choices[:8]
	}
	if appendManagedEventCommand(plmManagedEventCommand{Type: "choices", Choices: choices}) {
		setToolbarStatus("Comando evento aggiunto: Mostra scelte.")
	}
}

func addChangeOverworldEventCommand() {
	graphic, ok := showCharacterPicker(eventEditorWindow, "")
	if !ok || strings.TrimSpace(graphic) == "" {
		return
	}
	if appendManagedEventCommand(plmManagedEventCommand{Type: "change_overworld", Graphic: strings.TrimSpace(graphic)}) {
		setToolbarStatus("Comando PLM aggiunto: cambia overworld giocatore → " + strings.TrimSpace(graphic) + ".")
	}
}

func addCommonEventCommand() {
	id, name, ok := showCommonEventCommandDialog(eventEditorWindow)
	if !ok || id <= 0 {
		return
	}
	if appendManagedEventCommand(plmManagedEventCommand{Type: "common_event", CommonEventID: id, CommonEventName: name}) {
		setToolbarStatus(fmt.Sprintf("Comando evento aggiunto: evento comune #%d.", id))
	}
}

func addAutorunEventCommand() {
	if eventEditorPageIndex < 0 || eventEditorPageIndex >= len(eventEditorPages) {
		return
	}
	eventEditorPages[eventEditorPageIndex].Trigger = 3
	// Marker kept only once per page for a readable command list.
	if !currentPageHasManagedType("autorun") {
		appendManagedEventCommand(plmManagedEventCommand{Type: "autorun"})
	}
	loadEventEditorPageToControls()
	setToolbarStatus("Pagina evento impostata come Evento automatico (Autorun).")
}

func addScriptFromTextEventCommand() {
	text, ok := showExtendedMultilineDialog(eventEditorWindow, "Script da testo", "Descrivi cosa deve fare l'evento:", "Questo testo viene salvato come comando PLM strutturato. Il compilatore PLM potrà tradurlo nel runtime Python senza inserire Ruby non verificato.", "")
	if !ok || strings.TrimSpace(text) == "" {
		return
	}
	if appendManagedEventCommand(plmManagedEventCommand{Type: "script_text", Text: strings.TrimSpace(text)}) {
		setToolbarStatus("Comando PLM aggiunto: Script da testo.")
	}
}

func addAdvancedPythonEventCommand() {
	code, ok := showExtendedMultilineDialog(eventEditorWindow, "Script avanzato Python", "Codice Python:", "Codice destinato al runtime Python di PLM. Viene conservato integralmente nel comando PML_CMD e non viene convertito in Ruby.", "")
	if !ok || strings.TrimSpace(code) == "" {
		return
	}
	if appendManagedEventCommand(plmManagedEventCommand{Type: "python_script", Script: code}) {
		setToolbarStatus("Comando PLM aggiunto: Script avanzato Python.")
	}
}

func addTemporaryEventCommand() {
	if appendManagedEventCommand(plmManagedEventCommand{Type: "temporary_event"}) {
		setToolbarStatus("Comando evento aggiunto: cancella temporaneamente l'evento fino al ricaricamento mappa.")
	}
}

func addGiveItemEventCommand() {
	id, qty, _, ok := showEventCommandPickerDialog(eventEditorWindow, eventCommandGiveItem)
	if !ok {
		return
	}
	id = strings.ToUpper(strings.TrimSpace(id))
	if id == "" {
		return
	}
	if appendManagedEventCommand(plmManagedEventCommand{Type: "give_item", Item: id, Quantity: maxIntEventCmd(qty, 1)}) {
		setToolbarStatus("Comando evento aggiunto: Dai oggetto.")
	}
}

func addPlayerFollowsNPCCommand() {
	target, distance, ok := showFollowCommandDialog(eventEditorWindow, true)
	if !ok || target <= 0 {
		return
	}
	if appendManagedEventCommand(plmManagedEventCommand{Type: "player_follow_npc", TargetEventID: target, Distance: distance}) {
		setToolbarStatus(fmt.Sprintf("Comando PLM aggiunto: il giocatore segue l'evento #%d.", target))
	}
}

func addNPCFollowsPlayerCommand() {
	_, distance, ok := showFollowCommandDialog(eventEditorWindow, false)
	if !ok {
		return
	}
	if appendManagedEventCommand(plmManagedEventCommand{Type: "npc_follow_player", Distance: distance}) {
		setToolbarStatus("Comando PLM aggiunto: l'NPC/evento corrente segue il giocatore.")
	}
}

func currentPageHasManagedType(kind string) bool {
	if eventEditorPageIndex < 0 || eventEditorPageIndex >= len(eventEditorPages) {
		return false
	}
	p := eventEditorPages[eventEditorPageIndex]
	if p.Raw == nil {
		return false
	}
	lv, ok := anyMapValueCI(p.Raw, "list")
	if !ok {
		return false
	}
	arr, ok := lv.([]any)
	if !ok {
		return false
	}
	for _, node := range arr {
		m, ok := node.(map[string]any)
		if !ok || commandMapCode(m) != 108 {
			continue
		}
		if c, ok := parsePLMEventCommandMarker(commandFirstParam(m)); ok && strings.EqualFold(c.Type, kind) {
			return true
		}
	}
	return false
}

// -----------------------------------------------------------------------------
// Generic multiline command editor
// -----------------------------------------------------------------------------

func showExtendedMultilineDialog(owner syscall.Handle, title, label, info, initial string) (string, bool) {
	eventCommandDialogModeValue = eventCommandText
	ok := runEventCommandDialog(owner, title, 800, 560, func(hInst syscall.Handle) {
		createWindow("STATIC", label, WS_CHILD|WS_VISIBLE, 20, 18, 735, 26, eventCommandDialogWindow, 7690, hInst)
		eventCommandDialogText = createWindow("EDIT", initial, WS_CHILD|WS_VISIBLE|WS_BORDER|WS_VSCROLL|WS_TABSTOP|ES_MULTILINE|ES_AUTOVSCROLL|ES_WANTRETURN, 20, 48, 745, 380, eventCommandDialogWindow, idEventCmdText, hInst)
		createWindow("STATIC", info, WS_CHILD|WS_VISIBLE, 20, 438, 740, 45, eventCommandDialogWindow, 7691, hInst)
		createWindow("BUTTON", "Conferma", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 545, 490, 105, 36, eventCommandDialogWindow, idEventCmdOK, hInst)
		createWindow("BUTTON", "Annulla", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 660, 490, 105, 36, eventCommandDialogWindow, idEventCmdCancel, hInst)
	})
	return eventCommandDialogResultTxt, ok
}

// -----------------------------------------------------------------------------
// Self Switch command dialog
// -----------------------------------------------------------------------------

const (
	selfSwitchCmdClass = "PLMStudioSelfSwitchCommand01"
	idSelfSwitchName   = 7700
	idSelfSwitchState  = 7701
	idSelfSwitchOK     = 7702
	idSelfSwitchCancel = 7703
)

var selfSwitchCmdRegistered, selfSwitchCmdOpen, selfSwitchCmdAccepted bool
var selfSwitchCmdWindow, selfSwitchCmdName, selfSwitchCmdState syscall.Handle
var selfSwitchCmdResultName, selfSwitchCmdResultState string

func selfSwitchCommandWndProcFn(hwnd syscall.Handle, msg uint32, w, l uintptr) uintptr {
	switch msg {
	case WM_COMMAND:
		switch int(loword(w)) {
		case idSelfSwitchOK:
			name := strings.TrimSpace(getText(selfSwitchCmdName))
			if name == "" {
				msgbox("PML Studio - Self Switch", "Inserisci il nome del Self Switch. Puoi usare A/B/C/D oppure un nome personalizzato PLM.", MB_OK|MB_ICONINFORMATION)
				return 0
			}
			state := "ON"
			if comboSel(selfSwitchCmdState) == 1 {
				state = "OFF"
			}
			selfSwitchCmdResultName = name
			selfSwitchCmdResultState = state
			selfSwitchCmdAccepted = true
			pDestroyWindow.Call(uintptr(hwnd))
			return 0
		case idSelfSwitchCancel:
			pDestroyWindow.Call(uintptr(hwnd))
			return 0
		}
	case WM_CLOSE:
		pDestroyWindow.Call(uintptr(hwnd))
		return 0
	case WM_DESTROY:
		selfSwitchCmdOpen = false
		selfSwitchCmdWindow = 0
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(uintptr(hwnd), uintptr(msg), w, l)
	return r
}

var selfSwitchCommandWndProc = syscall.NewCallback(selfSwitchCommandWndProcFn)

func ensureSelfSwitchCommandClass() error {
	if selfSwitchCmdRegistered {
		return nil
	}
	h, _, _ := pGetModuleHandleW.Call(0)
	hi := syscall.Handle(h)
	cur, _, _ := pLoadCursorW.Call(0, IDC_ARROW)
	brush, _, _ := pCreateSolidBrush.Call(rgb(244, 244, 244))
	wc := WNDCLASSEX{cbSize: uint32(unsafe.Sizeof(WNDCLASSEX{})), lpfnWndProc: selfSwitchCommandWndProc, hInstance: hi, hCursor: syscall.Handle(cur), hbrBackground: syscall.Handle(brush), lpszClassName: wstr(selfSwitchCmdClass)}
	r, _, err := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	if r == 0 {
		return fmt.Errorf("registrazione Self Switch: %v", err)
	}
	selfSwitchCmdRegistered = true
	return nil
}

func showSelfSwitchCommandDialog(owner syscall.Handle) (string, string, bool) {
	if selfSwitchCmdOpen || ensureSelfSwitchCommandClass() != nil {
		return "", "", false
	}
	const ww, wh int32 = 520, 300
	x, y := centerOwnedWindow(ww, wh)
	h, _, _ := pGetModuleHandleW.Call(0)
	hi := syscall.Handle(h)
	selfSwitchCmdAccepted = false
	selfSwitchCmdOpen = true
	selfSwitchCmdWindow = createWindow(selfSwitchCmdClass, "Modifica / crea Self Switch", WS_OVERLAPPEDWINDOW|WS_VISIBLE|WS_CLIPCHILDREN, x, y, ww, wh, owner, 0, hi)
	if selfSwitchCmdWindow == 0 {
		selfSwitchCmdOpen = false
		return "", "", false
	}
	setWindowIcon(selfSwitchCmdWindow)
	createWindow("STATIC", "Self Switch:", WS_CHILD|WS_VISIBLE, 25, 30, 150, 24, selfSwitchCmdWindow, 7710, hi)
	selfSwitchCmdName = createWindow("EDIT", "A", WS_CHILD|WS_VISIBLE|WS_BORDER|WS_TABSTOP, 175, 25, 285, 32, selfSwitchCmdWindow, idSelfSwitchName, hi)
	createWindow("STATIC", "Stato:", WS_CHILD|WS_VISIBLE, 25, 82, 150, 24, selfSwitchCmdWindow, 7711, hi)
	selfSwitchCmdState = createWindow("COMBOBOX", "", WS_CHILD|WS_VISIBLE|WS_TABSTOP|CBS_DROPDOWNLIST, 175, 77, 180, 130, selfSwitchCmdWindow, idSelfSwitchState, hi)
	comboAdd(selfSwitchCmdState, "ON")
	comboAdd(selfSwitchCmdState, "OFF")
	pSendMessageW.Call(uintptr(selfSwitchCmdState), CB_SETCURSEL, 0, 0)
	createWindow("STATIC", "A/B/C/D vengono salvati anche come comando nativo. I nomi personalizzati restano Self Switch PLM.", WS_CHILD|WS_VISIBLE, 25, 132, 435, 50, selfSwitchCmdWindow, 7712, hi)
	createWindow("BUTTON", "Conferma", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 245, 205, 100, 36, selfSwitchCmdWindow, idSelfSwitchOK, hi)
	createWindow("BUTTON", "Annulla", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 360, 205, 100, 36, selfSwitchCmdWindow, idSelfSwitchCancel, hi)
	pEnableWindow.Call(uintptr(owner), 0)
	modalLoop(&selfSwitchCmdOpen)
	pEnableWindow.Call(uintptr(owner), 1)
	pSetFocus.Call(uintptr(owner))
	return selfSwitchCmdResultName, selfSwitchCmdResultState, selfSwitchCmdAccepted
}

// -----------------------------------------------------------------------------
// Variable command dialog
// -----------------------------------------------------------------------------

const (
	variableCmdClass    = "PLMStudioVariableCommand01"
	idVariableCmdList   = 7720
	idVariableCmdName   = 7721
	idVariableCmdOp     = 7722
	idVariableCmdValue  = 7723
	idVariableCmdOK     = 7724
	idVariableCmdCancel = 7725
)

var variableCmdRegistered, variableCmdOpen, variableCmdAccepted bool
var variableCmdWindow, variableCmdList, variableCmdName, variableCmdOp, variableCmdValue syscall.Handle
var variableCmdItems []eventNamedValue
var variableCmdCurrentID, variableCmdResultID, variableCmdResultValue int
var variableCmdResultName, variableCmdResultOp string

func variableCommandSelectedIndex() int {
	r, _, _ := pSendMessageW.Call(uintptr(variableCmdList), LB_GETCURSEL, 0, 0)
	if int32(r) < 0 {
		return -1
	}
	return int(r)
}
func variableCommandSelect() {
	i := variableCommandSelectedIndex()
	if i >= 0 && i < len(variableCmdItems) {
		variableCmdCurrentID = variableCmdItems[i].ID
		setText(variableCmdName, variableCmdItems[i].Name)
	}
}
func variableCommandWndProcFn(hwnd syscall.Handle, msg uint32, w, l uintptr) uintptr {
	switch msg {
	case WM_COMMAND:
		id := int(loword(w))
		notify := int(hiword(w))
		if id == idVariableCmdList && (notify == LBN_SELCHANGE || notify == LBN_DBLCLK) {
			variableCommandSelect()
			if notify == LBN_SELCHANGE {
				return 0
			}
		}
		switch id {
		case idVariableCmdOK:
			name := strings.TrimSpace(getText(variableCmdName))
			if name == "" {
				msgbox("PML Studio - Variabile", "Inserisci o seleziona una variabile.", MB_OK|MB_ICONINFORMATION)
				return 0
			}
			idv := variableCmdCurrentID
			if ex, ok := eventNamedValueByName(variableCmdItems, name); ok {
				idv = ex.ID
			}
			if idv <= 0 {
				idv = nextEventNamedValueID(variableCmdItems)
			}
			opSel := comboSel(variableCmdOp)
			ops := []string{"set", "add", "sub", "mul", "div", "mod"}
			if opSel < 0 || opSel >= len(ops) {
				opSel = 0
			}
			val := intField(variableCmdValue, 0)
			if (opSel == 4 || opSel == 5) && val == 0 {
				msgbox("PML Studio - Variabile", "Divisione/modulo per zero non consentiti.", MB_OK|MB_ICONINFORMATION)
				return 0
			}
			sw, vars := loadEventConditionCatalog()
			vars = upsertEventNamedValue(vars, idv, name)
			if err := saveEventConditionCatalog(sw, vars); err != nil {
				msgbox("PML Studio - Variabile", err.Error(), MB_OK|MB_ICONERROR)
				return 0
			}
			variableCmdResultID = idv
			variableCmdResultName = name
			variableCmdResultOp = ops[opSel]
			variableCmdResultValue = val
			variableCmdAccepted = true
			pDestroyWindow.Call(uintptr(hwnd))
			return 0
		case idVariableCmdCancel:
			pDestroyWindow.Call(uintptr(hwnd))
			return 0
		}
	case WM_CLOSE:
		pDestroyWindow.Call(uintptr(hwnd))
		return 0
	case WM_DESTROY:
		variableCmdOpen = false
		variableCmdWindow = 0
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(uintptr(hwnd), uintptr(msg), w, l)
	return r
}

var variableCommandWndProc = syscall.NewCallback(variableCommandWndProcFn)

func ensureVariableCommandClass() error {
	if variableCmdRegistered {
		return nil
	}
	h, _, _ := pGetModuleHandleW.Call(0)
	hi := syscall.Handle(h)
	cur, _, _ := pLoadCursorW.Call(0, IDC_ARROW)
	brush, _, _ := pCreateSolidBrush.Call(rgb(244, 244, 244))
	wc := WNDCLASSEX{cbSize: uint32(unsafe.Sizeof(WNDCLASSEX{})), lpfnWndProc: variableCommandWndProc, hInstance: hi, hCursor: syscall.Handle(cur), hbrBackground: syscall.Handle(brush), lpszClassName: wstr(variableCmdClass)}
	r, _, err := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	if r == 0 {
		return fmt.Errorf("registrazione variabile: %v", err)
	}
	variableCmdRegistered = true
	return nil
}
func showVariableCommandDialog(owner syscall.Handle) (int, string, string, int, bool) {
	if variableCmdOpen || ensureVariableCommandClass() != nil {
		return 0, "", "", 0, false
	}
	_, vars := loadEventConditionCatalog()
	variableCmdItems = vars
	variableCmdCurrentID = 0
	const ww, wh int32 = 760, 500
	x, y := centerOwnedWindow(ww, wh)
	h, _, _ := pGetModuleHandleW.Call(0)
	hi := syscall.Handle(h)
	variableCmdAccepted = false
	variableCmdOpen = true
	variableCmdWindow = createWindow(variableCmdClass, "Crea / modifica variabile", WS_OVERLAPPEDWINDOW|WS_VISIBLE|WS_CLIPCHILDREN, x, y, ww, wh, owner, 0, hi)
	if variableCmdWindow == 0 {
		variableCmdOpen = false
		return 0, "", "", 0, false
	}
	setWindowIcon(variableCmdWindow)
	createWindow("STATIC", "Variabili del progetto:", WS_CHILD|WS_VISIBLE, 20, 18, 300, 24, variableCmdWindow, 7730, hi)
	variableCmdList = createWindow("LISTBOX", "", WS_CHILD|WS_VISIBLE|WS_BORDER|WS_VSCROLL|WS_TABSTOP|LBS_NOTIFY|LBS_NOINTEGRALHEIGHT, 20, 45, 310, 335, variableCmdWindow, idVariableCmdList, hi)
	for _, v := range variableCmdItems {
		addList(variableCmdList, fmt.Sprintf("%04d: %s", v.ID, v.Name))
	}
	createWindow("STATIC", "Nome:", WS_CHILD|WS_VISIBLE, 365, 45, 100, 24, variableCmdWindow, 7731, hi)
	variableCmdName = createWindow("EDIT", "", WS_CHILD|WS_VISIBLE|WS_BORDER|WS_TABSTOP, 365, 72, 335, 32, variableCmdWindow, idVariableCmdName, hi)
	createWindow("STATIC", "Operazione:", WS_CHILD|WS_VISIBLE, 365, 128, 140, 24, variableCmdWindow, 7732, hi)
	variableCmdOp = createWindow("COMBOBOX", "", WS_CHILD|WS_VISIBLE|WS_TABSTOP|CBS_DROPDOWNLIST, 365, 155, 240, 180, variableCmdWindow, idVariableCmdOp, hi)
	for _, x := range []string{"Imposta (=)", "Aggiungi (+=)", "Sottrai (-=)", "Moltiplica (*=)", "Dividi (/=)", "Modulo (%)"} {
		comboAdd(variableCmdOp, x)
	}
	pSendMessageW.Call(uintptr(variableCmdOp), CB_SETCURSEL, 0, 0)
	createWindow("STATIC", "Valore:", WS_CHILD|WS_VISIBLE, 365, 218, 120, 24, variableCmdWindow, 7733, hi)
	variableCmdValue = createWindow("EDIT", "0", WS_CHILD|WS_VISIBLE|WS_BORDER|WS_TABSTOP, 365, 245, 160, 32, variableCmdWindow, idVariableCmdValue, hi)
	createWindow("STATIC", "Se scrivi un nome nuovo, PLM assegna automaticamente il prossimo ID libero e lo salva nel catalogo reale del progetto.", WS_CHILD|WS_VISIBLE, 365, 305, 335, 65, variableCmdWindow, 7734, hi)
	createWindow("BUTTON", "Conferma", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 485, 395, 100, 36, variableCmdWindow, idVariableCmdOK, hi)
	createWindow("BUTTON", "Annulla", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 600, 395, 100, 36, variableCmdWindow, idVariableCmdCancel, hi)
	pEnableWindow.Call(uintptr(owner), 0)
	modalLoop(&variableCmdOpen)
	pEnableWindow.Call(uintptr(owner), 1)
	pSetFocus.Call(uintptr(owner))
	return variableCmdResultID, variableCmdResultName, variableCmdResultOp, variableCmdResultValue, variableCmdAccepted
}

// -----------------------------------------------------------------------------
// Common Event selector
// -----------------------------------------------------------------------------
type plmCommonEventChoice struct {
	ID   int
	Name string
}

func loadPLMCommonEvents() []plmCommonEventChoice {
	if strings.TrimSpace(currentProject) == "" {
		return nil
	}
	paths := []string{filepath.Join(currentProject, "converted", "data", "common_events.json"), filepath.Join(currentProject, "converted", "data", "CommonEvents.json"), filepath.Join(currentProject, "converted", "common_events.json")}
	by := map[int]string{}
	var walk func(any)
	walk = func(v any) {
		switch t := v.(type) {
		case []any:
			for _, x := range t {
				walk(x)
			}
		case map[string]any:
			id := 0
			if rv, ok := anyMapValueCI(t, "id", "ID", "index"); ok {
				id = anyInt(rv)
			}
			name := ""
			if rv, ok := anyMapValueCI(t, "name", "Name"); ok {
				name = strings.TrimSpace(anyString(rv))
			}
			if id > 0 && name != "" {
				by[id] = name
			}
			for _, x := range t {
				if _, ok := x.(map[string]any); ok {
					walk(x)
				}
				if _, ok := x.([]any); ok {
					walk(x)
				}
			}
		}
	}
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		v, err := decodeJSONAny(b)
		if err == nil {
			walk(v)
		}
	}
	out := make([]plmCommonEventChoice, 0, len(by))
	for id, n := range by {
		out = append(out, plmCommonEventChoice{id, n})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

const (
	commonCmdClass = "PLMStudioCommonEventCommand01"
	idCommonList   = 7740
	idCommonID     = 7741
	idCommonName   = 7742
	idCommonOK     = 7743
	idCommonCancel = 7744
)

var commonCmdRegistered, commonCmdOpen, commonCmdAccepted bool
var commonCmdWindow, commonCmdList, commonCmdID, commonCmdName syscall.Handle
var commonCmdItems []plmCommonEventChoice
var commonCmdResultID int
var commonCmdResultName string

func commonCmdSelected() int {
	r, _, _ := pSendMessageW.Call(uintptr(commonCmdList), LB_GETCURSEL, 0, 0)
	if int32(r) < 0 {
		return -1
	}
	return int(r)
}
func commonCmdSelect() {
	i := commonCmdSelected()
	if i >= 0 && i < len(commonCmdItems) {
		setIntField(commonCmdID, commonCmdItems[i].ID)
		setText(commonCmdName, commonCmdItems[i].Name)
	}
}
func commonCmdWndProcFn(hwnd syscall.Handle, msg uint32, w, l uintptr) uintptr {
	switch msg {
	case WM_COMMAND:
		id := int(loword(w))
		n := int(hiword(w))
		if id == idCommonList && (n == LBN_SELCHANGE || n == LBN_DBLCLK) {
			commonCmdSelect()
			if n == LBN_SELCHANGE {
				return 0
			}
		}
		switch id {
		case idCommonOK:
			idv := intField(commonCmdID, 0)
			if idv <= 0 {
				msgbox("PML Studio - Evento comune", "Inserisci un ID evento comune valido.", MB_OK|MB_ICONINFORMATION)
				return 0
			}
			commonCmdResultID = idv
			commonCmdResultName = strings.TrimSpace(getText(commonCmdName))
			commonCmdAccepted = true
			pDestroyWindow.Call(uintptr(hwnd))
			return 0
		case idCommonCancel:
			pDestroyWindow.Call(uintptr(hwnd))
			return 0
		}
	case WM_CLOSE:
		pDestroyWindow.Call(uintptr(hwnd))
		return 0
	case WM_DESTROY:
		commonCmdOpen = false
		commonCmdWindow = 0
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(uintptr(hwnd), uintptr(msg), w, l)
	return r
}

var commonCmdWndProc = syscall.NewCallback(commonCmdWndProcFn)

func ensureCommonCmdClass() error {
	if commonCmdRegistered {
		return nil
	}
	h, _, _ := pGetModuleHandleW.Call(0)
	hi := syscall.Handle(h)
	cur, _, _ := pLoadCursorW.Call(0, IDC_ARROW)
	brush, _, _ := pCreateSolidBrush.Call(rgb(244, 244, 244))
	wc := WNDCLASSEX{cbSize: uint32(unsafe.Sizeof(WNDCLASSEX{})), lpfnWndProc: commonCmdWndProc, hInstance: hi, hCursor: syscall.Handle(cur), hbrBackground: syscall.Handle(brush), lpszClassName: wstr(commonCmdClass)}
	r, _, err := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	if r == 0 {
		return err
	}
	commonCmdRegistered = true
	return nil
}
func showCommonEventCommandDialog(owner syscall.Handle) (int, string, bool) {
	if commonCmdOpen || ensureCommonCmdClass() != nil {
		return 0, "", false
	}
	commonCmdItems = loadPLMCommonEvents()
	const ww, wh int32 = 700, 450
	x, y := centerOwnedWindow(ww, wh)
	h, _, _ := pGetModuleHandleW.Call(0)
	hi := syscall.Handle(h)
	commonCmdAccepted = false
	commonCmdOpen = true
	commonCmdWindow = createWindow(commonCmdClass, "Chiama evento comune", WS_OVERLAPPEDWINDOW|WS_VISIBLE|WS_CLIPCHILDREN, x, y, ww, wh, owner, 0, hi)
	if commonCmdWindow == 0 {
		commonCmdOpen = false
		return 0, "", false
	}
	setWindowIcon(commonCmdWindow)
	createWindow("STATIC", "Eventi comuni rilevati:", WS_CHILD|WS_VISIBLE, 20, 18, 280, 24, commonCmdWindow, 7750, hi)
	commonCmdList = createWindow("LISTBOX", "", WS_CHILD|WS_VISIBLE|WS_BORDER|WS_VSCROLL|WS_TABSTOP|LBS_NOTIFY|LBS_NOINTEGRALHEIGHT, 20, 45, 310, 300, commonCmdWindow, idCommonList, hi)
	for _, v := range commonCmdItems {
		addList(commonCmdList, fmt.Sprintf("%04d: %s", v.ID, v.Name))
	}
	createWindow("STATIC", "ID:", WS_CHILD|WS_VISIBLE, 365, 55, 80, 24, commonCmdWindow, 7751, hi)
	commonCmdID = createWindow("EDIT", "1", WS_CHILD|WS_VISIBLE|WS_BORDER|WS_TABSTOP, 365, 82, 110, 32, commonCmdWindow, idCommonID, hi)
	createWindow("STATIC", "Nome (facoltativo):", WS_CHILD|WS_VISIBLE, 365, 135, 220, 24, commonCmdWindow, 7752, hi)
	commonCmdName = createWindow("EDIT", "", WS_CHILD|WS_VISIBLE|WS_BORDER|WS_TABSTOP, 365, 162, 275, 32, commonCmdWindow, idCommonName, hi)
	createWindow("STATIC", "Se il progetto non espone ancora il catalogo, puoi inserire direttamente l'ID.", WS_CHILD|WS_VISIBLE, 365, 220, 275, 55, commonCmdWindow, 7753, hi)
	createWindow("BUTTON", "Conferma", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 425, 310, 100, 36, commonCmdWindow, idCommonOK, hi)
	createWindow("BUTTON", "Annulla", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 540, 310, 100, 36, commonCmdWindow, idCommonCancel, hi)
	pEnableWindow.Call(uintptr(owner), 0)
	modalLoop(&commonCmdOpen)
	pEnableWindow.Call(uintptr(owner), 1)
	pSetFocus.Call(uintptr(owner))
	return commonCmdResultID, commonCmdResultName, commonCmdAccepted
}

// -----------------------------------------------------------------------------
// Follow command dialog
// -----------------------------------------------------------------------------
const (
	followCmdClass   = "PLMStudioFollowCommand01"
	idFollowTarget   = 7760
	idFollowDistance = 7761
	idFollowOK       = 7762
	idFollowCancel   = 7763
)

var followCmdRegistered, followCmdOpen, followCmdAccepted, followCmdNeedsTarget bool
var followCmdWindow, followCmdTarget, followCmdDistance syscall.Handle
var followCmdResultTarget, followCmdResultDistance int

func followCmdWndProcFn(hwnd syscall.Handle, msg uint32, w, l uintptr) uintptr {
	switch msg {
	case WM_COMMAND:
		switch int(loword(w)) {
		case idFollowOK:
			t := 0
			if followCmdNeedsTarget {
				t = intField(followCmdTarget, 0)
				if t <= 0 {
					msgbox("PML Studio - Segui", "Inserisci l'ID dell'NPC/evento da seguire.", MB_OK|MB_ICONINFORMATION)
					return 0
				}
			}
			d := intField(followCmdDistance, 1)
			if d < 1 {
				d = 1
			}
			followCmdResultTarget = t
			followCmdResultDistance = d
			followCmdAccepted = true
			pDestroyWindow.Call(uintptr(hwnd))
			return 0
		case idFollowCancel:
			pDestroyWindow.Call(uintptr(hwnd))
			return 0
		}
	case WM_CLOSE:
		pDestroyWindow.Call(uintptr(hwnd))
		return 0
	case WM_DESTROY:
		followCmdOpen = false
		followCmdWindow = 0
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(uintptr(hwnd), uintptr(msg), w, l)
	return r
}

var followCmdWndProc = syscall.NewCallback(followCmdWndProcFn)

func ensureFollowCmdClass() error {
	if followCmdRegistered {
		return nil
	}
	h, _, _ := pGetModuleHandleW.Call(0)
	hi := syscall.Handle(h)
	cur, _, _ := pLoadCursorW.Call(0, IDC_ARROW)
	brush, _, _ := pCreateSolidBrush.Call(rgb(244, 244, 244))
	wc := WNDCLASSEX{cbSize: uint32(unsafe.Sizeof(WNDCLASSEX{})), lpfnWndProc: followCmdWndProc, hInstance: hi, hCursor: syscall.Handle(cur), hbrBackground: syscall.Handle(brush), lpszClassName: wstr(followCmdClass)}
	r, _, err := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	if r == 0 {
		return err
	}
	followCmdRegistered = true
	return nil
}
func showFollowCommandDialog(owner syscall.Handle, playerFollowsNPC bool) (int, int, bool) {
	if followCmdOpen || ensureFollowCmdClass() != nil {
		return 0, 0, false
	}
	followCmdNeedsTarget = playerFollowsNPC
	title := "NPC segue giocatore"
	if playerFollowsNPC {
		title = "Giocatore segue NPC/evento"
	}
	const ww, wh int32 = 540, 320
	x, y := centerOwnedWindow(ww, wh)
	h, _, _ := pGetModuleHandleW.Call(0)
	hi := syscall.Handle(h)
	followCmdAccepted = false
	followCmdOpen = true
	followCmdWindow = createWindow(followCmdClass, title, WS_OVERLAPPEDWINDOW|WS_VISIBLE|WS_CLIPCHILDREN, x, y, ww, wh, owner, 0, hi)
	if followCmdWindow == 0 {
		followCmdOpen = false
		return 0, 0, false
	}
	setWindowIcon(followCmdWindow)
	yy := int32(32)
	if playerFollowsNPC {
		createWindow("STATIC", "ID NPC/evento da seguire:", WS_CHILD|WS_VISIBLE, 25, yy, 210, 24, followCmdWindow, 7770, hi)
		followCmdTarget = createWindow("EDIT", "1", WS_CHILD|WS_VISIBLE|WS_BORDER|WS_TABSTOP, 250, yy-5, 120, 32, followCmdWindow, idFollowTarget, hi)
		yy += 60
	}
	createWindow("STATIC", "Distanza desiderata (tile):", WS_CHILD|WS_VISIBLE, 25, yy, 220, 24, followCmdWindow, 7771, hi)
	followCmdDistance = createWindow("EDIT", "1", WS_CHILD|WS_VISIBLE|WS_BORDER|WS_TABSTOP, 250, yy-5, 120, 32, followCmdWindow, idFollowDistance, hi)
	createWindow("STATIC", "Il comportamento è salvato come comando PLM dinamico e sarà eseguito dal runtime Python.", WS_CHILD|WS_VISIBLE, 25, yy+55, 450, 50, followCmdWindow, 7772, hi)
	createWindow("BUTTON", "Conferma", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 275, 225, 100, 36, followCmdWindow, idFollowOK, hi)
	createWindow("BUTTON", "Annulla", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 390, 225, 100, 36, followCmdWindow, idFollowCancel, hi)
	pEnableWindow.Call(uintptr(owner), 0)
	modalLoop(&followCmdOpen)
	pEnableWindow.Call(uintptr(owner), 1)
	pSetFocus.Call(uintptr(owner))
	return followCmdResultTarget, followCmdResultDistance, followCmdAccepted
}

// modalLoop centralizes the Win32 modal pump used by the small command forms.
func modalLoop(open *bool) {
	var m MSG
	repostQuit := false
	for *open {
		r, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) == -1 {
			break
		}
		if r == 0 {
			repostQuit = true
			break
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		pDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
	if repostQuit {
		pPostQuitMessage.Call(0)
	}
}

// strconv is kept here because event command forms deliberately accept signed
// constants; referencing it in a harmless helper also makes the intent clear.
var _ = strconv.IntSize
