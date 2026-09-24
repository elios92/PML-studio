//go:build windows

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

var convertedMapFileRE = regexp.MustCompile(`(?i)^Map(\d+)\.json$`)
var essentialsMapFileRE = regexp.MustCompile(`(?i)^Map(\d+)\.rxdata$`)

func essentialsMapIDs(root string) ([]int, error) {
	entries, err := os.ReadDir(filepath.Join(root, "Data"))
	if err != nil {
		return nil, err
	}
	ids := make([]int, 0)
	seen := map[int]bool{}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		match := essentialsMapFileRE.FindStringSubmatch(entry.Name())
		if len(match) != 2 {
			continue
		}
		id, _ := strconv.Atoi(match[1])
		if id <= 0 || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	sort.Ints(ids)
	return ids, nil
}

func convertedMapPathsByID(root string) (map[int]string, error) {
	mapsRoot := filepath.Join(root, "converted", "maps")
	found := map[int]string{}
	err := filepath.WalkDir(mapsRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		match := convertedMapFileRE.FindStringSubmatch(entry.Name())
		if len(match) != 2 {
			return nil
		}
		id, _ := strconv.Atoi(match[1])
		if id <= 0 {
			return nil
		}
		if previous, duplicate := found[id]; duplicate {
			return fmt.Errorf("Map%03d duplicata nel progetto convertito: %s e %s", id, previous, path)
		}
		found[id] = path
		return nil
	})
	if err != nil {
		return nil, err
	}
	return found, nil
}

func compareCopiedTree(sourceRoot, destRoot, label string) error {
	if !isDir(sourceRoot) {
		return nil
	}
	sourceFiles := map[string]string{}
	err := filepath.WalkDir(sourceRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(sourceRoot, path)
		if err != nil {
			return err
		}
		hash, err := sha256HexFile(path)
		if err != nil {
			return err
		}
		sourceFiles[filepath.ToSlash(rel)] = hash
		return nil
	})
	if err != nil {
		return fmt.Errorf("%s sorgente: %w", label, err)
	}
	destFiles := map[string]string{}
	err = filepath.WalkDir(destRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(destRoot, path)
		if err != nil {
			return err
		}
		hash, err := sha256HexFile(path)
		if err != nil {
			return err
		}
		destFiles[filepath.ToSlash(rel)] = hash
		return nil
	})
	if err != nil {
		return fmt.Errorf("%s destinazione: %w", label, err)
	}
	if len(sourceFiles) != len(destFiles) {
		return fmt.Errorf("%s non identico: sorgente=%d file, copia=%d file", label, len(sourceFiles), len(destFiles))
	}
	for rel, hash := range sourceFiles {
		got, ok := destFiles[rel]
		if !ok {
			return fmt.Errorf("%s: file mancante nella copia: %s", label, rel)
		}
		if !strings.EqualFold(hash, got) {
			return fmt.Errorf("%s: file diverso dalla sorgente: %s", label, rel)
		}
	}
	return nil
}

func compareCopiedTreeContainsSource(sourceRoot, destRoot, label string) error {
	if !isDir(sourceRoot) {
		return nil
	}
	return filepath.WalkDir(sourceRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(sourceRoot, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destRoot, rel)
		sourceHash, err := sha256HexFile(path)
		if err != nil {
			return err
		}
		targetHash, err := sha256HexFile(target)
		if err != nil {
			return fmt.Errorf("%s: file sorgente non preservato nella copia: %s", label, filepath.ToSlash(rel))
		}
		if !strings.EqualFold(sourceHash, targetHash) {
			return fmt.Errorf("%s: file diverso dalla sorgente: %s", label, filepath.ToSlash(rel))
		}
		return nil
	})
}

func validateNumericAssetAliases(source, dest string) error {
	path := filepath.Join(dest, "converted", "asset_aliases.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("manifest alias grafici mancante: %w", err)
	}
	var manifest plmAssetAliasManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return fmt.Errorf("manifest alias grafici non valido: %w", err)
	}
	if manifest.Schema != plmAssetAliasSchema || manifest.Version != 1 {
		return fmt.Errorf("manifest alias grafici schema/versione non validi: %q v%d", manifest.Schema, manifest.Version)
	}
	seen := map[string]bool{}
	for _, alias := range manifest.Entries {
		if strings.TrimSpace(alias.CanonicalName) == "" || strings.TrimSpace(alias.CanonicalRelative) == "" {
			return fmt.Errorf("alias grafico incompleto per tileset %d slot %d", alias.TilesetID, alias.Slot)
		}
		key := strings.ToLower(filepath.ToSlash(alias.CanonicalRelative))
		if seen[key] {
			return fmt.Errorf("alias grafico duplicato: %s", alias.CanonicalRelative)
		}
		seen[key] = true
		canonicalPath := filepath.Join(dest, filepath.FromSlash(alias.CanonicalRelative))
		hash, err := sha256HexFile(canonicalPath)
		if err != nil {
			return fmt.Errorf("alias grafico mancante: %s", alias.CanonicalRelative)
		}
		if !strings.EqualFold(hash, alias.SHA256) {
			return fmt.Errorf("alias grafico alterato: %s", alias.CanonicalRelative)
		}
		sourcePath := filepath.Join(source, filepath.FromSlash(alias.SourceRelative))
		sourceHash, err := sha256HexFile(sourcePath)
		if err != nil || !strings.EqualFold(sourceHash, alias.SHA256) {
			return fmt.Errorf("alias grafico non corrisponde alla sorgente: %s", alias.SourceRelative)
		}
	}

	// Validate the actual metadata consumed by map rendering. A tileset graphic
	// must be its numeric ID, and each non-empty autotile must be ID_slot.
	tilesetDir := filepath.Join(dest, "converted", "tilesets")
	entries, err := os.ReadDir(tilesetDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".json") {
			continue
		}
		var ts runtimeTilesetJSON
		b, err := os.ReadFile(filepath.Join(tilesetDir, entry.Name()))
		if err != nil {
			return err
		}
		if err := json.Unmarshal(b, &ts); err != nil {
			return err
		}
		if strings.TrimSpace(ts.SourceTilesetName) != "" {
			wanted := fmt.Sprintf("%03d", ts.ID)
			if ts.TilesetName != wanted {
				return fmt.Errorf("tileset %d: nome canonico %q, atteso %q", ts.ID, ts.TilesetName, wanted)
			}
		}
		for slot, sourceName := range ts.SourceAutotileNames {
			if strings.TrimSpace(sourceName) == "" {
				continue
			}
			wanted := fmt.Sprintf("%03d_%02d", ts.ID, slot+1)
			if slot >= len(ts.AutotileNames) || ts.AutotileNames[slot] != wanted {
				return fmt.Errorf("tileset %d autotile slot %d: nome canonico errato", ts.ID, slot+1)
			}
		}
	}
	return nil
}

func readCanonicalConnectionDocument(path string) (ConnectionDocument, error) {
	var doc ConnectionDocument
	data, err := os.ReadFile(path)
	if err != nil {
		return doc, err
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return doc, err
	}
	if strings.TrimSpace(doc.Schema) != connectionSchema || doc.Version != connectionVersion {
		return doc, fmt.Errorf("schema/versione non validi: %q v%d", doc.Schema, doc.Version)
	}
	return doc, nil
}

func validateConvertedMapTilesets(dest string, maps map[int]string) error {
	for mapID, path := range maps {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var raw map[string]any
		if err := json.Unmarshal(data, &raw); err != nil {
			return fmt.Errorf("Map%03d JSON non valido: %w", mapID, err)
		}
		rawID, ok := raw["tileset_id"]
		if !ok {
			continue
		}
		tilesetID := 0
		switch value := rawID.(type) {
		case float64:
			tilesetID = int(value)
		case int:
			tilesetID = value
		case string:
			tilesetID, _ = strconv.Atoi(strings.TrimSpace(value))
		}
		if tilesetID <= 0 {
			continue
		}
		tilesetPath := filepath.Join(dest, "converted", "tilesets", fmt.Sprintf("%03d.json", tilesetID))
		if !exists(tilesetPath) {
			return fmt.Errorf("Map%03d usa tileset %d ma manca %s", mapID, tilesetID, filepath.ToSlash(filepath.Join("converted", "tilesets", fmt.Sprintf("%03d.json", tilesetID))))
		}
	}
	return nil
}

func validateNoRuntimeTitlePlaceholder(dest string) error {
	roots := []string{filepath.Join(dest, "main.py"), filepath.Join(dest, "game")}
	for _, root := range roots {
		info, err := os.Stat(root)
		if err != nil {
			return err
		}
		if !info.IsDir() {
			data, err := os.ReadFile(root)
			if err != nil {
				return err
			}
			if strings.Contains(string(data), runtimeProjectTitlePlaceholder) {
				return fmt.Errorf("placeholder titolo non sostituito in %s", filepath.Base(root))
			}
			continue
		}
		err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".py") {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if strings.Contains(string(data), runtimeProjectTitlePlaceholder) {
				rel, _ := filepath.Rel(dest, path)
				return fmt.Errorf("placeholder titolo non sostituito in %s", filepath.ToSlash(rel))
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}


// validateRubyRuntimeCoverage prevents a preserved Ruby customization from
// being mistaken for a functional Python conversion. The current runtime has
// no general Ruby executor/translator, so any modified/custom/plugin script
// requires an explicit Python bridge before the project can be certified 1:1.
func validateRubyRuntimeCoverage(dest string) error {
	type scriptIndexEntry struct {
		Name           string `json:"name"`
		Classification string `json:"classification"`
	}
	type gap struct {
		Source         string `json:"source"`
		Name           string `json:"name"`
		Classification string `json:"classification"`
		Reason         string `json:"reason"`
	}
	var gaps []gap
	for _, rel := range []string{
		filepath.Join("converted", "scripts_ruby", "index.json"),
		filepath.Join("converted", "plugin_scripts_ruby", "index.json"),
	} {
		path := filepath.Join(dest, rel)
		if !exists(path) {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var entries []scriptIndexEntry
		if err := json.Unmarshal(data, &entries); err != nil {
			return fmt.Errorf("indice script Ruby non valido %s: %w", filepath.ToSlash(rel), err)
		}
		for _, entry := range entries {
			switch strings.ToLower(strings.TrimSpace(entry.Classification)) {
			case "core_modified", "custom", "plugin":
				gaps = append(gaps, gap{
					Source: filepath.ToSlash(rel),
					Name: entry.Name,
					Classification: entry.Classification,
					Reason: "nessun bridge/consumer Python verificato per questo script Ruby",
				})
			}
		}
	}
	if len(gaps) == 0 {
		return nil
	}
	reportPath := filepath.Join(dest, "converted", "runtime_compatibility_gaps.json")
	payload := struct {
		Schema string `json:"schema"`
		Version int `json:"version"`
		Gaps []gap `json:"gaps"`
	}{
		Schema: "pml.runtime_compatibility_gaps",
		Version: 1,
		Gaps: gaps,
	}
	if err := writeJSON(reportPath, payload); err != nil {
		return fmt.Errorf("scrittura report incompatibilita runtime: %w", err)
	}
	return fmt.Errorf("%d script Ruby modificati/custom/plugin non hanno ancora un consumer Python verificato; dettagli in %s", len(gaps), filepath.ToSlash(filepath.Join("converted", "runtime_compatibility_gaps.json")))
}

func validateConvertedEssentialsProject(source, dest string, report *EssentialsImportReport) error {
	if report == nil {
		return fmt.Errorf("report conversione assente")
	}
	if err := validateRuntimeInstall(dest); err != nil {
		return err
	}
	if err := validateRuntimePythonImports(dest); err != nil {
		return err
	}
	if err := validateLauncherExecution(dest); err != nil {
		return err
	}
	if err := validateNoRuntimeTitlePlaceholder(dest); err != nil {
		return err
	}
	if err := validateRubyRuntimeCoverage(dest); err != nil {
		return err
	}
	if err := validateConvertedAutotiles(dest); err != nil {
		return err
	}

	sourceMaps, err := essentialsMapIDs(source)
	if err != nil {
		return fmt.Errorf("inventario mappe sorgente: %w", err)
	}
	convertedMaps, err := convertedMapPathsByID(dest)
	if err != nil {
		return fmt.Errorf("inventario mappe convertite: %w", err)
	}
	if len(sourceMaps) != len(convertedMaps) || report.MapsConverted != len(sourceMaps) {
		return fmt.Errorf("mappe non 1:1: sorgente=%d, convertite=%d, report=%d", len(sourceMaps), len(convertedMaps), report.MapsConverted)
	}
	for _, id := range sourceMaps {
		if _, ok := convertedMaps[id]; !ok {
			return fmt.Errorf("Map%03d presente nella sorgente ma assente nel progetto convertito", id)
		}
	}

	requiredConverted := []string{
		filepath.Join("converted", "MapInfos.json"),
		filepath.Join("converted", "System.json"),
		filepath.Join("converted", "CommonEvents.json"),
		filepath.Join("converted", "Tilesets.json"),
		filepath.Join("converted", "map_connections.json"),
		filepath.Join("converted", "battle_settings.json"),
		filepath.Join("converted", "data", "plm_mechanics.json"),
		filepath.Join("converted", "asset_aliases.json"),
		filepath.Join("converted", "source_essentials", "manifest.json"),
		filepath.Join("game", "plm_battle_settings.py"),
	}
	for _, rel := range requiredConverted {
		info, err := os.Stat(filepath.Join(dest, rel))
		if err != nil || info.IsDir() || info.Size() == 0 {
			return fmt.Errorf("file convertito obbligatorio mancante/vuoto: %s", filepath.ToSlash(rel))
		}
	}

	if exists(filepath.Join(source, "Data", "messages.dat")) {
		messagesPath := filepath.Join(dest, "converted", "messages.json")
		info, statErr := os.Stat(messagesPath)
		if statErr != nil || info.IsDir() || info.Size() == 0 {
			return fmt.Errorf("Data/messages.dat presente nella sorgente ma converted/messages.json non generato o vuoto")
		}
		var messagesPayload struct {
			Format       string `json:"format"`
			Source       string `json:"source"`
			MessageTypes []any  `json:"message_types"`
		}
		data, readErr := os.ReadFile(messagesPath)
		if readErr != nil {
			return fmt.Errorf("lettura converted/messages.json: %w", readErr)
		}
		if err := json.Unmarshal(data, &messagesPayload); err != nil {
			return fmt.Errorf("converted/messages.json non valido: %w", err)
		}
		if messagesPayload.Format != "pokemon-essentials-v20.1-messages" || messagesPayload.Source != "messages.dat" || messagesPayload.MessageTypes == nil {
			return fmt.Errorf("converted/messages.json non rispetta il contratto runtime Essentials v20.1")
		}
	}
	if err := validateConvertedMapTilesets(dest, convertedMaps); err != nil {
		return err
	}

	canonicalConnections, err := readCanonicalConnectionDocument(filepath.Join(dest, "converted", "map_connections.json"))
	if err != nil {
		return fmt.Errorf("connessioni canoniche: %w", err)
	}
	if report.ConnectionsConverted != len(canonicalConnections.Connections) {
		return fmt.Errorf("conteggio connessioni incoerente: report=%d file=%d", report.ConnectionsConverted, len(canonicalConnections.Connections))
	}
	sourceConnections, sourceIssues, _ := buildConnectionDocumentFromEssentialsRoot(source)
	if len(sourceIssues) > 0 {
		return fmt.Errorf("connessioni sorgente non convertibili senza perdita")
	}
	if mismatch := compareConnectionDocuments(sourceConnections, canonicalConnections); mismatch != "" {
		return fmt.Errorf("connessioni non 1:1: %s", mismatch)
	}

	if err := compareCopiedTree(filepath.Join(source, "PBS"), filepath.Join(dest, "converted", "PBS"), "PBS"); err != nil {
		return err
	}
	// assets/Graphics preserves every original file, but it may also contain
	// deterministic _PLM_ID aliases used by the converted runtime. Therefore the
	// source tree must be a byte-identical subset rather than an equal-size tree.
	if err := compareCopiedTreeContainsSource(filepath.Join(source, "Graphics"), filepath.Join(dest, "assets", "Graphics"), "Graphics"); err != nil {
		return err
	}
	if err := validateNumericAssetAliases(source, dest); err != nil {
		return err
	}
	if isDir(filepath.Join(source, "Audio")) {
		if err := compareCopiedTree(filepath.Join(source, "Audio"), filepath.Join(dest, "assets", "Audio"), "Audio"); err != nil {
			return err
		}
	}
	if isDir(filepath.Join(source, "Fonts")) {
		if err := compareCopiedTree(filepath.Join(source, "Fonts"), filepath.Join(dest, "assets", "Fonts"), "Fonts"); err != nil {
			return err
		}
	}
	if isDir(filepath.Join(source, "Plugins")) {
		if err := compareCopiedTree(filepath.Join(source, "Plugins"), filepath.Join(dest, "converted", "plugins_ruby"), "Plugins Ruby"); err != nil {
			return err
		}
	}
	if err := validatePreservedEssentialsSource(source, dest); err != nil {
		return fmt.Errorf("snapshot sorgente Essentials: %w", err)
	}

	if exists(filepath.Join(source, "Data", "Scripts.rxdata")) {
		index := filepath.Join(dest, "converted", "scripts_ruby", "index.json")
		if !exists(index) || report.RubyScriptsExtracted == 0 {
			return fmt.Errorf("Scripts.rxdata non estratto correttamente")
		}
		// The original compiled archive is authoritative conversion evidence.
		// Validate it byte-for-byte so extraction/classification can never hide a
		// dropped, reordered or modified script.
		sourceScripts := filepath.Join(source, "Data", "Scripts.rxdata")
		preservedScripts := filepath.Join(dest, "converted", "source_compiled_data", "Scripts.rxdata")
		sourceHash, err := sha256HexFile(sourceScripts)
		if err != nil {
			return fmt.Errorf("hash Scripts.rxdata sorgente: %w", err)
		}
		preservedHash, err := sha256HexFile(preservedScripts)
		if err != nil {
			return fmt.Errorf("Scripts.rxdata originale non preservato: %w", err)
		}
		if !strings.EqualFold(sourceHash, preservedHash) {
			return fmt.Errorf("Scripts.rxdata preservato diverso dalla sorgente")
		}
	}
	if exists(filepath.Join(source, "Data", "PluginScripts.rxdata")) {
		if !exists(filepath.Join(dest, "converted", "plugin_scripts_ruby", "index.json")) {
			return fmt.Errorf("PluginScripts.rxdata non estratto correttamente")
		}
	}
	return nil
}


func validateConvertedAutotiles(dest string) error {
	tilesetDir := filepath.Join(dest, "converted", "tilesets")
	entries, err := os.ReadDir(tilesetDir)
	if err != nil {
		return fmt.Errorf("metadati tileset runtime mancanti: %w", err)
	}
	autotileDir := filepath.Join(dest, "assets", "Graphics", "Autotiles")
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".json") {
			continue
		}
		var ts runtimeTilesetJSON
		b, err := os.ReadFile(filepath.Join(tilesetDir, entry.Name()))
		if err != nil {
			return err
		}
		if err := json.Unmarshal(b, &ts); err != nil {
			return fmt.Errorf("tileset runtime %s non valido: %w", entry.Name(), err)
		}
		for slot, canonical := range ts.AutotileNames {
			if strings.TrimSpace(canonical) == "" {
				continue
			}
			found := false
			for _, ext := range []string{".png", ".PNG", ".bmp", ".BMP", ".jpg", ".JPG", ".jpeg", ".JPEG"} {
				p := filepath.Join(autotileDir, canonical+ext)
				if info, statErr := os.Stat(p); statErr == nil && !info.IsDir() && info.Size() > 0 {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("autotile runtime mancante: tileset %03d slot %d alias %s in assets/Graphics/Autotiles", ts.ID, slot+1, canonical)
			}
		}
	}
	return nil
}
