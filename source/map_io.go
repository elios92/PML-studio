//go:build windows

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"unsafe"
)

type mapTableJSON struct {
	Class      string                     `json:"_class,omitempty"`
	Dimensions int                        `json:"dimensions,omitempty"`
	Width      int                        `json:"width"`
	Height     int                        `json:"height"`
	Depth      int                        `json:"depth"`
	Layers     [][][]int                  `json:"layers"`
	Extra      map[string]json.RawMessage `json:"-"`
}

type MapDocument struct {
	Path      string
	Raw       map[string]json.RawMessage
	Table     mapTableJSON
	TilesetID int
}

var currentMapDoc *MapDocument

func ensureThreeMapLayers(t *mapTableJSON) {
	if t == nil || t.Width <= 0 || t.Height <= 0 {
		return
	}
	for len(t.Layers) < 3 {
		layer := make([][]int, t.Height)
		for y := 0; y < t.Height; y++ {
			layer[y] = make([]int, t.Width)
		}
		t.Layers = append(t.Layers, layer)
	}
	if t.Depth < 3 {
		t.Depth = 3
	}
}

func applyPLMLayerRoleMetadata(doc *MapDocument) {
	if doc == nil || doc.Raw == nil {
		return
	}
	meta := map[string]any{
		"version": 1,
		"layers": []map[string]any{
			{"index": 1, "role": "below_player", "label": "Sotto il giocatore"},
			{"index": 2, "role": "above_player", "label": "Sopra il giocatore"},
			{"index": 3, "role": "overhead", "label": "Copertura superiore / ponti"},
		},
	}
	if b, err := json.Marshal(meta); err == nil {
		doc.Raw["plm_layer_roles"] = b
	}
}

func decodeTablePreserve(raw json.RawMessage) (mapTableJSON, error) {
	var all map[string]json.RawMessage
	if err := json.Unmarshal(raw, &all); err != nil {
		return mapTableJSON{}, err
	}
	var t mapTableJSON
	_ = json.Unmarshal(all["_class"], &t.Class)
	_ = json.Unmarshal(all["dimensions"], &t.Dimensions)
	_ = json.Unmarshal(all["width"], &t.Width)
	_ = json.Unmarshal(all["height"], &t.Height)
	_ = json.Unmarshal(all["depth"], &t.Depth)
	if err := json.Unmarshal(all["layers"], &t.Layers); err != nil {
		return mapTableJSON{}, fmt.Errorf("layers non valide: %w", err)
	}
	t.Extra = make(map[string]json.RawMessage)
	for k, v := range all {
		switch k {
		case "_class", "dimensions", "width", "height", "depth", "layers":
		default:
			t.Extra[k] = v
		}
	}
	return t, nil
}

func encodeTablePreserve(t mapTableJSON) (json.RawMessage, error) {
	all := make(map[string]json.RawMessage, len(t.Extra)+6)
	for k, v := range t.Extra {
		all[k] = v
	}
	put := func(k string, v any) error {
		b, err := json.Marshal(v)
		if err == nil {
			all[k] = b
		}
		return err
	}
	if t.Class != "" {
		if err := put("_class", t.Class); err != nil {
			return nil, err
		}
	}
	if err := put("dimensions", t.Dimensions); err != nil {
		return nil, err
	}
	if err := put("width", t.Width); err != nil {
		return nil, err
	}
	if err := put("height", t.Height); err != nil {
		return nil, err
	}
	if err := put("depth", t.Depth); err != nil {
		return nil, err
	}
	if err := put("layers", t.Layers); err != nil {
		return nil, err
	}
	b, err := json.Marshal(all)
	return json.RawMessage(b), err
}

func validateMapLayers(t mapTableJSON) error {
	if t.Width <= 0 || t.Height <= 0 {
		return fmt.Errorf("dimensioni mappa non valide: %dx%d", t.Width, t.Height)
	}
	if t.Depth <= 0 {
		t.Depth = len(t.Layers)
	}
	if len(t.Layers) == 0 {
		return fmt.Errorf("la mappa non contiene layer")
	}
	// Alcune mappe convertite/tecniche possono avere meno di tre layer.
	// Il renderer è già in grado di comporre fino a 3 layer disponibili;
	// non scartiamo quindi una mappa valida solo perché depth < 3.
	for z, layer := range t.Layers {
		if len(layer) != t.Height {
			return fmt.Errorf("layer %d: altezza %d, attesa %d", z+1, len(layer), t.Height)
		}
		for y, row := range layer {
			if len(row) != t.Width {
				return fmt.Errorf("layer %d riga %d: larghezza %d, attesa %d", z+1, y, len(row), t.Width)
			}
		}
	}
	return nil
}

func loadMapDocument(path string) (*MapDocument, error) {
	done := diagEnterWorker("loadMapJSON()")
	defer done()
	diagLogf("[WORKER] loadMapJSON path=%s", path)
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var raw map[string]json.RawMessage
	if err := dec.Decode(&raw); err != nil {
		return nil, fmt.Errorf("JSON mappa non valido: %w", err)
	}
	dataRaw, ok := raw["data"]
	if !ok {
		return nil, fmt.Errorf("campo data assente")
	}
	table, err := decodeTablePreserve(dataRaw)
	if err != nil {
		return nil, fmt.Errorf("campo data: %w", err)
	}
	ensureThreeMapLayers(&table)
	if err := validateMapLayers(table); err != nil {
		return nil, err
	}
	var tilesetID int
	if r, ok := raw["tileset_id"]; ok {
		_ = json.Unmarshal(r, &tilesetID)
	}
	return &MapDocument{Path: path, Raw: raw, Table: table, TilesetID: tilesetID}, nil
}

func saveMapDocument(doc *MapDocument) error {
	if doc == nil || doc.Path == "" {
		return fmt.Errorf("nessuna mappa caricata")
	}
	ensureThreeMapLayers(&doc.Table)
	applyPLMLayerRoleMetadata(doc)
	if err := validateMapLayers(doc.Table); err != nil {
		return err
	}
	dataRaw, err := encodeTablePreserve(doc.Table)
	if err != nil {
		return err
	}
	doc.Raw["data"] = dataRaw
	out, err := json.MarshalIndent(doc.Raw, "", "  ")
	if err != nil {
		return err
	}
	out = append(out, '\n')

	bak := doc.Path + ".bak"
	if _, err := os.Stat(bak); os.IsNotExist(err) {
		original, readErr := os.ReadFile(doc.Path)
		if readErr == nil {
			_ = os.WriteFile(bak, original, 0644)
		}
	}

	tmp := doc.Path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		_ = f.Close()
		if !ok {
			_ = os.Remove(tmp)
		}
	}()
	if _, err = f.Write(out); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}

	// Sostituzione atomica lato Windows. Il file temporaneo è già stato
	// scritto e flushato: MoveFileExW sostituisce il JSON reale senza
	// lasciare una finestra in cui MapXXX.json manca.
	r, _, callErr := pMoveFileExW.Call(
		uintptr(unsafe.Pointer(wstr(tmp))),
		uintptr(unsafe.Pointer(wstr(doc.Path))),
		MOVEFILE_REPLACE_EXISTING|MOVEFILE_WRITE_THROUGH,
	)
	if r == 0 {
		return fmt.Errorf("sostituzione atomica fallita: %v", callErr)
	}
	ok = true
	return nil
}

func mapBackupPath(path string) string {
	return filepath.Clean(path) + ".bak"
}
