//go:build windows

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

const (
	idHeaderSave            = 1750
	idHeaderFeatureSave     = 1751
	idFeatureChapters       = 1770
	idFeatureMainMissions   = 1771
	idFeatureSideQuests     = 1772
	idFeatureRequests       = 1773
	idFeatureSOS            = 1774
	idFeatureInvestigations = 1775
	idFeatureMultiRegions   = 1776
	idRegionAdd             = 1777
	idRegionList            = 1778
	idRegionAssign          = 1779
	idRegionFolderList      = 1784
	idRegionUnassign        = 1786

	bsAutoCheckbox = 0x0003
	bmGetCheck     = 0x00F0
)

var (
	headerDirty               bool
	headerRefreshing          bool
	projectFeaturesDirty      bool
	projectFeaturesRefreshing bool
)

type ProjectFeaturesDoc struct {
	Version        int  `json:"version"`
	Chapters       bool `json:"chapters"`
	MainMissions   bool `json:"main_missions"`
	SideQuests     bool `json:"side_quests"`
	Requests       bool `json:"requests"`
	SOS            bool `json:"sos_events"`
	Investigations bool `json:"investigations_secrets"`
	MultiRegions   bool `json:"multi_regions"`
}

var (
	hwndHeaderFeatures        syscall.Handle
	headerPanelOldWndProc     uintptr
	headerFeaturesOldWndProc  uintptr
	hwndFeatureChapters       syscall.Handle
	hwndFeatureMainMissions   syscall.Handle
	hwndFeatureSideQuests     syscall.Handle
	hwndFeatureRequests       syscall.Handle
	hwndFeatureSOS            syscall.Handle
	hwndFeatureInvestigations syscall.Handle
	hwndFeatureMultiRegions   syscall.Handle
	hwndHeaderFeatureSave     syscall.Handle
	hwndRegionAssign          syscall.Handle
	hwndRegionUnassign        syscall.Handle
	hwndRegionFolderList      syscall.Handle
)

// I controlli della Vista Header sono contenuti in GROUPBOX reali. In Win32
// WM_COMMAND viene inviato al parent immediato del controllo; un BUTTON con
// BS_GROUPBOX non inoltra automaticamente il messaggio alla finestra principale.
// Senza questo bridge i pulsanti/combo visualmente funzionano, ma i comandi
// (Aggiungi regione, Sposta mappa, Salva struttura, cambio regione...) non
// raggiungono wndProc. Inoltriamo soltanto i messaggi di comando al main window,
// lasciando al proc originale del GROUPBOX tutto il rendering/comportamento.
var headerContainerWndProc = syscall.NewCallback(func(hwnd syscall.Handle, msg uint32, w, l uintptr) uintptr {
	if msg == WM_COMMAND && hwndMain != 0 {
		pSendMessageW.Call(uintptr(hwndMain), uintptr(msg), w, l)
		return 0
	}
	oldProc := headerPanelOldWndProc
	if hwnd == hwndHeaderFeatures {
		oldProc = headerFeaturesOldWndProc
	}
	if oldProc != 0 {
		ret, _, _ := pCallWindowProcW.Call(oldProc, uintptr(hwnd), uintptr(msg), w, l)
		return ret
	}
	ret, _, _ := pDefWindowProcW.Call(uintptr(hwnd), uintptr(msg), w, l)
	return ret
})

func installHeaderCommandForwarder(hwnd syscall.Handle, oldProc *uintptr) {
	if hwnd == 0 || oldProc == nil || *oldProc != 0 {
		return
	}
	ret, _, _ := pSetWindowLongPtrW.Call(uintptr(hwnd), ^uintptr(3), headerContainerWndProc)
	*oldProc = ret
}

func createHeaderEditor(hInst syscall.Handle) {
	hwndHeaderPanel = createWindow("BUTTON", "", WS_CHILD|BS_GROUPBOX, 0, 0, 0, 0, hwndMain, 1740, hInst)
	installHeaderCommandForwarder(hwndHeaderPanel, &headerPanelOldWndProc)
	createWindow("STATIC", "Nome mappa", WS_CHILD|WS_VISIBLE, 16, 30, 130, 18, hwndHeaderPanel, 1760, hInst)
	hwndHeaderName = createWindow("EDIT", "", WS_CHILD|WS_VISIBLE|WS_BORDER|WS_TABSTOP, 150, 27, 300, 22, hwndHeaderPanel, 1741, hInst)
	createWindow("STATIC", "Musica", WS_CHILD|WS_VISIBLE, 16, 62, 130, 18, hwndHeaderPanel, 1761, hInst)
	hwndHeaderMusic = createWindow("EDIT", "", WS_CHILD|WS_VISIBLE|WS_BORDER|WS_TABSTOP, 150, 59, 300, 22, hwndHeaderPanel, 1742, hInst)
	createWindow("STATIC", "Tipo mappa", WS_CHILD|WS_VISIBLE, 16, 94, 130, 18, hwndHeaderPanel, 1762, hInst)
	hwndHeaderType = createWindow("COMBOBOX", "", WS_CHILD|WS_VISIBLE|WS_TABSTOP|CBS_DROPDOWNLIST, 150, 91, 180, 160, hwndHeaderPanel, 1743, hInst)
	for _, s := range []string{"Non specificato", "Esterno", "Interno", "Grotta", "Subacqueo", "Altro"} {
		comboAdd(hwndHeaderType, s)
	}
	pSendMessageW.Call(uintptr(hwndHeaderType), CB_SETCURSEL, 0, 0)
	createWindow("STATIC", "Meteo", WS_CHILD|WS_VISIBLE, 16, 126, 130, 18, hwndHeaderPanel, 1763, hInst)
	hwndHeaderWeather = createWindow("COMBOBOX", "", WS_CHILD|WS_VISIBLE|WS_TABSTOP|CBS_DROPDOWNLIST, 150, 123, 180, 160, hwndHeaderPanel, 1744, hInst)
	for _, s := range []string{"Non specificato", "Nessuno", "Pioggia", "Tempesta", "Neve", "Nebbia"} {
		comboAdd(hwndHeaderWeather, s)
	}
	pSendMessageW.Call(uintptr(hwndHeaderWeather), CB_SETCURSEL, 0, 0)
	createWindow("STATIC", "Battle background", WS_CHILD|WS_VISIBLE, 16, 158, 130, 18, hwndHeaderPanel, 1764, hInst)
	hwndHeaderBattleBG = createWindow("EDIT", "", WS_CHILD|WS_VISIBLE|WS_BORDER|WS_TABSTOP, 150, 155, 300, 22, hwndHeaderPanel, 1745, hInst)
	createWindow("STATIC", "Regione", WS_CHILD|WS_VISIBLE, 16, 190, 130, 18, hwndHeaderPanel, 1765, hInst)
	hwndHeaderRegion = createWindow("EDIT", "", WS_CHILD|WS_VISIBLE|WS_BORDER|WS_TABSTOP, 150, 187, 180, 22, hwndHeaderPanel, 1746, hInst)
	createWindow("STATIC", "Proprietà / note", WS_CHILD|WS_VISIBLE, 16, 228, 130, 18, hwndHeaderPanel, 1766, hInst)
	hwndHeaderNotes = createWindow("EDIT", "", WS_CHILD|WS_VISIBLE|WS_BORDER|WS_TABSTOP|ES_MULTILINE|ES_AUTOVSCROLL|ES_WANTRETURN, 150, 225, 420, 112, hwndHeaderPanel, 1747, hInst)

	// Funzioni narrative e struttura territoriale a livello di progetto.
	// Le regioni sono separate dai metadati della singola mappa: quando
	// abilitate, ogni regione possiede cartelle Maps/Scripts/Data dedicate.
	hwndHeaderFeatures = createWindow("BUTTON", "Struttura gioco", WS_CHILD|WS_VISIBLE|BS_GROUPBOX, 16, 348, 554, 222, hwndHeaderPanel, 1767, hInst)
	installHeaderCommandForwarder(hwndHeaderFeatures, &headerFeaturesOldWndProc)
	hwndFeatureChapters = createWindow("BUTTON", "Capitoli", WS_CHILD|WS_VISIBLE|WS_TABSTOP|bsAutoCheckbox, 14, 22, 150, 20, hwndHeaderFeatures, idFeatureChapters, hInst)
	hwndFeatureMainMissions = createWindow("BUTTON", "Missioni principali", WS_CHILD|WS_VISIBLE|WS_TABSTOP|bsAutoCheckbox, 178, 22, 165, 20, hwndHeaderFeatures, idFeatureMainMissions, hInst)
	hwndFeatureSideQuests = createWindow("BUTTON", "Missioni secondarie / Quest", WS_CHILD|WS_VISIBLE|WS_TABSTOP|bsAutoCheckbox, 355, 22, 190, 20, hwndHeaderFeatures, idFeatureSideQuests, hInst)
	hwndFeatureRequests = createWindow("BUTTON", "Richieste / Incarichi", WS_CHILD|WS_VISIBLE|WS_TABSTOP|bsAutoCheckbox, 14, 50, 150, 20, hwndHeaderFeatures, idFeatureRequests, hInst)
	hwndFeatureSOS = createWindow("BUTTON", "Eventi / SOS", WS_CHILD|WS_VISIBLE|WS_TABSTOP|bsAutoCheckbox, 178, 50, 165, 20, hwndHeaderFeatures, idFeatureSOS, hInst)
	hwndFeatureInvestigations = createWindow("BUTTON", "Indagini / Segreti", WS_CHILD|WS_VISIBLE|WS_TABSTOP|bsAutoCheckbox, 355, 50, 190, 20, hwndHeaderFeatures, idFeatureInvestigations, hInst)
	hwndFeatureMultiRegions = createWindow("BUTTON", "Più regioni (cartelle separate)", WS_CHILD|WS_VISIBLE|WS_TABSTOP|bsAutoCheckbox, 14, 78, 220, 20, hwndHeaderFeatures, idFeatureMultiRegions, hInst)
	createWindow("STATIC", "Regione attiva", WS_CHILD|WS_VISIBLE, 245, 80, 90, 18, hwndHeaderFeatures, 1781, hInst)
	hwndRegionList = createWindow("COMBOBOX", "", WS_CHILD|WS_VISIBLE|WS_TABSTOP|CBS_DROPDOWNLIST, 337, 76, 207, 180, hwndHeaderFeatures, idRegionList, hInst)
	createWindow("STATIC", "Cartella destinazione", WS_CHILD|WS_VISIBLE, 14, 108, 118, 18, hwndHeaderFeatures, 1785, hInst)
	hwndRegionFolderList = createWindow("COMBOBOX", "", WS_CHILD|WS_VISIBLE|WS_TABSTOP|CBS_DROPDOWNLIST, 136, 104, 300, 220, hwndHeaderFeatures, idRegionFolderList, hInst)
	hwndRegionAssign = createWindow("BUTTON", "Sposta mappa", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 444, 104, 101, 24, hwndHeaderFeatures, idRegionAssign, hInst)
	createWindow("STATIC", "Mappa corrente", WS_CHILD|WS_VISIBLE, 14, 138, 118, 18, hwndHeaderFeatures, 1787, hInst)
	hwndRegionUnassign = createWindow("BUTTON", "Sposta a NON ASSEGNATA", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 136, 134, 190, 24, hwndHeaderFeatures, idRegionUnassign, hInst)
	createWindow("STATIC", "Nuova regione", WS_CHILD|WS_VISIBLE, 14, 168, 88, 18, hwndHeaderFeatures, 1782, hInst)
	hwndRegionName = createWindow("EDIT", "", WS_CHILD|WS_VISIBLE|WS_BORDER|WS_TABSTOP, 104, 165, 226, 22, hwndHeaderFeatures, 1783, hInst)
	hwndRegionAdd = createWindow("BUTTON", "Aggiungi regione", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 337, 164, 103, 24, hwndHeaderFeatures, idRegionAdd, hInst)
	hwndHeaderFeatureSave = createWindow("BUTTON", "Salva struttura gioco", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 14, 194, 155, 22, hwndHeaderFeatures, idHeaderFeatureSave, hInst)

	// Teniamo il salvataggio header separato dalla struttura del progetto e
	// libero in alto: in questo modo la sezione regioni può crescere senza
	// uscire dall'area minima della Vista Header.
	hwndHeaderSave = createWindow("BUTTON", "Salva header", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 460, 27, 110, 24, hwndHeaderPanel, idHeaderSave, hInst)
}

func headerHandles() []syscall.Handle {
	return []syscall.Handle{
		hwndHeaderPanel, hwndHeaderName, hwndHeaderMusic, hwndHeaderType, hwndHeaderWeather,
		hwndHeaderBattleBG, hwndHeaderRegion, hwndHeaderNotes, hwndHeaderFeatures,
		hwndFeatureChapters, hwndFeatureMainMissions, hwndFeatureSideQuests, hwndFeatureRequests,
		hwndFeatureSOS, hwndFeatureInvestigations, hwndFeatureMultiRegions, hwndRegionList,
		hwndRegionFolderList, hwndRegionName, hwndRegionAdd, hwndRegionAssign, hwndRegionUnassign, hwndHeaderFeatureSave, hwndHeaderSave,
	}
}

func showHeaderEditor(show bool) {
	for _, h := range headerHandles() {
		showControl(h, show)
	}
	if show {
		loadProjectFeatures()
		loadRegionRegistry()
		loadHeaderFromCurrentMap()
	}
}

func setChecked(h syscall.Handle, checked bool) {
	if h == 0 {
		return
	}
	state := uintptr(BST_UNCHECKED)
	if checked {
		state = uintptr(BST_CHECKED)
	}
	pSendMessageW.Call(uintptr(h), BM_SETCHECK, state, 0)
}

func isChecked(h syscall.Handle) bool {
	if h == 0 {
		return false
	}
	state, _, _ := pSendMessageW.Call(uintptr(h), bmGetCheck, 0, 0)
	return state == BST_CHECKED
}

func projectFeaturesPath() string {
	d := sideDir()
	if d == "" {
		return ""
	}
	return filepath.Join(d, "project_features.json")
}

func currentProjectFeatures() ProjectFeaturesDoc {
	return ProjectFeaturesDoc{
		Version:        1,
		Chapters:       isChecked(hwndFeatureChapters),
		MainMissions:   isChecked(hwndFeatureMainMissions),
		SideQuests:     isChecked(hwndFeatureSideQuests),
		Requests:       isChecked(hwndFeatureRequests),
		SOS:            isChecked(hwndFeatureSOS),
		Investigations: isChecked(hwndFeatureInvestigations),
		MultiRegions:   isChecked(hwndFeatureMultiRegions),
	}
}

func applyProjectFeatures(doc ProjectFeaturesDoc) {
	setChecked(hwndFeatureChapters, doc.Chapters)
	setChecked(hwndFeatureMainMissions, doc.MainMissions)
	setChecked(hwndFeatureSideQuests, doc.SideQuests)
	setChecked(hwndFeatureRequests, doc.Requests)
	setChecked(hwndFeatureSOS, doc.SOS)
	setChecked(hwndFeatureInvestigations, doc.Investigations)
	setChecked(hwndFeatureMultiRegions, doc.MultiRegions)
}

func loadProjectFeatures() {
	projectFeaturesRefreshing = true
	defer func() {
		projectFeaturesRefreshing = false
		projectFeaturesDirty = false
	}()
	applyProjectFeatures(ProjectFeaturesDoc{Version: 1})
	p := projectFeaturesPath()
	if p == "" {
		return
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return
	}
	var doc ProjectFeaturesDoc
	if json.Unmarshal(b, &doc) != nil {
		return
	}
	applyProjectFeatures(doc)
}

func saveProjectFeatures() error {
	p := projectFeaturesPath()
	if p == "" {
		return fmt.Errorf("nessun progetto aperto")
	}
	doc := currentProjectFeatures()
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		return err
	}
	if err := writeJSONAtomic(p, doc); err != nil {
		return err
	}
	if currentProject != "" {
		projectRegions.Enabled = doc.MultiRegions
		if len(projectRegions.Regions) > 0 || exists(regionRegistryPath()) {
			if err := saveRegionRegistry(); err != nil {
				return err
			}
		}
	}
	projectFeaturesDirty = false
	return nil
}

func headerRawField(m map[string]json.RawMessage, keys ...string) (json.RawMessage, bool) {
	for _, key := range keys {
		for k, v := range m {
			if strings.EqualFold(strings.TrimSpace(k), key) {
				return v, true
			}
		}
	}
	return nil, false
}

func headerRawObject(raw json.RawMessage) map[string]json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	var out map[string]json.RawMessage
	if json.Unmarshal(raw, &out) != nil {
		return nil
	}
	return out
}

func headerSources() []map[string]json.RawMessage {
	if currentMapDoc == nil || currentMapDoc.Raw == nil {
		return nil
	}
	out := []map[string]json.RawMessage{currentMapDoc.Raw}
	for _, key := range []string{"metadata", "map_metadata", "mapMetadata", "header", "properties"} {
		if raw, ok := headerRawField(currentMapDoc.Raw, key); ok {
			if obj := headerRawObject(raw); obj != nil {
				out = append(out, obj)
			}
		}
	}
	return out
}

func headerValue(keys ...string) string {
	for _, source := range headerSources() {
		raw, ok := headerRawField(source, keys...)
		if !ok {
			continue
		}
		var s string
		if json.Unmarshal(raw, &s) == nil {
			return strings.TrimSpace(s)
		}
		var n json.Number
		if json.Unmarshal(raw, &n) == nil && n.String() != "" {
			return n.String()
		}
		var i int
		if json.Unmarshal(raw, &i) == nil {
			return strconv.Itoa(i)
		}
	}
	return ""
}

func headerBool(keys ...string) (bool, bool) {
	for _, source := range headerSources() {
		raw, ok := headerRawField(source, keys...)
		if !ok {
			continue
		}
		var b bool
		if json.Unmarshal(raw, &b) == nil {
			return b, true
		}
	}
	return false, false
}

func headerAudioName() string {
	for _, source := range headerSources() {
		if s := headerStringFromKeys(source, "music", "music_name", "bgm_name", "background_music"); s != "" {
			return s
		}
		raw, ok := headerRawField(source, "bgm", "background_bgm", "audio_bgm")
		if !ok {
			continue
		}
		var direct string
		if json.Unmarshal(raw, &direct) == nil {
			return strings.TrimSpace(direct)
		}
		if obj := headerRawObject(raw); obj != nil {
			if s := headerStringFromKeys(obj, "name", "filename", "file"); s != "" {
				return s
			}
		}
	}
	return ""
}

func headerStringFromKeys(source map[string]json.RawMessage, keys ...string) string {
	raw, ok := headerRawField(source, keys...)
	if !ok {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return strings.TrimSpace(s)
	}
	var n int
	if json.Unmarshal(raw, &n) == nil {
		return strconv.Itoa(n)
	}
	return ""
}

func headerMapType() string {
	value := strings.ToLower(strings.TrimSpace(headerValue("map_type", "map_kind", "environment", "environment_type")))
	if value == "" {
		if outdoor, ok := headerBool("outdoor", "outdoor_map", "is_outdoor"); ok {
			if outdoor {
				return "Esterno"
			}
			return "Interno"
		}
		return "Non specificato"
	}
	switch value {
	case "esterno", "outdoor", "outside", "field", "overworld":
		return "Esterno"
	case "interno", "indoor", "inside", "building":
		return "Interno"
	case "grotta", "cave", "cavern":
		return "Grotta"
	case "subacqueo", "underwater", "under_water":
		return "Subacqueo"
	default:
		return "Altro"
	}
}

func headerWeatherName() string {
	value := strings.ToLower(strings.TrimSpace(headerValue("weather", "weather_type", "weatherType", "climate")))
	if value == "" {
		return "Non specificato"
	}
	switch value {
	case "none", "nessuno", "clear", "sun", "sunny", "0":
		return "Nessuno"
	case "rain", "rainy", "pioggia", "1":
		return "Pioggia"
	case "storm", "thunderstorm", "tempesta", "2":
		return "Tempesta"
	case "snow", "snowy", "neve", "3":
		return "Neve"
	case "fog", "foggy", "mist", "nebbia", "4":
		return "Nebbia"
	default:
		return "Non specificato"
	}
}

func setHeaderCombo(h syscall.Handle, options []string, value string) {
	wanted := strings.TrimSpace(value)
	for i, option := range options {
		if strings.EqualFold(option, wanted) {
			pSendMessageW.Call(uintptr(h), CB_SETCURSEL, uintptr(i), 0)
			return
		}
	}
	pSendMessageW.Call(uintptr(h), CB_SETCURSEL, 0, 0)
}

func clearHeaderFields() {
	setText(hwndHeaderPanel, "Dati mappa / Header")
	setText(hwndHeaderName, "")
	setText(hwndHeaderMusic, "")
	setHeaderCombo(hwndHeaderType, []string{"Non specificato", "Esterno", "Interno", "Grotta", "Subacqueo", "Altro"}, "Non specificato")
	setHeaderCombo(hwndHeaderWeather, []string{"Non specificato", "Nessuno", "Pioggia", "Tempesta", "Neve", "Nebbia"}, "Non specificato")
	setText(hwndHeaderBattleBG, "")
	setText(hwndHeaderRegion, "")
	setText(hwndHeaderNotes, "")
}

func loadHeaderFromCurrentMap() {
	if hwndHeaderPanel == 0 {
		return
	}
	headerRefreshing = true
	defer func() {
		headerRefreshing = false
		headerDirty = false
	}()
	if currentMap == nil || currentMapDoc == nil {
		clearHeaderFields()
		return
	}

	setText(hwndHeaderPanel, fmt.Sprintf("Dati mappa / Header — Map %03d | %dx%d | Tileset %d", currentMap.ID, currentMapW, currentMapH, currentMapDoc.TilesetID))
	setText(hwndHeaderName, currentMap.Name)
	setText(hwndHeaderMusic, headerAudioName())
	setHeaderCombo(hwndHeaderType, []string{"Non specificato", "Esterno", "Interno", "Grotta", "Subacqueo", "Altro"}, headerMapType())
	setHeaderCombo(hwndHeaderWeather, []string{"Non specificato", "Nessuno", "Pioggia", "Tempesta", "Neve", "Nebbia"}, headerWeatherName())
	setText(hwndHeaderBattleBG, headerValue("battle_background", "battleback", "battle_bg", "battleback_name", "battle_background_name"))
	regionValue := headerValue("region", "region_id", "map_region", "regionId")
	if strings.TrimSpace(regionValue) == "" && currentMap.RegionID != "" {
		regionValue = regionNameByID(currentMap.RegionID)
		if regionValue == "" {
			regionValue = currentMap.RegionID
		}
	}
	setText(hwndHeaderRegion, regionValue)
	setText(hwndHeaderNotes, headerValue("notes", "note", "description", "map_notes"))
}

func headerRawJSON(value any) json.RawMessage {
	b, _ := json.Marshal(value)
	return json.RawMessage(b)
}

func headerSetTopLevel(key string, value any) {
	if currentMapDoc == nil || currentMapDoc.Raw == nil {
		return
	}
	// Preserve the original key casing when the converter already emitted an
	// equivalent field. Otherwise write the canonical PLM field name.
	for existing := range currentMapDoc.Raw {
		if strings.EqualFold(strings.TrimSpace(existing), key) {
			currentMapDoc.Raw[existing] = headerRawJSON(value)
			return
		}
	}
	currentMapDoc.Raw[key] = headerRawJSON(value)
}

func headerPatchExistingAliases(source map[string]json.RawMessage, keys []string, value any) bool {
	if source == nil {
		return false
	}
	changed := false
	for existing := range source {
		for _, key := range keys {
			if strings.EqualFold(strings.TrimSpace(existing), key) {
				source[existing] = headerRawJSON(value)
				changed = true
				break
			}
		}
	}
	return changed
}

func headerPatchNestedAliases(keys []string, value any) {
	if currentMapDoc == nil || currentMapDoc.Raw == nil {
		return
	}
	for _, containerKey := range []string{"metadata", "map_metadata", "mapMetadata", "header", "properties"} {
		for actualKey, raw := range currentMapDoc.Raw {
			if !strings.EqualFold(actualKey, containerKey) {
				continue
			}
			obj := headerRawObject(raw)
			if obj == nil {
				continue
			}
			if headerPatchExistingAliases(obj, keys, value) {
				currentMapDoc.Raw[actualKey] = headerRawJSON(obj)
			}
		}
	}
}

func headerPatchBGMName(name string) {
	if currentMapDoc == nil || currentMapDoc.Raw == nil {
		return
	}
	// Update direct aliases if they already exist.
	headerPatchExistingAliases(currentMapDoc.Raw, []string{"music", "music_name", "bgm_name", "background_music"}, name)
	headerPatchNestedAliases([]string{"music", "music_name", "bgm_name", "background_music"}, name)

	// Preserve volume/pitch/etc. when the converter emitted an RPG::AudioFile
	// style object instead of a direct string.
	patchObj := func(source map[string]json.RawMessage) bool {
		changed := false
		for key, raw := range source {
			lk := strings.ToLower(strings.TrimSpace(key))
			if lk != "bgm" && lk != "background_bgm" && lk != "audio_bgm" {
				continue
			}
			obj := headerRawObject(raw)
			if obj == nil {
				continue
			}
			patched := false
			for objKey := range obj {
				ol := strings.ToLower(strings.TrimSpace(objKey))
				if ol == "name" || ol == "filename" || ol == "file" {
					obj[objKey] = headerRawJSON(name)
					patched = true
				}
			}
			if !patched {
				obj["name"] = headerRawJSON(name)
			}
			source[key] = headerRawJSON(obj)
			changed = true
		}
		return changed
	}
	patchObj(currentMapDoc.Raw)
	for _, containerKey := range []string{"metadata", "map_metadata", "mapMetadata", "header", "properties"} {
		for actualKey, raw := range currentMapDoc.Raw {
			if !strings.EqualFold(actualKey, containerKey) {
				continue
			}
			obj := headerRawObject(raw)
			if obj != nil && patchObj(obj) {
				currentMapDoc.Raw[actualKey] = headerRawJSON(obj)
			}
		}
	}
	// Canonical field used by PLM Studio and the Python runtime bridge.
	headerSetTopLevel("music", name)
}

func canonicalHeaderMapType(label string) string {
	switch strings.ToLower(strings.TrimSpace(label)) {
	case "esterno":
		return "outdoor"
	case "interno":
		return "indoor"
	case "grotta":
		return "cave"
	case "subacqueo":
		return "underwater"
	case "altro":
		return "other"
	default:
		return "unspecified"
	}
}

func canonicalHeaderWeather(label string) string {
	switch strings.ToLower(strings.TrimSpace(label)) {
	case "nessuno":
		return "none"
	case "pioggia":
		return "rain"
	case "tempesta":
		return "storm"
	case "neve":
		return "snow"
	case "nebbia":
		return "fog"
	default:
		return ""
	}
}

func updateMapInfoNameValue(v any, mapID int, keyHint, newName string) bool {
	switch t := v.(type) {
	case map[string]any:
		id := vint(t, "id", "map_id", "mapId")
		if id == 0 && keyHint != "" {
			id, _ = strconv.Atoi(keyHint)
		}
		if id == mapID {
			for _, preferred := range []string{"name", "display_name", "displayName"} {
				for k := range t {
					if strings.EqualFold(k, preferred) {
						t[k] = newName
						return true
					}
				}
			}
			t["name"] = newName
			return true
		}
		for k, child := range t {
			if updateMapInfoNameValue(child, mapID, k, newName) {
				return true
			}
		}
	case []any:
		for i, child := range t {
			if updateMapInfoNameValue(child, mapID, strconv.Itoa(i), newName) {
				return true
			}
		}
	}
	return false
}

func writeJSONAtomic(path string, value any) error {
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		_ = f.Close()
		if !ok {
			_ = os.Remove(tmp)
		}
	}()
	if _, err = f.Write(b); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	r, _, callErr := pMoveFileExW.Call(
		uintptr(unsafe.Pointer(wstr(tmp))),
		uintptr(unsafe.Pointer(wstr(path))),
		MOVEFILE_REPLACE_EXISTING|MOVEFILE_WRITE_THROUGH,
	)
	if r == 0 {
		return fmt.Errorf("sostituzione atomica di %s fallita: %v", filepath.Base(path), callErr)
	}
	ok = true
	return nil
}

func updateMapInfosName(mapID int, newName string) error {
	if currentProject == "" || mapID <= 0 {
		return nil
	}
	for _, path := range mapInfoCandidates(currentProject) {
		b, err := os.ReadFile(path)
		if err != nil || len(b) == 0 {
			continue
		}
		dec := json.NewDecoder(strings.NewReader(string(b)))
		dec.UseNumber()
		var doc any
		if err := dec.Decode(&doc); err != nil {
			return fmt.Errorf("MapInfos non leggibile: %w", err)
		}
		if !updateMapInfoNameValue(doc, mapID, "", newName) {
			// MapInfos can legitimately omit maps authored directly in PML Studio.
			return nil
		}
		bak := path + ".bak"
		if _, err := os.Stat(bak); os.IsNotExist(err) {
			_ = os.WriteFile(bak, b, 0644)
		}
		if err := writeJSONAtomic(path, doc); err != nil {
			return err
		}
		return nil
	}
	return nil
}

func applyHeaderFieldsToCurrentDocument(name, music, mapType, weather, battleBG, region, notes string) {
	// Name is canonical at the map root because the physical MapXXX.json index
	// can use it even when MapInfos is absent.
	headerSetTopLevel("name", name)
	headerPatchBGMName(music)
	headerSetTopLevel("map_type", mapType)
	headerPatchNestedAliases([]string{"map_type", "map_kind", "environment", "environment_type"}, mapType)
	headerSetTopLevel("weather", weather)
	headerPatchNestedAliases([]string{"weather", "weather_type", "weatherType", "climate"}, weather)
	headerSetTopLevel("battle_background", battleBG)
	headerPatchNestedAliases([]string{"battle_background", "battleback", "battle_bg", "battleback_name", "battle_background_name"}, battleBG)
	headerSetTopLevel("region", region)
	headerPatchNestedAliases([]string{"region", "region_id", "map_region", "regionId"}, region)
	headerSetTopLevel("notes", notes)
	headerPatchNestedAliases([]string{"notes", "note", "description", "map_notes"}, notes)
}

func confirmHeaderChanges() bool {
	if !headerDirty || currentMap == nil || currentMapDoc == nil {
		return true
	}
	result := msgboxResult(
		"PML Studio - Header mappa",
		fmt.Sprintf("L'Header di Map%03d contiene modifiche non salvate.\r\n\r\nSì = Salva\r\nNo = Non salvare\r\nAnnulla = Resta sulla mappa", currentMap.ID),
		MB_YESNOCANCEL|MB_ICONINFORMATION,
	)
	switch result {
	case IDYES:
		if err := saveHeaderToCurrentMap(); err != nil {
			msgbox("PML Studio - Header mappa", "Impossibile salvare l'Header:\r\n\r\n"+err.Error(), MB_OK|MB_ICONERROR)
			return false
		}
		return true
	case IDNO:
		headerDirty = false
		return true
	default:
		return false
	}
}

func saveHeaderToCurrentMap() error {
	if currentMap == nil || currentMapDoc == nil || currentMapDoc.Raw == nil {
		return fmt.Errorf("seleziona prima una mappa")
	}
	if err := ensureProjectMapFilesystemSafe("Salvataggio Header"); err != nil {
		return err
	}

	name := strings.TrimSpace(getText(hwndHeaderName))
	if name == "" {
		return fmt.Errorf("il nome mappa non può essere vuoto")
	}
	music := strings.TrimSpace(getText(hwndHeaderMusic))
	mapType := canonicalHeaderMapType(getText(hwndHeaderType))
	weather := canonicalHeaderWeather(getText(hwndHeaderWeather))
	battleBG := strings.TrimSpace(getText(hwndHeaderBattleBG))
	region := strings.TrimSpace(getText(hwndHeaderRegion))
	notes := strings.TrimSpace(getText(hwndHeaderNotes))

	// Header saving is isolated from unsaved tile/layer edits. Work on a fresh
	// disk snapshot, then mirror the same header fields into the active document
	// so a later map save cannot restore stale header values.
	activeDoc := currentMapDoc
	fresh, err := loadMapDocument(activeDoc.Path)
	if err != nil {
		return fmt.Errorf("ricaricamento Map%03d prima del salvataggio header: %w", currentMap.ID, err)
	}
	currentMapDoc = fresh
	applyHeaderFieldsToCurrentDocument(name, music, mapType, weather, battleBG, region, notes)
	if err := saveMapDocument(fresh); err != nil {
		currentMapDoc = activeDoc
		return fmt.Errorf("salvataggio Map%03d fallito: %w", currentMap.ID, err)
	}
	currentMapDoc = activeDoc
	applyHeaderFieldsToCurrentDocument(name, music, mapType, weather, battleBG, region, notes)
	if err := updateMapInfosName(currentMap.ID, name); err != nil {
		return fmt.Errorf("mappa salvata, ma aggiornamento MapInfos fallito: %w", err)
	}

	// Keep the in-memory index and every tree/tab label synchronized immediately.
	currentMap.Name = name
	for i := range maps {
		if maps[i].ID == currentMap.ID {
			maps[i].Name = name
			currentMap = &maps[i]
			break
		}
	}
	headerDirty = false
	updateMapQuickInfo()
	updateMapEditorStatus()
	scheduleMapTreeRefresh()
	loadHeaderFromCurrentMap()
	mapLogf("[HEADER] Map%03d saved permanently name=%q music=%q type=%q weather=%q region=%q", currentMap.ID, name, music, mapType, weather, region)
	return nil
}

func headerViewPlaceholder() string {
	if currentMap == nil || currentMapDoc == nil {
		return "Header mappa\r\n\r\nSeleziona una mappa per caricarne i dati."
	}
	return fmt.Sprintf("Map %03d\r\n%s\r\n%dx%d tile\r\nTileset %d\r\n\r\nI campi della Vista Header vengono letti dalla mappa selezionata.", currentMap.ID, currentMap.Name, currentMapW, currentMapH, currentMapDoc.TilesetID)
}
func headerCanvasMessage() string { return "Editor header mappa" }
