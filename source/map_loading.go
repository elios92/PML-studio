//go:build windows

package main

import (
	"fmt"
	"runtime/debug"
	"sync/atomic"
	"syscall"
	"time"
)

const (
	wmAppMapLoadProgress = 0x8001
	wmAppMapLoadDone     = 0x8002
	mapLoadTimeout       = 20 * time.Second
)

type mapLoadResult struct {
	Generation uint64
	Index      int
	OldIndex   int
	Doc        *MapDocument
	Tileset    *TilesetDescriptor
	Surface    *PixelSurface
	Err        error
	TilesetErr error
	Stage      string
	TimedOut   bool
	PanicStack string
}

var (
	mapLoadGeneration atomic.Uint64
	mapLoadResults    = make(chan mapLoadResult, 8)
	mapLoading        atomic.Bool
	mapLoadingPercent int
	mapLoadingStage   = "Pronto"
	mapLoadingMapID   int
	pendingMapIndex   = -1
)

func mapLoadStageText(stage uintptr) string {
	switch stage {
	case 1:
		return "Apertura file mappa"
	case 2:
		return "Lettura layer"
	case 3:
		return "Risoluzione tileset"
	case 4:
		return "Caricamento tileset e autotile"
	case 5:
		return "Preparazione editor"
	case 6:
		return "Completato"
	default:
		return "Caricamento mappa"
	}
}

func postMapMessage(msg uint32, generation uint64, lparam uintptr) {
	if hwndMain == 0 {
		return
	}
	// PostMessageW is asynchronous. Never use SendMessageW from the loader
	// goroutine: a synchronous cross-thread call can stall behind WM_PAINT and
	// make the application look frozen.
	pPostMessageW.Call(uintptr(hwndMain), uintptr(msg), uintptr(generation), lparam)
}

func postMapLoadProgress(generation uint64, percent int, stage uintptr) {
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	postMapMessage(wmAppMapLoadProgress, generation, uintptr(percent&0xff)|stage<<16)
}

func publishMapLoadResult(r mapLoadResult) {
	select {
	case mapLoadResults <- r:
	default:
		// Project switches/timeouts can leave stale generations queued. Never
		// drop the newest completion because an obsolete result occupies the
		// bounded channel: evict one old item and retry once.
		select {
		case stale := <-mapLoadResults:
			mapLogf("[MAP] evicted stale load result generation=%d while publishing generation=%d", stale.Generation, r.Generation)
		default:
		}
		select {
		case mapLoadResults <- r:
		default:
			mapLogf("[ERROR] map load result queue remained full for generation=%d", r.Generation)
		}
	}
	postMapMessage(wmAppMapLoadDone, r.Generation, 0)
}

// cancelMapLoadsForProjectSwitch invalidates every worker started for the
// previous project before global map state is replaced. A late result must never
// be applied to a new project's maps slice just because it has the same index.
func cancelMapLoadsForProjectSwitch() {
	mapLoadGeneration.Add(1)
	mapLoading.Store(false)
	mapLoadingPercent = 0
	mapLoadingStage = "Pronto"
	mapLoadingMapID = 0
	pendingMapIndex = -1
}

func startAsyncMapLoad(idx, oldIndex int) {
	if idx < 0 || idx >= len(maps) {
		return
	}
	// Movement permissions / Terrain Tags are map-local data too. A failed
	// write must block navigation, otherwise loadSidecars() for the next map
	// would reset the dirty overrides and silently lose the user's work.
	if permissionDataDirty && currentMap != nil && maps[idx].ID != currentMap.ID {
		savePermissions()
		if permissionDataDirty {
			msgbox("PML Studio", "Impossibile salvare Movimenti / Terrain Tags prima di cambiare mappa.\r\n\r\nIl cambio mappa è stato annullato per evitare perdita dati.", MB_OK|MB_ICONERROR)
			setToolbarStatus("Movimenti / Terrain Tags: cambio mappa bloccato - salvataggio non riuscito")
			return
		}
	}

	// Gli incontri sono un dato della mappa: se l'utente cambia mappa con
	// modifiche ancora in memoria, salviamole prima di sostituire currentMap.
	// In caso di errore il cambio viene bloccato, evitando perdita silenziosa.
	if encounterDirty && currentMap != nil && maps[idx].ID != currentMap.ID {
		if err := saveEncounterData(); err != nil {
			msgbox("PML Studio", "Impossibile salvare gli incontri prima di cambiare mappa:\r\n\r\n"+err.Error(), MB_OK|MB_ICONERROR)
			setToolbarStatus("Pokémon selvatici: cambio mappa bloccato - " + err.Error())
			return
		}
	}
	if mapLoading.Load() {
		// Do not discard rapid navigation. Keep only the most recent requested
		// map; when the current worker finishes, that map is loaded next.
		pendingMapIndex = idx
		setText(hwndStatus, fmt.Sprintf("Caricamento Map%03d in corso... Map%03d in coda", mapLoadingMapID, maps[idx].ID))
		diagLogf("[UI] map selection queued idx=%d Map%03d while Map%03d is loading", idx, maps[idx].ID, mapLoadingMapID)
		return
	}

	m := maps[idx]
	project := currentProject
	generation := mapLoadGeneration.Add(1)
	mapLoading.Store(true)
	mapLoadingPercent = 0
	mapLoadingStage = "Avvio caricamento"
	mapLoadingMapID = m.ID
	setText(hwndStatus, fmt.Sprintf("Caricamento Map%03d... 0%%", m.ID))
	invalidate(hwndCanvas)

	// Watchdog: if a decoder/resolver gets stuck, return control to the user
	// instead of leaving the Win32 window in a permanent loading state.
	go func(gen uint64, mapID int) {
		timer := time.NewTimer(mapLoadTimeout)
		defer timer.Stop()
		<-timer.C
		if gen != mapLoadGeneration.Load() || !mapLoading.Load() {
			return
		}
		publishMapLoadResult(mapLoadResult{
			Generation: gen,
			Index:      idx,
			OldIndex:   oldIndex,
			Err:        fmt.Errorf("tempo massimo di caricamento superato (%s)", mapLoadTimeout),
			Stage:      "Watchdog caricamento",
			TimedOut:   true,
		})
		mapLogf("[ERROR] Map%03d loading timeout after %s", mapID, mapLoadTimeout)
	}(generation, m.ID)

	go func() {
		stage := "Avvio caricamento"
		defer func() {
			if r := recover(); r != nil {
				stack := string(debug.Stack())
				mapLogf("[PANIC] Map%03d stage=%s panic=%v\n%s", m.ID, stage, r, stack)
				publishMapLoadResult(mapLoadResult{
					Generation: generation,
					Index:      idx,
					OldIndex:   oldIndex,
					Err:        fmt.Errorf("errore interno durante %s: %v", stage, r),
					Stage:      stage,
					PanicStack: stack,
				})
			}
		}()

		stage = "Apertura file mappa"
		postMapLoadProgress(generation, 5, 1)
		mapLogf("[MAP] Opening %s", m.File)
		doc, err := loadMapDocument(m.File)
		if err != nil {
			publishMapLoadResult(mapLoadResult{Generation: generation, Index: idx, OldIndex: oldIndex, Err: err, Stage: stage})
			return
		}

		stage = "Lettura layer"
		postMapLoadProgress(generation, 30, 2)
		if doc.Table.Width <= 0 || doc.Table.Height <= 0 || len(doc.Table.Layers) == 0 {
			publishMapLoadResult(mapLoadResult{
				Generation: generation,
				Index:      idx,
				OldIndex:   oldIndex,
				Err:        fmt.Errorf("dati mappa non validi: size=%dx%d layers=%d", doc.Table.Width, doc.Table.Height, len(doc.Table.Layers)),
				Stage:      stage,
			})
			return
		}
		mapLogf("[MAP] size=%dx%d tileset_id=%d layers=%d", doc.Table.Width, doc.Table.Height, doc.TilesetID, len(doc.Table.Layers))

		stage = "Risoluzione tileset"
		postMapLoadProgress(generation, 45, 3)
		stage = "Caricamento tileset e autotile"
		postMapLoadProgress(generation, 55, 4)
		ts, tsErr := loadTilesetDescriptor(project, doc.TilesetID)

		stage = "Preparazione editor"
		postMapLoadProgress(generation, 80, 5)
		var surface *PixelSurface
		if tsErr == nil && ts != nil {
			var surfErr error
			surface, surfErr = buildMapSurface(doc, ts)
			if surfErr != nil {
				publishMapLoadResult(mapLoadResult{Generation: generation, Index: idx, OldIndex: oldIndex, Err: surfErr, Stage: "Preparazione cache grafica"})
				return
			}
		}
		postMapLoadProgress(generation, 95, 5)
		publishMapLoadResult(mapLoadResult{
			Generation: generation,
			Index:      idx,
			OldIndex:   oldIndex,
			Doc:        doc,
			Tileset:    ts,
			Surface:    surface,
			TilesetErr: tsErr,
			Stage:      stage,
		})
	}()
}

func handleMapLoadProgress(generation uint64, packed uintptr) {
	if generation != mapLoadGeneration.Load() || !mapLoading.Load() {
		return
	}
	mapLoadingPercent = int(packed & 0xff)
	stage := (packed >> 16) & 0xffff
	mapLoadingStage = mapLoadStageText(stage)
	setText(hwndStatus, fmt.Sprintf("Caricamento Map%03d... %d%% - %s", mapLoadingMapID, mapLoadingPercent, mapLoadingStage))
	invalidate(hwndCanvas)
}

func takeMapLoadResult(generation uint64) (mapLoadResult, bool) {
	for {
		select {
		case r := <-mapLoadResults:
			if r.Generation == generation {
				return r, true
			}
		default:
			return mapLoadResult{}, false
		}
	}
}

func failMapLoad(r mapLoadResult) {
	mapLoading.Store(false)
	mapLoadingPercent = 0
	if r.Stage == "" {
		r.Stage = mapLoadingStage
	}
	mapLoadingStage = "Errore"

	if r.OldIndex >= 0 {
		selectMapListIndexForMap(r.OldIndex)
	}

	// Invalidate the worker generation. If a timed-out decoder eventually
	// finishes, its late result is ignored instead of overwriting current data.
	mapLoadGeneration.Add(1)

	detail := "errore sconosciuto"
	if r.Err != nil {
		detail = r.Err.Error()
	}
	mapLogf("[ERROR] Map%03d stage=%s: %s", mapLoadingMapID, r.Stage, detail)
	setText(hwndStatus, fmt.Sprintf("Errore Map%03d - %s: %s", mapLoadingMapID, r.Stage, detail))

	message := fmt.Sprintf(
		"Il caricamento della mappa è stato interrotto in modo sicuro.\r\n\r\nMappa: Map%03d\r\nFase: %s\r\nErrore: %s\r\n\r\nPLM Studio resta aperto e utilizzabile.\r\nControlla PLM_Studio_debug.log accanto all'eseguibile.",
		mapLoadingMapID, r.Stage, detail,
	)
	if r.TimedOut {
		message += "\r\n\r\nIl watchdog ha rilevato un'operazione troppo lunga; il risultato tardivo verrà ignorato."
	}
	msgbox("PLM Studio - Errore caricamento mappa", message, MB_OK|MB_ICONERROR)
	invalidate(hwndCanvas)
}

func handleMapLoadDone(generation uint64) {
	if generation != mapLoadGeneration.Load() {
		return
	}
	r, ok := takeMapLoadResult(generation)
	if !ok {
		return
	}

	// If the user selected another map while this one was loading, don't make
	// them click it again and don't briefly snap the list back. Discard the
	// now-stale result and immediately start the latest queued selection.
	if pendingMapIndex >= 0 && pendingMapIndex != r.Index {
		next := pendingMapIndex
		pendingMapIndex = -1
		mapLoading.Store(false)
		diagLogf("[UI] consuming queued map selection idx=%d", next)
		startAsyncMapLoad(next, currentMapIndex())
		return
	}
	pendingMapIndex = -1

	if r.Err != nil {
		failMapLoad(r)
		return
	}

	mapLoading.Store(false)
	mapLoadingPercent = 100
	mapLoadingStage = "Completato"

	if r.Index < 0 || r.Index >= len(maps) {
		failMapLoad(mapLoadResult{
			Generation: generation,
			Index:      r.Index,
			OldIndex:   r.OldIndex,
			Err:        fmt.Errorf("la mappa caricata non è più presente nell'elenco"),
			Stage:      "Applicazione risultato",
		})
		return
	}
	if r.Doc == nil {
		failMapLoad(mapLoadResult{
			Generation: generation,
			Index:      r.Index,
			OldIndex:   r.OldIndex,
			Err:        fmt.Errorf("documento mappa nullo"),
			Stage:      "Applicazione risultato",
		})
		return
	}

	m := &maps[r.Index]
	currentMap = m
	currentMapDoc = r.Doc
	currentTileset = r.Tileset
	currentMapSurface = r.Surface
	currentMapW = r.Doc.Table.Width
	currentMapH = r.Doc.Table.Height
	if m.RegionID != "" {
		syncActiveRegionToMap(m.RegionID)
	}
	activeLayer = 0
	mapScrollX, mapScrollY = 0, 0
	clearMapHistory()
	if currentTileset != nil {
		resetMapRenderCacheForTileset(currentTileset)
	}
	loadSidecars()
	updateMapQuickInfo()
	loadHeaderFromCurrentMap()
	if mode == "encounters" {
		loadEncounterEditorForCurrentMap()
	}
	if mode == "connections" {
		loadConnectionsEditorForCurrentMap()
	}
	updateMapToolButtonStates()
	pSendMessageW.Call(uintptr(hwndLevelCombo), CB_SETCURSEL, 0, 0)
	updateCanvasScrollbars(hwndCanvas)
	populatePaletteControls()
	updateInspector()

	if r.TilesetErr != nil {
		mapLogf("[ERROR] Tileset %d referenced by map was not found: %s", r.Doc.TilesetID, r.TilesetErr)
		setText(hwndStatus, fmt.Sprintf("Map%03d caricata | tileset_id=%d | ERRORE: %s", m.ID, r.Doc.TilesetID, r.TilesetErr.Error()))
	} else if currentTileset != nil {
		loadedAT := 0
		for _, at := range currentTileset.Autotiles {
			if at != nil {
				loadedAT++
			}
		}
		mapLogf("[TILESET] id=%d name=%q path=%s size=%dx%d metadata=%s", currentTileset.ID, currentTileset.TilesetName, currentTileset.TilesetPath, currentTileset.Tileset.Width, currentTileset.Tileset.Height, currentTileset.MetadataPath)
		mapLogf("[AUTOTILE] loaded %d/%d resources", loadedAT, len(currentTileset.Autotiles))
		mapLogf("[RENDER] ready")
		setText(hwndStatus, fmt.Sprintf("Map%03d | Tileset %d: %s | autotile %d/%d", m.ID, currentTileset.ID, currentTileset.TilesetName, loadedAT, len(currentTileset.Autotiles)))
	} else {
		setText(hwndStatus, fmt.Sprintf("Map%03d caricata | nessun tileset disponibile", m.ID))
	}

	updateMapEditorStatus()
	invalidate(hwndCanvas)
	// La semplice apertura di una mappa NON modifica la struttura del progetto.
	// Il nodo esiste già: selezionarlo è O(1) e evita di ricostruire centinaia
	// di elementi ad ogni click. Solo se il nodo manca davvero chiediamo un
	// rebuild di recupero nel ciclo UI successivo.
	if !selectMapListIndexForMap(r.Index) {
		mapLogf("[TREE] current map node missing after load idx=%d Map%03d; scheduling recovery rebuild", r.Index, m.ID)
		scheduleMapTreeRefresh()
	}
}

func drawMapLoadingOverlay(hdc syscall.Handle, r RECT) {
	if !mapLoading.Load() || hdc == 0 {
		return
	}
	cw := r.Right - r.Left
	ch := r.Bottom - r.Top
	boxW := int32(430)
	boxH := int32(118)
	if cw < boxW+20 {
		boxW = cw - 20
	}
	if boxW < 260 {
		boxW = 260
	}
	x := (cw - boxW) / 2
	y := (ch - boxH) / 2
	if y < 40 {
		y = 40
	}

	panel := createSolidBrush(rgb(245, 245, 245))
	border := createSolidBrush(rgb(125, 125, 125))
	barBg := createSolidBrush(rgb(220, 220, 220))
	barFill := createSolidBrush(rgb(60, 130, 210))
	defer deleteGDIObject(panel)
	defer deleteGDIObject(border)
	defer deleteGDIObject(barBg)
	defer deleteGDIObject(barFill)

	fillWithBrush(hdc, RECT{Left: x, Top: y, Right: x + boxW, Bottom: y + boxH}, border)
	fillWithBrush(hdc, RECT{Left: x + 1, Top: y + 1, Right: x + boxW - 1, Bottom: y + boxH - 1}, panel)

	text(hdc, x+16, y+14, fmt.Sprintf("Caricamento Map%03d", mapLoadingMapID), rgb(25, 25, 25))
	text(hdc, x+16, y+36, mapLoadingStage, rgb(70, 70, 70))

	barLeft := x + 16
	barTop := y + 64
	barRight := x + boxW - 16
	barBottom := barTop + 20
	fillWithBrush(hdc, RECT{Left: barLeft, Top: barTop, Right: barRight, Bottom: barBottom}, barBg)
	innerW := barRight - barLeft - 2
	filled := int32(int64(innerW) * int64(mapLoadingPercent) / 100)
	if filled > 0 {
		fillWithBrush(hdc, RECT{Left: barLeft + 1, Top: barTop + 1, Right: barLeft + 1 + filled, Bottom: barBottom - 1}, barFill)
	}
	text(hdc, x+boxW-62, y+88, fmt.Sprintf("%d%%", mapLoadingPercent), rgb(25, 25, 25))
}
