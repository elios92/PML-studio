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
)

const (
	regionRegistryVersion = 1
	cbResetContent        = 0x014B
)

type RegionDefinition struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	RootFolder    string `json:"root_folder"`
	MapsFolder    string `json:"maps_folder"`
	ScriptsFolder string `json:"scripts_folder"`
	DataFolder    string `json:"data_folder"`
}

type RegionRegistry struct {
	Version        int                `json:"version"`
	Enabled        bool               `json:"enabled"`
	ActiveRegionID string             `json:"active_region_id,omitempty"`
	Regions        []RegionDefinition `json:"regions"`
}

var (
	projectRegions    RegionRegistry
	hwndRegionName    syscall.Handle
	hwndRegionAdd     syscall.Handle
	hwndRegionList    syscall.Handle
	regionFolderPaths []string
)

func regionRegistryPath() string {
	if currentProject == "" {
		return ""
	}
	return filepath.Join(currentProject, "plm_regions.json")
}

func regionSlug(name string) string {
	name = strings.TrimSpace(name)
	var b strings.Builder
	lastSep := false
	for _, r := range name {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(unicode.ToLower(r))
			lastSep = false
		case r == '-' || r == '_' || unicode.IsSpace(r):
			if b.Len() > 0 && !lastSep {
				b.WriteByte('_')
				lastSep = true
			}
		}
	}
	return strings.Trim(b.String(), "_")
}

func cleanProjectRelativePath(p string) string {
	p = filepath.Clean(strings.TrimSpace(p))
	if p == "." || p == "" {
		return ""
	}
	p = strings.TrimPrefix(p, string(filepath.Separator))
	return filepath.ToSlash(p)
}

func canonicalRegionsRootRelative() string {
	return filepath.ToSlash(filepath.Join("converted", "maps", "regions"))
}

func canonicalRegionsRootPath() string {
	if currentProject == "" {
		return ""
	}
	return filepath.Clean(filepath.Join(currentProject, "converted", "maps", "regions"))
}

func canonicalConvertedMapsPath() string {
	if currentProject == "" {
		return ""
	}
	return filepath.Clean(filepath.Join(currentProject, "converted", "maps"))
}

func regionFolderComponent(name, id string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		name = id
	}
	var b strings.Builder
	lastSep := false
	for _, r := range name {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(unicode.ToUpper(r))
			lastSep = false
		case r == '-' || r == '_' || unicode.IsSpace(r):
			if b.Len() > 0 && !lastSep {
				b.WriteByte('_')
				lastSep = true
			}
		}
	}
	folder := strings.Trim(b.String(), "_")
	if folder == "" {
		folder = strings.ToUpper(strings.TrimSpace(id))
	}
	return folder
}

func canonicalRegionDefinition(name, id string) RegionDefinition {
	name = strings.TrimSpace(name)
	if strings.TrimSpace(id) == "" {
		id = regionSlug(name)
	}
	folder := regionFolderComponent(name, id)
	mapRoot := filepath.ToSlash(filepath.Join("converted", "maps", "regions", folder))
	return RegionDefinition{
		ID:         id,
		Name:       name,
		RootFolder: mapRoot,
		// Nella nuova struttura la cartella della regione È la cartella mappe:
		// converted/maps/regions/<REGIONE>/MapXXX.json. Non esiste un ulteriore
		// livello Maps, così il filesystem e l'albero dell'editor coincidono.
		MapsFolder:    mapRoot,
		ScriptsFolder: filepath.ToSlash(filepath.Join("converted", "scripts", "regions", folder)),
		DataFolder:    filepath.ToSlash(filepath.Join("converted", "data", "regions", folder)),
	}
}

func defaultRegionDefinition(name string) RegionDefinition {
	return canonicalRegionDefinition(name, regionSlug(name))
}

func regionByID(id string) *RegionDefinition {
	for i := range projectRegions.Regions {
		if strings.EqualFold(projectRegions.Regions[i].ID, id) {
			return &projectRegions.Regions[i]
		}
	}
	return nil
}

func regionNameByID(id string) string {
	if r := regionByID(id); r != nil {
		return r.Name
	}
	return ""
}

// discoverCanonicalRegionFolders rende il filesystem la fonte di verità per la
// nuova struttura converted/maps/regions/<REGIONE>. Se una cartella esiste ma
// non è ancora nel registro, viene registrata; se il registro contiene ancora
// i vecchi percorsi, viene riallineato solo quando trova la nuova cartella
// fisica, evitando di perdere mappe rimaste in una vecchia build.
func discoverCanonicalRegionFolders() bool {
	root := canonicalRegionsRootPath()
	if root == "" || !isDir(root) {
		return false
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return false
	}
	changed := false
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		folderName := strings.TrimSpace(entry.Name())
		if folderName == "" {
			continue
		}
		id := regionSlug(folderName)
		if id == "" {
			continue
		}
		discovered := canonicalRegionDefinition(folderName, id)
		idx := -1
		for i := range projectRegions.Regions {
			r := projectRegions.Regions[i]
			if strings.EqualFold(r.ID, id) || strings.EqualFold(regionFolderComponent(r.Name, r.ID), folderName) {
				idx = i
				break
			}
		}
		if idx < 0 {
			projectRegions.Regions = append(projectRegions.Regions, discovered)
			changed = true
			continue
		}
		old := projectRegions.Regions[idx]
		// Preserve the user-facing name when it is meaningful, but use the
		// physical folder casing/path discovered on disk.
		if strings.TrimSpace(old.Name) != "" {
			discovered.Name = old.Name
		}
		if old.RootFolder != discovered.RootFolder || old.MapsFolder != discovered.MapsFolder || old.ScriptsFolder != discovered.ScriptsFolder || old.DataFolder != discovered.DataFolder {
			projectRegions.Regions[idx] = discovered
			changed = true
		}
	}
	if len(projectRegions.Regions) > 0 && !projectRegions.Enabled {
		projectRegions.Enabled = true
		changed = true
	}
	if projectRegions.ActiveRegionID == "" && len(projectRegions.Regions) > 0 {
		projectRegions.ActiveRegionID = projectRegions.Regions[0].ID
		changed = true
	}
	return changed
}

func loadRegionRegistry() {
	projectRegions = RegionRegistry{Version: regionRegistryVersion}
	p := regionRegistryPath()
	if p == "" {
		refreshRegionControls()
		return
	}
	b, err := os.ReadFile(p)
	if err == nil {
		var doc RegionRegistry
		if json.Unmarshal(b, &doc) == nil {
			if doc.Version <= 0 {
				doc.Version = regionRegistryVersion
			}
			projectRegions = doc
		}
	}
	changed := discoverCanonicalRegionFolders()
	sort.SliceStable(projectRegions.Regions, func(i, j int) bool {
		return strings.ToLower(projectRegions.Regions[i].Name) < strings.ToLower(projectRegions.Regions[j].Name)
	})
	// Se abbiamo scoperto/riallineato cartelle reali, persisti subito il nuovo
	// percorso canonico. Così il loader e il runtime vedono gli stessi dati già
	// durante questa apertura del progetto, senza richiedere un Salva manuale.
	if changed {
		if raw, marshalErr := json.MarshalIndent(projectRegions, "", "  "); marshalErr == nil {
			_ = os.WriteFile(p, raw, 0644)
			_ = syncProjectMapFolders()
			_ = writeRuntimeRegionModule()
		}
	}
	refreshRegionControls()
}

func saveRegionRegistry() error {
	p := regionRegistryPath()
	if p == "" {
		return fmt.Errorf("nessun progetto aperto")
	}
	projectRegions.Version = regionRegistryVersion
	projectRegions.Enabled = isChecked(hwndFeatureMultiRegions)
	b, err := json.MarshalIndent(projectRegions, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(p, b, 0644); err != nil {
		return err
	}
	if err := syncProjectMapFolders(); err != nil {
		return err
	}
	return writeRuntimeRegionModule()
}

func ensureRegionFolders(r RegionDefinition) error {
	if currentProject == "" {
		return fmt.Errorf("nessun progetto aperto")
	}

	// Una regione PLM ha un solo requisito fisico: la cartella che contiene
	// realmente i MapXXX.json. Data/Scripts sono percorsi logici opzionali e
	// non devono essere creati finché una funzione futura non avrà davvero dei
	// dati da salvarci. In particolare __init__.py/region_info.py non sono
	// necessari al runtime e non devono rendere la regione un package Python.
	mapsRel := cleanProjectRelativePath(r.MapsFolder)
	if mapsRel == "" {
		return fmt.Errorf("percorso mappe regione non valido")
	}
	mapsRoot := filepath.Join(currentProject, filepath.FromSlash(mapsRel))
	if err := os.MkdirAll(mapsRoot, 0755); err != nil {
		return err
	}

	// region.json resta accanto alle mappe come metadato leggero della regione.
	// Non dipende dall'esistenza di Data, Scripts o di file Python.
	manifestPath := filepath.Join(mapsRoot, "region.json")
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(manifestPath, b, 0644)
}

func verifyRegionFolders(r RegionDefinition) error {
	if currentProject == "" {
		return fmt.Errorf("nessun progetto aperto")
	}

	// Solo la cartella mappe è strutturalmente obbligatoria. Data e Scripts
	// possono non esistere (o essere rimossi manualmente) senza compromettere
	// apertura, salvataggio, spostamento mappe o Playtest.
	rel := cleanProjectRelativePath(r.MapsFolder)
	if rel == "" {
		return fmt.Errorf("percorso mappe regione non valido")
	}
	abs := filepath.Join(currentProject, filepath.FromSlash(rel))
	info, err := os.Stat(abs)
	if err != nil {
		return fmt.Errorf("cartella mappe regione non creata: %s: %w", abs, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("il percorso mappe della regione non è una cartella: %s", abs)
	}
	return nil
}

func createRegionFromHeader() error {
	if currentProject == "" {
		return fmt.Errorf("nessun progetto aperto")
	}
	name := strings.TrimSpace(getText(hwndRegionName))
	if name == "" {
		return fmt.Errorf("inserisci il nome della regione")
	}
	candidate := defaultRegionDefinition(name)
	if candidate.ID == "" {
		return fmt.Errorf("nome regione non valido")
	}

	// Se la regione è già registrata ma le cartelle sono state cancellate o una
	// build precedente non le aveva create, "Aggiungi regione" ripara la
	// struttura fisica invece di fermarsi sul duplicato.
	r := candidate
	existingIndex := -1
	for i := range projectRegions.Regions {
		existing := projectRegions.Regions[i]
		if strings.EqualFold(existing.ID, candidate.ID) || strings.EqualFold(strings.TrimSpace(existing.Name), candidate.Name) {
			existingIndex = i
			// La nuova struttura è canonica: anche una regione registrata da una
			// build precedente viene riallineata a converted/maps/regions/<REGIONE>.
			// Il nome visuale resta quello già scelto dall'utente.
			if strings.TrimSpace(existing.Name) != "" {
				r.Name = existing.Name
			}
			break
		}
	}

	if err := ensureRegionFolders(r); err != nil {
		return fmt.Errorf("creazione cartelle regione fallita: %w", err)
	}
	if err := verifyRegionFolders(r); err != nil {
		return err
	}

	setChecked(hwndFeatureMultiRegions, true)
	projectRegions.Enabled = true
	if existingIndex < 0 {
		projectRegions.Regions = append(projectRegions.Regions, r)
	} else {
		projectRegions.Regions[existingIndex] = r
	}
	projectRegions.ActiveRegionID = r.ID
	sort.SliceStable(projectRegions.Regions, func(i, j int) bool {
		return strings.ToLower(projectRegions.Regions[i].Name) < strings.ToLower(projectRegions.Regions[j].Name)
	})
	if err := saveRegionRegistry(); err != nil {
		return err
	}
	if err := saveProjectFeatures(); err != nil {
		return err
	}
	setText(hwndRegionName, "")

	// Mantieni la vista globale: la nuova regione viene aperta, mentre le
	// regioni già esistenti restano visibili come cartelle chiuse. In questo
	// modo non perdi mai il contesto della struttura principale del progetto.
	mapRegionFilter = mapRegionFilterAll
	rebuildMapFileIndex(currentProject)
	refreshRegionControls()
	refreshMapListUI()
	focusRegionFolderInTree(r.ID)
	return nil
}

func regionMapsRoot(r *RegionDefinition) string {
	if currentProject == "" || r == nil {
		return ""
	}
	return filepath.Clean(filepath.Join(currentProject, filepath.FromSlash(r.MapsFolder)))
}

// refreshRegionFolderControls collega la Vista Header alla struttura fisica
// della regione. La combo contiene soltanto cartelle reali sotto Maps e ogni
// indice punta al relativo percorso assoluto, evitando path scritti a mano.
func refreshRegionFolderControls() {
	if hwndRegionFolderList == 0 {
		return
	}
	pSendMessageW.Call(uintptr(hwndRegionFolderList), cbResetContent, 0, 0)
	regionFolderPaths = regionFolderPaths[:0]

	r := activeRegion()
	root := regionMapsRoot(r)
	if r == nil || root == "" || !isDir(root) {
		comboAdd(hwndRegionFolderList, "Nessuna cartella Maps disponibile")
		pSendMessageW.Call(uintptr(hwndRegionFolderList), CB_SETCURSEL, 0, 0)
		return
	}

	type folderChoice struct {
		path  string
		label string
	}
	choices := []folderChoice{{path: root, label: r.Name + " (radice regione)"}}
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry == nil || !entry.IsDir() {
			return nil
		}
		path = filepath.Clean(path)
		if strings.EqualFold(path, root) {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil || rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
			return nil
		}
		choices = append(choices, folderChoice{path: path, label: r.Name + " / " + filepath.ToSlash(rel)})
		return nil
	})
	sort.SliceStable(choices[1:], func(i, j int) bool {
		return strings.ToLower(choices[i+1].label) < strings.ToLower(choices[j+1].label)
	})

	selected := 0
	currentDir := ""
	if currentMap != nil && strings.EqualFold(currentMap.RegionID, r.ID) && currentMap.File != "" {
		currentDir = filepath.Clean(filepath.Dir(currentMap.File))
	}
	for i, choice := range choices {
		comboAdd(hwndRegionFolderList, choice.label)
		regionFolderPaths = append(regionFolderPaths, choice.path)
		if currentDir != "" && strings.EqualFold(currentDir, choice.path) {
			selected = i
		}
	}
	pSendMessageW.Call(uintptr(hwndRegionFolderList), CB_SETCURSEL, uintptr(selected), 0)
}

func selectedHeaderRegionDestination() (string, error) {
	r := activeRegion()
	root := regionMapsRoot(r)
	if r == nil || root == "" {
		return "", fmt.Errorf("crea prima una regione")
	}
	idx := comboSel(hwndRegionFolderList)
	if idx < 0 || idx >= len(regionFolderPaths) {
		return "", fmt.Errorf("seleziona una cartella di destinazione")
	}
	target := filepath.Clean(regionFolderPaths[idx])
	if !pathWithinRoot(root, target) || !isDir(target) {
		return "", fmt.Errorf("cartella di destinazione non valida")
	}
	return target, nil
}

func selectedHeaderRegionDestinationLabel() string {
	target, err := selectedHeaderRegionDestination()
	if err != nil || currentProject == "" {
		return ""
	}
	rel, err := filepath.Rel(currentProject, target)
	if err != nil {
		return filepath.ToSlash(target)
	}
	return filepath.ToSlash(rel)
}

func refreshRegionControls() {
	refreshRegionControlsInternal(true)
}

// refreshRegionControlsNoTree aggiorna i controlli Header/combo senza
// ricostruire la navigazione mappe. La regione attiva può cambiare quando si
// apre una mappa, ma questo non modifica la gerarchia dei file.
func refreshRegionControlsNoTree() {
	refreshRegionControlsInternal(false)
}

func refreshRegionControlsInternal(rebuildTree bool) {
	if hwndRegionList != 0 {
		pSendMessageW.Call(uintptr(hwndRegionList), cbResetContent, 0, 0)
		if len(projectRegions.Regions) == 0 {
			comboAdd(hwndRegionList, "Nessuna regione creata")
			pSendMessageW.Call(uintptr(hwndRegionList), CB_SETCURSEL, 0, 0)
		} else {
			selected := 0
			for i, r := range projectRegions.Regions {
				comboAdd(hwndRegionList, r.Name+"  ["+r.ID+"]")
				if strings.EqualFold(r.ID, projectRegions.ActiveRegionID) {
					selected = i
				}
			}
			pSendMessageW.Call(uintptr(hwndRegionList), CB_SETCURSEL, uintptr(selected), 0)
		}
	}
	// La cartella di destinazione della Vista Header e il navigatore mappe
	// condividono lo stesso registro e lo stesso filesystem.
	refreshRegionFolderControls()
	if rebuildTree {
		refreshMapEditorRegionControls()
	} else {
		refreshMapEditorRegionControlsNoTree()
	}
}

func setActiveRegionFromCombo() {
	if hwndRegionList == 0 || len(projectRegions.Regions) == 0 {
		return
	}
	idx, _, _ := pSendMessageW.Call(uintptr(hwndRegionList), CB_GETCURSEL, 0, 0)
	i := int(idx)
	if i < 0 || i >= len(projectRegions.Regions) {
		return
	}
	projectRegions.ActiveRegionID = projectRegions.Regions[i].ID
	_ = saveRegionRegistry()
	refreshRegionControls()
}

// syncActiveRegionToMap mantiene la regione attiva coerente con la mappa
// realmente aperta. Non cambia il filtro della sidebar, quindi "Tutte le
// regioni" resta una vista globale anche passando da una regione all'altra.
func syncActiveRegionToMap(regionID string) {
	if strings.TrimSpace(regionID) == "" || regionByID(regionID) == nil {
		return
	}
	if strings.EqualFold(projectRegions.ActiveRegionID, regionID) {
		return
	}
	projectRegions.ActiveRegionID = regionID
	_ = saveRegionRegistry()
	refreshRegionControlsNoTree()
}

func mapCreationTargetLabel() string {
	if currentProject != "" {
		if target := selectedMapFolderTarget(); target != "" {
			if rel, err := filepath.Rel(currentProject, target); err == nil {
				return filepath.ToSlash(rel)
			}
			return target
		}
	}
	if projectRegions.Enabled {
		if r := activeRegion(); r != nil {
			return fmt.Sprintf("%s [%s]", r.Name, r.MapsFolder)
		}
	}
	return "cartella mappe principale del progetto"
}

func activeRegion() *RegionDefinition {
	if len(projectRegions.Regions) == 0 {
		return nil
	}
	if r := regionByID(projectRegions.ActiveRegionID); r != nil {
		return r
	}
	return &projectRegions.Regions[0]
}

func pathWithin(base, candidate string) bool {
	base = filepath.Clean(base)
	candidate = filepath.Clean(candidate)
	if base == "" || candidate == "" || base == "." || candidate == "." {
		return false
	}
	rel, err := filepath.Rel(base, candidate)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func canonicalFlatMapPath(mapID int) string {
	if currentProject == "" || mapID <= 0 {
		return ""
	}
	return filepath.Join(canonicalConvertedMapsPath(), fmt.Sprintf("Map%03d.json", mapID))
}

func mapDescendantIndices(rootID int) []int {
	if rootID <= 0 {
		return nil
	}
	children := make(map[int][]int)
	for i := range maps {
		if maps[i].ID <= 0 || maps[i].ParentID <= 0 {
			continue
		}
		children[maps[i].ParentID] = append(children[maps[i].ParentID], i)
	}
	out := make([]int, 0)
	seen := map[int]bool{rootID: true}
	queue := append([]int(nil), children[rootID]...)
	for len(queue) > 0 {
		idx := queue[0]
		queue = queue[1:]
		if idx < 0 || idx >= len(maps) {
			continue
		}
		id := maps[idx].ID
		if id <= 0 || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, idx)
		queue = append(queue, children[id]...)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := maps[out[i]], maps[out[j]]
		if a.Order != b.Order {
			return a.Order < b.Order
		}
		return a.ID < b.ID
	})
	return out
}

type mapMovePlanItem struct {
	MapID       int
	MapIndex    int
	OldPath     string
	TargetPath  string
	OldRegionID string
	BackupOld   string
	BackupNew   string
	TargetMade  bool
	BackupMade  bool
}

func moveMapBranchToRegion(rootIndex int, descendantIndices []int, r RegionDefinition, targetDir string) error {
	return moveMapBranchToRegionSingleSource(rootIndex, descendantIndices, r, targetDir)
}

func assignCurrentMapToActiveRegion() error {
	if currentProject == "" {
		return fmt.Errorf("nessun progetto aperto")
	}
	if currentMap == nil || currentMapDoc == nil {
		return fmt.Errorf("seleziona prima una mappa")
	}
	r := activeRegion()
	if r == nil {
		return fmt.Errorf("crea prima una regione")
	}
	if !saveCurrentMap() {
		return fmt.Errorf("la mappa corrente non è stata salvata")
	}
	if err := ensureRegionFolders(*r); err != nil {
		return err
	}
	targetDir, err := selectedHeaderRegionDestination()
	if err != nil {
		return err
	}

	rootIndex := -1
	for i := range maps {
		if maps[i].ID == currentMap.ID {
			rootIndex = i
			break
		}
	}
	if rootIndex < 0 {
		return fmt.Errorf("mappa corrente non presente nell'indice")
	}
	descendants := mapDescendantIndices(currentMap.ID)
	moveDescendants := false
	if len(descendants) > 0 {
		answer := msgboxResult(
			"PLM Studio",
			fmt.Sprintf("La mappa \"%s\" contiene %d sotto-mappa/e nel ramo RPG Maker.\r\n\r\nSpostare nella regione %s anche tutte le sotto-mappe?\r\n\r\nSì = sposta l'intero ramo\r\nNo = sposta solo questa mappa\r\nAnnulla = non modificare nulla", currentMap.Name, len(descendants), r.Name),
			MB_YESNOCANCEL|MB_ICONINFORMATION,
		)
		switch answer {
		case IDYES:
			moveDescendants = true
		case IDNO:
			moveDescendants = false
		default:
			return fmt.Errorf("operazione annullata")
		}
	}
	selectedDescendants := []int(nil)
	if moveDescendants {
		selectedDescendants = descendants
	}
	mapID := currentMap.ID
	if err := moveMapBranchToRegion(rootIndex, selectedDescendants, *r, targetDir); err != nil {
		return err
	}

	// Rescan dal filesystem e aggiorna albero/bridge senza cambiare Map ID o
	// gerarchia ParentID: in game la mappa resta la stessa, cambia solo il path.
	mapRegionFilter = mapRegionFilterAll
	reloadMapsAfterFilesystemChange(mapID)
	loadHeaderFromCurrentMap()
	updateMapQuickInfo()
	refreshRegionFolderControls()
	if moveDescendants {
		setToolbarStatus(fmt.Sprintf("Ramo spostato in %s: %d mappe", r.Name, 1+len(selectedDescendants)))
	} else {
		setToolbarStatus("Mappa spostata in " + r.Name)
	}
	return nil
}

func regionMapFiles(r RegionDefinition) map[int]string {
	out := map[int]string{}
	if currentProject == "" {
		return out
	}
	root := filepath.Join(currentProject, filepath.FromSlash(r.MapsFolder))
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry == nil || entry.IsDir() {
			return nil
		}
		id, ok := mapIDFromJSONName(entry.Name())
		if !ok {
			return nil
		}
		rel, err := filepath.Rel(currentProject, path)
		if err != nil {
			return nil
		}
		out[id] = filepath.ToSlash(rel)
		return nil
	})
	return out
}

func regionMapIDs(r RegionDefinition) []int {
	files := regionMapFiles(r)
	ids := make([]int, 0, len(files))
	for id := range files {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	return ids
}

func pyQuoted(s string) string { return strconv.Quote(s) }

func writeRuntimeRegionModule() error {
	if currentProject == "" {
		return nil
	}
	var b strings.Builder
	b.WriteString("# Generated automatically by PML Studio 0.5.\n")
	b.WriteString("# Runtime-facing region registry. Do not edit by hand.\n\n")
	b.WriteString(fmt.Sprintf("MULTI_REGION_ENABLED = %t\n", projectRegions.Enabled))
	b.WriteString("ACTIVE_REGION_ID = " + pyQuoted(projectRegions.ActiveRegionID) + "\n\n")
	b.WriteString("REGIONS = {\n")
	mapOwners := map[int]string{}
	mapPaths := map[int]string{}
	for _, r := range projectRegions.Regions {
		regionFiles := regionMapFiles(r)
		ids := make([]int, 0, len(regionFiles))
		for id := range regionFiles {
			ids = append(ids, id)
		}
		sort.Ints(ids)
		b.WriteString("    " + pyQuoted(r.ID) + ": {\n")
		b.WriteString("        \"name\": " + pyQuoted(r.Name) + ",\n")
		b.WriteString("        \"maps_folder\": " + pyQuoted(r.MapsFolder) + ",\n")
		b.WriteString("        \"scripts_folder\": " + pyQuoted(r.ScriptsFolder) + ",\n")
		b.WriteString("        \"data_folder\": " + pyQuoted(r.DataFolder) + ",\n")
		b.WriteString("        \"map_ids\": [")
		for i, id := range ids {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(strconv.Itoa(id))
			if owner, exists := mapOwners[id]; exists && !strings.EqualFold(owner, r.ID) {
				return fmt.Errorf("Map%03d compare in più regioni (%s e %s): gli ID mappa devono essere globalmente univoci", id, owner, r.ID)
			}
			mapOwners[id] = r.ID
		}
		b.WriteString("],\n")
		b.WriteString("        \"map_paths\": {\n")
		for _, id := range ids {
			path := regionFiles[id]
			mapPaths[id] = path
			b.WriteString(fmt.Sprintf("            %d: %s,\n", id, pyQuoted(path)))
		}
		b.WriteString("        },\n")
		b.WriteString("    },\n")
	}
	b.WriteString("}\n\n")
	b.WriteString("MAP_TO_REGION = {\n")
	ids := make([]int, 0, len(mapOwners))
	for id := range mapOwners {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	for _, id := range ids {
		b.WriteString(fmt.Sprintf("    %d: %s,\n", id, pyQuoted(mapOwners[id])))
	}
	b.WriteString("}\n\n")
	b.WriteString("MAP_PATHS = {\n")
	for _, id := range ids {
		if path, ok := mapPaths[id]; ok {
			b.WriteString(fmt.Sprintf("    %d: %s,\n", id, pyQuoted(path)))
		}
	}
	b.WriteString("}\n\n")
	b.WriteString("def get_region(region_id):\n")
	b.WriteString("    return REGIONS.get(region_id)\n\n")
	b.WriteString("def region_for_map(map_id):\n")
	b.WriteString("    return MAP_TO_REGION.get(int(map_id))\n\n")
	b.WriteString("def map_path_for_map(map_id):\n")
	b.WriteString("    return MAP_PATHS.get(int(map_id))\n")
	return os.WriteFile(filepath.Join(currentProject, "plm_regions.py"), []byte(b.String()), 0644)
}

func configuredRegionMapFolders(root string) []string {
	if root == "" {
		return nil
	}
	root = filepath.Clean(root)
	seen := map[string]bool{}
	out := make([]string, 0)
	appendFolder := func(rel string) {
		rel = cleanProjectRelativePath(rel)
		if rel == "" {
			return
		}
		abs := filepath.Clean(filepath.Join(root, filepath.FromSlash(rel)))
		key := strings.ToLower(abs)
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, abs)
	}

	// Preferisci il registro già caricato in memoria per il progetto corrente:
	// può essere stato appena riallineato dalla scansione fisica.
	if currentProject != "" && strings.EqualFold(filepath.Clean(currentProject), root) {
		for _, r := range projectRegions.Regions {
			appendFolder(r.MapsFolder)
		}
	} else {
		p := filepath.Join(root, "plm_regions.json")
		if b, err := os.ReadFile(p); err == nil {
			var doc RegionRegistry
			if json.Unmarshal(b, &doc) == nil {
				for _, r := range doc.Regions {
					appendFolder(r.MapsFolder)
				}
			}
		}
	}

	// La nuova struttura è riconoscibile anche senza registry: ogni directory
	// immediatamente sotto converted/maps/regions è una regione fisica.
	canonicalRoot := filepath.Join(root, "converted", "maps", "regions")
	if entries, err := os.ReadDir(canonicalRoot); err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				abs := filepath.Join(canonicalRoot, entry.Name())
				key := strings.ToLower(filepath.Clean(abs))
				if !seen[key] {
					seen[key] = true
					out = append(out, filepath.Clean(abs))
				}
			}
		}
	}
	return out
}

func regionForMapPath(root, mapPath string) string {
	root = filepath.Clean(root)
	mapPath = filepath.Clean(mapPath)
	if strings.TrimSpace(mapPath) == "" || mapPath == "." {
		return ""
	}

	regions := []RegionDefinition{}
	if currentProject != "" && strings.EqualFold(filepath.Clean(currentProject), root) {
		regions = append(regions, projectRegions.Regions...)
	} else {
		p := filepath.Join(root, "plm_regions.json")
		if b, err := os.ReadFile(p); err == nil {
			var doc RegionRegistry
			if json.Unmarshal(b, &doc) == nil {
				regions = append(regions, doc.Regions...)
			}
		}
	}
	for _, r := range regions {
		base := filepath.Clean(filepath.Join(root, filepath.FromSlash(r.MapsFolder)))
		rel, err := filepath.Rel(base, mapPath)
		if err == nil && (rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))) {
			return r.ID
		}
	}

	// Fallback fisico: converted/maps/regions/<REGIONE>/... identifica già la
	// regione anche se il registry è appena stato creato o deve essere riparato.
	canonicalRoot := filepath.Clean(filepath.Join(root, "converted", "maps", "regions"))
	rel, err := filepath.Rel(canonicalRoot, mapPath)
	if err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		parts := strings.Split(filepath.Clean(rel), string(filepath.Separator))
		if len(parts) > 0 && strings.TrimSpace(parts[0]) != "" {
			folder := parts[0]
			for _, r := range regions {
				if strings.EqualFold(regionFolderComponent(r.Name, r.ID), folder) || strings.EqualFold(filepath.Base(filepath.FromSlash(r.MapsFolder)), folder) {
					return r.ID
				}
			}
			return regionSlug(folder)
		}
	}
	return ""
}

func readProjectManifest(root string) (ProjectManifest, bool) {
	var m ProjectManifest
	b, err := os.ReadFile(filepath.Join(root, "plm_project.json"))
	if err != nil || json.Unmarshal(b, &m) != nil {
		return m, false
	}
	return m, true
}

func configuredProjectMapFolders(root string) []string {
	m, ok := readProjectManifest(root)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(m.MapFolders))
	for _, rel := range m.MapFolders {
		rel = cleanProjectRelativePath(rel)
		if rel != "" {
			out = append(out, filepath.Join(root, filepath.FromSlash(rel)))
		}
	}
	return out
}

func syncProjectMapFolders() error {
	if currentProject == "" {
		return nil
	}
	m, ok := readProjectManifest(currentProject)
	if !ok {
		m = ProjectManifest{
			FormatVersion: 1,
			Name:          filepath.Base(filepath.Clean(currentProject)),
			ProjectType:   "generic-monster-rpg",
			Root:          ".",
			CreatedBy:     "PLM Studio 0.5",
		}
	}
	seen := map[string]bool{}
	merged := make([]string, 0, len(m.MapFolders)+len(projectRegions.Regions))
	for _, rel := range m.MapFolders {
		rel = cleanProjectRelativePath(rel)
		if rel == "" || seen[strings.ToLower(rel)] {
			continue
		}
		seen[strings.ToLower(rel)] = true
		merged = append(merged, rel)
	}
	for _, r := range projectRegions.Regions {
		rel := cleanProjectRelativePath(r.MapsFolder)
		if rel == "" || seen[strings.ToLower(rel)] {
			continue
		}
		seen[strings.ToLower(rel)] = true
		merged = append(merged, rel)
	}
	m.MapFolders = merged
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(currentProject, "plm_project.json"), b, 0644)
}
