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
	"unicode"
	"unsafe"
)

type legendaryAreaEntry struct {
	Species string `json:"species"`
	Level   int    `json:"level"`
	Weight  int    `json:"weight,omitempty"`
}

type legendaryAreaDefinition struct {
	ID            string               `json:"id"`
	Name          string               `json:"name"`
	MapIDs        []int                `json:"map_ids"`
	ChancePercent int                  `json:"chance_percent"`
	Entries       []legendaryAreaEntry `json:"entries"`
}

type legendaryAreaRegistry struct {
	Version       int                       `json:"version"`
	UniqueSpecies bool                      `json:"unique_species"`
	Areas         []legendaryAreaDefinition `json:"areas"`
}

func legendaryAreaRegistryPath() string {
	if strings.TrimSpace(currentProject) == "" {
		return ""
	}
	return filepath.Join(currentProject, "converted", "data", "plm_legendary_areas.json")
}

func loadLegendaryAreaRegistry() legendaryAreaRegistry {
	reg := legendaryAreaRegistry{Version: 1, UniqueSpecies: true, Areas: []legendaryAreaDefinition{}}
	path := legendaryAreaRegistryPath()
	if path == "" {
		return reg
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return reg
	}
	var disk legendaryAreaRegistry
	if json.Unmarshal(data, &disk) != nil {
		return reg
	}
	if disk.Version <= 0 {
		disk.Version = 1
	}
	// Legendary species are unique by design: resolving one removes it from
	// every area that happens to reference the same species.
	disk.UniqueSpecies = true
	if disk.Areas == nil {
		disk.Areas = []legendaryAreaDefinition{}
	}
	for i := range disk.Areas {
		if disk.Areas[i].ChancePercent < 1 {
			disk.Areas[i].ChancePercent = 1
		}
		if disk.Areas[i].ChancePercent > 100 {
			disk.Areas[i].ChancePercent = 100
		}
		for j := range disk.Areas[i].Entries {
			disk.Areas[i].Entries[j].Species = strings.ToUpper(strings.TrimSpace(disk.Areas[i].Entries[j].Species))
			if disk.Areas[i].Entries[j].Level < 1 {
				disk.Areas[i].Entries[j].Level = 1
			}
			if disk.Areas[i].Entries[j].Weight < 1 {
				disk.Areas[i].Entries[j].Weight = 1
			}
		}
	}
	return disk
}

func saveLegendaryAreaRegistry(reg legendaryAreaRegistry) error {
	path := legendaryAreaRegistryPath()
	if path == "" {
		return fmt.Errorf("progetto non aperto")
	}
	reg.Version = 1
	reg.UniqueSpecies = true
	sort.SliceStable(reg.Areas, func(i, j int) bool {
		return strings.ToLower(reg.Areas[i].Name) < strings.ToLower(reg.Areas[j].Name)
	})
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(reg, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func legendaryAreaSlug(v string) string {
	v = strings.TrimSpace(v)
	var b strings.Builder
	lastUnderscore := false
	for _, r := range v {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(unicode.ToUpper(r))
			lastUnderscore = false
			continue
		}
		if !lastUnderscore && b.Len() > 0 {
			b.WriteByte('_')
			lastUnderscore = true
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		out = "AREA_LEGGENDARIA"
	}
	return "LEGENDARY_AREA_" + out
}

func parseLegendaryAreaMapIDs(text string) ([]int, error) {
	text = strings.NewReplacer(";", ",", "\r", ",", "\n", ",", " ", ",").Replace(text)
	seen := map[int]bool{}
	out := []int{}
	for _, part := range strings.Split(text, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		id, err := strconv.Atoi(part)
		if err != nil || id <= 0 {
			return nil, fmt.Errorf("Map ID non valido: %q", part)
		}
		exists := false
		for i := range maps {
			if maps[i].ID == id {
				exists = true
				break
			}
		}
		if currentMap != nil && currentMap.ID == id {
			exists = true
		}
		if !exists {
			return nil, fmt.Errorf("Map%03d non esiste nel progetto", id)
		}
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("inserisci almeno una mappa per l'area")
	}
	sort.Ints(out)
	return out, nil
}

func legendaryAreaForCurrentMap(reg legendaryAreaRegistry) (legendaryAreaDefinition, bool) {
	if currentMap == nil {
		return legendaryAreaDefinition{}, false
	}
	for _, area := range reg.Areas {
		for _, id := range area.MapIDs {
			if id == currentMap.ID {
				return area, true
			}
		}
	}
	return legendaryAreaDefinition{}, false
}

const (
	legendaryAreaDialogClassName = "PLMStudioLegendaryAreaDialog01"
	idLegendAreaName             = 8800
	idLegendAreaMaps             = 8801
	idLegendAreaChance           = 8802
	idLegendAreaList             = 8803
	idLegendAreaAdd              = 8804
	idLegendAreaRemove           = 8805
	idLegendAreaSave             = 8806
	idLegendAreaDelete           = 8807
	idLegendAreaCancel           = 8808
)

var (
	legendaryAreaDialogRegistered bool
	legendaryAreaDialogOpen       bool
	legendaryAreaDialogWindow     syscall.Handle
	legendaryAreaNameEdit         syscall.Handle
	legendaryAreaMapsEdit         syscall.Handle
	legendaryAreaChanceEdit       syscall.Handle
	legendaryAreaList             syscall.Handle
	legendaryAreaEntries          []legendaryAreaEntry
	legendaryAreaEditingID        string
	legendaryAreaAccepted         bool
)

func refreshLegendaryAreaEntryList() {
	if legendaryAreaList == 0 {
		return
	}
	pSendMessageW.Call(uintptr(legendaryAreaList), LB_RESETCONTENT, 0, 0)
	for _, entry := range legendaryAreaEntries {
		label := fmt.Sprintf("%s  ·  Lv.%d", speciesDisplayName(entry.Species), entry.Level)
		addList(legendaryAreaList, label)
	}
	if len(legendaryAreaEntries) > 0 {
		pSendMessageW.Call(uintptr(legendaryAreaList), LB_SETCURSEL, 0, 0)
	}
}

func legendaryAreaSelectedEntryIndex() int {
	if legendaryAreaList == 0 {
		return -1
	}
	r, _, _ := pSendMessageW.Call(uintptr(legendaryAreaList), LB_GETCURSEL, 0, 0)
	idx := int(int32(r))
	if idx < 0 || idx >= len(legendaryAreaEntries) {
		return -1
	}
	return idx
}

func legendaryAreaMapText(ids []int) string {
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		parts = append(parts, strconv.Itoa(id))
	}
	return strings.Join(parts, ", ")
}

func saveLegendaryAreaFromDialog() bool {
	name := strings.TrimSpace(getText(legendaryAreaNameEdit))
	if name == "" {
		msgbox("PML Studio - Area leggendaria", "Inserisci un nome per l'area.", MB_OK|MB_ICONINFORMATION)
		return false
	}
	mapIDs, err := parseLegendaryAreaMapIDs(getText(legendaryAreaMapsEdit))
	if err != nil {
		msgbox("PML Studio - Area leggendaria", err.Error(), MB_OK|MB_ICONINFORMATION)
		return false
	}
	chance, err := strconv.Atoi(strings.TrimSpace(getText(legendaryAreaChanceEdit)))
	if err != nil || chance < 1 || chance > 100 {
		msgbox("PML Studio - Area leggendaria", "La probabilità deve essere compresa tra 1 e 100.", MB_OK|MB_ICONINFORMATION)
		return false
	}
	if len(legendaryAreaEntries) == 0 {
		msgbox("PML Studio - Area leggendaria", "Aggiungi almeno un Pokémon leggendario al pool.", MB_OK|MB_ICONINFORMATION)
		return false
	}

	// Final anti-duplication validation. This runs again at save time so a
	// copied/imported registry cannot bypass the check performed by the Add button.
	seenSpecies := map[string]bool{}
	for _, entry := range legendaryAreaEntries {
		species := strings.ToUpper(strings.TrimSpace(entry.Species))
		if species == "" {
			continue
		}
		if seenSpecies[species] {
			msgbox("PML Studio - Leggendario duplicato", fmt.Sprintf("%s compare più di una volta nello stesso pool.\r\n\r\nRimuovi il duplicato prima di salvare.", speciesDisplayName(species)), MB_OK|MB_ICONINFORMATION)
			return false
		}
		seenSpecies[species] = true
		for _, current := range currentEditorLegendarySpecies() {
			if strings.EqualFold(current, species) {
				msgbox("PML Studio - Leggendario duplicato", fmt.Sprintf("%s è già configurato come leggendario nell'evento attualmente aperto.\r\n\r\nPLM Studio non permette una seconda configurazione della stessa specie.", speciesDisplayName(species)), MB_OK|MB_ICONINFORMATION)
				return false
			}
		}
		if usage, duplicate := findLegendaryDuplicate(species, strings.TrimSpace(legendaryAreaEditingID), false); duplicate {
			msgbox("PML Studio - Leggendario duplicato", legendaryDuplicateMessage(species, usage), MB_OK|MB_ICONINFORMATION)
			return false
		}
	}

	areaID := strings.TrimSpace(legendaryAreaEditingID)
	if areaID == "" {
		areaID = legendaryAreaSlug(name)
	}
	area := legendaryAreaDefinition{ID: areaID, Name: name, MapIDs: mapIDs, ChancePercent: chance, Entries: append([]legendaryAreaEntry(nil), legendaryAreaEntries...)}
	reg := loadLegendaryAreaRegistry()
	replaced := false
	for i := range reg.Areas {
		if strings.EqualFold(reg.Areas[i].ID, areaID) {
			reg.Areas[i] = area
			replaced = true
			break
		}
	}
	if !replaced {
		// Avoid silently creating two different areas with the same generated ID.
		for _, existing := range reg.Areas {
			if strings.EqualFold(existing.ID, area.ID) {
				msgbox("PML Studio - Area leggendaria", "Esiste già un'area con questo identificatore. Cambia il nome dell'area.", MB_OK|MB_ICONINFORMATION)
				return false
			}
		}
		reg.Areas = append(reg.Areas, area)
	}
	if err := saveLegendaryAreaRegistry(reg); err != nil {
		msgbox("PML Studio - Area leggendaria", "Salvataggio non riuscito:\r\n\r\n"+err.Error(), MB_OK|MB_ICONERROR)
		return false
	}
	legendaryAreaEditingID = area.ID
	setToolbarStatus(fmt.Sprintf("Area leggendaria salvata: %s · %d mappe · %d Pokémon.", area.Name, len(area.MapIDs), len(area.Entries)))
	return true
}

func deleteLegendaryAreaFromDialog() bool {
	id := strings.TrimSpace(legendaryAreaEditingID)
	if id == "" {
		return false
	}
	reg := loadLegendaryAreaRegistry()
	out := make([]legendaryAreaDefinition, 0, len(reg.Areas))
	removed := false
	for _, area := range reg.Areas {
		if strings.EqualFold(area.ID, id) {
			removed = true
			continue
		}
		out = append(out, area)
	}
	if !removed {
		return false
	}
	reg.Areas = out
	if err := saveLegendaryAreaRegistry(reg); err != nil {
		msgbox("PML Studio - Area leggendaria", "Eliminazione non riuscita:\r\n\r\n"+err.Error(), MB_OK|MB_ICONERROR)
		return false
	}
	setToolbarStatus("Area leggendaria eliminata.")
	return true
}

func legendaryAreaDialogWndProcFn(hwnd syscall.Handle, msg uint32, w, l uintptr) uintptr {
	switch msg {
	case WM_COMMAND:
		switch int(loword(w)) {
		case idLegendAreaAdd:
			species, level, _, ok := showEventCommandPickerDialog(hwnd, eventCommandFixedPokemon)
			if !ok {
				return 0
			}
			species = strings.ToUpper(strings.TrimSpace(species))
			if species == "" {
				return 0
			}
			for i := range legendaryAreaEntries {
				if strings.EqualFold(legendaryAreaEntries[i].Species, species) {
					legendaryAreaEntries[i].Level = maxIntEventCmd(level, 1)
					refreshLegendaryAreaEntryList()
					return 0
				}
			}
			for _, current := range currentEditorLegendarySpecies() {
				if strings.EqualFold(current, species) {
					msgbox("PML Studio - Leggendario duplicato", fmt.Sprintf("%s è già configurato come leggendario nell'evento attualmente aperto.\r\n\r\nNon può essere aggiunto anche a un'area random.", speciesDisplayName(species)), MB_OK|MB_ICONINFORMATION)
					return 0
				}
			}
			if usage, duplicate := findLegendaryDuplicate(species, strings.TrimSpace(legendaryAreaEditingID), false); duplicate {
				msgbox("PML Studio - Leggendario duplicato", legendaryDuplicateMessage(species, usage), MB_OK|MB_ICONINFORMATION)
				return 0
			}
			legendaryAreaEntries = append(legendaryAreaEntries, legendaryAreaEntry{Species: species, Level: maxIntEventCmd(level, 1), Weight: 1})
			refreshLegendaryAreaEntryList()
			return 0
		case idLegendAreaRemove:
			idx := legendaryAreaSelectedEntryIndex()
			if idx >= 0 {
				legendaryAreaEntries = append(legendaryAreaEntries[:idx], legendaryAreaEntries[idx+1:]...)
				refreshLegendaryAreaEntryList()
			}
			return 0
		case idLegendAreaSave:
			if saveLegendaryAreaFromDialog() {
				legendaryAreaAccepted = true
				pDestroyWindow.Call(uintptr(hwnd))
			}
			return 0
		case idLegendAreaDelete:
			if strings.TrimSpace(legendaryAreaEditingID) == "" {
				msgbox("PML Studio - Area leggendaria", "Questa è una nuova area e non è ancora stata salvata.", MB_OK|MB_ICONINFORMATION)
				return 0
			}
			if deleteLegendaryAreaFromDialog() {
				legendaryAreaAccepted = true
				pDestroyWindow.Call(uintptr(hwnd))
			}
			return 0
		case idLegendAreaCancel:
			pDestroyWindow.Call(uintptr(hwnd))
			return 0
		}
	case WM_CLOSE:
		pDestroyWindow.Call(uintptr(hwnd))
		return 0
	case WM_DESTROY:
		legendaryAreaDialogOpen = false
		legendaryAreaDialogWindow = 0
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(uintptr(hwnd), uintptr(msg), w, l)
	return r
}

var legendaryAreaDialogWndProc = syscall.NewCallback(legendaryAreaDialogWndProcFn)

func ensureLegendaryAreaDialogClass() error {
	if legendaryAreaDialogRegistered {
		return nil
	}
	h, _, _ := pGetModuleHandleW.Call(0)
	hInst := syscall.Handle(h)
	cursor, _, _ := pLoadCursorW.Call(0, IDC_ARROW)
	brush, _, _ := pCreateSolidBrush.Call(rgb(244, 244, 244))
	wc := WNDCLASSEX{
		cbSize:        uint32(unsafe.Sizeof(WNDCLASSEX{})),
		lpfnWndProc:   legendaryAreaDialogWndProc,
		hInstance:     hInst,
		hCursor:       syscall.Handle(cursor),
		hbrBackground: syscall.Handle(brush),
		lpszClassName: wstr(legendaryAreaDialogClassName),
	}
	r, _, err := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	if r == 0 {
		return fmt.Errorf("registrazione finestra Area leggendaria fallita: %v", err)
	}
	legendaryAreaDialogRegistered = true
	return nil
}

func showLegendaryAreaDialog(owner syscall.Handle) bool {
	if currentMap == nil || strings.TrimSpace(currentProject) == "" {
		msgbox("PML Studio - Area leggendaria", "Apri prima un progetto e una mappa.", MB_OK|MB_ICONINFORMATION)
		return false
	}
	if legendaryAreaDialogOpen {
		return false
	}
	if err := ensureLegendaryAreaDialogClass(); err != nil {
		msgbox("PML Studio - Area leggendaria", err.Error(), MB_OK|MB_ICONERROR)
		return false
	}

	reg := loadLegendaryAreaRegistry()
	existing, hasExisting := legendaryAreaForCurrentMap(reg)
	if hasExisting {
		legendaryAreaEditingID = existing.ID
		legendaryAreaEntries = append([]legendaryAreaEntry(nil), existing.Entries...)
	} else {
		legendaryAreaEditingID = ""
		legendaryAreaEntries = nil
	}

	const ww, wh int32 = 820, 650
	x, y := centerOwnedWindow(ww, wh)
	h, _, _ := pGetModuleHandleW.Call(0)
	hi := syscall.Handle(h)
	legendaryAreaAccepted = false
	legendaryAreaDialogOpen = true
	legendaryAreaDialogWindow = createWindow(legendaryAreaDialogClassName, "Leggendari random / area", WS_OVERLAPPEDWINDOW|WS_VISIBLE|WS_CLIPCHILDREN, x, y, ww, wh, owner, 0, hi)
	if legendaryAreaDialogWindow == 0 {
		legendaryAreaDialogOpen = false
		return false
	}
	setWindowIcon(legendaryAreaDialogWindow)

	createWindow("STATIC", "Configura un pool di leggendari casuali per una o più mappe. Cattura o sconfitta lo rimuovono definitivamente; fuga/perdita no.", WS_CHILD|WS_VISIBLE, 24, 20, 745, 42, legendaryAreaDialogWindow, 8810, hi)
	createWindow("STATIC", "Nome area:", WS_CHILD|WS_VISIBLE, 24, 80, 110, 22, legendaryAreaDialogWindow, 8811, hi)
	initialName := "Area " + currentMap.Name
	initialMaps := strconv.Itoa(currentMap.ID)
	initialChance := "5"
	if hasExisting {
		initialName = existing.Name
		initialMaps = legendaryAreaMapText(existing.MapIDs)
		initialChance = strconv.Itoa(existing.ChancePercent)
	}
	legendaryAreaNameEdit = createWindow("EDIT", initialName, WS_CHILD|WS_VISIBLE|WS_BORDER|WS_TABSTOP, 140, 76, 610, 28, legendaryAreaDialogWindow, idLegendAreaName, hi)

	createWindow("STATIC", "Map ID area:", WS_CHILD|WS_VISIBLE, 24, 122, 110, 22, legendaryAreaDialogWindow, 8812, hi)
	legendaryAreaMapsEdit = createWindow("EDIT", initialMaps, WS_CHILD|WS_VISIBLE|WS_BORDER|WS_TABSTOP, 140, 118, 610, 28, legendaryAreaDialogWindow, idLegendAreaMaps, hi)
	createWindow("STATIC", "Puoi inserire più mappe separate da virgola, es. 45, 46, 52.", WS_CHILD|WS_VISIBLE, 140, 150, 610, 20, legendaryAreaDialogWindow, 8813, hi)

	createWindow("STATIC", "Probabilità:", WS_CHILD|WS_VISIBLE, 24, 184, 110, 22, legendaryAreaDialogWindow, 8814, hi)
	legendaryAreaChanceEdit = createWindow("EDIT", initialChance, WS_CHILD|WS_VISIBLE|WS_BORDER|WS_TABSTOP, 140, 180, 70, 28, legendaryAreaDialogWindow, idLegendAreaChance, hi)
	createWindow("STATIC", "% per controllo incontro nell'area", WS_CHILD|WS_VISIBLE, 218, 184, 260, 22, legendaryAreaDialogWindow, 8815, hi)

	createWindow("BUTTON", "Pool leggendari", WS_CHILD|WS_VISIBLE|BS_GROUPBOX, 24, 230, 726, 292, legendaryAreaDialogWindow, 8816, hi)
	legendaryAreaList = createWindow("LISTBOX", "", WS_CHILD|WS_VISIBLE|WS_BORDER|WS_VSCROLL|WS_TABSTOP|LBS_NOTIFY|LBS_NOINTEGRALHEIGHT, 44, 263, 500, 225, legendaryAreaDialogWindow, idLegendAreaList, hi)
	createWindow("BUTTON", "+ Aggiungi Pokémon", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 566, 276, 160, 38, legendaryAreaDialogWindow, idLegendAreaAdd, hi)
	createWindow("BUTTON", "Rimuovi", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 566, 326, 160, 38, legendaryAreaDialogWindow, idLegendAreaRemove, hi)
	createWindow("STATIC", "Ogni specie è unica nel salvataggio: se viene catturata o sconfitta non può comparire di nuovo, nemmeno se presente in un'altra area. Se fuggi o perdi, resta disponibile.", WS_CHILD|WS_VISIBLE, 566, 386, 160, 92, legendaryAreaDialogWindow, 8817, hi)
	refreshLegendaryAreaEntryList()

	createWindow("BUTTON", "Elimina area", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 24, 552, 130, 38, legendaryAreaDialogWindow, idLegendAreaDelete, hi)
	createWindow("BUTTON", "Salva area", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 506, 552, 118, 38, legendaryAreaDialogWindow, idLegendAreaSave, hi)
	createWindow("BUTTON", "Annulla", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 632, 552, 118, 38, legendaryAreaDialogWindow, idLegendAreaCancel, hi)

	pEnableWindow.Call(uintptr(owner), 0)
	pShowWindow.Call(uintptr(legendaryAreaDialogWindow), SW_SHOW)
	pUpdateWindow.Call(uintptr(legendaryAreaDialogWindow))
	var m MSG
	repostQuit := false
	for legendaryAreaDialogOpen {
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
	return legendaryAreaAccepted
}

func addLegendaryAreaCommand() {
	if showLegendaryAreaDialog(eventEditorWindow) {
		setToolbarStatus("Area leggendaria aggiornata. Il runtime userà il pool sulle mappe configurate.")
	}
}
