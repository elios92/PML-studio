//go:build windows

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type EventDataRegistry struct {
	Version   int      `json:"version"`
	Switches  []string `json:"switches"`
	Variables []string `json:"variables"`
}

func eventDataRegistryPath() string {
	if currentProject == "" {
		return ""
	}
	dir := filepath.Join(currentProject, ".plm", "data")
	_ = os.MkdirAll(dir, 0755)
	return filepath.Join(dir, "event_globals.json")
}

func normalizeEventDataNames(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		name := strings.TrimSpace(value)
		if name == "" {
			continue
		}
		key := strings.ToLower(name)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, name)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return strings.ToLower(out[i]) < strings.ToLower(out[j])
	})
	return out
}

func namesFromJSONValue(v any) []string {
	out := []string{}
	switch t := v.(type) {
	case []any:
		for _, item := range t {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, s)
			}
		}
	case map[string]any:
		for _, item := range t {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, s)
			}
		}
	}
	return out
}

func collectSystemEventData(v any, switches, variables *[]string) {
	switch t := v.(type) {
	case map[string]any:
		for k, child := range t {
			key := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(k, "-", "_"), " ", "_"))
			switch key {
			case "switches", "switch_names", "switch_names_list":
				*switches = append(*switches, namesFromJSONValue(child)...)
			case "variables", "variable_names", "variable_names_list":
				*variables = append(*variables, namesFromJSONValue(child)...)
			}
			collectSystemEventData(child, switches, variables)
		}
	case []any:
		for _, child := range t {
			collectSystemEventData(child, switches, variables)
		}
	}
}

func importProjectEventData(reg *EventDataRegistry) {
	if currentProject == "" {
		return
	}
	candidates := []string{
		filepath.Join(currentProject, "converted", "System.json"),
		filepath.Join(currentProject, "converted", "system.json"),
		filepath.Join(currentProject, "converted", "data", "System.json"),
		filepath.Join(currentProject, "converted", "data", "system.json"),
		filepath.Join(currentProject, "Data", "System.json"),
		filepath.Join(currentProject, "data", "system.json"),
	}
	for _, candidate := range candidates {
		b, err := os.ReadFile(candidate)
		if err != nil {
			continue
		}
		var raw any
		if json.Unmarshal(b, &raw) != nil {
			continue
		}
		collectSystemEventData(raw, &reg.Switches, &reg.Variables)
	}
}

func loadEventDataRegistry() EventDataRegistry {
	reg := EventDataRegistry{Version: 1}
	if path := eventDataRegistryPath(); path != "" {
		if b, err := os.ReadFile(path); err == nil {
			_ = json.Unmarshal(b, &reg)
		}
	}
	importProjectEventData(&reg)
	reg.Version = 1
	reg.Switches = normalizeEventDataNames(reg.Switches)
	reg.Variables = normalizeEventDataNames(reg.Variables)
	return reg
}

func saveEventDataRegistry(reg EventDataRegistry) {
	path := eventDataRegistryPath()
	if path == "" {
		return
	}
	reg.Version = 1
	reg.Switches = normalizeEventDataNames(reg.Switches)
	reg.Variables = normalizeEventDataNames(reg.Variables)
	b, err := json.MarshalIndent(reg, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(path, b, 0644)
}

func registerEventSwitchName(name string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	reg := loadEventDataRegistry()
	reg.Switches = append(reg.Switches, name)
	saveEventDataRegistry(reg)
}

func registerEventVariableName(name string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	reg := loadEventDataRegistry()
	reg.Variables = append(reg.Variables, name)
	saveEventDataRegistry(reg)
}
