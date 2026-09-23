//go:build windows

package main

import "fmt"

// derivedPermissions/derivedTerrainTags sono la lettura dei dati reali della
// mappa e del tileset. Non vengono salvati nella mappa: rappresentano il
// comportamento di base di RPG Maker XP/Pokémon Essentials. Gli override PLM
// salvati in MapXXX.json hanno sempre precedenza.
var (
	derivedPermissions = map[string]string{}
	derivedTerrainTags = map[string]int{}
)

func tableValue(table []int, tileID int) int {
	if tileID < 0 || tileID >= len(table) {
		return 0
	}
	return table[tileID]
}

// deriveMovementPermissionAt converte la passabilità RMXP della cella nel
// vocabolario Movement Permissions usato da PML Studio.
//
// RPG Maker XP valuta i layer dall'alto verso il basso. Un tile con priority >
// 0 è normalmente un overlay e, se non blocca, lascia decidere il layer sotto.
// Qualunque flag direzionale di passaggio (bit 0..3) viene convertito nel
// codice 1 "bloccato" perché il formato PML corrente ha un singolo codice per
// cella e non quattro flag direzionali separati. Le celle completamente libere
// diventano C, il codice camminabile standard già definito nell'editor.
func deriveMovementPermissionAt(x, y int) string {
	if currentMapDoc == nil || currentTileset == nil || len(currentTileset.Passages) == 0 {
		return "C"
	}
	layers := currentMapDoc.Table.Layers
	for z := len(layers) - 1; z >= 0; z-- {
		if y < 0 || y >= len(layers[z]) || x < 0 || x >= len(layers[z][y]) {
			continue
		}
		tileID := layers[z][y][x]
		if tileID <= 0 {
			continue
		}
		passage := tableValue(currentTileset.Passages, tileID)
		priority := tableValue(currentTileset.Priorities, tileID)

		// I 4 bit bassi sono i blocchi direzionali RMXP. Anche un solo lato
		// bloccato significa che la cella non è liberamente camminabile.
		if passage&0x0F != 0 {
			return "1"
		}
		// Un tile priority 0 definisce il suolo effettivo. Un overlay con
		// priority > 0 e nessun blocco lascia decidere il tile sottostante.
		if priority == 0 {
			return "C"
		}
	}
	return "C"
}

// deriveTerrainTagAt replica la semantica RMXP: il primo Terrain Tag non-zero
// incontrato partendo dal layer superiore è quello effettivo della cella.
func deriveTerrainTagAt(x, y int) int {
	if currentMapDoc == nil || currentTileset == nil || len(currentTileset.TerrainTags) == 0 {
		return 0
	}
	layers := currentMapDoc.Table.Layers
	for z := len(layers) - 1; z >= 0; z-- {
		if y < 0 || y >= len(layers[z]) || x < 0 || x >= len(layers[z][y]) {
			continue
		}
		tileID := layers[z][y][x]
		if tileID <= 0 {
			continue
		}
		tag := tableValue(currentTileset.TerrainTags, tileID)
		if tag > 0 {
			if tag > 255 {
				return 255
			}
			return tag
		}
	}
	return 0
}

func rebuildDerivedPermissionData() {
	derivedPermissions = map[string]string{}
	derivedTerrainTags = map[string]int{}
	if currentMapDoc == nil || currentMapW <= 0 || currentMapH <= 0 {
		return
	}
	for y := 0; y < currentMapH; y++ {
		for x := 0; x < currentMapW; x++ {
			key := cellKey(x, y)
			if code := deriveMovementPermissionAt(x, y); code != "C" {
				derivedPermissions[key] = code
			}
			if tag := deriveTerrainTagAt(x, y); tag != 0 {
				derivedTerrainTags[key] = tag
			}
		}
	}
	if currentTileset != nil {
		mapLogf("[PERMISSIONS] derived from tileset %d: passages=%d priorities=%d terrain_tags=%d blocked_cells=%d tagged_cells=%d",
			currentTileset.ID, len(currentTileset.Passages), len(currentTileset.Priorities), len(currentTileset.TerrainTags), len(derivedPermissions), len(derivedTerrainTags))
	}
}

func refreshDerivedPermissionCell(x, y int) {
	if x < 0 || y < 0 || x >= currentMapW || y >= currentMapH {
		return
	}
	key := cellKey(x, y)
	code := deriveMovementPermissionAt(x, y)
	if code == "C" {
		delete(derivedPermissions, key)
	} else {
		derivedPermissions[key] = code
	}
	tag := deriveTerrainTagAt(x, y)
	if tag == 0 {
		delete(derivedTerrainTags, key)
	} else {
		derivedTerrainTags[key] = tag
	}
}

func resolvedMovementPermission(x, y int) string {
	key := cellKey(x, y)
	if v, ok := permissions[key]; ok {
		if v == "" {
			return "C"
		}
		return v
	}
	if v := derivedPermissions[key]; v != "" {
		return v
	}
	return "C"
}

func resolvedTerrainTag(x, y int) int {
	key := cellKey(x, y)
	if v, ok := terrainTags[key]; ok {
		return v
	}
	return derivedTerrainTags[key]
}

func permissionSourceAt(x, y int) string {
	key := cellKey(x, y)
	if permissionEditorMode == permissionModeTerrain {
		if _, ok := terrainTags[key]; ok {
			return "override mappa"
		}
		if _, ok := derivedTerrainTags[key]; ok {
			return "Terrain Tag tileset"
		}
		return "default"
	}
	if _, ok := permissions[key]; ok {
		return "override mappa"
	}
	if _, ok := derivedPermissions[key]; ok {
		return "collisione tileset"
	}
	return "default"
}

func derivedPermissionSummary() string {
	if currentTileset == nil {
		return "tileset non caricato"
	}
	if len(currentTileset.Passages) == 0 && len(currentTileset.TerrainTags) == 0 {
		return fmt.Sprintf("Tileset %d senza tabelle passages/terrain_tags convertite", currentTileset.ID)
	}
	return fmt.Sprintf("Tileset %d: %d passaggi, %d priorità, %d terrain tag", currentTileset.ID, len(currentTileset.Passages), len(currentTileset.Priorities), len(currentTileset.TerrainTags))
}
