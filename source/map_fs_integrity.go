package main

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var fsMapJSONNameRE = regexp.MustCompile(`(?i)^map0*([0-9]+)\.json$`)

type MapDuplicateConflict struct {
	MapID int
	Paths []string
}

type MapDuplicateRepairReport struct {
	RepairedIDs  []int
	RemovedPaths []string
	Conflicts    []MapDuplicateConflict
}

func fsMapIDFromJSONName(name string) (int, bool) {
	m := fsMapJSONNameRE.FindStringSubmatch(filepath.Base(name))
	if len(m) != 2 {
		return 0, false
	}
	id, err := strconv.Atoi(m[1])
	return id, err == nil && id > 0
}

func fsFileSHA256(path string) ([32]byte, error) {
	var zero [32]byte
	f, err := os.Open(path)
	if err != nil {
		return zero, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return zero, err
	}
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out, nil
}

func pathIsWithin(path, root string) bool {
	path = filepath.Clean(path)
	root = filepath.Clean(root)
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." || rel == "" {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

func scanMapCopies(mapsRoot string) (map[int][]string, error) {
	mapsRoot = filepath.Clean(strings.TrimSpace(mapsRoot))
	out := map[int][]string{}
	if mapsRoot == "" || mapsRoot == "." {
		return out, fmt.Errorf("cartella maps non valida")
	}
	if _, err := os.Stat(mapsRoot); err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return out, err
	}
	regionsRoot := filepath.Join(mapsRoot, "regions")
	err := filepath.WalkDir(mapsRoot, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry == nil {
			return nil
		}
		if entry.IsDir() {
			// converted/maps contains map files only at its root plus the
			// canonical regions tree. Other directories (encounters, caches,
			// generated data, etc.) are metadata and must never be interpreted
			// as physical maps merely because they contain MapXXX.json files.
			if !strings.EqualFold(filepath.Clean(path), mapsRoot) &&
				!strings.EqualFold(filepath.Clean(path), regionsRoot) &&
				!pathIsWithin(path, regionsRoot) {
				return filepath.SkipDir
			}
			return nil
		}
		id, ok := fsMapIDFromJSONName(entry.Name())
		if !ok {
			return nil
		}
		parent := filepath.Clean(filepath.Dir(path))
		if !strings.EqualFold(parent, mapsRoot) && !pathIsWithin(path, regionsRoot) {
			return nil
		}
		out[id] = append(out[id], filepath.Clean(path))
		return nil
	})
	if err != nil {
		return nil, err
	}
	for id := range out {
		sort.Slice(out[id], func(i, j int) bool {
			return strings.ToLower(out[id][i]) < strings.ToLower(out[id][j])
		})
	}
	return out, nil
}

func fsCopyVerifiedKeepSource(source, target string) error {
	source = filepath.Clean(source)
	target = filepath.Clean(target)
	if strings.EqualFold(source, target) {
		return nil
	}
	info, err := os.Stat(source)
	if err != nil {
		return fmt.Errorf("file sorgente non trovato: %w", err)
	}
	if info.IsDir() {
		return fmt.Errorf("la sorgente non è un file")
	}
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return fmt.Errorf("creazione cartella destinazione fallita: %w", err)
	}
	sourceHash, err := fsFileSHA256(source)
	if err != nil {
		return fmt.Errorf("verifica sorgente fallita: %w", err)
	}
	if _, err := os.Stat(target); err == nil {
		targetHash, hashErr := fsFileSHA256(target)
		if hashErr == nil && targetHash == sourceHash {
			return nil
		}
		return fmt.Errorf("esiste già un file diverso nella destinazione: %s", target)
	}

	tmp := target + ".plm_copy_tmp"
	_ = os.Remove(tmp)
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		return fmt.Errorf("creazione copia temporanea fallita: %w", err)
	}
	ok := false
	defer func() {
		_ = out.Close()
		if !ok {
			_ = os.Remove(tmp)
		}
	}()
	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copia fallita: %w", err)
	}
	if err := out.Sync(); err != nil {
		return fmt.Errorf("flush copia fallito: %w", err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("chiusura copia fallita: %w", err)
	}
	tmpHash, err := fsFileSHA256(tmp)
	if err != nil || tmpHash != sourceHash {
		if err != nil {
			return fmt.Errorf("verifica copia fallita: %w", err)
		}
		return fmt.Errorf("verifica copia fallita: contenuto diverso")
	}
	if err := os.Rename(tmp, target); err != nil {
		return fmt.Errorf("finalizzazione copia fallita: %w", err)
	}
	ok = true
	return nil
}

// fsMoveVerified performs a real MOVE. If the destination already exists with
// identical bytes, this is treated as an explicit canonical destination and
// the source is removed after hash verification. Different destination bytes
// are always a hard error.
func fsMoveVerified(source, target string) error {
	source = filepath.Clean(source)
	target = filepath.Clean(target)
	if strings.EqualFold(source, target) {
		return nil
	}
	if source == "" || target == "" || source == "." || target == "." {
		return fmt.Errorf("percorso sorgente/destinazione non valido")
	}
	srcInfo, err := os.Stat(source)
	if err != nil {
		return fmt.Errorf("file sorgente non trovato: %w", err)
	}
	if srcInfo.IsDir() {
		return fmt.Errorf("la sorgente non è un file mappa")
	}
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return fmt.Errorf("creazione cartella destinazione fallita: %w", err)
	}
	sourceHash, err := fsFileSHA256(source)
	if err != nil {
		return fmt.Errorf("verifica sorgente fallita: %w", err)
	}

	if _, err := os.Stat(target); err == nil {
		targetHash, hashErr := fsFileSHA256(target)
		if hashErr != nil {
			return fmt.Errorf("verifica destinazione fallita: %w", hashErr)
		}
		if targetHash != sourceHash {
			return fmt.Errorf("esiste già un file diverso nella destinazione: %s", target)
		}
		if err := os.Remove(source); err != nil {
			return fmt.Errorf("duplicato identico verificato ma impossibile rimuovere la sorgente: %w", err)
		}
		if _, err := os.Stat(source); !os.IsNotExist(err) {
			return fmt.Errorf("la sorgente esiste ancora dopo il MOVE")
		}
		return nil
	}

	// Fast path: atomic rename on the same filesystem.
	if err := os.Rename(source, target); err == nil {
		targetHash, hashErr := fsFileSHA256(target)
		if hashErr == nil && targetHash == sourceHash {
			if _, srcErr := os.Stat(source); os.IsNotExist(srcErr) {
				return nil
			}
		}
		_ = os.Rename(target, source)
		if hashErr != nil {
			return fmt.Errorf("spostamento non verificabile: %w", hashErr)
		}
		return fmt.Errorf("spostamento non verificato")
	}

	// Cross-filesystem fallback: copy to temp, fsync, verify, publish, delete.
	tmp := target + ".plm_move_tmp"
	_ = os.Remove(tmp)
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, srcInfo.Mode().Perm())
	if err != nil {
		_ = in.Close()
		return fmt.Errorf("creazione file temporaneo fallita: %w", err)
	}
	copied := false
	defer func() {
		_ = in.Close()
		_ = out.Close()
		if !copied {
			_ = os.Remove(tmp)
		}
	}()
	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copia mappa fallita: %w", err)
	}
	if err := out.Sync(); err != nil {
		return fmt.Errorf("flush mappa fallito: %w", err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("chiusura mappa temporanea fallita: %w", err)
	}
	_ = in.Close()
	tmpHash, err := fsFileSHA256(tmp)
	if err != nil || tmpHash != sourceHash {
		if err != nil {
			return fmt.Errorf("verifica copia fallita: %w", err)
		}
		return fmt.Errorf("verifica copia fallita: contenuto diverso dalla sorgente")
	}
	if err := os.Rename(tmp, target); err != nil {
		return fmt.Errorf("finalizzazione destinazione fallita: %w", err)
	}
	copied = true
	if err := os.Remove(source); err != nil {
		_ = os.Remove(target)
		return fmt.Errorf("impossibile rimuovere la sorgente dopo la copia verificata: %w", err)
	}
	targetHash, hashErr := fsFileSHA256(target)
	if hashErr != nil || targetHash != sourceHash {
		return fmt.Errorf("MOVE completato ma verifica finale destinazione fallita")
	}
	if _, err := os.Stat(source); !os.IsNotExist(err) {
		return fmt.Errorf("MOVE incompleto: la sorgente esiste ancora")
	}
	return nil
}

// chooseMapDuplicateCanonical returns the only safe canonical copy for an
// already-existing set of byte-identical MapXXX.json files. The root copy
// under converted/maps always wins when present: a legacy duplicate in a
// region is not proof that the user intentionally assigned the map there.
//
// If there is no root copy, exactly one regional copy is unambiguous and may
// be kept. Multiple regional copies are deliberately ambiguous and must be
// resolved by the user instead of silently choosing a region.
func chooseMapDuplicateCanonical(mapsRoot string, mapID int, paths []string) (string, bool) {
	mapsRoot = filepath.Clean(strings.TrimSpace(mapsRoot))
	if mapsRoot == "" || mapsRoot == "." || mapID <= 0 || len(paths) == 0 {
		return "", false
	}
	rootCandidate := filepath.Clean(filepath.Join(mapsRoot, fmt.Sprintf("Map%03d.json", mapID)))
	for _, p := range paths {
		if strings.EqualFold(filepath.Clean(p), rootCandidate) {
			return filepath.Clean(p), true
		}
	}

	regionsRoot := filepath.Join(mapsRoot, "regions")
	regional := make([]string, 0, len(paths))
	for _, p := range paths {
		clean := filepath.Clean(p)
		if pathIsWithin(clean, regionsRoot) {
			regional = append(regional, clean)
		}
	}
	if len(regional) == 1 {
		return regional[0], true
	}
	return "", false
}

func repairMapDuplicates(mapsRoot string, explicitCanonical map[int]string) (MapDuplicateRepairReport, error) {
	var report MapDuplicateRepairReport
	copies, err := scanMapCopies(mapsRoot)
	if err != nil {
		return report, err
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
				return report, fmt.Errorf("Map%03d: hash fallito per %s: %w", id, p, err)
			}
			if i > 0 && hashes[i] != hashes[0] {
				identical = false
			}
		}
		if !identical {
			report.Conflicts = append(report.Conflicts, MapDuplicateConflict{MapID: id, Paths: append([]string(nil), paths...)})
			continue
		}

		canonical := ""
		if explicitCanonical != nil {
			if wanted := filepath.Clean(explicitCanonical[id]); wanted != "." && wanted != "" {
				for _, p := range paths {
					if strings.EqualFold(filepath.Clean(p), wanted) {
						canonical = p
						break
					}
				}
			}
		}
		rootCandidate := filepath.Join(mapsRoot, fmt.Sprintf("Map%03d.json", id))
		if canonical == "" {
			for _, p := range paths {
				if strings.EqualFold(filepath.Clean(p), filepath.Clean(rootCandidate)) {
					canonical = p
					break
				}
			}
		}
		// No explicit move and no root copy: do not invent a region owner.
		// Move one identical copy to the root so the map becomes unassigned.
		if canonical == "" {
			source := paths[0]
			if err := fsMoveVerified(source, rootCandidate); err != nil {
				return report, fmt.Errorf("Map%03d: impossibile rendere canonica la copia root: %w", id, err)
			}
			canonical = rootCandidate
			// One of paths no longer exists after MOVE; the remaining copies
			// are still identical and will be deleted below.
		}

		canonicalHash, err := fsFileSHA256(canonical)
		if err != nil {
			return report, fmt.Errorf("Map%03d: verifica copia canonica fallita: %w", id, err)
		}
		for _, p := range paths {
			if strings.EqualFold(filepath.Clean(p), filepath.Clean(canonical)) {
				continue
			}
			if _, statErr := os.Stat(p); os.IsNotExist(statErr) {
				continue
			}
			h, hashErr := fsFileSHA256(p)
			if hashErr != nil || h != canonicalHash {
				report.Conflicts = append(report.Conflicts, MapDuplicateConflict{MapID: id, Paths: append([]string(nil), paths...)})
				canonical = ""
				break
			}
			if err := os.Remove(p); err != nil {
				return report, fmt.Errorf("Map%03d: impossibile eliminare duplicato identico %s: %w", id, p, err)
			}
			report.RemovedPaths = append(report.RemovedPaths, p)
		}
		if canonical != "" {
			report.RepairedIDs = append(report.RepairedIDs, id)
		}
	}
	return report, nil
}

func uniqueMapPathIndex(mapsRoot string) (map[int]string, []MapDuplicateConflict, error) {
	copies, err := scanMapCopies(mapsRoot)
	if err != nil {
		return nil, nil, err
	}
	index := make(map[int]string, len(copies))
	var conflicts []MapDuplicateConflict
	ids := make([]int, 0, len(copies))
	for id := range copies {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	for _, id := range ids {
		paths := copies[id]
		if len(paths) != 1 {
			conflicts = append(conflicts, MapDuplicateConflict{MapID: id, Paths: append([]string(nil), paths...)})
			continue
		}
		index[id] = paths[0]
	}
	return index, conflicts, nil
}

func resolveMapPathByID(mapsRoot string, mapID int) (string, error) {
	if mapID <= 0 {
		return "", fmt.Errorf("Map ID non valido")
	}
	index, conflicts, err := uniqueMapPathIndex(mapsRoot)
	if err != nil {
		return "", err
	}
	for _, c := range conflicts {
		if c.MapID == mapID {
			return "", fmt.Errorf("Map%03d ha %d copie fisiche; risoluzione ambigua", mapID, len(c.Paths))
		}
	}
	path := index[mapID]
	if path == "" {
		return "", fmt.Errorf("Map%03d non trovata", mapID)
	}
	return path, nil
}
