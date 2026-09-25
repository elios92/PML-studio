//go:build windows

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEssentialsV201Hotfix107Fingerprint(t *testing.T) {
	want := map[string]string{
		"Battle bug fixes.rb":    "bed1ee609335eaa1479d82ffd63be0e4221d465031b6e452425341ff8cf9fb52",
		"Compiler bug fixes.rb":  "debb5db01444ce69f04abe02f9634d4a7af9e62229b062f176836e9e189243a4",
		"Debug bug fixes.rb":     "50fbb577742e5572a389334fa633844539813aa68d9d6769ab90ca92e76ac219",
		"Misc bug fixes.rb":      "60d31d67c3c30b60ca127d25dc8134e8dd5650881236b823383c644125a8ed7b",
		"Overworld bug fixes.rb": "b0908b86cf0cc05ccc14f7a923c0ab74c84324577abc860ab85b9430694397c3",
	}
	if len(essentialsV201Hotfix107SHA256) != len(want) {
		t.Fatalf("hotfix fingerprint count=%d want=%d", len(essentialsV201Hotfix107SHA256), len(want))
	}
	for name, hash := range want {
		if !strings.EqualFold(essentialsV201Hotfix107SHA256[name], hash) {
			t.Fatalf("fingerprint %s mismatch", name)
		}
	}
}

func TestInstallEssentialsV201HotfixRuntime(t *testing.T) {
	root := t.TempDir()
	for _, rel := range []string{
		"game/capture_system.py",
		"game/wild_battle.py",
		"game/battle_mechanics.py",
		"game/item_effects.py",
		"game/daycare_system.py",
	} {
		data, err := readEmbeddedRuntimeFile(rel)
		if err != nil {
			t.Fatalf("read embedded %s: %v", rel, err)
		}
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(root, "converted"), 0755); err != nil {
		t.Fatal(err)
	}
	files := make([]essentialsHotfixFile, 0, len(essentialsV201HotfixFiles))
	for _, name := range essentialsV201HotfixFiles {
		files = append(files, essentialsHotfixFile{Name: name, SHA256: essentialsV201Hotfix107SHA256[name]})
	}
	profile := essentialsHotfixProfile{
		Schema: "pml.essentials_hotfix_profile", Version: 1, Detected: true,
		Name: "v20.1 Hotfixes", PluginVersion: "1.0.7", EssentialsVersion: "20.1",
		NativeProfile: essentialsV201HotfixProfile, ExactOfficial107: true, Files: files,
	}
	data, _ := json.Marshal(profile)
	if err := os.WriteFile(filepath.Join(root, "converted", "essentials_hotfixes.json"), data, 0644); err != nil {
		t.Fatal(err)
	}

	if err := installEssentialsV201HotfixRuntime(root); err != nil {
		t.Fatalf("install hotfix runtime: %v", err)
	}

	checks := map[string][]string{
		"game/capture_system.py": {
			`if str(ball or "").upper() == "HEAVYBALL"`,
			"weight_hg >= 3000",
			"capture_value(target, species_data, multiplier, ball)",
		},
		"game/wild_battle.py": {
			"throw_ball(self.enemy, species_data, multiplier, ball=ball)",
		},
		"game/battle_mechanics.py": {
			"ProtectUserFromDamagingMovesObstruct",
			`code == "HigherPriorityInGrassyTerrain"`,
			`if code == "IgnoreTargetDefSpDefEvaStatStages":`,
			`self.active_ability(ally) == "PASTELVEIL"`,
			"if fixed is None:",
			`self.active_ability(defender) == "LIQUIDOOZE"`,
		},
		"game/item_effects.py": {
			"was_fainted",
			"base_stats[0] == 1",
			`pokemon["hp"] = 1`,
		},
		"game/daycare_system.py": {
			"def _inherit_ability_hotfix",
			"def _inherit_ivs_hotfix",
			`"DESTINYKNOT"`,
			`"POWERWEIGHT": 0`,
		},
	}
	for rel, wants := range checks {
		body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		text := string(body)
		for _, want := range wants {
			if !strings.Contains(text, want) {
				t.Fatalf("%s missing %q", rel, want)
			}
		}
	}

	var coverage essentialsHotfixCoverage
	coverageData, err := os.ReadFile(filepath.Join(root, "converted", "essentials_hotfix_coverage.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(coverageData, &coverage); err != nil {
		t.Fatal(err)
	}
	if coverage.Profile != essentialsV201HotfixProfile || coverage.PluginVersion != "1.0.7" {
		t.Fatalf("wrong coverage profile: %+v", coverage)
	}
	if len(coverage.Entries) < 30 {
		t.Fatalf("hotfix coverage too small: %d", len(coverage.Entries))
	}
	for _, entry := range coverage.Entries {
		if entry.Status == "" || entry.Note == "" {
			t.Fatalf("incomplete coverage entry: %+v", entry)
		}
	}
}

func TestModifiedV201HotfixProfileIsNotNativeCovered(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "converted"), 0755); err != nil {
		t.Fatal(err)
	}
	profile := essentialsHotfixProfile{
		Schema: "pml.essentials_hotfix_profile", Version: 1, Detected: true,
		Name: "v20.1 Hotfixes", PluginVersion: "1.0.7", EssentialsVersion: "20.1",
		NativeProfile: "", ExactOfficial107: false,
	}
	data, _ := json.Marshal(profile)
	if err := os.WriteFile(filepath.Join(root, "converted", "essentials_hotfixes.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
	if _, enabled := loadExactEssentialsHotfixProfile(root); enabled {
		t.Fatal("modified/non-official hotfix must not activate native compatibility profile")
	}
	if got := nativeCoveredEssentialsHotfixHashes(root); len(got) != 0 {
		t.Fatalf("modified hotfix incorrectly covered: %v", got)
	}
}
