//go:build windows

package main

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestPatchNativeEventRawPreservesPagePayload(t *testing.T) {
	original := json.RawMessage(`{
		"_class":"RPG::Event",
		"id":7,
		"name":"NPC",
		"x":4,
		"y":5,
		"pages":[{
			"condition":{"switch1_valid":true,"switch1_id":12},
			"graphic":{"character_name":"hero"},
			"list":[{"code":101,"indent":0,"parameters":["Test"]},{"code":0,"indent":0,"parameters":[]}]
		}],
		"plugin_custom":{"keep":true,"values":[1,2,3]}
	}`)
	e := EditorEvent{ID: 99, Name: "NPC - copia", X: 10, Y: 11, Native: true, NativeRaw: original}
	patched := patchNativeEventRaw(e)
	var before, after map[string]any
	if err := json.Unmarshal(original, &before); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(patched, &after); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"id", "name", "x", "y"} {
		delete(before, key)
		delete(after, key)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("duplicating/renaming an event changed its page/custom payload\nbefore=%#v\nafter=%#v", before, after)
	}
}
