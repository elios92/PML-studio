//go:build windows

package main

import (
	"fmt"
	"path/filepath"
	"syscall"
	"unsafe"
)

const (
	idNewProject    = 1000
	idOpenProject   = 1001
	idRecentProject = 1002
	idSaveProject   = 1003
	idMapFolder     = 1004
	idNewMap        = 1005

	idViewMap         = 1010
	idViewPermissions = 1011
	idViewEvents      = 1012
	idViewEncounters  = 1013
	idViewHeader      = 1014
	idViewConnections = 1015
	idViewAnimations  = 1016
	idViewDatabase    = 1017

	idTilesetCombo = 1401
	idZoomCombo    = 1402
	idGridToggle   = 1403
	idLevelCombo   = 1404

	idPermModeMovement = 1800
	idPermModeTerrain  = 1801
	idPermMovementBase = 1810
	idPermTerrainBase  = 1830

	idMenuNew                = 2000
	idMenuOpen               = 2001
	idMenuRecent             = 2002
	idMenuSave               = 2003
	idMenuExit               = 2004
	idMenuUIAppearance       = 2005
	idMenuProjectFolder      = 2010
	idMenuProjectMap         = 2011
	idMenuRenameFolder       = 2012
	idMenuDeleteFolder       = 2013
	idMenuToolsMap           = 2020
	idMenuToolsPermissions   = 2021
	idMenuToolsEvents        = 2022
	idMenuToolsRepairMaps    = 2023
	idMenuToolsConnections   = 2024
	idMenuToolsInsertPalette = 2025
	idMenuToolsGameStart     = 2026
	idMenuHelpAbout          = 2030
)

func createMainMenu(hwnd syscall.Handle) {
	oldMenu, _, _ := user32.NewProc("GetMenu").Call(uintptr(hwnd))
	bar, _, _ := pCreateMenu.Call()
	fileMenu, _, _ := pCreatePopupMenu.Call()
	settingsMenu, _, _ := pCreatePopupMenu.Call()
	toolsMenu, _, _ := pCreatePopupMenu.Call()
	helpMenu, _, _ := pCreatePopupMenu.Call()

	appendMenu(syscall.Handle(fileMenu), MF_STRING, idMenuNew, "Nuovo progetto")
	appendMenu(syscall.Handle(fileMenu), MF_STRING, idMenuOpen, "Apri progetto...")
	appendMenu(syscall.Handle(fileMenu), MF_STRING, idMenuRecent, "Riapri ultimo")
	appendMenu(syscall.Handle(fileMenu), MF_SEPARATOR, 0, "")
	appendMenu(syscall.Handle(fileMenu), MF_STRING, idMenuSave, "Salva")
	appendMenu(syscall.Handle(fileMenu), MF_SEPARATOR, 0, "")
	appendMenu(syscall.Handle(fileMenu), MF_STRING, idMenuExit, "Esci")

	populateSettingsMenu(syscall.Handle(settingsMenu))
	appendMenu(syscall.Handle(toolsMenu), MF_STRING, idMenuProjectFolder, "Nuova sottocartella")
	appendMenu(syscall.Handle(toolsMenu), MF_STRING, idMenuRenameFolder, "Rinomina cartella selezionata")
	appendMenu(syscall.Handle(toolsMenu), MF_STRING, idMenuDeleteFolder, "Elimina cartella selezionata")
	appendMenu(syscall.Handle(toolsMenu), MF_STRING, idMenuProjectMap, "Nuova mappa")
	appendMenu(syscall.Handle(toolsMenu), MF_SEPARATOR, 0, "")
	appendMenu(syscall.Handle(toolsMenu), MF_STRING, idMenuToolsMap, "Vista mappa")
	appendMenu(syscall.Handle(toolsMenu), MF_STRING, idMenuToolsPermissions, "Vista movimenti permessi")
	appendMenu(syscall.Handle(toolsMenu), MF_STRING, idMenuToolsEvents, "Vista eventi")
	appendMenu(syscall.Handle(toolsMenu), MF_STRING, idMenuToolsConnections, "Vista connessioni")
	appendMenu(syscall.Handle(toolsMenu), MF_STRING, idMenuToolsInsertPalette, "Inserisci palette...")
	appendMenu(syscall.Handle(toolsMenu), MF_STRING, idMenuToolsGameStart, "Punto iniziale del gioco...")
	appendMenu(syscall.Handle(toolsMenu), MF_SEPARATOR, 0, "")
	appendMenu(syscall.Handle(toolsMenu), MF_STRING, idPermModeMovement, "Permessi: Movimenti")
	appendMenu(syscall.Handle(toolsMenu), MF_STRING, idPermModeTerrain, "Permessi: Terrain Tags")
	appendMenu(syscall.Handle(toolsMenu), MF_SEPARATOR, 0, "")
	appendMenu(syscall.Handle(toolsMenu), MF_STRING, idMenuToolsRepairMaps, "Repair Duplicate Map IDs")

	appendMenu(syscall.Handle(helpMenu), MF_STRING, idMenuHelpAbout, "Informazioni")

	appendPopup(syscall.Handle(bar), syscall.Handle(fileMenu), "File")
	appendPopup(syscall.Handle(bar), syscall.Handle(settingsMenu), "Impostazioni")
	appendPopup(syscall.Handle(bar), syscall.Handle(toolsMenu), "Strumenti")
	appendPopup(syscall.Handle(bar), syscall.Handle(helpMenu), "Aiuto")
	pSetMenu.Call(uintptr(hwnd), bar)
	if oldMenu != 0 {
		pDestroyMenu.Call(oldMenu)
	}
	pDrawMenuBar.Call(uintptr(hwnd))
}

func createMapToolControls(hInst syscall.Handle) {
	hwndTopMapBar = createWindow("STATIC", "", WS_CHILD|WS_VISIBLE, 0, 0, 0, 0, hwndMain, 1400, hInst)
	hwndTilesetLabel = createWindow("STATIC", "Tileset", WS_CHILD|WS_VISIBLE, 0, 0, 0, 0, hwndMain, 1410, hInst)
	hwndTilesetCombo = createWindow("COMBOBOX", "", WS_CHILD|WS_VISIBLE|WS_TABSTOP|CBS_DROPDOWNLIST, 0, 0, 0, 120, hwndMain, idTilesetCombo, hInst)
	comboAdd(hwndTilesetCombo, "Tileset progetto")
	pSendMessageW.Call(uintptr(hwndTilesetCombo), CB_SETCURSEL, 0, 0)
	hwndZoomLabel = createWindow("STATIC", "Zoom", WS_CHILD|WS_VISIBLE, 0, 0, 0, 0, hwndMain, 1411, hInst)
	hwndZoomCombo = createWindow("COMBOBOX", "", WS_CHILD|WS_VISIBLE|WS_TABSTOP|CBS_DROPDOWNLIST, 0, 0, 0, 120, hwndMain, idZoomCombo, hInst)
	for _, s := range []string{"Zoom 1/1", "Zoom 1/2", "Zoom 1/4"} {
		comboAdd(hwndZoomCombo, s)
	}
	pSendMessageW.Call(uintptr(hwndZoomCombo), CB_SETCURSEL, 0, 0)
	hwndGridToggle = createWindow("BUTTON", "Griglia ON", WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_PUSHBUTTON|BS_FLAT, 0, 0, 0, 0, hwndMain, idGridToggle, hInst)
	hwndLevelLabel = createWindow("STATIC", "Livello", WS_CHILD|WS_VISIBLE, 0, 0, 0, 0, hwndMain, 1412, hInst)
	hwndLevelCombo = createWindow("COMBOBOX", "", WS_CHILD|WS_VISIBLE|WS_TABSTOP|CBS_DROPDOWNLIST, 0, 0, 0, 120, hwndMain, idLevelCombo, hInst)
	for _, s := range []string{"Livello 1", "Livello 2", "Livello 3"} {
		comboAdd(hwndLevelCombo, s)
	}
	pSendMessageW.Call(uintptr(hwndLevelCombo), CB_SETCURSEL, 0, 0)
}

func createEditorControls(hInst syscall.Handle) {
	hwndCanvas = createWindow("PLMStudioCanvas03", "", WS_CHILD|WS_VISIBLE|WS_BORDER|WS_HSCROLL|WS_VSCROLL, 0, 0, 0, 0, hwndMain, 1199, hInst)

	// Vista permessi in stile Advance Map: la mappa resta al centro e la
	// colonna destra mantiene due modalita' distinte. I pulsanti non
	// cambiano il layout generale: commutano soltanto la lista sottostante.
	hwndPermModeMovement = createWindow("BUTTON", "Movimenti permessi", WS_CHILD|WS_TABSTOP|WS_GROUP|BS_AUTORADIOBUTTON|BS_PUSHLIKE, 0, 0, 0, 0, hwndMain, idPermModeMovement, hInst)
	hwndPermModeTerrain = createWindow("BUTTON", "Terrain Tags", WS_CHILD|WS_TABSTOP|BS_AUTORADIOBUTTON|BS_PUSHLIKE, 0, 0, 0, 0, hwndMain, idPermModeTerrain, hInst)
	hwndPermSelected = 0
	hwndPermHelp = 0
	hwndPermissionsPalette = createWindow("PLMStudioPalette03", "", WS_CHILD|WS_BORDER|WS_VSCROLL, 0, 0, 0, 0, hwndMain, 1804, hInst)
	// Il comportamento e' visibile direttamente nella riga della palette;
	// niente popup sopra la mappa.
	hwndPermBehaviorHint = 0
	hwndMovementButtons = nil
	hwndTerrainButtons = nil

	hwndEventInfo = createWindow("STATIC", "", WS_CHILD, 0, 0, 0, 0, hwndMain, 1220, hInst)
	hwndEventName = createWindow("EDIT", "", WS_CHILD|WS_BORDER|WS_TABSTOP, 0, 0, 0, 0, hwndMain, 1221, hInst)
	hwndEventTrigger = createWindow("COMBOBOX", "", WS_CHILD|WS_TABSTOP|CBS_DROPDOWNLIST, 0, 0, 0, 150, hwndMain, 1222, hInst)
	for _, s := range triggerNames() {
		comboAdd(hwndEventTrigger, s)
	}
	pSendMessageW.Call(uintptr(hwndEventTrigger), CB_SETCURSEL, 0, 0)
	hwndEventMove = createWindow("COMBOBOX", "", WS_CHILD|WS_TABSTOP|CBS_DROPDOWNLIST, 0, 0, 0, 150, hwndMain, 1223, hInst)
	for _, s := range moveNames() {
		comboAdd(hwndEventMove, s)
	}
	pSendMessageW.Call(uintptr(hwndEventMove), CB_SETCURSEL, 0, 0)
	hwndEventDialog = createWindow("EDIT", "", WS_CHILD|WS_BORDER|WS_TABSTOP|ES_MULTILINE|ES_AUTOVSCROLL|ES_WANTRETURN, 0, 0, 0, 0, hwndMain, 1224, hInst)
	hwndAddEvent = createWindow("BUTTON", "+ Evento", WS_CHILD, 0, 0, 0, 0, hwndMain, 1210, hInst)
	hwndSaveEvent = createWindow("BUTTON", "Salva", WS_CHILD, 0, 0, 0, 0, hwndMain, 1211, hInst)
	hwndDeleteEvent = createWindow("BUTTON", "Elimina", WS_CHILD, 0, 0, 0, 0, hwndMain, 1212, hInst)
}

func createMainControls(hInst syscall.Handle) {
	createMainMenu(hwndMain)
	createToolbarControls(hInst)
	createViewTabs(hInst)
	createMapToolControls(hInst)
	createLeftSidebar(hInst)
	// Il contenitore destro deve essere creato prima dei controlli specifici
	// delle viste. Altrimenti lo STATIC dell'inspector puo' coprire i pulsanti
	// Movimenti/Terrain ed Eventi per via dello Z-order Win32.
	createRightSidebar(hInst)
	createEditorControls(hInst)
	createEncounterEditor(hInst)
	createHeaderEditor(hInst)
	createConnectionsEditor(hInst)
	createAnimationEditor(hInst)
	createDataEditor(hInst)
	// Separatori trascinabili creati per ultimi: restano sopra ai pannelli
	// laterali e permettono il resize senza cambiare il layout degli editor.
	createResizeSplitters(hInst)
	updateMapToolButtonStates()
	hwndStatus = createWindow("STATIC", "PML Studio pronto", WS_CHILD|WS_VISIBLE, 0, 0, 0, 0, hwndMain, 1300, hInst)
}

func wndProc(hwnd syscall.Handle, msg uint32, w, l uintptr) uintptr {
	diagUIHeartbeat("wndProc")
	switch msg {
	case wmAppDiagHeartbeat:
		diagAckHeartbeat(uint64(w))
		return 0
	case wmAppMapLoadProgress:
		handleMapLoadProgress(uint64(w), l)
		return 0
	case wmAppMapLoadDone:
		handleMapLoadDone(uint64(w))
		return 0
	case wmAppRefreshMapTree:
		handleDeferredMapTreeRefresh()
		return 0
	case wmNotify:
		if handled, result := themeButtonNotify(l); handled {
			return result
		}
		if handled, result := handleMapTreeNotify(l); handled {
			return result
		}
	case WM_CTLCOLORSTATIC, WM_CTLCOLOREDIT, WM_CTLCOLORLISTBOX, WM_CTLCOLORBTN:
		if brush := modernCtlColor(msg, w, syscall.Handle(l)); brush != 0 {
			return brush
		}
	case WM_ERASEBKGND:
		if themeBrushWindow != 0 {
			var rc RECT
			pGetClientRect.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&rc)))
			pFillRect.Call(w, uintptr(unsafe.Pointer(&rc)), uintptr(themeBrushWindow))
			return 1
		}
	case WM_PAINT:
		dn, ds := diagWMPaintStart("main")
		r, _, _ := pDefWindowProcW.Call(uintptr(hwnd), uintptr(msg), w, l)
		diagWMPaintEnd(dn, "main", ds)
		return r
	case WM_SIZE:
		dn, ds := diagWMSizeStart()
		layout(hwnd)
		diagWMSizeEnd(dn, ds)
		return 0
	case WM_MOUSEWHEEL:
		// La rotella deve seguire il pannello sotto il puntatore, non il
		// controllo che in quel momento possiede il focus della tastiera.
		if routePaletteMouseWheel(w, l) {
			return 0
		}
	case WM_HSCROLL:
		if handleAnimationEditorScroll(syscall.Handle(l)) {
			return 0
		}
	case WM_COMMAND:
		id := loword(w)
		notify := hiword(w)
		diagWMCommand(id, notify)
		if handleSettingsMenuCommand(id) {
			return 0
		}
		if handleEncounterCommand(id, notify) {
			return 0
		}
		if handleConnectionsCommand(id, notify) {
			return 0
		}
		if handleDataEditorCommand(id, notify) {
			return 0
		}
		if handleEventToolbarCommand(uintptr(id)) {
			return 0
		}
		switch id {
		case idNewProject, idMenuNew:
			newProject()
		case idOpenProject, idMenuOpen:
			openProject()
		case idRecentProject, idMenuRecent:
			reopen()
		case idSaveProject, idMenuSave:
			saveAll()
		case idMenuUIAppearance:
			showUISettingsDialog()
		case idMapFolder, idMenuProjectFolder:
			createFolderFromMapTree()
		case idMenuRenameFolder:
			renameSelectedMapTreeFolder()
		case idMenuDeleteFolder:
			deleteSelectedMapTreeFolder()
		case idNewMap, idMenuProjectMap:
			target := selectedMapFolderTarget()
			showCreateMapDialog(target)
		case idUndo:
			undoMapEdit()
		case idRedo:
			redoMapEdit()
		case idToolSelect:
			activeMapTool = ToolSelect
			updateMapToolButtonStates()
			updateMapEditorStatus()
		case idToolPencil:
			activeMapTool = ToolPencil
			updateMapToolButtonStates()
			updateMapEditorStatus()
		case idToolRectangle:
			activeMapTool = ToolRectangle
			updateMapToolButtonStates()
			updateMapEditorStatus()
		case idToolFill:
			activeMapTool = ToolFill
			updateMapToolButtonStates()
			updateMapEditorStatus()
		case idToolEyedropper:
			activeMapTool = ToolEyedropper
			updateMapToolButtonStates()
			updateMapEditorStatus()
		case idToolEraser:
			activeMapTool = ToolEraser
			updateMapToolButtonStates()
			updateMapEditorStatus()
		case idLayer1, idLayer2, idLayer3:
			activeLayer = int(id - idLayer1)
			updateMapToolButtonStates()
			pSendMessageW.Call(uintptr(hwndLevelCombo), CB_SETCURSEL, uintptr(activeLayer), 0)
			updateMapEditorStatus()
			invalidate(hwndCanvas)
		case idToolbarConn:
			showConnectionsManager()
		case idZoomOut:
			if mapZoomIndex < 2 {
				mapZoomIndex++
				pSendMessageW.Call(uintptr(hwndZoomCombo), CB_SETCURSEL, uintptr(mapZoomIndex), 0)
				mapScrollX, mapScrollY = 0, 0
				updateCanvasScrollbars(hwndCanvas)
				invalidate(hwndCanvas)
			}
		case idZoomIn:
			if mapZoomIndex > 0 {
				mapZoomIndex--
				pSendMessageW.Call(uintptr(hwndZoomCombo), CB_SETCURSEL, uintptr(mapZoomIndex), 0)
				mapScrollX, mapScrollY = 0, 0
				updateCanvasScrollbars(hwndCanvas)
				invalidate(hwndCanvas)
			}
		case idSearch:
			setToolbarStatus("Ricerca mappe: digita nel campo di ricerca della barra superiore.")
		case idPlaytest:
			if err := startPlaytest(); err != nil {
				msgbox("PML Studio", "Impossibile avviare il playtest:\r\n\r\n"+err.Error(), MB_OK|MB_ICONERROR)
				setToolbarStatus("Playtest: " + err.Error())
			} else if currentMap != nil {
				setToolbarStatus("Playtest avviato con le preferenze selezionate. Dati salvati e verificati.")
			}
		case idStopPlay:
			if err := stopPlaytest(); err != nil {
				setToolbarStatus("Stop playtest: " + err.Error())
			} else {
				setToolbarStatus("Playtest arrestato.")
			}
		case 1741, 1742, 1745, 1746, 1747: // Header EDIT controls
			if notify == EN_CHANGE && !headerRefreshing {
				headerDirty = true
			}
		case 1743, 1744: // Header COMBOBOX controls
			if notify == CBN_SELCHANGE && !headerRefreshing {
				headerDirty = true
			}
		case idFeatureChapters, idFeatureMainMissions, idFeatureSideQuests, idFeatureRequests, idFeatureSOS, idFeatureInvestigations, idFeatureMultiRegions:
			if notify == BN_CLICKED && !projectFeaturesRefreshing {
				projectFeaturesDirty = true
			}
		case idHeaderFeatureSave:
			if err := saveProjectFeatures(); err != nil {
				setToolbarStatus("Struttura gioco: errore salvataggio - " + err.Error())
			} else {
				setToolbarStatus("Struttura gioco e configurazione regioni salvate.")
			}
		case idRegionAdd:
			if err := createRegionFromHeader(); err != nil {
				msgbox("PLM Studio", "Impossibile creare la regione:\r\n\r\n"+err.Error(), MB_OK|MB_ICONERROR)
				setToolbarStatus("Regioni: errore - " + err.Error())
			} else {
				if r := activeRegion(); r != nil {
					setToolbarStatus("Regione pronta: " + filepath.Join(currentProject, filepath.FromSlash(r.RootFolder)))
					promptPopulateRegion(r.ID)
				} else {
					setToolbarStatus("Regione creata: cartelle Maps, Scripts e Data collegate al progetto.")
				}
			}
		case idRegionList:
			if notify == 1 { // CBN_SELCHANGE
				setActiveRegionFromCombo()
			}
		case idRegionFolderList:
			if notify == 1 { // CBN_SELCHANGE
				if label := selectedHeaderRegionDestinationLabel(); label != "" {
					setToolbarStatus("Cartella destinazione mappa: " + label)
				}
			}
		case idRegionAssign:
			if err := assignCurrentMapToActiveRegion(); err != nil {
				msgbox("PLM Studio", "Impossibile assegnare la mappa alla regione:\r\n\r\n"+err.Error(), MB_OK|MB_ICONERROR)
				setToolbarStatus("Regioni: errore spostamento mappa - " + err.Error())
			} else {
				label := selectedHeaderRegionDestinationLabel()
				if label == "" {
					label = "cartella selezionata"
				}
				setToolbarStatus("Mappa corrente spostata in " + label + "; Map ID invariato.")
			}
		case idRegionUnassign:
			if err := unassignCurrentMapToRoot(); err != nil {
				msgbox("PML Studio", "Impossibile spostare la mappa tra le NON ASSEGNATE:\r\n\r\n"+err.Error(), MB_OK|MB_ICONERROR)
				setToolbarStatus("Mappe: errore MOVE a non assegnata - " + err.Error())
			}
		case idHeaderSave:
			if err := saveHeaderToCurrentMap(); err != nil {
				msgbox("PML Studio", "Impossibile salvare l'Header della mappa:\r\n\r\n"+err.Error(), MB_OK|MB_ICONERROR)
				setToolbarStatus("Header mappa: errore salvataggio - " + err.Error())
			} else if currentMap != nil {
				setToolbarStatus(fmt.Sprintf("Header Map%03d salvato permanentemente.", currentMap.ID))
			}
		case idHelpToolbar, idMenuHelpAbout:
			msgbox("PLM Studio", "PLM Studio 0.5 - editor nativo Windows", MB_OK|MB_ICONINFORMATION)
		case idViewMap, idMenuToolsMap:
			setMode("map")
		case idViewPermissions, idMenuToolsPermissions:
			setMode("permissions")
		case idViewEvents, idMenuToolsEvents:
			setMode("events")
		case idMenuToolsRepairMaps:
			showDuplicateRepairAndRefresh()
		case idMenuToolsInsertPalette:
			importNumberedPalettePNG()
		case idMenuToolsGameStart:
			showGameStartEditor()
		case idViewEncounters:
			setMode("encounters")
		case idViewHeader:
			setMode("header")
		case idViewConnections, idMenuToolsConnections:
			showConnectionsManager()
		case idViewAnimations:
			setMode("animations")
		case idViewDatabase:
			setMode("database")
		case idPaletteModeCombo:
			if notify == 1 { // CBN_SELCHANGE
				changeMapTilesetFromSelector()
			}
		case idZoomCombo:
			if notify == 1 { // CBN_SELCHANGE
				sel, _, _ := pSendMessageW.Call(uintptr(hwndZoomCombo), CB_GETCURSEL, 0, 0)
				if int(sel) >= 0 && int(sel) <= 2 {
					mapZoomIndex = int(sel)
					mapScrollX, mapScrollY = 0, 0
					updateCanvasScrollbars(hwndCanvas)
					invalidate(hwndCanvas)
				}
			}
		case idLevelCombo:
			if notify == 1 { // CBN_SELCHANGE
				sel, _, _ := pSendMessageW.Call(uintptr(hwndLevelCombo), CB_GETCURSEL, 0, 0)
				if int(sel) >= 0 && int(sel) < 3 {
					activeLayer = int(sel)
					updateMapToolButtonStates()
					updateMapEditorStatus()
					invalidate(hwndCanvas)
				}
			}
		case idMapRegionFilter:
			if notify == 1 { // CBN_SELCHANGE
				setMapRegionFilterFromCombo()
			}
		case idGridToggle:
			changeEditorPreferences(func(s *Settings) { s.GridDefault = !gridEnabled })
			if gridEnabled {
				setText(hwndGridToggle, "Griglia ON")
			} else {
				setText(hwndGridToggle, "Griglia OFF")
			}
			invalidate(hwndCanvas)
		case idMenuExit:
			pSendMessageW.Call(uintptr(hwnd), WM_CLOSE, 0, 0)
		case 1210:
			if mode == "events" {
				placeEvent = true
				setText(hwndStatus, "+ Evento: clicca una casella della mappa")
			}
		case 1211:
			saveSelectedEvent()
		case 1212:
			deleteSelectedEvent()
		default:
			if handleAnimationEditorCommand(int(id), int(notify)) {
				return 0
			}
			if handlePermissionCommand(id) {
				return 0
			}
		}
		return 0
	case 0x0113: // WM_TIMER
		if w == editorAutosaveTimer {
			runEditorAutosave()
			return 0
		}
	case 0x001A: // WM_SETTINGCHANGE
		if settings.Theme == "system" {
			applyLiveEditorTheme()
		}
	case WM_CLOSE:
		if settings.ConfirmExit && msgboxResult("PML Studio", "Chiudere l'editor?", MB_YESNO|MB_ICONINFORMATION) != IDYES {
			return 0
		}
		if confirmUnsavedProjectChanges("chiudere PLM Studio") {
			pDestroyWindow.Call(uintptr(hwnd))
		}
		return 0
	case WM_DESTROY:
		pPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(uintptr(hwnd), uintptr(msg), w, l)
	return r
}
