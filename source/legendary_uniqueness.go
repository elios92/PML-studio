//go:build windows

package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

type legendaryUsage struct {
	Species   string
	Kind      string
	MapID     int
	EventID   int
	EventName string
	AreaID    string
	AreaName  string
}

func (u legendaryUsage) locationLabel() string {
	switch u.Kind {
	case "area":
		if strings.TrimSpace(u.AreaName) != "" {
			return fmt.Sprintf("Area leggendaria \"%s\"", u.AreaName)
		}
		return "Area leggendaria " + strings.TrimSpace(u.AreaID)
	case "event":
		name := strings.TrimSpace(u.EventName)
		if name != "" {
			return fmt.Sprintf("Map%03d · Evento #%d \"%s\"", u.MapID, u.EventID, name)
		}
		return fmt.Sprintf("Map%03d · Evento #%d", u.MapID, u.EventID)
	default:
		return "posizione sconosciuta"
	}
}

func currentEditedEventID() int {
	if eventEditorEditingIndex >= 0 && eventEditorEditingIndex < len(events) {
		return events[eventEditorEditingIndex].ID
	}
	return 0
}

func collectManagedLegendaryCommands(v any, out *[]plmManagedEventCommand) {
	switch t := v.(type) {
	case string:
		if cmd, ok := parsePLMEventCommandMarker(t); ok && cmd.Type == "fixed_pokemon" && cmd.Legendary {
			*out = append(*out, cmd)
		}
	case []any:
		for _, child := range t {
			collectManagedLegendaryCommands(child, out)
		}
	case map[string]any:
		for _, child := range t {
			collectManagedLegendaryCommands(child, out)
		}
	case map[string]json.RawMessage:
		for _, raw := range t {
			var child any
			if err := json.Unmarshal(raw, &child); err == nil {
				collectManagedLegendaryCommands(child, out)
			}
		}
	}
}

func eventIdentityFromNative(v any, fallbackID int) (int, string) {
	id := fallbackID
	name := ""
	if m, ok := v.(map[string]any); ok {
		if x, ok := anyMapValueCI(m, "id", "event_id", "eventId"); ok && anyInt(x) > 0 {
			id = anyInt(x)
		}
		if x, ok := anyMapValueCI(m, "name"); ok {
			name = anyString(x)
		}
	}
	return id, name
}

func legendaryEventUsagesFromMap(path string, mapID int, excludeMapID int, excludeEventID int) []legendaryUsage {
	doc, err := loadMapDocument(path)
	if err != nil || doc == nil || doc.Raw == nil {
		return nil
	}
	raw, ok := doc.Raw["events"]
	if !ok {
		return nil
	}
	root, err := decodeJSONAny(raw)
	if err != nil {
		return nil
	}
	usages := []legendaryUsage{}
	scanEvent := func(fallbackID int, ev any) {
		eventID, eventName := eventIdentityFromNative(ev, fallbackID)
		if mapID == excludeMapID && eventID == excludeEventID && eventID > 0 {
			return
		}
		cmds := []plmManagedEventCommand{}
		collectManagedLegendaryCommands(ev, &cmds)
		for _, cmd := range cmds {
			species := strings.ToUpper(strings.TrimSpace(cmd.Species))
			if species == "" {
				continue
			}
			usages = append(usages, legendaryUsage{Species: species, Kind: "event", MapID: mapID, EventID: eventID, EventName: eventName})
		}
	}
	switch t := root.(type) {
	case map[string]any:
		for key, ev := range t {
			fallbackID := 0
			fmt.Sscanf(key, "%d", &fallbackID)
			scanEvent(fallbackID, ev)
		}
	case []any:
		for i, ev := range t {
			if ev != nil {
				scanEvent(i, ev)
			}
		}
	}
	return usages
}

func allLegendaryUsages(excludeAreaID string, excludeMapID int, excludeEventID int) []legendaryUsage {
	usages := []legendaryUsage{}
	reg := loadLegendaryAreaRegistry()
	for _, area := range reg.Areas {
		if strings.TrimSpace(excludeAreaID) != "" && strings.EqualFold(area.ID, excludeAreaID) {
			continue
		}
		for _, entry := range area.Entries {
			species := strings.ToUpper(strings.TrimSpace(entry.Species))
			if species == "" {
				continue
			}
			usages = append(usages, legendaryUsage{Species: species, Kind: "area", AreaID: area.ID, AreaName: area.Name})
		}
	}

	for _, m := range maps {
		path := strings.TrimSpace(m.File)
		if path == "" {
			continue
		}
		usages = append(usages, legendaryEventUsagesFromMap(path, m.ID, excludeMapID, excludeEventID)...)
	}
	if currentMap != nil {
		found := false
		for _, m := range maps {
			if m.ID == currentMap.ID {
				found = true
				break
			}
		}
		if !found && currentMapDoc != nil && strings.TrimSpace(currentMapDoc.Path) != "" {
			usages = append(usages, legendaryEventUsagesFromMap(currentMapDoc.Path, currentMap.ID, excludeMapID, excludeEventID)...)
		}
	}
	return usages
}

func findLegendaryDuplicate(species, excludeAreaID string, excludeCurrentEvent bool) (legendaryUsage, bool) {
	species = strings.ToUpper(strings.TrimSpace(species))
	if species == "" {
		return legendaryUsage{}, false
	}
	excludeMapID, excludeEventID := 0, 0
	if excludeCurrentEvent && currentMap != nil {
		excludeMapID = currentMap.ID
		excludeEventID = currentEditedEventID()
	}
	for _, usage := range allLegendaryUsages(excludeAreaID, excludeMapID, excludeEventID) {
		if strings.EqualFold(usage.Species, species) {
			return usage, true
		}
	}
	return legendaryUsage{}, false
}

func currentEditorLegendarySpecies() []string {
	seen := map[string]bool{}
	out := []string{}
	for _, p := range eventEditorPages {
		if p.Raw == nil {
			continue
		}
		cmds := []plmManagedEventCommand{}
		collectManagedLegendaryCommands(p.Raw, &cmds)
		for _, cmd := range cmds {
			species := strings.ToUpper(strings.TrimSpace(cmd.Species))
			if species != "" && !seen[species] {
				seen[species] = true
				out = append(out, species)
			}
		}
	}
	sort.Strings(out)
	return out
}

func validateCurrentEventLegendaryUniqueness() bool {
	speciesList := currentEditorLegendarySpecies()
	if len(speciesList) == 0 {
		return true
	}
	// A single event must not contain the same legendary more than once either.
	counts := map[string]int{}
	for _, p := range eventEditorPages {
		if p.Raw == nil {
			continue
		}
		cmds := []plmManagedEventCommand{}
		collectManagedLegendaryCommands(p.Raw, &cmds)
		for _, cmd := range cmds {
			species := strings.ToUpper(strings.TrimSpace(cmd.Species))
			if species != "" {
				counts[species]++
			}
		}
	}
	for species, count := range counts {
		if count > 1 {
			msgbox("PML Studio - Leggendario duplicato", fmt.Sprintf("%s è già presente più di una volta nello stesso evento.\r\n\r\nPLM Studio blocca il salvataggio per evitare duplicazioni involontarie.", speciesDisplayName(species)), MB_OK|MB_ICONINFORMATION)
			return false
		}
	}
	for _, species := range speciesList {
		if usage, ok := findLegendaryDuplicate(species, "", true); ok {
			msgbox("PML Studio - Leggendario duplicato", fmt.Sprintf("%s è già configurato nel progetto.\r\n\r\nPosizione esistente:\r\n%s\r\n\r\nUn Pokémon leggendario può essere configurato una sola volta. Rimuovi o modifica la configurazione esistente prima di continuare.", speciesDisplayName(species), usage.locationLabel()), MB_OK|MB_ICONINFORMATION)
			return false
		}
	}
	return true
}

func legendaryDuplicateMessage(species string, usage legendaryUsage) string {
	return fmt.Sprintf("%s è già configurato nel progetto.\r\n\r\nPosizione esistente:\r\n%s\r\n\r\nPLM Studio non permette di aggiungere una seconda copia dello stesso leggendario.", speciesDisplayName(species), usage.locationLabel())
}

func mapBaseName(path string) string { return filepath.Base(path) }
