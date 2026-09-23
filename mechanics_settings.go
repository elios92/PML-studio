package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const (
	mechanicsSettingsVersion  = 4
	mechanicsSettingsSchema   = "plm.mechanics.v4"
	mechanicsMinGeneration    = 1
	mechanicsMaxGeneration    = 8
	mechanicsDefaultPartySize = 6
	mechanicsMinPartySize     = 6
	mechanicsMaxPartySize     = 12
)

// mechanicsProjectSettings is the canonical project-side selection used by
// PML Studio.  It deliberately keeps mechanics generation separate from the
// optional generation-specific PBS bundle: Essentials v20.1 supports
// MECHANICS_GENERATION 1..8, but only ships PBS/Gen 5..Gen 8 data packs.
type mechanicsProjectSettings struct {
	Version                int    `json:"version"`
	Schema                 string `json:"schema"`
	MechanicsGeneration    int    `json:"mechanics_generation"`
	ActivePBSRoot          string `json:"active_pbs_root"`
	GenerationPBSAvailable bool   `json:"generation_pbs_available"`
	ReferencePBSGeneration int    `json:"reference_pbs_generation,omitempty"`
	ReferencePBSRoot       string `json:"reference_pbs_root,omitempty"`
	MaxPartySize           int    `json:"max_party_size"`
	MegaEvolutionsEnabled  bool   `json:"mega_evolutions_enabled"`
	AwakeningsEnabled      bool   `json:"awakenings_enabled"`
}

var mechanicsGenerationRE = regexp.MustCompile(`(?m)^\s*MECHANICS_GENERATION\s*=\s*([0-9]+)\b`)

func clampMechanicsGeneration(gen int) int {
	if gen < mechanicsMinGeneration {
		return mechanicsMinGeneration
	}
	if gen > mechanicsMaxGeneration {
		return mechanicsMaxGeneration
	}
	return gen
}

func clampMechanicsPartySize(size int) int {
	if size == 0 {
		return mechanicsDefaultPartySize
	}
	if size < mechanicsMinPartySize {
		return mechanicsMinPartySize
	}
	if size > mechanicsMaxPartySize {
		return mechanicsMaxPartySize
	}
	return size
}

func mechanicsSettingsPath(project string) string {
	return filepath.Join(project, "converted", "data", "plm_mechanics.json")
}

func projectPBSBaseRoot(project string) string {
	candidates := []string{
		filepath.Join(project, "converted", "PBS"),
		filepath.Join(project, "PBS"),
		filepath.Join(project, "pbs"),
	}
	for _, p := range candidates {
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			return p
		}
	}
	return filepath.Join(project, "converted", "PBS")
}

func mechanicsGenerationPBSDir(project string, gen int) (string, bool) {
	gen = clampMechanicsGeneration(gen)
	if gen < 5 {
		return projectPBSBaseRoot(project), false
	}
	base := projectPBSBaseRoot(project)
	candidate := filepath.Join(base, fmt.Sprintf("Gen %d", gen))
	if st, err := os.Stat(candidate); err == nil && st.IsDir() {
		return candidate, true
	}
	// Essentials v20.1's top-level PBS is the Gen 8 data set.  Therefore Gen 8
	// remains valid even when a converted/native project did not retain the
	// redundant PBS/Gen 8 directory.
	return base, false
}

func mechanicsActivePBSFile(project string, gen int, name string) string {
	// Imported/native project PBS is always the editable source of truth. A
	// generation bundle is only a canonical reference for compatibility/update
	// operations and must never silently replace customized project data.
	return filepath.Join(projectPBSBaseRoot(project), name)
}

func mechanicsProjectRelativePath(project, path string) string {
	rel, err := filepath.Rel(project, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}

func mechanicsReferencePBSRoot(project string, gen int) (string, bool, int) {
	dir, ok := mechanicsGenerationPBSDir(project, gen)
	if !ok {
		return "", false, 0
	}
	return mechanicsProjectRelativePath(project, dir), true, gen
}

func detectImportedMechanicsGeneration(project string) int {
	dir := filepath.Join(project, "converted", "scripts_ruby")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return mechanicsMaxGeneration
	}
	// Prefer scripts whose extracted filename clearly identifies Settings.
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".rb") {
			continue
		}
		names = append(names, e.Name())
	}
	sort.SliceStable(names, func(i, j int) bool {
		a := strings.Contains(strings.ToLower(names[i]), "settings")
		b := strings.Contains(strings.ToLower(names[j]), "settings")
		if a != b {
			return a
		}
		return strings.ToLower(names[i]) < strings.ToLower(names[j])
	})
	for _, name := range names {
		b, readErr := os.ReadFile(filepath.Join(dir, name))
		if readErr != nil {
			continue
		}
		m := mechanicsGenerationRE.FindSubmatch(b)
		if len(m) != 2 {
			continue
		}
		gen, convErr := strconv.Atoi(string(m[1]))
		if convErr == nil && gen >= mechanicsMinGeneration && gen <= mechanicsMaxGeneration {
			return gen
		}
	}
	return mechanicsMaxGeneration
}

func normalizedMechanicsSettings(project string, in mechanicsProjectSettings) mechanicsProjectSettings {
	gen := in.MechanicsGeneration
	if gen == 0 {
		gen = detectImportedMechanicsGeneration(project)
	}
	gen = clampMechanicsGeneration(gen)
	partySize := clampMechanicsPartySize(in.MaxPartySize)
	base := mechanicsProjectRelativePath(project, projectPBSBaseRoot(project))
	referenceRoot, referenceAvailable, referenceGen := mechanicsReferencePBSRoot(project, gen)
	return mechanicsProjectSettings{
		Version:                mechanicsSettingsVersion,
		Schema:                 mechanicsSettingsSchema,
		MechanicsGeneration:    gen,
		ActivePBSRoot:          base,
		GenerationPBSAvailable: referenceAvailable,
		ReferencePBSGeneration: referenceGen,
		ReferencePBSRoot:       referenceRoot,
		MaxPartySize:           partySize,
		MegaEvolutionsEnabled:  in.MegaEvolutionsEnabled,
		AwakeningsEnabled:      in.AwakeningsEnabled,
	}
}

func loadMechanicsSettings(project string) mechanicsProjectSettings {
	// Le meccaniche già presenti nel runtime restano attive nei progetti creati
	// prima dell'introduzione dei relativi flag. L'unmarshal sovrascrive questi
	// default solo quando le chiavi sono realmente presenti nel JSON.
	state := mechanicsProjectSettings{
		MegaEvolutionsEnabled: true,
		AwakeningsEnabled:     true,
	}
	if strings.TrimSpace(project) != "" {
		if b, err := os.ReadFile(mechanicsSettingsPath(project)); err == nil {
			_ = json.Unmarshal(b, &state)
		}
	}
	return normalizedMechanicsSettings(project, state)
}

func saveMechanicsSettings(project string, gen int) (mechanicsProjectSettings, error) {
	current := loadMechanicsSettings(project)
	return saveMechanicsSettingsOptionsExtended(project, gen, current.MaxPartySize, current.MegaEvolutionsEnabled, current.AwakeningsEnabled)
}

// saveMechanicsSettingsOptions mantiene compatibilità con i chiamanti esistenti:
// aggiorna generazione e dimensione squadra senza cambiare i flag Mega/Risvegli.
func saveMechanicsSettingsOptions(project string, gen int, maxPartySize int) (mechanicsProjectSettings, error) {
	current := loadMechanicsSettings(project)
	return saveMechanicsSettingsOptionsExtended(project, gen, maxPartySize, current.MegaEvolutionsEnabled, current.AwakeningsEnabled)
}

func saveMechanicsSettingsOptionsExtended(project string, gen int, maxPartySize int, megaEnabled bool, awakeningsEnabled bool) (mechanicsProjectSettings, error) {
	if strings.TrimSpace(project) == "" {
		return mechanicsProjectSettings{}, fmt.Errorf("nessun progetto aperto")
	}
	if maxPartySize < mechanicsMinPartySize || maxPartySize > mechanicsMaxPartySize {
		return mechanicsProjectSettings{}, fmt.Errorf("max_party_size deve essere compreso tra %d e %d", mechanicsMinPartySize, mechanicsMaxPartySize)
	}
	state := normalizedMechanicsSettings(project, mechanicsProjectSettings{
		MechanicsGeneration:   gen,
		MaxPartySize:          maxPartySize,
		MegaEvolutionsEnabled: megaEnabled,
		AwakeningsEnabled:     awakeningsEnabled,
	})
	b, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return mechanicsProjectSettings{}, err
	}
	b = append(b, '\n')
	path := mechanicsSettingsPath(project)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return mechanicsProjectSettings{}, err
	}
	tmp := path + ".tmp"
	bak := path + ".bak"
	if err := os.WriteFile(tmp, b, 0644); err != nil {
		return mechanicsProjectSettings{}, err
	}
	_ = os.Remove(bak)
	hadOld := false
	if _, statErr := os.Stat(path); statErr == nil {
		if err := os.Rename(path, bak); err != nil {
			_ = os.Remove(tmp)
			return mechanicsProjectSettings{}, err
		}
		hadOld = true
	}
	if err := os.Rename(tmp, path); err != nil {
		if hadOld {
			_ = os.Rename(bak, path)
		}
		_ = os.Remove(tmp)
		return mechanicsProjectSettings{}, err
	}
	_ = os.Remove(bak)
	return state, nil
}
