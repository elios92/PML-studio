//go:build windows

package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// tilesetSelectorIDs mirrors the rows of the right sidebar Tileset combo.
// The combo is a view over real project data; the selected ID is always the
// tileset_id persisted in the current MapXXX.json.
var tilesetSelectorIDs []int

func tilesetSelectorLabel(id int) string {
	ensurePaletteCatalog(currentProject)
	ensureTilesetMetadataIndex(currentProject)

	if p := paletteCatalogByID[id]; p != nil {
		return fmt.Sprintf("Tileset %03d - %s", id, filepath.Base(filepath.FromSlash(p.File)))
	}
	if rec, ok := tilesetMetadataByID[id]; ok {
		name := strings.TrimSpace(rec.Data.Name)
		if name == "" {
			name = strings.TrimSpace(rec.Data.TilesetName)
		}
		if name == "" {
			name = fmt.Sprintf("Tileset %d", id)
		}
		return fmt.Sprintf("Tileset %03d - %s", id, name)
	}
	return fmt.Sprintf("Tileset %03d - corrente", id)
}

func populateTilesetSelector() {
	if hwndPaletteModeCombo == 0 {
		return
	}
	pSendMessageW.Call(uintptr(hwndPaletteModeCombo), CB_RESETCONTENT, 0, 0)
	tilesetSelectorIDs = tilesetSelectorIDs[:0]

	if currentProject == "" {
		comboAdd(hwndPaletteModeCombo, "Nessun progetto")
		pSendMessageW.Call(uintptr(hwndPaletteModeCombo), CB_SETCURSEL, 0, 0)
		return
	}

	ensurePaletteCatalog(currentProject)
	ensureTilesetMetadataIndex(currentProject)
	idsSet := make(map[int]bool)
	for id := range paletteCatalogByID {
		if id > 0 {
			idsSet[id] = true
		}
	}
	for id := range tilesetMetadataByID {
		if id > 0 {
			idsSet[id] = true
		}
	}
	if currentMapDoc != nil && currentMapDoc.TilesetID > 0 {
		idsSet[currentMapDoc.TilesetID] = true
	}

	ids := make([]int, 0, len(idsSet))
	for id := range idsSet {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	if len(ids) == 0 {
		comboAdd(hwndPaletteModeCombo, "Nessun tileset caricato")
		pSendMessageW.Call(uintptr(hwndPaletteModeCombo), CB_SETCURSEL, 0, 0)
		return
	}

	selected := -1
	for _, id := range ids {
		comboAdd(hwndPaletteModeCombo, tilesetSelectorLabel(id))
		tilesetSelectorIDs = append(tilesetSelectorIDs, id)
		if currentMapDoc != nil && id == currentMapDoc.TilesetID {
			selected = len(tilesetSelectorIDs) - 1
		}
	}
	if selected < 0 {
		selected = 0
	}
	pSendMessageW.Call(uintptr(hwndPaletteModeCombo), CB_SETCURSEL, uintptr(selected), 0)
}

func selectedTilesetIDFromSelector() (int, bool) {
	if hwndPaletteModeCombo == 0 || len(tilesetSelectorIDs) == 0 {
		return 0, false
	}
	sel, _, _ := pSendMessageW.Call(uintptr(hwndPaletteModeCombo), CB_GETCURSEL, 0, 0)
	i := int(sel)
	if i < 0 || i >= len(tilesetSelectorIDs) {
		return 0, false
	}
	return tilesetSelectorIDs[i], true
}

func restoreTilesetSelectorSelection() {
	if currentMapDoc == nil || hwndPaletteModeCombo == 0 {
		return
	}
	for i, id := range tilesetSelectorIDs {
		if id == currentMapDoc.TilesetID {
			pSendMessageW.Call(uintptr(hwndPaletteModeCombo), CB_SETCURSEL, uintptr(i), 0)
			return
		}
	}
}

func setMapDocumentTilesetID(doc *MapDocument, id int) error {
	if doc == nil {
		return fmt.Errorf("nessuna mappa caricata")
	}
	raw, err := json.Marshal(id)
	if err != nil {
		return err
	}
	if doc.Raw == nil {
		doc.Raw = make(map[string]json.RawMessage)
	}
	doc.TilesetID = id
	doc.Raw["tileset_id"] = raw
	return nil
}

// applyTilesetToCurrentMap changes the real map tileset, not just the preview.
// The renderer is built before the MapXXX.json is committed, so an invalid PNG
// cannot leave the map pointing at a tileset that PLM Studio cannot render.
func applyTilesetToCurrentMap(id int) error {
	if currentProject == "" || currentMap == nil || currentMapDoc == nil {
		return fmt.Errorf("seleziona prima una mappa")
	}
	if id <= 0 {
		return fmt.Errorf("Tileset ID non valido: %d", id)
	}

	oldID := currentMapDoc.TilesetID
	oldTileset := currentTileset
	oldSurface := currentMapSurface
	oldRaw := currentMapDoc.Raw["tileset_id"]

	setText(hwndStatus, fmt.Sprintf("Caricamento Tileset %03d...", id))
	// A palette can have been replaced while PLM Studio is open. Force a
	// descriptor reload so selecting it always reflects the PNG on disk.
	delete(tilesetDescriptorCache, tilesetCacheKey(currentProject, id))
	ts, err := loadTilesetDescriptor(currentProject, id)
	if err != nil {
		restoreTilesetSelectorSelection()
		return fmt.Errorf("Tileset %03d non caricabile: %w", id, err)
	}
	surface, err := buildMapSurface(currentMapDoc, ts)
	if err != nil {
		restoreTilesetSelectorSelection()
		return fmt.Errorf("impossibile ridisegnare la mappa con Tileset %03d: %w", id, err)
	}

	if err := setMapDocumentTilesetID(currentMapDoc, id); err != nil {
		restoreTilesetSelectorSelection()
		return err
	}
	if err := saveMapDocument(currentMapDoc); err != nil {
		currentMapDoc.TilesetID = oldID
		if oldRaw != nil {
			currentMapDoc.Raw["tileset_id"] = oldRaw
		} else {
			delete(currentMapDoc.Raw, "tileset_id")
		}
		currentTileset = oldTileset
		currentMapSurface = oldSurface
		restoreTilesetSelectorSelection()
		return fmt.Errorf("salvataggio Map%03d fallito: %w", currentMap.ID, err)
	}

	currentTileset = ts
	currentMapSurface = surface
	resetMapRenderCacheForTileset(ts)
	tilePaletteScroll = 0
	autotilePaletteScroll = 0
	selectedTileID = 384
	rebuildDerivedPermissionData()
	populatePaletteControls()
	populateTilesetSelector()
	updateMapEditorStatus()
	updateCanvasScrollbars(hwndCanvas)
	invalidate(hwndCanvas)
	invalidate(hwndPaletteTilesetList)
	invalidate(hwndPaletteAutotileList)
	invalidate(hwndPaletteBorderArea)
	mapLogf("[TILESET] Map%03d tileset changed %d -> %d (%s)", currentMap.ID, oldID, id, ts.TilesetPath)
	setText(hwndStatus, fmt.Sprintf("Map%03d | Tileset %03d applicato e salvato | %s", currentMap.ID, id, filepath.Base(ts.TilesetPath)))
	return nil
}

func changeMapTilesetFromSelector() {
	id, ok := selectedTilesetIDFromSelector()
	if !ok || currentMapDoc == nil {
		return
	}
	if id == currentMapDoc.TilesetID {
		// Still rebuild the visual state from disk. This is useful after a PNG
		// was replaced through Strumenti -> Inserisci palette.
		delete(tilesetDescriptorCache, tilesetCacheKey(currentProject, id))
	}
	if err := applyTilesetToCurrentMap(id); err != nil {
		msgbox("PML Studio - Tileset", err.Error(), MB_OK|MB_ICONERROR)
		setText(hwndStatus, "Tileset: "+err.Error())
	}
}

// reloadCurrentTilesetGraphic is used after importing/replacing a numbered
// palette PNG. It does not change tileset_id; it only reloads and redraws the
// current map if that numbered tileset is in use.
func reloadCurrentTilesetGraphic(id int) {
	populateTilesetSelector()
	if currentMapDoc == nil || currentMap == nil || currentMapDoc.TilesetID != id {
		return
	}
	delete(tilesetDescriptorCache, tilesetCacheKey(currentProject, id))
	ts, err := loadTilesetDescriptor(currentProject, id)
	if err != nil {
		mapLogf("[PALETTE] reload tileset %d failed: %v", id, err)
		return
	}
	surface, err := buildMapSurface(currentMapDoc, ts)
	if err != nil {
		mapLogf("[PALETTE] redraw Map%03d with tileset %d failed: %v", currentMap.ID, id, err)
		return
	}
	currentTileset = ts
	currentMapSurface = surface
	resetMapRenderCacheForTileset(ts)
	rebuildDerivedPermissionData()
	populatePaletteControls()
	invalidate(hwndCanvas)
	invalidate(hwndPaletteTilesetList)
	invalidate(hwndPaletteAutotileList)
}
