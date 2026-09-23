//go:build windows

package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type TrainerTypeRecord struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type TrainerPokemonRecord struct {
	Species string `json:"species"`
	Level   int    `json:"level"`
}

type TrainerRecord struct {
	TrainerType string                 `json:"trainer_type"`
	Name        string                 `json:"name"`
	Version     int                    `json:"version"`
	Team        []TrainerPokemonRecord `json:"team"`
	Source      string                 `json:"source,omitempty"`
}

type TrainerDatabaseDocument struct {
	Version  int             `json:"version"`
	Schema   string          `json:"schema"`
	Trainers []TrainerRecord `json:"trainers"`
}

var (
	trainerCatalogProject string
	trainerCatalog        []TrainerRecord
	trainerTypeCatalog    []TrainerTypeRecord
)

func trainerDBPath() string {
	if currentProject == "" {
		return ""
	}
	return filepath.Join(currentProject, "converted", "data", "plm_trainers.json")
}

func trainerKey(t, n string, version int) string {
	return strings.ToUpper(strings.TrimSpace(t)) + "\x00" + strings.ToLower(strings.TrimSpace(n)) + "\x00" + strconv.Itoa(version)
}

func stripTrainerPBSComment(line string) string {
	inQuote := false
	for i, r := range line {
		if r == '"' {
			inQuote = !inQuote
		}
		if r == '#' && !inQuote {
			return line[:i]
		}
	}
	return line
}

func trainerPBSCandidates(name string) []string {
	if currentProject == "" {
		return nil
	}
	return []string{
		filepath.Join(currentProject, "PBS", name),
		filepath.Join(currentProject, "pbs", name),
		filepath.Join(currentProject, "converted", "PBS", name),
		filepath.Join(currentProject, "converted", "pbs", name),
	}
}

func parseTrainerSectionHeader(line string) (trainerType, name string, version int, ok bool) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "[") || !strings.HasSuffix(line, "]") {
		return "", "", 0, false
	}
	body := strings.TrimSpace(line[1 : len(line)-1])
	parts := strings.Split(body, ",")
	if len(parts) < 2 {
		return "", "", 0, false
	}
	trainerType = strings.ToUpper(strings.TrimSpace(parts[0]))
	name = strings.TrimSpace(parts[1])
	if len(parts) >= 3 {
		version, _ = strconv.Atoi(strings.TrimSpace(parts[2]))
	}
	return trainerType, name, version, trainerType != "" && name != ""
}

func parseTrainersPBS(path string) []TrainerRecord {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	out := []TrainerRecord{}
	var cur *TrainerRecord
	flush := func() {
		if cur == nil || cur.TrainerType == "" || cur.Name == "" {
			cur = nil
			return
		}
		if cur.Team == nil {
			cur.Team = []TrainerPokemonRecord{}
		}
		out = append(out, *cur)
		cur = nil
	}

	s := bufio.NewScanner(f)
	for s.Scan() {
		raw := strings.TrimSpace(stripTrainerPBSComment(s.Text()))
		if raw == "" {
			continue
		}
		if t, n, v, ok := parseTrainerSectionHeader(raw); ok {
			flush()
			cur = &TrainerRecord{TrainerType: t, Name: n, Version: v, Source: filepath.ToSlash(path)}
			continue
		}
		if cur == nil {
			continue
		}
		eq := strings.Index(raw, "=")
		if eq < 0 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(raw[:eq]))
		value := strings.TrimSpace(raw[eq+1:])
		if key == "pokemon" {
			parts := strings.Split(value, ",")
			if len(parts) >= 2 {
				species := strings.ToUpper(strings.TrimSpace(parts[0]))
				level, _ := strconv.Atoi(strings.TrimSpace(parts[1]))
				if species != "" {
					if level <= 0 {
						level = 1
					}
					cur.Team = append(cur.Team, TrainerPokemonRecord{Species: species, Level: level})
				}
			}
		}
	}
	flush()
	return out
}

func parseTrainerTypesPBS(path string) []TrainerTypeRecord {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	out := []TrainerTypeRecord{}
	section := ""
	display := ""
	flush := func() {
		id := strings.ToUpper(strings.TrimSpace(section))
		if id == "" {
			section, display = "", ""
			return
		}
		if strings.TrimSpace(display) == "" {
			display = id
		}
		out = append(out, TrainerTypeRecord{ID: id, Name: strings.TrimSpace(display)})
		section, display = "", ""
	}
	s := bufio.NewScanner(f)
	for s.Scan() {
		raw := strings.TrimSpace(stripTrainerPBSComment(s.Text()))
		if raw == "" {
			continue
		}
		if strings.HasPrefix(raw, "[") && strings.HasSuffix(raw, "]") {
			flush()
			section = strings.TrimSpace(raw[1 : len(raw)-1])
			continue
		}
		if section == "" {
			continue
		}
		if eq := strings.Index(raw, "="); eq >= 0 {
			key := strings.ToLower(strings.TrimSpace(raw[:eq]))
			value := strings.TrimSpace(raw[eq+1:])
			if key == "name" {
				display = value
			}
		}
	}
	flush()
	return out
}

func loadPLMTrainerOverrides() []TrainerRecord {
	path := trainerDBPath()
	if path == "" {
		return nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var doc TrainerDatabaseDocument
	if err := json.Unmarshal(b, &doc); err != nil {
		mapLogf("[TRAINERS] plm_trainers.json non valido: %v", err)
		return nil
	}
	return doc.Trainers
}

func savePLMTrainerDatabase(records []TrainerRecord) error {
	path := trainerDBPath()
	if path == "" {
		return fmt.Errorf("nessun progetto aperto")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	sort.Slice(records, func(i, j int) bool {
		if records[i].TrainerType != records[j].TrainerType {
			return records[i].TrainerType < records[j].TrainerType
		}
		if strings.ToLower(records[i].Name) != strings.ToLower(records[j].Name) {
			return strings.ToLower(records[i].Name) < strings.ToLower(records[j].Name)
		}
		return records[i].Version < records[j].Version
	})
	doc := TrainerDatabaseDocument{Version: 1, Schema: "pml.trainers.v1", Trainers: records}
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func rebuildTrainerCatalog() {
	trainerCatalogProject = currentProject
	byKey := map[string]TrainerRecord{}
	typeMap := map[string]TrainerTypeRecord{}

	for _, p := range trainerPBSCandidates("trainer_types.txt") {
		for _, tt := range parseTrainerTypesPBS(p) {
			if tt.ID != "" {
				typeMap[tt.ID] = tt
			}
		}
	}
	for _, p := range trainerPBSCandidates("trainers.txt") {
		for _, tr := range parseTrainersPBS(p) {
			k := trainerKey(tr.TrainerType, tr.Name, tr.Version)
			if _, exists := byKey[k]; !exists {
				byKey[k] = tr
			}
			if _, exists := typeMap[tr.TrainerType]; !exists {
				typeMap[tr.TrainerType] = TrainerTypeRecord{ID: tr.TrainerType, Name: tr.TrainerType}
			}
		}
	}

	// Gli override PML hanno precedenza sui PBS importati, ma restano nel
	// database dati del gioco, non nella MapXXX.json.
	for _, tr := range loadPLMTrainerOverrides() {
		tr.TrainerType = strings.ToUpper(strings.TrimSpace(tr.TrainerType))
		if tr.TrainerType == "" || strings.TrimSpace(tr.Name) == "" {
			continue
		}
		byKey[trainerKey(tr.TrainerType, tr.Name, tr.Version)] = tr
		if _, exists := typeMap[tr.TrainerType]; !exists {
			typeMap[tr.TrainerType] = TrainerTypeRecord{ID: tr.TrainerType, Name: tr.TrainerType}
		}
	}

	trainerCatalog = trainerCatalog[:0]
	for _, tr := range byKey {
		trainerCatalog = append(trainerCatalog, tr)
	}
	sort.Slice(trainerCatalog, func(i, j int) bool {
		if trainerCatalog[i].TrainerType != trainerCatalog[j].TrainerType {
			return trainerCatalog[i].TrainerType < trainerCatalog[j].TrainerType
		}
		if strings.ToLower(trainerCatalog[i].Name) != strings.ToLower(trainerCatalog[j].Name) {
			return strings.ToLower(trainerCatalog[i].Name) < strings.ToLower(trainerCatalog[j].Name)
		}
		return trainerCatalog[i].Version < trainerCatalog[j].Version
	})

	trainerTypeCatalog = trainerTypeCatalog[:0]
	for _, tt := range typeMap {
		trainerTypeCatalog = append(trainerTypeCatalog, tt)
	}
	sort.Slice(trainerTypeCatalog, func(i, j int) bool {
		a, b := strings.ToLower(trainerTypeCatalog[i].Name), strings.ToLower(trainerTypeCatalog[j].Name)
		if a == b {
			return trainerTypeCatalog[i].ID < trainerTypeCatalog[j].ID
		}
		return a < b
	})
	mapLogf("[TRAINERS] caricati %d allenatori e %d tipi allenatore", len(trainerCatalog), len(trainerTypeCatalog))
}

func ensureTrainerCatalog() {
	if currentProject == "" {
		return
	}
	if !strings.EqualFold(filepath.Clean(trainerCatalogProject), filepath.Clean(currentProject)) {
		rebuildTrainerCatalog()
	}
}

func upsertTrainerRecord(record TrainerRecord) error {
	ensureTrainerCatalog()
	record.TrainerType = strings.ToUpper(strings.TrimSpace(record.TrainerType))
	record.Name = strings.TrimSpace(record.Name)
	if record.TrainerType == "" || record.Name == "" {
		return fmt.Errorf("tipo e nome allenatore sono obbligatori")
	}
	if record.Version < 0 {
		record.Version = 0
	}
	for i := range record.Team {
		record.Team[i].Species = strings.ToUpper(strings.TrimSpace(record.Team[i].Species))
		if record.Team[i].Level < 1 {
			record.Team[i].Level = 1
		}
		if record.Team[i].Level > 100 {
			record.Team[i].Level = 100
		}
	}
	record.Source = "converted/data/plm_trainers.json"

	overrides := loadPLMTrainerOverrides()
	key := trainerKey(record.TrainerType, record.Name, record.Version)
	replaced := false
	for i := range overrides {
		if trainerKey(overrides[i].TrainerType, overrides[i].Name, overrides[i].Version) == key {
			overrides[i] = record
			replaced = true
			break
		}
	}
	if !replaced {
		overrides = append(overrides, record)
	}
	if err := savePLMTrainerDatabase(overrides); err != nil {
		return err
	}
	trainerCatalogProject = ""
	rebuildTrainerCatalog()
	return nil
}
