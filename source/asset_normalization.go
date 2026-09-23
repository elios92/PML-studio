//go:build windows

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// PLM keeps the original Essentials Graphics tree byte-for-byte for
// compatibility, but indexed resources also receive deterministic numeric
// aliases. Map rendering must never depend on a human filename such as
// "Outside.png" once Tilesets.rxdata has been converted.
const plmAssetAliasSchema = "plm.asset_aliases.v1"

type plmAssetAliasEntry struct {
	Category          string `json:"category"`
	SourceName        string `json:"source_name"`
	SourceRelative    string `json:"source_relative"`
	CanonicalName     string `json:"canonical_name"`
	CanonicalRelative string `json:"canonical_relative"`
	SHA256            string `json:"sha256"`
	TilesetID         int    `json:"tileset_id,omitempty"`
	Slot              int    `json:"slot,omitempty"`
}

type plmAssetAliasManifest struct {
	Schema  string               `json:"schema"`
	Version int                  `json:"version"`
	Entries []plmAssetAliasEntry `json:"entries"`
}

func supportedImportedImageExtension(ext string) bool {
	switch strings.ToLower(ext) {
	case ".png", ".jpg", ".jpeg", ".gif", ".bmp", ".webp":
		return true
	default:
		return false
	}
}

func normalizeAssetLookupKey(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	ext := filepath.Ext(value)
	if supportedImportedImageExtension(ext) {
		value = strings.TrimSuffix(value, ext)
	}
	value = strings.Trim(value, "/")
	return strings.ToLower(value)
}

// findEssentialsGraphic resolves a Graphics/<category> resource exactly as an
// Essentials project names it. Relative subdirectories are supported. If only
// a bare basename is supplied, ambiguity is treated as an error instead of
// silently choosing the wrong graphic.
func findEssentialsGraphic(sourceRoot, category, logicalName string) (string, error) {
	logicalName = strings.TrimSpace(logicalName)
	if logicalName == "" {
		return "", nil
	}
	categoryRoot := filepath.Join(sourceRoot, "Graphics", filepath.FromSlash(category))
	if !isDir(categoryRoot) {
		return "", fmt.Errorf("Graphics/%s non trovata", category)
	}

	wanted := normalizeAssetLookupKey(logicalName)
	wantedBase := strings.ToLower(filepath.Base(filepath.FromSlash(wanted)))
	exact := make([]string, 0, 1)
	byBase := make([]string, 0, 1)
	err := filepath.WalkDir(categoryRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !supportedImportedImageExtension(filepath.Ext(entry.Name())) {
			return nil
		}
		rel, err := filepath.Rel(categoryRoot, path)
		if err != nil {
			return err
		}
		relKey := normalizeAssetLookupKey(filepath.ToSlash(rel))
		if relKey == wanted {
			exact = append(exact, path)
			return nil
		}
		stem := strings.ToLower(strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name())))
		if !strings.Contains(wanted, "/") && stem == wantedBase {
			byBase = append(byBase, path)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(exact)
	sort.Strings(byBase)
	if len(exact) == 1 {
		return exact[0], nil
	}
	if len(exact) > 1 {
		return "", fmt.Errorf("grafica ambigua Graphics/%s/%s (%d corrispondenze)", category, logicalName, len(exact))
	}
	if len(byBase) == 1 {
		return byBase[0], nil
	}
	if len(byBase) > 1 {
		return "", fmt.Errorf("grafica ambigua Graphics/%s/%s (%d file con lo stesso nome)", category, logicalName, len(byBase))
	}
	return "", fmt.Errorf("grafica mancante Graphics/%s/%s", category, logicalName)
}

func createNumericGraphicAlias(sourceRoot, destRoot, category, sourceName, canonicalName string, tilesetID, slot int) (plmAssetAliasEntry, error) {
	sourcePath, err := findEssentialsGraphic(sourceRoot, category, sourceName)
	if err != nil {
		return plmAssetAliasEntry{}, err
	}
	if sourcePath == "" {
		return plmAssetAliasEntry{}, nil
	}
	ext := strings.ToLower(filepath.Ext(sourcePath))
	if !supportedImportedImageExtension(ext) {
		return plmAssetAliasEntry{}, fmt.Errorf("formato grafico non supportato: %s", sourcePath)
	}

	// Canonical PLM graphics live directly in assets/Graphics/<category>.
	// Preserve the original Essentials files, but never hide runtime aliases in
	// a private subdirectory: editor and runtime must resolve the same path.
	canonicalRelative := filepath.Join("assets", "Graphics", category, canonicalName+ext)
	canonicalPath := filepath.Join(destRoot, canonicalRelative)
	sourceHash, err := sha256HexFile(sourcePath)
	if err != nil {
		return plmAssetAliasEntry{}, err
	}
	if exists(canonicalPath) {
		existingHash, hashErr := sha256HexFile(canonicalPath)
		if hashErr != nil {
			return plmAssetAliasEntry{}, hashErr
		}
		if !strings.EqualFold(sourceHash, existingHash) {
			return plmAssetAliasEntry{}, fmt.Errorf("collisione alias grafico %s: esiste gia un file diverso", canonicalRelative)
		}
	} else if err := copyFileAtomic(sourcePath, canonicalPath); err != nil {
		return plmAssetAliasEntry{}, err
	}
	aliasHash, err := sha256HexFile(canonicalPath)
	if err != nil {
		return plmAssetAliasEntry{}, err
	}
	if !strings.EqualFold(sourceHash, aliasHash) {
		return plmAssetAliasEntry{}, fmt.Errorf("alias grafico non identico alla sorgente: %s", canonicalRelative)
	}
	sourceRel, _ := filepath.Rel(sourceRoot, sourcePath)
	return plmAssetAliasEntry{
		Category:          category,
		SourceName:        sourceName,
		SourceRelative:    filepath.ToSlash(sourceRel),
		CanonicalName:     canonicalName,
		CanonicalRelative: filepath.ToSlash(canonicalRelative),
		SHA256:            sourceHash,
		TilesetID:         tilesetID,
		Slot:              slot,
	}, nil
}

func cleanupGeneratedGraphicAliases(destRoot string) error {
	manifestPath := filepath.Join(destRoot, "converted", "asset_aliases.json")
	if b, err := os.ReadFile(manifestPath); err == nil && len(b) > 0 {
		var manifest plmAssetAliasManifest
		if json.Unmarshal(b, &manifest) == nil {
			root := filepath.Clean(destRoot)
			for _, entry := range manifest.Entries {
				rel := filepath.Clean(filepath.FromSlash(entry.CanonicalRelative))
				if rel == "." || filepath.IsAbs(rel) {
					continue
				}
				abs := filepath.Clean(filepath.Join(root, rel))
				prefix := root + string(os.PathSeparator)
				if !strings.HasPrefix(strings.ToLower(abs), strings.ToLower(prefix)) {
					continue
				}
				_ = os.Remove(abs)
			}
		}
	}

	// Migration cleanup for builds that generated numeric aliases under
	// _PLM_ID. These directories contain generated data only.
	for _, category := range []string{"Tilesets", "Autotiles"} {
		legacy := filepath.Join(destRoot, "assets", "Graphics", category, "_PLM_ID")
		if err := os.RemoveAll(legacy); err != nil {
			return fmt.Errorf("pulizia alias legacy %s: %w", category, err)
		}
	}
	return nil
}

func writeAssetAliasManifest(destRoot string, entries []plmAssetAliasEntry) error {
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Category != entries[j].Category {
			return entries[i].Category < entries[j].Category
		}
		if entries[i].TilesetID != entries[j].TilesetID {
			return entries[i].TilesetID < entries[j].TilesetID
		}
		if entries[i].Slot != entries[j].Slot {
			return entries[i].Slot < entries[j].Slot
		}
		return entries[i].CanonicalName < entries[j].CanonicalName
	})
	manifest := plmAssetAliasManifest{Schema: plmAssetAliasSchema, Version: 1, Entries: entries}
	return writeJSON(filepath.Join(destRoot, "converted", "asset_aliases.json"), manifest)
}
