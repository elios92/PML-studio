//go:build windows

package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"unsafe"
)

// fieldMoveDefinition is deliberately data-driven. PLM Studio never assumes
// that the project only has the stock HMs/MNs: any configured field move can
// appear here, including custom MN17+ entries.
type fieldMoveDefinition struct {
	ID       string
	Name     string
	Move     string
	Script   string
	Handler  string
	OneShot  bool
	Enabled  bool
	Explicit bool
	Source   string
}

func normalizeFieldMoveID(s string) string {
	return strings.ToUpper(strings.TrimSpace(s))
}

func looksLikeMachineID(id string) bool {
	id = normalizeFieldMoveID(id)
	return strings.HasPrefix(id, "MN") || strings.HasPrefix(id, "HM")
}

func configuredFieldMoves() []fieldMoveDefinition {
	if strings.TrimSpace(currentProject) == "" {
		return nil
	}
	byID := map[string]fieldMoveDefinition{}

	// Explicit PLM/project configurations have priority. The parser accepts
	// common field names so converted/imported projects do not need to be
	// rewritten just to appear in this picker.
	jsonCandidates := []string{
		filepath.Join("converted", "data", "plm_field_moves.json"),
		filepath.Join("converted", "data", "field_moves.json"),
		filepath.Join("data", "plm_field_moves.json"),
		filepath.Join("data", "field_moves.json"),
		filepath.Join(".plm", "field_moves.json"),
		filepath.Join("config", "field_moves.json"),
	}
	for _, rel := range jsonCandidates {
		parseFieldMoveJSON(filepath.Join(currentProject, rel), byID)
	}
	textCandidates := []string{
		filepath.Join("PBS", "field_moves.txt"),
		filepath.Join("pbs", "field_moves.txt"),
		filepath.Join("converted", "PBS", "field_moves.txt"),
		filepath.Join("converted", "pbs", "field_moves.txt"),
	}
	for _, rel := range textCandidates {
		parseFieldMovePBS(filepath.Join(currentProject, rel), byID, true)
	}

	// Essentials fallback: an HM/MN item with an assigned Move is already a
	// configured machine. This is enough for PLM to expose it without a stock
	// hard-coded MN01..MN16 table. Custom MN17/MN18/etc. work automatically.
	moveNames := loadFieldMoveMoveNames()
	itemCandidates := []string{
		filepath.Join("PBS", "items.txt"),
		filepath.Join("pbs", "items.txt"),
		filepath.Join("converted", "PBS", "items.txt"),
		filepath.Join("converted", "pbs", "items.txt"),
	}
	for _, rel := range itemCandidates {
		parseMachineItemsPBS(filepath.Join(currentProject, rel), byID, moveNames)
	}

	out := make([]fieldMoveDefinition, 0, len(byID))
	for _, d := range byID {
		if !d.Enabled {
			continue
		}
		d.ID = normalizeFieldMoveID(d.ID)
		d.Move = strings.ToUpper(strings.TrimSpace(d.Move))
		if d.ID == "" {
			continue
		}
		if strings.TrimSpace(d.Name) == "" {
			if n := moveNames[d.Move]; n != "" {
				d.Name = n
			} else if d.Move != "" {
				d.Name = d.Move
			} else {
				d.Name = d.ID
			}
		}
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool {
		ai, aj := strings.ToLower(out[i].Name), strings.ToLower(out[j].Name)
		if ai == aj {
			return out[i].ID < out[j].ID
		}
		return ai < aj
	})
	return out
}

func parseFieldMoveJSON(path string, dst map[string]fieldMoveDefinition) {
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	root, err := decodeJSONAny(b)
	if err != nil {
		return
	}
	var walk func(any, string)
	walk = func(v any, keyHint string) {
		switch t := v.(type) {
		case []any:
			for _, x := range t {
				walk(x, "")
			}
		case map[string]any:
			id := strings.TrimSpace(jsonStringCI(t, "id", "machine", "mn", "hm", "item", "internal_name", "internalname"))
			if id == "" && keyHint != "" {
				id = keyHint
			}
			move := strings.TrimSpace(jsonStringCI(t, "move", "move_id", "moveid"))
			name := strings.TrimSpace(jsonStringCI(t, "name", "display_name", "displayname", "title"))
			handler := strings.TrimSpace(jsonStringCI(t, "handler", "action", "event_handler", "eventhandler"))
			script := strings.TrimSpace(jsonStringCI(t, "script", "event_script", "eventscript", "ruby_script", "rubyscript"))

			// A file named field_moves.json may contain metadata containers;
			// only records that look like actual configured moves are accepted.
			if id != "" && (move != "" || handler != "" || script != "") {
				enabled := true
				if ev, ok := anyMapValueCI(t, "enabled", "active", "configured"); ok {
					enabled = eventAnyBool(ev)
				}
				oneShot := false
				if ov, ok := anyMapValueCI(t, "one_shot", "oneshot", "consume_event", "consumeevent"); ok {
					oneShot = eventAnyBool(ov)
				}
				key := normalizeFieldMoveID(id)
				d := fieldMoveDefinition{ID: key, Name: name, Move: move, Handler: handler, Script: script, OneShot: oneShot, Enabled: enabled, Explicit: true, Source: path}
				dst[key] = mergeFieldMoveDefinition(dst[key], d)
			}
			for k, x := range t {
				switch x.(type) {
				case map[string]any, []any:
					walk(x, k)
				}
			}
		}
	}
	walk(root, "")
}

func mergeFieldMoveDefinition(old, newer fieldMoveDefinition) fieldMoveDefinition {
	if old.ID == "" {
		return newer
	}
	// Explicit definitions override inferred Essentials data, while later
	// explicit files can fill missing fields without erasing valid values.
	if newer.Explicit || !old.Explicit {
		if newer.ID != "" {
			old.ID = newer.ID
		}
		if newer.Name != "" {
			old.Name = newer.Name
		}
		if newer.Move != "" {
			old.Move = newer.Move
		}
		if newer.Handler != "" {
			old.Handler = newer.Handler
		}
		if newer.Script != "" {
			old.Script = newer.Script
		}
		old.OneShot = newer.OneShot
		old.Enabled = newer.Enabled
		if newer.Source != "" {
			old.Source = newer.Source
		}
		old.Explicit = old.Explicit || newer.Explicit
	}
	return old
}

func parseFieldMovePBS(path string, dst map[string]fieldMoveDefinition, explicit bool) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	section := ""
	fields := map[string]string{}
	flush := func() {
		id := normalizeFieldMoveID(section)
		if id == "" {
			return
		}
		enabled := true
		if v, ok := fields["enabled"]; ok {
			enabled = strings.EqualFold(v, "true") || v == "1" || strings.EqualFold(v, "yes") || strings.EqualFold(v, "on")
		}
		d := fieldMoveDefinition{
			ID: id, Name: fields["name"], Move: fields["move"], Script: fields["script"], Handler: fields["handler"],
			Enabled: enabled, Explicit: explicit, Source: path,
		}
		if v := fields["oneshot"]; v != "" {
			d.OneShot = strings.EqualFold(v, "true") || v == "1" || strings.EqualFold(v, "yes") || strings.EqualFold(v, "on")
		}
		if d.Move != "" || d.Script != "" || d.Handler != "" {
			dst[id] = mergeFieldMoveDefinition(dst[id], d)
		}
	}
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := strings.TrimSpace(stripPBSComment(s.Text()))
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			flush()
			section = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "["), "]"))
			fields = map[string]string{}
			continue
		}
		if p := strings.Index(line, "="); p >= 0 && section != "" {
			k := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(line[:p]), "_", ""))
			fields[k] = strings.TrimSpace(line[p+1:])
		}
	}
	flush()
}

func parseMachineItemsPBS(path string, dst map[string]fieldMoveDefinition, moveNames map[string]string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	section, name, move := "", "", ""
	flush := func() {
		id := normalizeFieldMoveID(section)
		mv := strings.ToUpper(strings.TrimSpace(move))
		if id == "" || mv == "" || !looksLikeMachineID(id) {
			return
		}
		// Do not override a dedicated PLM Field Move configuration.
		if old := dst[id]; old.ID != "" && old.Explicit {
			return
		}
		n := strings.TrimSpace(name)
		if n == "" || strings.EqualFold(n, id) {
			if mn := strings.TrimSpace(moveNames[mv]); mn != "" {
				n = mn
			}
		}
		d := fieldMoveDefinition{ID: id, Name: n, Move: mv, Enabled: true, Explicit: false, Source: path}
		dst[id] = mergeFieldMoveDefinition(dst[id], d)
	}
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := strings.TrimSpace(stripPBSComment(s.Text()))
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			flush()
			section = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "["), "]"))
			name, move = "", ""
			continue
		}
		if section == "" {
			continue
		}
		if p := strings.Index(line, "="); p >= 0 {
			key := strings.TrimSpace(line[:p])
			val := strings.TrimSpace(line[p+1:])
			if strings.EqualFold(key, "Name") {
				name = val
			} else if strings.EqualFold(key, "Move") {
				move = val
			}
		}
	}
	flush()
}

func loadFieldMoveMoveNames() map[string]string {
	out := map[string]string{}
	if currentProject == "" {
		return out
	}
	candidates := []string{
		filepath.Join("PBS", "moves.txt"), filepath.Join("pbs", "moves.txt"),
		filepath.Join("converted", "PBS", "moves.txt"), filepath.Join("converted", "pbs", "moves.txt"),
	}
	for _, rel := range candidates {
		path := filepath.Join(currentProject, rel)
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		section, name := "", ""
		flush := func() {
			id := strings.ToUpper(strings.TrimSpace(section))
			if id != "" && strings.TrimSpace(name) != "" {
				out[id] = strings.TrimSpace(name)
			}
		}
		s := bufio.NewScanner(f)
		for s.Scan() {
			line := strings.TrimSpace(stripPBSComment(s.Text()))
			if line == "" {
				continue
			}
			if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
				flush()
				section = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "["), "]"))
				name = ""
				continue
			}
			if p := strings.Index(line, "="); p >= 0 && strings.EqualFold(strings.TrimSpace(line[:p]), "Name") {
				name = strings.TrimSpace(line[p+1:])
			}
		}
		flush()
		_ = f.Close()
	}
	return out
}

// -----------------------------------------------------------------------------
// Field Move / MN event dialog
// -----------------------------------------------------------------------------

const (
	fieldMoveDialogClass = "PLMStudioFieldMoveEvent01"
	idFieldMoveList      = 7960
	idFieldMoveGraphic   = 7961
	idFieldMovePickPNG   = 7962
	idFieldMoveOK        = 7963
	idFieldMoveCancel    = 7964
)

var (
	fieldMoveDialogRegistered  bool
	fieldMoveDialogOpen        bool
	fieldMoveDialogWindow      syscall.Handle
	fieldMoveDialogList        syscall.Handle
	fieldMoveDialogInfo        syscall.Handle
	fieldMoveDialogGraphic     syscall.Handle
	fieldMoveDialogCatalog     []fieldMoveDefinition
	fieldMoveDialogGraphicName string
	fieldMoveDialogAccepted    bool
	fieldMoveDialogResult      fieldMoveDefinition
)

func selectedFieldMoveIndex() int {
	if fieldMoveDialogList == 0 {
		return -1
	}
	r, _, _ := pSendMessageW.Call(uintptr(fieldMoveDialogList), LB_GETCURSEL, 0, 0)
	if int32(r) < 0 {
		return -1
	}
	return int(r)
}

func refreshFieldMoveDialogInfo() {
	idx := selectedFieldMoveIndex()
	if idx < 0 || idx >= len(fieldMoveDialogCatalog) {
		setText(fieldMoveDialogInfo, "Seleziona una MN/Field Move configurata.")
		return
	}
	d := fieldMoveDialogCatalog[idx]
	source := filepath.Base(d.Source)
	if source == "" {
		source = "configurazione progetto"
	}
	handler := strings.TrimSpace(d.Handler)
	if handler == "" {
		if strings.TrimSpace(d.Script) != "" {
			handler = "script evento configurato"
		} else {
			handler = "runtime PLM / Python"
		}
	}
	setText(fieldMoveDialogInfo, fmt.Sprintf("%s [%s]\r\nMossa: %s\r\nGestione: %s\r\nFonte: %s", d.Name, d.ID, d.Move, handler, source))
}

func fieldMoveDialogWndProcFn(hwnd syscall.Handle, msg uint32, w, l uintptr) uintptr {
	switch msg {
	case WM_COMMAND:
		id := int(loword(w))
		notify := int(hiword(w))
		if id == idFieldMoveList && notify == LBN_SELCHANGE {
			refreshFieldMoveDialogInfo()
			return 0
		}
		switch id {
		case idFieldMovePickPNG:
			current := fieldMoveDialogGraphicName
			graphic, ok := showCharacterPicker(hwnd, current)
			if ok {
				fieldMoveDialogGraphicName = strings.TrimSpace(graphic)
				if fieldMoveDialogGraphicName == "" {
					setText(fieldMoveDialogGraphic, "Nessun PNG selezionato")
				} else {
					setText(fieldMoveDialogGraphic, fieldMoveDialogGraphicName)
				}
			}
			return 0
		case idFieldMoveOK:
			idx := selectedFieldMoveIndex()
			if idx < 0 || idx >= len(fieldMoveDialogCatalog) {
				msgbox("PML Studio - Evento Campo / MN", "Seleziona prima una MN/Field Move configurata.", MB_OK|MB_ICONINFORMATION)
				return 0
			}
			if strings.TrimSpace(fieldMoveDialogGraphicName) == "" {
				msgbox("PML Studio - Evento Campo / MN", "Seleziona o importa il PNG da usare per questo evento.", MB_OK|MB_ICONINFORMATION)
				return 0
			}
			fieldMoveDialogResult = fieldMoveDialogCatalog[idx]
			fieldMoveDialogAccepted = true
			pDestroyWindow.Call(uintptr(hwnd))
			return 0
		case idFieldMoveCancel:
			pDestroyWindow.Call(uintptr(hwnd))
			return 0
		}
	case WM_CLOSE:
		pDestroyWindow.Call(uintptr(hwnd))
		return 0
	case WM_DESTROY:
		fieldMoveDialogOpen = false
		fieldMoveDialogWindow = 0
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(uintptr(hwnd), uintptr(msg), w, l)
	return r
}

var fieldMoveDialogWndProc = syscall.NewCallback(fieldMoveDialogWndProcFn)

func ensureFieldMoveDialogClass() error {
	if fieldMoveDialogRegistered {
		return nil
	}
	h, _, _ := pGetModuleHandleW.Call(0)
	hi := syscall.Handle(h)
	cur, _, _ := pLoadCursorW.Call(0, IDC_ARROW)
	brush, _, _ := pCreateSolidBrush.Call(rgb(244, 244, 244))
	wc := WNDCLASSEX{cbSize: uint32(unsafe.Sizeof(WNDCLASSEX{})), lpfnWndProc: fieldMoveDialogWndProc, hInstance: hi, hCursor: syscall.Handle(cur), hbrBackground: syscall.Handle(brush), lpszClassName: wstr(fieldMoveDialogClass)}
	r, _, err := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	if r == 0 {
		return fmt.Errorf("registrazione finestra Evento Campo/MN: %v", err)
	}
	fieldMoveDialogRegistered = true
	return nil
}

func showFieldMoveEventDialog(owner syscall.Handle) (fieldMoveDefinition, string, bool) {
	catalog := configuredFieldMoves()
	if len(catalog) == 0 {
		msgbox("PML Studio - Evento Campo / MN", "Nel progetto non risultano MN/Field Move configurate.\r\n\r\nPLM legge le configurazioni Field Move del progetto e, per i progetti Essentials, anche le voci MN/HM di PBS/items.txt che hanno una proprietà Move=... .", MB_OK|MB_ICONINFORMATION)
		return fieldMoveDefinition{}, "", false
	}
	if fieldMoveDialogOpen {
		return fieldMoveDefinition{}, "", false
	}
	if err := ensureFieldMoveDialogClass(); err != nil {
		msgbox("PML Studio - Evento Campo / MN", err.Error(), MB_OK|MB_ICONERROR)
		return fieldMoveDefinition{}, "", false
	}

	const ww, wh int32 = 790, 590
	x, y := centerOwnedWindow(ww, wh)
	h, _, _ := pGetModuleHandleW.Call(0)
	hi := syscall.Handle(h)
	fieldMoveDialogCatalog = catalog
	fieldMoveDialogGraphicName = ""
	if eventEditorPageIndex >= 0 && eventEditorPageIndex < len(eventEditorPages) {
		fieldMoveDialogGraphicName = strings.TrimSpace(eventEditorPages[eventEditorPageIndex].Graphic)
	}
	fieldMoveDialogAccepted = false
	fieldMoveDialogResult = fieldMoveDefinition{}
	fieldMoveDialogOpen = true
	fieldMoveDialogWindow = createWindow(fieldMoveDialogClass, "Crea evento Campo / MN", WS_OVERLAPPEDWINDOW|WS_VISIBLE|WS_CLIPCHILDREN, x, y, ww, wh, owner, 0, hi)
	if fieldMoveDialogWindow == 0 {
		fieldMoveDialogOpen = false
		return fieldMoveDefinition{}, "", false
	}
	setWindowIcon(fieldMoveDialogWindow)

	createWindow("STATIC", "1. Scegli una MN / Field Move già configurata", WS_CHILD|WS_VISIBLE, 22, 18, 720, 26, fieldMoveDialogWindow, 7970, hi)
	fieldMoveDialogList = createWindow("LISTBOX", "", WS_CHILD|WS_VISIBLE|WS_BORDER|WS_VSCROLL|WS_TABSTOP|LBS_NOTIFY|LBS_NOINTEGRALHEIGHT, 22, 48, 335, 345, fieldMoveDialogWindow, idFieldMoveList, hi)
	for _, d := range catalog {
		label := fmt.Sprintf("%s   [%s]", d.Name, d.ID)
		if strings.TrimSpace(d.Move) != "" {
			label += "  · " + d.Move
		}
		addList(fieldMoveDialogList, label)
	}
	pSendMessageW.Call(uintptr(fieldMoveDialogList), LB_SETCURSEL, 0, 0)

	createWindow("STATIC", "Configurazione", WS_CHILD|WS_VISIBLE, 390, 48, 330, 24, fieldMoveDialogWindow, 7971, hi)
	fieldMoveDialogInfo = createWindow("STATIC", "", WS_CHILD|WS_VISIBLE|WS_BORDER, 390, 76, 355, 145, fieldMoveDialogWindow, 7972, hi)

	createWindow("STATIC", "2. PNG / sprite dell'evento", WS_CHILD|WS_VISIBLE, 390, 246, 330, 24, fieldMoveDialogWindow, 7973, hi)
	fieldMoveDialogGraphic = createWindow("STATIC", "Nessun PNG selezionato", WS_CHILD|WS_VISIBLE|WS_BORDER, 390, 276, 355, 38, fieldMoveDialogWindow, idFieldMoveGraphic, hi)
	createWindow("BUTTON", "Scegli / importa PNG...", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 390, 326, 220, 40, fieldMoveDialogWindow, idFieldMovePickPNG, hi)
	createWindow("STATIC", "Il PNG viene gestito tramite Graphics/Characters, quindi resta collegato al progetto anche dopo lo spostamento della cartella.", WS_CHILD|WS_VISIBLE, 390, 378, 355, 58, fieldMoveDialogWindow, 7974, hi)

	createWindow("BUTTON", "Salva e crea evento", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 490, 492, 160, 40, fieldMoveDialogWindow, idFieldMoveOK, hi)
	createWindow("BUTTON", "Annulla", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 662, 492, 84, 40, fieldMoveDialogWindow, idFieldMoveCancel, hi)

	if fieldMoveDialogGraphicName != "" {
		setText(fieldMoveDialogGraphic, fieldMoveDialogGraphicName)
	}
	refreshFieldMoveDialogInfo()

	pEnableWindow.Call(uintptr(owner), 0)
	pShowWindow.Call(uintptr(fieldMoveDialogWindow), SW_SHOW)
	pUpdateWindow.Call(uintptr(fieldMoveDialogWindow))
	var m MSG
	repostQuit := false
	for fieldMoveDialogOpen {
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
	pEnableWindow.Call(uintptr(owner), 1)
	pSetFocus.Call(uintptr(owner))
	if repostQuit {
		pPostQuitMessage.Call(0)
	}
	return fieldMoveDialogResult, fieldMoveDialogGraphicName, fieldMoveDialogAccepted
}

func addFieldMoveEventCommand() {
	d, graphic, ok := showFieldMoveEventDialog(eventEditorWindow)
	if !ok {
		return
	}
	if eventEditorPageIndex < 0 || eventEditorPageIndex >= len(eventEditorPages) {
		return
	}

	p := &eventEditorPages[eventEditorPageIndex]
	p.Graphic = strings.TrimSpace(graphic)
	p.Trigger = 0 // Pulsante azione: l'ostacolo/interazione viene usato dal giocatore.
	p.MoveType = 0
	p.Through = false
	p.DirectionFix = true
	p.WalkAnim = false
	p.StepAnim = false

	currentName := strings.TrimSpace(getText(eventEditorName))
	upperName := strings.ToUpper(currentName)
	if currentName == "" || strings.HasPrefix(upperName, "EV") || strings.HasPrefix(upperName, "EVENT") {
		setText(eventEditorName, strings.TrimSpace(d.ID+" - "+d.Name))
	}

	c := plmManagedEventCommand{
		Type: "field_move", FieldMoveID: d.ID, FieldMoveName: d.Name, Move: d.Move,
		Handler: d.Handler, Script: d.Script, OneShot: d.OneShot,
	}
	if d.OneShot {
		c.SelfSwitch = "A"
	}
	if appendManagedEventCommand(c) {
		if d.OneShot {
			// The Python runtime uses the PML_CMD metadata to activate A only
			// after a successful field-move action. The blank page is created
			// now so the event is already structurally complete.
			ensureSimpleConsumedBlankPage()
		}
		loadEventEditorPageToControls()
		refreshEventEditorCommandList()
		setToolbarStatus(fmt.Sprintf("Evento Campo/MN creato: %s [%s] con sprite %s.", d.Name, d.ID, graphic))
	}
}
