//go:build windows

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestManifestBattleMigrationCompatibility(t *testing.T) {
	for _, payload := range []string{
		`{"format_version":1,"Name":"Legacy"}`,
		`{"mega_evolutions_enabled":true,"awakenings_enabled":false}`,
		`{"mega_evolutions_enabled":false,"awakenings_enabled":true}`,
		`{"mega_evolutions_enabled":true,"awakenings_enabled":true}`,
	} {
		t.Run(payload, func(t *testing.T) {
			root := t.TempDir()
			var manifest ProjectManifest
			if err := json.Unmarshal([]byte(payload), &manifest); err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(manifest)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "plm_project.json"), encoded, 0600); err != nil {
				t.Fatal(err)
			}
			settings, err := loadBattleSettings(root)
			if err != nil {
				t.Fatal(err)
			}
			if settings.MegaEvolutionsEnabled != manifest.MegaEvolutionsEnabled || settings.AwakeningsEnabled != manifest.AwakeningsEnabled || settings.MaxPartySize != 6 {
				t.Fatalf("migration lost values: %+v", settings)
			}
			// Once saved, the canonical settings must take precedence over the manifest.
			settings.MegaEvolutionsEnabled = !manifest.MegaEvolutionsEnabled
			settings.AwakeningsEnabled = !manifest.AwakeningsEnabled
			if err := saveBattleSettings(root, settings); err != nil {
				t.Fatal(err)
			}
			reopened, err := loadBattleSettings(root)
			if err != nil {
				t.Fatal(err)
			}
			if reopened != settings {
				t.Fatalf("canonical settings changed: %+v", reopened)
			}
		})
	}
}

func TestPermissionDocBorderCompatibility(t *testing.T) {
	oldTiles, oldDefined := borderDirectionTiles, borderDirectionDefined
	oldSlot, oldDirection := selectedBorderSlot, selectedBorderDirection
	t.Cleanup(func() {
		borderDirectionTiles, borderDirectionDefined = oldTiles, oldDefined
		selectedBorderSlot, selectedBorderDirection = oldSlot, oldDirection
	})
	for _, representation := range []string{"flat", "blocks"} {
		t.Run(representation, func(t *testing.T) {
			flat := map[string][]int{}
			blocks := map[string][][]int{}
			for i, key := range borderDirectionKeys {
				flat[key] = []int{i * 4, i*4 + 1, i*4 + 2, i*4 + 3}
				blocks[key] = [][]int{{i * 4, i*4 + 1}, {i*4 + 2, i*4 + 3}}
			}
			doc := PermissionDoc{Version: 6, MapID: 1, Width: 20, Height: 15,
				Cells: map[string]string{"0,0": "0"}, TerrainTags: map[string]int{"1,1": 2},
				Border: []int{1, 2, 3, 4}, BorderTiles: []int{1, 2, 3, 4}, BorderBlock: [][]int{{1, 2}, {3, 4}}, BorderWidth: 2, BorderHeight: 2,
				BorderSchema: "directional_2x2_v1", BorderDirectionWidth: 2, BorderDirectionHeight: 2}
			if representation == "flat" {
				doc.BorderDirections = flat
			} else {
				doc.BorderDirectionBlocks = blocks
			}
			encoded, err := json.Marshal(doc)
			if err != nil {
				t.Fatal(err)
			}
			var wire map[string]json.RawMessage
			if err := json.Unmarshal(encoded, &wire); err != nil {
				t.Fatal(err)
			}
			for _, key := range []string{"border", "border_block", "border_tiles", "border_schema", "border_direction_width", "border_direction_height"} {
				if _, ok := wire[key]; !ok {
					t.Fatalf("missing JSON key %s", key)
				}
			}
			key := "border_directions"
			if representation == "blocks" {
				key = "border_direction_blocks"
			}
			if _, ok := wire[key]; !ok {
				t.Fatalf("missing JSON key %s", key)
			}
			var reopened PermissionDoc
			if err := json.Unmarshal(encoded, &reopened); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(doc, reopened) {
				t.Fatal("round-trip changed legacy or current fields")
			}
			loadBorderBlock(reopened)
			gotFlat, gotBlocks := directionalBorderDocValues()
			if !reflect.DeepEqual(gotFlat, flat) || !reflect.DeepEqual(gotBlocks, blocks) {
				t.Fatal("editor border consumer changed tiles")
			}
		})
	}
	var legacy PermissionDoc
	if err := json.Unmarshal([]byte(`{"version":1,"border":[1,2,3,4],"cells":{"0,0":"0"}}`), &legacy); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &wire); err != nil {
		t.Fatal(err)
	}
	if _, ok := wire["border_schema"]; ok {
		t.Fatal("legacy document acquired directional schema")
	}
}
