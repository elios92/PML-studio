//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

func showControl(h syscall.Handle, visible bool) {
	if h == 0 {
		return
	}
	cmd := uintptr(SW_HIDE)
	if visible {
		cmd = SW_SHOW
	}
	pShowWindow.Call(uintptr(h), cmd)
}

func moveControl(h syscall.Handle, x, y, w, hgt int32) {
	if h == 0 || w <= 0 || hgt <= 0 {
		return
	}
	if layoutScalePercent > 100 {
		x = x * layoutScalePercent / 100
		y = y * layoutScalePercent / 100
		w = w * layoutScalePercent / 100
		hgt = hgt * layoutScalePercent / 100
	}
	pMoveWindow.Call(uintptr(h), uintptr(x), uintptr(y), uintptr(w), uintptr(hgt), 0)
}

// moveControlRepaint is reserved for controls that can jump between distinct
// layout bands (currently the main view tabs). MoveWindow with repaint=true
// forces Win32 to erase/repaint the vacated parent area, preventing stale
// button pixels from remaining visible when the map toolbar is removed.
//
// Keep ordinary controls on moveControl: repainting every child during resize
// would add unnecessary WM_PAINT traffic and can reintroduce UI stutter.
func moveControlRepaint(h syscall.Handle, x, y, w, hgt int32) {
	if h == 0 || w <= 0 || hgt <= 0 {
		return
	}
	if layoutScalePercent > 100 {
		x = x * layoutScalePercent / 100
		y = y * layoutScalePercent / 100
		w = w * layoutScalePercent / 100
		hgt = hgt * layoutScalePercent / 100
	}
	pMoveWindow.Call(uintptr(h), uintptr(x), uintptr(y), uintptr(w), uintptr(hgt), 1)
}

func viewDisplayName(m string) string {
	switch m {
	case "map":
		return "Vista mappa"
	case "permissions":
		return "Vista movimenti permessi"
	case "events":
		return "Vista eventi"
	case "encounters":
		return "Vista Pokémon selvatici"
	case "header":
		return "Vista header"
	case "connections":
		return "Connessioni"
	case "animations":
		return "Animazioni"
	case "database":
		return "Vista dati"
	default:
		return "Vista mappa"
	}
}

func viewPlaceholderText(m string) string {
	switch m {
	case "encounters":
		return encounterViewPlaceholder()
	case "header":
		return headerViewPlaceholder()
	case "connections":
		return connectionsViewPlaceholder()
	case "animations":
		return animationsViewPlaceholder()
	case "database":
		return databaseViewPlaceholder()
	default:
		return ""
	}
}

func viewCanvasMessage(m string) string {
	switch m {
	case "encounters":
		return encounterCanvasMessage()
	case "header":
		return headerCanvasMessage()
	case "connections":
		return connectionsCanvasMessage()
	case "animations":
		return animationsCanvasMessage()
	case "database":
		return databaseCanvasMessage()
	default:
		return ""
	}
}

func hideSpecializedViews() {
	// Reset forte prima di ogni cambio vista. Le viste specializzate (in
	// particolare Connessioni) sono composte da controlli Win32 figli diretti
	// di hwndMain; se una transizione salta un aggiornamento, possono restare
	// sopra la vista successiva. Nascondiamole sempre tutte prima di mostrare
	// quella richiesta.
	showEncounterEditor(false)
	showHeaderEditor(false)
	showConnectionsEditor(false)
	showAnimationEditor(false)
	showDataEditor(false)
}

func updateInspector() {
	// Ogni refresh parte da uno stato visivo pulito.
	hideSpecializedViews()
	palette := mode == "map"
	permissionsMode := mode == "permissions"
	eventsMode := mode == "events"
	encountersMode := mode == "encounters"
	headerMode := mode == "header"
	connectionsMode := mode == "connections"
	animationsMode := mode == "animations"
	dataMode := mode == "database"

	showControl(hwndCanvas, !encountersMode && !headerMode && !connectionsMode && !animationsMode && !dataMode)
	// La sidebar generica destra appartiene alla Vista mappa. Le altre viste
	// specializzate usano il canvas/pannello centrale a tutta larghezza.
	// La Vista permessi mantiene soltanto la propria colonna codici dedicata.
	showControl(hwndInspector, palette)
	showEncounterEditor(encountersMode)
	showHeaderEditor(headerMode)
	showConnectionsEditor(connectionsMode)
	showAnimationEditor(animationsMode)
	showDataEditor(dataMode)

	for _, h := range []syscall.Handle{hwndPaletteModeCombo, hwndPaletteTilesets, hwndPaletteTilesetList, hwndPaletteAutotile, hwndPaletteAutotileList, hwndPaletteBorders, hwndPaletteBorderArea} {
		showControl(h, palette)
	}
	for _, h := range mapToolHandles() {
		showControl(h, palette)
	}
	showControl(hwndPermModeMovement, false)
	showControl(hwndPermModeTerrain, false)
	showControl(hwndPermSelected, false)
	showControl(hwndPermHelp, false)
	showControl(hwndPermissionsPalette, permissionsMode)
	for _, h := range hwndMovementButtons {
		showControl(h, false)
	}
	for _, h := range hwndTerrainButtons {
		showControl(h, false)
	}
	for _, h := range []syscall.Handle{hwndEventName, hwndEventTrigger, hwndEventMove, hwndEventDialog, hwndEventInfo, hwndAddEvent, hwndSaveEvent, hwndDeleteEvent} {
		showControl(h, false)
	}
	for _, h := range eventToolbarHandles() {
		showControl(h, eventsMode)
	}
	// I placeholder delle viste specializzate vengono disegnati nel canvas
	// centrale; non serve più un pannello STATIC nella vecchia sidebar destra.
	showControl(hwndViewPlaceholder, false)

	switch mode {
	case "map":
		populatePaletteControls()
	case "permissions":
		updatePermissionControls()
	case "events":
		showEvent()
	case "encounters":
		setText(hwndViewPlaceholder, encounterViewPlaceholder())
	case "header":
		setText(hwndViewPlaceholder, headerViewPlaceholder())
	case "connections":
		loadConnectionsEditorForCurrentMap()
	case "animations":
		if animationDoc == nil {
			_ = loadAnimationDoc()
		}
	default:
		setText(hwndViewPlaceholder, viewPlaceholderText(mode))
	}
	updateTabSelection()
}

func setMode(m string) {
	// Nascondi sempre le viste specializzate PRIMA di cambiare mode. Questo
	// evita che i controlli della Vista Connessioni rimangano nello Z-order e
	// si sovrappongano alla vista successiva.
	hideSpecializedViews()
	oldMode := mode
	switch m {
	case "map", "permissions", "events", "encounters", "header", "connections", "animations", "database":
		mode = m
	default:
		mode = "map"
	}
	placeEvent = false
	// Le intro tecniche sono modificabili soltanto come eventi/animazioni, non
	// come normali mappe giocabili. Uscendo dalla Vista eventi torniamo quindi
	// alla vera mappa iniziale (o alla prima mappa giocabile disponibile).
	if mode != "events" && currentMapIsIntroAnimation() {
		if idx := firstNormalMapIndex(); idx >= 0 {
			selectMap(idx)
		}
	}
	updateInspector()
	layout(hwndMain)
	invalidate(hwndCanvas)
	// L'albero cambia contenuto entrando/uscendo dalla Vista eventi perché il
	// ramo EVENTI / ANIMAZIONI è intenzionalmente separato dalle mappe.
	if (oldMode == "events") != (mode == "events") {
		scheduleMapTreeRefresh()
	}
	setText(hwndStatus, "Modalità: "+viewDisplayName(mode))
}

var layoutScalePercent int32 = 100

func layout(hwnd syscall.Handle) {
	layoutScalePercent = int32(settings.UIScale)
	if layoutScalePercent < 100 {
		layoutScalePercent = 100
	}
	defer func() { layoutScalePercent = 100 }()
	dn, ds := diagLayoutStart()
	defer diagLayoutEnd(dn, ds)
	var r RECT
	pGetClientRect.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&r)))
	w, h := r.Right*100/layoutScalePercent, r.Bottom*100/layoutScalePercent
	if w < 980 {
		w = 980
	}
	if h < 620 {
		h = 620
	}

	ensureResizablePanelDefaults()
	const (
		// La toolbar completa appartiene soltanto alla Vista mappa. La Vista
		// eventi usa la stessa fascia esclusivamente per i propri comandi e per
		// Zoom/Griglia; tutte le altre viste la eliminano dal layout.
		toolbarFullH int32 = 54
		tabsH        int32 = 34
		statusH      int32 = 24
		gap          int32 = splitterThickness
	)

	// Le sidebar non sono più dimensioni fisse. I valori vengono conservati
	// nelle impostazioni dell'utente e limitati soltanto per lasciare al canvas
	// uno spazio minimo utilizzabile. La Vista Permessi mantiene una larghezza
	// indipendente perché la colonna codice + comportamento richiede più spazio.
	leftW := int32(settings.LeftPanelWidth)
	rightW := configuredRightPanelWidth()
	eventsMode := mode == "events"
	permissionsMode := mode == "permissions"
	mapMode := mode == "map"
	// Solo la Vista mappa usa la sidebar generica destra. La Vista permessi
	// conserva la propria colonna codici/terrain, che occupa lo stesso spazio
	// geometrico ma non è la sidebar strumenti. Tutte le altre viste sfruttano
	// l'intera area centrale. La Vista eventi resta invariata per ora.
	usesRightColumn := mapMode || permissionsMode
	minRight := int32(188)
	if permissionsMode {
		minRight = 300
	}
	if !usesRightColumn {
		leftW = clampI32(leftW, 150, w-gap-430)
		rightW = 0
	} else {
		leftW = clampI32(leftW, 150, w-minRight-gap*2-430)
		rightW = clampI32(rightW, minRight, w-leftW-gap*2-430)
	}

	toolbarY := int32(0)
	toolbarH := int32(0)
	if mapMode || eventsMode {
		toolbarH = toolbarFullH
	}
	tabsY := toolbarH + 1
	contentY := tabsY + tabsH
	contentH := h - contentY - statusH
	if contentH < 320 {
		contentH = 320
	}

	// La toolbar generale è una funzione della Vista mappa. Le altre viste non
	// devono mostrare comandi di map editing scollegati dal proprio contenuto.
	showControl(hwndToolbarStrip, mapMode || eventsMode)
	if mapMode || eventsMode {
		moveControl(hwndToolbarStrip, 0, toolbarY, w, toolbarH)
	}

	if eventsMode {
		// Vista eventi: restano i comandi evento e, della toolbar mappa, soltanto
		// Zoom e Griglia come richiesto per lavorare sul posizionamento degli eventi.
		for _, h := range standardToolbarHandles() {
			showControl(h, false)
		}
		for _, h := range eventToolbarHandles() {
			showControl(h, true)
		}

		x := int32(8)
		buttons := []struct {
			h syscall.Handle
			w int32
		}{
			{hwndEventCreate, 110}, {hwndEventCopy, 110}, {hwndEventModify, 120},
			{hwndEventDelete, 112}, {hwndEventDuplicate, 125}, {hwndEventRename, 135},
		}
		for i, b := range buttons {
			if i == 4 {
				x += 8
			}
			moveControl(b.h, x, toolbarY+8, b.w, 36)
			x += b.w + 5
		}

		x += 10
		showControl(hwndBtnZoomOut, false)
		showControl(hwndBtnZoomIn, false)
		showControl(hwndLevelCombo, false)
		if x+188 <= w-6 {
			showControl(hwndZoomCombo, true)
			showControl(hwndGridToggle, true)
			moveControl(hwndZoomCombo, x, toolbarY+10, 94, 140)
			moveControl(hwndGridToggle, x+100, toolbarY+10, 82, 30)
		} else {
			showControl(hwndZoomCombo, false)
			showControl(hwndGridToggle, false)
		}

		for _, h := range []syscall.Handle{
			hwndTilesetLabel, hwndTilesetCombo, hwndZoomLabel, hwndLevelLabel,
			hwndTopMapBar, hwndSortLabel, hwndSortCombo, hwndSearchBox,
		} {
			showControl(h, false)
		}
	} else if mapMode {
		// Vista mappa: mantiene la toolbar completa esistente.
		for _, h := range eventToolbarHandles() {
			showControl(h, false)
		}
		for _, h := range []syscall.Handle{
			hwndBtnNew, hwndBtnOpen, hwndBtnSave, hwndBtnRecent, hwndBtnUndo, hwndBtnRedo,
			hwndBtnFolder, hwndBtnMap, hwndBtnConnections, hwndSortCombo,
			hwndBtnZoomOut, hwndBtnZoomIn, hwndBtnSearch, hwndBtnHelp, hwndBtnPlay, hwndBtnStop,
		} {
			showControl(h, true)
		}
		for _, h := range mapToolHandles() {
			showControl(h, true)
		}

		x := int32(6)
		button := int32(40)
		place := func(h syscall.Handle, separator bool) {
			if h == 0 {
				return
			}
			if separator {
				x += 7
			}
			moveControl(h, x, toolbarY+7, button, 40)
			x += button + 2
		}
		place(hwndBtnNew, false)
		place(hwndBtnOpen, false)
		place(hwndBtnSave, false)
		place(hwndBtnUndo, true)
		place(hwndBtnRedo, false)
		for i, h := range []syscall.Handle{hwndToolSelect, hwndToolPencil, hwndToolRectangle, hwndToolFill, hwndToolEyedropper, hwndToolEraser} {
			place(h, i == 0)
		}
		for i, h := range []syscall.Handle{hwndLayer1, hwndLayer2, hwndLayer3} {
			place(h, i == 0)
		}
		place(hwndBtnFolder, true)
		place(hwndBtnMap, false)
		place(hwndBtnConnections, false)
		x += 4
		if x+200 < w-330 {
			showControl(hwndSortCombo, true)
			moveControl(hwndSortCombo, x, toolbarY+10, 198, 200)
			x += 204
		} else {
			showControl(hwndSortCombo, false)
		}
		place(hwndBtnZoomOut, true)
		place(hwndBtnZoomIn, false)
		place(hwndBtnSearch, true)
		place(hwndBtnHelp, false)
		place(hwndBtnPlay, true)
		place(hwndBtnStop, false)

		showControl(hwndTilesetLabel, false)
		showControl(hwndTilesetCombo, false)
		showControl(hwndZoomLabel, false)
		showControl(hwndLevelLabel, false)
		showControl(hwndTopMapBar, false)
		if x+286 < w-4 {
			showControl(hwndZoomCombo, true)
			showControl(hwndGridToggle, true)
			showControl(hwndLevelCombo, true)
			moveControl(hwndZoomCombo, x+6, toolbarY+10, 94, 140)
			moveControl(hwndGridToggle, x+104, toolbarY+10, 82, 30)
			moveControl(hwndLevelCombo, x+190, toolbarY+10, 92, 140)
			showControl(hwndSearchBox, w > 1750)
			if w > 1750 {
				moveControl(hwndSearchBox, x+288, toolbarY+10, w-(x+288)-10, 30)
			}
		} else {
			showControl(hwndZoomCombo, false)
			showControl(hwndGridToggle, false)
			showControl(hwndLevelCombo, false)
			showControl(hwndSearchBox, false)
		}
		showControl(hwndSortLabel, false)
	} else {
		// Permessi, Pokémon selvatici, Header, Connessioni, Animazioni e Dati:
		// nessun comando della toolbar mappa deve restare visibile. La fascia
		// viene anche rimossa dal layout per non lasciare spazio vuoto.
		for _, h := range standardToolbarHandles() {
			showControl(h, false)
		}
		for _, h := range eventToolbarHandles() {
			showControl(h, false)
		}
		for _, h := range []syscall.Handle{
			hwndTilesetLabel, hwndTilesetCombo, hwndZoomLabel, hwndLevelLabel,
			hwndTopMapBar, hwndSortLabel, hwndSortCombo, hwndSearchBox,
		} {
			showControl(h, false)
		}
	}

	// Sidebar sinistra parte già dalla riga tab, esattamente come Advance Map.
	moveControl(hwndSidebar, 0, tabsY, leftW, h-tabsY-statusH)
	// Navigazione mappe multi-regione: il selettore è parte della sidebar
	// condivisa da Mappa, Permessi, Eventi e dagli altri editor legati alla mappa.
	moveControl(hwndMapRegionLabel, 8, tabsY+8, 48, 20)
	moveControl(hwndMapRegionCombo, 58, tabsY+4, leftW-66, 180)

	// Split orizzontale della sidebar sinistra. Il blocco informazioni in basso
	// può essere ingrandito/ridotto; l'albero mappe prende tutto lo spazio restante.
	leftTreeTop := tabsY + 36
	leftBottom := h - statusH
	maxDetails := leftBottom - leftTreeTop - 80 - gap
	leftDetailsH := clampI32(int32(settings.LeftDetailsHeight), 72, maxDetails)
	leftInfoTop := leftBottom - leftDetailsH
	projectInfoH := int32(33)
	infoInnerGap := int32(2)
	quickInfoH := leftDetailsH - projectInfoH - infoInnerGap
	if quickInfoH < 36 {
		quickInfoH = 36
	}
	moveControl(hwndMapList, 3, leftTreeTop, leftW-7, leftInfoTop-leftTreeTop-gap)
	moveControl(hwndSplitLeftH, 3, leftInfoTop-gap, leftW-7, gap)
	moveControl(hwndMapQuickInfo, 3, leftInfoTop, leftW-7, quickInfoH)
	moveControl(hwndProjectInfo, 3, leftInfoTop+quickInfoH+infoInnerGap, leftW-7, projectInfoH)

	// Separatore verticale sinistro trascinabile.
	moveControl(hwndSplitLeftV, leftW, tabsY, gap, h-tabsY-statusH)

	centralX := leftW + gap
	centralW := w - leftW - rightW - gap*2
	if !usesRightColumn {
		centralW = w - leftW - gap
	}
	if centralW < 430 {
		centralW = 430
	}

	// Tab solo sopra l'area centrale: è la caratteristica visiva più evidente del riferimento.
	moveControlRepaint(hwndTabsStrip, centralX, tabsY, centralW, tabsH)
	tabs := []syscall.Handle{hwndModeMap, hwndModePass, hwndModeEvents, hwndModeEncounters, hwndModeHeader, hwndModeConnections, hwndModeAnimations, hwndModeDatabase}
	// Larghezze con margine DPI/padding per mostrare sempre le etichette complete
	// con Segoe UI. Movimenti permessi e Pokémon selvatici richiedono più spazio
	// perché sono le due etichette più lunghe della barra viste.
	widths := []int32{94, 220, 90, 205, 96, 108, 96, 88}
	tx := centralX + 4
	for i, t := range tabs {
		moveControlRepaint(t, tx, tabsY+2, widths[i], tabsH-3)
		tx += widths[i] + 1
	}

	moveControl(hwndCanvas, centralX, contentY, centralW, contentH)
	moveControl(hwndEncounterPanel, centralX, contentY, centralW, contentH)
	layoutEncounterEditor(centralW, contentH)
	moveControl(hwndHeaderPanel, centralX, contentY, centralW, contentH)
	layoutConnectionsEditor(centralX, contentY, centralW, contentH)
	layoutAnimationEditor(centralX, contentY, centralW, contentH)
	layoutDataEditor(centralX, contentY, centralW, contentH)

	// Palette destra: Blocco bordi -> Autotile -> Tiles. La larghezza è
	// regolabile tramite il separatore verticale; in Vista mappa anche le tre
	// sezioni possono essere ridimensionate verticalmente.
	rightX := centralX + centralW + gap
	showControl(hwndSplitRightV, usesRightColumn)
	if usesRightColumn {
		moveControl(hwndSplitRightV, rightX-gap, tabsY, gap, h-tabsY-statusH)
	}
	moveControl(hwndInspector, rightX, contentY, rightW, contentH)
	innerX, innerW := rightX+2, rightW-4
	moveControl(hwndPaletteModeCombo, innerX+2, contentY+3, innerW-4, 160)
	paletteY := contentY + 30
	paletteBottom := contentY + contentH - 2
	paletteAvail := paletteBottom - paletteY
	const (
		minBorderH = int32(58)
		minAutoH   = int32(68)
		minTilesH  = int32(96)
	)
	borderH := clampI32(int32(settings.PaletteBorderHeight), minBorderH, paletteAvail-minAutoH-minTilesH-gap*2)
	autoH := clampI32(int32(settings.PaletteAutotileHeight), minAutoH, paletteAvail-borderH-minTilesH-gap*2)
	autoY := paletteY + borderH + gap
	tilesY := autoY + autoH + gap
	moveControl(hwndPaletteBorders, innerX, paletteY, innerW, borderH)
	moveControl(hwndPaletteBorderArea, innerX+5, paletteY+18, innerW-10, borderH-23)
	moveControl(hwndSplitPalette1, innerX, paletteY+borderH, innerW, gap)
	moveControl(hwndPaletteAutotile, innerX, autoY, innerW, autoH)
	moveControl(hwndPaletteAutotileList, innerX+2, autoY+18, innerW-4, autoH-20)
	moveControl(hwndSplitPalette2, innerX, autoY+autoH, innerW, gap)
	moveControl(hwndPaletteTilesets, innerX, tilesY, innerW, paletteBottom-tilesY)
	moveControl(hwndPaletteTilesetList, innerX+2, tilesY+18, innerW-4, paletteBottom-tilesY-20)

	// I separatori orizzontali della palette servono solo nella Vista mappa.
	showControl(hwndSplitPalette1, mode == "map")
	showControl(hwndSplitPalette2, mode == "map")

	// Vista movimenti permessi: due pulsanti in testa alla stessa colonna
	// Advance Map. Sotto compare una sola lista alla volta: Movimenti oppure
	// Terrain Tags. La mappa centrale e le dimensioni della colonna restano
	// invariate.
	permButtonH := int32(27)
	permButtonGap := int32(2)
	permButtonW := (rightW - permButtonGap) / 2
	moveControl(hwndPermModeMovement, rightX, contentY, permButtonW, permButtonH)
	moveControl(hwndPermModeTerrain, rightX+permButtonW+permButtonGap, contentY, rightW-permButtonW-permButtonGap, permButtonH)
	moveControl(hwndPermissionsPalette, rightX, contentY+permButtonH+permButtonGap, rightW, contentH-permButtonH-permButtonGap)

	moveControl(hwndEventInfo, innerX+4, contentY+6, innerW-8, 34)
	moveControl(hwndEventName, innerX+4, contentY+45, innerW-8, 22)
	moveControl(hwndEventTrigger, innerX+4, contentY+72, innerW-8, 140)
	moveControl(hwndEventMove, innerX+4, contentY+102, innerW-8, 140)
	moveControl(hwndEventDialog, innerX+4, contentY+132, innerW-8, 190)
	moveControl(hwndAddEvent, innerX+4, contentY+329, 55, 23)
	moveControl(hwndSaveEvent, innerX+63, contentY+329, 55, 23)
	moveControl(hwndDeleteEvent, innerX+122, contentY+329, 58, 23)
	moveControl(hwndViewPlaceholder, innerX+5, contentY+7, innerW-10, contentH-14)

	// Geometria corrente usata dal wndproc dei separatori durante il drag.
	resizeLeftW = leftW
	resizeRightW = rightW
	resizeContentBottom = leftBottom
	resizeLeftTreeTop = leftTreeTop
	resizePaletteY = paletteY
	resizePaletteAvail = paletteAvail
	resizePaletteBorderH = borderH
	resizePaletteAutoY = autoY

	moveControl(hwndStatus, 0, h-statusH, w, statusH)
	pRedrawWindow.Call(uintptr(hwnd), 0, 0, RDW_INVALIDATE|RDW_ALLCHILDREN)
}
