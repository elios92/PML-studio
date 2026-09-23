//go:build windows

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	projectModeStandard = "standard"
	projectModeAdvanced = "advanced"
)

var currentProjectEditorMode = projectModeStandard

func normalizeProjectEditorMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case projectModeAdvanced, "advanced_developer", "developer", "dev":
		return projectModeAdvanced
	default:
		return projectModeStandard
	}
}

func projectEditorModeLabel(mode string) string {
	if normalizeProjectEditorMode(mode) == projectModeAdvanced {
		return "Advanced Developer"
	}
	return "Standard No-Code"
}

// chooseNewProjectEditorMode is intentionally shown only for a NEW project.
// Existing projects persist the decision in plm_project.json and reopen in the
// same mode without repeatedly asking the user.
func chooseNewProjectEditorMode() (string, bool) {
	result := msgboxResult(
		"PLM Studio - Modalità progetto",
		"Scegli la modalità del nuovo progetto.\r\n\r\n"+
			"SÌ  = Standard No-Code (consigliata)\r\n"+
			"Interfaccia visuale e strumenti guidati.\r\n\r\n"+
			"NO  = Advanced Developer\r\n"+
			"Mantiene tutte le funzioni No-Code e abilita gli strumenti tecnici/avanzati.\r\n\r\n"+
			"ANNULLA = non creare il progetto.",
		MB_YESNOCANCEL|MB_ICONINFORMATION,
	)
	switch result {
	case IDYES:
		return projectModeStandard, true
	case IDNO:
		return projectModeAdvanced, true
	default:
		return "", false
	}
}

func projectEditorModeFromManifest(root string) string {
	if m, ok := readProjectManifest(root); ok {
		return normalizeProjectEditorMode(m.EditorMode)
	}
	return projectModeStandard
}

func saveProjectEditorMode(root, mode string) error {
	if strings.TrimSpace(root) == "" {
		return fmt.Errorf("progetto non valido")
	}
	root = filepath.Clean(root)
	if root == "" || !isDir(root) {
		return fmt.Errorf("progetto non valido")
	}
	m, ok := readProjectManifest(root)
	if !ok {
		m = ProjectManifest{
			FormatVersion: 1,
			Name:          filepath.Base(root),
			ProjectType:   detectProjectType(root),
			Root:          ".",
			CreatedBy:     "PLM Studio",
			MapFolders:    []string{"converted/maps"},
		}
	}
	dst := filepath.Join(root, "plm_project.json")
	// Mode changes must preserve extension fields owned by the project/plugins.
	raw := map[string]json.RawMessage{}
	b, err := os.ReadFile(dst)
	if err == nil {
		if err = json.Unmarshal(b, &raw); err != nil || raw == nil {
			return fmt.Errorf("manifest non valido: impossibile cambiare modalità")
		}
	} else if !os.IsNotExist(err) {
		return err
	} else {
		b, err = json.Marshal(m)
		if err != nil {
			return err
		}
		if err = json.Unmarshal(b, &raw); err != nil {
			return err
		}
	}
	raw["editor_mode"], _ = json.Marshal(normalizeProjectEditorMode(mode))
	return writeJSONAtomic(dst, raw)
}

func applyProjectEditorMode(root string) {
	currentProjectEditorMode = projectEditorModeFromManifest(root)

	// Vista Dati follows the project mode by default. Advanced still contains
	// all Standard functionality; this only changes the initial surface.
	dataAdvancedMode = currentProjectEditorMode == projectModeAdvanced
	if hwndDataStandard != 0 && hwndDataAdvanced != 0 {
		refreshDataModeUI()
	}
}
