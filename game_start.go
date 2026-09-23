//go:build windows

package main

import (
	"bytes"
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
	gameStartSchema = "pml.game_start.v1"

	startWndClassName = "PLMStudioGameStart05"
	idStartMapCombo   = 1760
	idStartX          = 1761
	idStartY          = 1762
	idStartDirection  = 1763
	idStartAutoDetect = 1764
	idStartSave       = 1765
	idStartCancel     = 1766
)

type GameStartConfig struct {
	Version     int    `json:"version"`
	Schema      string `json:"schema"`
	MapID       int    `json:"map_id"`
	X           int    `json:"x"`
	Y           int    `json:"y"`
	Direction   int    `json:"direction"`
	IntroMapIDs []int  `json:"intro_map_ids,omitempty"`
	Source      string `json:"source,omitempty"`
}

var (
	gameStart GameStartConfig

	startWndRegistered bool
	startWndOpen       bool
	startWnd           syscall.Handle
	startMapCombo      syscall.Handle
	startXEdit         syscall.Handle
	startYEdit         syscall.Handle
	startDirCombo      syscall.Handle
	startInfoLabel     syscall.Handle
	startDialogMapIDs  []int
)

func gameStartPath() string {
	if currentProject == "" {
		return ""
	}
	return filepath.Join(currentProject, "converted", "data", "game_start.json")
}

func mapIndexByID(id int) int {
	for i := range maps {
		if maps[i].ID == id {
			return i
		}
	}
	return -1
}

func mapExistsByID(id int) bool { return mapIndexByID(id) >= 0 }

func isIntroAnimationMapID(id int) bool {
	for _, v := range gameStart.IntroMapIDs {
		if v == id {
			return true
		}
	}
	return false
}

func gameStartMapIndex() int { return mapIndexByID(gameStart.MapID) }

func directionLabel(dir int) string {
	switch dir {
	case 2:
		return "Giù"
	case 4:
		return "Sinistra"
	case 6:
		return "Destra"
	case 8:
		return "Su"
	default:
		return "Mantieni"
	}
}

func directionFromComboIndex(i int) int {
	switch i {
	case 1:
		return 2
	case 2:
		return 4
	case 3:
		return 6
	case 4:
		return 8
	default:
		return 0
	}
}

func directionComboIndex(dir int) int {
	switch dir {
	case 2:
		return 1
	case 4:
		return 2
	case 6:
		return 3
	case 8:
		return 4
	default:
		return 0
	}
}

func introNameScore(name string, id int) int {
	n := strings.ToLower(strings.TrimSpace(name))
	score := 0
	if n == "intro" || n == "introduzione" || n == "prologo" {
		score += 200
	}
	for _, token := range []string{"intro", "introduz", "prologo", "opening", "professor", "professore"} {
		if strings.Contains(n, token) {
			score += 80
		}
	}
	// Map001 is only a fallback hint. It is NEVER hidden merely because its ID
	// is 1; it must also contain a direct Transfer Player command.
	if id == 1 {
		score += 25
	}
	return score
}

func readMapEventsRaw(path string) (json.RawMessage, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var raw map[string]json.RawMessage
	if err := dec.Decode(&raw); err != nil {
		return nil, err
	}
	return raw["events"], nil
}

func detectGameStartConfig() GameStartConfig {
	cfg := GameStartConfig{Version: 1, Schema: gameStartSchema, Direction: 2, Source: "fallback_first_playable"}
	type candidate struct {
		idx, score int
	}
	candidates := []candidate{}
	for i, m := range maps {
		if m.File == "" {
			continue
		}
		if score := introNameScore(m.Name, m.ID); score > 0 {
			candidates = append(candidates, candidate{i, score})
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].score != candidates[j].score {
			return candidates[i].score > candidates[j].score
		}
		return maps[candidates[i].idx].ID < maps[candidates[j].idx].ID
	})

	for _, c := range candidates {
		source := maps[c.idx]
		raw, err := readMapEventsRaw(source.File)
		if err != nil || len(raw) == 0 {
			continue
		}
		targets := transferTargetsFromMapRaw(raw)
		for _, target := range targets {
			if target.MapID <= 0 || target.MapID == source.ID || !mapExistsByID(target.MapID) {
				continue
			}
			cfg.MapID = target.MapID
			cfg.X = target.X
			cfg.Y = target.Y
			cfg.Direction = target.Direction
			if cfg.Direction == 0 {
				cfg.Direction = 2
			}
			cfg.IntroMapIDs = []int{source.ID}
			cfg.Source = fmt.Sprintf("detected_transfer_from_Map%03d", source.ID)
			return cfg
		}
	}

	// No reliable intro transfer found: do not invent an intro map. Choose the
	// first normal map only as an editable starting-point fallback.
	for _, m := range maps {
		if introNameScore(m.Name, m.ID) >= 200 {
			continue
		}
		cfg.MapID = m.ID
		cfg.X, cfg.Y = 0, 0
		return cfg
	}
	if len(maps) > 0 {
		cfg.MapID = maps[0].ID
	}
	return cfg
}

func saveGameStartConfig() error {
	if currentProject == "" {
		return nil
	}
	if gameStart.Version == 0 {
		gameStart.Version = 1
	}
	gameStart.Schema = gameStartSchema
	p := gameStartPath()
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(gameStart, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0644); err != nil {
		return err
	}
	r, _, callErr := pMoveFileExW.Call(
		uintptr(unsafe.Pointer(wstr(tmp))),
		uintptr(unsafe.Pointer(wstr(p))),
		MOVEFILE_REPLACE_EXISTING|MOVEFILE_WRITE_THROUGH,
	)
	if r == 0 {
		_ = os.Remove(tmp)
		return fmt.Errorf("salvataggio punto iniziale fallito: %v", callErr)
	}
	return nil
}

func loadOrDetectGameStart() {
	gameStart = GameStartConfig{}
	p := gameStartPath()
	if b, err := os.ReadFile(p); err == nil {
		var cfg GameStartConfig
		if json.Unmarshal(b, &cfg) == nil && cfg.MapID > 0 && mapExistsByID(cfg.MapID) {
			if cfg.Version == 0 {
				cfg.Version = 1
			}
			if cfg.Schema == "" {
				cfg.Schema = gameStartSchema
			}
			gameStart = cfg
			mapLogf("[START] loaded Map%03d x=%d y=%d dir=%d intro=%v", cfg.MapID, cfg.X, cfg.Y, cfg.Direction, cfg.IntroMapIDs)
			return
		}
	}
	gameStart = detectGameStartConfig()
	if gameStart.MapID > 0 {
		if err := saveGameStartConfig(); err != nil {
			mapLogf("[START] save auto-detected config: %v", err)
		}
	}
	mapLogf("[START] detected Map%03d x=%d y=%d dir=%d intro=%v source=%s", gameStart.MapID, gameStart.X, gameStart.Y, gameStart.Direction, gameStart.IntroMapIDs, gameStart.Source)
}

func firstNormalMapIndex() int {
	if idx := gameStartMapIndex(); idx >= 0 {
		return idx
	}
	for i, m := range maps {
		if !isIntroAnimationMapID(m.ID) {
			return i
		}
	}
	if len(maps) > 0 {
		return 0
	}
	return -1
}

func currentMapIsIntroAnimation() bool {
	return currentMap != nil && isIntroAnimationMapID(currentMap.ID)
}

func introMapDescription() string {
	if len(gameStart.IntroMapIDs) == 0 {
		return "Nessuna intro/evento di sistema rilevata"
	}
	labels := make([]string, 0, len(gameStart.IntroMapIDs))
	for _, id := range gameStart.IntroMapIDs {
		if idx := mapIndexByID(id); idx >= 0 {
			labels = append(labels, fmt.Sprintf("Map%03d - %s", id, maps[idx].Name))
		} else {
			labels = append(labels, fmt.Sprintf("Map%03d", id))
		}
	}
	return "Intro/animazione: " + strings.Join(labels, ", ")
}

func updateStartDialogFromConfig(cfg GameStartConfig) {
	startDialogMapIDs = startDialogMapIDs[:0]
	pSendMessageW.Call(uintptr(startMapCombo), CB_RESETCONTENT, 0, 0)
	selected := -1
	for _, m := range maps {
		if isIntroAnimationMapID(m.ID) {
			continue
		}
		label := fmt.Sprintf("%03d - %s", m.ID, m.Name)
		comboAdd(startMapCombo, label)
		startDialogMapIDs = append(startDialogMapIDs, m.ID)
		if m.ID == cfg.MapID {
			selected = len(startDialogMapIDs) - 1
		}
	}
	if selected < 0 && len(startDialogMapIDs) > 0 {
		selected = 0
	}
	if selected >= 0 {
		pSendMessageW.Call(uintptr(startMapCombo), CB_SETCURSEL, uintptr(selected), 0)
	}
	setText(startXEdit, strconv.Itoa(cfg.X))
	setText(startYEdit, strconv.Itoa(cfg.Y))
	pSendMessageW.Call(uintptr(startDirCombo), CB_SETCURSEL, uintptr(directionComboIndex(cfg.Direction)), 0)
	setText(startInfoLabel, introMapDescription()+"\r\nRilevamento: "+cfg.Source)
}

func saveStartDialogValues() error {
	sel := comboSel(startMapCombo)
	if sel < 0 || sel >= len(startDialogMapIDs) {
		return fmt.Errorf("seleziona una mappa iniziale")
	}
	x, err := strconv.Atoi(strings.TrimSpace(getText(startXEdit)))
	if err != nil || x < 0 {
		return fmt.Errorf("coordinata X non valida")
	}
	y, err := strconv.Atoi(strings.TrimSpace(getText(startYEdit)))
	if err != nil || y < 0 {
		return fmt.Errorf("coordinata Y non valida")
	}
	mapID := startDialogMapIDs[sel]
	idx := mapIndexByID(mapID)
	if idx < 0 {
		return fmt.Errorf("mappa iniziale non trovata")
	}
	w, h := loadDims(maps[idx])
	if x >= w || y >= h {
		return fmt.Errorf("il punto %d,%d è fuori dalla Map%03d (%dx%d)", x, y, mapID, w, h)
	}
	gameStart.MapID = mapID
	gameStart.X = x
	gameStart.Y = y
	gameStart.Direction = directionFromComboIndex(comboSel(startDirCombo))
	gameStart.Version = 1
	gameStart.Schema = gameStartSchema
	gameStart.Source = "manual"
	if err := saveGameStartConfig(); err != nil {
		return err
	}
	scheduleMapTreeRefresh()
	invalidate(hwndCanvas)
	setToolbarStatus(fmt.Sprintf("Punto iniziale: Map%03d (%d,%d) %s", mapID, x, y, directionLabel(gameStart.Direction)))
	return nil
}

var startWndProc = syscall.NewCallback(func(hwnd syscall.Handle, msg uint32, w, l uintptr) uintptr {
	switch msg {
	case WM_COMMAND:
		switch loword(w) {
		case idStartAutoDetect:
			gameStart = detectGameStartConfig()
			updateStartDialogFromConfig(gameStart)
			return 0
		case idStartSave:
			if err := saveStartDialogValues(); err != nil {
				msgbox("PML Studio - Punto iniziale", err.Error(), MB_OK|MB_ICONERROR)
				return 0
			}
			pDestroyWindow.Call(uintptr(hwnd))
			return 0
		case idStartCancel:
			pDestroyWindow.Call(uintptr(hwnd))
			return 0
		}
	case WM_CLOSE:
		pDestroyWindow.Call(uintptr(hwnd))
		return 0
	case WM_DESTROY:
		startWndOpen = false
		startWnd = 0
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(uintptr(hwnd), uintptr(msg), w, l)
	return r
})

func ensureStartWndClass() error {
	if startWndRegistered {
		return nil
	}
	h, _, _ := pGetModuleHandleW.Call(0)
	hInst := syscall.Handle(h)
	cursor, _, _ := pLoadCursorW.Call(0, IDC_ARROW)
	brush, _, _ := pCreateSolidBrush.Call(rgb(244, 244, 244))
	wc := WNDCLASSEX{
		cbSize: uint32(unsafe.Sizeof(WNDCLASSEX{})), lpfnWndProc: startWndProc,
		hInstance: hInst, hCursor: syscall.Handle(cursor), hbrBackground: syscall.Handle(brush),
		lpszClassName: wstr(startWndClassName),
	}
	r, _, err := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	if r == 0 {
		return fmt.Errorf("registrazione finestra punto iniziale fallita: %v", err)
	}
	startWndRegistered = true
	return nil
}

func showGameStartEditor() {
	if currentProject == "" || len(maps) == 0 {
		msgbox("PML Studio - Punto iniziale", "Apri prima un progetto con almeno una mappa.", MB_OK|MB_ICONINFORMATION)
		return
	}
	if startWndOpen {
		return
	}
	if err := ensureStartWndClass(); err != nil {
		msgbox("PML Studio", err.Error(), MB_OK|MB_ICONERROR)
		return
	}
	const ww, wh int32 = 610, 390
	x, y := centerOwnedWindow(ww, wh)
	h, _, _ := pGetModuleHandleW.Call(0)
	hInst := syscall.Handle(h)
	startWndOpen = true
	startWnd = createWindow(startWndClassName, "PML Studio - Punto iniziale del gioco", WS_OVERLAPPEDWINDOW|WS_VISIBLE|WS_CLIPCHILDREN, x, y, ww, wh, hwndMain, 0, hInst)
	if startWnd == 0 {
		startWndOpen = false
		return
	}
	setWindowIcon(startWnd)
	createWindow("STATIC", "Mappa iniziale", WS_CHILD|WS_VISIBLE, 24, 28, 120, 24, startWnd, 1750, hInst)
	startMapCombo = createWindow("COMBOBOX", "", WS_CHILD|WS_VISIBLE|WS_TABSTOP|CBS_DROPDOWNLIST, 150, 24, 410, 220, startWnd, idStartMapCombo, hInst)
	createWindow("STATIC", "X", WS_CHILD|WS_VISIBLE, 24, 78, 40, 24, startWnd, 1751, hInst)
	startXEdit = createWindow("EDIT", "0", WS_CHILD|WS_VISIBLE|WS_TABSTOP|WS_BORDER, 70, 74, 95, 27, startWnd, idStartX, hInst)
	createWindow("STATIC", "Y", WS_CHILD|WS_VISIBLE, 190, 78, 40, 24, startWnd, 1752, hInst)
	startYEdit = createWindow("EDIT", "0", WS_CHILD|WS_VISIBLE|WS_TABSTOP|WS_BORDER, 235, 74, 95, 27, startWnd, idStartY, hInst)
	createWindow("STATIC", "Direzione", WS_CHILD|WS_VISIBLE, 355, 78, 75, 24, startWnd, 1753, hInst)
	startDirCombo = createWindow("COMBOBOX", "", WS_CHILD|WS_VISIBLE|WS_TABSTOP|CBS_DROPDOWNLIST, 435, 74, 125, 180, startWnd, idStartDirection, hInst)
	for _, name := range []string{"Mantieni", "Giù", "Sinistra", "Destra", "Su"} {
		comboAdd(startDirCombo, name)
	}
	startInfoLabel = createWindow("STATIC", "", WS_CHILD|WS_VISIBLE, 24, 126, 536, 70, startWnd, 1754, hInst)
	createWindow("STATIC", "Il rilevamento automatico legge il comando Transfer Player dell'intro. Puoi sempre cambiare mappa e coordinate manualmente.", WS_CHILD|WS_VISIBLE, 24, 205, 536, 50, startWnd, 1755, hInst)
	createWindow("BUTTON", "Rileva automaticamente", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 24, 276, 175, 32, startWnd, idStartAutoDetect, hInst)
	createWindow("BUTTON", "Salva", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 355, 276, 95, 32, startWnd, idStartSave, hInst)
	createWindow("BUTTON", "Annulla", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 465, 276, 95, 32, startWnd, idStartCancel, hInst)
	updateStartDialogFromConfig(gameStart)

	pEnableWindow.Call(uintptr(hwndMain), 0)
	pShowWindow.Call(uintptr(startWnd), SW_SHOW)
	pUpdateWindow.Call(uintptr(startWnd))
	var m MSG
	repostQuit := false
	for startWndOpen {
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
}
