//go:build windows

package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type mapImportSummary struct {
	Found      int
	Imported   int
	Moved      int
	Skipped    int
	FirstMapID int
}

func samePath(a, b string) bool {
	if strings.TrimSpace(a) == "" || strings.TrimSpace(b) == "" {
		return false
	}
	aa, errA := filepath.Abs(filepath.Clean(a))
	bb, errB := filepath.Abs(filepath.Clean(b))
	if errA != nil || errB != nil {
		return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
	}
	return strings.EqualFold(aa, bb)
}

func collectMapJSONFiles(root string) []string {
	var out []string
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry == nil || entry.IsDir() {
			return nil
		}
		if _, ok := mapIDFromJSONName(entry.Name()); ok {
			out = append(out, filepath.Clean(path))
		}
		return nil
	})
	sort.SliceStable(out, func(i, j int) bool {
		idi, _ := mapIDFromJSONName(filepath.Base(out[i]))
		idj, _ := mapIDFromJSONName(filepath.Base(out[j]))
		if idi != idj {
			return idi < idj
		}
		return strings.ToLower(out[i]) < strings.ToLower(out[j])
	})
	return out
}

func copyFileExact(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		_ = out.Close()
		if !ok {
			_ = os.Remove(dst)
		}
	}()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	if err := out.Sync(); err != nil {
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	ok = true
	return nil
}

func moveFileWithFallback(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	if err := copyFileExact(src, dst); err != nil {
		return err
	}
	return os.Remove(src)
}

func importMapsFromFolder(source, target string, moveInsideProject bool) (mapImportSummary, error) {
	var summary mapImportSummary
	if currentProject == "" {
		return summary, fmt.Errorf("nessun progetto aperto")
	}
	source = filepath.Clean(source)
	target = filepath.Clean(target)
	if !isDir(source) {
		return summary, fmt.Errorf("cartella sorgente non valida")
	}
	if !pathInsideProject(target) {
		return summary, fmt.Errorf("destinazione fuori dal progetto")
	}
	if samePath(source, target) {
		return summary, fmt.Errorf("sorgente e destinazione coincidono")
	}
	// Prevent recursive self-import when the region Maps folder is nested in
	// the selected source directory.
	if pathWithinRoot(source, target) {
		return summary, fmt.Errorf("la cartella sorgente contiene la destinazione: seleziona una cartella mappe più specifica")
	}

	files := collectMapJSONFiles(source)
	summary.Found = len(files)
	if len(files) == 0 {
		return summary, fmt.Errorf("nessun file MapXXX.json trovato nella cartella selezionata")
	}

	if currentMap != nil && currentMap.File != "" && pathWithinRoot(source, currentMap.File) {
		if !confirmMapChanges() {
			return summary, fmt.Errorf("operazione annullata")
		}
	}

	index := rebuildMapFileIndex(currentProject)
	currentMapMovedID := 0
	if currentMap != nil && pathWithinRoot(source, currentMap.File) && moveInsideProject {
		currentMapMovedID = currentMap.ID
	}

	for _, src := range files {
		id, ok := mapIDFromJSONName(filepath.Base(src))
		if !ok {
			summary.Skipped++
			continue
		}
		existing := index[id]
		if existing != "" && !samePath(existing, src) {
			summary.Skipped++
			continue
		}

		rel, err := filepath.Rel(source, src)
		if err != nil || rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
			summary.Skipped++
			continue
		}
		dst := filepath.Join(target, rel)
		if samePath(src, dst) {
			summary.Skipped++
			continue
		}
		if exists(dst) {
			summary.Skipped++
			continue
		}

		if moveInsideProject {
			if err := moveFileWithFallback(src, dst); err != nil {
				return summary, fmt.Errorf("spostamento %s fallito: %w", filepath.Base(src), err)
			}
			// Keep the one-time backup beside the physical map when present.
			if exists(src+".bak") && !exists(dst+".bak") {
				_ = moveFileWithFallback(src+".bak", dst+".bak")
			}
			summary.Moved++
		} else {
			if err := copyFileExact(src, dst); err != nil {
				return summary, fmt.Errorf("importazione %s fallita: %w", filepath.Base(src), err)
			}
			if exists(src+".bak") && !exists(dst+".bak") {
				_ = copyFileExact(src+".bak", dst+".bak")
			}
			summary.Imported++
		}
		if summary.FirstMapID == 0 {
			summary.FirstMapID = id
		}
	}

	rebuildMapFileIndex(currentProject)
	maps = loadMaps(currentProject)
	refreshMapEditorRegionControls()
	_ = writeRuntimeRegionModule()

	if currentMapMovedID > 0 {
		// Same logical map, new physical path: retaining the loaded document is
		// correct because its contents still describe that exact Map ID.
		reloadMapsAfterFilesystemChange(currentMapMovedID)
	} else if summary.FirstMapID > 0 {
		selectMapByID(summary.FirstMapID)
	} else {
		refreshMapListUI()
	}
	return summary, nil
}

func importMapsIntoRegion(regionID string) {
	r := regionByID(regionID)
	if r == nil {
		msgbox("PLM Studio", "Regione non trovata.", MB_OK|MB_ICONERROR)
		return
	}
	target := regionMapsRoot(r)
	if target == "" {
		msgbox("PLM Studio", "Cartella Maps della regione non disponibile.", MB_OK|MB_ICONERROR)
		return
	}
	source := browseFolder("Seleziona la cartella contenente le mappe da importare")
	if strings.TrimSpace(source) == "" {
		return
	}

	moveInside := pathInsideProject(source)
	if moveInside {
		result := msgboxResult(
			"PLM Studio - Importa mappe",
			"La cartella selezionata è già dentro questo progetto.\r\n\r\nLe MapXXX.json verranno SPOSTATE nella regione "+r.Name+" mantenendo gli stessi Map ID e la struttura delle sottocartelle.\r\n\r\nContinuare?",
			MB_YESNOCANCEL|MB_ICONINFORMATION,
		)
		if result != IDYES {
			return
		}
	}

	summary, err := importMapsFromFolder(source, target, moveInside)
	if err != nil {
		if err.Error() != "operazione annullata" {
			msgbox("PLM Studio", "Importazione mappe non riuscita:\r\n\r\n"+err.Error(), MB_OK|MB_ICONERROR)
		}
		return
	}

	action := "importate"
	count := summary.Imported
	if moveInside {
		action = "spostate"
		count = summary.Moved
	}
	msgbox(
		"PLM Studio - Importa mappe",
		fmt.Sprintf("Operazione completata.\r\n\r\nMappe trovate: %d\r\nMappe %s: %d\r\nMappe ignorate per ID duplicato/destinazione esistente: %d", summary.Found, action, count, summary.Skipped),
		MB_OK|MB_ICONINFORMATION,
	)
}

func promptPopulateRegion(regionID string) {
	r := regionByID(regionID)
	if r == nil {
		return
	}
	result := msgboxResult(
		"PML Studio - Regione creata",
		"La regione \""+r.Name+"\" è pronta.\r\n\r\nSì = Importa/sposta mappe esistenti da una cartella\r\nNo = Crea una nuova mappa vuota nella regione\r\nAnnulla = Lascia la regione vuota per ora",
		MB_YESNOCANCEL|MB_ICONINFORMATION,
	)
	switch result {
	case IDYES:
		importMapsIntoRegion(r.ID)
	case IDNO:
		showCreateMapDialog(regionMapsRoot(r))
	}
}
