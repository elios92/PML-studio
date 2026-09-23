//go:build windows

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

func decodeJSONAny(raw json.RawMessage) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	return v, nil
}

func anyMapValueCI(m map[string]any, names ...string) (any, bool) {
	for _, name := range names {
		for k, v := range m {
			normalized := strings.TrimSpace(strings.TrimPrefix(k, "@"))
			if strings.EqualFold(normalized, name) {
				return v, true
			}
		}
	}
	return nil, false
}

func anyString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case json.Number:
		return t.String()
	case float64:
		return strconv.Itoa(int(t))
	case int:
		return strconv.Itoa(t)
	}
	return ""
}

func anyInt(v any) int {
	switch t := v.(type) {
	case json.Number:
		n, _ := t.Int64()
		return int(n)
	case float64:
		return int(t)
	case int:
		return t
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(t))
		return n
	}
	return 0
}

func firstEventPage(m map[string]any) map[string]any {
	v, ok := anyMapValueCI(m, "pages")
	if !ok {
		return nil
	}
	switch pages := v.(type) {
	case []any:
		for _, p := range pages {
			if pm, ok := p.(map[string]any); ok {
				return pm
			}
		}
	case map[string]any:
		keys := make([]int, 0, len(pages))
		byKey := map[int]map[string]any{}
		for k, p := range pages {
			if pm, ok := p.(map[string]any); ok {
				n, _ := strconv.Atoi(k)
				keys = append(keys, n)
				byKey[n] = pm
			}
		}
		if len(keys) > 0 {
			best := keys[0]
			for _, k := range keys[1:] {
				if k < best {
					best = k
				}
			}
			return byKey[best]
		}
	}
	return nil
}

func nativeTriggerName(page map[string]any) string {
	if page == nil {
		return "Interazione"
	}
	v, ok := anyMapValueCI(page, "trigger")
	if !ok {
		return "Interazione"
	}
	if s, ok := v.(string); ok {
		ls := strings.ToLower(strings.TrimSpace(s))
		switch {
		case strings.Contains(ls, "autorun") || strings.Contains(ls, "automatic"):
			return "Autorun"
		case strings.Contains(ls, "parallel") || strings.Contains(ls, "paralle"):
			return "Parallelo"
		case strings.Contains(ls, "player") && strings.Contains(ls, "touch"):
			return "Contatto giocatore"
		case strings.Contains(ls, "event") && strings.Contains(ls, "touch"):
			return "Contatto evento"
		default:
			if strings.TrimSpace(s) != "" {
				return s
			}
		}
	}
	switch anyInt(v) {
	case 1:
		return "Contatto giocatore"
	case 2:
		return "Contatto evento"
	case 3:
		return "Autorun"
	case 4:
		return "Parallelo"
	default:
		return "Interazione"
	}
}

func nativeMovementName(page map[string]any) string {
	if page == nil {
		return "Fermo"
	}
	v, ok := anyMapValueCI(page, "move_type", "moveType", "movement_type")
	if !ok {
		return "Fermo"
	}
	if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
		return s
	}
	switch anyInt(v) {
	case 1:
		return "Casuale"
	case 2:
		return "Verso giocatore"
	case 3:
		return "Percorso definito"
	default:
		return "Fermo"
	}
}

func collectEventDialog(v any, out *[]string) {
	switch t := v.(type) {
	case map[string]any:
		code := 0
		if cv, ok := anyMapValueCI(t, "code"); ok {
			code = anyInt(cv)
		}
		if code == 101 || code == 401 {
			if pv, ok := anyMapValueCI(t, "parameters", "params"); ok {
				if arr, ok := pv.([]any); ok && len(arr) > 0 {
					s := strings.TrimSpace(anyString(arr[0]))
					if s != "" {
						*out = append(*out, s)
					}
				}
			}
		}
		for _, child := range t {
			collectEventDialog(child, out)
		}
	case []any:
		for _, child := range t {
			collectEventDialog(child, out)
		}
	}
}

func nativeEventGraphic(page map[string]any) (name string, hue, direction, pattern, tileID int) {
	if page == nil {
		return "", 0, 2, 0, 0
	}
	gv, ok := anyMapValueCI(page, "graphic")
	if !ok {
		return "", 0, 2, 0, 0
	}
	gm, ok := gv.(map[string]any)
	if !ok {
		return "", 0, 2, 0, 0
	}
	if v, ok := anyMapValueCI(gm, "character_name", "characterName", "character"); ok {
		name = strings.TrimSpace(anyString(v))
	}
	if v, ok := anyMapValueCI(gm, "character_hue", "characterHue", "hue"); ok {
		hue = anyInt(v)
	}
	if v, ok := anyMapValueCI(gm, "direction", "dir"); ok {
		direction = anyInt(v)
	}
	if direction == 0 {
		direction = 2
	}
	if v, ok := anyMapValueCI(gm, "pattern", "frame"); ok {
		pattern = anyInt(v)
	}
	if v, ok := anyMapValueCI(gm, "tile_id", "tileId", "tile"); ok {
		tileID = anyInt(v)
	}
	return
}

type nativeEventCommandClass struct {
	HasTransfer        bool
	HasScript          bool
	HasOtherMeaningful bool
}

func classifyNativeCommandNode(v any, c *nativeEventCommandClass) {
	if c == nil {
		return
	}
	switch t := v.(type) {
	case map[string]any:
		codePresent := false
		code := 0
		if cv, ok := anyMapValueCI(t, "code"); ok {
			codePresent = true
			code = anyInt(cv)
			switch code {
			case 201:
				c.HasTransfer = true
			case 355, 655:
				c.HasScript = true
			case 0, 108, 408:
				// Terminatori e commenti non trasformano un evento script-only
				// in un evento generico.
			default:
				if code != 0 {
					c.HasOtherMeaningful = true
				}
			}
		}
		if nv, ok := anyMapValueCI(t, "command", "command_name", "commandName"); ok {
			name := strings.ToLower(strings.TrimSpace(anyString(nv)))
			if name != "" {
				switch {
				case strings.Contains(name, "transfer") || strings.Contains(name, "trasfer") || strings.Contains(name, "warp"):
					c.HasTransfer = true
				case strings.Contains(name, "script") || strings.Contains(name, "python") || strings.Contains(name, "ruby"):
					c.HasScript = true
				case strings.Contains(name, "comment") || strings.Contains(name, "end"):
					// Informazioni non operative.
				default:
					c.HasOtherMeaningful = true
				}
			}
		} else if tv, ok := anyMapValueCI(t, "type"); ok {
			// Alcuni converter usano solo "type" per i comandi. Lo consideriamo
			// esclusivamente quando identifica esplicitamente Script/Warp, per non
			// confondere i normali campi strutturali delle pagine.
			name := strings.ToLower(strings.TrimSpace(anyString(tv)))
			switch {
			case strings.Contains(name, "transfer") || strings.Contains(name, "trasfer") || strings.Contains(name, "warp"):
				c.HasTransfer = true
			case strings.Contains(name, "script") || strings.Contains(name, "python") || strings.Contains(name, "ruby"):
				c.HasScript = true
			}
		} else if !codePresent {
			// Non considerare i normali campi della pagina (graphic, conditions,
			// ecc.) come comandi evento.
		}
		for k, child := range t {
			lk := strings.ToLower(strings.TrimSpace(k))
			if lk == "parameters" || lk == "params" {
				continue
			}
			classifyNativeCommandNode(child, c)
		}
	case []any:
		for _, child := range t {
			classifyNativeCommandNode(child, c)
		}
	}
}

func nativeEventCommandClassForEvent(m map[string]any) nativeEventCommandClass {
	var c nativeEventCommandClass
	if m == nil {
		return c
	}
	// Limitiamo la scansione alle pagine evento, evitando che altri campi
	// dell'evento vengano scambiati per comandi.
	if pages, ok := anyMapValueCI(m, "pages"); ok {
		classifyNativeCommandNode(pages, &c)
	}
	return c
}

var trainerBattleRefRE = regexp.MustCompile(`(?i)TrainerBattle\.start\s*\(\s*:(\w+)\s*,\s*["']([^"']+)["']\s*(?:,\s*(\d+))?`)

func findTrainerReference(v any) (trainerType, trainerName string, version int, ok bool) {
	switch t := v.(type) {
	case string:
		s := strings.TrimSpace(t)
		if strings.HasPrefix(strings.ToUpper(s), "PML_TRAINER_REF:") {
			body := strings.TrimSpace(s[len("PML_TRAINER_REF:"):])
			parts := strings.Split(body, "|")
			if len(parts) >= 2 {
				trainerType = strings.ToUpper(strings.TrimSpace(parts[0]))
				trainerName = strings.TrimSpace(parts[1])
				if len(parts) >= 3 {
					version, _ = strconv.Atoi(strings.TrimSpace(parts[2]))
				}
				return trainerType, trainerName, version, trainerType != "" && trainerName != ""
			}
		}
		if m := trainerBattleRefRE.FindStringSubmatch(s); len(m) >= 3 {
			trainerType = strings.ToUpper(strings.TrimSpace(m[1]))
			trainerName = strings.TrimSpace(m[2])
			if len(m) >= 4 && strings.TrimSpace(m[3]) != "" {
				version, _ = strconv.Atoi(strings.TrimSpace(m[3]))
			}
			return trainerType, trainerName, version, true
		}
	case []any:
		for _, child := range t {
			if tt, tn, tv, found := findTrainerReference(child); found {
				return tt, tn, tv, true
			}
		}
	case map[string]any:
		for _, child := range t {
			if tt, tn, tv, found := findTrainerReference(child); found {
				return tt, tn, tv, true
			}
		}
	}
	return "", "", 0, false
}

func nativeTrainerReference(m map[string]any) (string, string, int, bool) {
	if m == nil {
		return "", "", 0, false
	}
	if pages, ok := anyMapValueCI(m, "pages"); ok {
		return findTrainerReference(pages)
	}
	return "", "", 0, false
}

func inferEventKind(eventName, characterName string, tileID int) string {
	if tileID > 0 {
		return "Oggetto/Tileset"
	}
	if strings.TrimSpace(characterName) == "" {
		return "Evento logico"
	}
	l := strings.ToLower(eventName + " " + characterName)
	switch {
	case strings.Contains(l, "pokemon") || strings.Contains(l, "pokémon"):
		return "Pokémon"
	case strings.Contains(l, "door") || strings.Contains(l, "porta") || strings.Contains(l, "chest") || strings.Contains(l, "cassa") ||
		strings.Contains(l, "sign") || strings.Contains(l, "cartello") || strings.Contains(l, "rock") || strings.Contains(l, "roccia") ||
		strings.Contains(l, "tree") || strings.Contains(l, "albero") || strings.Contains(l, "item") || strings.Contains(l, "ball"):
		return "Oggetto"
	default:
		return "NPC"
	}
}

func editorEventFromNative(v any, key string) (EditorEvent, bool) {
	m, ok := v.(map[string]any)
	if !ok {
		return EditorEvent{}, false
	}
	id := 0
	if iv, ok := anyMapValueCI(m, "id", "event_id", "eventId"); ok {
		id = anyInt(iv)
	}
	if id <= 0 {
		id, _ = strconv.Atoi(strings.TrimSpace(key))
	}
	xv, xok := anyMapValueCI(m, "x")
	yv, yok := anyMapValueCI(m, "y")
	if id <= 0 || !xok || !yok {
		return EditorEvent{}, false
	}
	name := ""
	if nv, ok := anyMapValueCI(m, "name"); ok {
		name = anyString(nv)
	}
	if strings.TrimSpace(name) == "" {
		name = fmt.Sprintf("Evento %d", id)
	}
	page := firstEventPage(m)
	lines := []string{}
	if page != nil {
		collectEventDialog(page, &lines)
	}
	characterName, characterHue, characterDirection, characterPattern, graphicTileID := nativeEventGraphic(page)
	commandClass := nativeEventCommandClassForEvent(m)
	trainerType, trainerName, trainerVersion, isTrainer := nativeTrainerReference(m)
	eventKind := inferEventKind(name, characterName, graphicTileID)
	if isTrainer {
		eventKind = "Allenatore Pokémon"
	} else if commandClass.HasTransfer {
		eventKind = "Warp"
	} else if commandClass.HasScript && !commandClass.HasOtherMeaningful {
		eventKind = "Script"
	}
	raw, _ := json.Marshal(v)
	return EditorEvent{
		ID: id, Name: name, X: anyInt(xv), Y: anyInt(yv),
		Trigger: nativeTriggerName(page), Movement: nativeMovementName(page),
		Dialog:        strings.Join(lines, "\r\n"),
		CharacterName: characterName, CharacterHue: characterHue,
		CharacterDirection: characterDirection, CharacterPattern: characterPattern,
		GraphicTileID: graphicTileID, EventKind: eventKind,
		TrainerType: trainerType, TrainerName: trainerName, TrainerVersion: trainerVersion,
		Native: true, NativeKey: key, NativeRaw: raw,
	}, true
}

// loadEventsFromRealMap loads the converted RPG Maker/Pokémon Essentials event
// payload directly from MapXXX.json. The map is the canonical source; the old
// .plm event sidecar is only a fallback for maps authored by early 0.5 builds.
func loadEventsFromRealMap() (bool, error) {
	if currentMapDoc == nil || currentMapDoc.Raw == nil {
		return false, nil
	}
	raw, ok := currentMapDoc.Raw["events"]
	if !ok {
		return false, nil
	}
	v, err := decodeJSONAny(raw)
	if err != nil {
		return true, fmt.Errorf("eventi mappa non validi: %w", err)
	}
	loaded := make([]EditorEvent, 0)
	switch t := v.(type) {
	case map[string]any:
		for key, ev := range t {
			if e, ok := editorEventFromNative(ev, key); ok {
				loaded = append(loaded, e)
			}
		}
	case []any:
		for i, ev := range t {
			if ev == nil {
				continue
			}
			if e, ok := editorEventFromNative(ev, strconv.Itoa(i)); ok {
				loaded = append(loaded, e)
			}
		}
	case nil:
		// valid empty event collection
	default:
		return true, fmt.Errorf("formato eventi mappa non supportato")
	}
	// Stable ID order makes selection and duplicate behaviour deterministic.
	for i := 0; i < len(loaded); i++ {
		for j := i + 1; j < len(loaded); j++ {
			if loaded[j].ID < loaded[i].ID {
				loaded[i], loaded[j] = loaded[j], loaded[i]
			}
		}
	}
	events = loaded
	return true, nil
}

func patchNativeEventRaw(e EditorEvent) json.RawMessage {
	var m map[string]any
	if len(e.NativeRaw) > 0 {
		if v, err := decodeJSONAny(e.NativeRaw); err == nil {
			m, _ = v.(map[string]any)
		}
	}
	if m == nil {
		m = map[string]any{"_class": "RPG::Event", "pages": []any{}}
	}
	setCI := func(name string, value any) {
		for k := range m {
			normalized := strings.TrimSpace(strings.TrimPrefix(k, "@"))
			if strings.EqualFold(normalized, name) {
				m[k] = value
				return
			}
		}
		m[name] = value
	}
	setCI("id", e.ID)
	setCI("name", e.Name)
	setCI("x", e.X)
	setCI("y", e.Y)
	b, _ := json.Marshal(m)
	return b
}

func saveEventsIntoRealMap() error {
	if currentMapDoc == nil || currentMapDoc.Raw == nil {
		return nil
	}
	original := currentMapDoc.Raw["events"]
	originalValue, _ := decodeJSONAny(original)

	// Build the new event payload without mutating currentMapDoc yet. Event
	// editing has its own persistence boundary: saving an event must not
	// accidentally commit unsaved tile/layer changes just because they live in
	// the same in-memory MapDocument.
	var eventPayload json.RawMessage
	switch originalValue.(type) {
	case []any:
		maxID := 0
		for _, e := range events {
			if e.ID > maxID {
				maxID = e.ID
			}
		}
		arr := make([]json.RawMessage, maxID+1)
		for _, e := range events {
			if e.ID > 0 && e.ID < len(arr) {
				arr[e.ID] = patchNativeEventRaw(e)
			}
		}
		b, err := json.Marshal(arr)
		if err != nil {
			return err
		}
		eventPayload = b
	default:
		obj := map[string]json.RawMessage{}
		for _, e := range events {
			obj[strconv.Itoa(e.ID)] = patchNativeEventRaw(e)
		}
		b, err := json.Marshal(obj)
		if err != nil {
			return err
		}
		eventPayload = b
	}

	// Persist against a fresh disk snapshot and patch only the events field.
	// This preserves the user's decision to Save/Discard any unrelated map edit.
	fresh, err := loadMapDocument(currentMapDoc.Path)
	if err != nil {
		return fmt.Errorf("ricaricamento mappa prima del salvataggio eventi: %w", err)
	}
	fresh.Raw["events"] = eventPayload
	if err := saveMapDocument(fresh); err != nil {
		return err
	}

	// The event write succeeded. Mirror only that field into the active document;
	// its in-memory Table and every other pending editor field stay untouched.
	currentMapDoc.Raw["events"] = eventPayload
	if _, err := loadEventsFromRealMap(); err != nil {
		return fmt.Errorf("ricaricamento eventi salvati: %w", err)
	}
	if err := syncFlyPOIsForCurrentMap(); err != nil {
		return fmt.Errorf("sincronizzazione punti Volo/POI: %w", err)
	}
	if err := syncGlobalEventRegistryForCurrentMap(); err != nil {
		return fmt.Errorf("sincronizzazione eventi globali: %w", err)
	}
	return nil
}

// transferTarget is used both by the start-point detector and by future event
// command editors. RMXP command 201 is Transfer Player.
type transferTarget struct {
	MapID     int
	X, Y      int
	Direction int
}

func findTransferTargets(v any, out *[]transferTarget) {
	switch t := v.(type) {
	case map[string]any:
		code := 0
		if cv, ok := anyMapValueCI(t, "code"); ok {
			code = anyInt(cv)
		}
		if code == 201 {
			if pv, ok := anyMapValueCI(t, "parameters", "params"); ok {
				if p, ok := pv.([]any); ok {
					// RPG Maker XP: [direct_or_variable, map_id, x, y, direction, fade]
					if len(p) >= 4 && anyInt(p[0]) == 0 && anyInt(p[1]) > 0 {
						dir := 0
						if len(p) > 4 {
							dir = anyInt(p[4])
						}
						*out = append(*out, transferTarget{MapID: anyInt(p[1]), X: anyInt(p[2]), Y: anyInt(p[3]), Direction: dir})
					} else if len(p) >= 3 && anyInt(p[0]) > 0 {
						// Tolerate simplified converter payload [map_id, x, y, direction].
						dir := 0
						if len(p) > 3 {
							dir = anyInt(p[3])
						}
						*out = append(*out, transferTarget{MapID: anyInt(p[0]), X: anyInt(p[1]), Y: anyInt(p[2]), Direction: dir})
					}
				}
			}
		}
		// Also tolerate named command representations produced by converters.
		name := ""
		if nv, ok := anyMapValueCI(t, "command", "name", "type"); ok {
			name = strings.ToLower(anyString(nv))
		}
		if strings.Contains(name, "transfer") || strings.Contains(name, "trasfer") {
			mv, mok := anyMapValueCI(t, "map_id", "mapId", "target_map_id")
			xv, xok := anyMapValueCI(t, "x")
			yv, yok := anyMapValueCI(t, "y")
			if mok && xok && yok && anyInt(mv) > 0 {
				dir := 0
				if dv, ok := anyMapValueCI(t, "direction", "dir"); ok {
					dir = anyInt(dv)
				}
				*out = append(*out, transferTarget{MapID: anyInt(mv), X: anyInt(xv), Y: anyInt(yv), Direction: dir})
			}
		}
		for _, child := range t {
			findTransferTargets(child, out)
		}
	case []any:
		for _, child := range t {
			findTransferTargets(child, out)
		}
	}
}

func transferTargetsFromMapRaw(raw json.RawMessage) []transferTarget {
	if len(raw) == 0 {
		return nil
	}
	v, err := decodeJSONAny(raw)
	if err != nil {
		return nil
	}
	out := []transferTarget{}
	findTransferTargets(v, &out)
	return out
}
