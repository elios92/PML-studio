//go:build windows

package main

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// PaletteDescriptor represents one numbered PNG palette available to the
// project. The PNG itself is the source of truth; the JSON index is only a
// fast catalog for tools/runtime integrations.
type PaletteDescriptor struct {
	ID         int           `json:"id"`
	File       string        `json:"file"`
	Width      int           `json:"width"`
	Height     int           `json:"height"`
	ColorCount int           `json:"color_count"`
	Colors     []string      `json:"colors"`
	SHA256     string        `json:"sha256"`
	Surface    *PixelSurface `json:"-"`
}

type paletteCatalogFile struct {
	Version  int                 `json:"version"`
	Schema   string              `json:"schema"`
	Palettes []PaletteDescriptor `json:"palettes"`
}

var (
	paletteCatalogProject string
	paletteCatalogByID    map[int]*PaletteDescriptor
)

// Regola PML Studio: il file deve chiamarsi esclusivamente
// "pallette <numero>.png". La parola e il numero devono essere separati
// da almeno uno spazio. Esempi validi: pallette 1.png, pallette 25.png.
// Qualsiasi altro PNG viene ignorato/rifiutato.
var palettePNGNameRE = regexp.MustCompile(`(?i)^pallette +([1-9][0-9]*)\.png$`)

func paletteIDFromPNGName(name string) (int, bool) {
	m := palettePNGNameRE.FindStringSubmatch(filepath.Base(strings.TrimSpace(name)))
	if len(m) != 2 {
		return 0, false
	}
	id, err := strconv.Atoi(m[1])
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

func canonicalPaletteDir(project string) string {
	// Regola ufficiale PML Studio: le palette PNG appartengono alla cartella
	// Tilesets del progetto, insieme agli asset grafici a cui si applicano.
	return filepath.Join(filepath.Clean(project), "assets", "Graphics", "Tilesets")
}

func discoverPaletteDirs(project string) []string {
	// Il filesystem e' la fonte di verita': carichiamo palette solamente dalla
	// destinazione ufficiale richiesta dal progetto. File omonimi presenti in
	// vecchie cartelle Palettes non vengono usati automaticamente.
	dir := canonicalPaletteDir(project)
	if !isDir(dir) {
		return nil
	}
	return []string{dir}
}

func paletteIndexPath(project string) string {
	return filepath.Join(project, "converted", "data", "palettes", "index.json")
}

func paletteRelativePath(project, path string) string {
	rel, err := filepath.Rel(project, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}

func paletteHashString(path string) string {
	h, err := fsFileSHA256(path)
	if err != nil {
		return ""
	}
	return hex.EncodeToString(h[:])
}

func paletteColorsFromSurface(surface *PixelSurface) []string {
	if surface == nil || surface.Width <= 0 || surface.Height <= 0 {
		return nil
	}
	seen := make(map[uint64]bool)
	colors := make([]string, 0, 32)
	for i, rgb := range surface.Pixels {
		a := byte(255)
		if i < len(surface.Alpha) {
			a = surface.Alpha[i]
		}
		key := uint64(rgb)<<8 | uint64(a)
		if seen[key] {
			continue
		}
		seen[key] = true
		colors = append(colors, fmt.Sprintf("#%06X%02X", rgb&0xFFFFFF, a))
	}
	return colors
}

func writePaletteIndex(project string, entries []*PaletteDescriptor) error {
	out := paletteCatalogFile{Version: 2, Schema: "pml.palette_png.v2"}
	for _, p := range entries {
		if p == nil {
			continue
		}
		copy := *p
		copy.Surface = nil
		out.Palettes = append(out.Palettes, copy)
	}
	sort.Slice(out.Palettes, func(i, j int) bool { return out.Palettes[i].ID < out.Palettes[j].ID })
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	path := paletteIndexPath(project)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func rebuildPaletteCatalog(project string) error {
	project = filepath.Clean(project)
	byID := make(map[int]*PaletteDescriptor)
	duplicatePaths := map[int][]string{}

	dirs := discoverPaletteDirs(project)
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		sort.Slice(entries, func(i, j int) bool { return strings.ToLower(entries[i].Name()) < strings.ToLower(entries[j].Name()) })
		for _, e := range entries {
			if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".png") {
				continue
			}
			id, ok := paletteIDFromPNGName(e.Name())
			if !ok {
				// PNGs without a palette number are intentionally ignored.
				continue
			}
			path := filepath.Join(dir, e.Name())
			surface, err := loadPNGSurface(path)
			if err != nil {
				mapLogf("[PALETTE] Palette %d non leggibile: %s (%v)", id, path, err)
				continue
			}
			colors := paletteColorsFromSurface(surface)
			desc := &PaletteDescriptor{
				ID:         id,
				File:       paletteRelativePath(project, path),
				Width:      surface.Width,
				Height:     surface.Height,
				ColorCount: len(colors),
				Colors:     colors,
				SHA256:     paletteHashString(path),
				Surface:    surface,
			}
			if old := byID[id]; old != nil {
				duplicatePaths[id] = append(duplicatePaths[id], old.File, desc.File)
				// Deterministic choice: the canonical Palettes directory wins;
				// otherwise keep the first discovered image. We never delete
				// duplicate palette graphics automatically.
				canonical := strings.ToLower(filepath.Clean(canonicalPaletteDir(project)))
				thisDir := strings.ToLower(filepath.Clean(filepath.Dir(path)))
				oldAbs := filepath.Join(project, filepath.FromSlash(old.File))
				oldDir := strings.ToLower(filepath.Clean(filepath.Dir(oldAbs)))
				if thisDir == canonical && oldDir != canonical {
					byID[id] = desc
				}
				continue
			}
			byID[id] = desc
		}
	}

	ids := make([]int, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	ordered := make([]*PaletteDescriptor, 0, len(ids))
	for _, id := range ids {
		ordered = append(ordered, byID[id])
	}

	paletteCatalogProject = project
	paletteCatalogByID = byID
	if err := writePaletteIndex(project, ordered); err != nil {
		return err
	}
	if len(duplicatePaths) > 0 {
		mapLogf("[PALETTE] trovati ID palette duplicati: %d; nessun file eliminato", len(duplicatePaths))
	}
	mapLogf("[PALETTE] caricate %d palette numerate", len(byID))
	return nil
}

func ensurePaletteCatalog(project string) {
	project = filepath.Clean(project)
	if paletteCatalogByID == nil || !strings.EqualFold(paletteCatalogProject, project) {
		_ = rebuildPaletteCatalog(project)
	}
}

func paletteByID(id int) *PaletteDescriptor {
	if currentProject == "" {
		return nil
	}
	ensurePaletteCatalog(currentProject)
	return paletteCatalogByID[id]
}

func importNumberedPalettePNG() {
	if currentProject == "" {
		msgbox("PML Studio", "Apri prima un progetto.", MB_OK|MB_ICONINFORMATION)
		return
	}
	src := browsePalettePNG("Inserisci palette PNG")
	if src == "" {
		return
	}
	id, ok := paletteIDFromPNGName(filepath.Base(src))
	if !ok {
		msgbox("PML Studio - Inserisci palette",
			"Nome file non valido.\r\n\r\nIl PNG deve chiamarsi esattamente:\r\npallette <numero>.png\r\n\r\nEsempi:\r\npallette 1.png\r\npallette 25.png\r\n\r\nIl file non è stato importato.",
			MB_OK|MB_ICONERROR)
		return
	}
	surf, err := loadPNGSurface(src)
	if err != nil {
		msgbox("PML Studio - Inserisci palette", "Il file selezionato non è un PNG palette leggibile:\r\n\r\n"+err.Error(), MB_OK|MB_ICONERROR)
		return
	}

	dstDir := canonicalPaletteDir(currentProject)
	if err := os.MkdirAll(dstDir, 0755); err != nil {
		msgbox("PML Studio - Inserisci palette", "Impossibile creare la cartella palette:\r\n\r\n"+err.Error(), MB_OK|MB_ICONERROR)
		return
	}
	dst := filepath.Join(dstDir, fmt.Sprintf("pallette %d.png", id))

	if exists(dst) && !strings.EqualFold(filepath.Clean(src), filepath.Clean(dst)) {
		oldHash, oldErr := fsFileSHA256(dst)
		newHash, newErr := fsFileSHA256(src)
		if oldErr == nil && newErr == nil && oldHash == newHash {
			_ = rebuildPaletteCatalog(currentProject)
			clearTilesetDescriptorCache()
			reloadCurrentTilesetGraphic(id)
			setText(hwndStatus, fmt.Sprintf("Palette %03d già presente | %dx%d", id, surf.Width, surf.Height))
			return
		}
		if msgboxResult("PML Studio - Inserisci palette",
			fmt.Sprintf("La Palette %03d esiste già.\r\n\r\nSostituirla con il PNG selezionato?", id),
			MB_YESNOCANCEL|MB_ICONINFORMATION) != IDYES {
			return
		}
	}

	if !strings.EqualFold(filepath.Clean(src), filepath.Clean(dst)) {
		tmp := dst + ".tmp"
		_ = os.Remove(tmp)
		if err := copyFileExact(src, tmp); err != nil {
			msgbox("PML Studio - Inserisci palette", "Copia palette non riuscita:\r\n\r\n"+err.Error(), MB_OK|MB_ICONERROR)
			return
		}
		srcHash, err1 := fsFileSHA256(src)
		tmpHash, err2 := fsFileSHA256(tmp)
		if err1 != nil || err2 != nil || srcHash != tmpHash {
			_ = os.Remove(tmp)
			msgbox("PML Studio - Inserisci palette", "Verifica SHA-256 della palette fallita. Il file originale non è stato modificato.", MB_OK|MB_ICONERROR)
			return
		}
		_ = os.Remove(dst)
		if err := os.Rename(tmp, dst); err != nil {
			_ = os.Remove(tmp)
			msgbox("PML Studio - Inserisci palette", "Impossibile installare la palette:\r\n\r\n"+err.Error(), MB_OK|MB_ICONERROR)
			return
		}
	}

	// Rebuild both the generic Graphics asset index and the numbered palette
	// catalog so the new file is immediately available without reopening PLM.
	rebuildAssetIndex(currentProject)
	if err := rebuildPaletteCatalog(currentProject); err != nil {
		msgbox("PML Studio - Inserisci palette", "Palette copiata, ma indice non aggiornato:\r\n\r\n"+err.Error(), MB_OK|MB_ICONERROR)
		return
	}
	// La combo Tileset della Vista mappa deve riflettere immediatamente la
	// tavola appena installata. Se il Tileset ID è già in uso sulla mappa
	// corrente, ricarichiamo anche renderer/palette senza riavviare PLM.
	clearTilesetDescriptorCache()
	reloadCurrentTilesetGraphic(id)

	colors := paletteColorsFromSurface(surf)
	setText(hwndStatus, fmt.Sprintf("Tileset/pallette %d caricata | %d colori | %dx%d | %s", id, len(colors), surf.Width, surf.Height, paletteRelativePath(currentProject, dst)))
	msgbox("PML Studio - Inserisci palette",
		fmt.Sprintf("Palette %d caricata correttamente.\r\n\r\n%s\r\nDimensioni: %dx%d\r\nColori rilevati: %d", id, paletteRelativePath(currentProject, dst), surf.Width, surf.Height, len(colors)),
		MB_OK|MB_ICONINFORMATION)
}
