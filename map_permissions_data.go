//go:build windows

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
)

const (
	mapFieldMovementPermissions = "movement_permissions"
	mapFieldTerrainTags         = "terrain_tags"
	mapFieldMovementSchema      = "movement_schema"
	mapFieldTerrainSchema       = "terrain_schema"
	mapFieldBorderSchema        = "border_schema"
	mapFieldBorderDirections    = "border_directions"
	mapFieldBorderDirectionW    = "border_direction_width"
	mapFieldBorderDirectionH    = "border_direction_height"
)

var permissionDataDirty bool

func normalizeCellKey(k string) (string, bool) {
	p := strings.Split(strings.TrimSpace(k), ",")
	if len(p) != 2 {
		return "", false
	}
	x, e1 := strconv.Atoi(strings.TrimSpace(p[0]))
	y, e2 := strconv.Atoi(strings.TrimSpace(p[1]))
	if e1 != nil || e2 != nil || x < 0 || y < 0 || x >= currentMapW || y >= currentMapH {
		return "", false
	}
	return cellKey(x, y), true
}

func decodeMovementRaw(raw json.RawMessage) (map[string]string, bool) {
	if len(raw) == 0 {
		return nil, false
	}
	var obj map[string]string
	if json.Unmarshal(raw, &obj) == nil && obj != nil {
		out := map[string]string{}
		for k, v := range obj {
			nk, ok := normalizeCellKey(k)
			if !ok {
				continue
			}
			v = strings.ToUpper(strings.TrimSpace(v))
			if movementBehaviorForCode(v).Kind == "invalid" {
				continue
			}
			if v != "C" {
				out[nk] = v
			}
		}
		return out, true
	}
	var mat [][]string
	if json.Unmarshal(raw, &mat) == nil && mat != nil {
		out := map[string]string{}
		for y, row := range mat {
			for x, v := range row {
				if x >= currentMapW || y >= currentMapH {
					continue
				}
				v = strings.ToUpper(strings.TrimSpace(v))
				if movementBehaviorForCode(v).Kind == "invalid" || v == "C" || v == "" {
					continue
				}
				out[cellKey(x, y)] = v
			}
		}
		return out, true
	}
	return nil, false
}

func decodeTerrainRaw(raw json.RawMessage) (map[string]int, bool) {
	if len(raw) == 0 {
		return nil, false
	}
	var obj map[string]int
	if json.Unmarshal(raw, &obj) == nil && obj != nil {
		out := map[string]int{}
		for k, v := range obj {
			nk, ok := normalizeCellKey(k)
			if ok && v > 0 && v <= 255 {
				out[nk] = v
			}
		}
		return out, true
	}
	var mat [][]int
	if json.Unmarshal(raw, &mat) == nil && mat != nil {
		out := map[string]int{}
		for y, row := range mat {
			for x, v := range row {
				if x < currentMapW && y < currentMapH && v > 0 && v <= 255 {
					out[cellKey(x, y)] = v
				}
			}
		}
		return out, true
	}
	return nil, false
}

func loadCanonicalPermissionDataFromMap() (bool, bool) {
	if currentMapDoc == nil {
		return false, false
	}
	ml, tl := false, false
	if raw, ok := currentMapDoc.Raw[mapFieldMovementPermissions]; ok {
		if v, ok2 := decodeMovementRaw(raw); ok2 {
			permissions = v
			ml = true
		}
	}
	if raw, ok := currentMapDoc.Raw[mapFieldTerrainTags]; ok {
		if v, ok2 := decodeTerrainRaw(raw); ok2 {
			terrainTags = v
			tl = true
		}
	}
	return ml, tl
}

func loadCanonicalBorderDataFromMap() bool {
	if currentMapDoc == nil {
		return false
	}
	// Accetta esclusivamente il formato corrente 2x2. Un vecchio 4x4 non
	// viene interpretato parzialmente: resta nero finche' non viene riconfigurato.
	var schema string
	var bw, bh int
	_ = json.Unmarshal(currentMapDoc.Raw[mapFieldBorderSchema], &schema)
	_ = json.Unmarshal(currentMapDoc.Raw[mapFieldBorderDirectionW], &bw)
	_ = json.Unmarshal(currentMapDoc.Raw[mapFieldBorderDirectionH], &bh)
	if schema != "directional_2x2_v1" || bw != 2 || bh != 2 {
		return false
	}
	raw, ok := currentMapDoc.Raw[mapFieldBorderDirections]
	if !ok || len(raw) == 0 {
		return false
	}
	var dirs map[string][]int
	if json.Unmarshal(raw, &dirs) != nil || dirs == nil {
		return false
	}
	loaded := false
	for dir, key := range borderDirectionKeys {
		vals, exists := dirs[key]
		if !exists || len(vals) < borderGridCells {
			continue
		}
		for i := 0; i < borderGridCells; i++ {
			borderDirectionTiles[dir][i] = vals[i]
		}
		borderDirectionDefined[dir] = true
		loaded = true
	}
	return loaded
}

func syncPermissionDataIntoCurrentMap() error {
	if currentMapDoc == nil {
		return fmt.Errorf("nessuna mappa caricata")
	}
	// I vecchi formati bordo non fanno parte del formato PML corrente. Eliminiamo
	// eventuali residui legacy invece di mantenerli per compatibilita'.
	for _, k := range []string{"border", "border_tiles", "border_block", "border_width", "border_height"} {
		delete(currentMapDoc.Raw, k)
	}
	m, err := json.Marshal(permissions)
	if err != nil {
		return err
	}
	t, err := json.Marshal(terrainTags)
	if err != nil {
		return err
	}
	ms, _ := json.Marshal(movementSchemaAdvanceMap)
	ts, _ := json.Marshal(terrainSchemaRMXP)
	currentMapDoc.Raw[mapFieldMovementPermissions] = m
	currentMapDoc.Raw[mapFieldTerrainTags] = t
	currentMapDoc.Raw[mapFieldMovementSchema] = ms
	currentMapDoc.Raw[mapFieldTerrainSchema] = ts
	if hasDirectionalBorders() {
		dirs, _ := directionalBorderDocValues()
		bd, err := json.Marshal(dirs)
		if err != nil {
			return err
		}
		bs, _ := json.Marshal("directional_2x2_v1")
		bw, _ := json.Marshal(2)
		bh, _ := json.Marshal(2)
		currentMapDoc.Raw[mapFieldBorderDirections] = bd
		currentMapDoc.Raw[mapFieldBorderSchema] = bs
		currentMapDoc.Raw[mapFieldBorderDirectionW] = bw
		currentMapDoc.Raw[mapFieldBorderDirectionH] = bh
	} else {
		delete(currentMapDoc.Raw, mapFieldBorderDirections)
		delete(currentMapDoc.Raw, mapFieldBorderSchema)
		delete(currentMapDoc.Raw, mapFieldBorderDirectionW)
		delete(currentMapDoc.Raw, mapFieldBorderDirectionH)
	}
	return nil
}

func persistPermissionDataToRealMap() error {
	if currentMapDoc == nil || currentMapDoc.Path == "" {
		return fmt.Errorf("nessuna mappa caricata")
	}
	if err := syncPermissionDataIntoCurrentMap(); err != nil {
		return err
	}
	fresh, err := loadMapDocument(currentMapDoc.Path)
	if err != nil {
		return err
	}
	for _, k := range []string{"border", "border_tiles", "border_block", "border_width", "border_height"} {
		delete(fresh.Raw, k)
	}
	for _, k := range []string{mapFieldMovementPermissions, mapFieldTerrainTags, mapFieldMovementSchema, mapFieldTerrainSchema, mapFieldBorderSchema, mapFieldBorderDirections, mapFieldBorderDirectionW, mapFieldBorderDirectionH} {
		if v, ok := currentMapDoc.Raw[k]; ok {
			fresh.Raw[k] = v
		} else {
			delete(fresh.Raw, k)
		}
	}
	if err := saveMapDocument(fresh); err != nil {
		return err
	}
	permissionDataDirty = false
	return nil
}

func verifySavedPermissionData() error {
	if currentMapDoc == nil {
		return nil
	}
	b, err := os.ReadFile(currentMapDoc.Path)
	if err != nil {
		return err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	mv, ok := decodeMovementRaw(raw[mapFieldMovementPermissions])
	if !ok {
		return fmt.Errorf("movement_permissions assente/non valido in MapXXX.json")
	}
	tt, ok := decodeTerrainRaw(raw[mapFieldTerrainTags])
	if !ok {
		return fmt.Errorf("terrain_tags assente/non valido in MapXXX.json")
	}
	if !reflect.DeepEqual(mv, permissions) {
		return fmt.Errorf("movement_permissions su disco non coincidono con l'editor")
	}
	if !reflect.DeepEqual(tt, terrainTags) {
		return fmt.Errorf("terrain_tags su disco non coincidono con l'editor")
	}
	if hasDirectionalBorders() {
		var dirs map[string][]int
		if json.Unmarshal(raw[mapFieldBorderDirections], &dirs) != nil || dirs == nil {
			return fmt.Errorf("border_directions assente/non valido in MapXXX.json")
		}
		for dir, key := range borderDirectionKeys {
			if !borderDirectionDefined[dir] {
				continue
			}
			vals := dirs[key]
			if len(vals) < borderGridCells {
				return fmt.Errorf("border_directions.%s incompleto", key)
			}
			for i := 0; i < borderGridCells; i++ {
				if vals[i] != borderDirectionTiles[dir][i] {
					return fmt.Errorf("border_directions.%s non coincide alla cella %d", key, i+1)
				}
			}
		}
	}
	return nil
}
