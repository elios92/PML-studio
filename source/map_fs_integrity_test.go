package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTestMap(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
}

func copiesForID(t *testing.T, root string, id int) []string {
	t.Helper()
	m, err := scanMapCopies(root)
	if err != nil {
		t.Fatal(err)
	}
	return m[id]
}

func TestMoveRootToRegionSingleCopy(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "Map104.json")
	dst := filepath.Join(root, "regions", "NOVEPELAGO", "Interni", "Map104.json")
	writeTestMap(t, src, `{"id":104,"name":"A"}`)
	if err := fsMoveVerified(src, dst); err != nil {
		t.Fatal(err)
	}
	got := copiesForID(t, root, 104)
	if len(got) != 1 || !strings.EqualFold(got[0], dst) {
		t.Fatalf("copie=%v", got)
	}
}

func TestMoveRegionAToRegionBSingleCopy(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "regions", "A", "Map104.json")
	dst := filepath.Join(root, "regions", "B", "Map104.json")
	writeTestMap(t, src, `{"id":104,"name":"A"}`)
	if err := fsMoveVerified(src, dst); err != nil {
		t.Fatal(err)
	}
	got := copiesForID(t, root, 104)
	if len(got) != 1 || !strings.EqualFold(got[0], dst) {
		t.Fatalf("copie=%v", got)
	}
}

func TestMoveRegionToRootUnassigned(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "regions", "A", "Map104.json")
	dst := filepath.Join(root, "Map104.json")
	writeTestMap(t, src, `{"id":104,"name":"A"}`)
	if err := fsMoveVerified(src, dst); err != nil {
		t.Fatal(err)
	}
	got := copiesForID(t, root, 104)
	if len(got) != 1 || !strings.EqualFold(got[0], dst) {
		t.Fatalf("copie=%v", got)
	}
}

func TestMoveNestedFolderKeepsMapID(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "Map609.json")
	dst := filepath.Join(root, "regions", "NOVEPELAGO", "Nove Isole", "Interni", "Map609.json")
	body := `{"id":609,"name":"Nove Isole"}`
	writeTestMap(t, src, body)
	if err := fsMoveVerified(src, dst); err != nil {
		t.Fatal(err)
	}
	if id, ok := fsMapIDFromJSONName(filepath.Base(dst)); !ok || id != 609 {
		t.Fatalf("ID=%d ok=%v", id, ok)
	}
	b, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != body {
		t.Fatalf("contenuto alterato: %s", b)
	}
}

func TestRepairLegacyIdenticalRootAndRegionKeepsRoot(t *testing.T) {
	root := t.TempDir()
	flat := filepath.Join(root, "Map104.json")
	regional := filepath.Join(root, "regions", "NOVEPELAGO", "Map104.json")
	body := `{"id":104,"name":"A"}`
	writeTestMap(t, flat, body)
	writeTestMap(t, regional, body)
	report, err := repairMapDuplicates(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Conflicts) != 0 {
		t.Fatalf("conflitti=%v", report.Conflicts)
	}
	if _, err := os.Stat(flat); err != nil {
		t.Fatalf("root mancante: %v", err)
	}
	if _, err := os.Stat(regional); !os.IsNotExist(err) {
		t.Fatalf("regionale non eliminata")
	}
}

func TestRepairDifferentDuplicateDoesNotDelete(t *testing.T) {
	root := t.TempDir()
	flat := filepath.Join(root, "Map104.json")
	regional := filepath.Join(root, "regions", "NOVEPELAGO", "Map104.json")
	writeTestMap(t, flat, `{"id":104,"name":"ROOT"}`)
	writeTestMap(t, regional, `{"id":104,"name":"REGION"}`)
	report, err := repairMapDuplicates(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Conflicts) != 1 {
		t.Fatalf("conflitti=%d", len(report.Conflicts))
	}
	if _, err := os.Stat(flat); err != nil {
		t.Fatal("root cancellata")
	}
	if _, err := os.Stat(regional); err != nil {
		t.Fatal("regionale cancellata")
	}
}

func TestRescanAfterMoveMatchesFilesystem(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "Map200.json")
	dst := filepath.Join(root, "regions", "ATHERIA", "Citta", "Map200.json")
	writeTestMap(t, src, `{"id":200}`)
	if err := fsMoveVerified(src, dst); err != nil {
		t.Fatal(err)
	}
	idx, conflicts, err := uniqueMapPathIndex(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(conflicts) != 0 {
		t.Fatalf("conflitti=%v", conflicts)
	}
	if !strings.EqualFold(idx[200], dst) {
		t.Fatalf("indice=%q want=%q", idx[200], dst)
	}
}

func TestResolverFindsMapImmediatelyAfterMove(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "Map300.json")
	dst := filepath.Join(root, "regions", "ZEPHERIA", "Map300.json")
	writeTestMap(t, src, `{"id":300}`)
	if err := fsMoveVerified(src, dst); err != nil {
		t.Fatal(err)
	}
	resolved, err := resolveMapPathByID(root, 300)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.EqualFold(resolved, dst) {
		t.Fatalf("resolved=%q want=%q", resolved, dst)
	}
}

func TestExplicitMoveIdenticalDestinationKeepsDestinationAndRemovesSource(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "Map450.json")
	dst := filepath.Join(root, "regions", "NOVEPELAGO", "Map450.json")
	body := `{"id":450,"name":"Same"}`
	writeTestMap(t, src, body)
	writeTestMap(t, dst, body)
	if err := fsMoveVerified(src, dst); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatalf("sorgente ancora presente dopo MOVE esplicito")
	}
	if b, err := os.ReadFile(dst); err != nil || string(b) != body {
		t.Fatalf("destinazione non preservata: err=%v body=%q", err, b)
	}
}

func TestScanMapCopiesIgnoresEncounterMetadataFolder(t *testing.T) {
	root := t.TempDir()
	mapsRoot := filepath.Join(root, "converted", "maps")
	mustWrite := func(path, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite(filepath.Join(mapsRoot, "Map001.json"), `{"map":1}`)
	mustWrite(filepath.Join(mapsRoot, "encounters", "Map001.json"), `{"encounters":1}`)
	mustWrite(filepath.Join(mapsRoot, "regions", "NOVEPELAGO", "Interni", "Map002.json"), `{"map":2}`)

	copies, err := scanMapCopies(mapsRoot)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(copies[1]); got != 1 {
		t.Fatalf("Map001 should have exactly one physical map copy, got %d: %#v", got, copies[1])
	}
	if got := len(copies[2]); got != 1 {
		t.Fatalf("Map002 regional map should be indexed once, got %d: %#v", got, copies[2])
	}
}

func TestChooseDuplicateCanonicalRootWinsOverSingleRegion(t *testing.T) {
	root := t.TempDir()
	flat := filepath.Join(root, "Map609.json")
	regional := filepath.Join(root, "regions", "NOVEPELAGO", "Map609.json")
	got, ok := chooseMapDuplicateCanonical(root, 609, []string{regional, flat})
	if !ok {
		t.Fatal("root + regional identical copies must have an unambiguous canonical root")
	}
	if !strings.EqualFold(got, flat) {
		t.Fatalf("canonical=%q want root=%q", got, flat)
	}
}

func TestChooseDuplicateCanonicalSingleRegionWhenNoRoot(t *testing.T) {
	root := t.TempDir()
	regional := filepath.Join(root, "regions", "NOVEPELAGO", "Map609.json")
	got, ok := chooseMapDuplicateCanonical(root, 609, []string{regional})
	if !ok || !strings.EqualFold(got, regional) {
		t.Fatalf("canonical=%q ok=%v want=%q", got, ok, regional)
	}
}

func TestChooseDuplicateCanonicalMultipleRegionsWithoutRootIsAmbiguous(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "regions", "ATHERIA", "Map609.json")
	b := filepath.Join(root, "regions", "NOVEPELAGO", "Map609.json")
	if got, ok := chooseMapDuplicateCanonical(root, 609, []string{a, b}); ok || got != "" {
		t.Fatalf("ambiguous regional copies must not be auto-selected: canonical=%q ok=%v", got, ok)
	}
}

func TestChooseDuplicateCanonicalRootWinsEvenWithMultipleRegions(t *testing.T) {
	root := t.TempDir()
	flat := filepath.Join(root, "Map609.json")
	a := filepath.Join(root, "regions", "ATHERIA", "Map609.json")
	b := filepath.Join(root, "regions", "NOVEPELAGO", "Map609.json")
	got, ok := chooseMapDuplicateCanonical(root, 609, []string{a, b, flat})
	if !ok || !strings.EqualFold(got, flat) {
		t.Fatalf("canonical=%q ok=%v want root=%q", got, ok, flat)
	}
}
