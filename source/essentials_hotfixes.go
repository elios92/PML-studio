//go:build windows

package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const essentialsV201HotfixProfile = "pokemon-essentials-v20.1-hotfixes-1.0.7"

var essentialsV201HotfixFiles = []string{
	"Battle bug fixes.rb",
	"Compiler bug fixes.rb",
	"Debug bug fixes.rb",
	"Misc bug fixes.rb",
	"Overworld bug fixes.rb",
}

var essentialsV201Hotfix107SHA256 = map[string]string{
	"Battle bug fixes.rb":    "bed1ee609335eaa1479d82ffd63be0e4221d465031b6e452425341ff8cf9fb52",
	"Compiler bug fixes.rb":  "debb5db01444ce69f04abe02f9634d4a7af9e62229b062f176836e9e189243a4",
	"Debug bug fixes.rb":     "50fbb577742e5572a389334fa633844539813aa68d9d6769ab90ca92e76ac219",
	"Misc bug fixes.rb":      "60d31d67c3c30b60ca127d25dc8134e8dd5650881236b823383c644125a8ed7b",
	"Overworld bug fixes.rb": "b0908b86cf0cc05ccc14f7a923c0ab74c84324577abc860ab85b9430694397c3",
}

type essentialsHotfixFile struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

type essentialsHotfixProfile struct {
	Schema            string                `json:"schema"`
	Version           int                   `json:"version"`
	Detected          bool                  `json:"detected"`
	Name              string                `json:"name,omitempty"`
	PluginVersion     string                `json:"plugin_version,omitempty"`
	EssentialsVersion string                `json:"essentials_version,omitempty"`
	SourceRelative    string                `json:"source_relative,omitempty"`
	NativeProfile     string                `json:"native_profile,omitempty"`
	ExactOfficial107  bool                  `json:"exact_official_1_0_7"`
	Files             []essentialsHotfixFile `json:"files,omitempty"`
	ChangeLog         []string              `json:"change_log,omitempty"`
}

func parsePluginMeta(path string) (map[string]string, []string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	values := map[string]string{}
	var changes []string
	scanner := bufio.NewScanner(f)
	var pending string
	for scanner.Scan() {
		line := strings.TrimPrefix(scanner.Text(), "﻿")
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "# - ") {
			if pending != "" {
				changes = append(changes, strings.TrimSpace(pending))
			}
			pending = strings.TrimSpace(strings.TrimPrefix(trimmed, "# - "))
			continue
		}
		if pending != "" && strings.HasPrefix(trimmed, "#   ") {
			pending += " " + strings.TrimSpace(strings.TrimPrefix(trimmed, "#   "))
			continue
		}
		if pending != "" {
			changes = append(changes, strings.TrimSpace(pending))
			pending = ""
		}
		if strings.HasPrefix(trimmed, "#") || !strings.Contains(trimmed, "=") {
			continue
		}
		parts := strings.SplitN(trimmed, "=", 2)
		values[strings.ToLower(strings.TrimSpace(parts[0]))] = strings.TrimSpace(parts[1])
	}
	if pending != "" {
		changes = append(changes, strings.TrimSpace(pending))
	}
	if err := scanner.Err(); err != nil {
		return nil, nil, err
	}
	return values, changes, nil
}

func detectEssentialsV201Hotfixes(source string) (essentialsHotfixProfile, error) {
	profile := essentialsHotfixProfile{
		Schema:  "pml.essentials_hotfix_profile",
		Version: 1,
	}
	pluginsRoot := filepath.Join(source, "Plugins")
	if !isDir(pluginsRoot) {
		return profile, nil
	}
	var metas []string
	err := filepath.WalkDir(pluginsRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() && strings.EqualFold(entry.Name(), "meta.txt") {
			metas = append(metas, path)
		}
		return nil
	})
	if err != nil {
		return profile, err
	}
	sort.Strings(metas)
	for _, metaPath := range metas {
		values, changes, err := parsePluginMeta(metaPath)
		if err != nil {
			return profile, fmt.Errorf("lettura %s: %w", metaPath, err)
		}
		if !strings.EqualFold(strings.TrimSpace(values["name"]), "v20.1 Hotfixes") ||
			!strings.EqualFold(strings.TrimSpace(values["essentials"]), "20.1") {
			continue
		}
		version := strings.TrimSpace(values["version"])
		if version == "" {
			return profile, fmt.Errorf("plugin v20.1 Hotfixes senza Version in %s", metaPath)
		}
		root := filepath.Dir(metaPath)
		files := make([]essentialsHotfixFile, 0, len(essentialsV201HotfixFiles))
		exact107 := strings.EqualFold(version, "1.0.7")
		for _, name := range essentialsV201HotfixFiles {
			path := filepath.Join(root, name)
			info, statErr := os.Stat(path)
			if statErr != nil || info.IsDir() {
				return profile, fmt.Errorf("v20.1 Hotfixes %s incompleto: manca %s", version, name)
			}
			hash, hashErr := sha256HexFile(path)
			if hashErr != nil {
				return profile, hashErr
			}
			if wanted := essentialsV201Hotfix107SHA256[name]; wanted == "" || !strings.EqualFold(hash, wanted) {
				exact107 = false
			}
			files = append(files, essentialsHotfixFile{Name: name, SHA256: hash, Size: info.Size()})
		}
		rel, _ := filepath.Rel(source, root)
		profile.Detected = true
		profile.Name = values["name"]
		profile.PluginVersion = version
		profile.EssentialsVersion = values["essentials"]
		profile.SourceRelative = filepath.ToSlash(rel)
		profile.ExactOfficial107 = exact107
		if exact107 {
			profile.NativeProfile = essentialsV201HotfixProfile
		}
		profile.Files = files
		profile.ChangeLog = changes
		return profile, nil
	}
	return profile, nil
}

func writeEssentialsHotfixProfile(dest string, profile essentialsHotfixProfile) error {
	return writeJSON(filepath.Join(dest, "converted", "essentials_hotfixes.json"), profile)
}
