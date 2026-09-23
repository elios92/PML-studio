//go:build windows

package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
)

const (
	idDataPBSView           = 2398
	idDataPokemonSettings   = 2399
	idDataStandardMode      = 2400
	idDataAdvancedMode      = 2401
	idDataFile              = 2402
	idDataRecord            = 2403
	idDataField             = 2404
	idDataValue             = 2405
	idDataApply             = 2407
	idDataAddField          = 2408
	idDataAddValue          = 2409
	idDataAdd               = 2410
	idDataRemove            = 2411
	idDataNewRecordID       = 2412
	idDataNewRecord         = 2413
	idDataDuplicate         = 2414
	idDataDeleteRecord      = 2415
	idDataSave              = 2416
	idDataRawEditor         = 2417
	idDataRawSave           = 2418
	idDataSettingsGen       = 2420
	idDataSettingsApply     = 2421
	idDataSettingsRaw       = 2422
	idDataSettingsRawSave   = 2423
	idDataSettingsRawReload = 2424
	idDataSettingsPartySize = 2440
	idDataSettingsMega      = 2442
	idDataSettingsAwaken    = 2443

	cbnEditChange      = 5
	bsAutoCheckboxData = 0x0003
	bmGetCheckData     = 0x00F0
	bmSetCheckData     = 0x00F1
	bstUncheckedData   = 0
	bstCheckedData     = 1
)

var (
	hwndDataPanel syscall.Handle

	// Barra di navigazione interna a Vista Dati.
	hwndDataPBSView, hwndDataPokemonSettings syscall.Handle
	hwndDataStandard, hwndDataAdvanced       syscall.Handle
	hwndDataIntro                            syscall.Handle

	// Controlli comuni alla sottovista PBS.
	hwndDataFileLabel, hwndDataFile syscall.Handle

	// PBS - Standard No-Code.
	hwndDataStandardPanel                                      syscall.Handle
	hwndDataRecordLabel, hwndDataRecord                        syscall.Handle
	hwndDataFieldLabel, hwndDataField                          syscall.Handle
	hwndDataValueLabel, hwndDataValue                          syscall.Handle
	hwndDataAddTitle, hwndDataAddFieldLbl, hwndDataAddField    syscall.Handle
	hwndDataAddValueLbl, hwndDataAddValue                      syscall.Handle
	hwndDataNewIDLbl, hwndDataNewRecordID                      syscall.Handle
	hwndDataApply, hwndDataAdd, hwndDataRemove                 syscall.Handle
	hwndDataNewRecord, hwndDataDuplicate, hwndDataDeleteRecord syscall.Handle
	hwndDataStandardSave, hwndDataStandardStatus               syscall.Handle

	// PBS - Advanced: editor diretto del file PBS reale.
	hwndDataAdvancedPanel                                           syscall.Handle
	hwndDataRawEditor, hwndDataAdvancedSave, hwndDataAdvancedStatus syscall.Handle

	// Impostazioni Pokémon - Standard No-Code.
	hwndDataSettingsStandardPanel                         syscall.Handle
	hwndDataSettingsTitle, hwndDataSettingsInfo           syscall.Handle
	hwndDataSettingsGenLabel, hwndDataSettingsGen         syscall.Handle
	hwndDataSettingsPartyLabel, hwndDataSettingsPartySize syscall.Handle
	hwndDataSettingsMega, hwndDataSettingsAwaken          syscall.Handle
	hwndDataSettingsApply, hwndDataSettingsStatus         syscall.Handle

	// Impostazioni Pokémon - Advanced: configurazione tecnica reale.
	hwndDataSettingsAdvancedPanel                                                      syscall.Handle
	hwndDataSettingsRawLabel, hwndDataSettingsRawEditor                                syscall.Handle
	hwndDataSettingsRawSave, hwndDataSettingsRawReload, hwndDataSettingsAdvancedStatus syscall.Handle

	dataPanelOldWndProc                 uintptr
	dataStandardPanelOldWndProc         uintptr
	dataAdvancedPanelOldWndProc         uintptr
	dataSettingsStandardPanelOldWndProc uintptr
	dataSettingsAdvancedPanelOldWndProc uintptr

	dataDoc                    *dataPBSDocument
	dataFiles                  []string
	dataFields                 []dataPBSField
	dataAddFieldKeys           []string
	dataCurrentFile            int = -1
	dataCurrentRecord          int = -1
	dataCurrentField           int = -1
	dataAdvancedMode           bool
	dataPokemonSettingsMode    bool
	dataRefreshingUI           bool
	dataValueEdited            bool
	dataRawEdited              bool
	dataSettingsRawEdited      bool
	dataSettingsStandardEdited bool
	dataLoadedProject          string
)

var dataContainerWndProc = syscall.NewCallback(func(hwnd syscall.Handle, msg uint32, w, l uintptr) uintptr {
	if msg == WM_COMMAND && hwndMain != 0 {
		pSendMessageW.Call(uintptr(hwndMain), uintptr(msg), w, l)
		return 0
	}
	if dataPanelOldWndProc != 0 {
		r, _, _ := pCallWindowProcW.Call(dataPanelOldWndProc, uintptr(hwnd), uintptr(msg), w, l)
		return r
	}
	r, _, _ := pDefWindowProcW.Call(uintptr(hwnd), uintptr(msg), w, l)
	return r
})

var dataChildPanelWndProc = syscall.NewCallback(func(hwnd syscall.Handle, msg uint32, w, l uintptr) uintptr {
	if msg == WM_COMMAND && hwndDataPanel != 0 {
		pSendMessageW.Call(uintptr(hwndDataPanel), uintptr(msg), w, l)
		return 0
	}
	old := dataStandardPanelOldWndProc
	switch hwnd {
	case hwndDataAdvancedPanel:
		old = dataAdvancedPanelOldWndProc
	case hwndDataSettingsStandardPanel:
		old = dataSettingsStandardPanelOldWndProc
	case hwndDataSettingsAdvancedPanel:
		old = dataSettingsAdvancedPanelOldWndProc
	}
	if old != 0 {
		r, _, _ := pCallWindowProcW.Call(old, uintptr(hwnd), uintptr(msg), w, l)
		return r
	}
	r, _, _ := pDefWindowProcW.Call(uintptr(hwnd), uintptr(msg), w, l)
	return r
})

func installDataPanelForwarder() {
	if hwndDataPanel == 0 || dataPanelOldWndProc != 0 {
		return
	}
	r, _, _ := pSetWindowLongPtrW.Call(uintptr(hwndDataPanel), ^uintptr(3), dataContainerWndProc)
	dataPanelOldWndProc = r
}

func installDataChildForwarder(hwnd syscall.Handle, oldProc *uintptr) {
	if hwnd == 0 || oldProc == nil || *oldProc != 0 {
		return
	}
	r, _, _ := pSetWindowLongPtrW.Call(uintptr(hwnd), ^uintptr(3), dataChildPanelWndProc)
	*oldProc = r
}

func createDataEditor(hInst syscall.Handle) {
	// Un solo contenitore per Vista Dati. Le quattro superfici operative sono
	// pannelli distinti e mutuamente esclusivi, quindi non possono sovrapporsi.
	hwndDataPanel = createWindow("BUTTON", "", WS_CHILD|BS_GROUPBOX|WS_CLIPCHILDREN, 0, 0, 0, 0, hwndMain, 2380, hInst)
	installDataPanelForwarder()

	hwndDataPBSView = createWindow("BUTTON", "Dati PBS", WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_AUTORADIOBUTTON|BS_PUSHLIKE, 0, 0, 0, 0, hwndDataPanel, idDataPBSView, hInst)
	hwndDataPokemonSettings = createWindow("BUTTON", "Impostazioni Pokémon", WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_AUTORADIOBUTTON|BS_PUSHLIKE, 0, 0, 0, 0, hwndDataPanel, idDataPokemonSettings, hInst)
	hwndDataStandard = createWindow("BUTTON", "Standard", WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_AUTORADIOBUTTON|BS_PUSHLIKE, 0, 0, 0, 0, hwndDataPanel, idDataStandardMode, hInst)
	hwndDataAdvanced = createWindow("BUTTON", "Avanzata", WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_AUTORADIOBUTTON|BS_PUSHLIKE, 0, 0, 0, 0, hwndDataPanel, idDataAdvancedMode, hInst)
	hwndDataIntro = createWindow("STATIC", "", WS_CHILD|WS_VISIBLE, 0, 0, 0, 0, hwndDataPanel, 2381, hInst)

	hwndDataFileLabel = createWindow("STATIC", "Tipo dati", WS_CHILD, 0, 0, 0, 0, hwndDataPanel, 2382, hInst)
	hwndDataFile = createWindow("COMBOBOX", "", WS_CHILD|WS_TABSTOP|CBS_DROPDOWNLIST|WS_VSCROLL, 0, 0, 0, 300, hwndDataPanel, idDataFile, hInst)

	// PBS Standard: ripristino dell'interfaccia No-Code originale con menu a
	// tendina e campi compilabili. Nessun accesso diretto al testo PBS.
	hwndDataStandardPanel = createWindow("STATIC", "", WS_CHILD|WS_CLIPCHILDREN, 0, 0, 0, 0, hwndDataPanel, 2383, hInst)
	installDataChildForwarder(hwndDataStandardPanel, &dataStandardPanelOldWndProc)
	hwndDataRecordLabel = createWindow("STATIC", "Voce", WS_CHILD|WS_VISIBLE, 0, 0, 0, 0, hwndDataStandardPanel, 2384, hInst)
	hwndDataRecord = createWindow("COMBOBOX", "", WS_CHILD|WS_VISIBLE|WS_TABSTOP|CBS_DROPDOWNLIST|WS_VSCROLL, 0, 0, 0, 320, hwndDataStandardPanel, idDataRecord, hInst)
	hwndDataFieldLabel = createWindow("STATIC", "Campo", WS_CHILD|WS_VISIBLE, 0, 0, 0, 0, hwndDataStandardPanel, 2385, hInst)
	hwndDataField = createWindow("COMBOBOX", "", WS_CHILD|WS_VISIBLE|WS_TABSTOP|CBS_DROPDOWNLIST|WS_VSCROLL, 0, 0, 0, 300, hwndDataStandardPanel, idDataField, hInst)
	hwndDataValueLabel = createWindow("STATIC", "Valore", WS_CHILD|WS_VISIBLE, 0, 0, 0, 0, hwndDataStandardPanel, 2386, hInst)
	hwndDataValue = createWindow("COMBOBOX", "", WS_CHILD|WS_VISIBLE|WS_TABSTOP|CBS_DROPDOWN|WS_VSCROLL, 0, 0, 0, 320, hwndDataStandardPanel, idDataValue, hInst)
	hwndDataAddTitle = createWindow("STATIC", "Aggiungi dati", WS_CHILD|WS_VISIBLE, 0, 0, 0, 0, hwndDataStandardPanel, 2387, hInst)
	hwndDataAddFieldLbl = createWindow("STATIC", "Nuovo campo", WS_CHILD|WS_VISIBLE, 0, 0, 0, 0, hwndDataStandardPanel, 2388, hInst)
	hwndDataAddField = createWindow("COMBOBOX", "", WS_CHILD|WS_VISIBLE|WS_TABSTOP|CBS_DROPDOWN|WS_VSCROLL, 0, 0, 0, 300, hwndDataStandardPanel, idDataAddField, hInst)
	hwndDataAddValueLbl = createWindow("STATIC", "Nuovo valore", WS_CHILD|WS_VISIBLE, 0, 0, 0, 0, hwndDataStandardPanel, 2389, hInst)
	hwndDataAddValue = createWindow("EDIT", "", WS_CHILD|WS_VISIBLE|WS_BORDER|WS_TABSTOP, 0, 0, 0, 0, hwndDataStandardPanel, idDataAddValue, hInst)
	hwndDataNewIDLbl = createWindow("STATIC", "ID nuova voce", WS_CHILD|WS_VISIBLE, 0, 0, 0, 0, hwndDataStandardPanel, 2390, hInst)
	hwndDataNewRecordID = createWindow("EDIT", "", WS_CHILD|WS_VISIBLE|WS_BORDER|WS_TABSTOP, 0, 0, 0, 0, hwndDataStandardPanel, idDataNewRecordID, hInst)
	hwndDataApply = createWindow("BUTTON", "Applica campo", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 0, 0, 0, 0, hwndDataStandardPanel, idDataApply, hInst)
	hwndDataAdd = createWindow("BUTTON", "Aggiungi campo", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 0, 0, 0, 0, hwndDataStandardPanel, idDataAdd, hInst)
	hwndDataRemove = createWindow("BUTTON", "Rimuovi campo", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 0, 0, 0, 0, hwndDataStandardPanel, idDataRemove, hInst)
	hwndDataNewRecord = createWindow("BUTTON", "Nuova voce", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 0, 0, 0, 0, hwndDataStandardPanel, idDataNewRecord, hInst)
	hwndDataDuplicate = createWindow("BUTTON", "Duplica voce", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 0, 0, 0, 0, hwndDataStandardPanel, idDataDuplicate, hInst)
	hwndDataDeleteRecord = createWindow("BUTTON", "Elimina voce", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 0, 0, 0, 0, hwndDataStandardPanel, idDataDeleteRecord, hInst)
	hwndDataStandardSave = createWindow("BUTTON", "Salva dati", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 0, 0, 0, 0, hwndDataStandardPanel, idDataSave, hInst)
	hwndDataStandardStatus = createWindow("STATIC", "", WS_CHILD|WS_VISIBLE, 0, 0, 0, 0, hwndDataStandardPanel, 2391, hInst)

	// PBS Advanced: editor testuale del PBS reale. È fisicamente separato dal
	// pannello Standard e non condivide coordinate o controlli.
	hwndDataAdvancedPanel = createWindow("STATIC", "", WS_CHILD|WS_CLIPCHILDREN, 0, 0, 0, 0, hwndDataPanel, 2392, hInst)
	installDataChildForwarder(hwndDataAdvancedPanel, &dataAdvancedPanelOldWndProc)
	hwndDataRawEditor = createWindow("EDIT", "", WS_CHILD|WS_VISIBLE|WS_BORDER|WS_TABSTOP|WS_VSCROLL|WS_HSCROLL|ES_MULTILINE|ES_AUTOVSCROLL|ES_WANTRETURN, 0, 0, 0, 0, hwndDataAdvancedPanel, idDataRawEditor, hInst)
	pSendMessageW.Call(uintptr(hwndDataRawEditor), 0x00C5, 16*1024*1024, 0)
	hwndDataAdvancedStatus = createWindow("STATIC", "", WS_CHILD|WS_VISIBLE, 0, 0, 0, 0, hwndDataAdvancedPanel, 2393, hInst)
	hwndDataAdvancedSave = createWindow("BUTTON", "Salva PBS", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 0, 0, 0, 0, hwndDataAdvancedPanel, idDataRawSave, hInst)

	// Impostazioni Pokémon Standard: solo controlli guidati/no-code.
	hwndDataSettingsStandardPanel = createWindow("STATIC", "", WS_CHILD|WS_CLIPCHILDREN, 0, 0, 0, 0, hwndDataPanel, 2425, hInst)
	installDataChildForwarder(hwndDataSettingsStandardPanel, &dataSettingsStandardPanelOldWndProc)
	hwndDataSettingsTitle = createWindow("STATIC", "Generazione e meccaniche Pokémon", WS_CHILD|WS_VISIBLE, 0, 0, 0, 0, hwndDataSettingsStandardPanel, 2426, hInst)
	hwndDataSettingsInfo = createWindow("STATIC", "Scegli il profilo meccanico, la dimensione massima della squadra e quali trasformazioni speciali sono abilitate. I PBS personalizzati del progetto restano la fonte di verità e non vengono sostituiti.", WS_CHILD|WS_VISIBLE, 0, 0, 0, 0, hwndDataSettingsStandardPanel, 2427, hInst)
	hwndDataSettingsGenLabel = createWindow("STATIC", "Generazione meccaniche", WS_CHILD|WS_VISIBLE, 0, 0, 0, 0, hwndDataSettingsStandardPanel, 2428, hInst)
	hwndDataSettingsGen = createWindow("COMBOBOX", "", WS_CHILD|WS_VISIBLE|WS_TABSTOP|CBS_DROPDOWNLIST|WS_VSCROLL, 0, 0, 0, 280, hwndDataSettingsStandardPanel, idDataSettingsGen, hInst)
	for gen := 1; gen <= 8; gen++ {
		comboAdd(hwndDataSettingsGen, fmt.Sprintf("%dª generazione", gen))
	}
	hwndDataSettingsPartyLabel = createWindow("STATIC", "Numero massimo Pokémon in squadra", WS_CHILD|WS_VISIBLE, 0, 0, 0, 0, hwndDataSettingsStandardPanel, 2441, hInst)
	hwndDataSettingsPartySize = createWindow("COMBOBOX", "", WS_CHILD|WS_VISIBLE|WS_TABSTOP|CBS_DROPDOWNLIST|WS_VSCROLL, 0, 0, 0, 280, hwndDataSettingsStandardPanel, idDataSettingsPartySize, hInst)
	for size := mechanicsMinPartySize; size <= mechanicsMaxPartySize; size++ {
		comboAdd(hwndDataSettingsPartySize, fmt.Sprintf("%d Pokémon", size))
	}
	hwndDataSettingsMega = createWindow("BUTTON", "Attiva Mega Evoluzioni", WS_CHILD|WS_VISIBLE|WS_TABSTOP|bsAutoCheckboxData, 0, 0, 0, 0, hwndDataSettingsStandardPanel, idDataSettingsMega, hInst)
	hwndDataSettingsAwaken = createWindow("BUTTON", "Attiva Risvegli", WS_CHILD|WS_VISIBLE|WS_TABSTOP|bsAutoCheckboxData, 0, 0, 0, 0, hwndDataSettingsStandardPanel, idDataSettingsAwaken, hInst)
	hwndDataSettingsApply = createWindow("BUTTON", "Applica impostazioni", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 0, 0, 0, 0, hwndDataSettingsStandardPanel, idDataSettingsApply, hInst)
	hwndDataSettingsStatus = createWindow("STATIC", "", WS_CHILD|WS_VISIBLE, 0, 0, 0, 0, hwndDataSettingsStandardPanel, 2429, hInst)

	// Impostazioni Pokémon Advanced: per ora espone la configurazione tecnica
	// reale plm_mechanics.json. Non viene inventato codice Python scollegato dal
	// runtime: quando esisterà un consumer Python dedicato, sarà aggiunto qui.
	hwndDataSettingsAdvancedPanel = createWindow("STATIC", "", WS_CHILD|WS_CLIPCHILDREN, 0, 0, 0, 0, hwndDataPanel, 2430, hInst)
	installDataChildForwarder(hwndDataSettingsAdvancedPanel, &dataSettingsAdvancedPanelOldWndProc)
	hwndDataSettingsRawLabel = createWindow("STATIC", "Configurazione tecnica reale — converted\\data\\plm_mechanics.json", WS_CHILD|WS_VISIBLE, 0, 0, 0, 0, hwndDataSettingsAdvancedPanel, 2431, hInst)
	hwndDataSettingsRawEditor = createWindow("EDIT", "", WS_CHILD|WS_VISIBLE|WS_BORDER|WS_TABSTOP|WS_VSCROLL|WS_HSCROLL|ES_MULTILINE|ES_AUTOVSCROLL|ES_WANTRETURN, 0, 0, 0, 0, hwndDataSettingsAdvancedPanel, idDataSettingsRaw, hInst)
	pSendMessageW.Call(uintptr(hwndDataSettingsRawEditor), 0x00C5, 4*1024*1024, 0)
	hwndDataSettingsRawSave = createWindow("BUTTON", "Salva configurazione", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 0, 0, 0, 0, hwndDataSettingsAdvancedPanel, idDataSettingsRawSave, hInst)
	hwndDataSettingsRawReload = createWindow("BUTTON", "Ricarica", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 0, 0, 0, 0, hwndDataSettingsAdvancedPanel, idDataSettingsRawReload, hInst)
	hwndDataSettingsAdvancedStatus = createWindow("STATIC", "", WS_CHILD|WS_VISIBLE, 0, 0, 0, 0, hwndDataSettingsAdvancedPanel, 2432, hInst)

	setButtonCheck(hwndDataPBSView, true)
	setButtonCheck(hwndDataPokemonSettings, false)
	setButtonCheck(hwndDataStandard, true)
	setButtonCheck(hwndDataAdvanced, false)
	showControl(hwndDataPanel, false)
	refreshDataModeUI()
}

func showDataEditor(show bool) {
	showControl(hwndDataPanel, show)
	if !show {
		return
	}
	if dataPokemonSettingsMode {
		refreshDataSettingsUI(true)
	} else {
		loadDataEditorForProject()
	}
	refreshDataModeUI()
	layout(hwndMain)
}

func layoutDataEditor(x, y, w, h int32) {
	if hwndDataPanel == 0 || w < 520 || h < 360 {
		return
	}
	moveControl(hwndDataPanel, x, y, w, h)

	pad := int32(14)
	topY := int32(16)
	moveControl(hwndDataPBSView, pad, topY, 118, 28)
	moveControl(hwndDataPokemonSettings, pad+124, topY, 166, 28)
	moveControl(hwndDataStandard, w-pad-196, topY, 94, 28)
	moveControl(hwndDataAdvanced, w-pad-98, topY, 94, 28)
	moveControl(hwndDataIntro, pad, 50, w-pad*2, 22)

	contentTop := int32(82)
	if !dataPokemonSettingsMode {
		labelW := int32(90)
		moveControl(hwndDataFileLabel, pad, contentTop+5, labelW, 20)
		moveControl(hwndDataFile, pad+labelW, contentTop, w-pad*2-labelW, 300)
		contentTop += 38
	}

	contentX := pad
	contentW := w - pad*2
	contentH := h - contentTop - pad
	if contentW < 240 {
		contentW = 240
	}
	if contentH < 180 {
		contentH = 180
	}

	if dataPokemonSettingsMode {
		if dataAdvancedMode {
			moveControl(hwndDataSettingsAdvancedPanel, contentX, contentTop, contentW, contentH)
			moveControl(hwndDataSettingsRawLabel, 0, 0, contentW, 22)
			buttonH := int32(28)
			statusH := int32(20)
			bottomGap := int32(8)
			buttonsY := contentH - buttonH
			statusY := buttonsY - bottomGap - statusH
			editorH := statusY - 8 - 28
			if editorH < 100 {
				editorH = 100
			}
			moveControl(hwndDataSettingsRawEditor, 0, 28, contentW, editorH)
			moveControl(hwndDataSettingsAdvancedStatus, 0, statusY, contentW-330, statusH)
			moveControl(hwndDataSettingsRawReload, contentW-322, buttonsY, 150, buttonH)
			moveControl(hwndDataSettingsRawSave, contentW-166, buttonsY, 166, buttonH)
		} else {
			moveControl(hwndDataSettingsStandardPanel, contentX, contentTop, contentW, contentH)
			moveControl(hwndDataSettingsTitle, 0, 0, contentW, 24)
			moveControl(hwndDataSettingsInfo, 0, 30, contentW, 44)
			moveControl(hwndDataSettingsGenLabel, 0, 92, 220, 22)
			moveControl(hwndDataSettingsGen, 250, 88, 220, 260)
			moveControl(hwndDataSettingsPartyLabel, 0, 132, 240, 22)
			moveControl(hwndDataSettingsPartySize, 250, 128, contentW-250, 220)
			moveControl(hwndDataSettingsMega, 0, 168, 250, 22)
			moveControl(hwndDataSettingsAwaken, 270, 168, 250, 22)
			moveControl(hwndDataSettingsApply, 0, 204, 190, 28)
			moveControl(hwndDataSettingsStatus, 0, 244, contentW, contentH-244)
		}
		return
	}

	if dataAdvancedMode {
		moveControl(hwndDataAdvancedPanel, contentX, contentTop, contentW, contentH)
		buttonH := int32(28)
		statusH := int32(20)
		gap := int32(8)
		buttonY := contentH - buttonH
		statusY := buttonY - gap - statusH
		editorH := statusY - gap
		if editorH < 100 {
			editorH = 100
		}
		moveControl(hwndDataRawEditor, 0, 0, contentW, editorH)
		moveControl(hwndDataAdvancedStatus, 0, statusY, contentW-178, statusH)
		moveControl(hwndDataAdvancedSave, contentW-170, buttonY, 170, buttonH)
		return
	}

	moveControl(hwndDataStandardPanel, contentX, contentTop, contentW, contentH)
	labelW := int32(118)
	ctrlX := labelW
	ctrlW := contentW - ctrlX
	if ctrlW < 180 {
		ctrlW = 180
	}
	yy := int32(0)
	moveControl(hwndDataRecordLabel, 0, yy+4, labelW-8, 20)
	moveControl(hwndDataRecord, ctrlX, yy, ctrlW, 320)
	yy += 38
	moveControl(hwndDataFieldLabel, 0, yy+4, labelW-8, 20)
	moveControl(hwndDataField, ctrlX, yy, ctrlW, 300)
	yy += 38
	moveControl(hwndDataValueLabel, 0, yy+4, labelW-8, 20)
	moveControl(hwndDataValue, ctrlX, yy, ctrlW, 320)
	yy += 52
	moveControl(hwndDataAddTitle, 0, yy, contentW, 22)
	yy += 30
	moveControl(hwndDataAddFieldLbl, 0, yy+4, labelW-8, 20)
	moveControl(hwndDataAddField, ctrlX, yy, ctrlW, 300)
	yy += 38
	moveControl(hwndDataAddValueLbl, 0, yy+4, labelW-8, 20)
	moveControl(hwndDataAddValue, ctrlX, yy, ctrlW, 25)
	yy += 38
	moveControl(hwndDataNewIDLbl, 0, yy+4, labelW-8, 20)
	moveControl(hwndDataNewRecordID, ctrlX, yy, ctrlW, 25)

	buttonGap := int32(8)
	buttonY1 := contentH - 86
	buttonY2 := contentH - 52
	if buttonY1 < yy+42 {
		buttonY1 = yy + 42
		buttonY2 = buttonY1 + 34
	}
	b1 := (contentW - buttonGap*2) / 3
	moveControl(hwndDataApply, 0, buttonY1, b1, 28)
	moveControl(hwndDataAdd, b1+buttonGap, buttonY1, b1, 28)
	moveControl(hwndDataRemove, (b1+buttonGap)*2, buttonY1, contentW-(b1+buttonGap)*2, 28)
	b2 := (contentW - buttonGap*3) / 4
	moveControl(hwndDataNewRecord, 0, buttonY2, b2, 28)
	moveControl(hwndDataDuplicate, b2+buttonGap, buttonY2, b2, 28)
	moveControl(hwndDataDeleteRecord, (b2+buttonGap)*2, buttonY2, b2, 28)
	moveControl(hwndDataStandardSave, (b2+buttonGap)*3, buttonY2, contentW-(b2+buttonGap)*3, 28)
	moveControl(hwndDataStandardStatus, 0, contentH-18, contentW, 18)
}

func refreshDataModeUI() {
	if hwndDataPanel == 0 {
		return
	}
	setButtonCheck(hwndDataPBSView, !dataPokemonSettingsMode)
	setButtonCheck(hwndDataPokemonSettings, dataPokemonSettingsMode)
	setButtonCheck(hwndDataStandard, !dataAdvancedMode)
	setButtonCheck(hwndDataAdvanced, dataAdvancedMode)

	showControl(hwndDataFileLabel, !dataPokemonSettingsMode)
	showControl(hwndDataFile, !dataPokemonSettingsMode)

	// Regola anti-sovrapposizione: prima nascondi TUTTE le superfici, poi
	// mostra esattamente quella selezionata.
	showControl(hwndDataStandardPanel, false)
	showControl(hwndDataAdvancedPanel, false)
	showControl(hwndDataSettingsStandardPanel, false)
	showControl(hwndDataSettingsAdvancedPanel, false)

	if dataPokemonSettingsMode {
		if dataAdvancedMode {
			showControl(hwndDataSettingsAdvancedPanel, true)
			setText(hwndDataIntro, "Configurazione tecnica del progetto. La modalità Standard resta No-Code e non mostra dati grezzi.")
			refreshDataSettingsUI(false)
		} else {
			showControl(hwndDataSettingsStandardPanel, true)
			setText(hwndDataIntro, "Configura le meccaniche Pokémon tramite campi e menu. Nessun codice tecnico è esposto in Standard.")
			refreshDataSettingsUI(false)
		}
	} else {
		if dataAdvancedMode {
			showControl(hwndDataAdvancedPanel, true)
			setText(hwndDataIntro, "Editor diretto del file PBS reale selezionato. Le modifiche vengono scritte nel PBS del progetto.")
			syncDataRawEditorFromDocument(false)
		} else {
			showControl(hwndDataStandardPanel, true)
			setText(hwndDataIntro, "Scegli tipo dati, voce, campo e valore dai menu. I PBS vengono aggiornati dietro le quinte.")
		}
		updateDataStatus()
	}
}

func loadDataEditorForProject() {
	if dataRefreshingUI {
		return
	}
	dataRefreshingUI = true
	defer func() { dataRefreshingUI = false }()

	clearCombo(hwndDataFile)
	dataFiles = nil
	if currentProject == "" {
		dataDoc = nil
		dataLoadedProject = ""
		dataCurrentFile = -1
		clearDataRecordAndFieldUI()
		setText(hwndDataStandardStatus, "Apri o importa un progetto per usare i dati PBS.")
		setText(hwndDataAdvancedStatus, "Apri o importa un progetto per usare i dati PBS.")
		return
	}

	dataFiles = discoverDataPBSFiles(currentProject)
	for _, name := range dataFiles {
		comboAdd(hwndDataFile, friendlyDataFileLabel(name))
	}
	if len(dataFiles) == 0 {
		dataDoc = nil
		dataLoadedProject = currentProject
		dataCurrentFile = -1
		clearDataRecordAndFieldUI()
		setText(hwndDataStandardStatus, "Nessun file PBS trovato nel progetto.")
		setText(hwndDataAdvancedStatus, "Nessun file PBS trovato nel progetto.")
		return
	}

	wanted := 0
	if dataDoc != nil && dataLoadedProject == currentProject {
		base := filepath.Base(dataDoc.Path)
		for i, name := range dataFiles {
			if strings.EqualFold(name, base) {
				wanted = i
				break
			}
		}
	}
	pSendMessageW.Call(uintptr(hwndDataFile), CB_SETCURSEL, uintptr(wanted), 0)
	dataCurrentFile = wanted

	if dataDoc != nil && dataLoadedProject == currentProject && strings.EqualFold(filepath.Base(dataDoc.Path), dataFiles[wanted]) {
		preferred := dataCurrentRecord
		if preferred < 0 {
			preferred = 0
		}
		populateDataRecordCombo(preferred)
		populateDataAddFieldCombo()
		updateDataStatus()
		return
	}
	if err := loadDataFileByIndex(wanted, false); err != nil {
		dataDoc = nil
		dataLoadedProject = currentProject
		clearDataRecordAndFieldUI()
		setText(hwndDataStandardStatus, "Errore caricamento dati: "+err.Error())
		setText(hwndDataAdvancedStatus, "Errore caricamento dati: "+err.Error())
	}
}

func loadDataFileByIndex(index int, savePrevious bool) error {
	if index < 0 || index >= len(dataFiles) {
		return fmt.Errorf("tipo dati non valido")
	}
	if savePrevious && dataDoc != nil && dataDoc.Dirty {
		if err := saveDataCurrent(); err != nil {
			return err
		}
	}
	path := dataPBSFilePath(currentProject, dataFiles[index])
	doc, err := loadDataPBSDocument(path)
	if err != nil {
		return err
	}
	dataDoc = doc
	dataLoadedProject = currentProject
	dataCurrentFile = index
	dataCurrentRecord = -1
	dataCurrentField = -1
	dataRawEdited = false
	populateDataRecordCombo(0)
	populateDataAddFieldCombo()
	syncDataRawEditorFromDocument(true)
	updateDataStatus()
	return nil
}

func clearDataRecordAndFieldUI() {
	clearCombo(hwndDataRecord)
	clearCombo(hwndDataField)
	clearCombo(hwndDataValue)
	clearCombo(hwndDataAddField)
	setText(hwndDataValue, "")
	setText(hwndDataAddValue, "")
	setText(hwndDataNewRecordID, "")
	dataFields = nil
	dataAddFieldKeys = nil
	dataCurrentRecord = -1
	dataCurrentField = -1
	dataValueEdited = false
	if dataDoc == nil {
		prev := dataRefreshingUI
		dataRefreshingUI = true
		setText(hwndDataRawEditor, "")
		dataRefreshingUI = prev
		dataRawEdited = false
	}
	updateDataControlsEnabled()
}

func populateDataRecordCombo(preferred int) {
	clearCombo(hwndDataRecord)
	if dataDoc == nil || len(dataDoc.Records) == 0 {
		dataCurrentRecord = -1
		populateDataFieldCombo(-1, 0)
		updateDataControlsEnabled()
		return
	}
	for i := range dataDoc.Records {
		comboAdd(hwndDataRecord, dataDoc.recordDisplayName(i))
	}
	if preferred < 0 || preferred >= len(dataDoc.Records) {
		preferred = 0
	}
	pSendMessageW.Call(uintptr(hwndDataRecord), CB_SETCURSEL, uintptr(preferred), 0)
	dataCurrentRecord = preferred
	populateDataFieldCombo(preferred, 0)
	updateDataControlsEnabled()
}

func populateDataFieldCombo(recordIndex, preferred int) {
	clearCombo(hwndDataField)
	dataFields = nil
	dataCurrentField = -1
	if dataDoc == nil || recordIndex < 0 || recordIndex >= len(dataDoc.Records) {
		clearCombo(hwndDataValue)
		setText(hwndDataValue, "")
		return
	}
	dataFields = dataDoc.fields(recordIndex)
	for _, f := range dataFields {
		comboAdd(hwndDataField, friendlyDataFieldLabel(f))
	}
	if len(dataFields) == 0 {
		return
	}
	if preferred < 0 || preferred >= len(dataFields) {
		preferred = 0
	}
	pSendMessageW.Call(uintptr(hwndDataField), CB_SETCURSEL, uintptr(preferred), 0)
	dataCurrentField = preferred
	loadDataCurrentFieldValue()
}

func loadDataCurrentFieldValue() {
	prev := dataRefreshingUI
	dataRefreshingUI = true
	defer func() { dataRefreshingUI = prev }()
	clearCombo(hwndDataValue)
	setText(hwndDataValue, "")
	if dataDoc == nil || dataCurrentField < 0 || dataCurrentField >= len(dataFields) {
		dataValueEdited = false
		return
	}
	f := dataFields[dataCurrentField]
	for _, value := range dataValueSuggestions(f) {
		comboAdd(hwndDataValue, value)
	}
	setText(hwndDataValue, f.Value)
	dataValueEdited = false
}

func dataValueSuggestions(field dataPBSField) []string {
	if dataDoc == nil || field.IsHeader || field.Raw || strings.TrimSpace(field.Key) == "" {
		return nil
	}
	seen := map[string]bool{}
	values := make([]string, 0, 32)
	add := func(v string) {
		v = strings.TrimSpace(v)
		if v == "" || seen[strings.ToLower(v)] || len(values) >= 80 {
			return
		}
		seen[strings.ToLower(v)] = true
		values = append(values, v)
	}
	key := strings.ToLower(field.Key)
	if strings.Contains(key, "boolean") || strings.HasPrefix(key, "is") || strings.HasPrefix(key, "has") || key == "consumable" {
		add("true")
		add("false")
	}
	for i := range dataDoc.Records {
		for _, f := range dataDoc.fields(i) {
			if !f.IsHeader && !f.Raw && strings.EqualFold(f.Key, field.Key) {
				add(f.Value)
			}
		}
	}
	sort.SliceStable(values, func(i, j int) bool { return strings.ToLower(values[i]) < strings.ToLower(values[j]) })
	return values
}

func populateDataAddFieldCombo() {
	clearCombo(hwndDataAddField)
	dataAddFieldKeys = nil
	if dataDoc == nil {
		return
	}
	seen := map[string]bool{}
	for i := range dataDoc.Records {
		for _, f := range dataDoc.fields(i) {
			if f.IsHeader || f.Raw || f.Indent != "" || strings.TrimSpace(f.Key) == "" {
				continue
			}
			lk := strings.ToLower(f.Key)
			if seen[lk] {
				continue
			}
			seen[lk] = true
			dataAddFieldKeys = append(dataAddFieldKeys, f.Key)
		}
	}
	sort.SliceStable(dataAddFieldKeys, func(i, j int) bool {
		return strings.ToLower(friendlyDataKeyLabel(dataAddFieldKeys[i])) < strings.ToLower(friendlyDataKeyLabel(dataAddFieldKeys[j]))
	})
	for _, key := range dataAddFieldKeys {
		comboAdd(hwndDataAddField, friendlyDataKeyLabel(key))
	}
	setText(hwndDataAddField, "")
}

func dataCurrentRecordAllowsFieldStructure() bool {
	return dataDoc != nil && dataDoc.recordAllowsFlatFieldStructure(dataCurrentRecord)
}

func dataSelectedFieldRemovable() bool {
	if !dataCurrentRecordAllowsFieldStructure() || dataCurrentField < 0 || dataCurrentField >= len(dataFields) {
		return false
	}
	return !dataFields[dataCurrentField].IsHeader
}

func applyPendingPBSUIEdits() error {
	if dataDoc == nil || dataPokemonSettingsMode {
		return nil
	}
	if dataAdvancedMode {
		if dataRawEdited {
			return applyDataRawEditorToDocument()
		}
		return nil
	}
	if dataValueEdited {
		return applyDataCurrentField(false)
	}
	return nil
}

func selectedNewFieldKey() string {
	sel := comboSel(hwndDataAddField)
	if sel >= 0 && sel < len(dataAddFieldKeys) {
		return dataAddFieldKeys[sel]
	}
	typed := strings.TrimSpace(getText(hwndDataAddField))
	for _, key := range dataAddFieldKeys {
		if strings.EqualFold(typed, friendlyDataKeyLabel(key)) || strings.EqualFold(typed, key) {
			return key
		}
	}
	return typed
}

func applyDataCurrentField(showStatus bool) error {
	if dataDoc == nil || dataCurrentRecord < 0 || dataCurrentField < 0 || dataCurrentField >= len(dataFields) {
		return fmt.Errorf("seleziona prima una voce e un campo")
	}
	f := dataFields[dataCurrentField]
	newValue := getText(hwndDataValue)
	if !dataValueEdited && newValue == f.Value {
		return nil
	}
	fieldIndex := dataCurrentField
	recordIndex := dataCurrentRecord
	if err := dataDoc.setField(recordIndex, f, f.Key, newValue, false); err != nil {
		return err
	}
	populateDataRecordCombo(recordIndex)
	populateDataFieldCombo(recordIndex, fieldIndex)
	populateDataAddFieldCombo()
	dataValueEdited = false
	updateDataStatus()
	if showStatus {
		setToolbarStatus("Vista Dati: campo aggiornato. Premi Salva dati per scriverlo nel progetto.")
	}
	return nil
}

func addDataFieldFromForm() error {
	if dataDoc == nil || dataCurrentRecord < 0 {
		return fmt.Errorf("seleziona prima una voce")
	}
	if !dataCurrentRecordAllowsFieldStructure() {
		return fmt.Errorf("questa voce usa una struttura PBS complessa; modifica i valori esistenti oppure usa Avanzata/la vista dedicata")
	}
	if dataValueEdited {
		if err := applyDataCurrentField(false); err != nil {
			return err
		}
	}
	key := selectedNewFieldKey()
	value := getText(hwndDataAddValue)
	if err := dataDoc.addField(dataCurrentRecord, key, value); err != nil {
		return err
	}
	setText(hwndDataAddField, "")
	setText(hwndDataAddValue, "")
	populateDataRecordCombo(dataCurrentRecord)
	populateDataFieldCombo(dataCurrentRecord, len(dataDoc.fields(dataCurrentRecord))-1)
	populateDataAddFieldCombo()
	updateDataStatus()
	return nil
}

func removeDataCurrentField() error {
	if dataDoc == nil || dataCurrentRecord < 0 || dataCurrentField < 0 || dataCurrentField >= len(dataFields) {
		return fmt.Errorf("seleziona prima un campo")
	}
	if !dataSelectedFieldRemovable() {
		return fmt.Errorf("questa voce usa una struttura PBS complessa; la rimozione strutturale è disponibile in Avanzata/la vista dedicata")
	}
	f := dataFields[dataCurrentField]
	if err := dataDoc.deleteField(f); err != nil {
		return err
	}
	preferred := dataCurrentField
	if preferred >= len(dataDoc.fields(dataCurrentRecord)) {
		preferred--
	}
	populateDataRecordCombo(dataCurrentRecord)
	populateDataFieldCombo(dataCurrentRecord, preferred)
	populateDataAddFieldCombo()
	updateDataStatus()
	return nil
}

func createDataRecordFromForm(duplicate bool) error {
	if dataDoc == nil {
		return fmt.Errorf("seleziona prima un tipo di dati")
	}
	if dataValueEdited {
		if err := applyDataCurrentField(false); err != nil {
			return err
		}
	}
	id := strings.TrimSpace(getText(hwndDataNewRecordID))
	if id == "" {
		return fmt.Errorf("scrivi prima l'ID della nuova voce")
	}
	var idx int
	var err error
	if duplicate {
		if dataCurrentRecord < 0 {
			return fmt.Errorf("seleziona la voce da duplicare")
		}
		idx, err = dataDoc.duplicateRecord(dataCurrentRecord, id)
	} else {
		idx, err = dataDoc.addRecord(id)
	}
	if err != nil {
		return err
	}
	setText(hwndDataNewRecordID, "")
	populateDataRecordCombo(idx)
	populateDataAddFieldCombo()
	updateDataStatus()
	return nil
}

func deleteDataCurrentRecord() error {
	if dataDoc == nil || dataCurrentRecord < 0 {
		return fmt.Errorf("seleziona prima una voce")
	}
	idx := dataCurrentRecord
	if err := dataDoc.deleteRecord(idx); err != nil {
		return err
	}
	if idx >= len(dataDoc.Records) {
		idx = len(dataDoc.Records) - 1
	}
	populateDataRecordCombo(idx)
	populateDataAddFieldCombo()
	updateDataStatus()
	return nil
}

func dataDocumentText() string {
	if dataDoc == nil {
		return ""
	}
	return strings.Join(dataDoc.Lines, "\r\n")
}

func syncDataRawEditorFromDocument(force bool) {
	if hwndDataRawEditor == 0 || dataDoc == nil {
		return
	}
	if dataRawEdited && !force {
		return
	}
	prev := dataRefreshingUI
	dataRefreshingUI = true
	setText(hwndDataRawEditor, dataDocumentText())
	dataRefreshingUI = prev
	dataRawEdited = false
}

func applyDataRawEditorToDocument() error {
	if dataDoc == nil || !dataRawEdited {
		return nil
	}
	raw := getText(hwndDataRawEditor)
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	raw = strings.ReplaceAll(raw, "\r", "\n")
	dataDoc.Lines = strings.Split(raw, "\n")
	dataDoc.Dirty = true
	dataDoc.reparse()
	dataRawEdited = false
	return nil
}

func rebuildStandardDataUIFromDocument() {
	if dataDoc == nil {
		clearDataRecordAndFieldUI()
		return
	}
	preferred := dataCurrentRecord
	if preferred < 0 || preferred >= len(dataDoc.Records) {
		preferred = 0
	}
	populateDataRecordCombo(preferred)
	populateDataAddFieldCombo()
}

func saveDataCurrent() error {
	if dataDoc == nil {
		return nil
	}
	if dataAdvancedMode && !dataPokemonSettingsMode && dataRawEdited {
		if err := applyDataRawEditorToDocument(); err != nil {
			return err
		}
	}
	if !dataAdvancedMode && !dataPokemonSettingsMode && dataValueEdited {
		if err := applyDataCurrentField(false); err != nil {
			return err
		}
	}
	if err := dataDoc.save(); err != nil {
		return err
	}
	rebuildStandardDataUIFromDocument()
	syncDataRawEditorFromDocument(true)
	updateDataStatus()
	return nil
}

func updateDataControlsEnabled() {
	hasDoc := dataDoc != nil
	hasRecord := hasDoc && dataCurrentRecord >= 0 && dataCurrentRecord < len(dataDoc.Records)
	hasField := hasRecord && dataCurrentField >= 0 && dataCurrentField < len(dataFields)
	standard := !dataAdvancedMode && !dataPokemonSettingsMode
	structuralFieldEdit := standard && hasRecord && dataCurrentRecordAllowsFieldStructure()
	removableField := standard && hasField && dataSelectedFieldRemovable()
	sectionDocument := hasDoc && len(dataDoc.Records) > 0
	for _, p := range []struct {
		h       syscall.Handle
		enabled bool
	}{
		{hwndDataFile, len(dataFiles) > 0 && !dataPokemonSettingsMode},
		{hwndDataRecord, standard && hasDoc}, {hwndDataField, standard && hasRecord}, {hwndDataValue, standard && hasField},
		{hwndDataApply, standard && hasField}, {hwndDataRemove, removableField},
		{hwndDataAddField, structuralFieldEdit}, {hwndDataAddValue, structuralFieldEdit}, {hwndDataAdd, structuralFieldEdit},
		{hwndDataNewRecordID, standard && sectionDocument}, {hwndDataNewRecord, standard && sectionDocument}, {hwndDataDuplicate, standard && hasRecord},
		{hwndDataDeleteRecord, standard && hasRecord}, {hwndDataRawEditor, dataAdvancedMode && !dataPokemonSettingsMode && hasDoc},
		{hwndDataStandardSave, standard && hasDoc}, {hwndDataAdvancedSave, dataAdvancedMode && !dataPokemonSettingsMode && hasDoc},
	} {
		if p.h != 0 {
			v := uintptr(0)
			if p.enabled {
				v = 1
			}
			pEnableWindow.Call(uintptr(p.h), v)
		}
	}
}

func updateDataStatus() {
	updateDataControlsEnabled()
	if dataDoc == nil {
		setText(hwndDataStandardStatus, "Nessun dato caricato.")
		setText(hwndDataAdvancedStatus, "Nessun dato caricato.")
		return
	}
	dirty := ""
	if dataDoc.Dirty || dataRawEdited || dataValueEdited {
		dirty = " • modifiche da salvare"
	}
	structure := ""
	if !dataAdvancedMode && !dataPokemonSettingsMode && dataCurrentRecord >= 0 && !dataCurrentRecordAllowsFieldStructure() {
		structure = " • struttura complessa: Aggiungi/Rimuovi campo protetti"
	}
	setText(hwndDataStandardStatus, fmt.Sprintf("%d voci%s%s", len(dataDoc.Records), dirty, structure))
	setText(hwndDataAdvancedStatus, fmt.Sprintf("%s • %d righe%s", dataDoc.Path, len(dataDoc.Lines), dirty))
}

func setDataSettingsCheckbox(hwnd syscall.Handle, checked bool) {
	if hwnd == 0 {
		return
	}
	state := uintptr(bstUncheckedData)
	if checked {
		state = uintptr(bstCheckedData)
	}
	pSendMessageW.Call(uintptr(hwnd), bmSetCheckData, state, 0)
}

func dataSettingsCheckboxChecked(hwnd syscall.Handle) bool {
	if hwnd == 0 {
		return false
	}
	state, _, _ := pSendMessageW.Call(uintptr(hwnd), bmGetCheckData, 0, 0)
	return state == uintptr(bstCheckedData)
}

func refreshDataSettingsUI(forceRaw bool) {
	if currentProject == "" {
		pSendMessageW.Call(uintptr(hwndDataSettingsGen), CB_SETCURSEL, ^uintptr(0), 0)
		pSendMessageW.Call(uintptr(hwndDataSettingsPartySize), CB_SETCURSEL, ^uintptr(0), 0)
		setDataSettingsCheckbox(hwndDataSettingsMega, false)
		setDataSettingsCheckbox(hwndDataSettingsAwaken, false)
		setText(hwndDataSettingsStatus, "Apri o importa un progetto per configurare le meccaniche Pokémon.")
		if forceRaw || !dataSettingsRawEdited {
			prev := dataRefreshingUI
			dataRefreshingUI = true
			setText(hwndDataSettingsRawEditor, "")
			dataRefreshingUI = prev
			dataSettingsRawEdited = false
		}
		setText(hwndDataSettingsAdvancedStatus, "Nessun progetto aperto.")
		dataSettingsStandardEdited = false
		return
	}
	state := loadMechanicsSettings(currentProject)
	idx := state.MechanicsGeneration - 1
	if idx < 0 || idx > 7 {
		idx = 7
	}
	partyIdx := state.MaxPartySize - mechanicsMinPartySize
	if partyIdx < 0 || partyIdx > mechanicsMaxPartySize-mechanicsMinPartySize {
		partyIdx = mechanicsDefaultPartySize - mechanicsMinPartySize
	}
	if forceRaw || !dataSettingsStandardEdited {
		pSendMessageW.Call(uintptr(hwndDataSettingsGen), CB_SETCURSEL, uintptr(idx), 0)
		pSendMessageW.Call(uintptr(hwndDataSettingsPartySize), CB_SETCURSEL, uintptr(partyIdx), 0)
		setDataSettingsCheckbox(hwndDataSettingsMega, state.MegaEvolutionsEnabled)
		setDataSettingsCheckbox(hwndDataSettingsAwaken, state.AwakeningsEnabled)
	}
	ref := "nessun pacchetto PBS generazionale separato"
	if state.GenerationPBSAvailable {
		ref = filepath.FromSlash(state.ReferencePBSRoot)
	}
	standardDirty := ""
	if dataSettingsStandardEdited {
		standardDirty = "\r\n\r\nModifica selezionata ma non ancora applicata."
	}
	megaText := "disattivate"
	if state.MegaEvolutionsEnabled {
		megaText = "attive"
	}
	awakenText := "disattivati"
	if state.AwakeningsEnabled {
		awakenText = "attivi"
	}
	setText(hwndDataSettingsStatus, fmt.Sprintf(
		"Profilo attivo: %dª generazione\r\nMECHANICS_GENERATION = %d\r\nNumero massimo Pokémon in squadra: %d\r\nMega Evoluzioni: %s\r\nRisvegli: %s\r\nPBS progetto: %s\r\nRiferimento generazionale: %s\r\n\r\nIl PBS del progetto resta invariato; la generazione selezionata non sostituisce i dati custom.%s",
		state.MechanicsGeneration, state.MechanicsGeneration, state.MaxPartySize, megaText, awakenText, filepath.FromSlash(state.ActivePBSRoot), ref, standardDirty))
	if forceRaw || !dataSettingsRawEdited {
		b, _ := json.MarshalIndent(state, "", "  ")
		prev := dataRefreshingUI
		dataRefreshingUI = true
		setText(hwndDataSettingsRawEditor, string(b)+"\r\n")
		dataRefreshingUI = prev
		dataSettingsRawEdited = false
	}
	path := mechanicsSettingsPath(currentProject)
	dirty := ""
	if dataSettingsRawEdited {
		dirty = " • modifiche non salvate"
	}
	setText(hwndDataSettingsAdvancedStatus, path+dirty)
}

func applyDataSettingsStandard() error {
	if currentProject == "" {
		return fmt.Errorf("nessun progetto aperto")
	}
	idx := comboSel(hwndDataSettingsGen)
	if idx < 0 || idx > 7 {
		return fmt.Errorf("seleziona una generazione da 1 a 8")
	}
	partyIdx := comboSel(hwndDataSettingsPartySize)
	if partyIdx < 0 || partyIdx > mechanicsMaxPartySize-mechanicsMinPartySize {
		return fmt.Errorf("seleziona una dimensione squadra da %d a %d Pokémon", mechanicsMinPartySize, mechanicsMaxPartySize)
	}
	maxPartySize := mechanicsMinPartySize + partyIdx
	megaEnabled := dataSettingsCheckboxChecked(hwndDataSettingsMega)
	awakeningsEnabled := dataSettingsCheckboxChecked(hwndDataSettingsAwaken)
	if _, err := saveMechanicsSettingsOptionsExtended(currentProject, idx+1, maxPartySize, megaEnabled, awakeningsEnabled); err != nil {
		return err
	}
	dataSettingsRawEdited = false
	dataSettingsStandardEdited = false
	refreshDataSettingsUI(true)
	setToolbarStatus(fmt.Sprintf("Impostazioni Pokémon: %dª generazione, squadra max %d, Mega=%t, Risvegli=%t.", idx+1, maxPartySize, megaEnabled, awakeningsEnabled))
	return nil
}

func saveDataSettingsRaw() error {
	if currentProject == "" {
		return fmt.Errorf("nessun progetto aperto")
	}
	raw := strings.TrimSpace(getText(hwndDataSettingsRawEditor))
	if raw == "" {
		return fmt.Errorf("la configurazione non può essere vuota")
	}
	cfg := mechanicsProjectSettings{
		MegaEvolutionsEnabled: true,
		AwakeningsEnabled:     true,
	}
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return fmt.Errorf("JSON non valido: %w", err)
	}
	if cfg.MechanicsGeneration < 1 || cfg.MechanicsGeneration > 8 {
		return fmt.Errorf("mechanics_generation deve essere compreso tra 1 e 8")
	}
	if cfg.MaxPartySize == 0 {
		cfg.MaxPartySize = mechanicsDefaultPartySize
	}
	if cfg.MaxPartySize < mechanicsMinPartySize || cfg.MaxPartySize > mechanicsMaxPartySize {
		return fmt.Errorf("max_party_size deve essere compreso tra %d e %d", mechanicsMinPartySize, mechanicsMaxPartySize)
	}
	if _, err := saveMechanicsSettingsOptionsExtended(currentProject, cfg.MechanicsGeneration, cfg.MaxPartySize, cfg.MegaEvolutionsEnabled, cfg.AwakeningsEnabled); err != nil {
		return err
	}
	dataSettingsRawEdited = false
	dataSettingsStandardEdited = false
	refreshDataSettingsUI(true)
	setToolbarStatus("Impostazioni Pokémon: configurazione tecnica salvata.")
	return nil
}

func confirmSettingsStandardLeave() bool {
	if !dataSettingsStandardEdited {
		return true
	}
	result := msgboxResult(
		"PML Studio - Impostazioni Pokémon",
		"Le impostazioni Pokémon selezionate non sono ancora state applicate.\r\n\r\nVuoi applicarle prima di cambiare schermata?",
		MB_YESNOCANCEL|MB_ICONINFORMATION,
	)
	switch result {
	case IDYES:
		if err := applyDataSettingsStandard(); err != nil {
			showDataError("Impossibile applicare le impostazioni Pokémon", err)
			return false
		}
		return true
	case IDNO:
		dataSettingsStandardEdited = false
		refreshDataSettingsUI(true)
		return true
	default:
		return false
	}
}

func confirmSettingsLeave() bool {
	if dataAdvancedMode {
		return confirmSettingsRawLeave()
	}
	return confirmSettingsStandardLeave()
}

func confirmSettingsRawLeave() bool {
	if !dataSettingsRawEdited {
		return true
	}
	result := msgboxResult(
		"PML Studio - Impostazioni Pokémon",
		"La configurazione tecnica contiene modifiche non salvate.\r\n\r\nVuoi salvarle prima di cambiare schermata?",
		MB_YESNOCANCEL|MB_ICONINFORMATION,
	)
	switch result {
	case IDYES:
		if err := saveDataSettingsRaw(); err != nil {
			showDataError("Impossibile salvare le impostazioni Pokémon", err)
			return false
		}
		return true
	case IDNO:
		dataSettingsRawEdited = false
		refreshDataSettingsUI(true)
		return true
	default:
		return false
	}
}

func handleDataEditorCommand(id uint16, notify uint16) bool {
	switch id {
	case idDataPBSView:
		if notify == BN_CLICKED {
			if dataPokemonSettingsMode && !confirmSettingsLeave() {
				return true
			}
			dataPokemonSettingsMode = false
			loadDataEditorForProject()
			refreshDataModeUI()
			layout(hwndMain)
		}
		return true
	case idDataPokemonSettings:
		if notify == BN_CLICKED {
			if !dataPokemonSettingsMode {
				if err := applyPendingPBSUIEdits(); err != nil {
					showDataError("Impossibile applicare le modifiche PBS", err)
					return true
				}
				rebuildStandardDataUIFromDocument()
			}
			dataPokemonSettingsMode = true
			refreshDataSettingsUI(true)
			refreshDataModeUI()
			layout(hwndMain)
		}
		return true
	case idDataStandardMode:
		if notify == BN_CLICKED {
			if dataPokemonSettingsMode {
				if dataAdvancedMode && !confirmSettingsRawLeave() {
					return true
				}
			} else if dataAdvancedMode {
				if err := applyPendingPBSUIEdits(); err != nil {
					showDataError("Impossibile applicare il PBS", err)
					return true
				}
				rebuildStandardDataUIFromDocument()
			}
			dataAdvancedMode = false
			refreshDataModeUI()
			layout(hwndMain)
		}
		return true
	case idDataAdvancedMode:
		if notify == BN_CLICKED {
			if dataPokemonSettingsMode {
				if !dataAdvancedMode && !confirmSettingsStandardLeave() {
					return true
				}
			} else if err := applyPendingPBSUIEdits(); err != nil {
				showDataError("Impossibile applicare il campo", err)
				return true
			}
			dataAdvancedMode = true
			if dataPokemonSettingsMode {
				refreshDataSettingsUI(true)
			} else {
				syncDataRawEditorFromDocument(true)
			}
			refreshDataModeUI()
			layout(hwndMain)
		}
		return true
	case idDataFile:
		if notify == CBN_SELCHANGE && !dataRefreshingUI && !dataPokemonSettingsMode {
			idx := comboSel(hwndDataFile)
			if idx >= 0 && idx < len(dataFiles) && idx != dataCurrentFile {
				if err := applyPendingPBSUIEdits(); err != nil {
					showDataError("Impossibile applicare le modifiche PBS", err)
					return true
				}
				if err := loadDataFileByIndex(idx, true); err != nil {
					showDataError("Impossibile cambiare tipo di dati", err)
				}
			}
		}
		return true
	case idDataRecord:
		if notify == CBN_SELCHANGE && !dataRefreshingUI {
			target := comboSel(hwndDataRecord)
			if dataValueEdited {
				if err := applyDataCurrentField(false); err != nil {
					showDataError("Impossibile applicare il campo", err)
					return true
				}
			}
			if dataDoc != nil && target >= 0 && target < len(dataDoc.Records) {
				dataCurrentRecord = target
				populateDataFieldCombo(target, 0)
				updateDataStatus()
			}
		}
		return true
	case idDataField:
		if notify == CBN_SELCHANGE && !dataRefreshingUI {
			target := comboSel(hwndDataField)
			if dataValueEdited {
				if err := applyDataCurrentField(false); err != nil {
					showDataError("Impossibile applicare il campo", err)
					return true
				}
			}
			if target >= 0 && target < len(dataFields) {
				dataCurrentField = target
				loadDataCurrentFieldValue()
			}
		}
		return true
	case idDataValue:
		if (notify == cbnEditChange || notify == CBN_SELCHANGE || notify == EN_CHANGE) && !dataRefreshingUI {
			dataValueEdited = true
		}
		return true
	case idDataRawEditor:
		if notify == EN_CHANGE && !dataRefreshingUI {
			dataRawEdited = true
			if dataDoc != nil {
				dataDoc.Dirty = true
			}
			updateDataStatus()
		}
		return true
	case idDataSettingsGen, idDataSettingsPartySize:
		if notify == CBN_SELCHANGE && !dataRefreshingUI {
			dataSettingsStandardEdited = true
			refreshDataSettingsUI(false)
		}
		return true
	case idDataSettingsRaw:
		if notify == EN_CHANGE && !dataRefreshingUI {
			dataSettingsRawEdited = true
			refreshDataSettingsUI(false)
		}
		return true
	case idDataApply:
		if notify == BN_CLICKED {
			if err := applyDataCurrentField(true); err != nil {
				showDataError("Impossibile applicare il campo", err)
			}
		}
		return true
	case idDataAdd:
		if notify == BN_CLICKED {
			if err := addDataFieldFromForm(); err != nil {
				showDataError("Impossibile aggiungere il campo", err)
			} else {
				setToolbarStatus("Vista Dati: nuovo campo aggiunto.")
			}
		}
		return true
	case idDataRemove:
		if notify == BN_CLICKED {
			if err := removeDataCurrentField(); err != nil {
				showDataError("Impossibile rimuovere il campo", err)
			} else {
				setToolbarStatus("Vista Dati: campo rimosso.")
			}
		}
		return true
	case idDataNewRecord:
		if notify == BN_CLICKED {
			if err := createDataRecordFromForm(false); err != nil {
				showDataError("Impossibile creare la voce", err)
			} else {
				setToolbarStatus("Vista Dati: nuova voce creata.")
			}
		}
		return true
	case idDataDuplicate:
		if notify == BN_CLICKED {
			if err := createDataRecordFromForm(true); err != nil {
				showDataError("Impossibile duplicare la voce", err)
			} else {
				setToolbarStatus("Vista Dati: voce duplicata.")
			}
		}
		return true
	case idDataDeleteRecord:
		if notify == BN_CLICKED {
			if err := deleteDataCurrentRecord(); err != nil {
				showDataError("Impossibile eliminare la voce", err)
			} else {
				setToolbarStatus("Vista Dati: voce eliminata.")
			}
		}
		return true
	case idDataSave, idDataRawSave:
		if notify == BN_CLICKED {
			if err := saveDataCurrent(); err != nil {
				showDataError("Impossibile salvare i dati", err)
			} else if id == idDataRawSave {
				setToolbarStatus("Vista Dati: PBS salvato nel progetto.")
			} else {
				setToolbarStatus("Vista Dati: dati salvati nel progetto.")
			}
		}
		return true
	case idDataSettingsMega, idDataSettingsAwaken:
		if notify == BN_CLICKED && !dataRefreshingUI {
			dataSettingsStandardEdited = true
			refreshDataSettingsUI(false)
		}
		return true
	case idDataSettingsApply:
		if notify == BN_CLICKED {
			if err := applyDataSettingsStandard(); err != nil {
				showDataError("Impossibile applicare le impostazioni Pokémon", err)
			}
		}
		return true
	case idDataSettingsRawSave:
		if notify == BN_CLICKED {
			if err := saveDataSettingsRaw(); err != nil {
				showDataError("Impossibile salvare le impostazioni Pokémon", err)
			}
		}
		return true
	case idDataSettingsRawReload:
		if notify == BN_CLICKED {
			dataSettingsRawEdited = false
			refreshDataSettingsUI(true)
			setToolbarStatus("Impostazioni Pokémon: configurazione ricaricata.")
		}
		return true
	}
	return false
}

func clearCombo(h syscall.Handle) {
	if h != 0 {
		pSendMessageW.Call(uintptr(h), CB_RESETCONTENT, 0, 0)
	}
}

func friendlyDataFileLabel(name string) string {
	base := strings.ToLower(strings.TrimSuffix(filepath.Base(name), filepath.Ext(name)))
	labels := map[string]string{
		"pokemon": "Pokémon", "pokemon_forms": "Forme Pokémon", "pokemon_metrics": "Metriche Pokémon",
		"moves": "Mosse", "items": "Strumenti", "abilities": "Abilità", "types": "Tipi",
		"trainers": "Allenatori", "trainer_types": "Tipi allenatore", "encounters": "Incontri selvatici",
		"metadata": "Dati generali", "map_metadata": "Dati mappe", "town_map": "Mappa regione",
		"berry_plants": "Piante di bacche", "phone": "Telefono", "shadow_pokemon": "Pokémon Ombra",
		"regional_dexes": "Pokédex regionali", "ribbons": "Fiocchi", "animations": "Animazioni",
	}
	if label := labels[base]; label != "" {
		return label
	}
	pretty := strings.ReplaceAll(base, "_", " ")
	if pretty == "" {
		return name
	}
	return strings.ToUpper(pretty[:1]) + pretty[1:]
}

func friendlyDataFieldLabel(f dataPBSField) string {
	if f.IsHeader {
		return "ID interno"
	}
	if f.Raw {
		return "Valore aggiuntivo"
	}
	return friendlyDataKeyLabel(f.Key)
}

func friendlyDataKeyLabel(key string) string {
	labels := map[string]string{
		"Name": "Nome", "NamePlural": "Nome plurale", "InternalName": "Nome interno",
		"Type": "Tipo", "Types": "Tipi", "Type1": "Tipo 1", "Type2": "Tipo 2",
		"BaseStats": "Statistiche base", "GenderRatio": "Rapporto sesso", "GrowthRate": "Crescita",
		"BaseExp": "Esperienza base", "CatchRate": "Tasso cattura", "Happiness": "Felicità",
		"Abilities": "Abilità", "HiddenAbilities": "Abilità nascosta", "Moves": "Mosse per livello",
		"TutorMoves": "Mosse tutor", "EggMoves": "Mosse Uovo", "EggGroups": "Gruppi Uovo",
		"Height": "Altezza", "Weight": "Peso", "Color": "Colore", "Shape": "Forma",
		"Habitat": "Habitat", "Category": "Categoria", "Pokedex": "Descrizione Pokédex",
		"Generation": "Generazione", "Evolutions": "Evoluzioni", "Description": "Descrizione",
		"Power": "Potenza", "BaseDamage": "Potenza", "Accuracy": "Precisione", "TotalPP": "PP",
		"Target": "Bersaglio", "Priority": "Priorità", "FunctionCode": "Funzione",
		"Price": "Prezzo", "Pocket": "Tasca", "Consumable": "Consumabile",
		"Weaknesses": "Debolezze", "Resistances": "Resistenze", "Immunities": "Immunità",
		"TrainerType": "Tipo allenatore", "BaseMoney": "Ricompensa base", "StartMoney": "Denaro iniziale",
		"Home": "Casa / ritorno", "Flags": "Proprietà",
	}
	if label := labels[key]; label != "" {
		return label
	}
	if strings.TrimSpace(key) == "" {
		return "Campo"
	}
	return key
}

func showDataError(title string, err error) {
	if err == nil {
		return
	}
	msgbox("PML Studio - Vista Dati", title+":\r\n\r\n"+err.Error(), MB_OK|MB_ICONERROR)
	setToolbarStatus("Vista Dati: " + err.Error())
}
