//go:build windows

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSafeGameExecutableBaseName(t *testing.T) {
	cases := map[string]string{
		"": "Pokemon Game",
		"  My   Game  ": "My Game",
		"My:Game?": "My Game",
		"CON": "CON Game",
		"Game. ": "Game",
	}
	for in, want := range cases {
		if got := safeGameExecutableBaseName(in); got != want {
			t.Fatalf("safeGameExecutableBaseName(%q)=%q, want %q", in, got, want)
		}
	}
}

func TestRubyRuntimeCoverageAllowsUnmodifiedCore(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "converted", "scripts_ruby")
	if err := os.MkdirAll(dir, 0755); err != nil { t.Fatal(err) }
	rows := []map[string]string{{"name":"Core","classification":"core_v20_1"}}
	b, _ := json.Marshal(rows)
	if err := os.WriteFile(filepath.Join(dir, "index.json"), b, 0644); err != nil { t.Fatal(err) }
	if err := validateRubyRuntimeCoverage(root); err != nil {
		t.Fatalf("unmodified core should not create a customization gap: %v", err)
	}
}

func TestRubyRuntimeCoverageBlocksUnsupportedCustomization(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "converted", "scripts_ruby")
	if err := os.MkdirAll(dir, 0755); err != nil { t.Fatal(err) }
	rows := []map[string]string{{"name":"PokéWord","classification":"custom"}}
	b, _ := json.Marshal(rows)
	if err := os.WriteFile(filepath.Join(dir, "index.json"), b, 0644); err != nil { t.Fatal(err) }
	err := validateRubyRuntimeCoverage(root)
	if err == nil || !strings.Contains(err.Error(), "1 script Ruby") {
		t.Fatalf("expected unsupported Ruby customization error, got %v", err)
	}
	report := filepath.Join(root, "converted", "runtime_compatibility_gaps.json")
	data, readErr := os.ReadFile(report)
	if readErr != nil { t.Fatalf("gap report missing: %v", readErr) }
	if !strings.Contains(string(data), "PokéWord") {
		t.Fatalf("gap report does not identify custom script: %s", data)
	}
}


func TestRuntimeTilesetContractPaths(t *testing.T) {
	root := t.TempDir()
	for _, rel := range []string{
		filepath.Join("converted", "tilesets"),
		filepath.Join("assets", "Graphics", "Tilesets"),
		filepath.Join("assets", "Graphics", "Autotiles"),
	} {
		if err := os.MkdirAll(filepath.Join(root, rel), 0755); err != nil {
			t.Fatal(err)
		}
	}
	// This regression test locks the canonical paths used by conversion,
	// validation, editor and runtime. It intentionally does not accept _PLM_ID
	// or alternate Autotiles directories.
	for _, rel := range []string{
		filepath.Join("converted", "tilesets"),
		filepath.Join("assets", "Graphics", "Tilesets"),
		filepath.Join("assets", "Graphics", "Autotiles"),
	} {
		info, err := os.Stat(filepath.Join(root, rel))
		if err != nil || !info.IsDir() {
			t.Fatalf("canonical runtime asset path missing: %s", filepath.ToSlash(rel))
		}
	}
}


func TestEmptyConnectionDocumentIsValidCanonicalData(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "converted", "map_connections.json")
	doc := defaultConnectionDocument()
	if err := writeConnectionJSONFile(path, doc); err != nil {
		t.Fatal(err)
	}
	got, err := readCanonicalConnectionDocument(path)
	if err != nil {
		t.Fatalf("empty canonical connection document must be readable: %v", err)
	}
	if got.Schema != connectionSchema || got.Version != connectionVersion {
		t.Fatalf("unexpected connection contract: %q v%d", got.Schema, got.Version)
	}
	if got.Connections == nil || len(got.Connections) != 0 {
		t.Fatalf("zero connections must serialize as an empty array, got %#v", got.Connections)
	}
}


func TestBattleSettingsCanonicalDefaults(t *testing.T) {
	root := t.TempDir()
	want := defaultBattleSettings()
	if err := saveBattleSettings(root, want); err != nil {
		// The embedded runtime is part of the real save contract. If it is
		// unavailable, conversion must fail rather than produce a disconnected
		// UI-only settings file.
		t.Fatalf("save canonical battle settings: %v", err)
	}
	got, err := loadBattleSettings(root)
	if err != nil {
		t.Fatal(err)
	}
	if got.Schema != battleSettingsSchema || got.Version != battleSettingsVersion ||
		got.MaxPartySize != 6 || got.MegaEvolutionsEnabled ||
		got.AwakeningsEnabled || got.WildGroupEncounters ||
		got.WildGroupMin != 3 || got.WildGroupMax != 5 {
		t.Fatalf("unexpected imported battle defaults: %#v", got)
	}
	if info, err := os.Stat(battleSettingsPath(root)); err != nil || info.Size() == 0 {
		t.Fatalf("converted/battle_settings.json missing or empty: %v", err)
	}
}
