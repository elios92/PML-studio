package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func testPBSHash(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func writeTestFile(t *testing.T, path string, b []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0644); err != nil {
		t.Fatal(err)
	}
}

func withTestPBSBaseline(t *testing.T, files map[string][]byte) {
	t.Helper()
	old := essentialsV201PBSFileHashes
	fake := make(map[string]string, len(files))
	for rel, b := range files {
		fake[rel] = testPBSHash(b)
	}
	essentialsV201PBSFileHashes = fake
	t.Cleanup(func() { essentialsV201PBSFileHashes = old })
}

func TestAnalyzeEssentialsPBSV201Exact(t *testing.T) {
	baseline := map[string][]byte{
		"pokemon.txt":       []byte("[BULBASAUR]\nName = Bulbasaur\n"),
		"Gen 8/pokemon.txt": []byte("[BULBASAUR]\nName = Bulbasaur\n"),
	}
	withTestPBSBaseline(t, baseline)
	root := t.TempDir()
	for rel, b := range baseline {
		writeTestFile(t, filepath.Join(root, filepath.FromSlash(rel)), b)
	}
	got, err := analyzeEssentialsPBSV201(root)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "BASE_V20_1" || got.ExactFiles != 2 || got.ExactMatchPercent != 100 {
		t.Fatalf("unexpected exact analysis: %+v", got)
	}
}

func TestAnalyzeEssentialsPBSV201ModifiedPreservedClassification(t *testing.T) {
	baseline := map[string][]byte{
		"pokemon.txt": []byte("[BULBASAUR]\nName = Bulbasaur\n"),
		"moves.txt":   []byte("[TACKLE]\nName = Tackle\n"),
	}
	withTestPBSBaseline(t, baseline)
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "pokemon.txt"), []byte("[BULBASAUR]\nName = Bulbasaur Custom\n[CUSTOMMON]\nName = Custommon\n"))
	writeTestFile(t, filepath.Join(root, "my_custom_data.txt"), []byte("custom=1\n"))
	got, err := analyzeEssentialsPBSV201(root)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "MODIFIED_V20_1" {
		t.Fatalf("expected modified status: %+v", got)
	}
	if len(got.ModifiedFiles) != 1 || got.ModifiedFiles[0] != "pokemon.txt" {
		t.Fatalf("modified files not detected: %+v", got.ModifiedFiles)
	}
	if len(got.MissingFiles) != 1 || got.MissingFiles[0] != "moves.txt" {
		t.Fatalf("missing files not detected: %+v", got.MissingFiles)
	}
	if len(got.AddedFiles) != 1 || got.AddedFiles[0] != "my_custom_data.txt" {
		t.Fatalf("added files not detected: %+v", got.AddedFiles)
	}
}

func TestVerifyTreeExact(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	writeTestFile(t, filepath.Join(src, "pokemon.txt"), []byte("custom pokemon\n"))
	writeTestFile(t, filepath.Join(src, "Gen 6", "moves.txt"), []byte("gen6 moves\n"))
	writeTestFile(t, filepath.Join(dst, "pokemon.txt"), []byte("custom pokemon\n"))
	writeTestFile(t, filepath.Join(dst, "Gen 6", "moves.txt"), []byte("gen6 moves\n"))
	if err := verifyTreeExact(src, dst); err != nil {
		t.Fatalf("exact copy rejected: %v", err)
	}
	writeTestFile(t, filepath.Join(dst, "pokemon.txt"), []byte("different\n"))
	if err := verifyTreeExact(src, dst); err == nil {
		t.Fatal("modified destination was not detected")
	}
}

func TestMechanicsKeepsProjectPBSAsSourceOfTruth(t *testing.T) {
	project := t.TempDir()
	base := filepath.Join(project, "converted", "PBS")
	writeTestFile(t, filepath.Join(base, "pokemon.txt"), []byte("project custom\n"))
	writeTestFile(t, filepath.Join(base, "Gen 6", "pokemon.txt"), []byte("official gen6 reference\n"))

	gotPath := mechanicsActivePBSFile(project, 6, "pokemon.txt")
	wantPath := filepath.Join(base, "pokemon.txt")
	if filepath.Clean(gotPath) != filepath.Clean(wantPath) {
		t.Fatalf("generation bundle replaced project PBS: got %s want %s", gotPath, wantPath)
	}

	state := normalizedMechanicsSettings(project, mechanicsProjectSettings{MechanicsGeneration: 6})
	if state.ActivePBSRoot != "converted/PBS" {
		t.Fatalf("wrong active PBS root: %+v", state)
	}
	if !state.GenerationPBSAvailable || state.ReferencePBSRoot != "converted/PBS/Gen 6" {
		t.Fatalf("generation reference not detected: %+v", state)
	}
}

func TestEssentialsV201ReferencePBSWhenAvailable(t *testing.T) {
	root := os.Getenv("PLM_ESSENTIALS_V201_PBS")
	if root == "" {
		t.Skip("set PLM_ESSENTIALS_V201_PBS to run reference integration test")
	}
	got, err := analyzeEssentialsPBSV201(root)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "BASE_V20_1" || got.ExactMatchPercent != 100 || len(got.ModifiedFiles) != 0 || len(got.MissingFiles) != 0 || len(got.AddedFiles) != 0 {
		t.Fatalf("canonical v20.1 PBS reference does not match embedded fingerprints: %+v", got)
	}
}

func copyTestTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, b, 0644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestModifiedV201PBSIsDetectedAndCopiedOneToOneWhenReferenceAvailable(t *testing.T) {
	reference := os.Getenv("PLM_ESSENTIALS_V201_PBS")
	if reference == "" {
		t.Skip("set PLM_ESSENTIALS_V201_PBS to run reference integration test")
	}
	source := t.TempDir()
	copyTestTree(t, reference, source)

	pokemonPath := filepath.Join(source, "pokemon.txt")
	original, err := os.ReadFile(pokemonPath)
	if err != nil {
		t.Fatal(err)
	}
	customMarker := []byte("\n# PLM TEST CUSTOM DATA\n[CUSTOM_TEST_MON]\nName = Custom Test Mon\n")
	if err := os.WriteFile(pokemonPath, append(original, customMarker...), 0644); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(source, "custom_project_data.txt"), []byte("custom=true\n"))

	analysis, err := analyzeEssentialsPBSV201(source)
	if err != nil {
		t.Fatal(err)
	}
	if analysis.Status != "MODIFIED_V20_1" {
		t.Fatalf("modified v20.1 project not recognized as modified: %+v", analysis)
	}
	foundPokemon := false
	for _, rel := range analysis.ModifiedFiles {
		if rel == "pokemon.txt" {
			foundPokemon = true
			break
		}
	}
	if !foundPokemon {
		t.Fatalf("modified pokemon.txt not reported: %+v", analysis.ModifiedFiles)
	}
	foundCustom := false
	for _, rel := range analysis.AddedFiles {
		if rel == "custom_project_data.txt" {
			foundCustom = true
			break
		}
	}
	if !foundCustom {
		t.Fatalf("custom PBS file not reported: %+v", analysis.AddedFiles)
	}

	converted := t.TempDir()
	copyTestTree(t, source, converted)
	if err := verifyTreeExact(source, converted); err != nil {
		t.Fatalf("modified/custom PBS not preserved 1:1: %v", err)
	}
}
