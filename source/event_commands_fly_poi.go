//go:build windows

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"unsafe"
)

// flyPOIPoint is the canonical PLM Studio representation of a Fly landing POI.
// A point always belongs to the same region as its physical map. Cross-region
// Fly is deliberately disabled at registry level, not left to UI convention.
type flyPOIPoint struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	RegionID      string `json:"region_id"`
	MapID         int    `json:"map_id"`
	X             int    `json:"x"`
	Y             int    `json:"y"`
	SourceEventID int    `json:"source_event_id"`
	Unlocked      bool   `json:"unlocked"`
}

type flyPOIRegistry struct {
	Version          int           `json:"version"`
	AllowCrossRegion bool          `json:"allow_cross_region"`
	Points           []flyPOIPoint `json:"points"`
}

func flyPOIRegistryPath() string {
	if strings.TrimSpace(currentProject) == "" {
		return ""
	}
	return filepath.Join(currentProject, "converted", "data", "plm_fly_points.json")
}

func loadFlyPOIRegistry() flyPOIRegistry {
	reg := flyPOIRegistry{Version: 1, AllowCrossRegion: false, Points: []flyPOIPoint{}}
	p := flyPOIRegistryPath()
	if p == "" {
		return reg
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return reg
	}
	var disk flyPOIRegistry
	if json.Unmarshal(b, &disk) != nil {
		return reg
	}
	if disk.Version <= 0 {
		disk.Version = 1
	}
	// This project intentionally forbids inter-region Fly. Even a legacy file
	// cannot silently turn the restriction back on.
	disk.AllowCrossRegion = false
	if disk.Points == nil {
		disk.Points = []flyPOIPoint{}
	}
	return disk
}

func saveFlyPOIRegistry(reg flyPOIRegistry) error {
	p := flyPOIRegistryPath()
	if p == "" {
		return fmt.Errorf("progetto non aperto")
	}
	reg.Version = 1
	reg.AllowCrossRegion = false
	sort.SliceStable(reg.Points, func(i, j int) bool {
		if !strings.EqualFold(reg.Points[i].RegionID, reg.Points[j].RegionID) {
			return strings.ToLower(reg.Points[i].RegionID) < strings.ToLower(reg.Points[j].RegionID)
		}
		if reg.Points[i].MapID != reg.Points[j].MapID {
			return reg.Points[i].MapID < reg.Points[j].MapID
		}
		return reg.Points[i].SourceEventID < reg.Points[j].SourceEventID
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
	return os.Rename(tmp, p)
}

func mapRegionIDByMapID(mapID int) string {
	for i := range maps {
		if maps[i].ID == mapID {
			return strings.TrimSpace(maps[i].RegionID)
		}
	}
	if currentMap != nil && currentMap.ID == mapID {
		return strings.TrimSpace(currentMap.RegionID)
	}
	return ""
}

// flyPOIsForSourceMap is the selector contract the Python runtime can mirror:
// only points from the source map's current region are returned.
func flyPOIsForSourceMap(sourceMapID int) []flyPOIPoint {
	sourceRegion := mapRegionIDByMapID(sourceMapID)
	if sourceRegion == "" {
		return nil
	}
	reg := loadFlyPOIRegistry()
	out := make([]flyPOIPoint, 0, len(reg.Points))
	for _, p := range reg.Points {
		if strings.EqualFold(strings.TrimSpace(p.RegionID), sourceRegion) {
			out = append(out, p)
		}
	}
	return out
}

func flyTargetAllowed(sourceMapID, targetMapID int) bool {
	sr := mapRegionIDByMapID(sourceMapID)
	tr := mapRegionIDByMapID(targetMapID)
	return sr != "" && tr != "" && strings.EqualFold(sr, tr)
}

func flyPOICommandFromValue(v any) (plmManagedEventCommand, bool) {
	switch t := v.(type) {
	case map[string]any:
		if commandMapCode(t) == 108 {
			if c, ok := parsePLMEventCommandMarker(commandFirstParam(t)); ok && c.Type == "fly_poi" {
				return c, true
			}
		}
		for _, child := range t {
			if c, ok := flyPOICommandFromValue(child); ok {
				return c, true
			}
		}
	case []any:
		for _, child := range t {
			if c, ok := flyPOICommandFromValue(child); ok {
				return c, true
			}
		}
	}
	return plmManagedEventCommand{}, false
}

func flyPOICommandFromEditorEvent(e EditorEvent) (plmManagedEventCommand, bool) {
	if len(e.NativeRaw) == 0 {
		return plmManagedEventCommand{}, false
	}
	v, err := decodeJSONAny(e.NativeRaw)
	if err != nil {
		return plmManagedEventCommand{}, false
	}
	return flyPOICommandFromValue(v)
}

// syncFlyPOIsForCurrentMap makes the event marker the source of truth. It also
// fixes stale region IDs if a map is later reassigned to another region.
func syncFlyPOIsForCurrentMap() error {
	if currentMap == nil || strings.TrimSpace(currentProject) == "" {
		return nil
	}
	reg := loadFlyPOIRegistry()
	kept := make([]flyPOIPoint, 0, len(reg.Points)+4)
	for _, p := range reg.Points {
		if p.MapID != currentMap.ID {
			kept = append(kept, p)
		}
	}

	regionID := strings.TrimSpace(currentMap.RegionID)
	if regionID != "" {
		for _, e := range events {
			c, ok := flyPOICommandFromEditorEvent(e)
			if !ok {
				continue
			}
			name := strings.TrimSpace(c.POIName)
			if name == "" {
				name = strings.TrimSpace(e.Name)
			}
			if name == "" {
				name = fmt.Sprintf("Punto Volo Map%03d", currentMap.ID)
			}
			rid := strings.ToLower(strings.TrimSpace(regionID))
			kept = append(kept, flyPOIPoint{
				ID:            fmt.Sprintf("fly_%s_%03d_%03d", rid, currentMap.ID, e.ID),
				Name:          name,
				RegionID:      regionID,
				MapID:         currentMap.ID,
				X:             e.X,
				Y:             e.Y,
				SourceEventID: e.ID,
				Unlocked:      c.Unlocked,
			})
		}
	}
	reg.Points = kept
	return saveFlyPOIRegistry(reg)
}

const (
	flyPOIDialogClassName = "PLMStudioFlyPOIDialog01"
	idFlyPOIName          = 8060
	idFlyPOIUnlocked      = 8061
	idFlyPOIOK            = 8062
	idFlyPOICancel        = 8063
)

var (
	flyPOIDialogRegistered bool
	flyPOIDialogOpen       bool
	flyPOIDialogWindow     syscall.Handle
	flyPOINameEdit         syscall.Handle
	flyPOIUnlockedCheck    syscall.Handle
	flyPOIAccepted         bool
	flyPOIResultName       string
	flyPOIResultUnlocked   bool
)

func flyPOIDialogWndProcFn(hwnd syscall.Handle, msg uint32, w, l uintptr) uintptr {
	switch msg {
	case WM_COMMAND:
		switch int(loword(w)) {
		case idFlyPOIOK:
			name := strings.TrimSpace(getText(flyPOINameEdit))
			if name == "" {
				msgbox("PML Studio - Punto Volo", "Inserisci il nome del punto di atterraggio/POI.", MB_OK|MB_ICONINFORMATION)
				return 0
			}
			flyPOIResultName = name
			flyPOIResultUnlocked = checked(flyPOIUnlockedCheck)
			flyPOIAccepted = true
			pDestroyWindow.Call(uintptr(hwnd))
			return 0
		case idFlyPOICancel:
			pDestroyWindow.Call(uintptr(hwnd))
			return 0
		}
	case WM_CLOSE:
		pDestroyWindow.Call(uintptr(hwnd))
		return 0
	case WM_DESTROY:
		flyPOIDialogOpen = false
		flyPOIDialogWindow = 0
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(uintptr(hwnd), uintptr(msg), w, l)
	return r
}

var flyPOIDialogWndProc = syscall.NewCallback(flyPOIDialogWndProcFn)

func ensureFlyPOIDialogClass() error {
	if flyPOIDialogRegistered {
		return nil
	}
	h, _, _ := pGetModuleHandleW.Call(0)
	hInst := syscall.Handle(h)
	cursor, _, _ := pLoadCursorW.Call(0, IDC_ARROW)
	brush, _, _ := pCreateSolidBrush.Call(rgb(244, 244, 244))
	wc := WNDCLASSEX{
		cbSize:        uint32(unsafe.Sizeof(WNDCLASSEX{})),
		lpfnWndProc:   flyPOIDialogWndProc,
		hInstance:     hInst,
		hCursor:       syscall.Handle(cursor),
		hbrBackground: syscall.Handle(brush),
		lpszClassName: wstr(flyPOIDialogClassName),
	}
	r, _, err := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	if r == 0 {
		return fmt.Errorf("registrazione finestra Punto Volo fallita: %v", err)
	}
	flyPOIDialogRegistered = true
	return nil
}

func currentFlyPOIPositionText() string {
	if eventEditorEditingIndex >= 0 && eventEditorEditingIndex < len(events) {
		e := events[eventEditorEditingIndex]
		return fmt.Sprintf("Map%03d · casella (%d,%d)", currentMap.ID, e.X, e.Y)
	}
	if eventEditorTargetX >= 0 && eventEditorTargetY >= 0 {
		return fmt.Sprintf("Map%03d · casella (%d,%d)", currentMap.ID, eventEditorTargetX, eventEditorTargetY)
	}
	return fmt.Sprintf("Map%03d · la casella verrà registrata quando posizioni l'evento", currentMap.ID)
}

func showFlyPOIDialog(owner syscall.Handle) (string, bool, bool) {
	if currentMap == nil {
		return "", false, false
	}
	regionID := strings.TrimSpace(currentMap.RegionID)
	if regionID == "" {
		msgbox("PML Studio - Punto Volo", "La mappa corrente non appartiene a una regione.\r\n\r\nAssegna prima la mappa a una regione: i punti Volo non possono esistere fuori da una regione.", MB_OK|MB_ICONINFORMATION)
		return "", false, false
	}
	if flyPOIDialogOpen {
		return "", false, false
	}
	if err := ensureFlyPOIDialogClass(); err != nil {
		msgbox("PML Studio - Punto Volo", err.Error(), MB_OK|MB_ICONERROR)
		return "", false, false
	}

	const ww, wh int32 = 680, 360
	x, y := centerOwnedWindow(ww, wh)
	h, _, _ := pGetModuleHandleW.Call(0)
	hInst := syscall.Handle(h)
	flyPOIAccepted = false
	flyPOIResultName = ""
	flyPOIResultUnlocked = true
	flyPOIDialogOpen = true
	flyPOIDialogWindow = createWindow(flyPOIDialogClassName, "Punto Volo / POI", WS_OVERLAPPEDWINDOW|WS_VISIBLE|WS_CLIPCHILDREN, x, y, ww, wh, owner, 0, hInst)
	if flyPOIDialogWindow == 0 {
		flyPOIDialogOpen = false
		return "", false, false
	}
	setWindowIcon(flyPOIDialogWindow)

	regionName := mapRegionDisplayName(regionID)
	createWindow("STATIC", "Punto di atterraggio Volo / POI", WS_CHILD|WS_VISIBLE, 28, 24, 595, 24, flyPOIDialogWindow, 8070, hInst)
	createWindow("STATIC", "Regione:", WS_CHILD|WS_VISIBLE, 28, 66, 90, 22, flyPOIDialogWindow, 8071, hInst)
	createWindow("STATIC", fmt.Sprintf("%s [%s]", regionName, strings.ToUpper(regionID)), WS_CHILD|WS_VISIBLE|WS_BORDER, 120, 62, 500, 26, flyPOIDialogWindow, 8072, hInst)
	createWindow("STATIC", "Posizione:", WS_CHILD|WS_VISIBLE, 28, 105, 90, 22, flyPOIDialogWindow, 8073, hInst)
	createWindow("STATIC", currentFlyPOIPositionText(), WS_CHILD|WS_VISIBLE|WS_BORDER, 120, 101, 500, 26, flyPOIDialogWindow, 8074, hInst)
	createWindow("STATIC", "Nome POI:", WS_CHILD|WS_VISIBLE, 28, 147, 90, 22, flyPOIDialogWindow, 8075, hInst)
	flyPOINameEdit = createWindow("EDIT", currentMap.Name, WS_CHILD|WS_VISIBLE|WS_BORDER|WS_TABSTOP, 120, 143, 500, 28, flyPOIDialogWindow, idFlyPOIName, hInst)
	flyPOIUnlockedCheck = createWindow("BUTTON", "Punto già sbloccato quando il gioco inizia", WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_AUTOCHECKBOX, 120, 190, 410, 28, flyPOIDialogWindow, idFlyPOIUnlocked, hInst)
	pSendMessageW.Call(uintptr(flyPOIUnlockedCheck), BM_SETCHECK, BST_CHECKED, 0)

	createWindow("STATIC", "Regola Volo: il punto appartiene sempre alla regione della mappa corrente. Durante il Volo saranno mostrate solo destinazioni della stessa regione; il passaggio tra regioni è disabilitato.", WS_CHILD|WS_VISIBLE, 28, 232, 592, 48, flyPOIDialogWindow, 8076, hInst)
	createWindow("BUTTON", "Salva punto", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 392, 288, 110, 34, flyPOIDialogWindow, idFlyPOIOK, hInst)
	createWindow("BUTTON", "Annulla", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 510, 288, 110, 34, flyPOIDialogWindow, idFlyPOICancel, hInst)

	pEnableWindow.Call(uintptr(owner), 0)
	pShowWindow.Call(uintptr(flyPOIDialogWindow), SW_SHOW)
	pUpdateWindow.Call(uintptr(flyPOIDialogWindow))
	var m MSG
	repostQuit := false
	for flyPOIDialogOpen {
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
	return flyPOIResultName, flyPOIResultUnlocked, flyPOIAccepted
}

func addFlyPOIEventCommand() {
	if currentMap == nil {
		return
	}
	regionID := strings.TrimSpace(currentMap.RegionID)
	if regionID == "" {
		msgbox("PML Studio - Punto Volo", "Assegna prima la mappa corrente a una regione.", MB_OK|MB_ICONINFORMATION)
		return
	}
	name, unlocked, ok := showFlyPOIDialog(eventEditorWindow)
	if !ok {
		return
	}
	c := plmManagedEventCommand{
		Type:     "fly_poi",
		POIName:  strings.TrimSpace(name),
		RegionID: regionID,
		Unlocked: unlocked,
	}
	if eventEditorPageIndex >= 0 && eventEditorPageIndex < len(eventEditorPages) {
		p := &eventEditorPages[eventEditorPageIndex]
		// A landing marker is not an obstacle and has no visible overworld
		// graphic by default. It exists as data attached to this map tile.
		p.Graphic = ""
		p.Through = true
		p.DirectionFix = true
		p.MoveType = 0
		p.Trigger = 0
	}
	currentName := strings.TrimSpace(getText(eventEditorName))
	if currentName == "" || strings.HasPrefix(strings.ToLower(currentName), "evento ") {
		setText(eventEditorName, "FlyPOI "+strings.TrimSpace(name))
	}
	if appendManagedEventCommand(c) {
		refreshEventEditorCommandList()
		setToolbarStatus("Punto Volo/POI aggiunto: verrà registrato nella regione corrente al salvataggio dell'evento.")
	}
}
