//go:build windows

package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
)

const (
	idEncounterMethod     = 1710
	idEncounterMethodRate = 1711
	idEncounterMin        = 1712
	idEncounterMax        = 1713
	idEncounterSave       = 1714
	idEncounterApply      = 1715
	idEncounterSearch     = 1716
	idEncounterAvailable  = 1717
	idEncounterAssigned   = 1718
	idEncounterAdd        = 1719
	idEncounterRemove     = 1720
	idEncounterWeight     = 1721

	encounterSchema = "pml.map_encounters.v1"
)

const (
	lbnSelChange = 1
	lbnDblClk    = 2
	cbnSelChange = 1
	enChange     = 0x0300
)

type EncounterSpecies struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type EncounterSlot struct {
	Species  string `json:"species"`
	Name     string `json:"name,omitempty"`
	Weight   int    `json:"weight"`
	MinLevel int    `json:"min_level"`
	MaxLevel int    `json:"max_level"`
}

type EncounterMethodData struct {
	Label string          `json:"label,omitempty"`
	Rate  int             `json:"rate"`
	Slots []EncounterSlot `json:"slots"`
}

type EncounterMapDocument struct {
	Version int                             `json:"version"`
	Schema  string                          `json:"schema"`
	MapID   int                             `json:"map_id"`
	Methods map[string]*EncounterMethodData `json:"methods"`
}

type encounterMethodDef struct {
	ID    string
	Label string
}

var encounterMethodDefs = []encounterMethodDef{
	{ID: "land", Label: "Erba alta"},
	{ID: "land_morning", Label: "Erba alta - Mattina"},
	{ID: "land_day", Label: "Erba alta - Giorno"},
	{ID: "land_night", Label: "Erba alta - Notte"},
	{ID: "cave", Label: "Grotta"},
	{ID: "water", Label: "Acqua / Surf"},
	{ID: "old_rod", Label: "Pesca - Amo vecchio"},
	{ID: "good_rod", Label: "Pesca - Amo buono"},
	{ID: "super_rod", Label: "Pesca - Super amo"},
	{ID: "rock_smash", Label: "Spaccaroccia"},
	{ID: "headbutt_low", Label: "Headbutt - Basso"},
	{ID: "headbutt_high", Label: "Headbutt - Alto"},
	{ID: "bug_contest", Label: "Gara Pigliamosche"},
	{ID: "special", Label: "Eventi speciali"},
}

var encounterPBSMethodAliases = map[string]string{
	"land":         "land",
	"landmorning":  "land_morning",
	"landday":      "land_day",
	"landnight":    "land_night",
	"cave":         "cave",
	"water":        "water",
	"oldrod":       "old_rod",
	"goodrod":      "good_rod",
	"superrod":     "super_rod",
	"rocksmash":    "rock_smash",
	"headbuttlow":  "headbutt_low",
	"headbutthigh": "headbutt_high",
	"bugcontest":   "bug_contest",
	"special":      "special",
}

var (
	// Tutti i controlli sono figli diretti del pannello principale: in questo
	// modo non si sovrappongono e il layout puo' essere ridimensionato in modo
	// prevedibile anche con finestre strette.
	hwndEncounterMapLabel       syscall.Handle
	hwndEncounterMethodLabel    syscall.Handle
	hwndEncounterMethodCombo    syscall.Handle
	hwndEncounterMethodRateLbl  syscall.Handle
	hwndEncounterSearchLabel    syscall.Handle
	hwndEncounterSearch         syscall.Handle
	hwndEncounterAvailableLabel syscall.Handle
	hwndEncounterAvailable      syscall.Handle
	hwndEncounterAssignedLabel  syscall.Handle
	hwndEncounterAssigned       syscall.Handle
	hwndEncounterAdd            syscall.Handle
	hwndEncounterRemove         syscall.Handle
	hwndEncounterSlotLabel      syscall.Handle
	hwndEncounterWeightLabel    syscall.Handle
	hwndEncounterWeight         syscall.Handle
	hwndEncounterMinLabel       syscall.Handle
	hwndEncounterMaxLabel       syscall.Handle
	hwndEncounterSummary        syscall.Handle

	encounterPanelOldWndProc uintptr

	encounterDoc             EncounterMapDocument
	encounterCurrentMethod   string
	encounterSpeciesCatalog  []EncounterSpecies
	encounterFilteredSpecies []EncounterSpecies
	encounterSpeciesProject  string
	encounterDirty           bool
	encounterLoadedMapID     int
	encounterLoadingUI       bool
)

var encounterContainerWndProc = syscall.NewCallback(func(hwnd syscall.Handle, msg uint32, w, l uintptr) uintptr {
	if msg == WM_COMMAND && hwndMain != 0 {
		pSendMessageW.Call(uintptr(hwndMain), uintptr(msg), w, l)
		return 0
	}
	if encounterPanelOldWndProc != 0 {
		ret, _, _ := pCallWindowProcW.Call(encounterPanelOldWndProc, uintptr(hwnd), uintptr(msg), w, l)
		return ret
	}
	ret, _, _ := pDefWindowProcW.Call(uintptr(hwnd), uintptr(msg), w, l)
	return ret
})

func installEncounterCommandForwarder() {
	if hwndEncounterPanel == 0 || encounterPanelOldWndProc != 0 {
		return
	}
	ret, _, _ := pSetWindowLongPtrW.Call(uintptr(hwndEncounterPanel), ^uintptr(3), encounterContainerWndProc)
	encounterPanelOldWndProc = ret
}

func createEncounterEditor(hInst syscall.Handle) {
	hwndEncounterPanel = createWindow("BUTTON", "", WS_CHILD|BS_GROUPBOX, 0, 0, 0, 0, hwndMain, 1700, hInst)
	installEncounterCommandForwarder()

	hwndEncounterMapLabel = createWindow("STATIC", "Nessuna mappa selezionata", WS_CHILD|WS_VISIBLE, 0, 0, 0, 0, hwndEncounterPanel, 1701, hInst)
	hwndEncounterMethodLabel = createWindow("STATIC", "Metodo / terreno", WS_CHILD|WS_VISIBLE, 0, 0, 0, 0, hwndEncounterPanel, 1702, hInst)
	hwndEncounterMethodCombo = createWindow("COMBOBOX", "", WS_CHILD|WS_VISIBLE|WS_TABSTOP|CBS_DROPDOWNLIST, 0, 0, 0, 200, hwndEncounterPanel, idEncounterMethod, hInst)
	hwndEncounterMethodRateLbl = createWindow("STATIC", "Frequenza metodo", WS_CHILD|WS_VISIBLE, 0, 0, 0, 0, hwndEncounterPanel, 1703, hInst)
	hwndEncounterRate = createWindow("EDIT", "21", WS_CHILD|WS_VISIBLE|WS_BORDER|WS_TABSTOP, 0, 0, 0, 0, hwndEncounterPanel, idEncounterMethodRate, hInst)
	hwndEncounterSearchLabel = createWindow("STATIC", "Cerca Pokémon", WS_CHILD|WS_VISIBLE, 0, 0, 0, 0, hwndEncounterPanel, 1704, hInst)
	hwndEncounterSearch = createWindow("EDIT", "", WS_CHILD|WS_VISIBLE|WS_BORDER|WS_TABSTOP, 0, 0, 0, 0, hwndEncounterPanel, idEncounterSearch, hInst)

	hwndEncounterAvailableLabel = createWindow("STATIC", "Pokémon disponibili", WS_CHILD|WS_VISIBLE, 0, 0, 0, 0, hwndEncounterPanel, 1705, hInst)
	hwndEncounterAvailable = createWindow("LISTBOX", "", WS_CHILD|WS_VISIBLE|WS_BORDER|WS_VSCROLL|WS_TABSTOP|LBS_NOTIFY|LBS_NOINTEGRALHEIGHT, 0, 0, 0, 0, hwndEncounterPanel, idEncounterAvailable, hInst)
	hwndEncounterAssignedLabel = createWindow("STATIC", "Incontri presenti nella mappa", WS_CHILD|WS_VISIBLE, 0, 0, 0, 0, hwndEncounterPanel, 1706, hInst)
	hwndEncounterAssigned = createWindow("LISTBOX", "", WS_CHILD|WS_VISIBLE|WS_BORDER|WS_VSCROLL|WS_TABSTOP|LBS_NOTIFY|LBS_NOINTEGRALHEIGHT, 0, 0, 0, 0, hwndEncounterPanel, idEncounterAssigned, hInst)

	hwndEncounterAdd = createWindow("BUTTON", "Aggiungi  →", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 0, 0, 0, 0, hwndEncounterPanel, idEncounterAdd, hInst)
	hwndEncounterRemove = createWindow("BUTTON", "←  Rimuovi", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 0, 0, 0, 0, hwndEncounterPanel, idEncounterRemove, hInst)

	hwndEncounterSlotLabel = createWindow("STATIC", "Dettagli slot selezionato", WS_CHILD|WS_VISIBLE, 0, 0, 0, 0, hwndEncounterPanel, 1707, hInst)
	hwndEncounterWeightLabel = createWindow("STATIC", "Peso / %", WS_CHILD|WS_VISIBLE, 0, 0, 0, 0, hwndEncounterPanel, 1708, hInst)
	hwndEncounterWeight = createWindow("EDIT", "20", WS_CHILD|WS_VISIBLE|WS_BORDER|WS_TABSTOP, 0, 0, 0, 0, hwndEncounterPanel, idEncounterWeight, hInst)
	hwndEncounterMinLabel = createWindow("STATIC", "Livello min", WS_CHILD|WS_VISIBLE, 0, 0, 0, 0, hwndEncounterPanel, 1709, hInst)
	hwndEncounterMin = createWindow("EDIT", "2", WS_CHILD|WS_VISIBLE|WS_BORDER|WS_TABSTOP, 0, 0, 0, 0, hwndEncounterPanel, idEncounterMin, hInst)
	hwndEncounterMaxLabel = createWindow("STATIC", "Livello max", WS_CHILD|WS_VISIBLE, 0, 0, 0, 0, hwndEncounterPanel, 1722, hInst)
	hwndEncounterMax = createWindow("EDIT", "5", WS_CHILD|WS_VISIBLE|WS_BORDER|WS_TABSTOP, 0, 0, 0, 0, hwndEncounterPanel, idEncounterMax, hInst)
	hwndEncounterExpand = createWindow("BUTTON", "Applica valori", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 0, 0, 0, 0, hwndEncounterPanel, idEncounterApply, hInst)
	hwndEncounterSave = createWindow("BUTTON", "Salva incontri", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 0, 0, 0, 0, hwndEncounterPanel, idEncounterSave, hInst)
	hwndEncounterSummary = createWindow("STATIC", "", WS_CHILD|WS_VISIBLE, 0, 0, 0, 0, hwndEncounterPanel, 1723, hInst)

	populateEncounterMethodCombo()
}

func encounterHandles() []syscall.Handle {
	return []syscall.Handle{
		hwndEncounterPanel,
		hwndEncounterMapLabel, hwndEncounterMethodLabel, hwndEncounterMethodCombo, hwndEncounterMethodRateLbl, hwndEncounterRate,
		hwndEncounterSearchLabel, hwndEncounterSearch, hwndEncounterAvailableLabel, hwndEncounterAvailable,
		hwndEncounterAssignedLabel, hwndEncounterAssigned, hwndEncounterAdd, hwndEncounterRemove,
		hwndEncounterSlotLabel, hwndEncounterWeightLabel, hwndEncounterWeight, hwndEncounterMinLabel, hwndEncounterMin,
		hwndEncounterMaxLabel, hwndEncounterMax, hwndEncounterExpand, hwndEncounterSave, hwndEncounterSummary,
	}
}

func showEncounterEditor(show bool) {
	showControl(hwndEncounterPanel, show)
	if show {
		loadEncounterEditorForCurrentMap()
	}
}

func layoutEncounterEditor(w, h int32) {
	if hwndEncounterPanel == 0 || w <= 0 || h <= 0 {
		return
	}
	pad := int32(16)
	// La Vista Pokémon selvatici viene mostrata senza toolbar generale. Lascia
	// quindi un margine superiore interno dedicato, così la prima riga di
	// controlli non resta troppo vicina alla barra delle viste e non viene
	// visivamente tagliata. Il margine è locale a questo editor e non altera
	// nessun'altra vista.
	topPad := int32(16)
	innerW := w - pad*2
	if innerW < 620 {
		innerW = 620
	}

	// Riga superiore: mappa, metodo, frequenza e ricerca.
	moveControl(hwndEncounterMapLabel, pad, topPad+24, innerW, 22)
	moveControl(hwndEncounterMethodLabel, pad, topPad+55, 110, 20)
	moveControl(hwndEncounterMethodCombo, pad+112, topPad+51, 220, 220)
	moveControl(hwndEncounterMethodRateLbl, pad+348, topPad+55, 116, 20)
	moveControl(hwndEncounterRate, pad+468, topPad+51, 64, 24)
	moveControl(hwndEncounterSearchLabel, pad+552, topPad+55, 96, 20)
	searchW := innerW - 552 - 96
	if searchW < 180 {
		searchW = 180
	}
	moveControl(hwndEncounterSearch, pad+650, topPad+51, searchW, 24)

	listTop := topPad + 105
	bottomH := int32(118)
	listBottom := h - bottomH - 18
	if listBottom < listTop+180 {
		listBottom = listTop + 180
	}
	listH := listBottom - listTop
	middleW := int32(112)
	paneGap := int32(12)
	paneW := (innerW - middleW - paneGap*2) / 2
	if paneW < 220 {
		paneW = 220
	}
	rightX := pad + paneW + paneGap + middleW + paneGap
	midX := pad + paneW + paneGap

	moveControl(hwndEncounterAvailableLabel, pad, listTop-24, paneW, 20)
	moveControl(hwndEncounterAvailable, pad, listTop, paneW, listH)
	moveControl(hwndEncounterAssignedLabel, rightX, listTop-24, paneW, 20)
	moveControl(hwndEncounterAssigned, rightX, listTop, paneW, listH)

	buttonY := listTop + listH/2 - 34
	moveControl(hwndEncounterAdd, midX, buttonY, middleW, 30)
	moveControl(hwndEncounterRemove, midX, buttonY+40, middleW, 30)

	propsY := listBottom + 14
	moveControl(hwndEncounterSlotLabel, pad, propsY, 180, 20)
	moveControl(hwndEncounterWeightLabel, pad, propsY+31, 72, 20)
	moveControl(hwndEncounterWeight, pad+76, propsY+27, 64, 24)
	moveControl(hwndEncounterMinLabel, pad+160, propsY+31, 78, 20)
	moveControl(hwndEncounterMin, pad+242, propsY+27, 64, 24)
	moveControl(hwndEncounterMaxLabel, pad+326, propsY+31, 78, 20)
	moveControl(hwndEncounterMax, pad+408, propsY+27, 64, 24)
	moveControl(hwndEncounterExpand, pad+492, propsY+26, 112, 27)
	moveControl(hwndEncounterSave, pad+616, propsY+26, 120, 27)
	moveControl(hwndEncounterSummary, pad, propsY+64, innerW, 34)
}

func encounterViewPlaceholder() string {
	return "Pokémon selvatici\r\n\r\nScegli il metodo/terreno, cerca una specie e usa Aggiungi →.\r\nGli incontri presenti nella mappa sono nella lista di destra."
}

func encounterCanvasMessage() string { return "Editor Pokémon selvatici" }

func defaultEncounterRate(method string) int {
	switch method {
	case "old_rod", "good_rod", "super_rod":
		return 15
	case "rock_smash", "headbutt_low", "headbutt_high", "special":
		return 10
	default:
		return 21
	}
}

func encounterMethodLabel(id string) string {
	for _, d := range encounterMethodDefs {
		if d.ID == id {
			return d.Label
		}
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return "Altro"
	}
	return id
}

func ensureEncounterMethodDef(id, label string) {
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	for _, d := range encounterMethodDefs {
		if d.ID == id {
			return
		}
	}
	if strings.TrimSpace(label) == "" {
		label = id
	}
	encounterMethodDefs = append(encounterMethodDefs, encounterMethodDef{ID: id, Label: label})
}

func populateEncounterMethodCombo() {
	if hwndEncounterMethodCombo == 0 {
		return
	}
	selected := encounterCurrentMethod
	pSendMessageW.Call(uintptr(hwndEncounterMethodCombo), CB_RESETCONTENT, 0, 0)
	for _, d := range encounterMethodDefs {
		comboAdd(hwndEncounterMethodCombo, d.Label)
	}
	idx := 0
	for i, d := range encounterMethodDefs {
		if d.ID == selected {
			idx = i
			break
		}
	}
	pSendMessageW.Call(uintptr(hwndEncounterMethodCombo), CB_SETCURSEL, uintptr(idx), 0)
	if len(encounterMethodDefs) > 0 {
		encounterCurrentMethod = encounterMethodDefs[idx].ID
	}
}

func selectedEncounterMethodFromCombo() string {
	if hwndEncounterMethodCombo == 0 || len(encounterMethodDefs) == 0 {
		return "land"
	}
	r, _, _ := pSendMessageW.Call(uintptr(hwndEncounterMethodCombo), CB_GETCURSEL, 0, 0)
	i := int(int32(r))
	if i < 0 || i >= len(encounterMethodDefs) {
		return encounterMethodDefs[0].ID
	}
	return encounterMethodDefs[i].ID
}

func encounterFilePath(mapID int) string {
	if currentProject == "" || mapID <= 0 {
		return ""
	}
	return filepath.Join(currentProject, "converted", "data", "encounters", fmt.Sprintf("Map%03d.json", mapID))
}

func legacyEncounterFilePath(mapID int) string {
	if currentProject == "" || mapID <= 0 {
		return ""
	}
	return filepath.Join(currentProject, "converted", "maps", "encounters", fmt.Sprintf("Map%03d.json", mapID))
}

// migrateLegacyEncounterStorage moves the encounter sidecars created by the
// first encounter-editor implementation out of converted/maps. Keeping any
// MapXXX.json metadata below converted/maps is unsafe because physical map
// discovery intentionally uses that namespace for real map files.
func migrateLegacyEncounterStorage() error {
	if currentProject == "" {
		return nil
	}
	legacyDir := filepath.Join(currentProject, "converted", "maps", "encounters")
	info, err := os.Stat(legacyDir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("percorso legacy incontri non valido: %s", legacyDir)
	}
	entries, err := os.ReadDir(legacyDir)
	if err != nil {
		return err
	}
	var problems []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		id, ok := fsMapIDFromJSONName(entry.Name())
		if !ok {
			continue
		}
		src := filepath.Join(legacyDir, entry.Name())
		dst := encounterFilePath(id)
		if err := fsMoveVerified(src, dst); err != nil {
			problems = append(problems, fmt.Sprintf("%s -> %s: %v", src, dst, err))
		}
	}
	// Remove the obsolete folder only if it is now empty.
	if remaining, readErr := os.ReadDir(legacyDir); readErr == nil && len(remaining) == 0 {
		_ = os.Remove(legacyDir)
	}
	if len(problems) > 0 {
		return fmt.Errorf("migrazione incontri legacy incompleta:\r\n%s", strings.Join(problems, "\r\n"))
	}
	return nil
}

func migrateLegacyEncounterForMap(mapID int) error {
	if mapID <= 0 {
		return nil
	}
	src := legacyEncounterFilePath(mapID)
	if src == "" {
		return nil
	}
	if _, err := os.Stat(src); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	return fsMoveVerified(src, encounterFilePath(mapID))
}

func newEncounterDoc(mapID int) EncounterMapDocument {
	return EncounterMapDocument{Version: 1, Schema: encounterSchema, MapID: mapID, Methods: map[string]*EncounterMethodData{}}
}

func ensureEncounterMethod(id string) *EncounterMethodData {
	if encounterDoc.Methods == nil {
		encounterDoc.Methods = map[string]*EncounterMethodData{}
	}
	m := encounterDoc.Methods[id]
	if m == nil {
		m = &EncounterMethodData{Label: encounterMethodLabel(id), Rate: defaultEncounterRate(id), Slots: []EncounterSlot{}}
		encounterDoc.Methods[id] = m
	}
	if m.Label == "" {
		m.Label = encounterMethodLabel(id)
	}
	return m
}

func currentEncounterMethodData() *EncounterMethodData {
	if encounterCurrentMethod == "" {
		encounterCurrentMethod = selectedEncounterMethodFromCombo()
	}
	return ensureEncounterMethod(encounterCurrentMethod)
}

func loadEncounterEditorForCurrentMap() {
	if hwndEncounterPanel == 0 {
		return
	}
	if currentMap == nil || currentProject == "" {
		encounterLoadedMapID = 0
		encounterDoc = newEncounterDoc(0)
		setText(hwndEncounterMapLabel, "Nessuna mappa selezionata")
		clearList(hwndEncounterAvailable)
		clearList(hwndEncounterAssigned)
		setText(hwndEncounterSummary, "Apri una mappa per modificarne gli incontri selvatici.")
		return
	}
	if encounterLoadedMapID == currentMap.ID && encounterDoc.MapID == currentMap.ID {
		refreshEncounterUI()
		return
	}

	encounterSpeciesCatalog = loadEncounterSpeciesCatalog()
	encounterDoc = newEncounterDoc(currentMap.ID)
	encounterLoadedMapID = currentMap.ID
	encounterDirty = false

	if err := migrateLegacyEncounterForMap(currentMap.ID); err != nil {
		setToolbarStatus("Pokémon selvatici: migrazione dati legacy non riuscita")
	}
	path := encounterFilePath(currentMap.ID)
	if b, err := os.ReadFile(path); err == nil {
		var doc EncounterMapDocument
		if json.Unmarshal(b, &doc) == nil && doc.MapID == currentMap.ID {
			if doc.Methods == nil {
				doc.Methods = map[string]*EncounterMethodData{}
			}
			encounterDoc = doc
			for id, m := range doc.Methods {
				label := ""
				if m != nil {
					label = m.Label
				}
				ensureEncounterMethodDef(id, label)
			}
		}
	} else if imported, ok := importEssentialsEncounters(currentMap.ID); ok {
		encounterDoc = imported
		for id, m := range imported.Methods {
			ensureEncounterMethodDef(id, m.Label)
		}
	}

	populateEncounterMethodCombo()
	if encounterCurrentMethod == "" {
		encounterCurrentMethod = "land"
	}
	setText(hwndEncounterMapLabel, fmt.Sprintf("Map %03d — %s", currentMap.ID, currentMap.Name))
	setText(hwndEncounterSearch, "")
	refreshEncounterUI()
}

func refreshEncounterUI() {
	if hwndEncounterPanel == 0 || currentMap == nil {
		return
	}
	encounterLoadingUI = true
	defer func() { encounterLoadingUI = false }()

	method := currentEncounterMethodData()
	setText(hwndEncounterRate, strconv.Itoa(method.Rate))
	refreshEncounterAssignedList(-1)
	refreshEncounterAvailableList()
	updateEncounterSummary()
}

func refreshEncounterAvailableList() {
	if hwndEncounterAvailable == 0 {
		return
	}
	query := strings.ToLower(strings.TrimSpace(getText(hwndEncounterSearch)))
	used := map[string]bool{}
	for _, slot := range currentEncounterMethodData().Slots {
		used[strings.ToUpper(strings.TrimSpace(slot.Species))] = true
	}
	clearList(hwndEncounterAvailable)
	encounterFilteredSpecies = encounterFilteredSpecies[:0]
	for _, sp := range encounterSpeciesCatalog {
		id := strings.ToUpper(strings.TrimSpace(sp.ID))
		if id == "" || used[id] {
			continue
		}
		if query != "" {
			hay := strings.ToLower(sp.ID + " " + sp.Name)
			if !strings.Contains(hay, query) {
				continue
			}
		}
		encounterFilteredSpecies = append(encounterFilteredSpecies, sp)
		label := sp.Name
		if label == "" || strings.EqualFold(label, sp.ID) {
			label = sp.ID
		} else {
			label = sp.Name + "  [" + sp.ID + "]"
		}
		addList(hwndEncounterAvailable, label)
	}
	if len(encounterFilteredSpecies) == 0 {
		if len(encounterSpeciesCatalog) == 0 {
			addList(hwndEncounterAvailable, "Catalogo Pokémon non trovato")
		} else if query != "" {
			addList(hwndEncounterAvailable, "Nessun risultato")
		}
	}
}

func refreshEncounterAssignedList(selectIndex int) {
	if hwndEncounterAssigned == 0 {
		return
	}
	clearList(hwndEncounterAssigned)
	slots := currentEncounterMethodData().Slots
	for _, slot := range slots {
		name := strings.TrimSpace(slot.Name)
		if name == "" {
			name = speciesDisplayName(slot.Species)
		}
		addList(hwndEncounterAssigned, fmt.Sprintf("%3d%%   Lv %d-%d   %s", slot.Weight, slot.MinLevel, slot.MaxLevel, name))
	}
	if len(slots) == 0 {
		addList(hwndEncounterAssigned, "Nessun incontro per questo metodo")
		setText(hwndEncounterWeight, "20")
		setText(hwndEncounterMin, "2")
		setText(hwndEncounterMax, "5")
		return
	}
	if selectIndex < 0 || selectIndex >= len(slots) {
		selectIndex = 0
	}
	pSendMessageW.Call(uintptr(hwndEncounterAssigned), LB_SETCURSEL, uintptr(selectIndex), 0)
	loadEncounterSlotEditors(selectIndex)
}

func updateEncounterSummary() {
	if hwndEncounterSummary == 0 || currentMap == nil {
		return
	}
	m := currentEncounterMethodData()
	total := 0
	for _, s := range m.Slots {
		total += s.Weight
	}
	state := "salvato"
	if encounterDirty {
		state = "modificato — premi Salva incontri"
	}
	setText(hwndEncounterSummary, fmt.Sprintf("%s • %d Pokémon • peso totale %d • frequenza metodo %d • %s", encounterMethodLabel(encounterCurrentMethod), len(m.Slots), total, m.Rate, state))
}

func listSelection(h syscall.Handle) int {
	if h == 0 {
		return -1
	}
	r, _, _ := pSendMessageW.Call(uintptr(h), LB_GETCURSEL, 0, 0)
	i := int(int32(r))
	if i < 0 {
		return -1
	}
	return i
}

func parseEncounterNumber(h syscall.Handle, fallback, min, max int) int {
	n, err := strconv.Atoi(strings.TrimSpace(getText(h)))
	if err != nil {
		n = fallback
	}
	if n < min {
		n = min
	}
	if n > max {
		n = max
	}
	return n
}

func loadEncounterSlotEditors(index int) {
	slots := currentEncounterMethodData().Slots
	if index < 0 || index >= len(slots) {
		return
	}
	s := slots[index]
	setText(hwndEncounterWeight, strconv.Itoa(s.Weight))
	setText(hwndEncounterMin, strconv.Itoa(s.MinLevel))
	setText(hwndEncounterMax, strconv.Itoa(s.MaxLevel))
}

func commitEncounterMethodRate() {
	if currentMap == nil {
		return
	}
	m := currentEncounterMethodData()
	n := parseEncounterNumber(hwndEncounterRate, m.Rate, 0, 255)
	if m.Rate != n {
		m.Rate = n
		encounterDirty = true
	}
	setText(hwndEncounterRate, strconv.Itoa(n))
}

func applyEncounterSlotEditors(showErrors bool) bool {
	if currentMap == nil {
		return false
	}
	idx := listSelection(hwndEncounterAssigned)
	m := currentEncounterMethodData()
	if idx < 0 || idx >= len(m.Slots) {
		if showErrors {
			setToolbarStatus("Pokémon selvatici: seleziona prima uno slot nella lista di destra.")
		}
		return false
	}
	old := m.Slots[idx]
	weight := parseEncounterNumber(hwndEncounterWeight, old.Weight, 1, 10000)
	minLv := parseEncounterNumber(hwndEncounterMin, old.MinLevel, 1, 999)
	maxLv := parseEncounterNumber(hwndEncounterMax, old.MaxLevel, 1, 999)
	if maxLv < minLv {
		maxLv = minLv
	}
	if old.Weight != weight || old.MinLevel != minLv || old.MaxLevel != maxLv {
		m.Slots[idx].Weight = weight
		m.Slots[idx].MinLevel = minLv
		m.Slots[idx].MaxLevel = maxLv
		encounterDirty = true
	}
	refreshEncounterAssignedList(idx)
	updateEncounterSummary()
	return true
}

func addSelectedEncounterSpecies() {
	if currentMap == nil {
		setToolbarStatus("Pokémon selvatici: apri prima una mappa.")
		return
	}
	idx := listSelection(hwndEncounterAvailable)
	if idx < 0 || idx >= len(encounterFilteredSpecies) {
		setToolbarStatus("Pokémon selvatici: seleziona una specie dalla lista di sinistra.")
		return
	}
	sp := encounterFilteredSpecies[idx]
	m := currentEncounterMethodData()
	m.Slots = append(m.Slots, EncounterSlot{Species: sp.ID, Name: sp.Name, Weight: 20, MinLevel: 2, MaxLevel: 5})
	encounterDirty = true
	newIdx := len(m.Slots) - 1
	refreshEncounterAssignedList(newIdx)
	refreshEncounterAvailableList()
	updateEncounterSummary()
	setToolbarStatus("Pokémon selvatici: " + sp.Name + " aggiunto a " + encounterMethodLabel(encounterCurrentMethod) + ".")
}

func removeSelectedEncounterSpecies() {
	if currentMap == nil {
		return
	}
	idx := listSelection(hwndEncounterAssigned)
	m := currentEncounterMethodData()
	if idx < 0 || idx >= len(m.Slots) {
		setToolbarStatus("Pokémon selvatici: seleziona un incontro dalla lista di destra.")
		return
	}
	removed := m.Slots[idx]
	m.Slots = append(m.Slots[:idx], m.Slots[idx+1:]...)
	encounterDirty = true
	next := idx
	if next >= len(m.Slots) {
		next = len(m.Slots) - 1
	}
	refreshEncounterAssignedList(next)
	refreshEncounterAvailableList()
	updateEncounterSummary()
	setToolbarStatus("Pokémon selvatici: " + speciesDisplayName(removed.Species) + " rimosso dal metodo corrente.")
}

func changeEncounterMethodFromCombo() {
	if encounterLoadingUI || currentMap == nil {
		return
	}
	commitEncounterMethodRate()
	encounterCurrentMethod = selectedEncounterMethodFromCombo()
	m := currentEncounterMethodData()
	setText(hwndEncounterRate, strconv.Itoa(m.Rate))
	refreshEncounterAssignedList(-1)
	refreshEncounterAvailableList()
	updateEncounterSummary()
}

func saveEncounterData() error {
	if currentProject == "" || currentMap == nil {
		return nil
	}
	// Salva sempre il documento della mappa realmente corrente. Se la vista
	// incontri non e' mai stata aperta, carichiamo prima il sidecar/PBS invece
	// di rischiare di sovrascriverlo con un documento vuoto.
	if encounterLoadedMapID != currentMap.ID || encounterDoc.MapID != currentMap.ID {
		loadEncounterEditorForCurrentMap()
	}
	if err := ensureProjectMapFilesystemSafe("Salvataggio incontri selvatici"); err != nil {
		return err
	}
	commitEncounterMethodRate()
	applyEncounterSlotEditors(false)
	encounterDoc.Version = 1
	encounterDoc.Schema = encounterSchema
	encounterDoc.MapID = currentMap.ID
	if encounterDoc.Methods == nil {
		encounterDoc.Methods = map[string]*EncounterMethodData{}
	}
	path := encounterFilePath(currentMap.ID)
	if path == "" {
		return fmt.Errorf("percorso incontri non disponibile")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(encounterDoc, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	encounterDirty = false
	updateEncounterSummary()
	return nil
}

func handleEncounterCommand(id uint16, notify uint16) bool {
	switch id {
	case idEncounterMethod:
		if notify == cbnSelChange {
			changeEncounterMethodFromCombo()
		}
		return true
	case idEncounterSearch:
		if notify == enChange && !encounterLoadingUI {
			refreshEncounterAvailableList()
		}
		return true
	case idEncounterAvailable:
		if notify == lbnDblClk {
			addSelectedEncounterSpecies()
		}
		return true
	case idEncounterAssigned:
		if notify == lbnSelChange {
			idx := listSelection(hwndEncounterAssigned)
			loadEncounterSlotEditors(idx)
		} else if notify == lbnDblClk {
			applyEncounterSlotEditors(false)
		}
		return true
	case idEncounterAdd:
		addSelectedEncounterSpecies()
		return true
	case idEncounterRemove:
		removeSelectedEncounterSpecies()
		return true
	case idEncounterApply:
		if applyEncounterSlotEditors(true) {
			setToolbarStatus("Pokémon selvatici: valori dello slot aggiornati.")
		}
		return true
	case idEncounterSave:
		if err := saveEncounterData(); err != nil {
			msgbox("PML Studio", "Impossibile salvare gli incontri selvatici:\r\n\r\n"+err.Error(), MB_OK|MB_ICONERROR)
			setToolbarStatus("Pokémon selvatici: errore salvataggio - " + err.Error())
		} else {
			setToolbarStatus(fmt.Sprintf("Pokémon selvatici: incontri Map%03d salvati.", currentMap.ID))
		}
		return true
	case idEncounterMethodRate:
		if notify == enChange && !encounterLoadingUI {
			// Il valore viene validato al cambio metodo / Salva. Segniamo soltanto
			// la vista come modificata per non interrompere la digitazione.
			encounterDirty = true
			updateEncounterSummary()
		}
		return true
	case idEncounterWeight, idEncounterMin, idEncounterMax:
		if notify == enChange && !encounterLoadingUI {
			encounterDirty = true
			updateEncounterSummary()
		}
		return true
	}
	return false
}

func speciesDisplayName(id string) string {
	for _, sp := range encounterSpeciesCatalog {
		if strings.EqualFold(sp.ID, id) {
			if strings.TrimSpace(sp.Name) != "" {
				return sp.Name
			}
			return sp.ID
		}
	}
	return id
}

func loadEncounterSpeciesCatalog() []EncounterSpecies {
	if currentProject == "" {
		return nil
	}
	if encounterSpeciesProject == currentProject && len(encounterSpeciesCatalog) > 0 {
		return encounterSpeciesCatalog
	}
	encounterSpeciesProject = currentProject
	catalog := map[string]EncounterSpecies{}

	for _, rel := range []string{
		filepath.Join("PBS", "pokemon.txt"), filepath.Join("pbs", "pokemon.txt"),
		filepath.Join("converted", "PBS", "pokemon.txt"), filepath.Join("converted", "pbs", "pokemon.txt"),
	} {
		parsePokemonPBS(filepath.Join(currentProject, rel), catalog)
	}

	for _, rel := range []string{
		filepath.Join("converted", "data", "species.json"), filepath.Join("converted", "data", "pokemon.json"),
		filepath.Join("converted", "species.json"), filepath.Join("converted", "pokemon.json"),
	} {
		parseSpeciesJSON(filepath.Join(currentProject, rel), catalog)
	}

	// Se il catalogo specie non e' disponibile ma esiste encounters.txt,
	// recuperiamo almeno gli ID gia' usati dal progetto.
	if len(catalog) == 0 {
		for _, rel := range []string{filepath.Join("PBS", "encounters.txt"), filepath.Join("pbs", "encounters.txt")} {
			parseSpeciesIDsFromEncounterPBS(filepath.Join(currentProject, rel), catalog)
		}
	}

	out := make([]EncounterSpecies, 0, len(catalog))
	for _, sp := range catalog {
		out = append(out, sp)
	}
	sort.Slice(out, func(i, j int) bool {
		a := strings.ToLower(out[i].Name)
		b := strings.ToLower(out[j].Name)
		if a == b {
			return out[i].ID < out[j].ID
		}
		return a < b
	})
	encounterSpeciesCatalog = out
	return out
}

func parsePokemonPBS(path string, dst map[string]EncounterSpecies) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	var section, name, internal string
	flush := func() {
		id := strings.TrimSpace(internal)
		if id == "" {
			id = strings.TrimSpace(section)
		}
		if id == "" {
			return
		}
		id = strings.ToUpper(id)
		if strings.TrimSpace(name) == "" {
			name = id
		}
		dst[id] = EncounterSpecies{ID: id, Name: strings.TrimSpace(name)}
	}
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := strings.TrimSpace(stripPBSComment(s.Text()))
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			flush()
			section = strings.TrimSpace(line[1 : len(line)-1])
			name, internal = "", ""
			continue
		}
		if eq := strings.Index(line, "="); eq >= 0 {
			key := strings.ToLower(strings.TrimSpace(line[:eq]))
			val := strings.TrimSpace(line[eq+1:])
			switch key {
			case "name":
				name = val
			case "internalname", "internal_name", "id":
				internal = val
			}
		}
	}
	flush()
}

func parseSpeciesJSON(path string, dst map[string]EncounterSpecies) {
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var root any
	if json.Unmarshal(b, &root) != nil {
		return
	}
	var walk func(v any, keyHint string)
	walk = func(v any, keyHint string) {
		switch t := v.(type) {
		case []any:
			for _, item := range t {
				walk(item, "")
			}
		case map[string]any:
			id := jsonStringCI(t, "id", "internal_name", "internalname", "species")
			name := jsonStringCI(t, "name", "display_name", "real_name")
			if id == "" && keyHint != "" && name != "" {
				id = keyHint
			}
			if id != "" && name != "" {
				id = strings.ToUpper(strings.TrimSpace(id))
				dst[id] = EncounterSpecies{ID: id, Name: strings.TrimSpace(name)}
			}
			for k, item := range t {
				walk(item, k)
			}
		}
	}
	walk(root, "")
}

func jsonStringCI(m map[string]any, keys ...string) string {
	for _, want := range keys {
		for k, v := range m {
			if !strings.EqualFold(k, want) {
				continue
			}
			switch x := v.(type) {
			case string:
				return x
			case float64:
				return strconv.Itoa(int(x))
			}
		}
	}
	return ""
}

func parseSpeciesIDsFromEncounterPBS(path string, dst map[string]EncounterSpecies) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	activeMethod := ""
	s := bufio.NewScanner(f)
	for s.Scan() {
		raw := stripPBSComment(s.Text())
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "[") {
			activeMethod = ""
			continue
		}
		parts := splitCSVLine(line)
		if len(parts) == 0 {
			continue
		}
		if id, ok := encounterPBSMethodID(parts[0]); ok {
			activeMethod = id
			continue
		}
		if activeMethod != "" {
			id := strings.ToUpper(strings.TrimSpace(parts[0]))
			if id != "" {
				dst[id] = EncounterSpecies{ID: id, Name: id}
			}
		}
	}
}

func importEssentialsEncounters(mapID int) (EncounterMapDocument, bool) {
	for _, rel := range []string{
		filepath.Join("PBS", "encounters.txt"), filepath.Join("pbs", "encounters.txt"),
		filepath.Join("converted", "PBS", "encounters.txt"), filepath.Join("converted", "pbs", "encounters.txt"),
	} {
		if doc, ok := parseEncounterPBSForMap(filepath.Join(currentProject, rel), mapID); ok {
			return doc, true
		}
	}
	return newEncounterDoc(mapID), false
}

func parseEncounterPBSForMap(path string, mapID int) (EncounterMapDocument, bool) {
	f, err := os.Open(path)
	if err != nil {
		return EncounterMapDocument{}, false
	}
	defer f.Close()
	doc := newEncounterDoc(mapID)
	inSection := false
	found := false
	methodID := ""
	s := bufio.NewScanner(f)
	for s.Scan() {
		raw := stripPBSComment(s.Text())
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section := strings.TrimSpace(line[1 : len(line)-1])
			sectionID := parseMapSectionID(section)
			if inSection && sectionID != mapID {
				break
			}
			inSection = sectionID == mapID
			if inSection {
				found = true
			}
			methodID = ""
			continue
		}
		if !inSection {
			continue
		}
		parts := splitCSVLine(line)
		if len(parts) == 0 {
			continue
		}
		if id, ok := encounterPBSMethodID(parts[0]); ok {
			methodID = id
			ensureEncounterMethodDef(id, encounterMethodLabel(id))
			rate := defaultEncounterRate(id)
			if len(parts) >= 2 {
				if n, e := strconv.Atoi(strings.TrimSpace(parts[1])); e == nil {
					rate = n
				}
			}
			doc.Methods[id] = &EncounterMethodData{Label: encounterMethodLabel(id), Rate: rate, Slots: []EncounterSlot{}}
			continue
		}
		if methodID == "" || len(parts) < 2 {
			continue
		}
		sp := strings.ToUpper(strings.TrimSpace(parts[0]))
		weight, _ := strconv.Atoi(strings.TrimSpace(parts[1]))
		minLv, maxLv := 2, 5
		if len(parts) >= 3 {
			if n, e := strconv.Atoi(strings.TrimSpace(parts[2])); e == nil {
				minLv = n
			}
		}
		if len(parts) >= 4 {
			if n, e := strconv.Atoi(strings.TrimSpace(parts[3])); e == nil {
				maxLv = n
			}
		} else {
			maxLv = minLv
		}
		if weight <= 0 {
			weight = 1
		}
		if maxLv < minLv {
			maxLv = minLv
		}
		m := doc.Methods[methodID]
		m.Slots = append(m.Slots, EncounterSlot{Species: sp, Name: speciesDisplayName(sp), Weight: weight, MinLevel: minLv, MaxLevel: maxLv})
	}
	return doc, found
}

func parseMapSectionID(section string) int {
	section = strings.TrimSpace(section)
	if comma := strings.Index(section, ","); comma >= 0 {
		section = section[:comma]
	}
	section = strings.TrimSpace(section)
	n, _ := strconv.Atoi(section)
	return n
}

func encounterPBSMethodID(v string) (string, bool) {
	key := strings.ToLower(strings.TrimSpace(v))
	key = strings.ReplaceAll(key, "_", "")
	key = strings.ReplaceAll(key, " ", "")
	id, ok := encounterPBSMethodAliases[key]
	return id, ok
}

func splitCSVLine(line string) []string {
	parts := strings.Split(line, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		out = append(out, strings.TrimSpace(p))
	}
	return out
}

func stripPBSComment(line string) string {
	if i := strings.Index(line, "#"); i >= 0 {
		return line[:i]
	}
	return line
}
