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

// Project-wide event systems. These commands are intentionally stored as
// PML_CMD markers: their state belongs to the save/runtime, not to one RMXP
// switch table. The registry below is the editor/runtime contract.

const (
	globalEventsPaletteClass = "PLMStudioGlobalEventsPalette01"
	globalEventDialogClass   = "PLMStudioGlobalEventDialog01"

	idGlobalEventButtonBase = 8300
	idGlobalEventsBack      = 8310

	idGlobalFieldA = 8330
	idGlobalFieldB = 8331
	idGlobalFieldC = 8332
	idGlobalFieldD = 8333
	idGlobalFieldE = 8334
	idGlobalCheck  = 8335
	idGlobalOK     = 8336
	idGlobalCancel = 8337
)

type globalEventChoice int

const (
	globalEventNone globalEventChoice = iota
	globalEventFlag
	globalEventPass
	globalEventEventTicket
	globalEventItemAccess
	globalEventShipTravel
)

type globalEventPaletteEntry struct {
	Title       string
	Description string
	Choice      globalEventChoice
}

var globalEventPaletteEntries = []globalEventPaletteEntry{
	{"Evento globale", "Flag persistente disponibile in tutte le mappe.", globalEventFlag},
	{"Pass permanente", "Pass globale riutilizzabile, ad es. TriPass.", globalEventPass},
	{"Biglietto evento", "Crea o usa un biglietto evento monouso. Usa lo stesso Ticket ID sugli NPC coinvolti.", globalEventEventTicket},
	{"Viaggio nave / trasporto", "Destinazione globale con eventuale Pass permanente richiesto.", globalEventShipTravel},
}

var globalEventsPaletteRegistered bool
var globalEventsPaletteOpen bool
var globalEventsPaletteWindow syscall.Handle
var globalEventsPaletteAccepted bool
var globalEventsPaletteResult globalEventChoice

func globalEventsPaletteWndProcFn(hwnd syscall.Handle, msg uint32, w, l uintptr) uintptr {
	switch msg {
	case WM_COMMAND:
		id := int(loword(w))
		if id >= idGlobalEventButtonBase && id < idGlobalEventButtonBase+len(globalEventPaletteEntries) {
			globalEventsPaletteResult = globalEventPaletteEntries[id-idGlobalEventButtonBase].Choice
			globalEventsPaletteAccepted = true
			pDestroyWindow.Call(uintptr(hwnd))
			return 0
		}
		if id == idGlobalEventsBack {
			pDestroyWindow.Call(uintptr(hwnd))
			return 0
		}
	case WM_CLOSE:
		pDestroyWindow.Call(uintptr(hwnd))
		return 0
	case WM_DESTROY:
		globalEventsPaletteOpen = false
		globalEventsPaletteWindow = 0
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(uintptr(hwnd), uintptr(msg), w, l)
	return r
}

var globalEventsPaletteWndProc = syscall.NewCallback(globalEventsPaletteWndProcFn)

func ensureGlobalEventsPaletteClass() error {
	if globalEventsPaletteRegistered {
		return nil
	}
	h, _, _ := pGetModuleHandleW.Call(0)
	hi := syscall.Handle(h)
	cur, _, _ := pLoadCursorW.Call(0, IDC_ARROW)
	brush, _, _ := pCreateSolidBrush.Call(rgb(244, 244, 244))
	wc := WNDCLASSEX{cbSize: uint32(unsafe.Sizeof(WNDCLASSEX{})), lpfnWndProc: globalEventsPaletteWndProc, hInstance: hi, hCursor: syscall.Handle(cur), hbrBackground: syscall.Handle(brush), lpszClassName: wstr(globalEventsPaletteClass)}
	r, _, err := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	if r == 0 {
		return fmt.Errorf("registrazione Eventi globali: %v", err)
	}
	globalEventsPaletteRegistered = true
	return nil
}

func showGlobalEventsPalette(owner syscall.Handle) (globalEventChoice, bool) {
	if globalEventsPaletteOpen {
		return globalEventNone, false
	}
	if err := ensureGlobalEventsPaletteClass(); err != nil {
		msgbox("PML Studio - Eventi globali", err.Error(), MB_OK|MB_ICONERROR)
		return globalEventNone, false
	}
	const ww, wh int32 = 760, 590
	x, y := centerOwnedWindow(ww, wh)
	h, _, _ := pGetModuleHandleW.Call(0)
	hi := syscall.Handle(h)
	globalEventsPaletteAccepted = false
	globalEventsPaletteResult = globalEventNone
	globalEventsPaletteOpen = true
	globalEventsPaletteWindow = createWindow(globalEventsPaletteClass, "Eventi globali / Viaggi", WS_OVERLAPPEDWINDOW|WS_VISIBLE|WS_CLIPCHILDREN, x, y, ww, wh, owner, 0, hi)
	if globalEventsPaletteWindow == 0 {
		globalEventsPaletteOpen = false
		return globalEventNone, false
	}
	setWindowIcon(globalEventsPaletteWindow)
	createWindow("STATIC", "Scegli il sistema globale da inserire nell'evento.", WS_CHILD|WS_VISIBLE, 32, 22, 680, 26, globalEventsPaletteWindow, 8320, hi)
	const bw, bh int32 = 320, 72
	positions := [][2]int32{{36, 64}, {388, 64}, {36, 190}, {388, 190}, {36, 316}, {388, 316}}
	for i, entry := range globalEventPaletteEntries {
		pos := positions[i]
		createWindow("BUTTON", entry.Title, WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_PUSHBUTTON, pos[0], pos[1], bw, bh, globalEventsPaletteWindow, uintptr(idGlobalEventButtonBase+i), hi)
		createWindow("STATIC", entry.Description, WS_CHILD|WS_VISIBLE, pos[0]+8, pos[1]+76, bw-16, 38, globalEventsPaletteWindow, uintptr(8321+i), hi)
	}
	createWindow("BUTTON", "← Indietro", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 36, 490, 150, 40, globalEventsPaletteWindow, idGlobalEventsBack, hi)
	createWindow("STATIC", "I Pass sono permanenti. I Biglietti evento collegano 2 NPC e NON creano Warp. Gli accessi con oggetto verificano la Borsa. I viaggi in nave possono richiedere un Pass permanente; il Volo resta regionale.", WS_CHILD|WS_VISIBLE, 210, 478, 500, 62, globalEventsPaletteWindow, 8328, hi)
	pEnableWindow.Call(uintptr(owner), 0)
	modalLoop(&globalEventsPaletteOpen)
	pEnableWindow.Call(uintptr(owner), 1)
	pSetFocus.Call(uintptr(owner))
	return globalEventsPaletteResult, globalEventsPaletteAccepted
}

// -----------------------------------------------------------------------------
// Global registry
// -----------------------------------------------------------------------------

type globalFlagDefinition struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type globalTicketDefinition struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	ItemID string `json:"item_id"`
	Kind   string `json:"kind"` // pass | event_ticket
}

type globalTravelRoute struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Transport      string `json:"transport"`
	MapID          int    `json:"map_id"`
	X              int    `json:"x"`
	Y              int    `json:"y"`
	Direction      int    `json:"direction"`
	RequiredTicket string `json:"required_ticket,omitempty"`
	ConsumeTicket  bool   `json:"consume_ticket"`
	SourceMapID    int    `json:"source_map_id"`
	SourceEventID  int    `json:"source_event_id"`
}

type globalRebattleDefinition struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	MapID          int    `json:"map_id"`
	EventID        int    `json:"event_id"`
	CooldownSteps  int    `json:"cooldown_steps"`
	MaxUses        int    `json:"max_uses"`
	RequiredGlobal string `json:"required_global,omitempty"`
}

type globalEventRegistry struct {
	Version   int                        `json:"version"`
	Flags     []globalFlagDefinition     `json:"flags"`
	Tickets   []globalTicketDefinition   `json:"tickets"`
	Routes    []globalTravelRoute        `json:"travel_routes"`
	Rebattles []globalRebattleDefinition `json:"rebattles"`
}

func globalEventRegistryPath() string {
	if strings.TrimSpace(currentProject) == "" {
		return ""
	}
	return filepath.Join(currentProject, "converted", "data", "plm_global_events.json")
}

func loadGlobalEventRegistry() globalEventRegistry {
	reg := globalEventRegistry{Version: 2, Flags: []globalFlagDefinition{}, Tickets: []globalTicketDefinition{}, Routes: []globalTravelRoute{}, Rebattles: []globalRebattleDefinition{}}
	p := globalEventRegistryPath()
	if p == "" {
		return reg
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return reg
	}
	var disk globalEventRegistry
	if json.Unmarshal(b, &disk) != nil {
		return reg
	}
	if disk.Version <= 0 {
		disk.Version = 1
	}
	if disk.Flags == nil {
		disk.Flags = []globalFlagDefinition{}
	}
	if disk.Tickets == nil {
		disk.Tickets = []globalTicketDefinition{}
	}
	for i := range disk.Tickets {
		if strings.TrimSpace(disk.Tickets[i].Kind) == "" {
			disk.Tickets[i].Kind = "pass"
		}
	}
	if disk.Routes == nil {
		disk.Routes = []globalTravelRoute{}
	}
	if disk.Rebattles == nil {
		disk.Rebattles = []globalRebattleDefinition{}
	}
	return disk
}

func saveGlobalEventRegistry(reg globalEventRegistry) error {
	p := globalEventRegistryPath()
	if p == "" {
		return fmt.Errorf("progetto non aperto")
	}
	reg.Version = 2
	sort.SliceStable(reg.Flags, func(i, j int) bool { return strings.ToLower(reg.Flags[i].ID) < strings.ToLower(reg.Flags[j].ID) })
	sort.SliceStable(reg.Tickets, func(i, j int) bool { return strings.ToLower(reg.Tickets[i].ID) < strings.ToLower(reg.Tickets[j].ID) })
	sort.SliceStable(reg.Routes, func(i, j int) bool {
		if reg.Routes[i].SourceMapID != reg.Routes[j].SourceMapID {
			return reg.Routes[i].SourceMapID < reg.Routes[j].SourceMapID
		}
		return reg.Routes[i].SourceEventID < reg.Routes[j].SourceEventID
	})
	sort.SliceStable(reg.Rebattles, func(i, j int) bool {
		if reg.Rebattles[i].MapID != reg.Rebattles[j].MapID {
			return reg.Rebattles[i].MapID < reg.Rebattles[j].MapID
		}
		return reg.Rebattles[i].EventID < reg.Rebattles[j].EventID
	})
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(reg, "", "  ")
	if err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0644); err != nil {
		return err
	}
	if err := os.Rename(tmp, p); err != nil {
		return err
	}
	// Installing the runtime bridge here keeps editor data and executable
	// behavior in sync immediately after the first global command is saved.
	if err := ensureGlobalEventsRuntimeBridge(currentProject); err != nil {
		return err
	}
	return nil
}

func upsertGlobalFlagDefinition(id, name string) error {
	id = normalizeGlobalID(id)
	name = strings.TrimSpace(name)
	reg := loadGlobalEventRegistry()
	found := false
	for i := range reg.Flags {
		if strings.EqualFold(reg.Flags[i].ID, id) {
			reg.Flags[i].ID = id
			if name != "" {
				reg.Flags[i].Name = name
			}
			found = true
			break
		}
	}
	if !found {
		reg.Flags = append(reg.Flags, globalFlagDefinition{ID: id, Name: name})
	}
	return saveGlobalEventRegistry(reg)
}

func upsertGlobalTicketDefinition(id, name, itemID, kind string) error {
	id = normalizeGlobalID(id)
	itemID = strings.ToUpper(strings.TrimSpace(itemID))
	name = strings.TrimSpace(name)
	kind = normalizeTicketKind(kind)
	reg := loadGlobalEventRegistry()
	found := false
	for i := range reg.Tickets {
		if strings.EqualFold(reg.Tickets[i].ID, id) {
			reg.Tickets[i] = globalTicketDefinition{ID: id, Name: name, ItemID: itemID, Kind: kind}
			found = true
			break
		}
	}
	if !found {
		reg.Tickets = append(reg.Tickets, globalTicketDefinition{ID: id, Name: name, ItemID: itemID, Kind: kind})
	}
	return saveGlobalEventRegistry(reg)
}

func normalizeTicketKind(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "event_ticket", "ticket", "biglietto_evento":
		return "event_ticket"
	default:
		return "pass"
	}
}

func ticketKindLabel(kind string) string {
	if normalizeTicketKind(kind) == "event_ticket" {
		return "BIGLIETTO EVENTO"
	}
	return "PASS"
}

func globalTicketKindByID(reg globalEventRegistry, id string) string {
	id = normalizeGlobalID(id)
	for _, t := range reg.Tickets {
		if strings.EqualFold(t.ID, id) {
			return normalizeTicketKind(t.Kind)
		}
	}
	return "pass"
}

func normalizeGlobalID(s string) string {
	s = strings.ToUpper(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
		} else if r == ' ' || r == '-' || r == '/' {
			if b.Len() > 0 && !strings.HasSuffix(b.String(), "_") {
				b.WriteByte('_')
			}
		}
	}
	return strings.Trim(b.String(), "_")
}

func globalTicketActionLabel(action string) string {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "grant":
		return "dai/sblocca"
	case "require":
		return "richiedi"
	case "consume":
		return "consuma/rimuovi"
	default:
		return strings.TrimSpace(action)
	}
}

func collectManagedCommands(v any, out *[]plmManagedEventCommand) {
	switch t := v.(type) {
	case map[string]any:
		if commandMapCode(t) == 108 {
			if c, ok := parsePLMEventCommandMarker(commandFirstParam(t)); ok {
				*out = append(*out, c)
			}
		}
		for _, child := range t {
			collectManagedCommands(child, out)
		}
	case []any:
		for _, child := range t {
			collectManagedCommands(child, out)
		}
	}
}

func managedCommandsFromEditorEvent(e EditorEvent) []plmManagedEventCommand {
	if len(e.NativeRaw) == 0 {
		return nil
	}
	v, err := decodeJSONAny(e.NativeRaw)
	if err != nil {
		return nil
	}
	var out []plmManagedEventCommand
	collectManagedCommands(v, &out)
	return out
}

func syncGlobalEventRegistryForCurrentMap() error {
	if currentMap == nil || strings.TrimSpace(currentProject) == "" {
		return nil
	}
	reg := loadGlobalEventRegistry()
	routes := make([]globalTravelRoute, 0, len(reg.Routes)+4)
	for _, r := range reg.Routes {
		if r.SourceMapID != currentMap.ID {
			routes = append(routes, r)
		}
	}
	rebattles := make([]globalRebattleDefinition, 0, len(reg.Rebattles)+4)
	for _, r := range reg.Rebattles {
		if r.MapID != currentMap.ID {
			rebattles = append(rebattles, r)
		}
	}

	for _, e := range events {
		for _, c := range managedCommandsFromEditorEvent(e) {
			switch c.Type {
			case "ship_travel":
				routeID := normalizeGlobalID(c.RouteID)
				if routeID == "" {
					routeID = fmt.Sprintf("TRAVEL_%03d_%03d", currentMap.ID, e.ID)
				}
				name := strings.TrimSpace(c.RouteName)
				if name == "" {
					name = fmt.Sprintf("Viaggio Map%03d", c.MapID)
				}
				routes = append(routes, globalTravelRoute{ID: routeID, Name: name, Transport: strings.TrimSpace(c.Transport), MapID: c.MapID, X: c.X, Y: c.Y, Direction: c.Direction, RequiredTicket: normalizeGlobalID(c.RequiredTicket), ConsumeTicket: c.Consume, SourceMapID: currentMap.ID, SourceEventID: e.ID})
			case "rebattle":
				id := normalizeGlobalID(c.GlobalID)
				if id == "" {
					id = fmt.Sprintf("REBATTLE_%03d_%03d", currentMap.ID, e.ID)
				}
				name := strings.TrimSpace(c.GlobalName)
				if name == "" {
					name = strings.TrimSpace(e.Name)
				}
				rebattles = append(rebattles, globalRebattleDefinition{ID: id, Name: name, MapID: currentMap.ID, EventID: e.ID, CooldownSteps: maxIntEventCmd(c.CooldownSteps, 0), MaxUses: c.MaxUses, RequiredGlobal: normalizeGlobalID(c.RequiredGlobal)})
			}
		}
	}
	reg.Routes = routes
	reg.Rebattles = rebattles
	return saveGlobalEventRegistry(reg)
}

// -----------------------------------------------------------------------------
// Generic command dialog
// -----------------------------------------------------------------------------

type globalDialogMode int

const (
	globalDialogFlag globalDialogMode = iota
	globalDialogTicketMeta
	globalDialogTravelMeta
	globalDialogRebattle
)

var globalDialogRegistered bool
var globalDialogOpen bool
var globalDialogWindow syscall.Handle
var globalDialogModeValue globalDialogMode
var globalFieldA, globalFieldB, globalFieldC, globalFieldD, globalFieldE syscall.Handle
var globalCheck syscall.Handle
var globalDialogAccepted bool
var globalDialogResult plmManagedEventCommand
var globalDialogTicketItem string
var globalDialogTicketKind string
var globalDialogTravelMap, globalDialogTravelX, globalDialogTravelY, globalDialogTravelDir int
var globalDialogTicketIDs []string
var globalDialogFlagIDs []string

func globalDialogWndProcFn(hwnd syscall.Handle, msg uint32, w, l uintptr) uintptr {
	switch msg {
	case WM_COMMAND:
		switch int(loword(w)) {
		case idGlobalOK:
			if commitGlobalDialog() {
				pDestroyWindow.Call(uintptr(hwnd))
			}
			return 0
		case idGlobalCancel:
			pDestroyWindow.Call(uintptr(hwnd))
			return 0
		}
	case WM_CLOSE:
		pDestroyWindow.Call(uintptr(hwnd))
		return 0
	case WM_DESTROY:
		globalDialogOpen = false
		globalDialogWindow = 0
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(uintptr(hwnd), uintptr(msg), w, l)
	return r
}

var globalDialogWndProc = syscall.NewCallback(globalDialogWndProcFn)

func ensureGlobalDialogClass() error {
	if globalDialogRegistered {
		return nil
	}
	h, _, _ := pGetModuleHandleW.Call(0)
	hi := syscall.Handle(h)
	cur, _, _ := pLoadCursorW.Call(0, IDC_ARROW)
	brush, _, _ := pCreateSolidBrush.Call(rgb(244, 244, 244))
	wc := WNDCLASSEX{cbSize: uint32(unsafe.Sizeof(WNDCLASSEX{})), lpfnWndProc: globalDialogWndProc, hInstance: hi, hCursor: syscall.Handle(cur), hbrBackground: syscall.Handle(brush), lpszClassName: wstr(globalEventDialogClass)}
	r, _, err := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	if r == 0 {
		return fmt.Errorf("registrazione dialogo eventi globali: %v", err)
	}
	globalDialogRegistered = true
	return nil
}

func beginGlobalDialog(owner syscall.Handle, title string, ww, wh int32, create func(syscall.Handle)) bool {
	if globalDialogOpen {
		return false
	}
	if err := ensureGlobalDialogClass(); err != nil {
		msgbox("PML Studio - Eventi globali", err.Error(), MB_OK|MB_ICONERROR)
		return false
	}
	x, y := centerOwnedWindow(ww, wh)
	h, _, _ := pGetModuleHandleW.Call(0)
	hi := syscall.Handle(h)
	globalDialogAccepted = false
	globalDialogResult = plmManagedEventCommand{}
	globalDialogOpen = true
	globalDialogWindow = createWindow(globalEventDialogClass, title, WS_OVERLAPPEDWINDOW|WS_VISIBLE|WS_CLIPCHILDREN, x, y, ww, wh, owner, 0, hi)
	if globalDialogWindow == 0 {
		globalDialogOpen = false
		return false
	}
	setWindowIcon(globalDialogWindow)
	create(hi)
	pEnableWindow.Call(uintptr(owner), 0)
	modalLoop(&globalDialogOpen)
	pEnableWindow.Call(uintptr(owner), 1)
	pSetFocus.Call(uintptr(owner))
	return globalDialogAccepted
}

func commitGlobalDialog() bool {
	switch globalDialogModeValue {
	case globalDialogFlag:
		id := normalizeGlobalID(getText(globalFieldA))
		name := strings.TrimSpace(getText(globalFieldB))
		if id == "" {
			msgbox("PML Studio - Evento globale", "Inserisci un ID globale valido.", MB_OK|MB_ICONINFORMATION)
			return false
		}
		if name == "" {
			name = id
		}
		actions := []string{"on", "off", "toggle", "require_on", "require_off"}
		idx := comboSel(globalFieldC)
		if idx < 0 || idx >= len(actions) {
			idx = 0
		}
		globalDialogResult = plmManagedEventCommand{Type: "global_flag", GlobalID: id, GlobalName: name, Action: actions[idx]}
	case globalDialogTicketMeta:
		id := normalizeGlobalID(getText(globalFieldA))
		name := strings.TrimSpace(getText(globalFieldB))
		if id == "" {
			id = normalizeGlobalID(globalDialogTicketItem)
		}
		if id == "" {
			msgbox("PML Studio - Biglietto/Pass", "Inserisci un ID del pass.", MB_OK|MB_ICONINFORMATION)
			return false
		}
		if name == "" {
			name = eventItemDisplayName(globalDialogTicketItem)
		}
		kind := normalizeTicketKind(globalDialogTicketKind)
		actions := []string{"grant", "require"}
		if kind == "event_ticket" {
			actions = append(actions, "consume")
		}
		idx := comboSel(globalFieldC)
		if idx < 0 || idx >= len(actions) {
			idx = 0
		}
		globalDialogResult = plmManagedEventCommand{Type: "global_ticket", TicketID: id, TicketName: name, TicketKind: kind, Item: strings.ToUpper(strings.TrimSpace(globalDialogTicketItem)), Action: actions[idx]}
	case globalDialogTravelMeta:
		routeID := normalizeGlobalID(getText(globalFieldA))
		name := strings.TrimSpace(getText(globalFieldB))
		if routeID == "" {
			routeID = fmt.Sprintf("SHIP_%03d_%d_%d", globalDialogTravelMap, globalDialogTravelX, globalDialogTravelY)
		}
		if name == "" {
			name = "Viaggio per " + eventWarpMapName(globalDialogTravelMap)
		}
		req := ""
		idx := comboSel(globalFieldC)
		if idx > 0 && idx-1 < len(globalDialogTicketIDs) {
			req = globalDialogTicketIDs[idx-1]
		}
		transport := strings.TrimSpace(getText(globalFieldD))
		if transport == "" {
			transport = "Nave"
		}
		reg := loadGlobalEventRegistry()
		consume := req != "" && globalTicketKindByID(reg, req) == "event_ticket"
		globalDialogResult = plmManagedEventCommand{Type: "ship_travel", RouteID: routeID, RouteName: name, Transport: transport, MapID: globalDialogTravelMap, X: globalDialogTravelX, Y: globalDialogTravelY, Direction: globalDialogTravelDir, RequiredTicket: req, Consume: consume}
	case globalDialogRebattle:
		id := normalizeGlobalID(getText(globalFieldA))
		name := strings.TrimSpace(getText(globalFieldB))
		if id == "" {
			if currentMap != nil {
				id = fmt.Sprintf("REBATTLE_%03d", currentMap.ID)
			} else {
				id = "REBATTLE"
			}
		}
		if name == "" {
			name = strings.TrimSpace(getText(eventEditorName))
			if name == "" {
				name = id
			}
		}
		steps, _ := strconv.Atoi(strings.TrimSpace(getText(globalFieldC)))
		if steps < 0 {
			steps = 0
		}
		maxUses, _ := strconv.Atoi(strings.TrimSpace(getText(globalFieldD)))
		if maxUses < 0 {
			maxUses = 0
		}
		req := ""
		idx := comboSel(globalFieldE)
		if idx > 0 && idx-1 < len(globalDialogFlagIDs) {
			req = globalDialogFlagIDs[idx-1]
		}
		globalDialogResult = plmManagedEventCommand{Type: "rebattle", GlobalID: id, GlobalName: name, CooldownSteps: steps, MaxUses: maxUses, RequiredGlobal: req}
	}
	globalDialogAccepted = true
	return true
}

func showGlobalFlagDialog(owner syscall.Handle) (plmManagedEventCommand, bool) {
	globalDialogModeValue = globalDialogFlag
	ok := beginGlobalDialog(owner, "Evento globale persistente", 690, 390, func(hi syscall.Handle) {
		createWindow("STATIC", "ID globale:", WS_CHILD|WS_VISIBLE, 28, 32, 140, 24, globalDialogWindow, 8340, hi)
		globalFieldA = createWindow("EDIT", "", WS_CHILD|WS_VISIBLE|WS_BORDER|WS_TABSTOP, 175, 27, 445, 30, globalDialogWindow, idGlobalFieldA, hi)
		createWindow("STATIC", "Nome:", WS_CHILD|WS_VISIBLE, 28, 82, 140, 24, globalDialogWindow, 8341, hi)
		globalFieldB = createWindow("EDIT", "", WS_CHILD|WS_VISIBLE|WS_BORDER|WS_TABSTOP, 175, 77, 445, 30, globalDialogWindow, idGlobalFieldB, hi)
		createWindow("STATIC", "Azione:", WS_CHILD|WS_VISIBLE, 28, 132, 140, 24, globalDialogWindow, 8342, hi)
		globalFieldC = createWindow("COMBOBOX", "", WS_CHILD|WS_VISIBLE|WS_TABSTOP|CBS_DROPDOWNLIST, 175, 127, 300, 180, globalDialogWindow, idGlobalFieldC, hi)
		for _, s := range []string{"Imposta ON", "Imposta OFF", "Inverti ON/OFF", "Richiedi ON per continuare", "Richiedi OFF per continuare"} {
			comboAdd(globalFieldC, s)
		}
		comboSelectIndex(globalFieldC, 0)
		createWindow("STATIC", "È uno stato persistente del salvataggio, accessibile da qualsiasi mappa. Usalo per eventi mondiali, accessi, capitoli e sblocchi.", WS_CHILD|WS_VISIBLE, 28, 190, 590, 62, globalDialogWindow, 8343, hi)
		createWindow("BUTTON", "Salva", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 390, 286, 105, 38, globalDialogWindow, idGlobalOK, hi)
		createWindow("BUTTON", "Annulla", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 510, 286, 105, 38, globalDialogWindow, idGlobalCancel, hi)
	})
	return globalDialogResult, ok
}

func showGlobalTicketMetaDialog(owner syscall.Handle, itemID, kind string) (plmManagedEventCommand, bool) {
	globalDialogModeValue = globalDialogTicketMeta
	globalDialogTicketItem = strings.ToUpper(strings.TrimSpace(itemID))
	globalDialogTicketKind = normalizeTicketKind(kind)
	defaultName := eventItemDisplayName(globalDialogTicketItem)
	title := "Pass permanente globale"
	if globalDialogTicketKind == "event_ticket" {
		title = "Biglietto evento monouso"
	}
	ok := beginGlobalDialog(owner, title, 720, 450, func(hi syscall.Handle) {
		createWindow("STATIC", "Oggetto reale:", WS_CHILD|WS_VISIBLE, 28, 28, 150, 24, globalDialogWindow, 8350, hi)
		createWindow("STATIC", fmt.Sprintf("%s [%s]", defaultName, globalDialogTicketItem), WS_CHILD|WS_VISIBLE|WS_BORDER, 182, 24, 470, 28, globalDialogWindow, 8351, hi)
		createWindow("STATIC", "ID globale:", WS_CHILD|WS_VISIBLE, 28, 78, 150, 24, globalDialogWindow, 8352, hi)
		globalFieldA = createWindow("EDIT", normalizeGlobalID(globalDialogTicketItem), WS_CHILD|WS_VISIBLE|WS_BORDER|WS_TABSTOP, 182, 73, 470, 30, globalDialogWindow, idGlobalFieldA, hi)
		createWindow("STATIC", "Nome:", WS_CHILD|WS_VISIBLE, 28, 128, 150, 24, globalDialogWindow, 8353, hi)
		globalFieldB = createWindow("EDIT", defaultName, WS_CHILD|WS_VISIBLE|WS_BORDER|WS_TABSTOP, 182, 123, 470, 30, globalDialogWindow, idGlobalFieldB, hi)
		createWindow("STATIC", "Azione evento:", WS_CHILD|WS_VISIBLE, 28, 178, 150, 24, globalDialogWindow, 8354, hi)
		globalFieldC = createWindow("COMBOBOX", "", WS_CHILD|WS_VISIBLE|WS_TABSTOP|CBS_DROPDOWNLIST, 182, 173, 340, 160, globalDialogWindow, idGlobalFieldC, hi)
		if globalDialogTicketKind == "event_ticket" {
			for _, s := range []string{"Dai il Biglietto evento", "Richiedi il Biglietto evento", "Consuma / riscatta definitivamente"} {
				comboAdd(globalFieldC, s)
			}
			createWindow("STATIC", "MONOUSO: al primo riscatto viene rimosso dall'inventario e marcato come già utilizzato nel salvataggio. Lo stesso Ticket ID non può essere usato una seconda volta.", WS_CHILD|WS_VISIBLE, 28, 232, 620, 72, globalDialogWindow, 8355, hi)
		} else {
			for _, s := range []string{"Dai / sblocca il Pass", "Richiedi il Pass per continuare"} {
				comboAdd(globalFieldC, s)
			}
			createWindow("STATIC", "PERMANENTE: esempio TriPass. Una volta ottenuto resta riutilizzabile e i viaggi non lo consumano.", WS_CHILD|WS_VISIBLE, 28, 232, 620, 62, globalDialogWindow, 8355, hi)
		}
		comboSelectIndex(globalFieldC, 0)
		createWindow("BUTTON", "Salva", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 420, 340, 105, 38, globalDialogWindow, idGlobalOK, hi)
		createWindow("BUTTON", "Annulla", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 540, 340, 105, 38, globalDialogWindow, idGlobalCancel, hi)
	})
	return globalDialogResult, ok
}

func showGlobalTravelMetaDialog(owner syscall.Handle, mapID, x, y, dir int) (plmManagedEventCommand, bool) {
	globalDialogModeValue = globalDialogTravelMeta
	globalDialogTravelMap, globalDialogTravelX, globalDialogTravelY, globalDialogTravelDir = mapID, x, y, dir
	reg := loadGlobalEventRegistry()
	globalDialogTicketIDs = globalDialogTicketIDs[:0]
	ok := beginGlobalDialog(owner, "Viaggio nave / trasporto", 760, 510, func(hi syscall.Handle) {
		createWindow("STATIC", "Destinazione:", WS_CHILD|WS_VISIBLE, 28, 28, 140, 24, globalDialogWindow, 8360, hi)
		createWindow("STATIC", fmt.Sprintf("Map%03d - %s · (%d,%d)", mapID, eventWarpMapName(mapID), x, y), WS_CHILD|WS_VISIBLE|WS_BORDER, 175, 24, 515, 28, globalDialogWindow, 8361, hi)
		createWindow("STATIC", "ID rotta:", WS_CHILD|WS_VISIBLE, 28, 78, 140, 24, globalDialogWindow, 8362, hi)
		globalFieldA = createWindow("EDIT", fmt.Sprintf("SHIP_%03d_%d_%d", mapID, x, y), WS_CHILD|WS_VISIBLE|WS_BORDER|WS_TABSTOP, 175, 73, 515, 30, globalDialogWindow, idGlobalFieldA, hi)
		createWindow("STATIC", "Nome rotta:", WS_CHILD|WS_VISIBLE, 28, 128, 140, 24, globalDialogWindow, 8363, hi)
		globalFieldB = createWindow("EDIT", "Viaggio per "+eventWarpMapName(mapID), WS_CHILD|WS_VISIBLE|WS_BORDER|WS_TABSTOP, 175, 123, 515, 30, globalDialogWindow, idGlobalFieldB, hi)
		createWindow("STATIC", "Pass richiesto:", WS_CHILD|WS_VISIBLE, 28, 178, 140, 24, globalDialogWindow, 8364, hi)
		globalFieldC = createWindow("COMBOBOX", "", WS_CHILD|WS_VISIBLE|WS_TABSTOP|CBS_DROPDOWNLIST|WS_VSCROLL, 175, 173, 515, 220, globalDialogWindow, idGlobalFieldC, hi)
		comboAdd(globalFieldC, "Nessun Pass richiesto")
		for _, t := range reg.Tickets {
			if normalizeTicketKind(t.Kind) != "pass" {
				continue
			}
			label := strings.TrimSpace(t.Name)
			if label == "" {
				label = t.ID
			}
			comboAdd(globalFieldC, fmt.Sprintf("%s [%s · %s]", label, t.ID, ticketKindLabel(t.Kind)))
			globalDialogTicketIDs = append(globalDialogTicketIDs, t.ID)
		}
		comboSelectIndex(globalFieldC, 0)
		createWindow("STATIC", "Trasporto:", WS_CHILD|WS_VISIBLE, 28, 228, 140, 24, globalDialogWindow, 8365, hi)
		globalFieldD = createWindow("EDIT", "Nave", WS_CHILD|WS_VISIBLE|WS_BORDER|WS_TABSTOP, 175, 223, 260, 30, globalDialogWindow, idGlobalFieldD, hi)
		globalCheck = 0
		createWindow("STATIC", "Il viaggio può richiedere solo un Pass permanente e riutilizzabile. I Biglietti evento non creano viaggi/Warp: vengono usati esclusivamente dai controlli accesso tra NPC. Il trasporto può collegare regioni diverse; il Volo resta regionale.", WS_CHILD|WS_VISIBLE, 28, 276, 660, 76, globalDialogWindow, 8366, hi)
		createWindow("BUTTON", "Crea viaggio", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 438, 405, 120, 38, globalDialogWindow, idGlobalOK, hi)
		createWindow("BUTTON", "Annulla", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 570, 405, 120, 38, globalDialogWindow, idGlobalCancel, hi)
	})
	return globalDialogResult, ok
}

func showGlobalRebattleDialog(owner syscall.Handle) (plmManagedEventCommand, bool) {
	globalDialogModeValue = globalDialogRebattle
	reg := loadGlobalEventRegistry()
	globalDialogFlagIDs = globalDialogFlagIDs[:0]
	defaultName := strings.TrimSpace(getText(eventEditorName))
	if defaultName == "" {
		defaultName = "Rebattle"
	}
	defaultID := "REBATTLE"
	if currentMap != nil {
		defaultID = fmt.Sprintf("REBATTLE_%03d", currentMap.ID)
	}
	ok := beginGlobalDialog(owner, "Rebattle / Rematch", 760, 520, func(hi syscall.Handle) {
		createWindow("STATIC", "ID persistente:", WS_CHILD|WS_VISIBLE, 28, 28, 150, 24, globalDialogWindow, 8370, hi)
		globalFieldA = createWindow("EDIT", defaultID, WS_CHILD|WS_VISIBLE|WS_BORDER|WS_TABSTOP, 185, 23, 500, 30, globalDialogWindow, idGlobalFieldA, hi)
		createWindow("STATIC", "Nome:", WS_CHILD|WS_VISIBLE, 28, 78, 150, 24, globalDialogWindow, 8371, hi)
		globalFieldB = createWindow("EDIT", defaultName, WS_CHILD|WS_VISIBLE|WS_BORDER|WS_TABSTOP, 185, 73, 500, 30, globalDialogWindow, idGlobalFieldB, hi)
		createWindow("STATIC", "Cooldown passi:", WS_CHILD|WS_VISIBLE, 28, 128, 150, 24, globalDialogWindow, 8372, hi)
		globalFieldC = createWindow("EDIT", "100", WS_CHILD|WS_VISIBLE|WS_BORDER|WS_TABSTOP, 185, 123, 160, 30, globalDialogWindow, idGlobalFieldC, hi)
		createWindow("STATIC", "Max rematch:", WS_CHILD|WS_VISIBLE, 375, 128, 130, 24, globalDialogWindow, 8373, hi)
		globalFieldD = createWindow("EDIT", "0", WS_CHILD|WS_VISIBLE|WS_BORDER|WS_TABSTOP, 510, 123, 175, 30, globalDialogWindow, idGlobalFieldD, hi)
		createWindow("STATIC", "0 = illimitati", WS_CHILD|WS_VISIBLE, 510, 156, 175, 22, globalDialogWindow, 8374, hi)
		createWindow("STATIC", "Richiede evento globale:", WS_CHILD|WS_VISIBLE, 28, 205, 150, 24, globalDialogWindow, 8375, hi)
		globalFieldE = createWindow("COMBOBOX", "", WS_CHILD|WS_VISIBLE|WS_TABSTOP|CBS_DROPDOWNLIST|WS_VSCROLL, 185, 200, 500, 220, globalDialogWindow, idGlobalFieldE, hi)
		comboAdd(globalFieldE, "Nessun requisito globale")
		for _, f := range reg.Flags {
			label := strings.TrimSpace(f.Name)
			if label == "" {
				label = f.ID
			}
			comboAdd(globalFieldE, fmt.Sprintf("%s [%s]", label, f.ID))
			globalDialogFlagIDs = append(globalDialogFlagIDs, f.ID)
		}
		comboSelectIndex(globalFieldE, 0)
		createWindow("STATIC", "Il runtime conserverà vittorie/rematch per Map ID + Event ID. Il cooldown è globale e persistente nel salvataggio; 100 passi è il valore iniziale consigliato, ma puoi cambiarlo.", WS_CHILD|WS_VISIBLE, 28, 270, 660, 72, globalDialogWindow, 8376, hi)
		createWindow("BUTTON", "Abilita Rebattle", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 410, 397, 145, 38, globalDialogWindow, idGlobalOK, hi)
		createWindow("BUTTON", "Annulla", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 570, 397, 115, 38, globalDialogWindow, idGlobalCancel, hi)
	})
	return globalDialogResult, ok
}

// -----------------------------------------------------------------------------
// Public command entry points
// -----------------------------------------------------------------------------

func addGlobalFlagCommand() {
	c, ok := showGlobalFlagDialog(eventEditorWindow)
	if !ok {
		return
	}
	if err := upsertGlobalFlagDefinition(c.GlobalID, c.GlobalName); err != nil {
		msgbox("PML Studio - Evento globale", "Impossibile aggiornare il registro globale:\r\n\r\n"+err.Error(), MB_OK|MB_ICONERROR)
		return
	}
	if appendManagedEventCommand(c) {
		setToolbarStatus("Comando aggiunto: Evento globale " + c.GlobalID + ".")
	}
}

func addGlobalTicketCommand(kind string) {
	itemID, _, _, ok := showEventCommandPickerDialog(eventEditorWindow, eventCommandGiveItem)
	if !ok || strings.TrimSpace(itemID) == "" {
		return
	}
	kind = normalizeTicketKind(kind)
	c, ok := showGlobalTicketMetaDialog(eventEditorWindow, itemID, kind)
	if !ok {
		return
	}
	if err := upsertGlobalTicketDefinition(c.TicketID, c.TicketName, c.Item, c.TicketKind); err != nil {
		msgbox("PML Studio - Biglietti/Pass", "Impossibile aggiornare il registro globale:\r\n\r\n"+err.Error(), MB_OK|MB_ICONERROR)
		return
	}
	if appendManagedEventCommand(c) {
		setToolbarStatus("Comando aggiunto: " + ticketKindLabel(c.TicketKind) + " " + c.TicketID + " · " + globalTicketActionLabel(c.Action) + ".")
	}
}

func addShipTravelCommand() {
	mapID, x, y, dir, ok := showEventWarpDialog(eventEditorWindow)
	if !ok {
		return
	}
	c, ok := showGlobalTravelMetaDialog(eventEditorWindow, mapID, x, y, dir)
	if !ok {
		return
	}
	if appendManagedEventCommand(c) {
		setToolbarStatus("Comando aggiunto: viaggio " + c.Transport + " → " + eventWarpMapName(c.MapID) + ".")
	}
}

func addRebattleCommand() {
	c, ok := showGlobalRebattleDialog(eventEditorWindow)
	if !ok {
		return
	}
	if appendManagedEventCommand(c) {
		setToolbarStatus(fmt.Sprintf("Comando aggiunto: Rebattle/Rematch · cooldown %d passi.", c.CooldownSteps))
	}
}

func addGlobalEventsCommand() {
	for {
		choice, ok := showGlobalEventsPalette(eventEditorWindow)
		if !ok {
			return
		}
		switch choice {
		case globalEventFlag:
			addGlobalFlagCommand()
		case globalEventPass:
			addGlobalTicketCommand("pass")
		case globalEventEventTicket:
			addGlobalTicketCommand("event_ticket")
		case globalEventShipTravel:
			addShipTravelCommand()
		default:
			return
		}
	}
}
