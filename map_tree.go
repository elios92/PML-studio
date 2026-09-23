//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

const (
	wmNotify = 0x004E
	// Deferred TreeView rebuild. Rebuilding the map tree while another Win32
	// notification is still being handled can leave the native control visually
	// empty until the region combo is changed by hand. Post the rebuild to the
	// main message queue instead.
	wmAppRefreshMapTree = 0x8003

	iccTreeviewClasses = 0x00000002

	tvsHasButtons    = 0x0001
	tvsHasLines      = 0x0002
	tvsLinesAtRoot   = 0x0004
	tvsEditLabels    = 0x0008
	tvsShowSelAlways = 0x0020
	tvsNoHScroll     = 0x8000

	tvFirst          = 0x1100
	tvmDeleteItem    = tvFirst + 1
	tvmExpand        = tvFirst + 2
	tvmSetImageList  = tvFirst + 9
	tvmGetNextItem   = tvFirst + 10
	tvmSelectItem    = tvFirst + 11
	tvmEnsureVisible = tvFirst + 20
	tvmInsertItemW   = tvFirst + 50
	tvmEditLabelW    = tvFirst + 65
	tvmGetItemState  = tvFirst + 39

	tvgnCaret    = 0x0009
	tveCollapse  = 0x0001
	tveExpand    = 0x0002
	tvisExpanded = 0x0020

	wmSetRedraw = 0x000B
	wmHScroll   = 0x0114
	sbLeft      = 6

	tvifText          = 0x0001
	tvifImage         = 0x0002
	tvifSelectedImage = 0x0020
	tvifChildren      = 0x0040

	tvsilNormal = 0

	// Common-control notifications are negative values in the Win32 headers.
	tvnFirst         = -400
	tvnSelChangedW   = tvnFirst - 51
	tvnEndLabelEditW = tvnFirst - 60

	shgfiSmallIcon         = 0x000000001
	shgfiSysIconIndex      = 0x000004000
	shgfiUseFileAttributes = 0x000000010
	fileAttributeDirectory = 0x00000010
	fileAttributeNormal    = 0x00000080
)

var (
	comctl32 = syscall.NewLazyDLL("comctl32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")

	pInitCommonControlsEx = comctl32.NewProc("InitCommonControlsEx")
	pSHGetFileInfoW       = shell32.NewProc("SHGetFileInfoW")

	mapTreeImageList      uintptr
	mapTreeFolderIcon     int32
	mapTreeMapIcon        int32
	mapTreeSelectionSync  bool
	mapTreeNodes          = map[uintptr]mapTreeNode{}
	mapTreeItemByMapIndex = map[int]uintptr{}
	mapTreeItemByPath     = map[string]uintptr{}
	mapTreeRefreshPosted  bool
)

type initCommonControlsEx struct {
	DwSize uint32
	DwICC  uint32
}

type nmhdr struct {
	HwndFrom syscall.Handle
	IDFrom   uintptr
	Code     int32
}

type tvItemW struct {
	Mask           uint32
	HItem          uintptr
	State          uint32
	StateMask      uint32
	PszText        *uint16
	CchTextMax     int32
	IImage         int32
	ISelectedImage int32
	CChildren      int32
	LParam         uintptr
}

type tvInsertStructW struct {
	HParent      uintptr
	HInsertAfter uintptr
	Item         tvItemW
}

type nmTvDispInfoW struct {
	Hdr  nmhdr
	Item tvItemW
}

type shFileInfoW struct {
	HIcon       syscall.Handle
	IIcon       int32
	Attributes  uint32
	DisplayName [260]uint16
	TypeName    [80]uint16
}

type mapTreeNode struct {
	Kind        string
	MapIndex    int
	RegionID    string
	Path        string
	ManagedRoot bool
}

func tviRoot() uintptr { return ^uintptr(0xFFFF) } // (HTREEITEM)-0x10000
func tviLast() uintptr { return ^uintptr(0xFFFD) } // (HTREEITEM)-0x0FFFE

func scheduleMapTreeRefresh() {
	if hwndMain == 0 || hwndMapList == 0 {
		return
	}
	if mapTreeRefreshPosted {
		return
	}
	mapTreeRefreshPosted = true
	pPostMessageW.Call(uintptr(hwndMain), uintptr(wmAppRefreshMapTree), 0, 0)
}

func handleDeferredMapTreeRefresh() {
	mapTreeRefreshPosted = false
	if hwndMapList == 0 {
		return
	}
	// A region may have been removed/repaired while the reload was running.
	// Never keep a stale filter that would make the tree look empty.
	if mapRegionFilter != mapRegionFilterAll && mapRegionFilter != mapRegionFilterUnassigned && regionByID(mapRegionFilter) == nil {
		mapRegionFilter = mapRegionFilterAll
		if hwndMapRegionCombo != 0 {
			pSendMessageW.Call(uintptr(hwndMapRegionCombo), CB_SETCURSEL, 0, 0)
		}
	}
	refreshMapListUI()
	mapLogf("[TREE] deferred refresh filter=%s maps=%d mapNodes=%d", mapRegionFilter, len(maps), len(mapTreeItemByMapIndex))
}

func ensureMapTreeCommonControls() {
	icc := initCommonControlsEx{DwSize: uint32(unsafe.Sizeof(initCommonControlsEx{})), DwICC: iccTreeviewClasses}
	pInitCommonControlsEx.Call(uintptr(unsafe.Pointer(&icc)))
}

func shellSystemIcon(path string, attrs uint32) (uintptr, int32) {
	var info shFileInfoW
	flags := uintptr(shgfiSysIconIndex | shgfiSmallIcon | shgfiUseFileAttributes)
	h, _, _ := pSHGetFileInfoW.Call(
		uintptr(unsafe.Pointer(wstr(path))),
		uintptr(attrs),
		uintptr(unsafe.Pointer(&info)),
		unsafe.Sizeof(info),
		flags,
	)
	return h, info.IIcon
}

func attachMapTreeSystemIcons() {
	if hwndMapList == 0 {
		return
	}
	himl, folderIdx := shellSystemIcon(`C:\PLM_FOLDER`, fileAttributeDirectory)
	_, mapIdx := shellSystemIcon(`C:\Map001.json`, fileAttributeNormal)
	if himl == 0 {
		return
	}
	mapTreeImageList = himl
	mapTreeFolderIcon = folderIdx
	mapTreeMapIcon = mapIdx
	pSendMessageW.Call(uintptr(hwndMapList), tvmSetImageList, tvsilNormal, himl)
}

func normalizeTreePath(p string) string {
	if strings.TrimSpace(p) == "" {
		return ""
	}
	abs, err := filepath.Abs(filepath.Clean(p))
	if err == nil {
		p = abs
	}
	return strings.ToLower(filepath.Clean(p))
}

func clearMapTreeUI() {
	mapTreeNodes = map[uintptr]mapTreeNode{}
	mapTreeItemByMapIndex = map[int]uintptr{}
	mapTreeItemByPath = map[string]uintptr{}
	if hwndMapList != 0 {
		pSendMessageW.Call(uintptr(hwndMapList), tvmDeleteItem, 0, tviRoot())
	}
}

func treeInsert(parent uintptr, text string, icon int32, hasChildren bool, meta mapTreeNode) uintptr {
	if hwndMapList == 0 {
		return 0
	}
	item := tvItemW{
		Mask:           tvifText | tvifChildren,
		PszText:        wstr(text),
		CChildren:      0,
		IImage:         icon,
		ISelectedImage: icon,
	}
	if hasChildren {
		item.CChildren = 1
	}
	if mapTreeImageList != 0 {
		item.Mask |= tvifImage | tvifSelectedImage
	}
	ins := tvInsertStructW{HParent: parent, HInsertAfter: tviLast(), Item: item}
	h, _, _ := pSendMessageW.Call(uintptr(hwndMapList), tvmInsertItemW, 0, uintptr(unsafe.Pointer(&ins)))
	if h != 0 {
		mapTreeNodes[h] = meta
		if meta.Path != "" {
			// Map files are indexed as well so expanded/collapsed RPG-style map
			// branches survive a UI refresh just like folder nodes.
			mapTreeItemByPath[normalizeTreePath(meta.Path)] = h
		}
	}
	return h
}

func treeExpand(item uintptr) {
	if hwndMapList != 0 && item != 0 {
		pSendMessageW.Call(uintptr(hwndMapList), tvmExpand, tveExpand, item)
	}
}

func treeCollapse(item uintptr) {
	if hwndMapList != 0 && item != 0 {
		pSendMessageW.Call(uintptr(hwndMapList), tvmExpand, tveCollapse, item)
	}
}

func resetMapTreeHorizontalScroll() {
	if hwndMapList == 0 {
		return
	}
	// TreeView_EnsureVisible scrolls both vertically and horizontally. When a
	// deeply nested regional map is selected, the native control can leave the
	// whole tree shifted to the right and visually cut the beginning of every
	// label. Keep the hierarchy anchored to the left; vertical visibility is
	// preserved by TVM_ENSUREVISIBLE.
	pSendMessageW.Call(uintptr(hwndMapList), wmHScroll, sbLeft, 0)
}

func treeSelect(item uintptr) {
	if hwndMapList == 0 || item == 0 {
		return
	}
	mapTreeSelectionSync = true
	pSendMessageW.Call(uintptr(hwndMapList), tvmSelectItem, tvgnCaret, item)
	pSendMessageW.Call(uintptr(hwndMapList), tvmEnsureVisible, 0, item)
	resetMapTreeHorizontalScroll()
	mapTreeSelectionSync = false
}

func captureMapTreeExpandedPaths() map[string]bool {
	out := map[string]bool{}
	if hwndMapList == 0 {
		return out
	}
	for path, item := range mapTreeItemByPath {
		if item == 0 {
			continue
		}
		state, _, _ := pSendMessageW.Call(uintptr(hwndMapList), tvmGetItemState, item, tvisExpanded)
		if state&tvisExpanded != 0 {
			out[path] = true
		}
	}
	return out
}

func restoreMapTreeExpandedPaths(expanded map[string]bool) {
	if hwndMapList == 0 || len(expanded) == 0 {
		return
	}
	for path := range expanded {
		if item := mapTreeItemByPath[path]; item != 0 {
			treeExpand(item)
		}
	}
	resetMapTreeHorizontalScroll()
}

func focusRegionFolderInTree(regionID string) {
	if currentProject == "" || strings.TrimSpace(regionID) == "" {
		return
	}
	for _, r := range projectRegions.Regions {
		item := mapTreeItemByPath[normalizeTreePath(filepath.Join(currentProject, filepath.FromSlash(r.RootFolder)))]
		if item == 0 {
			continue
		}
		if strings.EqualFold(r.ID, regionID) {
			treeExpand(item)
			treeSelect(item)
		} else {
			treeCollapse(item)
		}
	}
}

func showMapTreeMessage(message string) {
	clearMapTreeUI()
	if hwndMapList == 0 {
		return
	}
	root := treeInsert(tviRoot(), "PML Studio", mapTreeFolderIcon, true, mapTreeNode{Kind: "project"})
	treeInsert(root, message, mapTreeMapIcon, false, mapTreeNode{Kind: "message"})
	treeExpand(root)
}

func projectTreeRootName() string {
	if currentProject == "" {
		return "PML Studio"
	}
	if name := strings.TrimSpace(projectName(currentProject)); name != "" {
		return name
	}
	return filepath.Base(currentProject)
}

func mapTreeRegionIsExpanded(regionID string) bool {
	if mapRegionFilter != mapRegionFilterAll {
		return true
	}
	// In the global view only the active region opens automatically. Older
	// regions remain clearly visible as collapsed folders and can be expanded
	// manually to reveal their maps.
	return strings.EqualFold(projectRegions.ActiveRegionID, regionID)
}

func selectedMapTreeNode() (uintptr, mapTreeNode, bool) {
	if hwndMapList == 0 {
		return 0, mapTreeNode{}, false
	}
	item, _, _ := pSendMessageW.Call(uintptr(hwndMapList), tvmGetNextItem, tvgnCaret, 0)
	meta, ok := mapTreeNodes[item]
	return item, meta, ok
}

func utf16PtrString(p *uint16) string {
	if p == nil {
		return ""
	}
	buf := (*[4096]uint16)(unsafe.Pointer(p))
	n := 0
	for n < len(buf) && buf[n] != 0 {
		n++
	}
	return syscall.UTF16ToString(buf[:n])
}

func validWindowsFolderName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("il nome della cartella non può essere vuoto")
	}
	if name == "." || name == ".." {
		return fmt.Errorf("nome cartella non valido")
	}
	if strings.ContainsAny(name, `<>:"/\\|?*`) {
		return fmt.Errorf("il nome contiene caratteri non validi per Windows")
	}
	if strings.HasSuffix(name, ".") || strings.HasSuffix(name, " ") {
		return fmt.Errorf("il nome non può terminare con punto o spazio")
	}
	return nil
}

func pathInsideProject(path string) bool {
	if currentProject == "" || strings.TrimSpace(path) == "" {
		return false
	}
	root, err1 := filepath.Abs(currentProject)
	target, err2 := filepath.Abs(path)
	if err1 != nil || err2 != nil {
		return false
	}
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func regionMapsPath(regionID string) string {
	if r := regionByID(regionID); r != nil {
		return filepath.Join(currentProject, filepath.FromSlash(r.MapsFolder))
	}
	return ""
}

func primaryProjectMapsPath() string {
	if currentProject == "" {
		return ""
	}
	for _, p := range configuredProjectMapFolders(currentProject) {
		// Region roots are already included in the manifest. For the primary
		// project target prefer a folder that is not owned by a region.
		if regionForMapPath(currentProject, p) == "" {
			return p
		}
	}
	for _, candidate := range []string{"maps", "Maps", filepath.Join("converted", "maps"), filepath.Join("converted", "Maps")} {
		p := filepath.Join(currentProject, candidate)
		if isDir(p) {
			return p
		}
	}
	return filepath.Join(currentProject, "maps")
}

func selectedMapFolderTarget() string {
	_, meta, ok := selectedMapTreeNode()
	if ok {
		switch meta.Kind {
		case "folder", "region_maps", "region_scripts", "region_data", "unassigned_maps":
			if meta.Path != "" {
				return meta.Path
			}
		case "map":
			if meta.MapIndex >= 0 && meta.MapIndex < len(maps) && maps[meta.MapIndex].File != "" {
				return filepath.Dir(maps[meta.MapIndex].File)
			}
		case "region":
			if p := regionMapsPath(meta.RegionID); p != "" {
				return p
			}
		case "project":
			return primaryProjectMapsPath()
		}
	}
	if mapRegionFilter != mapRegionFilterAll && mapRegionFilter != mapRegionFilterUnassigned {
		if p := regionMapsPath(mapRegionFilter); p != "" {
			return p
		}
	}
	if r := activeRegion(); projectRegions.Enabled && r != nil {
		if p := regionMapsPath(r.ID); p != "" {
			return p
		}
	}
	return primaryProjectMapsPath()
}

func pathWithinRoot(root, path string) bool {
	root, err1 := filepath.Abs(filepath.Clean(root))
	path, err2 := filepath.Abs(filepath.Clean(path))
	if err1 != nil || err2 != nil {
		return false
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func ensureScriptPackageForFolder(path string) {
	if currentProject == "" {
		return
	}
	for _, r := range projectRegions.Regions {
		scriptsRoot := filepath.Join(currentProject, filepath.FromSlash(r.ScriptsFolder))
		if pathWithinRoot(scriptsRoot, path) {
			initPath := filepath.Join(path, "__init__.py")
			if !exists(initPath) {
				_ = os.WriteFile(initPath, []byte("# Generated by PML Studio.\n"), 0644)
			}
			return
		}
	}
}

func uniqueNewFolderPath(parent string) string {
	base := filepath.Join(parent, "Nuova cartella")
	if !exists(base) {
		return base
	}
	for i := 2; i < 10000; i++ {
		candidate := filepath.Join(parent, fmt.Sprintf("Nuova cartella (%d)", i))
		if !exists(candidate) {
			return candidate
		}
	}
	return filepath.Join(parent, "Nuova cartella - PLM")
}

func createFolderFromMapTree() {
	if currentProject == "" {
		msgbox("PLM Studio", "Apri prima un progetto.", MB_OK|MB_ICONINFORMATION)
		return
	}
	parent := filepath.Clean(selectedMapFolderTarget())
	if parent == "" || !pathInsideProject(parent) {
		msgbox("PLM Studio", "Seleziona una cartella valida dell'albero mappe.", MB_OK|MB_ICONERROR)
		return
	}
	if err := os.MkdirAll(parent, 0755); err != nil {
		msgbox("PLM Studio", "Impossibile preparare la cartella di destinazione:\r\n\r\n"+err.Error(), MB_OK|MB_ICONERROR)
		return
	}
	newPath := uniqueNewFolderPath(parent)
	if err := os.Mkdir(newPath, 0755); err != nil {
		msgbox("PLM Studio", "Impossibile creare la cartella:\r\n\r\n"+err.Error(), MB_OK|MB_ICONERROR)
		return
	}
	ensureScriptPackageForFolder(newPath)
	refreshMapListUI()
	item := mapTreeItemByPath[normalizeTreePath(newPath)]
	if item != 0 {
		treeSelect(item)
		pSetFocus.Call(uintptr(hwndMapList))
		pSendMessageW.Call(uintptr(hwndMapList), tvmEditLabelW, 0, item)
	}
	setToolbarStatus("Cartella creata: " + filepath.Base(newPath) + " — scrivi il nuovo nome e premi Invio.")
}

func renameSelectedMapTreeFolder() {
	item, meta, ok := selectedMapTreeNode()
	if !ok || meta.Kind != "folder" || meta.Path == "" {
		msgbox("PLM Studio", "Seleziona una sottocartella reale da rinominare. Le cartelle strutturali Maps/Scripts/Data e le regioni si gestiscono dai rispettivi strumenti.", MB_OK|MB_ICONINFORMATION)
		return
	}
	if meta.ManagedRoot {
		msgbox("PLM Studio", "Questa è una cartella strutturale del progetto e non può essere rinominata da qui.", MB_OK|MB_ICONINFORMATION)
		return
	}
	pSetFocus.Call(uintptr(hwndMapList))
	pSendMessageW.Call(uintptr(hwndMapList), tvmEditLabelW, 0, item)
}

func folderIsEmpty(path string) (bool, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return false, err
	}
	if len(entries) == 0 {
		return true, nil
	}
	// A newly-created Scripts folder contains only the package marker generated
	// by PML Studio. Treat that marker as empty content for safe deletion.
	if len(entries) == 1 && strings.EqualFold(entries[0].Name(), "__init__.py") && !entries[0].IsDir() {
		b, err := os.ReadFile(filepath.Join(path, entries[0].Name()))
		if err == nil && strings.TrimSpace(string(b)) == "# Generated by PML Studio." {
			return true, nil
		}
	}
	return false, nil
}

func deleteSelectedMapTreeFolder() {
	_, meta, ok := selectedMapTreeNode()
	if !ok || meta.Kind != "folder" || meta.Path == "" || meta.ManagedRoot {
		msgbox("PLM Studio", "Seleziona una sottocartella reale e non strutturale da eliminare.", MB_OK|MB_ICONINFORMATION)
		return
	}
	empty, err := folderIsEmpty(meta.Path)
	if err != nil {
		msgbox("PLM Studio", "Impossibile leggere la cartella:\r\n\r\n"+err.Error(), MB_OK|MB_ICONERROR)
		return
	}
	if !empty {
		msgbox("PLM Studio", "Per sicurezza PML Studio elimina solo cartelle vuote. Sposta o elimina prima il contenuto della cartella.", MB_OK|MB_ICONINFORMATION)
		return
	}
	if msgboxResult("PLM Studio", "Eliminare la cartella vuota \""+filepath.Base(meta.Path)+"\"?", MB_YESNOCANCEL|MB_ICONINFORMATION) != IDYES {
		return
	}
	initPath := filepath.Join(meta.Path, "__init__.py")
	if b, err := os.ReadFile(initPath); err == nil && strings.TrimSpace(string(b)) == "# Generated by PML Studio." {
		_ = os.Remove(initPath)
	}
	if err := os.Remove(meta.Path); err != nil {
		msgbox("PLM Studio", "Impossibile eliminare la cartella:\r\n\r\n"+err.Error(), MB_OK|MB_ICONERROR)
		return
	}
	refreshMapListUI()
	setToolbarStatus("Cartella eliminata.")
}

func reloadMapsAfterFilesystemChange(preserveMapID int) {
	if currentProject == "" {
		return
	}
	rebuildMapFileIndex(currentProject)
	maps = loadMaps(currentProject)
	refreshMapEditorRegionControls()
	if preserveMapID <= 0 {
		refreshMapListUI()
		return
	}
	idx := -1
	for i := range maps {
		if maps[i].ID == preserveMapID {
			idx = i
			break
		}
	}
	if idx < 0 {
		currentMap = nil
		currentMapDoc = nil
		updateMapQuickInfo()
		refreshMapListUI()
		return
	}
	currentMap = &maps[idx]
	if currentMapDoc != nil {
		currentMapDoc.Path = currentMap.File
	}
	refreshMapListUI()
	selectMapListIndexForMap(idx)
	updateMapQuickInfo()
}

func applyFolderRename(meta mapTreeNode, newName string) error {
	if meta.Kind != "folder" || meta.Path == "" || meta.ManagedRoot {
		return fmt.Errorf("questa cartella non può essere rinominata")
	}
	newName = strings.TrimSpace(newName)
	if err := validWindowsFolderName(newName); err != nil {
		return err
	}
	oldPath := filepath.Clean(meta.Path)
	if !pathInsideProject(oldPath) {
		return fmt.Errorf("percorso fuori dal progetto")
	}
	if strings.EqualFold(filepath.Base(oldPath), newName) {
		return nil
	}
	newPath := filepath.Join(filepath.Dir(oldPath), newName)
	if !pathInsideProject(newPath) {
		return fmt.Errorf("destinazione non valida")
	}
	if exists(newPath) {
		return fmt.Errorf("esiste già una cartella o un file con questo nome")
	}
	preserveID := 0
	if currentMap != nil {
		preserveID = currentMap.ID
		currentFile := currentMap.File
		if currentMapDoc != nil && currentMapDoc.Path != "" {
			currentFile = currentMapDoc.Path
		}
		if currentFile != "" {
			rel, err := filepath.Rel(oldPath, currentFile)
			if err == nil && (rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))) {
				if !confirmMapChanges() {
					return fmt.Errorf("operazione annullata")
				}
			}
		}
	}
	if err := os.Rename(oldPath, newPath); err != nil {
		return err
	}
	reloadMapsAfterFilesystemChange(preserveID)
	if item := mapTreeItemByPath[normalizeTreePath(newPath)]; item != 0 {
		treeSelect(item)
	}
	setToolbarStatus("Cartella rinominata: " + newName)
	return nil
}

// handleMapTreeNotify returns (handled, result). TVN_ENDLABELEDIT requires a
// TRUE result on success; selection changes return 0 as usual.
func handleMapTreeNotify(l uintptr) (bool, uintptr) {
	if l == 0 || hwndMapList == 0 {
		return false, 0
	}
	hdr := (*nmhdr)(unsafe.Pointer(l))
	if hdr.HwndFrom != hwndMapList || hdr.IDFrom != 1101 {
		return false, 0
	}

	switch hdr.Code {
	case tvnSelChangedW:
		if mapTreeSelectionSync {
			return true, 0
		}
		item, _, _ := pSendMessageW.Call(uintptr(hwndMapList), tvmGetNextItem, tvgnCaret, 0)
		meta, ok := mapTreeNodes[item]
		if !ok {
			return true, 0
		}
		switch meta.Kind {
		case "map":
			if meta.MapIndex >= 0 && meta.MapIndex < len(maps) {
				selectMap(meta.MapIndex)
			}
		case "region", "region_maps":
			if meta.RegionID != "" && regionByID(meta.RegionID) != nil && !strings.EqualFold(projectRegions.ActiveRegionID, meta.RegionID) {
				projectRegions.ActiveRegionID = meta.RegionID
				_ = saveRegionRegistry()
				refreshRegionControlsNoTree()
				setToolbarStatus("Regione attiva: " + mapRegionDisplayName(meta.RegionID))
			}
		case "folder", "region_scripts", "region_data", "unassigned_maps":
			if meta.Path != "" {
				setToolbarStatus("Cartella: " + meta.Path)
			}
		}
		return true, 0

	case tvnEndLabelEditW:
		info := (*nmTvDispInfoW)(unsafe.Pointer(l))
		if info.Item.PszText == nil {
			return true, 0 // editing cancelled
		}
		meta, ok := mapTreeNodes[info.Item.HItem]
		if !ok {
			return true, 0
		}
		newName := utf16PtrString(info.Item.PszText)
		if err := applyFolderRename(meta, newName); err != nil {
			if err.Error() != "operazione annullata" {
				msgbox("PLM Studio", "Impossibile rinominare la cartella:\r\n\r\n"+err.Error(), MB_OK|MB_ICONERROR)
			}
			return true, 0
		}
		return true, 1
	}
	return false, 0
}
