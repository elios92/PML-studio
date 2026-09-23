//go:build windows

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var pythonQuotedTokenRE = regexp.MustCompile(`"([A-Za-z0-9_\-]+)"`)

func pythonRuntimeTypeSet(source []byte, setName string) map[string]bool {
	out := map[string]bool{}
	text := string(source)
	startToken := setName + " = {"
	start := strings.Index(text, startToken)
	if start < 0 {
		return out
	}
	start += len(startToken)
	end := strings.Index(text[start:], "}\n")
	if end < 0 {
		return out
	}
	body := text[start : start+end]
	for _, m := range pythonQuotedTokenRE.FindAllStringSubmatch(body, -1) {
		if len(m) > 1 {
			out[strings.TrimSpace(m[1])] = true
		}
	}
	return out
}

func installedEventRuntimeTypes() map[string]bool {
	out := pythonRuntimeTypeSet(plmEventToolsRuntimePython, "RUNTIME_TYPES")
	for k := range pythonRuntimeTypeSet(plmGlobalEventsRuntimePython, "GLOBAL_TYPES") {
		out[k] = true
	}
	return out
}

func installedDirectRuntimeTypes() map[string]bool {
	out := pythonRuntimeTypeSet(plmEventToolsRuntimePython, "DIRECT_RUNTIME_TYPES")
	for k := range pythonRuntimeTypeSet(plmGlobalEventsRuntimePython, "GLOBAL_TYPES") {
		out[k] = true
	}
	return out
}

func installedNativeBackedTypes() map[string]bool {
	return pythonRuntimeTypeSet(plmEventToolsRuntimePython, "NATIVE_BACKED_TYPES")
}

func installedDataOnlyTypes() map[string]bool {
	return pythonRuntimeTypeSet(plmEventToolsRuntimePython, "DATA_ONLY_TYPES")
}

type eventRuntimeMarkerUse struct {
	Type         string
	File         string
	NativeOK     bool
	NativeReason string
}

func eventCommandCodeAny(v any) int {
	m, ok := v.(map[string]any)
	if !ok {
		return -1
	}
	for k, raw := range m {
		if strings.EqualFold(strings.TrimSpace(k), "code") {
			switch n := raw.(type) {
			case float64:
				return int(n)
			case int:
				return n
			case json.Number:
				i, _ := n.Int64()
				return int(i)
			}
		}
	}
	return -1
}

func eventCommandFirstString(v any) string {
	m, ok := v.(map[string]any)
	if !ok {
		return ""
	}
	var params any
	for k, raw := range m {
		if strings.EqualFold(strings.TrimSpace(k), "parameters") {
			params = raw
			break
		}
	}
	arr, ok := params.([]any)
	if !ok || len(arr) == 0 {
		return ""
	}
	return strings.TrimSpace(anyString(arr[0]))
}

func expectedNativeCodes(payload map[string]any) []int {
	kind := strings.TrimSpace(anyString(payload["type"]))
	switch kind {
	case "warp":
		return []int{201}
	case "self_switch":
		name := strings.ToUpper(strings.TrimSpace(anyString(payload["self_switch"])))
		if name == "A" || name == "B" || name == "C" || name == "D" {
			return []int{123}
		}
		return nil
	case "variable":
		return []int{122}
	case "choices":
		return []int{102}
	case "common_event":
		return []int{117}
	case "wait_frames":
		return []int{106}
	case "audio_play":
		switch strings.ToLower(strings.TrimSpace(anyString(payload["audio_kind"]))) {
		case "bgm":
			return []int{241}
		case "bgs":
			return []int{245}
		case "me":
			return []int{249}
		default:
			return []int{250}
		}
	case "audio_stop":
		switch strings.ToLower(strings.TrimSpace(anyString(payload["audio_kind"]))) {
		case "bgs":
			return []int{245}
		case "me":
			return []int{249}
		case "se":
			return []int{251}
		default:
			return []int{241}
		}
	case "audio_fade":
		if strings.EqualFold(strings.TrimSpace(anyString(payload["audio_kind"])), "bgs") {
			return []int{246}
		}
		return []int{242}
	case "camera_pan":
		return []int{203}
	case "camera_shake":
		return []int{225}
	}
	return nil
}

func listHasExpectedNativeAfter(list []any, markerIndex int, payload map[string]any) (bool, string) {
	expected := expectedNativeCodes(payload)
	if len(expected) == 0 {
		return true, ""
	}
	// Generated compatibility commands are emitted immediately after the marker.
	// Search only until the next PML marker/end so another command cannot satisfy
	// this contract accidentally.
	seen := map[int]bool{}
	for i := markerIndex + 1; i < len(list); i++ {
		code := eventCommandCodeAny(list[i])
		if code == 108 && strings.HasPrefix(eventCommandFirstString(list[i]), "PML_CMD:") {
			break
		}
		if code == 0 {
			break
		}
		seen[code] = true
	}
	for _, code := range expected {
		if !seen[code] {
			return false, fmt.Sprintf("manca comando nativo RPG #%d", code)
		}
	}
	return true, ""
}

func collectEventRuntimeMarkers(v any, file string, out *[]eventRuntimeMarkerUse, trainerRefs *int) {
	switch t := v.(type) {
	case map[string]any:
		for _, child := range t {
			collectEventRuntimeMarkers(child, file, out, trainerRefs)
		}
	case []any:
		for i, child := range t {
			if eventCommandCodeAny(child) == 108 {
				s := eventCommandFirstString(child)
				if strings.HasPrefix(s, "PML_CMD:") {
					var payload map[string]any
					if err := json.Unmarshal([]byte(strings.TrimPrefix(s, "PML_CMD:")), &payload); err != nil {
						*out = append(*out, eventRuntimeMarkerUse{Type: "<PML_CMD JSON NON VALIDO>", File: file})
					} else {
						kind := strings.TrimSpace(anyString(payload["type"]))
						if kind == "" {
							kind = "<PML_CMD SENZA TYPE>"
						}
						ok, why := listHasExpectedNativeAfter(t, i, payload)
						*out = append(*out, eventRuntimeMarkerUse{Type: kind, File: file, NativeOK: ok, NativeReason: why})
					}
				} else if strings.HasPrefix(s, "PML_TRAINER_REF:") {
					*trainerRefs = *trainerRefs + 1
				}
			}
			collectEventRuntimeMarkers(child, file, out, trainerRefs)
		}
	}
}

func scanProjectEventRuntimeMarkers(projectRoot string) ([]eventRuntimeMarkerUse, int, error) {
	root := filepath.Join(projectRoot, "converted", "maps")
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		return nil, 0, nil
	}
	uses := []eventRuntimeMarkerUse{}
	trainerRefs := 0
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || !strings.EqualFold(filepath.Ext(path), ".json") {
			return nil
		}
		name := strings.ToLower(filepath.Base(path))
		if !strings.HasPrefix(name, "map") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var doc any
		if err := json.Unmarshal(data, &doc); err != nil {
			return fmt.Errorf("%s: JSON non valido: %w", path, err)
		}
		rel, _ := filepath.Rel(projectRoot, path)
		collectEventRuntimeMarkers(doc, filepath.ToSlash(rel), &uses, &trainerRefs)
		return nil
	})
	return uses, trainerRefs, err
}

func validateProjectEventRuntimeCoverage(projectRoot string) error {
	root := strings.TrimSpace(projectRoot)
	if root == "" {
		return fmt.Errorf("progetto non aperto")
	}
	supported := installedEventRuntimeTypes()
	direct := installedDirectRuntimeTypes()
	native := installedNativeBackedTypes()
	dataOnly := installedDataOnlyTypes()
	if len(supported) == 0 || len(direct) == 0 || len(native) == 0 {
		return fmt.Errorf("manifest runtime comandi evento non leggibile")
	}
	uses, trainerRefs, err := scanProjectEventRuntimeMarkers(root)
	if err != nil {
		return err
	}
	unknown := map[string]map[string]bool{}
	brokenNative := map[string]map[string]bool{}
	for _, use := range uses {
		if !supported[use.Type] {
			if unknown[use.Type] == nil {
				unknown[use.Type] = map[string]bool{}
			}
			unknown[use.Type][use.File] = true
			continue
		}
		// Every supported type must have exactly one declared execution strategy.
		strategies := 0
		if direct[use.Type] {
			strategies++
		}
		if native[use.Type] {
			strategies++
		}
		if dataOnly[use.Type] {
			strategies++
		}
		if strategies != 1 {
			key := use.Type + " (contratto runtime ambiguo)"
			if unknown[key] == nil {
				unknown[key] = map[string]bool{}
			}
			unknown[key][use.File] = true
			continue
		}
		if native[use.Type] && !use.NativeOK {
			key := use.Type
			if strings.TrimSpace(use.NativeReason) != "" {
				key += " - " + use.NativeReason
			}
			if brokenNative[key] == nil {
				brokenNative[key] = map[string]bool{}
			}
			brokenNative[key][use.File] = true
		}
	}
	if trainerRefs > 0 && !strings.Contains(string(plmEventToolsRuntimePython), "PML_TRAINER_REF:") {
		unknown["trainer_battle"] = map[string]bool{"eventi Trainer": true}
	}
	if len(unknown) == 0 && len(brokenNative) == 0 {
		return nil
	}
	lines := []string{"Verifica runtime Comandi Evento fallita:"}
	appendIssueGroup := func(title string, group map[string]map[string]bool) {
		if len(group) == 0 {
			return
		}
		lines = append(lines, "", title)
		keys := make([]string, 0, len(group))
		for kind := range group {
			keys = append(keys, kind)
		}
		sort.Strings(keys)
		for _, kind := range keys {
			files := make([]string, 0, len(group[kind]))
			for file := range group[kind] {
				files = append(files, file)
			}
			sort.Strings(files)
			if len(files) > 3 {
				files = append(files[:3], "...")
			}
			lines = append(lines, fmt.Sprintf("- %s: %s", kind, strings.Join(files, ", ")))
		}
	}
	appendIssueGroup("Handler runtime mancanti/ambigui:", unknown)
	appendIssueGroup("Comandi nativi mancanti dietro il PML_CMD:", brokenNative)
	lines = append(lines, "", "Il Playtest viene bloccato: PLM non esegue eventi parziali o solo apparentemente compilati.")
	return fmt.Errorf("%s", strings.Join(lines, "\n"))
}
