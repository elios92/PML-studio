//go:build windows

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

const (
	mapCreateClassName = "PLMStudioMapCreate05"

	idMapCreateName    = 1910
	idMapCreateID      = 1911
	idMapCreateWidth   = 1912
	idMapCreateHeight  = 1913
	idMapCreateTileset = 1914
	idMapCreateOK      = 1915
	idMapCreateCancel  = 1916
)

var (
	mapCreateClassRegistered bool
	mapCreateWindow          syscall.Handle
	mapCreateName            syscall.Handle
	mapCreateID              syscall.Handle
	mapCreateWidth           syscall.Handle
	mapCreateHeight          syscall.Handle
	mapCreateTileset         syscall.Handle
	mapCreateTargetLabel     syscall.Handle
	mapCreateTarget          string
	mapCreateOpen            bool
	mapCreateCreatedMapID    int
)

func nextAvailableMapID() int {
	maxID := 0
	for _, m := range maps {
		if m.ID > maxID {
			maxID = m.ID
		}
	}
	for id := range projectMapFileIndex(currentProject) {
		if id > maxID {
			maxID = id
		}
	}
	if maxID < 1 {
		return 1
	}
	return maxID + 1
}

func rawJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return json.RawMessage(b)
}

func emptyMapLayers(width, height int) [][][]int {
	layers := make([][][]int, 3)
	for z := 0; z < 3; z++ {
		layers[z] = make([][]int, height)
		for y := 0; y < height; y++ {
			layers[z][y] = make([]int, width)
		}
	}
	return layers
}

func createNewMapFile(target string, id int, name string, width, height, tilesetID int) (string, error) {
	if currentProject == "" {
		return "", fmt.Errorf("nessun progetto aperto")
	}
	target = filepath.Clean(target)
	if target == "" || !pathInsideProject(target) {
		return "", fmt.Errorf("cartella di destinazione non valida")
	}
	if err := os.MkdirAll(target, 0755); err != nil {
		return "", err
	}
	if id <= 0 {
		return "", fmt.Errorf("Map ID non valido")
	}
	// Re-scan before reserving the ID in case files were added externally while
	// the editor was open. Map IDs are global across all regions.
	rebuildMapFileIndex(currentProject)
	if existing := findMapFile(currentProject, id); existing != "" {
		return "", fmt.Errorf("Map%03d esiste già in %s", id, existing)
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("inserisci il nome della mappa")
	}
	if width < 1 || height < 1 || width > 1000 || height > 1000 {
		return "", fmt.Errorf("dimensioni non valide: usa valori tra 1 e 1000")
	}
	if tilesetID < 0 {
		return "", fmt.Errorf("Tileset ID non valido")
	}

	path := filepath.Join(target, fmt.Sprintf("Map%03d.json", id))
	if exists(path) {
		return "", fmt.Errorf("il file %s esiste già", filepath.Base(path))
	}

	doc := &MapDocument{
		Path: path,
		Raw: map[string]json.RawMessage{
			"_class":       rawJSON("RPG::Map"),
			"name":         rawJSON(name),
			"tileset_id":   rawJSON(tilesetID),
			"width":        rawJSON(width),
			"height":       rawJSON(height),
			"events":       rawJSON(map[string]any{}),
			"autoplay_bgm": rawJSON(false),
			"autoplay_bgs": rawJSON(false),
		},
		Table: mapTableJSON{
			Class:      "Table",
			Dimensions: 3,
			Width:      width,
			Height:     height,
			Depth:      3,
			Layers:     emptyMapLayers(width, height),
		},
		TilesetID: tilesetID,
	}
	if err := saveMapDocument(doc); err != nil {
		return "", err
	}
	return path, nil
}

func selectMapByID(id int) {
	idx := -1
	for i := range maps {
		if maps[i].ID == id {
			idx = i
			break
		}
	}
	if idx >= 0 {
		refreshMapListUI()
		selectMap(idx)
	} else {
		refreshMapListUI()
	}
}

func finishCreatedMap(id int) {
	rebuildMapFileIndex(currentProject)
	maps = loadMaps(currentProject)
	refreshMapEditorRegionControls()
	_ = writeRuntimeRegionModule()
	selectMapByID(id)
}

func createMapFromDialog() bool {
	id, err := strconv.Atoi(strings.TrimSpace(getText(mapCreateID)))
	if err != nil {
		msgbox("PLM Studio", "Map ID non valido.", MB_OK|MB_ICONERROR)
		return false
	}
	width, err := strconv.Atoi(strings.TrimSpace(getText(mapCreateWidth)))
	if err != nil {
		msgbox("PLM Studio", "Larghezza non valida.", MB_OK|MB_ICONERROR)
		return false
	}
	height, err := strconv.Atoi(strings.TrimSpace(getText(mapCreateHeight)))
	if err != nil {
		msgbox("PLM Studio", "Altezza non valida.", MB_OK|MB_ICONERROR)
		return false
	}
	tilesetID, err := strconv.Atoi(strings.TrimSpace(getText(mapCreateTileset)))
	if err != nil {
		msgbox("PLM Studio", "Tileset ID non valido.", MB_OK|MB_ICONERROR)
		return false
	}
	name := strings.TrimSpace(getText(mapCreateName))

	if !confirmMapChanges() {
		return false
	}
	path, err := createNewMapFile(mapCreateTarget, id, name, width, height, tilesetID)
	if err != nil {
		msgbox("PLM Studio", "Impossibile creare la mappa:\r\n\r\n"+err.Error(), MB_OK|MB_ICONERROR)
		return false
	}
	mapCreateCreatedMapID = id
	setToolbarStatus("Mappa creata: " + filepath.Base(path))
	return true
}

var mapCreateWndProc = syscall.NewCallback(func(hwnd syscall.Handle, msg uint32, w, l uintptr) uintptr {
	switch msg {
	case WM_COMMAND:
		switch loword(w) {
		case idMapCreateOK:
			if createMapFromDialog() {
				pDestroyWindow.Call(uintptr(hwnd))
			}
			return 0
		case idMapCreateCancel:
			pDestroyWindow.Call(uintptr(hwnd))
			return 0
		}
	case WM_CLOSE:
		pDestroyWindow.Call(uintptr(hwnd))
		return 0
	case WM_DESTROY:
		mapCreateOpen = false
		mapCreateWindow = 0
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(uintptr(hwnd), uintptr(msg), w, l)
	return r
})

func ensureMapCreateClass() error {
	if mapCreateClassRegistered {
		return nil
	}
	h, _, _ := pGetModuleHandleW.Call(0)
	hInst := syscall.Handle(h)
	cursor, _, _ := pLoadCursorW.Call(0, IDC_ARROW)
	brush, _, _ := pCreateSolidBrush.Call(rgb(244, 244, 244))
	wc := WNDCLASSEX{
		cbSize:        uint32(unsafe.Sizeof(WNDCLASSEX{})),
		lpfnWndProc:   mapCreateWndProc,
		hInstance:     hInst,
		hCursor:       syscall.Handle(cursor),
		hbrBackground: syscall.Handle(brush),
		lpszClassName: wstr(mapCreateClassName),
	}
	r, _, callErr := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	if r == 0 {
		return fmt.Errorf("registrazione finestra Nuova mappa fallita: %v", callErr)
	}
	mapCreateClassRegistered = true
	return nil
}

func mapCreateDefaultTilesetID() int {
	if currentMapDoc != nil && currentMapDoc.TilesetID >= 0 {
		return currentMapDoc.TilesetID
	}
	return 1
}

func centerOwnedWindow(width, height int32) (int32, int32) {
	var r RECT
	if hwndMain != 0 {
		pGetWindowRect.Call(uintptr(hwndMain), uintptr(unsafe.Pointer(&r)))
		x := r.Left + (r.Right-r.Left-width)/2
		y := r.Top + (r.Bottom-r.Top-height)/2
		return x, y
	}
	return -2147483648, -2147483648
}

// showCreateMapDialog creates a real map file in the selected physical Maps
// folder. The resulting Map ID is immediately indexed and visible in the same
// tree used by every map-based editor.
func showCreateMapDialog(target string) int {
	if currentProject == "" {
		msgbox("PLM Studio", "Apri prima un progetto.", MB_OK|MB_ICONINFORMATION)
		return 0
	}
	target = filepath.Clean(target)
	if target == "" || !pathInsideProject(target) {
		msgbox("PLM Studio", "Seleziona prima una cartella Maps valida nell'albero.", MB_OK|MB_ICONERROR)
		return 0
	}
	if err := ensureMapCreateClass(); err != nil {
		msgbox("PLM Studio", err.Error(), MB_OK|MB_ICONERROR)
		return 0
	}

	const winW, winH int32 = 540, 350
	x, y := centerOwnedWindow(winW, winH)
	h, _, _ := pGetModuleHandleW.Call(0)
	hInst := syscall.Handle(h)
	mapCreateTarget = target
	mapCreateCreatedMapID = 0
	mapCreateOpen = true
	mapCreateWindow = createWindow(mapCreateClassName, "PML Studio - Nuova mappa", WS_OVERLAPPEDWINDOW|WS_VISIBLE|WS_CLIPCHILDREN, x, y, winW, winH, hwndMain, 0, hInst)
	if mapCreateWindow == 0 {
		mapCreateOpen = false
		msgbox("PLM Studio", "Impossibile aprire la finestra Nuova mappa.", MB_OK|MB_ICONERROR)
		return 0
	}
	setWindowIcon(mapCreateWindow)

	createWindow("STATIC", "Nome mappa", WS_CHILD|WS_VISIBLE, 20, 25, 130, 20, mapCreateWindow, 1920, hInst)
	mapCreateName = createWindow("EDIT", "Nuova mappa", WS_CHILD|WS_VISIBLE|WS_BORDER|WS_TABSTOP, 160, 22, 335, 24, mapCreateWindow, idMapCreateName, hInst)
	createWindow("STATIC", "Map ID", WS_CHILD|WS_VISIBLE, 20, 60, 130, 20, mapCreateWindow, 1921, hInst)
	mapCreateID = createWindow("EDIT", strconv.Itoa(nextAvailableMapID()), WS_CHILD|WS_VISIBLE|WS_BORDER|WS_TABSTOP, 160, 57, 110, 24, mapCreateWindow, idMapCreateID, hInst)
	createWindow("STATIC", "Larghezza", WS_CHILD|WS_VISIBLE, 20, 95, 130, 20, mapCreateWindow, 1922, hInst)
	mapCreateWidth = createWindow("EDIT", "20", WS_CHILD|WS_VISIBLE|WS_BORDER|WS_TABSTOP, 160, 92, 110, 24, mapCreateWindow, idMapCreateWidth, hInst)
	createWindow("STATIC", "Altezza", WS_CHILD|WS_VISIBLE, 290, 95, 80, 20, mapCreateWindow, 1923, hInst)
	mapCreateHeight = createWindow("EDIT", "15", WS_CHILD|WS_VISIBLE|WS_BORDER|WS_TABSTOP, 385, 92, 110, 24, mapCreateWindow, idMapCreateHeight, hInst)
	createWindow("STATIC", "Tileset ID", WS_CHILD|WS_VISIBLE, 20, 130, 130, 20, mapCreateWindow, 1924, hInst)
	mapCreateTileset = createWindow("EDIT", strconv.Itoa(mapCreateDefaultTilesetID()), WS_CHILD|WS_VISIBLE|WS_BORDER|WS_TABSTOP, 160, 127, 110, 24, mapCreateWindow, idMapCreateTileset, hInst)
	createWindow("STATIC", "Destinazione", WS_CHILD|WS_VISIBLE, 20, 170, 130, 20, mapCreateWindow, 1925, hInst)
	label := target
	if rel, err := filepath.Rel(currentProject, target); err == nil {
		label = filepath.ToSlash(rel)
	}
	mapCreateTargetLabel = createWindow("STATIC", label, WS_CHILD|WS_VISIBLE|WS_BORDER, 160, 167, 335, 48, mapCreateWindow, 1926, hInst)
	createWindow("STATIC", "La mappa verrà collegata automaticamente all'indice del progetto e alla regione della cartella selezionata.", WS_CHILD|WS_VISIBLE, 20, 225, 475, 36, mapCreateWindow, 1927, hInst)
	createWindow("BUTTON", "Crea mappa", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 285, 270, 100, 28, mapCreateWindow, idMapCreateOK, hInst)
	createWindow("BUTTON", "Annulla", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 395, 270, 100, 28, mapCreateWindow, idMapCreateCancel, hInst)

	pEnableWindow.Call(uintptr(hwndMain), 0)
	pShowWindow.Call(uintptr(mapCreateWindow), SW_SHOW)
	pUpdateWindow.Call(uintptr(mapCreateWindow))
	pSetFocus.Call(uintptr(mapCreateName))

	var m MSG
	repostQuit := false
	for mapCreateOpen {
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
	pEnableWindow.Call(uintptr(hwndMain), 1)
	pSetFocus.Call(uintptr(hwndMain))
	if repostQuit {
		pPostQuitMessage.Call(0)
	}

	if mapCreateCreatedMapID > 0 {
		finishCreatedMap(mapCreateCreatedMapID)
	}
	return mapCreateCreatedMapID
}
