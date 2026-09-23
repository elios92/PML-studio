//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

const idMapRegionFilter = 1105

const (
	mapRegionFilterAll        = "__all__"
	mapRegionFilterUnassigned = "__unassigned__"
)

var (
	mapRegionFilter   = mapRegionFilterAll
	mapRegionComboIDs []string
)

func createLeftSidebar(hInst syscall.Handle) {
	// Advance Map-style navigation, now using a real Win32 tree so regions and
	// their folders are visually separated instead of being flattened rows.
	hwndSidebar = createWindow("STATIC", "", WS_CHILD|WS_VISIBLE, 0, 0, 0, 0, hwndMain, 1100, hInst)
	hwndMapRegionLabel = createWindow("STATIC", "Regione", WS_CHILD|WS_VISIBLE, 0, 0, 0, 0, hwndMain, 1104, hInst)
	hwndMapRegionCombo = createWindow("COMBOBOX", "", WS_CHILD|WS_VISIBLE|WS_TABSTOP|CBS_DROPDOWNLIST, 0, 0, 0, 180, hwndMain, idMapRegionFilter, hInst)

	ensureMapTreeCommonControls()
	treeStyle := uint32(WS_CHILD | WS_VISIBLE | WS_BORDER | WS_VSCROLL | WS_TABSTOP | tvsHasButtons | tvsHasLines | tvsLinesAtRoot | tvsEditLabels | tvsShowSelAlways | tvsNoHScroll)
	hwndMapList = createWindow("SysTreeView32", "", treeStyle, 0, 0, 0, 0, hwndMain, 1101, hInst)
	attachMapTreeSystemIcons()
	applyModernTreePalette(hwndMapList)
	showMapTreeMessage("Apri un progetto per visualizzare le mappe")

	hwndProjectInfo = createWindow("STATIC", "Nessun progetto aperto", WS_CHILD|WS_VISIBLE, 0, 0, 0, 0, hwndMain, 1102, hInst)
	hwndMapQuickInfo = createWindow("STATIC", "Nessuna mappa selezionata", WS_CHILD|WS_VISIBLE, 0, 0, 0, 0, hwndMain, 1103, hInst)
	refreshMapEditorRegionControls()
}

func resetMapEditorRegionFilter() {
	mapRegionFilter = mapRegionFilterAll
	mapRegionComboIDs = nil
}

func mapRegionDisplayName(regionID string) string {
	if strings.TrimSpace(regionID) == "" {
		return "Senza regione"
	}
	if name := regionNameByID(regionID); name != "" {
		return name
	}
	return regionID
}

func refreshMapEditorRegionControls() {
	refreshMapEditorRegionControlsInternal(true)
}

// refreshMapEditorRegionControlsNoTree aggiorna soltanto la combo/regione
// visualizzata. Serve durante la semplice selezione/caricamento di una mappa:
// in quel caso la struttura dell'albero non è cambiata e ricostruire centinaia
// di nodi sarebbe lavoro inutile oltre che una fonte di refresh sovrapposti.
func refreshMapEditorRegionControlsNoTree() {
	refreshMapEditorRegionControlsInternal(false)
}

func refreshMapEditorRegionControlsInternal(rebuildTree bool) {
	if hwndMapRegionCombo == 0 {
		return
	}
	pSendMessageW.Call(uintptr(hwndMapRegionCombo), cbResetContent, 0, 0)
	mapRegionComboIDs = mapRegionComboIDs[:0]

	comboAdd(hwndMapRegionCombo, "Tutte le regioni")
	mapRegionComboIDs = append(mapRegionComboIDs, mapRegionFilterAll)

	if len(projectRegions.Regions) > 0 {
		comboAdd(hwndMapRegionCombo, "Mappe senza regione")
		mapRegionComboIDs = append(mapRegionComboIDs, mapRegionFilterUnassigned)
		for _, r := range projectRegions.Regions {
			label := r.Name
			if strings.EqualFold(r.ID, projectRegions.ActiveRegionID) {
				label += "  [attiva]"
			}
			comboAdd(hwndMapRegionCombo, label)
			mapRegionComboIDs = append(mapRegionComboIDs, r.ID)
		}
	}

	selected := 0
	for i, id := range mapRegionComboIDs {
		if strings.EqualFold(id, mapRegionFilter) {
			selected = i
			break
		}
	}
	if selected == 0 && mapRegionFilter != mapRegionFilterAll {
		mapRegionFilter = mapRegionFilterAll
	}
	pSendMessageW.Call(uintptr(hwndMapRegionCombo), CB_SETCURSEL, uintptr(selected), 0)
	// Do not rebuild the native TreeView inline while combos/header controls are
	// themselves being refreshed. A posted rebuild runs after the current Win32
	// message completes and prevents the tree from remaining visually empty.
	if rebuildTree {
		scheduleMapTreeRefresh()
	}
}

func setMapRegionFilterFromCombo() {
	if hwndMapRegionCombo == 0 || len(mapRegionComboIDs) == 0 {
		return
	}
	idx, _, _ := pSendMessageW.Call(uintptr(hwndMapRegionCombo), CB_GETCURSEL, 0, 0)
	i := int(idx)
	if i < 0 || i >= len(mapRegionComboIDs) {
		return
	}
	mapRegionFilter = mapRegionComboIDs[i]

	// A real region selected in the Map Editor becomes the project active
	// region. Global/unassigned views do not alter the destination region.
	if mapRegionFilter != mapRegionFilterAll && mapRegionFilter != mapRegionFilterUnassigned {
		if regionByID(mapRegionFilter) != nil {
			projectRegions.ActiveRegionID = mapRegionFilter
			_ = saveRegionRegistry()
		}
	}
	refreshRegionControls()
	// refreshRegionControls updates the combos; rebuild the tree on the next UI
	// turn so the selection notification cannot invalidate the TreeView midway.
	scheduleMapTreeRefresh()
	if mapRegionFilter == mapRegionFilterAll {
		setToolbarStatus("Mappe: tutte le regioni")
	} else if mapRegionFilter == mapRegionFilterUnassigned {
		setToolbarStatus("Mappe: senza regione")
	} else {
		setToolbarStatus("Regione attiva: " + mapRegionDisplayName(mapRegionFilter))
	}
}

func mapMatchesRegionFilter(m MapEntry) bool {
	switch mapRegionFilter {
	case mapRegionFilterAll:
		return true
	case mapRegionFilterUnassigned:
		return strings.TrimSpace(m.RegionID) == ""
	default:
		return strings.EqualFold(m.RegionID, mapRegionFilter)
	}
}

func mapIndexForFile(path string) int {
	path = filepath.Clean(path)
	for i := range maps {
		if maps[i].File != "" && strings.EqualFold(filepath.Clean(maps[i].File), path) {
			return i
		}
	}
	return -1
}

func directMapIndicesInFolder(dir string, regionID string) []int {
	out := make([]int, 0)
	dir = filepath.Clean(dir)
	for i, m := range maps {
		if m.File == "" || !strings.EqualFold(strings.TrimSpace(m.RegionID), strings.TrimSpace(regionID)) {
			continue
		}
		if strings.EqualFold(filepath.Clean(filepath.Dir(m.File)), dir) {
			out = append(out, i)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if maps[out[i]].ID != maps[out[j]].ID {
			return maps[out[i]].ID < maps[out[j]].ID
		}
		return strings.ToLower(maps[out[i]].Name) < strings.ToLower(maps[out[j]].Name)
	})
	return out
}

// rpgTreeMapIndices returns the maps that belong to a visual RPG Maker-style
// branch. The physical file location can be nested in any subfolder; the
// visual hierarchy is driven by MapInfos ParentID/Order, exactly like RMXP.
func rpgTreeMapIndices(regionID string, rootPath string) []int {
	out := make([]int, 0)
	for i, m := range maps {
		// Le mappe usate esclusivamente come intro/animazioni di sistema non
		// appartengono all'albero delle mappe giocabili. In Vista eventi vengono
		// mostrate in un ramo dedicato EVENTI / ANIMAZIONI.
		if isIntroAnimationMapID(m.ID) {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(m.RegionID), strings.TrimSpace(regionID)) {
			continue
		}
		if strings.TrimSpace(rootPath) != "" && strings.TrimSpace(m.File) != "" && !pathWithinRoot(rootPath, m.File) {
			continue
		}
		out = append(out, i)
	}
	return out
}

func sortRPGMapIndices(indices []int) {
	sort.SliceStable(indices, func(i, j int) bool {
		a, b := maps[indices[i]], maps[indices[j]]
		if a.Order != b.Order {
			return a.Order < b.Order
		}
		if a.ID != b.ID {
			return a.ID < b.ID
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})
}

// insertRPGMapHierarchy renders the logical map tree from MapInfos. A child is
// nested only if its parent exists inside the same region/root branch. This
// keeps regions strictly separated even if an old MapInfos entry still points
// to a parent that lives in another region.
func insertRPGMapHierarchy(parent uintptr, regionID string, rootPath string) int {
	indices := rpgTreeMapIndices(regionID, rootPath)
	if len(indices) == 0 {
		return 0
	}

	byID := make(map[int]int, len(indices))
	included := make(map[int]bool, len(indices))
	for _, idx := range indices {
		byID[maps[idx].ID] = idx
		included[idx] = true
	}

	children := make(map[int][]int)
	roots := make([]int, 0)
	for _, idx := range indices {
		m := maps[idx]
		parentIdx, ok := byID[m.ParentID]
		if m.ParentID <= 0 || !ok || parentIdx == idx || !included[parentIdx] {
			roots = append(roots, idx)
			continue
		}
		children[parentIdx] = append(children[parentIdx], idx)
	}
	sortRPGMapIndices(roots)
	for key := range children {
		sortRPGMapIndices(children[key])
	}

	visited := make(map[int]bool, len(indices))
	var addMap func(uintptr, int)
	addMap = func(parentItem uintptr, idx int) {
		if idx < 0 || idx >= len(maps) || visited[idx] {
			return
		}
		visited[idx] = true
		m := maps[idx]
		kids := children[idx]
		label := m.Name
		if m.ID == gameStart.MapID {
			label = "▶ " + label + "  [Punto iniziale]"
		}
		item := treeInsert(parentItem, label, mapTreeMapIcon, len(kids) > 0, mapTreeNode{
			Kind:     "map",
			MapIndex: idx,
			RegionID: m.RegionID,
			Path:     m.File,
		})
		if item == 0 {
			return
		}
		mapTreeItemByMapIndex[idx] = item
		for _, childIdx := range kids {
			addMap(item, childIdx)
		}
		if m.Expanded && len(kids) > 0 {
			treeExpand(item)
		}
	}

	for _, idx := range roots {
		addMap(parent, idx)
	}
	// Corrupt/cyclic MapInfos should never make maps disappear. Any map not
	// reached above is added at the branch root as a safe fallback.
	remaining := make([]int, 0)
	for _, idx := range indices {
		if !visited[idx] {
			remaining = append(remaining, idx)
		}
	}
	sortRPGMapIndices(remaining)
	for _, idx := range remaining {
		addMap(parent, idx)
	}
	return len(indices)
}

// mapTreeInsertPhysicalContents mirrors the real filesystem below a Maps
// directory. Folder nodes are real directories; map leaves are the real
// MapXXX.json files already indexed by the project loader.
func mapTreeInsertPhysicalContents(parent uintptr, dir, regionID string, includeMaps bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	dirs := make([]os.DirEntry, 0)
	for _, entry := range entries {
		if entry.IsDir() {
			dirs = append(dirs, entry)
		}
	}
	sort.Slice(dirs, func(i, j int) bool { return strings.ToLower(dirs[i].Name()) < strings.ToLower(dirs[j].Name()) })

	for _, entry := range dirs {
		childPath := filepath.Join(dir, entry.Name())
		// La cartella converted/maps/regions è un ramo strutturale gestito
		// separatamente. Nella vista delle mappe non assegnate non deve comparire
		// una seconda volta come normale sottocartella.
		if strings.TrimSpace(regionID) == "" {
			regionsRoot := canonicalRegionsRootPath()
			if regionsRoot != "" && strings.EqualFold(filepath.Clean(childPath), filepath.Clean(regionsRoot)) {
				continue
			}
		}
		child := treeInsert(parent, entry.Name(), mapTreeFolderIcon, true, mapTreeNode{Kind: "folder", RegionID: regionID, Path: childPath})
		mapTreeInsertPhysicalContents(child, childPath, regionID, includeMaps)
	}

	if !includeMaps {
		return
	}
	for _, idx := range directMapIndicesInFolder(dir, regionID) {
		m := maps[idx]
		item := treeInsert(parent, fmt.Sprintf("%03d  %s", m.ID, m.Name), mapTreeMapIcon, false, mapTreeNode{Kind: "map", MapIndex: idx, RegionID: m.RegionID, Path: m.File})
		if item != 0 {
			mapTreeItemByMapIndex[idx] = item
		}
	}
}

func insertRegionTree(parent uintptr, r RegionDefinition, forceExpand bool) uintptr {
	// La cartella fisica della regione rimane il contenitore principale, ma le
	// mappe al suo interno vengono mostrate con la gerarchia logica ParentID di
	// RPG Maker XP, non come una lista piatta di file.
	regionPath := filepath.Join(currentProject, filepath.FromSlash(r.MapsFolder))
	// In the tree prefer the real folder name (e.g. NOVEPELAGO), because that
	// is the canonical regional container the user sees on disk.
	label := strings.TrimSpace(filepath.Base(regionPath))
	if label == "" || label == "." {
		label = strings.TrimSpace(r.Name)
	}
	if label == "" {
		label = r.ID
	}
	regionItem := treeInsert(parent, label, mapTreeFolderIcon, true, mapTreeNode{Kind: "region", RegionID: r.ID, Path: regionPath, ManagedRoot: true})
	if regionItem == 0 {
		return 0
	}
	insertRPGMapHierarchy(regionItem, r.ID, regionPath)
	if forceExpand || mapTreeRegionIsExpanded(r.ID) {
		treeExpand(regionItem)
	}
	return regionItem
}

func projectMapRootsForTree() []string {
	if currentProject == "" {
		return nil
	}
	candidates := append([]string{}, configuredProjectMapFolders(currentProject)...)
	for _, rel := range []string{filepath.Join("converted", "maps"), filepath.Join("converted", "Maps"), "maps", "Maps", filepath.Join("Data", "maps"), filepath.Join("data", "maps")} {
		candidates = append(candidates, filepath.Join(currentProject, rel))
	}
	seen := map[string]bool{}
	out := make([]string, 0)
	for _, p := range candidates {
		p = filepath.Clean(p)
		if !isDir(p) || regionForMapPath(currentProject, p) != "" {
			continue
		}
		key := strings.ToLower(p)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, p)
	}
	if len(out) == 0 {
		p := primaryProjectMapsPath()
		if p != "" {
			_ = os.MkdirAll(p, 0755)
			out = append(out, p)
		}
	}
	return out
}

func insertUnassignedMapRoots(parent uintptr) {
	roots := projectMapRootsForTree()
	if len(roots) == 0 {
		return
	}
	for _, rootPath := range roots {
		count := len(rpgTreeMapIndices("", rootPath))
		if count == 0 {
			continue
		}
		item := treeInsert(parent, "MAPPE PRINCIPALI", mapTreeFolderIcon, true, mapTreeNode{Kind: "unassigned_maps", Path: rootPath, ManagedRoot: true})
		insertRPGMapHierarchy(item, "", rootPath)
		treeExpand(item)
	}
}

func insertSystemAnimationMaps(parent uintptr) {
	if mode != "events" || len(gameStart.IntroMapIDs) == 0 {
		return
	}
	group := treeInsert(parent, "EVENTI / ANIMAZIONI", mapTreeFolderIcon, true, mapTreeNode{Kind: "system_events", ManagedRoot: true})
	if group == 0 {
		return
	}
	for _, id := range gameStart.IntroMapIDs {
		idx := mapIndexByID(id)
		if idx < 0 || idx >= len(maps) {
			continue
		}
		m := maps[idx]
		label := m.Name
		if strings.TrimSpace(label) == "" {
			label = fmt.Sprintf("Map%03d", m.ID)
		}
		label += "  [animazione]"
		item := treeInsert(group, label, mapTreeMapIcon, false, mapTreeNode{Kind: "map", MapIndex: idx, RegionID: m.RegionID, Path: m.File})
		if item != 0 {
			mapTreeItemByMapIndex[idx] = item
		}
	}
	treeExpand(group)
}

func insertCanonicalConvertedMapTree(parent uintptr, filter string) bool {
	mapsPath := canonicalConvertedMapsPath()
	regionsPath := canonicalRegionsRootPath()
	if mapsPath == "" {
		return false
	}
	if !isDir(mapsPath) && len(projectRegions.Regions) == 0 {
		return false
	}

	// Vista tipo RPG Maker XP: il progetto è la radice, le regioni sono cartelle
	// principali e dentro ciascuna cartella le mappe seguono ParentID/Order.
	// I dettagli fisici converted/maps/regions restano nel modello dati ma non
	// sporcano la navigazione quotidiana dell'editor.
	if filter == mapRegionFilterAll || filter == mapRegionFilterUnassigned {
		if isDir(mapsPath) {
			insertUnassignedMapRoots(parent)
		}
	}

	if filter != mapRegionFilterUnassigned && (len(projectRegions.Regions) > 0 || isDir(regionsPath)) {
		if filter == mapRegionFilterAll {
			for _, r := range projectRegions.Regions {
				insertRegionTree(parent, r, false)
			}
		} else if r := regionByID(filter); r != nil {
			insertRegionTree(parent, *r, true)
		}
	}
	return true
}

func refreshMapListUI() {
	if hwndMapList == 0 {
		return
	}
	started := time.Now()
	// Con molti nodi il controllo TreeView può ridisegnarsi diverse volte
	// durante Delete/Insert, mostrando temporaneamente un albero incompleto.
	// Costruiamo tutto a redraw sospeso e mostriamo il risultato solo alla fine.
	pSendMessageW.Call(uintptr(hwndMapList), wmSetRedraw, 0, 0)
	defer func() {
		pSendMessageW.Call(uintptr(hwndMapList), wmSetRedraw, 1, 0)
		invalidate(hwndMapList)
		mapLogf("[TREE] rebuild completed in %s nodes=%d", time.Since(started).Round(time.Millisecond), len(mapTreeItemByMapIndex))
	}()
	expandedPaths := captureMapTreeExpandedPaths()
	clearMapTreeUI()
	if currentProject == "" {
		showMapTreeMessage("Apri un progetto per visualizzare le mappe")
		return
	}

	root := treeInsert(tviRoot(), projectTreeRootName(), mapTreeFolderIcon, true, mapTreeNode{Kind: "project", Path: currentProject, ManagedRoot: true})
	if root == 0 {
		return
	}

	insertSystemAnimationMaps(root)

	// Per i progetti Python convertiti la Vista Mappa rispecchia esattamente
	// la nuova gerarchia fisica: progetto -> converted -> maps -> regions ->
	// <REGIONE>. I progetti legacy mantengono il fallback precedente.
	if !insertCanonicalConvertedMapTree(root, mapRegionFilter) {
		switch mapRegionFilter {
		case mapRegionFilterUnassigned:
			insertUnassignedMapRoots(root)

		case mapRegionFilterAll:
			insertUnassignedMapRoots(root)
			if len(projectRegions.Regions) > 0 {
				regionsPath := canonicalRegionsRootPath()
				regionsFolder := treeInsert(root, "regions", mapTreeFolderIcon, true, mapTreeNode{Kind: "regions", Path: regionsPath, ManagedRoot: true})
				for _, r := range projectRegions.Regions {
					insertRegionTree(regionsFolder, r, false)
				}
				treeExpand(regionsFolder)
			}

		default:
			if r := regionByID(mapRegionFilter); r != nil {
				insertRegionTree(root, *r, true)
			} else {
				treeInsert(root, "[Regione non trovata]", mapTreeMapIcon, false, mapTreeNode{Kind: "message"})
			}
		}
	}

	treeExpand(root)
	restoreMapTreeExpandedPaths(expandedPaths)
	if currentMap != nil {
		selectMapListIndexForMap(currentMapIndex())
	}
	resetMapTreeHorizontalScroll()
	mapLogf("[TREE] rebuilt filter=%s maps=%d mapNodes=%d expanded=%d", mapRegionFilter, len(maps), len(mapTreeItemByMapIndex), len(expandedPaths))
}

func selectMapListIndexForMap(mapIndex int) bool {
	if hwndMapList == 0 || mapIndex < 0 {
		return false
	}
	if item := mapTreeItemByMapIndex[mapIndex]; item != 0 {
		treeSelect(item)
		return true
	}
	return false
}

func updateMapQuickInfo() {
	if hwndMapQuickInfo == 0 {
		return
	}
	if currentMap == nil {
		setText(hwndMapQuickInfo, "Nessuna mappa selezionata")
		return
	}
	region := mapRegionDisplayName(currentMap.RegionID)
	setText(hwndMapQuickInfo, fmt.Sprintf("Nome: %s\r\nID mappa: %03d\r\nRegione: %s\r\nDimensioni: %dx%d", currentMap.Name, currentMap.ID, region, currentMapW, currentMapH))
}
