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
