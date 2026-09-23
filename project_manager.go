//go:build windows

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

func exists(p string) bool { _, e := os.Stat(p); return e == nil }
func isDir(p string) bool  { s, e := os.Stat(p); return e == nil && s.IsDir() }
func detectProjectType(r string) string {
	// main.py is the authoritative entry point for an already converted PLM/Python project.
	if exists(filepath.Join(r, "main.py")) {
		return "PLM/Python Engine"
	}
	if exists(filepath.Join(r, "plm_project.json")) {
		return "PLM Studio"
	}
	if matches, _ := filepath.Glob(filepath.Join(r, "*.rxproj")); len(matches) > 0 {
		return "RPG Maker XP / Pokémon Essentials"
	}
	if exists(filepath.Join(r, "Data", "MapInfos.rxdata")) {
		return "RPG Maker XP / Pokémon Essentials"
	}
	return "Progetto non riconosciuto"
}
func projectName(r string) string {
	if b, e := os.ReadFile(filepath.Join(r, "plm_project.json")); e == nil {
		var m ProjectManifest
		if json.Unmarshal(b, &m) == nil && strings.TrimSpace(m.Name) != "" {
			return m.Name
		}
	}
	return filepath.Base(filepath.Clean(r))
}
func parseIntAny(v any) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case string:
		n, _ := strconv.Atoi(t)
		return n
	case json.Number:
		n, _ := t.Int64()
		return int(n)
	}
	return 0
}
func vstr(m map[string]any, ks ...string) string {
	for _, k := range ks {
		for mk, mv := range m {
			if strings.EqualFold(mk, k) {
				if s, ok := mv.(string); ok {
					return s
				}
			}
		}
	}
	return ""
}
func vint(m map[string]any, ks ...string) int {
	for _, k := range ks {
		for mk, mv := range m {
			if strings.EqualFold(mk, k) {
				return parseIntAny(mv)
			}
		}
	}
	return 0
}
func vbool(m map[string]any, ks ...string) bool {
	for _, k := range ks {
		for mk, mv := range m {
			if !strings.EqualFold(mk, k) {
				continue
			}
			switch t := mv.(type) {
			case bool:
				return t
			case string:
				t = strings.TrimSpace(strings.ToLower(t))
				return t == "true" || t == "1" || t == "yes"
			case float64:
				return t != 0
			case json.Number:
				n, _ := t.Int64()
				return n != 0
			}
		}
	}
	return false
}
func extractMaps(v any, h string, out *[]MapEntry) {
	switch t := v.(type) {
	case map[string]any:
		n := vstr(t, "name", "display_name", "displayName")
		id := vint(t, "id", "map_id", "mapId")
		if id == 0 && h != "" {
			id, _ = strconv.Atoi(h)
		}
		pa := vint(t, "parent_id", "parentId", "parent")
		o := vint(t, "order")
		expanded := vbool(t, "expanded", "is_expanded", "isExpanded")
		if n != "" && id != 0 {
			*out = append(*out, MapEntry{ID: id, Name: n, ParentID: pa, Order: o, Expanded: expanded})
		}
		for k, c := range t {
			lk := strings.ToLower(k)
			if lk == "name" || lk == "display_name" || lk == "id" || lk == "map_id" || lk == "parent_id" || lk == "order" {
				continue
			}
			extractMaps(c, k, out)
		}
	case []any:
		for i, c := range t {
			extractMaps(c, strconv.Itoa(i), out)
		}
	}
}
func mapInfoCandidates(root string) []string {
	return []string{
		filepath.Join(root, "converted", "MapInfos.json"),
		filepath.Join(root, "converted", "map_infos.json"),
		filepath.Join(root, "converted", "mapinfos.json"),
		filepath.Join(root, "Data", "MapInfos.json"),
		filepath.Join(root, "data", "MapInfos.json"),
		filepath.Join(root, "MapInfos.json"),
	}
}

func mapsDirCandidates(root string) []string {
	candidates := []string{
		filepath.Join(root, "converted", "maps"),
		filepath.Join(root, "converted", "Maps"),
		filepath.Join(root, "maps"),
		filepath.Join(root, "Maps"),
		filepath.Join(root, "Data", "maps"),
		filepath.Join(root, "data", "maps"),
	}
	// Le cartelle dichiarate dal progetto e quelle delle regioni sono fonti
	// mappe di prima classe. In questo modo una regione appena creata viene
	// agganciata automaticamente al loader senza scansioni ricorsive casuali.
	candidates = append(candidates, configuredProjectMapFolders(root)...)
	candidates = append(candidates, configuredRegionMapFolders(root)...)

	seen := map[string]bool{}
	out := make([]string, 0, len(candidates))
	for _, dir := range candidates {
		dir = filepath.Clean(dir)
		key := strings.ToLower(dir)
		if dir == "." || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, dir)
	}
	return out
}

var mapJSONNameRE = regexp.MustCompile(`(?i)^map0*([0-9]+)\.json$`)
var embeddedMapNameRE = regexp.MustCompile(`(?i)"(?:name|map_name|display_name)"\s*:\s*("(?:\\.|[^"\\])*")`)

func embeddedMapName(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	const limit = 128 * 1024
	b, err := io.ReadAll(io.LimitReader(f, limit))
	if err != nil {
		return ""
	}
	m := embeddedMapNameRE.FindSubmatch(b)
	if len(m) != 2 {
		return ""
	}
	name, err := strconv.Unquote(string(m[1]))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(name)
}

func mapIDFromJSONName(name string) (int, bool) {
	m := mapJSONNameRE.FindStringSubmatch(filepath.Base(name))
	if len(m) != 2 {
		return 0, false
	}
	id, err := strconv.Atoi(m[1])
	return id, err == nil && id > 0
}

var (
	mapFileIndexRoot string
	mapFileIndex     map[int]string
)

func rebuildMapFileIndex(root string) map[int]string {
	root = filepath.Clean(root)
	index := make(map[int]string)

	// Converted projects use exactly one physical source of truth:
	// converted/maps, recursively. Region ownership is derived only from the
	// path below converted/maps/regions/<REGIONE>; no metadata preference is
	// allowed to override the filesystem.
	canonicalRoot := filepath.Join(root, "converted", "maps")
	if isDir(canonicalRoot) {
		copies, err := scanMapCopies(canonicalRoot)
		if err == nil {
			ids := make([]int, 0, len(copies))
			for id := range copies {
				ids = append(ids, id)
			}
			sort.Ints(ids)
			for _, id := range ids {
				paths := copies[id]
				if len(paths) == 1 {
					index[id] = filepath.Clean(paths[0])
					continue
				}
				// A divergent duplicate is a blocking project error, but the UI still
				// needs a deterministic file to display while the user resolves it.
				// Prefer the root copy because root explicitly means NON ASSEGNATA;
				// otherwise use lexical order without inferring any region ownership.
				rootPath := filepath.Join(canonicalRoot, fmt.Sprintf("Map%03d.json", id))
				chosen := ""
				for _, p := range paths {
					if strings.EqualFold(filepath.Clean(p), filepath.Clean(rootPath)) {
						chosen = p
						break
					}
				}
				if chosen == "" {
					chosen = paths[0]
				}
				index[id] = filepath.Clean(chosen)
				mapLogf("[MAP DUPLICATES] Map%03d ha %d copie; indicizzazione temporanea=%s", id, len(paths), chosen)
			}
			mapFileIndexRoot = root
			mapFileIndex = index
			return index
		}
	}

	// Fallback per progetti generici/legacy che non hanno converted/maps.
	for _, dir := range mapsDirCandidates(root) {
		if !isDir(dir) {
			continue
		}
		_ = filepath.WalkDir(dir, func(path string, e os.DirEntry, err error) error {
			if err != nil || e == nil || e.IsDir() {
				return nil
			}
			id, ok := mapIDFromJSONName(e.Name())
			if !ok {
				return nil
			}
			path = filepath.Clean(path)
			if previous, exists := index[id]; !exists || strings.ToLower(path) < strings.ToLower(previous) {
				index[id] = path
			}
			return nil
		})
	}
	mapFileIndexRoot = root
	mapFileIndex = index
	return index
}

func projectMapFileIndex(root string) map[int]string {
	root = filepath.Clean(root)
	if mapFileIndex == nil || !strings.EqualFold(mapFileIndexRoot, root) {
		return rebuildMapFileIndex(root)
	}
	return mapFileIndex
}

func findMapFile(root string, id int) string {
	if id <= 0 {
		return ""
	}
	return projectMapFileIndex(root)[id]
}

func readMapInfos(root string) []MapEntry {
	for _, p := range mapInfoCandidates(root) {
		b, err := os.ReadFile(p)
		if err != nil || len(b) == 0 {
			continue
		}
		var v any
		dec := json.NewDecoder(strings.NewReader(string(b)))
		dec.UseNumber()
		if err := dec.Decode(&v); err != nil {
			continue
		}
		var out []MapEntry
		extractMaps(v, "", &out)
		if len(out) > 0 {
			return out
		}
	}
	return nil
}

func scanMapFiles(root string) []MapEntry {
	index := projectMapFileIndex(root)
	out := make([]MapEntry, 0, len(index))
	for id, path := range index {
		name := embeddedMapName(path)
		if name == "" {
			name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		}
		out = append(out, MapEntry{ID: id, Name: name, File: path, RegionID: regionForMapPath(root, path)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func loadMaps(root string) []MapEntry {
	done := diagEnterUI("loadMaps()")
	defer done()
	diagLogf("[UI] loadMaps() root=%s START", root)
	defer diagLogf("[UI] loadMaps() root=%s END", root)
	// Build the MapXXX.json index once. MapInfos lookups are O(1) afterwards.
	index := rebuildMapFileIndex(root)
	raw := readMapInfos(root)
	uniq := make(map[int]MapEntry, len(raw))
	for _, m := range raw {
		if m.ID <= 0 || strings.TrimSpace(m.Name) == "" {
			continue
		}
		m.File = index[m.ID]
		m.RegionID = regionForMapPath(root, m.File)
		uniq[m.ID] = m
	}

	// MapInfos describes the original converted project, but PML Studio can
	// create/import additional MapXXX.json files afterwards. Merge every indexed
	// file that is not present in MapInfos so newly-authored regional maps become
	// first-class project maps immediately.
	for id, path := range index {
		if _, ok := uniq[id]; ok {
			continue
		}
		name := embeddedMapName(path)
		if name == "" {
			name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		}
		uniq[id] = MapEntry{ID: id, Name: name, File: path, RegionID: regionForMapPath(root, path)}
	}

	var out []MapEntry
	for _, m := range uniq {
		out = append(out, m)
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Order != out[j].Order {
			return out[i].Order < out[j].Order
		}
		return out[i].ID < out[j].ID
	})
	return out
}

var (
	jsonWidthRE     = regexp.MustCompile(`(?i)"width"\s*:\s*([0-9]+)`)
	jsonHeightRE    = regexp.MustCompile(`(?i)"height"\s*:\s*([0-9]+)`)
	jsonMapWidthRE  = regexp.MustCompile(`(?i)"map_width"\s*:\s*([0-9]+)`)
	jsonMapHeightRE = regexp.MustCompile(`(?i)"map_height"\s*:\s*([0-9]+)`)
)

func firstRegexpInt(b []byte, re *regexp.Regexp) int {
	m := re.FindSubmatch(b)
	if len(m) != 2 {
		return 0
	}
	n, _ := strconv.Atoi(string(m[1]))
	return n
}

func loadDims(m MapEntry) (int, int) {
	if m.File == "" {
		return 20, 15
	}
	f, err := os.Open(m.File)
	if err != nil {
		return 20, 15
	}
	defer f.Close()

	// The map JSON may contain very large tile/layer arrays. Opening a project
	// must not decode or traverse all those arrays merely to obtain dimensions.
	// Read only the header area where converters place map metadata.
	const headerLimit = 1024 * 1024
	b, err := io.ReadAll(io.LimitReader(f, headerLimit))
	if err != nil || len(b) == 0 {
		return 20, 15
	}

	w := firstRegexpInt(b, jsonMapWidthRE)
	h := firstRegexpInt(b, jsonMapHeightRE)
	if w <= 0 {
		w = firstRegexpInt(b, jsonWidthRE)
	}
	if h <= 0 {
		h = firstRegexpInt(b, jsonHeightRE)
	}
	if w <= 0 || h <= 0 {
		return 20, 15
	}

	// Reject absurd/corrupt values before they reach the renderer.
	if w > 10000 || h > 10000 {
		return 20, 15
	}
	return w, h
}
func currentMapIndex() int {
	if currentMap == nil {
		return -1
	}
	for i := range maps {
		if maps[i].ID == currentMap.ID {
			return i
		}
	}
	return -1
}

func saveCurrentMap() bool {
	if currentMapDoc == nil || !mapDirty {
		return true
	}
	if err := ensureProjectMapFilesystemSafe("Salvataggio"); err != nil {
		msgbox("PML Studio", err.Error(), MB_OK|MB_ICONERROR)
		return false
	}
	if err := saveMapDocument(currentMapDoc); err != nil {
		msgbox("PLM Studio", "Impossibile salvare la mappa reale:\r\n\r\n"+err.Error(), MB_OK|MB_ICONERROR)
		return false
	}
	mapDirty = false
	updateMapEditorStatus()
	invalidate(hwndCanvas)
	return true
}

func confirmMapChanges() bool {
	if !mapDirty || currentMapDoc == nil {
		return true
	}
	name := "mappa corrente"
	if currentMap != nil {
		name = fmt.Sprintf("%03d - %s", currentMap.ID, currentMap.Name)
	}
	result := msgboxResult(
		"PLM Studio",
		"La mappa "+name+" contiene modifiche non salvate.\r\n\r\nSì = Salva\r\nNo = Non salvare\r\nAnnulla = Resta sulla mappa",
		MB_YESNOCANCEL|MB_ICONINFORMATION,
	)
	switch result {
	case IDYES:
		return saveCurrentMap()
	case IDNO:
		mapDirty = false
		return true
	default:
		return false
	}
}

func selectMap(idx int) {
	done := diagEnterUI("selectMap()")
	defer done()
	diagLogf("[UI] selectMap() idx=%d START", idx)
	defer diagLogf("[UI] selectMap() idx=%d END", idx)
	if idx < 0 || idx >= len(maps) {
		return
	}
	oldIndex := currentMapIndex()
	if currentMap != nil && oldIndex != idx {
		if !confirmHeaderChanges() || !confirmMapChanges() {
			if oldIndex >= 0 {
				selectMapListIndexForMap(oldIndex)
			}
			return
		}
	}

	m := &maps[idx]
	if m.File == "" {
		msgbox("PLM Studio", fmt.Sprintf("Il file reale della mappa %03d non è stato trovato.", m.ID), MB_OK|MB_ICONERROR)
		if oldIndex >= 0 {
			selectMapListIndexForMap(oldIndex)
		}
		return
	}

	// Il caricamento pesante viene eseguito fuori dal message thread Win32.
	// In questo modo la finestra rimane reattiva e il canvas mostra una
	// percentuale reale di avanzamento invece di apparire bloccato.
	startAsyncMapLoad(idx, oldIndex)
}

func mapDepth(m MapEntry, byID map[int]MapEntry) int {
	depth := 0
	seen := map[int]bool{}
	p := m.ParentID
	for p != 0 && depth < 6 && !seen[p] {
		seen[p] = true
		parent, ok := byID[p]
		if !ok {
			break
		}
		depth++
		p = parent.ParentID
	}
	return depth
}

func mapDisplayLabel(m MapEntry, byID map[int]MapEntry) string {
	depth := mapDepth(m, byID)
	prefix := ""
	for i := 0; i < depth; i++ {
		prefix += "   "
	}
	if depth > 0 {
		prefix += "- "
	}
	return fmt.Sprintf("%s%03d  %s", prefix, m.ID, m.Name)
}
func populateProject(root string) {
	defer func() {
		if r := recover(); r != nil {
			currentMap = nil
			maps = nil
			showMapTreeMessage("[Errore durante il caricamento del progetto]")
			updateMapQuickInfo()
			setText(hwndStatus, "Errore caricamento progetto")
			msgbox("PLM Studio", fmt.Sprintf("Il progetto non può essere caricato completamente.\r\n\r\nDettaglio: %v", r), MB_OK|MB_ICONERROR)
		}
	}()

	root = filepath.Clean(root)
	if root == "" || !isDir(root) {
		msgbox("PLM Studio", "La cartella selezionata non è valida.", MB_OK|MB_ICONERROR)
		return
	}

	// Any loader still running belongs to the previous project. Invalidate it
	// before replacing currentProject/maps/currentMapDoc; otherwise a late result
	// with the same numeric map index could be applied to the new project.
	cancelMapLoadsForProjectSwitch()

	// La conferma delle modifiche viene gestita prima di cambiare progetto.
	// Da questo punto azzeriamo lo stato della vecchia mappa per evitare
	// puntatori alla precedente slice maps durante la selezione iniziale.
	mapDirty = false
	permissionDataDirty = false
	encounterDirty = false
	dataDoc = nil
	dataLoadedProject = ""
	currentMap = nil
	currentMapDoc = nil
	maps = nil
	currentTileset = nil
	currentMapSurface = nil
	clearMapHistory()
	mapScrollX, mapScrollY = 0, 0

	currentProject = root
	currentProjectType = detectProjectType(root)
	applyProjectEditorMode(root)
	resetMapEditorRegionFilter()
	// Converted projects use assets/Graphics + assets/Audio as the canonical
	// resource roots. Rebuild the asset index immediately so a freshly imported
	// project never inherits stale paths from the previous project.
	if isDir(filepath.Join(root, "assets")) {
		if err := ensureCanonicalAssetLayout(root); err != nil {
			mapLogf("[ASSETS] canonical layout: %v", err)
		}
	}
	assetIndexProject = ""
	assetFilesByBase = nil
	assetGraphicsDirs = nil
	rebuildAssetIndex(root)
	// Carica subito le funzioni narrative del progetto. In questo modo anche
	// il comando Salva tutto preserva correttamente le opzioni senza richiedere
	// che l'utente apra prima la Vista Header.
	loadProjectFeatures()
	loadRegionRegistry()
	// Indicizza gli sprite reali del progetto per la Vista eventi. Gli eventi
	// mantengono character_name nel payload MapXXX.json e vengono renderizzati
	// direttamente dai PNG presenti in Graphics/Characters.
	if err := rebuildCharacterCatalog(root); err != nil {
		mapLogf("[CHARACTERS] indicizzazione fallita: %v", err)
	}
	// Carica le palette PNG numerate da assets/Graphics/Tilesets e aggiorna
	// converted/data/palettes/index.json. Sono accettati solo i file
	// "pallette <numero>.png"; gli altri PNG non sono palette.
	if err := rebuildPaletteCatalog(root); err != nil {
		mapLogf("[PALETTE] caricamento catalogo: %v", err)
	}
	// La prima versione dell'editor incontri salvava MapXXX.json sotto
	// converted/maps/encounters. Questi file NON sono mappe e devono essere
	// migrati prima del controllo duplicati, altrimenti possono sembrare Map ID
	// reali in conflitto.
	if err := migrateLegacyEncounterStorage(); err != nil {
		mapLogf("[ENCOUNTERS] migrazione storage legacy: %v", err)
		msgbox("PML Studio - Migrazione incontri", "Non tutti i dati incontri legacy sono stati migrati automaticamente.\r\n\r\n"+err.Error(), MB_OK|MB_ICONINFORMATION)
	}
	// Migrazione/repair fisica: il filesystem è la fonte di verità. Le copie
	// legacy identiche vengono consolidate; copie differenti non vengono mai
	// cancellate e bloccano salvataggio/playtest finché l'utente non risolve.
	if report, err := repairProjectDuplicateMapIDs(false); err != nil {
		mapLogf("[MAP DUPLICATES] repair apertura fallito: %v", err)
	} else {
		if len(report.RepairedIDs) > 0 {
			mapLogf("[MAP DUPLICATES] apertura: %d Map ID riparati, %d copie eliminate", len(report.RepairedIDs), len(report.RemovedPaths))
		}
		if len(report.Conflicts) > 0 {
			msgbox("PML Studio - Conflitto Map ID", duplicateConflictMessage(report.Conflicts), MB_OK|MB_ICONERROR)
		}
	}
	name := projectName(root)
	setText(hwndMain, "PLM Studio - "+name+" ["+projectEditorModeLabel(currentProjectEditorMode)+"]")
	setText(hwndProjectInfo, name+"\r\n"+currentProjectType+"\r\n"+currentProject)
	clearMapTreeUI()

	maps = loadMaps(root)
	// Rileva il vero punto iniziale del giocatore dai comandi evento della
	// intro (Transfer Player) oppure carica quello impostato manualmente.
	// Map001 non viene mai considerata automaticamente giocabile solo per ID.
	loadOrDetectGameStart()
	refreshMapEditorRegionControls()
	if len(maps) > 0 {
		playableCount := 0
		for _, m := range maps {
			if !isIntroAnimationMapID(m.ID) {
				playableCount++
			}
		}
		setText(hwndStatus, fmt.Sprintf("Aperto: %s | %d mappe giocabili | %d eventi/animazioni | %d regioni", name, playableCount, len(gameStart.IntroMapIDs), len(projectRegions.Regions)))
		if idx := firstNormalMapIndex(); idx >= 0 {
			selectMap(idx)
		}
	} else {
		currentMap = nil
		updateMapQuickInfo()
		invalidate(hwndCanvas)
	}
	rememberProject(root)
	createMainMenu(hwndMain)
}
func projectRootFromMain(mainPath string) (string, error) {
	mainPath = filepath.Clean(strings.TrimSpace(mainPath))
	if mainPath == "" {
		return "", fmt.Errorf("main.py non selezionato")
	}
	info, err := os.Stat(mainPath)
	if err != nil || info.IsDir() {
		return "", fmt.Errorf("il file selezionato non è valido")
	}
	if !strings.EqualFold(filepath.Base(mainPath), "main.py") {
		return "", fmt.Errorf("seleziona il file main.py del progetto")
	}
	return filepath.Dir(mainPath), nil
}

func openProject() {
	mainPath := browseProjectMain("Apri progetto PLM convertito")
	if mainPath == "" {
		return
	}
	root, err := projectRootFromMain(mainPath)
	if err != nil {
		msgbox("PLM Studio", "Impossibile aprire il progetto:\r\n\r\n"+err.Error(), MB_OK|MB_ICONERROR)
		return
	}
	if !confirmUnsavedProjectChanges("aprire un altro progetto") {
		return
	}
	populateProject(root)
}
func reopen() {
	if !confirmUnsavedProjectChanges("riaprire l'ultimo progetto") {
		return
	}
	if settings.LastProject != "" && isDir(settings.LastProject) {
		populateProject(settings.LastProject)
	} else {
		msgbox("PLM Studio", "Nessun progetto recente disponibile.", MB_OK|MB_ICONINFORMATION)
	}
}
func newProject() {
	source := browseFolder("Seleziona il progetto Pokémon Essentials / RPG Maker XP da convertire")
	if source == "" {
		return
	}
	if err := validateEssentialsProject(source); err != nil {
		msgbox("PLM Studio - Nuovo progetto", "Progetto Essentials non valido:\r\n\r\n"+err.Error(), MB_OK|MB_ICONERROR)
		return
	}
	dest := browseFolderAt("Seleziona la cartella di destinazione del nuovo progetto Python", settings.ProjectsPath)
	if dest == "" {
		return
	}
	if strings.EqualFold(filepath.Clean(source), filepath.Clean(dest)) {
		msgbox("PLM Studio - Nuovo progetto", "La cartella Python deve essere diversa dal progetto Essentials originale.\r\n\r\nPLM non modifica mai il progetto sorgente.", MB_OK|MB_ICONERROR)
		return
	}
	if destinationLooksNonEmpty(dest) {
		answer := msgboxResult("PLM Studio - Nuovo progetto", "La cartella di destinazione non è vuota.\r\n\r\nI file con lo stesso nome potranno essere sostituiti durante la conversione. Continuare?", MB_YESNOCANCEL|MB_ICONINFORMATION)
		if answer != IDYES {
			return
		}
	}
	if !confirmUnsavedProjectChanges("creare un nuovo progetto") {
		return
	}
	mode, ok := chooseNewProjectEditorMode()
	if !ok {
		return
	}
	setToolbarStatus("Conversione progetto Pokémon Essentials in Python...")
	report, err := showEssentialsConversionProgress(hwndMain, source, dest)
	if err != nil {
		setToolbarStatus("Conversione fallita: " + err.Error())
		msgbox("PLM Studio - Conversione Essentials", "Conversione non riuscita:\r\n\r\n"+err.Error()+"\r\n\r\nIl progetto Essentials originale non è stato modificato.", MB_OK|MB_ICONERROR)
		return
	}
	if err := saveProjectEditorMode(dest, mode); err != nil {
		msgbox("PLM Studio - Modalità progetto", "Conversione completata, ma non è stato possibile salvare la modalità scelta:\r\n\r\n"+err.Error(), MB_OK|MB_ICONERROR)
		return
	}
	populateProject(dest)
	setToolbarStatus(fmt.Sprintf("Conversione completata: %d mappe, %d regioni, %d archivi dati · %s", report.MapsConverted, report.RegionsCreated, report.DataFilesConverted, projectEditorModeLabel(mode)))
	msg := fmt.Sprintf("Progetto convertito in PLM/Python.\r\n\r\nMappe: %d\r\nRegioni create: %d\r\nMappe assegnate alle regioni: %d\r\nArchivi dati: %d\r\nScript Ruby estratti: %d", report.MapsConverted, report.RegionsCreated, report.RegionalMapsOrganized, report.DataFilesConverted, report.RubyScriptsExtracted)
	if len(report.Warnings) > 0 {
		msg += fmt.Sprintf("\r\n\r\nAvvisi: %d (vedi converted\\conversion_report.json)", len(report.Warnings))
	}
	msgbox("PLM Studio - Conversione completata", msg, MB_OK|MB_ICONINFORMATION)
}
func saveAll() {
	_ = saveAllProjectChanges()
}
