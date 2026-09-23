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

type plmStoryChapter struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Order       int    `json:"order"`
	Description string `json:"description,omitempty"`
}
type plmStoryMission struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	ChapterID   string `json:"chapter_id,omitempty"`
	Objective   string `json:"objective,omitempty"`
	Description string `json:"description,omitempty"`
}
type plmStoryScene struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	ChapterID string `json:"chapter_id,omitempty"`
	Script    string `json:"script"`
	MapID     int    `json:"map_id,omitempty"`
	EventID   int    `json:"event_id,omitempty"`
}
type plmStoryDoc struct {
	Version  int               `json:"version"`
	Chapters []plmStoryChapter `json:"chapters"`
	Missions []plmStoryMission `json:"missions"`
	Scenes   []plmStoryScene   `json:"scenes"`
}

func storyDataPath() string {
	if currentProject == "" {
		return ""
	}
	return filepath.Join(currentProject, "converted", "data", "plm_story.json")
}
func loadStoryDoc() plmStoryDoc {
	d := plmStoryDoc{Version: 1}
	p := storyDataPath()
	if p == "" {
		return d
	}
	b, e := os.ReadFile(p)
	if e == nil {
		_ = json.Unmarshal(b, &d)
	}
	if d.Version == 0 {
		d.Version = 1
	}
	return d
}
func saveStoryDoc(d plmStoryDoc) error {
	p := storyDataPath()
	if p == "" {
		return fmt.Errorf("nessun progetto aperto")
	}
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		return err
	}
	d.Version = 1
	b, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, b, 0644)
}
func storyID(s string) string {
	s = strings.ToUpper(strings.TrimSpace(s))
	r := strings.NewReplacer(" ", "_", "-", "_", "/", "_", "\\", "_", ":", "_")
	return r.Replace(s)
}

const (
	storyPaletteClass = "PLMStudioNarrativePalette01"
	idStoryChapter    = 9700
	idStoryMission    = 9701
	idStoryScene      = 9702
	idStoryState      = 9703
	idStoryCondition  = 9704
	idStoryBack       = 9705
)

var storyPaletteRegistered, storyPaletteOpen, storyPaletteAccepted bool
var storyPaletteWindow syscall.Handle
var storyPaletteChoice int

func storyPaletteWndProcFn(hwnd syscall.Handle, msg uint32, w, l uintptr) uintptr {
	if msg == WM_COMMAND {
		id := int(loword(w))
		if id >= idStoryChapter && id <= idStoryCondition {
			storyPaletteChoice = id
			storyPaletteAccepted = true
			pDestroyWindow.Call(uintptr(hwnd))
			return 0
		}
		if id == idStoryBack {
			pDestroyWindow.Call(uintptr(hwnd))
			return 0
		}
	}
	if msg == WM_CLOSE {
		pDestroyWindow.Call(uintptr(hwnd))
		return 0
	}
	if msg == WM_DESTROY {
		storyPaletteOpen = false
		storyPaletteWindow = 0
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(uintptr(hwnd), uintptr(msg), w, l)
	return r
}

var storyPaletteWndProc = syscall.NewCallback(storyPaletteWndProcFn)

func ensureStoryPaletteClass() error {
	if storyPaletteRegistered {
		return nil
	}
	h, _, _ := pGetModuleHandleW.Call(0)
	hi := syscall.Handle(h)
	cur, _, _ := pLoadCursorW.Call(0, IDC_ARROW)
	br, _, _ := pCreateSolidBrush.Call(rgb(244, 244, 244))
	wc := WNDCLASSEX{cbSize: uint32(unsafe.Sizeof(WNDCLASSEX{})), lpfnWndProc: storyPaletteWndProc, hInstance: hi, hCursor: syscall.Handle(cur), hbrBackground: syscall.Handle(br), lpszClassName: wstr(storyPaletteClass)}
	r, _, e := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	if r == 0 {
		return fmt.Errorf("registrazione Narrativa/Missioni: %v", e)
	}
	storyPaletteRegistered = true
	return nil
}
func showStoryPalette(owner syscall.Handle) (int, bool) {
	if storyPaletteOpen {
		return 0, false
	}
	if err := ensureStoryPaletteClass(); err != nil {
		msgbox("PML Studio - Narrativa", err.Error(), MB_OK|MB_ICONERROR)
		return 0, false
	}
	const ww, wh int32 = 690, 450
	x, y := centerOwnedWindow(ww, wh)
	h, _, _ := pGetModuleHandleW.Call(0)
	hi := syscall.Handle(h)
	storyPaletteOpen = true
	storyPaletteAccepted = false
	storyPaletteChoice = 0
	storyPaletteWindow = createWindow(storyPaletteClass, "Narrativa / Missioni", WS_OVERLAPPEDWINDOW|WS_VISIBLE|WS_CLIPCHILDREN, x, y, ww, wh, owner, 0, hi)
	if storyPaletteWindow == 0 {
		storyPaletteOpen = false
		return 0, false
	}
	setWindowIcon(storyPaletteWindow)
	createWindow("STATIC", "Sistema narrativo separato dagli eventi: definisci capitoli/missioni, poi usa gli stati negli eventi.", WS_CHILD|WS_VISIBLE, 28, 22, 620, 42, storyPaletteWindow, 9710, hi)
	bs := []struct {
		id   int
		t    string
		x, y int32
	}{{idStoryChapter, "Capitolo", 45, 85}, {idStoryMission, "Missione / Quest", 255, 85}, {idStoryScene, "Scena da sceneggiatura", 465, 85}, {idStoryState, "Cambia stato", 45, 175}, {idStoryCondition, "Condizione narrativa", 255, 175}, {idStoryBack, "← Indietro", 465, 285}}
	for _, b := range bs {
		createWindow("BUTTON", b.t, WS_CHILD|WS_VISIBLE|WS_TABSTOP, b.x, b.y, 180, 62, storyPaletteWindow, uintptr(b.id), hi)
	}
	createWindow("STATIC", "Le scene compilate usano comandi evento reali e vengono racchiuse automaticamente tra Inizio/Fine cutscene.", WS_CHILD|WS_VISIBLE, 45, 265, 390, 58, storyPaletteWindow, 9711, hi)
	pEnableWindow.Call(uintptr(owner), 0)
	modalLoop(&storyPaletteOpen)
	pEnableWindow.Call(uintptr(owner), 1)
	pSetFocus.Call(uintptr(owner))
	return storyPaletteChoice, storyPaletteAccepted
}

func editStoryChapter() {
	v, ok := showSimpleCommandForm(eventEditorWindow, "Capitolo", "Crea o aggiorna un capitolo del progetto. L'ID rimane stabile anche se cambi il titolo.", []simpleFormField{{Key: "id", Label: "ID capitolo", Kind: simpleFormText}, {Key: "name", Label: "Titolo", Kind: simpleFormText}, {Key: "order", Label: "Ordine", Kind: simpleFormNumber, Initial: "1"}, {Key: "desc", Label: "Descrizione", Kind: simpleFormMultiline}})
	if !ok {
		return
	}
	id := storyID(v["id"])
	if id == "" {
		id = storyID(v["name"])
	}
	if id == "" {
		return
	}
	d := loadStoryDoc()
	rec := plmStoryChapter{ID: id, Name: strings.TrimSpace(v["name"]), Order: formInt(v, "order", 1), Description: strings.TrimSpace(v["desc"])}
	found := false
	for i := range d.Chapters {
		if strings.EqualFold(d.Chapters[i].ID, id) {
			d.Chapters[i] = rec
			found = true
			break
		}
	}
	if !found {
		d.Chapters = append(d.Chapters, rec)
	}
	sort.SliceStable(d.Chapters, func(i, j int) bool { return d.Chapters[i].Order < d.Chapters[j].Order })
	if err := saveStoryDoc(d); err != nil {
		msgbox("PML Studio - Capitolo", err.Error(), MB_OK|MB_ICONERROR)
		return
	}
	setToolbarStatus("Capitolo salvato: " + rec.Name)
}
func editStoryMission() {
	d := loadStoryDoc()
	chap := []string{"(nessun capitolo)"}
	for _, c := range d.Chapters {
		chap = append(chap, c.ID+" — "+c.Name)
	}
	v, ok := showSimpleCommandForm(eventEditorWindow, "Missione / Quest", "Definizione persistente. Lo stato (avviata/completata/fallita) viene gestito dai comandi evento.", []simpleFormField{{Key: "id", Label: "ID missione", Kind: simpleFormText}, {Key: "name", Label: "Titolo", Kind: simpleFormText}, {Key: "kind", Label: "Tipo", Kind: simpleFormCombo, Options: []string{"Missione principale", "Missione secondaria / Quest", "Richiesta / Incarico", "Evento / SOS", "Indagine / Segreto"}}, {Key: "chapter", Label: "Capitolo", Kind: simpleFormCombo, Options: chap}, {Key: "objective", Label: "Obiettivo", Kind: simpleFormText}, {Key: "desc", Label: "Descrizione", Kind: simpleFormMultiline}})
	if !ok {
		return
	}
	id := storyID(v["id"])
	if id == "" {
		id = storyID(v["name"])
	}
	if id == "" {
		return
	}
	chapter := strings.TrimSpace(v["chapter"])
	if strings.HasPrefix(chapter, "(") {
		chapter = ""
	} else if k := strings.Index(chapter, " — "); k >= 0 {
		chapter = chapter[:k]
	}
	rec := plmStoryMission{ID: id, Name: strings.TrimSpace(v["name"]), Kind: strings.TrimSpace(v["kind"]), ChapterID: chapter, Objective: strings.TrimSpace(v["objective"]), Description: strings.TrimSpace(v["desc"])}
	found := false
	for i := range d.Missions {
		if strings.EqualFold(d.Missions[i].ID, id) {
			d.Missions[i] = rec
			found = true
			break
		}
	}
	if !found {
		d.Missions = append(d.Missions, rec)
	}
	if err := saveStoryDoc(d); err != nil {
		msgbox("PML Studio - Missione", err.Error(), MB_OK|MB_ICONERROR)
		return
	}
	setToolbarStatus("Missione/Quest salvata: " + rec.Name)
}

func storyEntityOptions(d plmStoryDoc) []string {
	out := []string{}
	for _, c := range d.Chapters {
		out = append(out, "chapter|"+c.ID+"|"+c.Name)
	}
	for _, m := range d.Missions {
		out = append(out, "mission|"+m.ID+"|"+m.Name)
	}
	sort.Strings(out)
	return out
}
func addStoryStateCommand() {
	d := loadStoryDoc()
	opts := storyEntityOptions(d)
	if len(opts) == 0 {
		msgbox("PML Studio - Narrativa", "Crea prima almeno un Capitolo o una Missione/Quest.", MB_OK|MB_ICONINFORMATION)
		return
	}
	v, ok := showSimpleCommandForm(eventEditorWindow, "Cambia stato narrativo", "Lo stato viene salvato nel salvataggio del gioco ed è disponibile da qualsiasi mappa.", []simpleFormField{{Key: "entity", Label: "Elemento", Kind: simpleFormCombo, Options: opts}, {Key: "action", Label: "Azione", Kind: simpleFormCombo, Options: []string{"START", "COMPLETE", "FAIL", "RESET", "SET_STAGE"}}, {Key: "stage", Label: "Stage (solo SET_STAGE)", Kind: simpleFormNumber, Initial: "1"}})
	if !ok {
		return
	}
	parts := strings.SplitN(v["entity"], "|", 3)
	if len(parts) < 3 {
		return
	}
	c := plmManagedEventCommand{Type: "story_state", StoryType: parts[0], StoryID: parts[1], StoryName: parts[2], Action: strings.ToLower(v["action"]), Stage: formInt(v, "stage", 1)}
	if appendManagedEventCommand(c) {
		setToolbarStatus("Stato narrativo aggiunto: " + managedEventCommandLabel(c))
	}
}

func sceneMoveOp(dir string) (string, bool) {
	switch strings.ToUpper(strings.TrimSpace(dir)) {
	case "SU", "UP":
		return "up", true
	case "GIU", "GIÙ", "DOWN":
		return "down", true
	case "SINISTRA", "LEFT":
		return "left", true
	case "DESTRA", "RIGHT":
		return "right", true
	}
	return "", false
}

func compileSceneScript(sceneID, sceneName, script string) (int, error) {
	count := 0
	if appendManagedEventCommand(plmManagedEventCommand{Type: "scene_marker", SceneID: sceneID, StoryName: sceneName, Action: "begin"}) {
		count++
	}
	appendManagedEventCommand(plmManagedEventCommand{Type: "cutscene_begin"})
	count++
	lines := strings.Split(strings.ReplaceAll(script, "\r", ""), "\n")
	for ln, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		upper := strings.ToUpper(line)
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			body := strings.TrimSpace(line[1 : len(line)-1])
			parts := strings.Fields(body)
			if len(parts) == 0 {
				continue
			}
			cmd := strings.ToUpper(parts[0])
			switch cmd {
			case "ATTENDI", "WAIT":
				if len(parts) < 2 {
					return count, fmt.Errorf("riga %d: ATTENDI richiede i frame", ln+1)
				}
				n, _ := strconv.Atoi(parts[1])
				if n < 1 {
					n = 1
				}
				appendManagedEventCommand(plmManagedEventCommand{Type: "wait_frames", Duration: n})
				count++
			case "BGM", "SE", "BGS", "ME":
				if len(parts) < 2 {
					return count, fmt.Errorf("riga %d: %s richiede il nome audio", ln+1, cmd)
				}
				name := strings.Join(parts[1:], " ")
				appendManagedEventCommand(plmManagedEventCommand{Type: "audio_play", AudioKind: strings.ToLower(cmd), AudioName: name, Volume: 100, Pitch: 100})
				count++
			case "STOPBGM", "STOP_BGM":
				appendManagedEventCommand(plmManagedEventCommand{Type: "audio_stop", AudioKind: "bgm"})
				count++
			case "FLASH":
				dur := 20
				if len(parts) > 2 {
					dur, _ = strconv.Atoi(parts[len(parts)-1])
				}
				col := "BIANCO"
				if len(parts) > 1 {
					col = strings.ToUpper(parts[1])
				}
				r, g, b := 255, 255, 255
				if col == "NERO" {
					r, g, b = 0, 0, 0
				}
				appendManagedEventCommand(plmManagedEventCommand{Type: "screen_flash", Red: r, Green: g, Blue: b, Alpha: 255, Duration: maxIntEventCmd(dur, 1), Wait: true})
				count++
			case "MUOVI", "MOVE":
				if len(parts) < 4 {
					return count, fmt.Errorf("riga %d: usa [MUOVI GIOCATORE DIREZIONE PASSI] oppure [MUOVI NPC ID DIREZIONE PASSI]", ln+1)
				}
				target := strings.ToUpper(parts[1])
				idxDir := 2
				targetID := 0
				if target == "NPC" || target == "EVENTO" {
					if len(parts) < 5 {
						return count, fmt.Errorf("riga %d: MUOVI NPC richiede ID, direzione e passi", ln+1)
					}
					targetID, _ = strconv.Atoi(parts[2])
					idxDir = 3
				}
				op, ok := sceneMoveOp(parts[idxDir])
				if !ok {
					return count, fmt.Errorf("riga %d: direzione movimento non valida", ln+1)
				}
				steps, _ := strconv.Atoi(parts[idxDir+1])
				if steps < 1 {
					steps = 1
				}
				if steps > 999 {
					steps = 999
				}
				moves := make([]plmMovementStep, 0, steps)
				for i := 0; i < steps; i++ {
					moves = append(moves, plmMovementStep{Op: op})
				}
				c := plmManagedEventCommand{Type: "movement_route", Target: "player", Moves: moves, RepeatCount: 1, Wait: true}
				if target == "NPC" || target == "EVENTO" {
					c.Target = "event"
					c.TargetEventID = targetID
				}
				appendManagedEventCommand(c)
				count++
			case "WARP":
				if len(parts) < 4 {
					return count, fmt.Errorf("riga %d: WARP richiede MapID X Y", ln+1)
				}
				mid, _ := strconv.Atoi(parts[1])
				xx, _ := strconv.Atoi(parts[2])
				yy, _ := strconv.Atoi(parts[3])
				if mid <= 0 {
					return count, fmt.Errorf("riga %d: MapID Warp non valido", ln+1)
				}
				appendManagedEventCommand(plmManagedEventCommand{Type: "warp", MapID: mid, X: xx, Y: yy, Direction: 0})
				count++
			case "MISSIONE", "QUEST", "CAPITOLO":
				if len(parts) < 3 {
					return count, fmt.Errorf("riga %d: %s richiede ID e azione", ln+1, cmd)
				}
				typ := "mission"
				if cmd == "CAPITOLO" {
					typ = "chapter"
				}
				appendManagedEventCommand(plmManagedEventCommand{Type: "story_state", StoryType: typ, StoryID: storyID(parts[1]), StoryName: parts[1], Action: strings.ToLower(parts[2])})
				count++
			default:
				return count, fmt.Errorf("riga %d: comando scena non riconosciuto: %s", ln+1, cmd)
			}
			continue
		}
		if idx := strings.Index(line, ":"); idx > 0 && idx < 48 {
			speaker := strings.TrimSpace(line[:idx])
			text := strings.TrimSpace(line[idx+1:])
			if text != "" {
				appendShowTextEventCommand(speaker + ": " + text)
				count++
				continue
			}
		}
		_ = upper
		appendShowTextEventCommand(line)
		count++
	}
	appendManagedEventCommand(plmManagedEventCommand{Type: "cutscene_end"})
	count++
	appendManagedEventCommand(plmManagedEventCommand{Type: "scene_marker", SceneID: sceneID, StoryName: sceneName, Action: "end"})
	count++
	return count, nil
}

func createStoryScene() {
	d := loadStoryDoc()
	chap := []string{"(nessun capitolo)"}
	for _, c := range d.Chapters {
		chap = append(chap, c.ID+" — "+c.Name)
	}
	example := "Professor: Finalmente sei arrivato.\r\n[ATTENDI 20]\r\n[SE Door enter]\r\n[BGM Theme]\r\nNarratore: La scena continua...\r\n[FLASH BIANCO 15]"
	v, ok := showSimpleCommandForm(eventEditorWindow, "Scena da sceneggiatura", "Ogni riga 'Nome: testo' diventa dialogo. Direttive supportate: [ATTENDI n], [BGM nome], [BGS nome], [ME nome], [SE nome], [STOPBGM], [FLASH BIANCO|NERO n], [MUOVI GIOCATORE SU 3], [MUOVI NPC 12 DESTRA 2], [WARP MapID X Y], [MISSIONE ID START|COMPLETE|FAIL], [QUEST ...], [CAPITOLO ...].", []simpleFormField{{Key: "id", Label: "ID scena", Kind: simpleFormText}, {Key: "name", Label: "Titolo scena", Kind: simpleFormText}, {Key: "chapter", Label: "Capitolo", Kind: simpleFormCombo, Options: chap}, {Key: "script", Label: "Sceneggiatura", Kind: simpleFormMultiline, Initial: example}})
	if !ok {
		return
	}
	id := storyID(v["id"])
	if id == "" {
		id = storyID(v["name"])
	}
	if id == "" {
		msgbox("PML Studio - Scena", "Inserisci un ID o un titolo scena.", MB_OK|MB_ICONINFORMATION)
		return
	}
	chapter := v["chapter"]
	if strings.HasPrefix(chapter, "(") {
		chapter = ""
	} else if k := strings.Index(chapter, " — "); k >= 0 {
		chapter = chapter[:k]
	}
	eventID := 0
	if eventEditorEditingIndex >= 0 && eventEditorEditingIndex < len(events) {
		eventID = events[eventEditorEditingIndex].ID
	}
	rec := plmStoryScene{ID: id, Name: strings.TrimSpace(v["name"]), ChapterID: chapter, Script: v["script"], MapID: func() int {
		if currentMap != nil {
			return currentMap.ID
		}
		return 0
	}(), EventID: eventID}
	found := false
	for i := range d.Scenes {
		if strings.EqualFold(d.Scenes[i].ID, id) {
			d.Scenes[i] = rec
			found = true
			break
		}
	}
	if !found {
		d.Scenes = append(d.Scenes, rec)
	}
	if err := saveStoryDoc(d); err != nil {
		msgbox("PML Studio - Scena", err.Error(), MB_OK|MB_ICONERROR)
		return
	}
	n, err := compileSceneScript(id, rec.Name, rec.Script)
	if err != nil {
		msgbox("PML Studio - Scena", "Scena salvata, ma compilazione interrotta:\r\n\r\n"+err.Error(), MB_OK|MB_ICONERROR)
		return
	}
	setToolbarStatus(fmt.Sprintf("Scena %s compilata: %d comandi evento.", rec.Name, n))
}

func addNarrativeEventCommand() {
	for {
		choice, ok := showStoryPalette(eventEditorWindow)
		if !ok {
			return
		}
		switch choice {
		case idStoryChapter:
			editStoryChapter()
		case idStoryMission:
			editStoryMission()
		case idStoryScene:
			createStoryScene()
		case idStoryState:
			addStoryStateCommand()
		case idStoryCondition:
			addProgressConditionEventCommand()
		}
	}
}
