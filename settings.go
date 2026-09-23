//go:build windows

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const uiSettingsVersion = 1

// Optional build-time override for isolated UI tests. Empty in release builds.
var preferencesDirectoryOverride string

func appDataDir() string {
	if preferencesDirectoryOverride != "" {
		return preferencesDirectoryOverride
	}
	if isolated := os.Getenv("PML_SETTINGS_DIR"); isolated != "" {
		return isolated
	}
	b := os.Getenv("APPDATA")
	if b == "" {
		home, _ := os.UserHomeDir()
		b = filepath.Join(home, "AppData", "Roaming")
	}
	return filepath.Join(b, "PLM Studio")
}

func normalizeUISettings() {
	normalizeEditorPreferences(&settings)
	// I settings.json creati dalle build precedenti non contengono questi campi.
	// Versioniamo solo le preferenze UI per distinguere "false" esplicito dal
	// vecchio campo assente e mantenere la griglia attiva per compatibilità.
	if settings.UISettingsVersion < uiSettingsVersion {
		settings.UISettingsVersion = uiSettingsVersion
		if strings.TrimSpace(settings.UISkin) == "" {
			settings.UISkin = "windows10"
		}
		if settings.UIFontSize == 0 {
			settings.UIFontSize = 16
		}
		settings.GridDefault = true
	}

	switch settings.UISkin {
	case "windows10", "windows10_gray", "windows10_soft", "dark", "blue", "custom":
	default:
		settings.UISkin = "windows10"
	}
	if settings.UIFontSize != 14 && settings.UIFontSize != 16 && settings.UIFontSize != 18 {
		settings.UIFontSize = 16
	}
}

func loadSettings() {
	b, e := os.ReadFile(filepath.Join(appDataDir(), "settings.json"))
	if e == nil {
		var loaded Settings
		if e = json.Unmarshal(b, &loaded); e == nil {
			settings = loaded
		} else {
			diagLogf("[SETTINGS] JSON non valido: %v", e)
		}
	}
	normalizeUISettings()
}

func validateExistingPreferences(path string) error {
	b, e := os.ReadFile(path)
	if os.IsNotExist(e) {
		return nil
	}
	if e != nil {
		return e
	}
	if !json.Valid(b) {
		return fmt.Errorf("settings.json non valido: originale conservato; correggerlo prima di salvare")
	}
	return nil
}

func saveSettings() {
	if err := persistEditorPreferences(settings); err != nil {
		diagLogf("[SETTINGS] %v", err)
		setToolbarStatus("Impostazioni non salvate: " + err.Error())
	}
	// Le preferenze non sono dati del progetto, ma una scrittura interrotta non
	// deve comunque lasciare un JSON parziale. Riutilizziamo il writer atomico
	// già verificato dall'editor Header.
}

func rememberProject(p string) {
	p = filepath.Clean(p)
	settings.LastProject = p
	out := []string{p}
	for _, q := range settings.RecentProjects {
		if strings.EqualFold(filepath.Clean(q), p) {
			continue
		}
		if _, e := os.Stat(q); e == nil {
			out = append(out, q)
		}
		if len(out) >= 10 {
			break
		}
	}
	settings.RecentProjects = out
	saveSettings()
	updateRecent()
}

func updateRecent() {
	if hwndBtnRecent != 0 {
		if settings.LastProject != "" {
			setText(hwndBtnRecent, "Riapri ultimo")
		} else {
			setText(hwndBtnRecent, "Nessun recente")
		}
	}
}
