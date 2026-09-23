//go:build windows

package main

import (
	"fmt"
	"strings"
)

func unsavedProjectAreas() []string {
	areas := make([]string, 0, 6)
	if mapDirty && currentMapDoc != nil {
		areas = append(areas, "Mappa")
	}
	if permissionDataDirty {
		areas = append(areas, "Movimenti / Terrain Tags")
	}
	if encounterDirty {
		areas = append(areas, "Incontri selvatici")
	}
	if headerDirty {
		areas = append(areas, "Header mappa")
	}
	if projectFeaturesDirty {
		areas = append(areas, "Struttura gioco")
	}
	if dataDoc != nil && (dataDoc.Dirty || dataValueEdited || dataRawEdited) {
		areas = append(areas, "Vista Dati")
	}
	if dataSettingsStandardEdited || dataSettingsRawEdited {
		areas = append(areas, "Impostazioni Pokémon")
	}
	if translationHasDirty() {
		areas = append(areas, "Traduzioni")
	}
	return areas
}

func hasUnsavedProjectChanges() bool { return len(unsavedProjectAreas()) > 0 }

func discardUnsavedProjectFlags() {
	mapDirty = false
	permissionDataDirty = false
	encounterDirty = false
	headerDirty = false
	projectFeaturesDirty = false
	if dataDoc != nil {
		dataDoc.Dirty = false
	}
	dataValueEdited = false
	dataRawEdited = false
	dataSettingsStandardEdited = false
	dataSettingsRawEdited = false
}

// saveAllProjectChanges is the authoritative save path used by File > Salva,
// close confirmation and project switching. It returns false if any tracked
// editor cannot be persisted, so the application never closes after a failed
// save while pretending that the changes were applied.
func saveAllProjectChanges() bool {
	if currentProject == "" {
		return true
	}
	if err := ensureProjectMapFilesystemSafe("Salvataggio"); err != nil {
		msgbox("PML Studio", err.Error(), MB_OK|MB_ICONERROR)
		setText(hwndStatus, "Salvataggio bloccato: Map ID duplicati")
		return false
	}
	if settings.BackupBeforeSave {
		if _, err := backupProjectData(currentProject, settings.BackupPath); err != nil {
			msgbox("Backup", "Salvataggio interrotto: "+err.Error(), MB_OK|MB_ICONERROR)
			return false
		}
	}
	if !saveCurrentMap() {
		return false
	}
	if headerDirty {
		if err := saveHeaderToCurrentMap(); err != nil {
			msgbox("PML Studio - Salvataggio", "Impossibile salvare l'Header mappa:\r\n\r\n"+err.Error(), MB_OK|MB_ICONERROR)
			return false
		}
	}

	hadPermissions := permissionDataDirty
	savePermissions()
	if hadPermissions && permissionDataDirty {
		msgbox("PLM Studio - Salvataggio", "Non è stato possibile salvare Movimenti / Terrain Tags.\r\n\r\nIl progetto resterà aperto.", MB_OK|MB_ICONERROR)
		return false
	}

	if err := saveEvents(); err != nil {
		msgbox("PML Studio - Salvataggio", "Impossibile salvare gli eventi:\r\n\r\n"+err.Error(), MB_OK|MB_ICONERROR)
		return false
	}

	if err := saveEncounterData(); err != nil {
		setText(hwndStatus, "Errore salvataggio incontri selvatici: "+err.Error())
		msgbox("PLM Studio - Salvataggio", "Impossibile salvare gli incontri selvatici:\r\n\r\n"+err.Error(), MB_OK|MB_ICONERROR)
		return false
	}

	if dataDoc != nil && (dataDoc.Dirty || dataValueEdited || dataRawEdited) {
		if err := saveDataCurrent(); err != nil {
			msgbox("PLM Studio - Salvataggio", "Impossibile salvare la Vista Dati:\r\n\r\n"+err.Error(), MB_OK|MB_ICONERROR)
			return false
		}
	}
	if dataSettingsRawEdited {
		if err := saveDataSettingsRaw(); err != nil {
			msgbox("PML Studio - Salvataggio", "Impossibile salvare le Impostazioni Pokémon:\r\n\r\n"+err.Error(), MB_OK|MB_ICONERROR)
			return false
		}
	} else if dataSettingsStandardEdited {
		if err := applyDataSettingsStandard(); err != nil {
			msgbox("PML Studio - Salvataggio", "Impossibile applicare le Impostazioni Pokémon:\r\n\r\n"+err.Error(), MB_OK|MB_ICONERROR)
			return false
		}
	}

	if translationHasDirty() {
		if !saveAllTranslations() {
			return false
		}
	}

	if err := saveProjectFeatures(); err != nil {
		setText(hwndStatus, "Dati progetto non salvati: "+err.Error())
		msgbox("PLM Studio - Salvataggio", "Impossibile salvare tutti i dati del progetto:\r\n\r\n"+err.Error(), MB_OK|MB_ICONERROR)
		return false
	}

	setText(hwndStatus, "Progetto salvato.")
	return true
}

func confirmUnsavedProjectChanges(action string) bool {
	areas := unsavedProjectAreas()
	if len(areas) == 0 {
		return true
	}
	if strings.TrimSpace(action) == "" {
		action = "continuare"
	}
	message := "Ci sono modifiche non salvate in:\r\n\r\n• " + strings.Join(areas, "\r\n• ") +
		"\r\n\r\nVuoi applicare e salvare le modifiche prima di " + action + "?" +
		"\r\n\r\nSì = Applica e salva\r\nNo = Continua senza salvare\r\nAnnulla = Resta nel progetto"
	result := msgboxResult("PLM Studio - Modifiche non salvate", message, MB_YESNOCANCEL|MB_ICONINFORMATION)
	switch result {
	case IDYES:
		return saveAllProjectChanges()
	case IDNO:
		// Do not clear the dirty flags here. The user may still cancel the file
		// picker/new-project workflow after this prompt. A successful project
		// switch resets the editor state inside populateProject().
		return true
	default:
		return false
	}
}

func unsavedProjectSummary() string {
	areas := unsavedProjectAreas()
	if len(areas) == 0 {
		return "nessuna modifica"
	}
	return fmt.Sprintf("%d aree modificate: %s", len(areas), strings.Join(areas, ", "))
}
