//go:build windows

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// dataBattleSettingsDirty belongs to the battle-settings subsystem rather than
// database_editor.go. project_unsaved.go also reads this state, so keeping it
// here prevents the global save path from depending on a specific UI revision.
var dataBattleSettingsDirty bool

const (
	battleSettingsSchema      = "plm.battle_settings"
	battleSettingsVersion     = 1
	battleSettingsVirtualFile = "@plm_battle_settings"
)

type BattleSettings struct {
	Schema                string `json:"schema"`
	Version               int    `json:"version"`
	MaxPartySize          int    `json:"max_party_size"`
	MegaEvolutionsEnabled bool   `json:"mega_evolutions_enabled"`
	AwakeningsEnabled     bool   `json:"awakenings_enabled"`
	WildGroupEncounters   bool   `json:"wild_group_encounters"`
	WildGroupMin          int    `json:"wild_group_min"`
	WildGroupMax          int    `json:"wild_group_max"`
}

func defaultBattleSettings() BattleSettings {
	return BattleSettings{
		Schema:                battleSettingsSchema,
		Version:               battleSettingsVersion,
		MaxPartySize:          6,
		MegaEvolutionsEnabled: false,
		AwakeningsEnabled:     false,
		WildGroupEncounters:   false,
		WildGroupMin:          3,
		WildGroupMax:          5,
	}
}

func normalizeBattleSettings(s BattleSettings) BattleSettings {
	s.Schema = battleSettingsSchema
	s.Version = battleSettingsVersion
	if s.MaxPartySize < 1 {
		s.MaxPartySize = 1
	}
	if s.MaxPartySize > 10 {
		s.MaxPartySize = 10
	}
	if s.WildGroupMin < 1 {
		s.WildGroupMin = 1
	}
	if s.WildGroupMin > 5 {
		s.WildGroupMin = 5
	}
	if s.WildGroupMax < 1 {
		s.WildGroupMax = 1
	}
	if s.WildGroupMax > 5 {
		s.WildGroupMax = 5
	}
	if s.WildGroupMin > s.WildGroupMax {
		s.WildGroupMin, s.WildGroupMax = s.WildGroupMax, s.WildGroupMin
	}
	return s
}

func battleSettingsPath(root string) string {
	return filepath.Join(root, "converted", "battle_settings.json")
}

func loadBattleSettings(root string) (BattleSettings, error) {
	s := defaultBattleSettings()
	if root == "" {
		return s, fmt.Errorf("nessun progetto aperto")
	}
	path := battleSettingsPath(root)
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		// Migrazione non distruttiva dalle due vecchie opzioni memorizzate nel
		// manifest. Il nuovo file battle_settings.json diventa poi canonico.
		if m, ok := readProjectManifest(root); ok {
			s.MegaEvolutionsEnabled = m.MegaEvolutionsEnabled
			s.AwakeningsEnabled = m.AwakeningsEnabled
		}
		return normalizeBattleSettings(s), nil
	}
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(b, &s); err != nil {
		return defaultBattleSettings(), fmt.Errorf("%s: %w", filepath.ToSlash(path), err)
	}
	if s.Schema != "" && s.Schema != battleSettingsSchema {
		return defaultBattleSettings(), fmt.Errorf("schema impostazioni lotte non supportato: %q", s.Schema)
	}
	if s.Version != 0 && s.Version != battleSettingsVersion {
		return defaultBattleSettings(), fmt.Errorf("versione impostazioni lotte non supportata: %d", s.Version)
	}
	return normalizeBattleSettings(s), nil
}

func saveBattleSettings(root string, s BattleSettings) error {
	if root == "" {
		return fmt.Errorf("nessun progetto aperto")
	}
	s = normalizeBattleSettings(s)
	if err := os.MkdirAll(filepath.Join(root, "converted"), 0755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	path := battleSettingsPath(root)
	tmp := path + ".plm.tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := ensureBattleSettingsRuntime(root); err != nil {
		return err
	}
	return ensurePartySizeRuntime(root)
}

const plmBattleSettingsRuntimeArchivePath = "_plm_templates/plm_battle_settings.py"

func ensureBattleSettingsRuntime(root string) error {
	if root == "" {
		return fmt.Errorf("nessun progetto aperto")
	}
	plmBattleSettingsRuntimePython, err := readEmbeddedRuntimeFile(plmBattleSettingsRuntimeArchivePath)
	if err != nil {
		return fmt.Errorf("runtime impostazioni lotta non disponibile: %w", err)
	}
	gameDir := filepath.Join(root, "game")
	if err := os.MkdirAll(gameDir, 0755); err != nil {
		return err
	}
	path := filepath.Join(gameDir, "plm_battle_settings.py")
	if current, err := os.ReadFile(path); err == nil && string(current) == string(plmBattleSettingsRuntimePython) {
		return nil
	}
	tmp := path + ".plm.tmp"
	if err := os.WriteFile(tmp, plmBattleSettingsRuntimePython, 0644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
