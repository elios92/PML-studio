package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMergeMissingPBSRecordsPreservesExisting(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "pokemon.txt")
	source := filepath.Join(dir, "gen_pokemon.txt")
	original := []byte("# custom project\r\n#-------------------------------\r\n[BULBASAUR]\r\nName = Bulbasaur Custom\r\nCustomField = KEEP_ME\r\n#-------------------------------\r\n[NOCTHAROS]\r\nName = Noctharos\r\n")
	gen := []byte("# official\r\n#-------------------------------\r\n[BULBASAUR]\r\nName = Bulbasaur Official\r\n#-------------------------------\r\n[CHARMANDER]\r\nName = Charmander\r\n")
	if err := os.WriteFile(target, original, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, gen, 0644); err != nil {
		t.Fatal(err)
	}

	res, err := mergeMissingPBSRecords(target, source)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.AddedIDs) != 1 || res.AddedIDs[0] != "CHARMANDER" {
		t.Fatalf("added IDs = %#v", res.AddedIDs)
	}
	after, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(after, original) {
		t.Fatalf("existing custom PBS bytes were modified")
	}
	text := string(after)
	if !strings.Contains(text, "CustomField = KEEP_ME") || !strings.Contains(text, "[NOCTHAROS]") {
		t.Fatalf("custom records were not preserved")
	}
	if strings.Contains(text, "Bulbasaur Official") {
		t.Fatalf("existing BULBASAUR was overwritten")
	}
	if !strings.Contains(text, "[CHARMANDER]") {
		t.Fatalf("missing Pokémon was not appended")
	}

	res2, err := mergeMissingPBSRecords(target, source)
	if err != nil {
		t.Fatal(err)
	}
	if len(res2.AddedIDs) != 0 {
		t.Fatalf("second merge should be idempotent, got %#v", res2.AddedIDs)
	}
}

func TestGenerationMergeAddsPokemonAndDependenciesOnly(t *testing.T) {
	project := t.TempDir()
	base := filepath.Join(project, "converted", "PBS")
	genDir := filepath.Join(base, "Gen 6")
	if err := os.MkdirAll(genDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "pokemon.txt"), []byte("[CUSTOMMON]\nName = Custommon\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(genDir, "pokemon.txt"), []byte("[CUSTOMMON]\nName = OfficialShouldNotWin\n#-------------------------------\n[NEWMON]\nName = Newmon\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "moves.txt"), []byte("[CUSTOMMOVE]\nName = Custom Move\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(genDir, "moves.txt"), []byte("[NEWMOVE]\nName = New Move\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "encounters.txt"), []byte("[001]\nLand,1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(genDir, "encounters.txt"), []byte("[999]\nLand,1\n"), 0644); err != nil {
		t.Fatal(err)
	}

	report, err := mergeGenerationPBSAdditive(project, 6)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.AddedPokemon) != 1 || report.AddedPokemon[0] != "NEWMON" {
		t.Fatalf("added Pokémon = %#v", report.AddedPokemon)
	}
	pokemon, _ := os.ReadFile(filepath.Join(base, "pokemon.txt"))
	if !strings.Contains(string(pokemon), "[CUSTOMMON]") || !strings.Contains(string(pokemon), "[NEWMON]") {
		t.Fatalf("pokemon merge incomplete")
	}
	if strings.Contains(string(pokemon), "OfficialShouldNotWin") {
		t.Fatalf("custom Pokémon was overwritten")
	}
	moves, _ := os.ReadFile(filepath.Join(base, "moves.txt"))
	if !strings.Contains(string(moves), "[CUSTOMMOVE]") || !strings.Contains(string(moves), "[NEWMOVE]") {
		t.Fatalf("dependency merge incomplete")
	}
	encounters, _ := os.ReadFile(filepath.Join(base, "encounters.txt"))
	if strings.Contains(string(encounters), "[999]") {
		t.Fatalf("encounters must not be merged")
	}
}
