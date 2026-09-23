//go:build windows

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

const (
	permissionModeMovement   = "movement"
	permissionModeTerrain    = "terrain"
	movementSchemaAdvanceMap = "advance_map_collision_00_3f_v1"
	terrainSchemaRMXP        = "rpg_maker_xp_plus_plugin_terrain_tag_0_255_v3"
)

var (
	permissionPaletteScroll int
	hwndPermBehaviorHint    syscall.Handle
	permissionHintTracked   bool
)

// movementPermissionCodes restituisce l'intero byte collisione usato dalla
// vista Movement Permissions di Advance Map/Porymap: 00..3F.
func movementPermissionCodes() []string {
	out := make([]string, 0, 0x40)
	for i := 0; i <= 0x3F; i++ {
		out = append(out, strings.ToUpper(strconv.FormatInt(int64(i), 16)))
	}
	return out
}

func terrainTagCodes() []int {
	out := make([]int, 0, 256)
	for i := 0; i <= 255; i++ {
		out = append(out, i)
	}
	return out
}

func movementBehaviorForCode(code string) MovementBehaviorSpec {
	code = strings.ToUpper(strings.TrimSpace(code))
	n64, err := strconv.ParseInt(code, 16, 32)
	if err != nil || n64 < 0 || n64 > 0x3F {
		return MovementBehaviorSpec{Code: code, Kind: "invalid", Description: "Codice movimento non valido"}
	}
	n := int(n64)
	spec := MovementBehaviorSpec{Code: strings.ToUpper(strconv.FormatInt(int64(n), 16)), Elevation: -1}

	switch n {
	case 0x00:
		spec.Kind = "transition"
		spec.Passable = true
		spec.Transition = true
		spec.Description = "Cambio quota / transizione tra livelli (scale, porte, raccordi)"
		return spec
	case 0x01:
		spec.Kind = "blocked"
		spec.Blocked = true
		spec.Description = "Invalicabile a tutte le quote"
		return spec
	case 0x04:
		spec.Kind = "surf"
		spec.Passable = true
		spec.Surf = true
		spec.Elevation = 0
		spec.Description = "Acqua transitabile con Surf (quota acqua)"
		return spec
	case 0x05:
		spec.Kind = "surf_blocked"
		spec.Blocked = true
		spec.Surf = true
		spec.Elevation = 0
		spec.Description = "Ostacolo in acqua: blocca il movimento durante Surf"
		return spec
	case 0x3C:
		spec.Kind = "multi_level"
		spec.Passable = true
		spec.MultiLevel = true
		spec.Description = "Ponte / multi-livello: mantiene la quota di ingresso e consente passaggio sopra/sotto"
		return spec
	case 0x3D:
		spec.Kind = "multi_level_blocked"
		spec.Blocked = true
		spec.MultiLevel = true
		spec.Description = "Variante invalicabile del multi-livello/ponte"
		return spec
	}

	// Advance Map codifica le elevazioni in gruppi di 4. I valori base
	// 08,0C,10,...,38 sono transitabili alla quota corrispondente; +1 e'
	// la variante invalicabile della stessa quota.
	if n >= 0x08 && n <= 0x39 {
		base := n &^ 0x03
		elevation := base/4 - 1
		variant := n & 0x03
		spec.Elevation = elevation
		switch variant {
		case 0:
			spec.Kind = "elevation"
			spec.Passable = true
			spec.Description = fmt.Sprintf("Transitabile alla quota %d", elevation)
		case 1:
			spec.Kind = "elevation_blocked"
			spec.Blocked = true
			spec.Description = fmt.Sprintf("Ostacolo alla quota %d", elevation)
		default:
			spec.Kind = "elevation_special"
			spec.Description = fmt.Sprintf("Variante speciale collisione %X alla quota %d; comportamento preservato per il runtime", variant, elevation)
		}
		return spec
	}

	spec.Kind = "reserved"
	spec.Description = "Codice Advance Map riservato/speciale; il valore viene preservato senza reinterpretarlo"
	return spec
}

func movementBehaviorDefinitions() map[string]MovementBehaviorSpec {
	out := make(map[string]MovementBehaviorSpec, 0x40)
	for _, code := range movementPermissionCodes() {
		out[code] = movementBehaviorForCode(code)
	}
	return out
}

func terrainTagBehavior(tag int) TerrainTagSpec {
	if tag <= 0 {
		return TerrainTagSpec{Tag: 0, Kind: "none", Description: "Nessun Terrain Tag"}
	}

	// I tag 0..7 sono quelli nativi esposti dall'editor RPG Maker XP.
	// Il loro significato concreto dipende dal tileset/runtime del progetto.
	if tag <= 7 {
		return TerrainTagSpec{
			Tag:         tag,
			Kind:        "rpg_maker_xp",
			Description: fmt.Sprintf("Terrain Tag RPG Maker XP %d", tag),
		}
	}

	// Registro ufficiale dei Terrain Tag estesi usati dalle MN del progetto.
	// 18/19 e 20 provengono gia' dal plugin originale. 21..26 sono assegnati
	// alle sei MN personalizzate, in modo che editor e runtime possano
	// riconoscere in modo univoco il comportamento richiesto dalla casella.
	switch tag {
	case 18:
		return TerrainTagSpec{
			Tag:               18,
			Kind:              "rock_climb_up",
			Description:       "MN07 ROCKCLIMB - parete Scalaroccia / direzione superiore",
			FieldMove:         "ROCKCLIMB",
			Activation:        "front_interaction",
			RuntimeAction:     "rock_climb",
			RequiresFieldMove: true,
			BlocksWithoutMove: true,
			Extended:          true,
		}
	case 19:
		return TerrainTagSpec{
			Tag:               19,
			Kind:              "rock_climb_down",
			Description:       "MN07 ROCKCLIMB - parete Scalaroccia / direzione inferiore",
			FieldMove:         "ROCKCLIMB",
			Activation:        "front_interaction",
			RuntimeAction:     "rock_climb",
			RequiresFieldMove: true,
			BlocksWithoutMove: true,
			Extended:          true,
		}
	case 20:
		return TerrainTagSpec{
			Tag:               20,
			Kind:              "whirlpool",
			Description:       "MN12 WHIRLPOOL - vortice attraversabile durante Surf",
			FieldMove:         "WHIRLPOOL",
			Activation:        "front_interaction",
			RuntimeAction:     "cross_whirlpool",
			RequiresSurf:      true,
			RequiresFieldMove: true,
			BlocksWithoutMove: true,
			Extended:          true,
		}
	case 21:
		return TerrainTagSpec{
			Tag:               21,
			Kind:              "breakable_wall",
			Description:       "MN10 RUMPABREAKING - muro/ostacolo fragile distruggibile per aprire un percorso o area segreta",
			FieldMove:         "RUMPABREAKING",
			Activation:        "front_interaction",
			RuntimeAction:     "break_fragile_obstacle",
			RequiresFieldMove: true,
			BlocksWithoutMove: true,
			CustomMN:          true,
			Extended:          true,
		}
	case 22:
		return TerrainTagSpec{
			Tag:               22,
			Kind:              "rapid_growth",
			Description:       "MN11 RAPIDGROWTH - pianta/rampicante che puo' crescere per creare percorsi, rimuovere ostacoli o risolvere enigmi",
			FieldMove:         "RAPIDGROWTH",
			Activation:        "front_interaction",
			RuntimeAction:     "rapid_growth",
			RequiresFieldMove: true,
			CustomMN:          true,
			Extended:          true,
		}
	case 23:
		return TerrainTagSpec{
			Tag:               23,
			Kind:              "ultra_time_rift",
			Description:       "MN13 ULTRATIMERISK - punto di attivazione per apertura varco e trasferimento alla destinazione configurata",
			FieldMove:         "ULTRATIMERISK",
			Activation:        "front_interaction",
			RuntimeAction:     "open_ultra_time_rift",
			RequiresFieldMove: true,
			CustomMN:          true,
			Extended:          true,
		}
	case 24:
		return TerrainTagSpec{
			Tag:               24,
			Kind:              "wind_hazard",
			Description:       "MN14 BREZZOAREA - zona di vento/uragano/tornado attraversabile in sicurezza con protezione temporanea",
			FieldMove:         "BREZZOAREA",
			Activation:        "hazard_zone",
			RuntimeAction:     "neutralize_wind_hazard",
			RequiresFieldMove: true,
			BlocksWithoutMove: true,
			TemporaryEffect:   true,
			CustomMN:          true,
			Extended:          true,
		}
	case 25:
		return TerrainTagSpec{
			Tag:               25,
			Kind:              "unstable_ground_hazard",
			Description:       "MN15 THERMOMOTION - palude/sabbie mobili attraversabili in sicurezza con protezione temporanea",
			FieldMove:         "THERMOMOTION",
			Activation:        "hazard_zone",
			RuntimeAction:     "stabilize_unstable_ground",
			RequiresFieldMove: true,
			BlocksWithoutMove: true,
			TemporaryEffect:   true,
			CustomMN:          true,
			Extended:          true,
		}
	case 26:
		return TerrainTagSpec{
			Tag:               26,
			Kind:              "digital_glitch",
			Description:       "MN16 TECORRUPTION - dispositivo/glitch/sistema di sicurezza manipolabile dalla MN",
			FieldMove:         "TECORRUPTION",
			Activation:        "front_interaction",
			RuntimeAction:     "alter_digital_system",
			RequiresFieldMove: true,
			CustomMN:          true,
			Extended:          true,
		}
	}

	// Qualunque altro valore viene preservato integralmente per plugin futuri.
	return TerrainTagSpec{
		Tag:         tag,
		Kind:        "custom_plugin",
		Description: fmt.Sprintf("Terrain Tag esteso/plugin %d", tag),
		Extended:    true,
	}
}

func terrainTagDefinitions() map[string]TerrainTagSpec {
	// Evita di serializzare 256 descrizioni in ogni singola mappa. Salviamo
	// sempre i tag RPG Maker XP, quelli estesi gia' noti al progetto e tutti
	// i tag realmente usati nella mappa corrente.
	tags := map[int]bool{}
	for i := 0; i <= 7; i++ {
		tags[i] = true
	}
	for _, tag := range []int{18, 19, 20, 21, 22, 23, 24, 25, 26, selectedTerrainTag} {
		if tag >= 0 && tag <= 255 {
			tags[tag] = true
		}
	}
	for _, tag := range terrainTags {
		if tag >= 0 && tag <= 255 {
			tags[tag] = true
		}
	}

	ordered := make([]int, 0, len(tags))
	for tag := range tags {
		ordered = append(ordered, tag)
	}
	sort.Ints(ordered)

	out := map[string]TerrainTagSpec{}
	for _, tag := range ordered {
		out[strconv.Itoa(tag)] = terrainTagBehavior(tag)
	}
	return out
}

func sideDir() string {
	if currentProject == "" {
		return ""
	}
	p := filepath.Join(currentProject, ".plm", "editor")
	_ = os.MkdirAll(filepath.Join(p, "permissions"), 0755)
	_ = os.MkdirAll(filepath.Join(p, "events"), 0755)
	return p
}

func cellKey(x, y int) string { return fmt.Sprintf("%d,%d", x, y) }

func loadSidecars() {
	permissions = map[string]string{}
	terrainTags = map[string]int{}
	permissionDataDirty = false
	resetBorderBlock()
	events = nil
	selectedEvent = -1
	if currentMap == nil {
		derivedPermissions = map[string]string{}
		derivedTerrainTags = map[string]int{}
		return
	}
	// Prima di leggere gli override PLM ricostruiamo il comportamento reale
	// dalla mappa e dalle tabelle del tileset (passages/priorities/terrain tags).
	rebuildDerivedPermissionData()
	d := sideDir()
	pp := filepath.Join(d, "permissions", fmt.Sprintf("Map%03d.json", currentMap.ID))
	var legacy PermissionDoc
	legacyOK := false
	if b, e := os.ReadFile(pp); e == nil && json.Unmarshal(b, &legacy) == nil {
		legacyOK = true
		loadBorderBlock(legacy)
	}

	// Fonte canonica: MapXXX.json. Il sidecar viene usato solo come migrazione
	// per progetti 0.5 precedenti che non hanno ancora i campi nella mappa.
	movementLoaded, terrainLoaded := loadCanonicalPermissionDataFromMap()
	_ = loadCanonicalBorderDataFromMap()
	if !movementLoaded && legacyOK {
		if legacy.MovementPermissions != nil {
			permissions = legacy.MovementPermissions
		} else if legacy.Cells != nil {
			permissions = legacy.Cells
		}
		permissionDataDirty = true
	}
	if !terrainLoaded && legacyOK && legacy.TerrainTags != nil {
		terrainTags = legacy.TerrainTags
		permissionDataDirty = true
	}
	// Mantiene in memoria i campi canonici; verranno scritti al primo Salva,
	// alla prima modifica o prima del Playtest.
	_ = syncPermissionDataIntoCurrentMap()

	// Gli eventi reali della MapXXX.json hanno precedenza assoluta. Il vecchio
	// sidecar .plm viene letto soltanto se la mappa convertita non contiene
	// ancora un campo events (compatibilità con le prime build 0.5).
	nativeEvents, nativeErr := loadEventsFromRealMap()
	if nativeErr != nil {
		mapLogf("[EVENTS] Map%03d lettura eventi reali: %v", currentMap.ID, nativeErr)
	}
	if !nativeEvents {
		ep := filepath.Join(d, "events", fmt.Sprintf("Map%03d.json", currentMap.ID))
		if b, e := os.ReadFile(ep); e == nil {
			var doc EventDoc
			if json.Unmarshal(b, &doc) == nil {
				events = doc.Events
			}
		}
	}
}

func savePermissions() {
	if currentMap == nil {
		return
	}
	if knownMapDuplicateConflict() {
		setText(hwndStatus, "Salvataggio movimenti/terrain tags bloccato: Map ID duplicati da risolvere")
		return
	}
	d := sideDir()
	directionalFlat, directionalBlocks := directionalBorderDocValues()
	doc := PermissionDoc{
		Version: 6, MapID: currentMap.ID, Width: currentMapW, Height: currentMapH,
		Cells:                 permissions,
		MovementPermissions:   permissions,
		TerrainTags:           terrainTags,
		MovementSchema:        movementSchemaAdvanceMap,
		TerrainSchema:         terrainSchemaRMXP,
		MovementBehaviors:     movementBehaviorDefinitions(),
		TerrainBehaviors:      terrainTagDefinitions(),
		BorderSchema:          "directional_2x2_v1",
		BorderDirections:      directionalFlat,
		BorderDirectionBlocks: directionalBlocks,
	}
	if hasDirectionalBorders() {
		doc.BorderDirectionWidth = 2
		doc.BorderDirectionHeight = 2
	}
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		setText(hwndStatus, "Errore salvataggio movimenti/terrain tags: "+err.Error())
		return
	}
	if err := os.WriteFile(filepath.Join(d, "permissions", fmt.Sprintf("Map%03d.json", currentMap.ID)), b, 0644); err != nil {
		setText(hwndStatus, "Errore salvataggio movimenti/terrain tags: "+err.Error())
		return
	}
	if permissionDataDirty {
		if err := persistPermissionDataToRealMap(); err != nil {
			setText(hwndStatus, "Errore salvataggio dati reali mappa: "+err.Error())
			return
		}
		setText(hwndStatus, "Movimenti e Terrain Tags salvati in MapXXX.json")
	}
}

func permissionOverlayValue(x, y int) string {
	if permissionEditorMode == permissionModeTerrain {
		return strconv.Itoa(resolvedTerrainTag(x, y))
	}
	return strings.ToUpper(resolvedMovementPermission(x, y))
}

func advanceMapCodeColor(code string) uintptr {
	n64, _ := strconv.ParseInt(strings.TrimSpace(code), 16, 32)
	n := int(n64)
	fixed := []uintptr{
		rgb(20, 45, 220), rgb(235, 35, 35), rgb(20, 220, 90), rgb(20, 190, 200),
		rgb(220, 35, 220), rgb(245, 220, 20), rgb(95, 45, 150), rgb(135, 20, 20),
		rgb(115, 120, 25), rgb(20, 155, 45), rgb(20, 135, 125), rgb(45, 60, 155),
		rgb(105, 45, 155), rgb(225, 45, 115), rgb(175, 95, 35), rgb(245, 135, 20),
		rgb(55, 155, 50), rgb(65, 225, 85), rgb(125, 30, 65), rgb(50, 55, 95),
		rgb(55, 145, 55), rgb(85, 160, 195), rgb(235, 150, 35), rgb(125, 45, 45),
		rgb(20, 120, 125), rgb(205, 40, 45), rgb(65, 170, 70), rgb(45, 80, 150),
	}
	if n >= 0 && n < len(fixed) {
		return fixed[n]
	}
	// I codici superiori continuano la stessa famiglia cromatica in modo
	// deterministico, cosi' la palette resta leggibile senza significati inventati.
	return rgb(byte(45+(n*53)%180), byte(45+(n*97)%180), byte(45+(n*31)%180))
}

func terrainTagColor(tag int) uintptr {
	colors := []uintptr{
		rgb(90, 90, 90), rgb(205, 45, 45), rgb(45, 145, 210), rgb(60, 165, 75),
		rgb(215, 135, 30), rgb(135, 70, 180), rgb(30, 155, 150), rgb(190, 75, 130),
	}
	if tag >= 0 && tag < len(colors) {
		return colors[tag]
	}
	// Evidenzia in modo stabile i tag MN gia' presenti nel progetto.
	switch tag {
	case 18:
		return rgb(110, 105, 70)
	case 19:
		return rgb(145, 125, 75)
	case 20:
		return rgb(45, 105, 205)
	case 21: // RUMPABREAKING
		return rgb(150, 80, 55)
	case 22: // RAPIDGROWTH
		return rgb(45, 165, 70)
	case 23: // ULTRATIMERISK
		return rgb(145, 70, 200)
	case 24: // BREZZOAREA
		return rgb(70, 170, 215)
	case 25: // THERMOMOTION
		return rgb(210, 135, 45)
	case 26: // TECORRUPTION
		return rgb(35, 185, 170)
	}
	if tag < 0 {
		tag = 0
	}
	// Colore deterministico per qualunque tag plugin/custom.
	return rgb(byte(45+(tag*67)%180), byte(45+(tag*101)%180), byte(45+(tag*43)%180))
}

func permissionOverlayColor(x, y int) uintptr {
	if permissionEditorMode == permissionModeTerrain {
		return terrainTagColor(resolvedTerrainTag(x, y))
	}
	return advanceMapCodeColor(permissionOverlayValue(x, y))
}

func permissionTextColor(color uintptr) uintptr {
	r := color & 0xFF
	g := (color >> 8) & 0xFF
	b := (color >> 16) & 0xFF
	if r+g+b < 300 {
		return rgb(250, 250, 250)
	}
	return rgb(15, 15, 15)
}

func permissionDescription() string {
	if permissionEditorMode == permissionModeTerrain {
		return terrainTagBehavior(selectedTerrainTag).Description
	}
	return movementBehaviorForCode(selectedPerm).Description
}

func setPermissionEditorMode(m string) {
	hidePermissionBehaviorHint()
	if m != permissionModeTerrain {
		m = permissionModeMovement
	}
	permissionEditorMode = m
	permissionPaletteScroll = 0
	updatePermissionControls()
	updatePaletteScroll(hwndPermissionsPalette)
	invalidate(hwndPermissionsPalette)
	invalidate(hwndCanvas)
	if m == permissionModeTerrain {
		setText(hwndStatus, fmt.Sprintf("Terrain Tags - selezionato %d - %s", selectedTerrainTag, terrainTagBehavior(selectedTerrainTag).Description))
	} else {
		spec := movementBehaviorForCode(selectedPerm)
		setText(hwndStatus, "Movimenti permessi - codice "+spec.Code+" - "+spec.Description)
	}
}

func selectMovementPermission(code string) {
	code = strings.ToUpper(strings.TrimSpace(code))
	spec := movementBehaviorForCode(code)
	if spec.Kind == "invalid" {
		return
	}
	selectedPerm = spec.Code
	updatePermissionControls()
	invalidate(hwndPermissionsPalette)
	setText(hwndStatus, "Movimento "+spec.Code+" - "+spec.Description)
}

func selectTerrainTag(tag int) {
	if tag < 0 {
		tag = 0
	}
	if tag > 255 {
		tag = 255
	}
	selectedTerrainTag = tag
	updatePermissionControls()
	invalidate(hwndPermissionsPalette)
	setText(hwndStatus, fmt.Sprintf("Terrain Tag %d - %s", tag, terrainTagBehavior(tag).Description))
}

func applyPermissionAt(x, y int) {
	if currentMap == nil || x < 0 || y < 0 || x >= currentMapW || y >= currentMapH {
		return
	}
	key := cellKey(x, y)
	if permissionEditorMode == permissionModeTerrain {
		oldResolved := resolvedTerrainTag(x, y)
		base := derivedTerrainTags[key]
		// Se il valore scelto coincide con il tileset reale l'override non serve.
		// Lo 0 viene invece salvato esplicitamente quando deve annullare un tag base.
		if selectedTerrainTag == base {
			delete(terrainTags, key)
		} else {
			terrainTags[key] = selectedTerrainTag
		}
		if oldResolved != selectedTerrainTag {
			permissionDataDirty = true
			_ = syncPermissionDataIntoCurrentMap()
		}
		setText(hwndStatus, fmt.Sprintf("Terrain Tag %d applicato a %d,%d - %s [override mappa]", selectedTerrainTag, x, y, terrainTagBehavior(selectedTerrainTag).Description))
	} else {
		oldResolved := resolvedMovementPermission(x, y)
		base := derivedPermissions[key]
		if base == "" {
			base = "C"
		}
		// Anche C deve poter essere un override esplicito se il tileset reale
		// considera la cella bloccata. Se coincide col valore derivato, eliminiamo
		// l'override e torniamo a seguire automaticamente il tileset.
		if selectedPerm == base {
			delete(permissions, key)
		} else {
			permissions[key] = selectedPerm
		}
		if oldResolved != selectedPerm {
			permissionDataDirty = true
			_ = syncPermissionDataIntoCurrentMap()
		}
		spec := movementBehaviorForCode(selectedPerm)
		setText(hwndStatus, fmt.Sprintf("Movimento %s applicato a %d,%d - %s [override mappa]", selectedPerm, x, y, spec.Description))
	}
}

func samplePermissionAt(x, y int) {
	if currentMap == nil || x < 0 || y < 0 || x >= currentMapW || y >= currentMapH {
		return
	}
	if permissionEditorMode == permissionModeTerrain {
		tag := resolvedTerrainTag(x, y)
		selectTerrainTag(tag)
		setText(hwndStatus, fmt.Sprintf("Terrain Tag %d campionato a %d,%d [%s]", tag, x, y, permissionSourceAt(x, y)))
		return
	}
	code := resolvedMovementPermission(x, y)
	selectMovementPermission(code)
	setText(hwndStatus, fmt.Sprintf("Movimento %s campionato a %d,%d [%s]", code, x, y, permissionSourceAt(x, y)))
}

func permissionHelpText() string {
	if permissionEditorMode == permissionModeTerrain {
		return fmt.Sprintf("%s\r\n\r\nSinistro/trascina: applica\r\nDestro: campiona", permissionDescription())
	}
	spec := movementBehaviorForCode(selectedPerm)
	extra := ""
	if spec.Elevation >= 0 {
		extra = fmt.Sprintf("\r\nQuota: %d", spec.Elevation)
	}
	return fmt.Sprintf("Codice %s\r\n%s%s\r\n\r\nSinistro/trascina: applica\r\nDestro: campiona", spec.Code, spec.Description, extra)
}

func updatePermissionControls() {
	active := mode == "permissions"
	showControl(hwndPermModeMovement, active)
	showControl(hwndPermModeTerrain, active)
	showControl(hwndPermSelected, active)
	showControl(hwndPermHelp, active)
	showControl(hwndPermissionsPalette, active)
	for _, h := range hwndMovementButtons {
		showControl(h, false)
	}
	for _, h := range hwndTerrainButtons {
		showControl(h, false)
	}
	if !active {
		hidePermissionBehaviorHint()
		return
	}
	// I due pulsanti hanno testo fisso e stato push/radio: l'utente vede
	// sempre chiaramente quale delle due liste indipendenti e' attiva.
	setText(hwndPermModeMovement, "Movimenti permessi")
	setText(hwndPermModeTerrain, "Terrain Tags")
	if permissionEditorMode == permissionModeMovement {
		pSendMessageW.Call(uintptr(hwndPermModeMovement), BM_SETCHECK, BST_CHECKED, 0)
		pSendMessageW.Call(uintptr(hwndPermModeTerrain), BM_SETCHECK, BST_UNCHECKED, 0)
		setText(hwndPermSelected, "Selezionato: "+selectedPerm)
	} else {
		pSendMessageW.Call(uintptr(hwndPermModeMovement), BM_SETCHECK, BST_UNCHECKED, 0)
		pSendMessageW.Call(uintptr(hwndPermModeTerrain), BM_SETCHECK, BST_CHECKED, 0)
		setText(hwndPermSelected, fmt.Sprintf("Selezionato: Terrain %d", selectedTerrainTag))
	}
	setText(hwndPermHelp, permissionHelpText())
}

func handlePermissionCommand(id uint16) bool {
	switch id {
	case idPermModeMovement:
		if mode != "permissions" {
			setMode("permissions")
		}
		setPermissionEditorMode(permissionModeMovement)
		return true
	case idPermModeTerrain:
		if mode != "permissions" {
			setMode("permissions")
		}
		setPermissionEditorMode(permissionModeTerrain)
		return true
	}
	return false
}

func permissionPaletteCount() int {
	if permissionEditorMode == permissionModeTerrain {
		return len(terrainTagCodes())
	}
	return len(movementPermissionCodes())
}

func permissionPaletteRowsVisible(hwnd syscall.Handle) int {
	var r RECT
	pGetClientRect.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&r)))
	rows := int(r.Bottom-r.Top) / 30
	if rows < 1 {
		rows = 1
	}
	return rows
}

func updatePermissionPaletteScroll(hwnd syscall.Handle) {
	if hwnd == 0 {
		return
	}
	rowsVisible := permissionPaletteRowsVisible(hwnd)
	maxPos := permissionPaletteCount() - rowsVisible
	if maxPos < 0 {
		maxPos = 0
	}
	if permissionPaletteScroll < 0 {
		permissionPaletteScroll = 0
	}
	if permissionPaletteScroll > maxPos {
		permissionPaletteScroll = maxPos
	}
	si := SCROLLINFO{CbSize: uint32(unsafe.Sizeof(SCROLLINFO{})), FMask: SIF_RANGE | SIF_PAGE | SIF_POS, NMin: 0, NMax: int32(maxInt(permissionPaletteCount()-1, 0)), NPage: uint32(rowsVisible), NPos: int32(permissionPaletteScroll)}
	pSetScrollInfo.Call(uintptr(hwnd), SB_VERT, uintptr(unsafe.Pointer(&si)), 0)
}

func movementPaletteBehavior(code string) string {
	spec := movementBehaviorForCode(code)
	switch spec.Kind {
	case "transition":
		return "Cambio quota / scale / raccordi"
	case "blocked":
		return "Bloccato a tutte le quote"
	case "surf":
		return "Acqua — richiede Surf"
	case "surf_blocked":
		return "Ostacolo durante Surf"
	case "multi_level":
		return "Ponte / passaggio multi-livello"
	case "multi_level_blocked":
		return "Multi-livello bloccato"
	case "elevation":
		if spec.Code == "C" {
			return "Camminabile standard — quota 2"
		}
		return fmt.Sprintf("Camminabile — quota %d", spec.Elevation)
	case "elevation_blocked":
		return fmt.Sprintf("Bloccato — quota %d", spec.Elevation)
	case "elevation_special":
		return fmt.Sprintf("Collisione speciale — quota %d", spec.Elevation)
	case "reserved":
		return "Codice speciale/riservato"
	default:
		return spec.Description
	}
}

func terrainPaletteBehavior(tag int) string {
	spec := terrainTagBehavior(tag)
	if spec.FieldMove != "" {
		switch tag {
		case 18:
			return "ROCKCLIMB ↑ — parete scalabile"
		case 19:
			return "ROCKCLIMB ↓ — parete scalabile"
		case 20:
			return "WHIRLPOOL — vortice durante Surf"
		case 21:
			return "RUMPABREAKING — ostacolo fragile"
		case 22:
			return "RAPIDGROWTH — crescita/rampicante"
		case 23:
			return "ULTRATIMERISK — varco temporale"
		case 24:
			return "BREZZOAREA — zona vento"
		case 25:
			return "THERMOMOTION — terreno instabile"
		case 26:
			return "TECORRUPTION — sistema/glitch"
		default:
			return spec.FieldMove + " — " + spec.Kind
		}
	}
	if tag == 0 {
		return "Nessun Terrain Tag"
	}
	if tag <= 7 {
		return fmt.Sprintf("Terrain Tag RMXP %d", tag)
	}
	return fmt.Sprintf("Terrain Tag esteso/plugin %d", tag)
}

func permissionPalettePaint(hwnd syscall.Handle) {
	var ps PAINTSTRUCT
	hdc, _, _ := pBeginPaint.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&ps)))
	if hdc == 0 {
		return
	}
	defer pEndPaint.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&ps)))
	var r RECT
	pGetClientRect.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&r)))
	bg := createSolidBrush(rgb(245, 245, 245))
	fillWithBrush(syscall.Handle(hdc), r, bg)
	deleteGDIObject(bg)
	count := permissionPaletteCount()
	rows := permissionPaletteRowsVisible(hwnd) + 1
	end := permissionPaletteScroll + rows
	if end > count {
		end = count
	}
	codes := movementPermissionCodes()
	tags := terrainTagCodes()
	for i := permissionPaletteScroll; i < end; i++ {
		y := int32((i - permissionPaletteScroll) * 30)
		var codeLabel, behavior string
		var color uintptr
		selected := false
		if permissionEditorMode == permissionModeTerrain {
			codeLabel = strconv.Itoa(tags[i])
			behavior = terrainPaletteBehavior(tags[i])
			color = terrainTagColor(tags[i])
			selected = tags[i] == selectedTerrainTag
		} else {
			codeLabel = codes[i]
			behavior = movementPaletteBehavior(codeLabel)
			color = advanceMapCodeColor(codeLabel)
			selected = codeLabel == selectedPerm
		}

		rowRect := RECT{Left: 2, Top: y + 2, Right: r.Right - 3, Bottom: y + 29}
		codeRight := int32(48)
		if codeRight > rowRect.Right-4 {
			codeRight = rowRect.Right - 4
		}
		codeRect := RECT{Left: rowRect.Left, Top: rowRect.Top, Right: codeRight, Bottom: rowRect.Bottom}
		descRect := RECT{Left: codeRight, Top: rowRect.Top, Right: rowRect.Right, Bottom: rowRect.Bottom}

		codeBrush := createSolidBrush(color)
		fillWithBrush(syscall.Handle(hdc), codeRect, codeBrush)
		deleteGDIObject(codeBrush)
		descBrush := createSolidBrush(rgb(250, 250, 250))
		fillWithBrush(syscall.Handle(hdc), descRect, descBrush)
		deleteGDIObject(descBrush)

		codeTC := permissionTextColor(color)
		text(syscall.Handle(hdc), codeRect.Left+6, y+7, codeLabel, codeTC)
		text(syscall.Handle(hdc), descRect.Left+7, y+7, behavior, rgb(25, 25, 25))

		if selected {
			pen, _, _ := pCreatePen.Call(PS_SOLID, 2, rgb(35, 95, 210))
			old, _, _ := pSelectObject.Call(hdc, pen)
			drawOutlineRect(hdc, rowRect.Left, rowRect.Top, rowRect.Right, rowRect.Bottom)
			if old != 0 {
				pSelectObject.Call(hdc, old)
			}
			if pen != 0 {
				pDeleteObject.Call(pen)
			}
		}
	}
}

func movementBehaviorDisplay(code string) string {
	spec := movementBehaviorForCode(code)
	return fmt.Sprintf("%s — %s", spec.Code, spec.Description)
}

func terrainBehaviorDisplay(tag int) string {
	spec := terrainTagBehavior(tag)
	name := fmt.Sprintf("Terrain Tag %d", tag)
	if spec.FieldMove != "" {
		name = fmt.Sprintf("%d — %s", tag, spec.FieldMove)
	}
	behavior := spec.Description
	if spec.BlocksWithoutMove && spec.FieldMove != "" {
		behavior += " | Senza " + spec.FieldMove + " il passaggio resta bloccato."
	}
	return name + "\r\n" + behavior
}

func permissionBehaviorTextForIndex(idx int) string {
	if idx < 0 || idx >= permissionPaletteCount() {
		return ""
	}
	if permissionEditorMode == permissionModeTerrain {
		return terrainBehaviorDisplay(terrainTagCodes()[idx])
	}
	return movementBehaviorDisplay(movementPermissionCodes()[idx])
}

func showPermissionBehaviorHint(hwnd syscall.Handle, idx int) {
	if hwndPermBehaviorHint == 0 || idx < 0 || idx >= permissionPaletteCount() {
		return
	}
	label := permissionBehaviorTextForIndex(idx)
	if label == "" {
		return
	}
	setText(hwndPermBehaviorHint, label)

	var wr RECT
	if r, _, _ := pGetWindowRect.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&wr))); r == 0 {
		return
	}
	const hintW int32 = 390
	const hintH int32 = 58
	row := idx - permissionPaletteScroll
	y := wr.Top + int32(row*30)
	x := wr.Left - hintW - 6
	if x < 4 {
		x = wr.Right + 6
	}
	pMoveWindow.Call(uintptr(hwndPermBehaviorHint), uintptr(x), uintptr(y), uintptr(hintW), uintptr(hintH), 1)
	pShowWindow.Call(uintptr(hwndPermBehaviorHint), SW_SHOW)
}

func hidePermissionBehaviorHint() {
	if hwndPermBehaviorHint != 0 {
		pShowWindow.Call(uintptr(hwndPermBehaviorHint), SW_HIDE)
	}
	permissionHintTracked = false
}

func permissionPaletteHover(hwnd syscall.Handle, l uintptr) {
	y := int(int16(hiword(l)))
	if y < 0 {
		hidePermissionBehaviorHint()
		return
	}
	idx := permissionPaletteScroll + y/30
	if idx < 0 || idx >= permissionPaletteCount() {
		hidePermissionBehaviorHint()
		return
	}
	showPermissionBehaviorHint(hwnd, idx)
	if !permissionHintTracked {
		tme := TRACKMOUSEEVENT{
			CbSize:    uint32(unsafe.Sizeof(TRACKMOUSEEVENT{})),
			DwFlags:   TME_LEAVE,
			HwndTrack: hwnd,
		}
		pTrackMouseEvent.Call(uintptr(unsafe.Pointer(&tme)))
		permissionHintTracked = true
	}
	if permissionEditorMode == permissionModeTerrain {
		tag := terrainTagCodes()[idx]
		setText(hwndStatus, terrainBehaviorDisplay(tag))
	} else {
		setText(hwndStatus, "Movimento "+movementBehaviorDisplay(movementPermissionCodes()[idx]))
	}
}

func permissionPaletteClick(hwnd syscall.Handle, l uintptr) {
	y := int(int16(hiword(l)))
	if y < 0 {
		return
	}
	idx := permissionPaletteScroll + y/30
	if idx < 0 || idx >= permissionPaletteCount() {
		return
	}
	if permissionEditorMode == permissionModeTerrain {
		selectTerrainTag(terrainTagCodes()[idx])
	} else {
		selectMovementPermission(movementPermissionCodes()[idx])
	}
	showPermissionBehaviorHint(hwnd, idx)
}

func permissionPaletteScrollBy(hwnd syscall.Handle, w uintptr) {
	rows := permissionPaletteRowsVisible(hwnd)
	switch int(loword(w)) {
	case SB_LINEUP:
		permissionPaletteScroll--
	case SB_LINEDOWN:
		permissionPaletteScroll++
	case SB_PAGEUP:
		permissionPaletteScroll -= rows
	case SB_PAGEDOWN:
		permissionPaletteScroll += rows
	case SB_THUMBTRACK, SB_THUMBPOSITION:
		var si SCROLLINFO
		si.CbSize = uint32(unsafe.Sizeof(SCROLLINFO{}))
		si.FMask = SIF_TRACKPOS
		pGetScrollInfo.Call(uintptr(hwnd), SB_VERT, uintptr(unsafe.Pointer(&si)))
		permissionPaletteScroll = int(si.NTrackPos)
	}
	updatePermissionPaletteScroll(hwnd)
	invalidate(hwnd)
}
