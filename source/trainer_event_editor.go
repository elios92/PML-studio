//go:build windows

package main

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

const (
	trainerEventClassName = "PLMStudioTrainerEvent05"

	idTrainerExisting = 2600
	idTrainerType     = 2601
	idTrainerName     = 2602
	idTrainerVersion  = 2603
	idTrainerSprite   = 2604
	idTrainerTeam     = 2605
	idTrainerSpecies  = 2606
	idTrainerLevel    = 2607
	idTrainerAdd      = 2608
	idTrainerRemove   = 2609
	idTrainerCreate   = 2610
	idTrainerCancel   = 2611
)

var (
	trainerEventClassRegistered bool
	trainerEventOpen            bool
	trainerEventWindow          syscall.Handle

	hwndTrainerExisting syscall.Handle
	hwndTrainerType     syscall.Handle
	hwndTrainerName     syscall.Handle
	hwndTrainerVersion  syscall.Handle
	hwndTrainerSprite   syscall.Handle
	hwndTrainerTeam     syscall.Handle
	hwndTrainerSpecies  syscall.Handle
	hwndTrainerLevel    syscall.Handle

	trainerEventExistingRecords []TrainerRecord
	trainerEventTypes           []TrainerTypeRecord
	trainerEventSprites         []string
	trainerEventSpecies         []EncounterSpecies
	trainerEventTeam            []TrainerPokemonRecord

	pendingTrainerEvent *EditorEvent

	trainerCommandMode   bool
	trainerCommandRecord *TrainerRecord
)

func comboSelectIndex(h syscall.Handle, idx int) {
	if h == 0 {
		return
	}
	pSendMessageW.Call(uintptr(h), CB_SETCURSEL, uintptr(idx), 0)
}

func setComboFromStrings(h syscall.Handle, values []string, selectIndex int) {
	if h == 0 {
		return
	}
	pSendMessageW.Call(uintptr(h), CB_RESETCONTENT, 0, 0)
	for _, s := range values {
		comboAdd(h, s)
	}
	if len(values) > 0 {
		if selectIndex < 0 || selectIndex >= len(values) {
			selectIndex = 0
		}
		comboSelectIndex(h, selectIndex)
	}
}

func trainerRecordDisplay(tr TrainerRecord) string {
	suffix := ""
	if tr.Version > 0 {
		suffix = fmt.Sprintf("  [v%d]", tr.Version)
	}
	return fmt.Sprintf("%s / %s%s", tr.TrainerType, tr.Name, suffix)
}

func refreshTrainerTeamList() {
	clearList(hwndTrainerTeam)
	for i, p := range trainerEventTeam {
		addList(hwndTrainerTeam, fmt.Sprintf("%d. %s  Lv.%d", i+1, speciesDisplayName(p.Species), p.Level))
	}
}

func loadTrainerRecordIntoDialog(tr TrainerRecord) {
	// trainer type
	typeIdx := 0
	for i, tt := range trainerEventTypes {
		if strings.EqualFold(tt.ID, tr.TrainerType) {
			typeIdx = i
			break
		}
	}
	comboSelectIndex(hwndTrainerType, typeIdx)
	setText(hwndTrainerName, tr.Name)
	setText(hwndTrainerVersion, strconv.Itoa(tr.Version))
	trainerEventTeam = append(trainerEventTeam[:0], tr.Team...)
	refreshTrainerTeamList()
}

func selectedTrainerTypeID() string {
	idx := comboSel(hwndTrainerType)
	if idx >= 0 && idx < len(trainerEventTypes) {
		return trainerEventTypes[idx].ID
	}
	return ""
}

func selectedTrainerSpriteName() string {
	idx := comboSel(hwndTrainerSprite)
	if idx >= 0 && idx < len(trainerEventSprites) {
		return trainerEventSprites[idx]
	}
	return ""
}

func selectedTrainerSpeciesID() string {
	idx := comboSel(hwndTrainerSpecies)
	if idx >= 0 && idx < len(trainerEventSpecies) {
		return trainerEventSpecies[idx].ID
	}
	return ""
}

func addTrainerTeamMemberFromDialog() {
	species := selectedTrainerSpeciesID()
	if species == "" {
		msgbox("PML Studio - Allenatore", "Seleziona un Pokémon.", MB_OK|MB_ICONINFORMATION)
		return
	}
	level, err := strconv.Atoi(strings.TrimSpace(getText(hwndTrainerLevel)))
	if err != nil || level < 1 || level > 100 {
		msgbox("PML Studio - Allenatore", "Il livello deve essere compreso tra 1 e 100.", MB_OK|MB_ICONERROR)
		return
	}
	if len(trainerEventTeam) >= 6 {
		msgbox("PML Studio - Allenatore", "Una squadra allenatore può contenere al massimo 6 Pokémon.", MB_OK|MB_ICONINFORMATION)
		return
	}
	trainerEventTeam = append(trainerEventTeam, TrainerPokemonRecord{Species: species, Level: level})
	refreshTrainerTeamList()
}

func removeTrainerTeamMemberFromDialog() {
	idx := listSel(hwndTrainerTeam)
	if idx < 0 || idx >= len(trainerEventTeam) {
		return
	}
	trainerEventTeam = append(trainerEventTeam[:idx], trainerEventTeam[idx+1:]...)
	refreshTrainerTeamList()
}

func listSel(h syscall.Handle) int {
	if h == 0 {
		return -1
	}
	r, _, _ := pSendMessageW.Call(uintptr(h), LB_GETCURSEL, 0, 0)
	if int32(r) < 0 {
		return -1
	}
	return int(r)
}

func trainerBattleScript(tr TrainerRecord) string {
	// Manteniamo una sintassi Essentials leggibile nel payload evento. Il runtime
	// Python potrà tradurla nel comando nativo equivalente durante la conversione.
	safeName := strings.ReplaceAll(tr.Name, `"`, `\"`)
	return fmt.Sprintf(`TrainerBattle.start(:%s, "%s", %d)`, tr.TrainerType, safeName, tr.Version)
}

func newTrainerEventRaw(tr TrainerRecord, sprite string) json.RawMessage {
	script := trainerBattleScript(tr)
	marker := fmt.Sprintf("PML_TRAINER_REF:%s|%s|%d", tr.TrainerType, tr.Name, tr.Version)
	raw := map[string]any{
		"_class": "RPG::Event",
		"id":     0,
		"name":   "Allenatore " + tr.Name,
		"x":      0,
		"y":      0,
		"pages": []any{
			map[string]any{
				"_class": "RPG::Event::Page",
				"condition": map[string]any{
					"_class": "RPG::Event::Page::Condition",
				},
				"graphic": map[string]any{
					"_class":         "RPG::Event::Page::Graphic",
					"tile_id":        0,
					"character_name": sprite,
					"character_hue":  0,
					"direction":      2,
					"pattern":        0,
					"opacity":        255,
					"blend_type":     0,
				},
				"move_type":      0,
				"move_speed":     3,
				"move_frequency": 3,
				"walk_anime":     true,
				"step_anime":     false,
				"direction_fix":  false,
				"through":        false,
				"always_on_top":  false,
				"trigger":        0,
				"list": []any{
					map[string]any{"_class": "RPG::EventCommand", "code": 108, "indent": 0, "parameters": []any{marker}},
					map[string]any{"_class": "RPG::EventCommand", "code": 355, "indent": 0, "parameters": []any{script}},
					map[string]any{"_class": "RPG::EventCommand", "code": 0, "indent": 0, "parameters": []any{}},
				},
			},
		},
	}
	b, _ := json.Marshal(raw)
	return b
}

func prepareTrainerEventFromDialog() bool {
	trainerType := selectedTrainerTypeID()
	name := strings.TrimSpace(getText(hwndTrainerName))
	version, err := strconv.Atoi(strings.TrimSpace(getText(hwndTrainerVersion)))
	if err != nil || version < 0 {
		msgbox("PML Studio - Allenatore", "Versione allenatore non valida.", MB_OK|MB_ICONERROR)
		return false
	}
	if trainerType == "" || name == "" {
		msgbox("PML Studio - Allenatore", "Tipo e nome allenatore sono obbligatori.", MB_OK|MB_ICONERROR)
		return false
	}
	if len(trainerEventTeam) == 0 {
		msgbox("PML Studio - Allenatore", "Aggiungi almeno un Pokémon alla squadra.", MB_OK|MB_ICONINFORMATION)
		return false
	}
	sprite := selectedTrainerSpriteName()
	if !trainerCommandMode && sprite == "" {
		msgbox("PML Studio - Allenatore", "Seleziona uno sprite NPC da Graphics/Characters.", MB_OK|MB_ICONERROR)
		return false
	}

	tr := TrainerRecord{TrainerType: trainerType, Name: name, Version: version, Team: append([]TrainerPokemonRecord(nil), trainerEventTeam...)}
	if err := upsertTrainerRecord(tr); err != nil {
		msgbox("PML Studio - Allenatore", "Impossibile salvare i dati dell'allenatore:\r\n\r\n"+err.Error(), MB_OK|MB_ICONERROR)
		return false
	}
	if trainerCommandMode {
		copyTr := tr
		trainerCommandRecord = &copyTr
		return true
	}

	raw := newTrainerEventRaw(tr, sprite)
	e := EditorEvent{
		ID:                 0,
		Name:               "Allenatore " + name,
		X:                  0,
		Y:                  0,
		Trigger:            "Interazione",
		Movement:           "Fermo",
		CharacterName:      sprite,
		CharacterDirection: 2,
		CharacterPattern:   0,
		EventKind:          "Allenatore Pokémon",
		TrainerType:        tr.TrainerType,
		TrainerName:        tr.Name,
		TrainerVersion:     tr.Version,
		Native:             true,
		NativeRaw:          raw,
	}
	pendingTrainerEvent = &e
	setToolbarStatus("Allenatore pronto: clicca una casella della mappa per posizionarlo.")
	return true
}

func placePendingTrainerEvent(x, y int) bool {
	if pendingTrainerEvent == nil {
		return false
	}
	e := *pendingTrainerEvent
	e.ID = nextEventID()
	e.X, e.Y = x, y
	e.NativeKey = strconv.Itoa(e.ID)
	e.NativeRaw = patchNativeEventRaw(e)
	events = append(events, e)
	selectedEvent = len(events) - 1
	pendingTrainerEvent = nil
	if err := saveEventsIntoRealMap(); err != nil {
		setToolbarStatus("Errore creazione allenatore: " + err.Error())
		return true
	}
	showEvent()
	invalidate(hwndCanvas)
	setToolbarStatus(fmt.Sprintf("Allenatore creato: #%d %s", e.ID, e.TrainerName))
	return true
}

func trainerEventWndProcFn(hwnd syscall.Handle, msg uint32, w, l uintptr) uintptr {
	switch msg {
	case WM_COMMAND:
		id := loword(w)
		notify := hiword(w)
		switch id {
		case idTrainerExisting:
			if notify == 1 { // CBN_SELCHANGE
				idx := comboSel(hwndTrainerExisting)
				if idx > 0 && idx-1 < len(trainerEventExistingRecords) {
					loadTrainerRecordIntoDialog(trainerEventExistingRecords[idx-1])
				} else if idx == 0 {
					setText(hwndTrainerName, "")
					setText(hwndTrainerVersion, "0")
					trainerEventTeam = trainerEventTeam[:0]
					refreshTrainerTeamList()
				}
			}
			return 0
		case idTrainerAdd:
			addTrainerTeamMemberFromDialog()
			return 0
		case idTrainerRemove:
			removeTrainerTeamMemberFromDialog()
			return 0
		case idTrainerCreate:
			if prepareTrainerEventFromDialog() {
				pDestroyWindow.Call(uintptr(hwnd))
			}
			return 0
		case idTrainerCancel:
			pDestroyWindow.Call(uintptr(hwnd))
			return 0
		}
	case WM_CLOSE:
		pDestroyWindow.Call(uintptr(hwnd))
		return 0
	case WM_DESTROY:
		trainerEventOpen = false
		trainerEventWindow = 0
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(uintptr(hwnd), uintptr(msg), w, l)
	return r
}

var trainerEventWndProc = syscall.NewCallback(trainerEventWndProcFn)

func ensureTrainerEventClass() error {
	if trainerEventClassRegistered {
		return nil
	}
	h, _, _ := pGetModuleHandleW.Call(0)
	hInst := syscall.Handle(h)
	cursor, _, _ := pLoadCursorW.Call(0, IDC_ARROW)
	brush, _, _ := pCreateSolidBrush.Call(rgb(244, 244, 244))
	wc := WNDCLASSEX{
		cbSize:        uint32(unsafe.Sizeof(WNDCLASSEX{})),
		lpfnWndProc:   trainerEventWndProc,
		hInstance:     hInst,
		hCursor:       syscall.Handle(cursor),
		hbrBackground: syscall.Handle(brush),
		lpszClassName: wstr(trainerEventClassName),
	}
	r, _, err := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	if r == 0 {
		return fmt.Errorf("registrazione editor allenatore fallita: %v", err)
	}
	trainerEventClassRegistered = true
	return nil
}

func showTrainerEventDialog(owner syscall.Handle, commandMode bool) {
	if currentMap == nil || currentMapDoc == nil {
		msgbox("PML Studio - Eventi", "Apri prima una mappa.", MB_OK|MB_ICONINFORMATION)
		return
	}
	trainerCommandMode = commandMode
	trainerCommandRecord = nil
	if err := ensureTrainerEventClass(); err != nil {
		msgbox("PML Studio - Eventi", err.Error(), MB_OK|MB_ICONERROR)
		return
	}
	ensureTrainerCatalog()
	trainerEventExistingRecords = append(trainerEventExistingRecords[:0], trainerCatalog...)
	trainerEventTypes = append(trainerEventTypes[:0], trainerTypeCatalog...)
	trainerEventSprites = characterSpriteNames()
	trainerEventSpecies = loadEncounterSpeciesCatalog()
	trainerEventTeam = trainerEventTeam[:0]

	if len(trainerEventTypes) == 0 {
		msgbox("PML Studio - Eventi", "Nessun tipo allenatore trovato. Controlla PBS/trainer_types.txt o i dati convertiti.", MB_OK|MB_ICONERROR)
		return
	}
	if len(trainerEventSpecies) == 0 {
		msgbox("PML Studio - Eventi", "Nessun Pokémon trovato. Controlla PBS/pokemon.txt o i dati convertiti.", MB_OK|MB_ICONERROR)
		return
	}
	if len(trainerEventSprites) == 0 {
		msgbox("PML Studio - Eventi", "Nessuno sprite trovato in Graphics/Characters.", MB_OK|MB_ICONERROR)
		return
	}

	const ww, wh int32 = 900, 650
	x, y := centerOwnedWindow(ww, wh)
	h, _, _ := pGetModuleHandleW.Call(0)
	hInst := syscall.Handle(h)
	trainerEventOpen = true
	trainerEventWindow = createWindow(trainerEventClassName, func() string {
		if commandMode {
			return "PML Studio - Comando Allenatore Pokémon"
		}
		return "PML Studio - Crea NPC Allenatore Pokémon"
	}(), WS_OVERLAPPEDWINDOW|WS_VISIBLE|WS_CLIPCHILDREN, x, y, ww, wh, owner, 0, hInst)
	if trainerEventWindow == 0 {
		trainerEventOpen = false
		return
	}
	setWindowIcon(trainerEventWindow)

	createWindow("STATIC", "Allenatore esistente", WS_CHILD|WS_VISIBLE, 22, 20, 150, 22, trainerEventWindow, 2650, hInst)
	hwndTrainerExisting = createWindow("COMBOBOX", "", WS_CHILD|WS_VISIBLE|WS_TABSTOP|CBS_DROPDOWNLIST, 180, 16, 675, 260, trainerEventWindow, idTrainerExisting, hInst)
	existingLabels := []string{"(Nuovo allenatore)"}
	for _, tr := range trainerEventExistingRecords {
		existingLabels = append(existingLabels, trainerRecordDisplay(tr))
	}
	setComboFromStrings(hwndTrainerExisting, existingLabels, 0)

	createWindow("STATIC", "Tipo/Classe", WS_CHILD|WS_VISIBLE, 22, 62, 150, 22, trainerEventWindow, 2651, hInst)
	hwndTrainerType = createWindow("COMBOBOX", "", WS_CHILD|WS_VISIBLE|WS_TABSTOP|CBS_DROPDOWNLIST, 180, 58, 300, 260, trainerEventWindow, idTrainerType, hInst)
	typeLabels := make([]string, 0, len(trainerEventTypes))
	for _, tt := range trainerEventTypes {
		label := tt.ID
		if tt.Name != "" && !strings.EqualFold(tt.Name, tt.ID) {
			label = tt.Name + "  [" + tt.ID + "]"
		}
		typeLabels = append(typeLabels, label)
	}
	setComboFromStrings(hwndTrainerType, typeLabels, 0)

	createWindow("STATIC", "Nome allenatore", WS_CHILD|WS_VISIBLE, 500, 62, 130, 22, trainerEventWindow, 2652, hInst)
	hwndTrainerName = createWindow("EDIT", "", WS_CHILD|WS_VISIBLE|WS_BORDER|WS_TABSTOP, 635, 58, 220, 28, trainerEventWindow, idTrainerName, hInst)

	createWindow("STATIC", "Versione", WS_CHILD|WS_VISIBLE, 22, 102, 150, 22, trainerEventWindow, 2653, hInst)
	hwndTrainerVersion = createWindow("EDIT", "0", WS_CHILD|WS_VISIBLE|WS_BORDER|WS_TABSTOP, 180, 98, 85, 28, trainerEventWindow, idTrainerVersion, hInst)

	createWindow("STATIC", "Sprite NPC", WS_CHILD|WS_VISIBLE, 290, 102, 100, 22, trainerEventWindow, 2654, hInst)
	hwndTrainerSprite = createWindow("COMBOBOX", "", WS_CHILD|WS_VISIBLE|WS_TABSTOP|CBS_DROPDOWNLIST, 390, 98, 465, 300, trainerEventWindow, idTrainerSprite, hInst)
	setComboFromStrings(hwndTrainerSprite, trainerEventSprites, 0)

	createWindow("STATIC", "Squadra Pokémon", WS_CHILD|WS_VISIBLE, 22, 145, 160, 24, trainerEventWindow, 2655, hInst)
	hwndTrainerTeam = createWindow("LISTBOX", "", WS_CHILD|WS_VISIBLE|WS_BORDER|WS_VSCROLL|WS_TABSTOP|LBS_NOTIFY|LBS_NOINTEGRALHEIGHT, 22, 175, 833, 260, trainerEventWindow, idTrainerTeam, hInst)

	createWindow("STATIC", "Pokémon", WS_CHILD|WS_VISIBLE, 22, 455, 90, 22, trainerEventWindow, 2656, hInst)
	hwndTrainerSpecies = createWindow("COMBOBOX", "", WS_CHILD|WS_VISIBLE|WS_TABSTOP|CBS_DROPDOWNLIST, 110, 450, 390, 320, trainerEventWindow, idTrainerSpecies, hInst)
	speciesLabels := make([]string, 0, len(trainerEventSpecies))
	for _, sp := range trainerEventSpecies {
		label := sp.Name
		if label == "" {
			label = sp.ID
		}
		if !strings.EqualFold(label, sp.ID) {
			label += "  [" + sp.ID + "]"
		}
		speciesLabels = append(speciesLabels, label)
	}
	setComboFromStrings(hwndTrainerSpecies, speciesLabels, 0)

	createWindow("STATIC", "Livello", WS_CHILD|WS_VISIBLE, 520, 455, 65, 22, trainerEventWindow, 2657, hInst)
	hwndTrainerLevel = createWindow("EDIT", "5", WS_CHILD|WS_VISIBLE|WS_BORDER|WS_TABSTOP, 585, 450, 70, 28, trainerEventWindow, idTrainerLevel, hInst)
	createWindow("BUTTON", "Aggiungi Pokémon", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 670, 448, 185, 32, trainerEventWindow, idTrainerAdd, hInst)
	createWindow("BUTTON", "Rimuovi selezionato", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 670, 490, 185, 32, trainerEventWindow, idTrainerRemove, hInst)

	createWindow("STATIC", "I dati allenatore vengono caricati dai file PBS/dati convertiti e gli override vengono salvati in converted/data/plm_trainers.json. La mappa conserva solo il riferimento all'allenatore e lo sprite evento.", WS_CHILD|WS_VISIBLE, 22, 530, 833, 42, trainerEventWindow, 2658, hInst)

	createWindow("BUTTON", "Salva e posiziona evento", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 580, 585, 180, 34, trainerEventWindow, idTrainerCreate, hInst)
	createWindow("BUTTON", "Annulla", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 770, 585, 85, 34, trainerEventWindow, idTrainerCancel, hInst)

	refreshTrainerTeamList()

	pEnableWindow.Call(uintptr(owner), 0)
	pShowWindow.Call(uintptr(trainerEventWindow), SW_SHOW)
	pUpdateWindow.Call(uintptr(trainerEventWindow))

	var m MSG
	repostQuit := false
	for trainerEventOpen {
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
}

func showCreateTrainerEventDialog() {
	showTrainerEventDialog(hwndMain, false)
}

func showTrainerCommandDialog(owner syscall.Handle) (TrainerRecord, bool) {
	showTrainerEventDialog(owner, true)
	trainerCommandMode = false
	if trainerCommandRecord == nil {
		return TrainerRecord{}, false
	}
	tr := *trainerCommandRecord
	trainerCommandRecord = nil
	return tr, true
}
