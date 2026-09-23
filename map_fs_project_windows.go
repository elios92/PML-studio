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
)

type projectMapIndexDocument struct {
	Version  int               `json:"version"`
	MapPaths map[string]string `json:"map_paths"`
}

// writeProjectMapIndex persists the single-source-of-truth map resolver used by
// the Python playtest bootstrap. Every Map ID must resolve to exactly one
// physical MapXXX.json before this file is written.
func writeProjectMapIndex() error {
	if currentProject == "" {
		return fmt.Errorf("nessun progetto aperto")
	}
	mapsRoot := canonicalConvertedMapsPath()
	index, conflicts, err := uniqueMapPathIndex(mapsRoot)
	if err != nil {
		return err
	}
	if len(conflicts) > 0 {
		return fmt.Errorf("impossibile generare l'indice mappe: sono presenti Map ID duplicati")
	}

	doc := projectMapIndexDocument{
		Version:  1,
		MapPaths: make(map[string]string, len(index)),
	}
	ids := make([]int, 0, len(index))
	for id := range index {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	for _, id := range ids {
		path := filepath.Clean(index[id])
		rel, relErr := filepath.Rel(currentProject, path)
		if relErr != nil || rel == "." || rel == "" || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return fmt.Errorf("Map%03d: percorso fuori dal progetto: %s", id, path)
		}
		doc.MapPaths[strconv.Itoa(id)] = filepath.ToSlash(rel)
	}

	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	dir := filepath.Join(currentProject, ".plm")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	path := filepath.Join(dir, "map_index.json")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0644); err != nil {
		return err
	}
	if exists(path) {
		_ = os.Remove(path)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func projectMapDuplicateList() ([]MapDuplicateConflict, error) {
	if currentProject == "" {
		return nil, fmt.Errorf("nessun progetto aperto")
	}
	copies, err := scanMapCopies(canonicalConvertedMapsPath())
	if err != nil {
		return nil, err
	}
	ids := make([]int, 0, len(copies))
	for id := range copies {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	var out []MapDuplicateConflict
	for _, id := range ids {
		paths := copies[id]
		if len(paths) > 1 {
			out = append(out, MapDuplicateConflict{MapID: id, Paths: append([]string(nil), paths...)})
		}
	}
	return out, nil
}

func knownMapDuplicateConflict() bool {
	conflicts, err := projectMapDuplicateList()
	return err != nil || len(conflicts) > 0
}

// ensureProjectMapFilesystemSafe is deliberately non-destructive. Save and
// playtest operations must never decide which duplicate file wins. Automatic
// cleanup is done only while opening a project or from the explicit repair
// command, and only for byte-identical copies.
func ensureProjectMapFilesystemSafe(context string) error {
	context = strings.TrimSpace(context)
	if context == "" {
		context = "Operazione"
	}
	conflicts, err := projectMapDuplicateList()
	if err != nil {
		return fmt.Errorf("%s bloccato: controllo filesystem mappe fallito: %w", context, err)
	}
	if len(conflicts) > 0 {
		return fmt.Errorf("%s bloccato.\r\n\r\n%s", context, duplicateConflictMessage(conflicts))
	}
	if err := writeProjectMapIndex(); err != nil {
		return fmt.Errorf("%s bloccato: indice mappe non aggiornabile: %w", context, err)
	}
	return nil
}

func duplicateConflictMessage(conflicts []MapDuplicateConflict) string {
	if len(conflicts) == 0 {
		return "Nessun Map ID duplicato rilevato."
	}
	var b strings.Builder
	b.WriteString("Sono presenti più file fisici con lo stesso Map ID. PML Studio non può salvare o avviare il Playtest finché ogni Map ID non corrisponde a un solo MapXXX.json.\r\n\r\n")
	limit := len(conflicts)
	if limit > 8 {
		limit = 8
	}
	for i := 0; i < limit; i++ {
		c := conflicts[i]
		b.WriteString(fmt.Sprintf("Map%03d:\r\n", c.MapID))
		pathLimit := len(c.Paths)
		if pathLimit > 4 {
			pathLimit = 4
		}
		for j := 0; j < pathLimit; j++ {
			p := c.Paths[j]
			if currentProject != "" {
				if rel, err := filepath.Rel(currentProject, p); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
					p = filepath.ToSlash(rel)
				}
			}
			b.WriteString("  - " + p + "\r\n")
		}
		if len(c.Paths) > pathLimit {
			b.WriteString(fmt.Sprintf("  - ... altre %d copie\r\n", len(c.Paths)-pathLimit))
		}
	}
	if len(conflicts) > limit {
		b.WriteString(fmt.Sprintf("\r\n... altri %d Map ID duplicati.\r\n", len(conflicts)-limit))
	}
	b.WriteString("\r\nUsa Strumenti > Ripara Map ID duplicati. Le copie byte-per-byte identiche possono essere consolidate automaticamente; file con contenuti diversi non vengono mai cancellati.")
	return b.String()
}

func chooseProjectDuplicateCanonical(mapID int, paths []string) (string, bool) {
	return chooseMapDuplicateCanonical(canonicalConvertedMapsPath(), mapID, paths)
}

// repairProjectDuplicateMapIDs only deletes verified, byte-identical duplicate
// names. Divergent files and ambiguous multi-region ownership are returned as
// conflicts and left untouched.
func repairProjectDuplicateMapIDs(ask bool) (MapDuplicateRepairReport, error) {
	var report MapDuplicateRepairReport
	if currentProject == "" {
		return report, fmt.Errorf("nessun progetto aperto")
	}
	mapsRoot := canonicalConvertedMapsPath()
	copies, err := scanMapCopies(mapsRoot)
	if err != nil {
		return report, err
	}

	duplicateCount := 0
	for _, paths := range copies {
		if len(paths) > 1 {
			duplicateCount++
		}
	}
	if duplicateCount == 0 {
		_ = writeProjectMapIndex()
		return report, nil
	}
	if ask {
		answer := msgboxResult(
			"PML Studio - Ripara Map ID",
			fmt.Sprintf("Trovati %d Map ID con più copie fisiche.\r\n\r\nPML Studio eliminerà SOLO copie con contenuto identico verificato tramite SHA-256. I file diversi o assegnati ambiguamente a più regioni resteranno intatti.\r\n\r\nContinuare?", duplicateCount),
			MB_YESNOCANCEL|MB_ICONINFORMATION,
		)
		if answer != IDYES {
			return report, fmt.Errorf("operazione annullata")
		}
	}

	ids := make([]int, 0, len(copies))
	for id := range copies {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	for _, id := range ids {
		paths := copies[id]
		if len(paths) <= 1 {
			continue
		}
		hashes := make([][32]byte, len(paths))
		identical := true
		for i, p := range paths {
			hashes[i], err = fsFileSHA256(p)
			if err != nil {
				return report, fmt.Errorf("Map%03d: verifica SHA-256 fallita per %s: %w", id, p, err)
			}
			if i > 0 && hashes[i] != hashes[0] {
				identical = false
			}
		}
		if !identical {
			report.Conflicts = append(report.Conflicts, MapDuplicateConflict{MapID: id, Paths: append([]string(nil), paths...)})
			continue
		}
		canonical, ok := chooseProjectDuplicateCanonical(id, paths)
		if !ok || canonical == "" {
			report.Conflicts = append(report.Conflicts, MapDuplicateConflict{MapID: id, Paths: append([]string(nil), paths...)})
			continue
		}
		canonicalHash, hashErr := fsFileSHA256(canonical)
		if hashErr != nil {
			return report, fmt.Errorf("Map%03d: verifica copia canonica fallita: %w", id, hashErr)
		}
		removed := 0
		for _, p := range paths {
			p = filepath.Clean(p)
			if strings.EqualFold(p, canonical) {
				continue
			}
			if !pathIsWithin(p, mapsRoot) && !strings.EqualFold(filepath.Dir(p), mapsRoot) {
				return report, fmt.Errorf("Map%03d: rifiutata eliminazione fuori da converted/maps: %s", id, p)
			}
			h, hashErr := fsFileSHA256(p)
			if hashErr != nil || h != canonicalHash {
				report.Conflicts = append(report.Conflicts, MapDuplicateConflict{MapID: id, Paths: append([]string(nil), paths...)})
				removed = -1
				break
			}
			if err := os.Remove(p); err != nil {
				return report, fmt.Errorf("Map%03d: impossibile eliminare duplicato identico %s: %w", id, p, err)
			}
			report.RemovedPaths = append(report.RemovedPaths, p)
			removed++
		}
		if removed > 0 {
			report.RepairedIDs = append(report.RepairedIDs, id)
		}
	}

	rebuildMapFileIndex(currentProject)
	if len(report.Conflicts) == 0 {
		if err := writeRuntimeRegionModule(); err != nil {
			return report, err
		}
		if err := writeProjectMapIndex(); err != nil {
			return report, err
		}
	}
	return report, nil
}

func showDuplicateRepairAndRefresh() {
	if currentProject == "" {
		msgbox("PML Studio", "Apri prima un progetto.", MB_OK|MB_ICONINFORMATION)
		return
	}
	if mapDirty {
		msgbox("PML Studio - Ripara Map ID", "La mappa corrente contiene modifiche non salvate. Completa o annulla le modifiche prima di riparare i duplicati, così nessun file aperto viene sostituito durante l'operazione.", MB_OK|MB_ICONINFORMATION)
		return
	}
	preserveID := 0
	if currentMap != nil {
		preserveID = currentMap.ID
	}
	report, err := repairProjectDuplicateMapIDs(true)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "annullata") {
			return
		}
		msgbox("PML Studio - Ripara Map ID", "Riparazione non completata:\r\n\r\n"+err.Error(), MB_OK|MB_ICONERROR)
		return
	}
	reloadMapsAfterFilesystemChange(preserveID)
	if len(report.Conflicts) > 0 {
		msgbox("PML Studio - Conflitti rimasti", duplicateConflictMessage(report.Conflicts), MB_OK|MB_ICONERROR)
		setToolbarStatus(fmt.Sprintf("Map ID: %d riparati, %d conflitti da risolvere", len(report.RepairedIDs), len(report.Conflicts)))
		return
	}
	msgbox("PML Studio - Ripara Map ID", fmt.Sprintf("Controllo completato.\r\n\r\nMap ID consolidati: %d\r\nCopie identiche eliminate: %d\r\nConflitti: 0", len(report.RepairedIDs), len(report.RemovedPaths)), MB_OK|MB_ICONINFORMATION)
	setToolbarStatus(fmt.Sprintf("Map ID verificati: %d riparati, nessun conflitto", len(report.RepairedIDs)))
}

type singleSourceMapMove struct {
	MapID       int
	MapIndex    int
	OldPath     string
	TargetPath  string
	OldRegionID string
	BackupOld   string
	BackupNew   string
	Moved       bool
	BackupMoved bool
}

func rollbackSingleSourceMoves(plan []singleSourceMapMove) {
	for i := len(plan) - 1; i >= 0; i-- {
		p := &plan[i]
		if p.BackupMoved && exists(p.BackupNew) && !exists(p.BackupOld) {
			_ = fsMoveVerified(p.BackupNew, p.BackupOld)
		}
		if p.Moved && exists(p.TargetPath) && !exists(p.OldPath) {
			_ = fsMoveVerified(p.TargetPath, p.OldPath)
		}
	}
}

// moveMapBranchToRegionSingleSource is the current region move implementation.
// A map is MOVED, never copied: one Map ID -> one physical MapXXX.json.
func moveMapBranchToRegionSingleSource(rootIndex int, descendantIndices []int, r RegionDefinition, targetDir string) error {
	if currentProject == "" {
		return fmt.Errorf("nessun progetto aperto")
	}
	if err := ensureProjectMapFilesystemSafe("Spostamento mappa"); err != nil {
		return err
	}
	mapsRoot := canonicalConvertedMapsPath()
	regionBase := filepath.Join(currentProject, filepath.FromSlash(r.MapsFolder))
	targetDir = filepath.Clean(targetDir)
	if !pathWithin(regionBase, targetDir) {
		return fmt.Errorf("destinazione fuori dalla regione %s", r.Name)
	}
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return fmt.Errorf("impossibile creare la cartella di destinazione: %w", err)
	}

	indices := make([]int, 0, 1+len(descendantIndices))
	indices = append(indices, rootIndex)
	indices = append(indices, descendantIndices...)
	plan := make([]singleSourceMapMove, 0, len(indices))
	seenTargets := map[string]int{}
	for _, idx := range indices {
		if idx < 0 || idx >= len(maps) {
			continue
		}
		m := maps[idx]
		source := filepath.Clean(m.File)
		if idx == rootIndex && currentMapDoc != nil && strings.TrimSpace(currentMapDoc.Path) != "" {
			source = filepath.Clean(currentMapDoc.Path)
		}
		if source == "" || !exists(source) {
			return fmt.Errorf("Map%03d (%s): file reale non trovato", m.ID, m.Name)
		}
		if !pathIsWithin(source, mapsRoot) && !strings.EqualFold(filepath.Dir(source), mapsRoot) {
			return fmt.Errorf("Map%03d: sorgente fuori da converted/maps: %s", m.ID, source)
		}
		target := filepath.Join(targetDir, fmt.Sprintf("Map%03d.json", m.ID))
		key := strings.ToLower(filepath.Clean(target))
		if previous, exists := seenTargets[key]; exists && previous != m.ID {
			return fmt.Errorf("conflitto destinazione tra Map%03d e Map%03d", previous, m.ID)
		}
		seenTargets[key] = m.ID
		if !strings.EqualFold(source, target) && exists(target) {
			return fmt.Errorf("Map%03d: esiste già un file nella destinazione: %s. Usa prima Ripara Map ID duplicati.", m.ID, target)
		}
		backupOld := mapBackupPath(source)
		backupNew := mapBackupPath(target)
		if !strings.EqualFold(backupOld, backupNew) && exists(backupOld) && exists(backupNew) {
			return fmt.Errorf("Map%03d: esiste già anche il backup di destinazione %s", m.ID, backupNew)
		}
		plan = append(plan, singleSourceMapMove{
			MapID:       m.ID,
			MapIndex:    idx,
			OldPath:     source,
			TargetPath:  target,
			OldRegionID: m.RegionID,
			BackupOld:   backupOld,
			BackupNew:   backupNew,
		})
	}
	if len(plan) == 0 {
		return fmt.Errorf("nessuna mappa da spostare")
	}

	for i := range plan {
		p := &plan[i]
		if !strings.EqualFold(p.OldPath, p.TargetPath) {
			if err := fsMoveVerified(p.OldPath, p.TargetPath); err != nil {
				rollbackSingleSourceMoves(plan[:i])
				return fmt.Errorf("Map%03d: MOVE fallito: %w", p.MapID, err)
			}
			p.Moved = true
		}
		if !strings.EqualFold(p.BackupOld, p.BackupNew) && exists(p.BackupOld) {
			if err := fsMoveVerified(p.BackupOld, p.BackupNew); err != nil {
				rollbackSingleSourceMoves(plan[:i+1])
				return fmt.Errorf("Map%03d: MOVE backup fallito: %w", p.MapID, err)
			}
			p.BackupMoved = true
		}
	}

	oldActive := projectRegions.ActiveRegionID
	projectRegions.ActiveRegionID = r.ID
	rebuildMapFileIndex(currentProject)
	if err := saveRegionRegistry(); err != nil {
		projectRegions.ActiveRegionID = oldActive
		rollbackSingleSourceMoves(plan)
		rebuildMapFileIndex(currentProject)
		_ = saveRegionRegistry()
		_ = writeProjectMapIndex()
		return fmt.Errorf("aggiornamento registro regioni fallito; MOVE annullato: %w", err)
	}
	if err := writeProjectMapIndex(); err != nil {
		projectRegions.ActiveRegionID = oldActive
		rollbackSingleSourceMoves(plan)
		rebuildMapFileIndex(currentProject)
		_ = saveRegionRegistry()
		_ = writeProjectMapIndex()
		return fmt.Errorf("aggiornamento indice mappe fallito; MOVE annullato: %w", err)
	}

	for i := range plan {
		p := &plan[i]
		if p.MapIndex >= 0 && p.MapIndex < len(maps) {
			maps[p.MapIndex].File = p.TargetPath
			maps[p.MapIndex].RegionID = r.ID
		}
		if currentMap != nil && currentMap.ID == p.MapID && currentMapDoc != nil {
			currentMapDoc.Path = p.TargetPath
		}
	}
	return nil
}

func unassignCurrentMapToRoot() error {
	if currentProject == "" {
		return fmt.Errorf("nessun progetto aperto")
	}
	if currentMap == nil || currentMapDoc == nil {
		return fmt.Errorf("seleziona prima una mappa")
	}
	if err := ensureProjectMapFilesystemSafe("Spostamento tra le NON ASSEGNATE"); err != nil {
		return err
	}
	if !saveCurrentMap() {
		return fmt.Errorf("la mappa corrente non è stata salvata")
	}
	mapID := currentMap.ID
	source := filepath.Clean(currentMapDoc.Path)
	target := filepath.Clean(canonicalFlatMapPath(mapID))
	if target == "" {
		return fmt.Errorf("percorso NON ASSEGNATE non valido")
	}
	if strings.EqualFold(source, target) {
		currentMap.RegionID = ""
		setToolbarStatus(fmt.Sprintf("Map%03d è già tra le NON ASSEGNATE", mapID))
		return nil
	}
	if exists(target) {
		return fmt.Errorf("esiste già %s. Usa prima Ripara Map ID duplicati", target)
	}
	backupOld := mapBackupPath(source)
	backupNew := mapBackupPath(target)
	if exists(backupOld) && exists(backupNew) {
		return fmt.Errorf("esiste già il backup di destinazione %s", backupNew)
	}
	if err := fsMoveVerified(source, target); err != nil {
		return fmt.Errorf("MOVE mappa fallito: %w", err)
	}
	backupMoved := false
	if exists(backupOld) {
		if err := fsMoveVerified(backupOld, backupNew); err != nil {
			_ = fsMoveVerified(target, source)
			return fmt.Errorf("MOVE backup fallito; mappa ripristinata: %w", err)
		}
		backupMoved = true
	}

	rebuildMapFileIndex(currentProject)
	if err := writeRuntimeRegionModule(); err != nil {
		if backupMoved {
			_ = fsMoveVerified(backupNew, backupOld)
		}
		_ = fsMoveVerified(target, source)
		rebuildMapFileIndex(currentProject)
		_ = writeRuntimeRegionModule()
		return fmt.Errorf("aggiornamento registro runtime fallito; MOVE annullato: %w", err)
	}
	if err := writeProjectMapIndex(); err != nil {
		if backupMoved {
			_ = fsMoveVerified(backupNew, backupOld)
		}
		_ = fsMoveVerified(target, source)
		rebuildMapFileIndex(currentProject)
		_ = writeRuntimeRegionModule()
		_ = writeProjectMapIndex()
		return fmt.Errorf("aggiornamento indice mappe fallito; MOVE annullato: %w", err)
	}

	for i := range maps {
		if maps[i].ID == mapID {
			maps[i].File = target
			maps[i].RegionID = ""
			currentMap = &maps[i]
			break
		}
	}
	currentMapDoc.Path = target
	mapRegionFilter = mapRegionFilterAll
	reloadMapsAfterFilesystemChange(mapID)
	loadHeaderFromCurrentMap()
	updateMapQuickInfo()
	refreshRegionFolderControls()
	setToolbarStatus(fmt.Sprintf("Map%03d spostata tra le NON ASSEGNATE", mapID))
	return nil
}
