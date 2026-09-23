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

type importedRegionInfo struct {
	Index int
	Name  string
}

func readPBSLines(path string) []string {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	s := strings.TrimPrefix(string(b), "\ufeff")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.Split(s, "\n")
}

func parseTownMapRegions(path string) map[int]importedRegionInfo {
	out := map[int]importedRegionInfo{}
	secRE := regexp.MustCompile(`^\s*\[(\d+)\]\s*$`)
	current := -1
	for _, raw := range readPBSLines(path) {
		line := strings.TrimSpace(strings.SplitN(raw, "#", 2)[0])
		if line == "" {
			continue
		}
		if m := secRE.FindStringSubmatch(line); len(m) == 2 {
			current, _ = strconv.Atoi(m[1])
			if current >= 0 {
				out[current] = importedRegionInfo{Index: current, Name: fmt.Sprintf("Regione %d", current+1)}
			}
			continue
		}
		if current < 0 || !strings.Contains(line, "=") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		if strings.EqualFold(key, "Name") && val != "" {
			r := out[current]
			r.Name = val
			out[current] = r
		}
	}
	return out
}

func parseMapMetadataRegions(path string) map[int]int {
	out := map[int]int{}
	secRE := regexp.MustCompile(`^\s*\[(\d+)\]\s*$`)
	currentMap := 0
	for _, raw := range readPBSLines(path) {
		line := strings.TrimSpace(strings.SplitN(raw, "#", 2)[0])
		if line == "" {
			continue
		}
		if m := secRE.FindStringSubmatch(line); len(m) == 2 {
			currentMap, _ = strconv.Atoi(m[1])
			continue
		}
		if currentMap <= 0 || !strings.Contains(line, "=") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if !strings.EqualFold(strings.TrimSpace(parts[0]), "MapPosition") {
			continue
		}
		vals := strings.Split(strings.TrimSpace(parts[1]), ",")
		if len(vals) < 1 {
			continue
		}
		regionIndex, err := strconv.Atoi(strings.TrimSpace(vals[0]))
		if err == nil && regionIndex >= 0 {
			out[currentMap] = regionIndex
		}
	}
	return out
}

func writeImportedRegionManifest(project string, r RegionDefinition) error {
	abs := filepath.Join(project, filepath.FromSlash(r.MapsFolder))
	if err := os.MkdirAll(abs, 0755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(abs, "region.json"), b, 0644)
}

// organizeImportedMapsByRegions mirrors Essentials' world-map region number.
// MapPosition's first integer is the region index. Maps with no explicit
// MapPosition remain in converted/maps and are intentionally left unassigned.
func organizeImportedMapsByRegions(project string) (regionsCreated, mapsMoved int, err error) {
	pbsRoot := filepath.Join(project, "converted", "PBS")
	townPath := filepath.Join(pbsRoot, "town_map.txt")
	metadataPath := filepath.Join(pbsRoot, "map_metadata.txt")
	if !exists(townPath) || !exists(metadataPath) {
		return 0, 0, nil
	}

	names := parseTownMapRegions(townPath)
	mapRegions := parseMapMetadataRegions(metadataPath)
	if len(names) == 0 || len(mapRegions) == 0 {
		return 0, 0, nil
	}

	// Every world-map region declared in town_map.txt gets its own PLM folder,
	// even if it currently contains no MapPosition entries. This keeps Regione 1,
	// Regione 2, ... stable while the project grows. Metadata may also reference
	// a region not declared in town_map.txt; preserve it with a generated name.
	used := map[int]bool{}
	for idx := range names {
		used[idx] = true
	}
	for _, regionIndex := range mapRegions {
		used[regionIndex] = true
		if _, ok := names[regionIndex]; !ok {
			names[regionIndex] = importedRegionInfo{Index: regionIndex, Name: fmt.Sprintf("Regione %d", regionIndex+1)}
		}
	}

	indices := make([]int, 0, len(used))
	for idx := range used {
		indices = append(indices, idx)
	}
	sort.Ints(indices)

	registry := RegionRegistry{Version: regionRegistryVersion, Enabled: true}
	byIndex := map[int]RegionDefinition{}
	for _, idx := range indices {
		info := names[idx]
		id := fmt.Sprintf("region_%d", idx)
		r := canonicalRegionDefinition(info.Name, id)
		byIndex[idx] = r
		registry.Regions = append(registry.Regions, r)
		if registry.ActiveRegionID == "" {
			registry.ActiveRegionID = r.ID
		}
		if err := writeImportedRegionManifest(project, r); err != nil {
			return regionsCreated, mapsMoved, err
		}
		regionsCreated++
	}

	mapsRoot := filepath.Join(project, "converted", "maps")
	ids := make([]int, 0, len(mapRegions))
	for id := range mapRegions {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	for _, mapID := range ids {
		r, ok := byIndex[mapRegions[mapID]]
		if !ok {
			continue
		}
		src := filepath.Join(mapsRoot, fmt.Sprintf("Map%03d.json", mapID))
		if !exists(src) {
			continue
		}
		dst := filepath.Join(project, filepath.FromSlash(r.MapsFolder), filepath.Base(src))
		if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
			return regionsCreated, mapsMoved, err
		}
		if exists(dst) {
			return regionsCreated, mapsMoved, fmt.Errorf("Map%03d esiste già nella regione %s", mapID, r.Name)
		}
		if err := os.Rename(src, dst); err != nil {
			return regionsCreated, mapsMoved, err
		}
		mapsMoved++
	}

	if err := writeJSON(filepath.Join(project, "plm_regions.json"), registry); err != nil {
		return regionsCreated, mapsMoved, err
	}
	return regionsCreated, mapsMoved, nil
}
