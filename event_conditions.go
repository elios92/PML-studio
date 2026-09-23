//go:build windows

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

const (
	eventConditionPickerClassName = "PLMStudioEventConditionPicker01"

	idCondList       = 7400
	idCondName       = 7401
	idCondState      = 7402
	idCondThreshold  = 7403
	idCondPreviewVal = 7404
	idCondOK         = 7405
	idCondCancel     = 7406
)

type eventNamedValue struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type eventConditionCatalogFile struct {
	Version   int               `json:"version"`
	Switches  []eventNamedValue `json:"switches"`
	Variables []eventNamedValue `json:"variables"`
}

type eventConditionPickerMode int

const (
	conditionPickerSwitch eventConditionPickerMode = iota
	conditionPickerVariable
)

var (
	eventConditionPickerRegistered bool
	eventConditionPickerOpen       bool
	eventConditionPickerWindow     syscall.Handle
	eventConditionPickerOwner      syscall.Handle
	eventConditionPickerList       syscall.Handle
	eventConditionPickerName       syscall.Handle
	eventConditionPickerState      syscall.Handle
	eventConditionPickerThreshold  syscall.Handle
	eventConditionPickerPreviewVal syscall.Handle
	eventConditionPickerPreview    syscall.Handle

	eventConditionPickerModeValue   eventConditionPickerMode
	eventConditionPickerItems       []eventNamedValue
	eventConditionPickerCurrentID   int
	eventConditionPickerAccepted    bool
	eventConditionPickerResultID    int
	eventConditionPickerResultName  string
	eventConditionPickerResultOn    bool
	eventConditionPickerResultValue int

	eventConditionCatalogCacheProject   string
	eventConditionCatalogCacheSwitches  []eventNamedValue
	eventConditionCatalogCacheVariables []eventNamedValue
	eventConditionCatalogCacheValid     bool
)

func eventConditionCatalogPath() string {
	if strings.TrimSpace(currentProject) == "" {
		return ""
	}
	return filepath.Join(currentProject, "converted", "data", "plm_switches_variables.json")
}

func normalizeEventNamedValues(values []eventNamedValue) []eventNamedValue {
	byID := map[int]eventNamedValue{}
	for _, v := range values {
		if v.ID <= 0 {
			continue
		}
		v.Name = strings.TrimSpace(v.Name)
		if old, ok := byID[v.ID]; ok {
			if old.Name == "" && v.Name != "" {
				byID[v.ID] = v
			}
			continue
		}
		byID[v.ID] = v
	}
	ids := make([]int, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	out := make([]eventNamedValue, 0, len(ids))
	for _, id := range ids {
		out = append(out, byID[id])
	}
	return out
}

func mergeEventNamedValues(dst []eventNamedValue, src []eventNamedValue) []eventNamedValue {
	byID := map[int]eventNamedValue{}
	for _, v := range dst {
		if v.ID > 0 {
			byID[v.ID] = eventNamedValue{ID: v.ID, Name: strings.TrimSpace(v.Name)}
		}
	}
	for _, v := range src {
		if v.ID <= 0 {
			continue
		}
		v.Name = strings.TrimSpace(v.Name)
		old, ok := byID[v.ID]
		if !ok || (old.Name == "" && v.Name != "") {
			byID[v.ID] = v
		}
	}
	out := make([]eventNamedValue, 0, len(byID))
	for _, v := range byID {
		out = append(out, v)
	}
	return normalizeEventNamedValues(out)
}

func eventNamedValuesFromAny(v any) []eventNamedValue {
	out := []eventNamedValue{}
	switch t := v.(type) {
	case []any:
		for i, item := range t {
			id := i
			name := ""
			switch x := item.(type) {
			case string:
				name = strings.TrimSpace(x)
			case map[string]any:
				if rv, ok := anyMapValueCI(x, "id", "index"); ok {
					id = anyInt(rv)
				}
				if rv, ok := anyMapValueCI(x, "name", "label"); ok {
					name = strings.TrimSpace(anyString(rv))
				}
			}
			if id > 0 && name != "" {
				out = append(out, eventNamedValue{ID: id, Name: name})
			}
		}
	case map[string]any:
		for k, item := range t {
			id, _ := strconv.Atoi(strings.TrimSpace(k))
			name := ""
			switch x := item.(type) {
			case string:
				name = strings.TrimSpace(x)
			case map[string]any:
				if rv, ok := anyMapValueCI(x, "id", "index"); ok {
					id = anyInt(rv)
				}
				if rv, ok := anyMapValueCI(x, "name", "label"); ok {
					name = strings.TrimSpace(anyString(rv))
				}
			}
			if id > 0 && name != "" {
				out = append(out, eventNamedValue{ID: id, Name: name})
			}
		}
	}
	return normalizeEventNamedValues(out)
}

func findNamedValuesByKeys(v any, keys ...string) []eventNamedValue {
	wanted := map[string]bool{}
	for _, k := range keys {
		wanted[strings.ToLower(strings.TrimSpace(k))] = true
	}
	var walk func(any) []eventNamedValue
	walk = func(node any) []eventNamedValue {
		switch t := node.(type) {
		case map[string]any:
			for k, value := range t {
				if wanted[strings.ToLower(strings.TrimSpace(k))] {
					if found := eventNamedValuesFromAny(value); len(found) > 0 {
						return found
					}
				}
			}
			for _, value := range t {
				if found := walk(value); len(found) > 0 {
					return found
				}
			}
		case []any:
			for _, value := range t {
				if found := walk(value); len(found) > 0 {
					return found
				}
			}
		}
		return nil
	}
	return walk(v)
}

func loadNamedValuesFromJSON(path string) (switches, variables []eventNamedValue) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, nil
	}
	v, err := decodeJSONAny(b)
	if err != nil {
		return nil, nil
	}
	switches = findNamedValuesByKeys(v, "switches", "switch_names", "switchNames", "game_switches")
	variables = findNamedValuesByKeys(v, "variables", "variable_names", "variableNames", "game_variables")
	return normalizeEventNamedValues(switches), normalizeEventNamedValues(variables)
}

func collectEventConditionRefs(v any, switches, variables *[]eventNamedValue) {
	switch t := v.(type) {
	case map[string]any:
		if cv, ok := anyMapValueCI(t, "condition"); ok {
			if c, ok := cv.(map[string]any); ok {
				for _, prefix := range []string{"switch1", "switch2"} {
					id := 0
					if rv, ok := anyMapValueCI(c, prefix+"_id"); ok {
						id = anyInt(rv)
					}
					if id > 0 {
						name := ""
						if rv, ok := anyMapValueCI(c, "plm_"+prefix+"_name", prefix+"_name"); ok {
							name = strings.TrimSpace(anyString(rv))
						}
						*switches = append(*switches, eventNamedValue{ID: id, Name: name})
					}
				}
				id := 0
				if rv, ok := anyMapValueCI(c, "variable_id"); ok {
					id = anyInt(rv)
				}
				if id > 0 {
					name := ""
					if rv, ok := anyMapValueCI(c, "plm_variable_name", "variable_name"); ok {
						name = strings.TrimSpace(anyString(rv))
					}
					*variables = append(*variables, eventNamedValue{ID: id, Name: name})
				}
			}
		}
		for _, value := range t {
			collectEventConditionRefs(value, switches, variables)
		}
	case []any:
		for _, value := range t {
			collectEventConditionRefs(value, switches, variables)
		}
	}
}

func invalidateEventConditionCatalogCache() {
	eventConditionCatalogCacheValid = false
	eventConditionCatalogCacheProject = ""
	eventConditionCatalogCacheSwitches = nil
	eventConditionCatalogCacheVariables = nil
}

func cloneEventNamedValues(values []eventNamedValue) []eventNamedValue {
	return append([]eventNamedValue(nil), values...)
}

func loadEventConditionCatalog() (switches, variables []eventNamedValue) {
	if strings.TrimSpace(currentProject) == "" {
		return nil, nil
	}
	projectKey := strings.ToLower(filepath.Clean(currentProject))
	if eventConditionCatalogCacheValid && eventConditionCatalogCacheProject == projectKey {
		return cloneEventNamedValues(eventConditionCatalogCacheSwitches), cloneEventNamedValues(eventConditionCatalogCacheVariables)
	}
	candidates := []string{
		eventConditionCatalogPath(),
		filepath.Join(currentProject, "converted", "data", "system.json"),
		filepath.Join(currentProject, "converted", "data", "System.json"),
		filepath.Join(currentProject, "converted", "system.json"),
		filepath.Join(currentProject, "converted", "System.json"),
		filepath.Join(currentProject, "data", "system.json"),
		filepath.Join(currentProject, "Data", "System.json"),
		filepath.Join(currentProject, "system.json"),
		filepath.Join(currentProject, "System.json"),
	}
	seen := map[string]bool{}
	for _, path := range candidates {
		path = filepath.Clean(path)
		key := strings.ToLower(path)
		if seen[key] || !exists(path) {
			continue
		}
		seen[key] = true
		s, v := loadNamedValuesFromJSON(path)
		switches = mergeEventNamedValues(switches, s)
		variables = mergeEventNamedValues(variables, v)
	}

	mapsRoot := filepath.Join(currentProject, "converted", "maps")
	_ = filepath.WalkDir(mapsRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil || d == nil || d.IsDir() || !strings.EqualFold(filepath.Ext(path), ".json") {
			return nil
		}
		base := strings.ToLower(filepath.Base(path))
		if !strings.HasPrefix(base, "map") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		v, err := decodeJSONAny(b)
		if err != nil {
			return nil
		}
		var s, vars []eventNamedValue
		collectEventConditionRefs(v, &s, &vars)
		switches = mergeEventNamedValues(switches, s)
		variables = mergeEventNamedValues(variables, vars)
		return nil
	})
	switches = normalizeEventNamedValues(switches)
	variables = normalizeEventNamedValues(variables)
	eventConditionCatalogCacheProject = projectKey
	eventConditionCatalogCacheSwitches = cloneEventNamedValues(switches)
	eventConditionCatalogCacheVariables = cloneEventNamedValues(variables)
	eventConditionCatalogCacheValid = true
	return cloneEventNamedValues(switches), cloneEventNamedValues(variables)
}

func saveEventConditionCatalog(switches, variables []eventNamedValue) error {
	path := eventConditionCatalogPath()
	if path == "" {
		return fmt.Errorf("nessun progetto aperto")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	doc := eventConditionCatalogFile{Version: 1, Switches: normalizeEventNamedValues(switches), Variables: normalizeEventNamedValues(variables)}
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	_ = os.Remove(path)
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	eventConditionCatalogCacheProject = strings.ToLower(filepath.Clean(currentProject))
	eventConditionCatalogCacheSwitches = cloneEventNamedValues(doc.Switches)
	eventConditionCatalogCacheVariables = cloneEventNamedValues(doc.Variables)
	eventConditionCatalogCacheValid = true
	return nil
}

func eventNamedValueByID(values []eventNamedValue, id int) (eventNamedValue, bool) {
	for _, v := range values {
		if v.ID == id {
			return v, true
		}
	}
	return eventNamedValue{}, false
}

func eventNamedValueByName(values []eventNamedValue, name string) (eventNamedValue, bool) {
	name = strings.TrimSpace(name)
	for _, v := range values {
		if name != "" && strings.EqualFold(strings.TrimSpace(v.Name), name) {
			return v, true
		}
	}
	return eventNamedValue{}, false
}

func nextEventNamedValueID(values []eventNamedValue) int {
	maxID := 0
	for _, v := range values {
		if v.ID > maxID {
			maxID = v.ID
		}
	}
	return maxID + 1
}

func upsertEventNamedValue(values []eventNamedValue, id int, name string) []eventNamedValue {
	name = strings.TrimSpace(name)
	if id <= 0 {
		return normalizeEventNamedValues(values)
	}
	found := false
	for i := range values {
		if values[i].ID == id {
			values[i].Name = name
			found = true
			break
		}
	}
	if !found {
		values = append(values, eventNamedValue{ID: id, Name: name})
	}
	return normalizeEventNamedValues(values)
}

func eventConditionDisplayName(id int, name string, kind string) string {
	name = strings.TrimSpace(name)
	if id <= 0 {
		return "Non impostato"
	}
	if name == "" {
		name = "(senza nome)"
	}
	return fmt.Sprintf("%04d: %s", id, name)
}

func eventConditionPickerSelectedIndex() int {
	if eventConditionPickerList == 0 {
		return -1
	}
	r, _, _ := pSendMessageW.Call(uintptr(eventConditionPickerList), LB_GETCURSEL, 0, 0)
	if int32(r) < 0 {
		return -1
	}
	return int(r)
}

func refreshEventConditionPickerPreview() {
	if eventConditionPickerPreview == 0 {
		return
	}
	name := strings.TrimSpace(getText(eventConditionPickerName))
	if name == "" {
		name = "condizione selezionata"
	}
	if eventConditionPickerModeValue == conditionPickerSwitch {
		state := "ON"
		if comboSel(eventConditionPickerState) == 1 {
			state = "OFF"
		}
		setText(eventConditionPickerPreview, fmt.Sprintf("Anteprima: la pagina è attiva quando %s è %s.", name, state))
		return
	}
	threshold := intField(eventConditionPickerThreshold, 0)
	previewValue := intField(eventConditionPickerPreviewVal, 0)
	result := "FALSA"
	if previewValue >= threshold {
		result = "VERA"
	}
	setText(eventConditionPickerPreview, fmt.Sprintf("Anteprima: %s = %d  →  %d >= %d  →  condizione %s", name, previewValue, previewValue, threshold, result))
}

func selectEventConditionPickerItem(index int) {
	if index < 0 || index >= len(eventConditionPickerItems) {
		return
	}
	v := eventConditionPickerItems[index]
	eventConditionPickerCurrentID = v.ID
	setText(eventConditionPickerName, v.Name)
	refreshEventConditionPickerPreview()
}

func commitEventConditionPicker() bool {
	name := strings.TrimSpace(getText(eventConditionPickerName))
	selected := eventConditionPickerSelectedIndex()
	id := eventConditionPickerCurrentID
	if selected >= 0 && selected < len(eventConditionPickerItems) {
		id = eventConditionPickerItems[selected].ID
	}
	if name == "" {
		msgbox("PML Studio - Condizione evento", "Inserisci un nome oppure seleziona una voce già esistente.", MB_OK|MB_ICONINFORMATION)
		return false
	}
	if id <= 0 {
		if existing, ok := eventNamedValueByName(eventConditionPickerItems, name); ok {
			id = existing.ID
		} else {
			id = nextEventNamedValueID(eventConditionPickerItems)
		}
	}

	switches, variables := loadEventConditionCatalog()
	if eventConditionPickerModeValue == conditionPickerSwitch {
		switches = upsertEventNamedValue(switches, id, name)
		eventConditionPickerResultOn = comboSel(eventConditionPickerState) != 1
	} else {
		variables = upsertEventNamedValue(variables, id, name)
		eventConditionPickerResultValue = intField(eventConditionPickerThreshold, 0)
	}
	if err := saveEventConditionCatalog(switches, variables); err != nil {
		msgbox("PML Studio - Condizione evento", "Impossibile salvare il catalogo interruttori/variabili:\r\n\r\n"+err.Error(), MB_OK|MB_ICONERROR)
		return false
	}
	eventConditionPickerResultID = id
	eventConditionPickerResultName = name
	eventConditionPickerAccepted = true
	return true
}

func eventConditionPickerWndProcFn(hwnd syscall.Handle, msg uint32, w, l uintptr) uintptr {
	switch msg {
	case WM_COMMAND:
		id := int(loword(w))
		notify := int(hiword(w))
		if id == idCondList && notify == LBN_SELCHANGE {
			selectEventConditionPickerItem(eventConditionPickerSelectedIndex())
			return 0
		}
		if id == idCondList && notify == LBN_DBLCLK {
			selectEventConditionPickerItem(eventConditionPickerSelectedIndex())
			if commitEventConditionPicker() {
				pDestroyWindow.Call(uintptr(hwnd))
			}
			return 0
		}
		if (id == idCondName || id == idCondThreshold || id == idCondPreviewVal) && notify == 0x0300 { // EN_CHANGE
			refreshEventConditionPickerPreview()
			return 0
		}
		if id == idCondState && notify == 1 { // CBN_SELCHANGE
			refreshEventConditionPickerPreview()
			return 0
		}
		switch id {
		case idCondOK:
			if commitEventConditionPicker() {
				pDestroyWindow.Call(uintptr(hwnd))
			}
			return 0
		case idCondCancel:
			pDestroyWindow.Call(uintptr(hwnd))
			return 0
		}
	case WM_CLOSE:
		pDestroyWindow.Call(uintptr(hwnd))
		return 0
	case WM_DESTROY:
		eventConditionPickerOpen = false
		eventConditionPickerWindow = 0
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(uintptr(hwnd), uintptr(msg), w, l)
	return r
}

var eventConditionPickerWndProc = syscall.NewCallback(eventConditionPickerWndProcFn)

func ensureEventConditionPickerClass() error {
	if eventConditionPickerRegistered {
		return nil
	}
	h, _, _ := pGetModuleHandleW.Call(0)
	hInst := syscall.Handle(h)
	cursor, _, _ := pLoadCursorW.Call(0, IDC_ARROW)
	brush, _, _ := pCreateSolidBrush.Call(rgb(244, 244, 244))
	wc := WNDCLASSEX{cbSize: uint32(unsafe.Sizeof(WNDCLASSEX{})), lpfnWndProc: eventConditionPickerWndProc, hInstance: hInst, hCursor: syscall.Handle(cursor), hbrBackground: syscall.Handle(brush), lpszClassName: wstr(eventConditionPickerClassName)}
	r, _, err := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	if r == 0 {
		return fmt.Errorf("registrazione selettore condizioni fallita: %v", err)
	}
	eventConditionPickerRegistered = true
	return nil
}

func showEventConditionPicker(owner syscall.Handle, mode eventConditionPickerMode, currentID int, currentName string, currentOn bool, currentValue int) (int, string, bool, int, bool) {
	if eventConditionPickerOpen {
		return 0, "", true, 0, false
	}
	if err := ensureEventConditionPickerClass(); err != nil {
		msgbox("PML Studio - Condizione evento", err.Error(), MB_OK|MB_ICONERROR)
		return 0, "", true, 0, false
	}
	switches, variables := loadEventConditionCatalog()
	items := switches
	title := "Seleziona interruttore"
	if mode == conditionPickerVariable {
		items = variables
		title = "Seleziona variabile"
	}
	if currentID > 0 {
		if _, ok := eventNamedValueByID(items, currentID); !ok {
			items = append(items, eventNamedValue{ID: currentID, Name: strings.TrimSpace(currentName)})
			items = normalizeEventNamedValues(items)
		}
	}

	const ww, wh int32 = 920, 610
	x, y := centerOwnedWindow(ww, wh)
	h, _, _ := pGetModuleHandleW.Call(0)
	hInst := syscall.Handle(h)
	eventConditionPickerModeValue = mode
	eventConditionPickerItems = items
	eventConditionPickerCurrentID = currentID
	eventConditionPickerAccepted = false
	eventConditionPickerResultID = 0
	eventConditionPickerResultName = ""
	eventConditionPickerResultOn = currentOn
	eventConditionPickerResultValue = currentValue
	eventConditionPickerOwner = owner
	eventConditionPickerOpen = true
	eventConditionPickerWindow = createWindow(eventConditionPickerClassName, title, WS_OVERLAPPEDWINDOW|WS_VISIBLE|WS_CLIPCHILDREN, x, y, ww, wh, owner, 0, hInst)
	if eventConditionPickerWindow == 0 {
		eventConditionPickerOpen = false
		return 0, "", true, 0, false
	}
	setWindowIcon(eventConditionPickerWindow)

	createWindow("STATIC", "Esistenti nel progetto:", WS_CHILD|WS_VISIBLE, 18, 18, 370, 24, eventConditionPickerWindow, 7410, hInst)
	eventConditionPickerList = createWindow("LISTBOX", "", WS_CHILD|WS_VISIBLE|WS_BORDER|WS_VSCROLL|WS_TABSTOP|LBS_NOTIFY|LBS_NOINTEGRALHEIGHT, 18, 45, 390, 485, eventConditionPickerWindow, idCondList, hInst)
	for i, v := range items {
		label := eventConditionDisplayName(v.ID, v.Name, "")
		addList(eventConditionPickerList, label)
		if v.ID == currentID {
			pSendMessageW.Call(uintptr(eventConditionPickerList), LB_SETCURSEL, uintptr(i), 0)
		}
	}

	label := "Nome interruttore:"
	if mode == conditionPickerVariable {
		label = "Nome variabile:"
	}
	createWindow("STATIC", label, WS_CHILD|WS_VISIBLE, 445, 45, 410, 24, eventConditionPickerWindow, 7411, hInst)
	eventConditionPickerName = createWindow("EDIT", strings.TrimSpace(currentName), WS_CHILD|WS_VISIBLE|WS_BORDER|WS_TABSTOP, 445, 73, 410, 32, eventConditionPickerWindow, idCondName, hInst)

	if mode == conditionPickerSwitch {
		createWindow("STATIC", "Stato richiesto:", WS_CHILD|WS_VISIBLE, 445, 125, 260, 24, eventConditionPickerWindow, 7412, hInst)
		eventConditionPickerState = createWindow("COMBOBOX", "", WS_CHILD|WS_VISIBLE|WS_TABSTOP|CBS_DROPDOWNLIST, 445, 153, 240, 120, eventConditionPickerWindow, idCondState, hInst)
		setComboFromStrings(eventConditionPickerState, []string{"ON", "OFF"}, 0)
		if !currentOn {
			comboSelectIndex(eventConditionPickerState, 1)
		}
		eventConditionPickerThreshold = 0
		eventConditionPickerPreviewVal = 0
	} else {
		eventConditionPickerState = 0
		createWindow("STATIC", "Valore minimo per attivare la pagina:", WS_CHILD|WS_VISIBLE, 445, 125, 410, 24, eventConditionPickerWindow, 7413, hInst)
		eventConditionPickerThreshold = createWindow("EDIT", strconv.Itoa(currentValue), WS_CHILD|WS_VISIBLE|WS_BORDER|WS_TABSTOP, 445, 153, 180, 32, eventConditionPickerWindow, idCondThreshold, hInst)
		createWindow("STATIC", "Prova valore variabile:", WS_CHILD|WS_VISIBLE, 445, 205, 350, 24, eventConditionPickerWindow, 7414, hInst)
		eventConditionPickerPreviewVal = createWindow("EDIT", strconv.Itoa(currentValue), WS_CHILD|WS_VISIBLE|WS_BORDER|WS_TABSTOP, 445, 233, 180, 32, eventConditionPickerWindow, idCondPreviewVal, hInst)
	}

	eventConditionPickerPreview = createWindow("STATIC", "", WS_CHILD|WS_VISIBLE, 445, 300, 410, 96, eventConditionPickerWindow, 7415, hInst)
	createWindow("STATIC", "Puoi selezionare una voce esistente oppure scrivere un nuovo nome. Un nuovo nome riceve automaticamente il prossimo ID libero.", WS_CHILD|WS_VISIBLE, 445, 410, 410, 72, eventConditionPickerWindow, 7416, hInst)
	createWindow("BUTTON", "Conferma", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 645, 520, 100, 34, eventConditionPickerWindow, idCondOK, hInst)
	createWindow("BUTTON", "Annulla", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 755, 520, 100, 34, eventConditionPickerWindow, idCondCancel, hInst)

	if currentID > 0 && strings.TrimSpace(currentName) == "" {
		if v, ok := eventNamedValueByID(items, currentID); ok {
			setText(eventConditionPickerName, v.Name)
		}
	}
	refreshEventConditionPickerPreview()
	pEnableWindow.Call(uintptr(owner), 0)
	pShowWindow.Call(uintptr(eventConditionPickerWindow), SW_SHOW)
	pUpdateWindow.Call(uintptr(eventConditionPickerWindow))

	var m MSG
	repostQuit := false
	for eventConditionPickerOpen {
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
	return eventConditionPickerResultID, eventConditionPickerResultName, eventConditionPickerResultOn, eventConditionPickerResultValue, eventConditionPickerAccepted
}

func projectSwitchName(id int) string {
	if id <= 0 {
		return ""
	}
	switches, _ := loadEventConditionCatalog()
	if v, ok := eventNamedValueByID(switches, id); ok {
		return v.Name
	}
	return ""
}

func projectVariableName(id int) string {
	if id <= 0 {
		return ""
	}
	_, variables := loadEventConditionCatalog()
	if v, ok := eventNamedValueByID(variables, id); ok {
		return v.Name
	}
	return ""
}
