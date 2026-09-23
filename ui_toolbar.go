//go:build windows

package main

import "syscall"

const (
	idUndo        = 1600
	idRedo        = 1601
	idToolbarConn = 1602
	idZoomOut     = 1603
	idZoomIn      = 1604
	idSearch      = 1605
	idHelpToolbar = 1606
	idPlaytest    = 1607
	idStopPlay    = 1608
	idSortCombo   = 1610
	idSearchBox   = 1611

	idToolSelect     = 1620
	idToolPencil     = 1621
	idToolRectangle  = 1622
	idToolFill       = 1623
	idToolEyedropper = 1624
	idToolEraser     = 1625
	idLayer1         = 1631
	idLayer2         = 1632
	idLayer3         = 1633

	idEventCreate    = 1660
	idEventCopy      = 1661
	idEventModify    = 1662
	idEventDelete    = 1663
	idEventMoveMap   = 1664
	idEventDuplicate = 1665
	idEventRename    = 1666
)

var (
	hwndToolSelect, hwndToolPencil, hwndToolRectangle syscall.Handle
	hwndToolFill, hwndToolEyedropper, hwndToolEraser  syscall.Handle
	hwndLayer1, hwndLayer2, hwndLayer3                syscall.Handle
)

func toolbarButton(hInst syscall.Handle, id uintptr, kind string) syscall.Handle {
	h := createWindow("BUTTON", "", WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_PUSHBUTTON|BS_FLAT|BS_BITMAP, 0, 0, 0, 0, hwndMain, id, hInst)
	setButtonBitmap(h, kind)
	return h
}

func mapToolbarButton(hInst syscall.Handle, id uintptr, kind string, group bool) syscall.Handle {
	style := uint32(WS_CHILD | WS_VISIBLE | WS_TABSTOP | BS_AUTORADIOBUTTON | BS_PUSHLIKE | BS_FLAT | BS_BITMAP)
	if group {
		style |= WS_GROUP
	}
	h := createWindow("BUTTON", "", style, 0, 0, 0, 0, hwndMain, id, hInst)
	setButtonBitmap(h, kind)
	return h
}

func eventToolbarButton(hInst syscall.Handle, id uintptr, text string) syscall.Handle {
	return createWindow("BUTTON", text, WS_CHILD|WS_TABSTOP|BS_PUSHBUTTON|BS_FLAT, 0, 0, 0, 0, hwndMain, id, hInst)
}

func eventToolbarHandles() []syscall.Handle {
	return []syscall.Handle{
		hwndEventCreate, hwndEventCopy, hwndEventModify, hwndEventDelete,
		hwndEventDuplicate, hwndEventRename,
	}
}

func standardToolbarHandles() []syscall.Handle {
	return []syscall.Handle{
		hwndBtnNew, hwndBtnOpen, hwndBtnSave, hwndBtnRecent, hwndBtnUndo, hwndBtnRedo,
		hwndToolSelect, hwndToolPencil, hwndToolRectangle, hwndToolFill, hwndToolEyedropper, hwndToolEraser,
		hwndLayer1, hwndLayer2, hwndLayer3, hwndBtnFolder, hwndBtnMap, hwndBtnConnections,
		hwndSortCombo, hwndBtnZoomOut, hwndBtnZoomIn, hwndBtnSearch, hwndBtnHelp, hwndBtnPlay, hwndBtnStop,
		hwndSearchBox, hwndZoomCombo, hwndGridToggle, hwndLevelCombo,
	}
}

func createToolbarControls(hInst syscall.Handle) {
	hwndToolbarStrip = createWindow("STATIC", "", WS_CHILD|WS_VISIBLE, 0, 0, 0, 0, hwndMain, 1599, hInst)

	hwndBtnNew = toolbarButton(hInst, idNewProject, "new")
	hwndBtnOpen = toolbarButton(hInst, idOpenProject, "open")
	hwndBtnSave = toolbarButton(hInst, idSaveProject, "save")
	hwndBtnRecent = toolbarButton(hInst, idRecentProject, "recent")
	hwndBtnUndo = toolbarButton(hInst, idUndo, "undo")
	hwndBtnRedo = toolbarButton(hInst, idRedo, "redo")

	// Strumenti mappa della 0.4.
	hwndToolSelect = mapToolbarButton(hInst, idToolSelect, "select", true)
	hwndToolPencil = mapToolbarButton(hInst, idToolPencil, "pencil", false)
	hwndToolRectangle = mapToolbarButton(hInst, idToolRectangle, "rectangle", false)
	hwndToolFill = mapToolbarButton(hInst, idToolFill, "fill", false)
	hwndToolEyedropper = mapToolbarButton(hInst, idToolEyedropper, "eyedropper", false)
	hwndToolEraser = mapToolbarButton(hInst, idToolEraser, "eraser", false)
	hwndLayer1 = mapToolbarButton(hInst, idLayer1, "layer1", true)
	hwndLayer2 = mapToolbarButton(hInst, idLayer2, "layer2", false)
	hwndLayer3 = mapToolbarButton(hInst, idLayer3, "layer3", false)

	hwndBtnFolder = toolbarButton(hInst, idMapFolder, "folder")
	hwndBtnMap = toolbarButton(hInst, idNewMap, "map")
	hwndBtnConnections = toolbarButton(hInst, idToolbarConn, "conn")
	hwndBtnZoomOut = toolbarButton(hInst, idZoomOut, "zoomout")
	hwndBtnZoomIn = toolbarButton(hInst, idZoomIn, "zoomin")
	hwndBtnSearch = toolbarButton(hInst, idSearch, "search")
	hwndBtnHelp = toolbarButton(hInst, idHelpToolbar, "help")
	hwndBtnPlay = toolbarButton(hInst, idPlaytest, "play")
	hwndBtnStop = toolbarButton(hInst, idStopPlay, "stop")

	hwndSortCombo = createWindow("COMBOBOX", "", WS_CHILD|WS_VISIBLE|WS_TABSTOP|CBS_DROPDOWNLIST, 0, 0, 0, 180, hwndMain, idSortCombo, hInst)
	for _, s := range []string{"Ordina per nome mappa", "Ordina per ID mappa", "Ordine progetto"} {
		comboAdd(hwndSortCombo, s)
	}
	pSendMessageW.Call(uintptr(hwndSortCombo), CB_SETCURSEL, 0, 0)

	hwndSearchBox = createWindow("EDIT", "", WS_CHILD|WS_VISIBLE|WS_BORDER|WS_TABSTOP, 0, 0, 0, 0, hwndMain, idSearchBox, hInst)

	// Toolbar dedicata alla Vista eventi. Questi controlli restano nascosti
	// in tutte le altre viste: l'editor eventi non riusa gli strumenti mappa.
	hwndEventCreate = eventToolbarButton(hInst, idEventCreate, "Crea evento")
	hwndEventCopy = eventToolbarButton(hInst, idEventCopy, "Copia evento")
	hwndEventModify = eventToolbarButton(hInst, idEventModify, "Modifica evento")
	hwndEventDelete = eventToolbarButton(hInst, idEventDelete, "Elimina evento")
	hwndEventDuplicate = eventToolbarButton(hInst, idEventDuplicate, "Duplica evento")
	hwndEventRename = eventToolbarButton(hInst, idEventRename, "Rinomina evento")
	for _, h := range eventToolbarHandles() {
		showControl(h, false)
	}
}

func mapToolHandles() []syscall.Handle {
	return []syscall.Handle{
		hwndToolSelect, hwndToolPencil, hwndToolRectangle,
		hwndToolFill, hwndToolEyedropper, hwndToolEraser,
		hwndLayer1, hwndLayer2, hwndLayer3,
	}
}

func updateMapToolButtonStates() {
	tools := []syscall.Handle{hwndToolSelect, hwndToolPencil, hwndToolRectangle, hwndToolFill, hwndToolEyedropper, hwndToolEraser}
	for i, h := range tools {
		check := uintptr(BST_UNCHECKED)
		if int(activeMapTool) == i {
			check = BST_CHECKED
		}
		pSendMessageW.Call(uintptr(h), BM_SETCHECK, check, 0)
	}
	layers := []syscall.Handle{hwndLayer1, hwndLayer2, hwndLayer3}
	for i, h := range layers {
		check := uintptr(BST_UNCHECKED)
		if activeLayer == i {
			check = BST_CHECKED
		}
		pSendMessageW.Call(uintptr(h), BM_SETCHECK, check, 0)
	}
}

func setToolbarStatus(s string) {
	setText(hwndStatus, s)
}
