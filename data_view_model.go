package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type dataPBSRecord struct {
	ID         string
	HeaderLine int
	StartLine  int
	EndLine    int
}

type dataPBSField struct {
	Line     int
	Key      string
	Value    string
	Indent   string
	Raw      bool
	IsHeader bool
}

type dataPBSDocument struct {
	Path    string
	Lines   []string
	Records []dataPBSRecord
	Newline string
	HadBOM  bool
	Dirty   bool
}

func loadDataPBSDocument(path string) (*dataPBSDocument, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	doc := &dataPBSDocument{Path: path, Newline: "\n"}
	if len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF {
		doc.HadBOM = true
		b = b[3:]
	}
	s := string(b)
	if strings.Contains(s, "\r\n") {
		doc.Newline = "\r\n"
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	doc.Lines = strings.Split(s, "\n")
	doc.reparse()
	return doc, nil
}

// parseDataPBSHeaderLine recognizes the section syntax used by Essentials v20.1,
// including headers with an inline comment such as "[005] # Route 1".  Prefix
// and suffix are returned so an ID edit can preserve spacing/comments byte-for-byte.
func parseDataPBSHeaderLine(line string) (id, prefix, suffix string, ok bool) {
	open := strings.Index(line, "[")
	if open < 0 || strings.TrimSpace(line[:open]) != "" {
		return "", "", "", false
	}
	closeRel := strings.Index(line[open+1:], "]")
	if closeRel < 0 {
		return "", "", "", false
	}
	close := open + 1 + closeRel
	id = strings.TrimSpace(line[open+1 : close])
	if id == "" {
		return "", "", "", false
	}
	rest := strings.TrimSpace(line[close+1:])
	if rest != "" && !strings.HasPrefix(rest, "#") {
		return "", "", "", false
	}
	return id, line[:open], line[close+1:], true
}

func replaceDataPBSHeaderID(line, id string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", fmt.Errorf("l'ID interno non può essere vuoto")
	}
	_, prefix, suffix, ok := parseDataPBSHeaderLine(line)
	if !ok {
		return "", fmt.Errorf("intestazione PBS non valida")
	}
	return prefix + "[" + id + "]" + suffix, nil
}

func (d *dataPBSDocument) reparse() {
	d.Records = nil
	starts := []int{}
	ids := []string{}
	for i, line := range d.Lines {
		id, _, _, ok := parseDataPBSHeaderLine(line)
		if ok {
			starts = append(starts, i)
			ids = append(ids, id)
		}
	}
	for i, start := range starts {
		end := len(d.Lines)
		if i+1 < len(starts) {
			end = starts[i+1]
		}
		d.Records = append(d.Records, dataPBSRecord{ID: ids[i], HeaderLine: start, StartLine: start, EndLine: end})
	}
}

func (d *dataPBSDocument) fields(recordIndex int) []dataPBSField {
	if recordIndex < 0 || recordIndex >= len(d.Records) {
		return nil
	}
	r := d.Records[recordIndex]
	out := []dataPBSField{{Line: r.HeaderLine, Key: "ID", Value: r.ID, IsHeader: true}}
	for i := r.HeaderLine + 1; i < r.EndLine && i < len(d.Lines); i++ {
		line := d.Lines[i]
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		indentLen := len(line) - len(strings.TrimLeft(line, " \t"))
		indent := ""
		if indentLen > 0 {
			indent = line[:indentLen]
		}
		if eq := strings.Index(trimmed, "="); eq >= 0 {
			key := strings.TrimSpace(trimmed[:eq])
			value := strings.TrimSpace(trimmed[eq+1:])
			out = append(out, dataPBSField{Line: i, Key: key, Value: value, Indent: indent})
		} else {
			out = append(out, dataPBSField{Line: i, Key: "", Value: trimmed, Indent: indent, Raw: true})
		}
	}
	return out
}

func (d *dataPBSDocument) recordAllowsFlatFieldStructure(recordIndex int) bool {
	if d == nil || recordIndex < 0 || recordIndex >= len(d.Records) {
		return false
	}
	for _, f := range d.fields(recordIndex) {
		if f.IsHeader {
			continue
		}
		if f.Raw || f.Indent != "" {
			return false
		}
	}
	return true
}

func (d *dataPBSDocument) recordDisplayName(index int) string {
	if index < 0 || index >= len(d.Records) {
		return ""
	}
	r := d.Records[index]
	for _, f := range d.fields(index) {
		if strings.EqualFold(f.Key, "Name") && strings.TrimSpace(f.Value) != "" {
			return r.ID + " — " + strings.TrimSpace(f.Value)
		}
	}
	return r.ID
}

func (d *dataPBSDocument) setField(recordIndex int, field dataPBSField, newKey, newValue string, allowKeyChange bool) error {
	if recordIndex < 0 || recordIndex >= len(d.Records) {
		return fmt.Errorf("record non valido")
	}
	if field.Line < 0 || field.Line >= len(d.Lines) {
		return fmt.Errorf("campo non valido")
	}
	newValue = strings.TrimSpace(newValue)
	if field.IsHeader {
		if newValue == "" {
			return fmt.Errorf("l'ID interno non può essere vuoto")
		}
		for i, r := range d.Records {
			if i != recordIndex && strings.EqualFold(strings.TrimSpace(r.ID), newValue) {
				return fmt.Errorf("esiste già un record con ID %s", newValue)
			}
		}
		replaced, err := replaceDataPBSHeaderID(d.Lines[field.Line], newValue)
		if err != nil {
			return err
		}
		d.Lines[field.Line] = replaced
	} else if field.Raw {
		d.Lines[field.Line] = field.Indent + newValue
	} else {
		key := field.Key
		if allowKeyChange && strings.TrimSpace(newKey) != "" {
			key = strings.TrimSpace(newKey)
		}
		if key == "" {
			return fmt.Errorf("nome campo non valido")
		}
		d.Lines[field.Line] = field.Indent + key + " = " + newValue
	}
	d.Dirty = true
	d.reparse()
	return nil
}

func (d *dataPBSDocument) addField(recordIndex int, key, value string) error {
	if recordIndex < 0 || recordIndex >= len(d.Records) {
		return fmt.Errorf("seleziona prima un record")
	}
	key = strings.TrimSpace(key)
	value = strings.TrimSpace(value)
	if key == "" && value == "" {
		return fmt.Errorf("inserisci un campo o un valore")
	}
	r := d.Records[recordIndex]
	insertAt := r.EndLine
	// I separatori/commenti tra record restano dopo il nuovo campo.
	for insertAt > r.HeaderLine+1 {
		t := strings.TrimSpace(d.Lines[insertAt-1])
		if t == "" || strings.HasPrefix(t, "#") {
			insertAt--
			continue
		}
		break
	}
	line := value
	if key != "" {
		line = key + " = " + value
	}
	d.Lines = append(d.Lines, "")
	copy(d.Lines[insertAt+1:], d.Lines[insertAt:])
	d.Lines[insertAt] = line
	d.Dirty = true
	d.reparse()
	return nil
}

func (d *dataPBSDocument) deleteField(field dataPBSField) error {
	if field.IsHeader {
		return fmt.Errorf("l'ID del record non può essere eliminato; elimina il record")
	}
	if field.Line < 0 || field.Line >= len(d.Lines) {
		return fmt.Errorf("campo non valido")
	}
	d.Lines = append(d.Lines[:field.Line], d.Lines[field.Line+1:]...)
	d.Dirty = true
	d.reparse()
	return nil
}

func (d *dataPBSDocument) addRecord(id string) (int, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return -1, fmt.Errorf("inserisci l'ID del nuovo record")
	}
	for _, r := range d.Records {
		if strings.EqualFold(r.ID, id) {
			return -1, fmt.Errorf("esiste già un record con ID %s", id)
		}
	}
	if len(d.Lines) > 0 && strings.TrimSpace(d.Lines[len(d.Lines)-1]) != "" {
		d.Lines = append(d.Lines, "")
	}
	d.Lines = append(d.Lines, "#-------------------------------", "["+id+"]", "")
	d.Dirty = true
	d.reparse()
	for i := range d.Records {
		if strings.EqualFold(d.Records[i].ID, id) {
			return i, nil
		}
	}
	return len(d.Records) - 1, nil
}

func (d *dataPBSDocument) duplicateRecord(recordIndex int, id string) (int, error) {
	if recordIndex < 0 || recordIndex >= len(d.Records) {
		return -1, fmt.Errorf("record non valido")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return -1, fmt.Errorf("inserisci l'ID del nuovo record")
	}
	for _, r := range d.Records {
		if strings.EqualFold(r.ID, id) {
			return -1, fmt.Errorf("esiste già un record con ID %s", id)
		}
	}
	r := d.Records[recordIndex]
	end := r.EndLine
	// The canonical separator belongs to the following record.  Excluding it
	// keeps a duplicate self-contained while preserving every real record line,
	// comment and indentation exactly.
	for end > r.HeaderLine+1 {
		t := strings.TrimSpace(d.Lines[end-1])
		if t == "" || t == "#-------------------------------" {
			end--
			continue
		}
		break
	}
	block := append([]string(nil), d.Lines[r.HeaderLine:end]...)
	if len(block) == 0 {
		return -1, fmt.Errorf("record vuoto")
	}
	header, err := replaceDataPBSHeaderID(block[0], id)
	if err != nil {
		return -1, err
	}
	block[0] = header
	if len(d.Lines) > 0 && strings.TrimSpace(d.Lines[len(d.Lines)-1]) != "" {
		d.Lines = append(d.Lines, "")
	}
	d.Lines = append(d.Lines, "#-------------------------------")
	d.Lines = append(d.Lines, block...)
	d.Lines = append(d.Lines, "")
	d.Dirty = true
	d.reparse()
	for i := range d.Records {
		if strings.EqualFold(d.Records[i].ID, id) {
			return i, nil
		}
	}
	return -1, fmt.Errorf("duplicazione record non riuscita")
}

func (d *dataPBSDocument) deleteRecord(recordIndex int) error {
	if recordIndex < 0 || recordIndex >= len(d.Records) {
		return fmt.Errorf("record non valido")
	}
	r := d.Records[recordIndex]
	start := r.HeaderLine
	// Se il separatore immediatamente precedente appartiene al record, rimuovilo.
	for start > 0 {
		t := strings.TrimSpace(d.Lines[start-1])
		if t == "#-------------------------------" || t == "" {
			start--
			continue
		}
		break
	}
	end := r.EndLine
	if end < start {
		end = start
	}
	d.Lines = append(d.Lines[:start], d.Lines[end:]...)
	d.Dirty = true
	d.reparse()
	return nil
}

func (d *dataPBSDocument) pbsBytes() []byte {
	s := strings.Join(d.Lines, d.Newline)
	b := []byte(s)
	if d.HadBOM {
		b = append([]byte{0xEF, 0xBB, 0xBF}, b...)
	}
	return b
}

type dataAtomicWrite struct {
	Target string
	Data   []byte
	Mode   os.FileMode
	Tmp    string
	Bak    string
	HadOld bool
}

// writeDataAtomicSet commits all files as one small transaction.  Either every
// target is replaced, or all original targets are restored.  This prevents the
// PBS and its visual JSON mirror from diverging after a partial save failure.
func writeDataAtomicSet(writes []dataAtomicWrite) error {
	if len(writes) == 0 {
		return nil
	}
	for i := range writes {
		w := &writes[i]
		if strings.TrimSpace(w.Target) == "" {
			return fmt.Errorf("percorso di salvataggio non valido")
		}
		if w.Mode == 0 {
			w.Mode = 0644
		}
		if err := os.MkdirAll(filepath.Dir(w.Target), 0755); err != nil {
			return err
		}
		w.Tmp = w.Target + ".plm.tmp"
		w.Bak = w.Target + ".plm.bak"
		_ = os.Remove(w.Tmp)
		_ = os.Remove(w.Bak)
		if err := os.WriteFile(w.Tmp, w.Data, w.Mode); err != nil {
			for j := 0; j <= i; j++ {
				_ = os.Remove(writes[j].Tmp)
			}
			return err
		}
	}

	// First move every old target out of the way.  No new target is visible yet.
	for i := range writes {
		w := &writes[i]
		if _, err := os.Stat(w.Target); err == nil {
			if err := os.Rename(w.Target, w.Bak); err != nil {
				for j := 0; j < i; j++ {
					if writes[j].HadOld {
						_ = os.Rename(writes[j].Bak, writes[j].Target)
					}
				}
				for j := range writes {
					_ = os.Remove(writes[j].Tmp)
				}
				return err
			}
			w.HadOld = true
		} else if !os.IsNotExist(err) {
			for j := 0; j < i; j++ {
				if writes[j].HadOld {
					_ = os.Rename(writes[j].Bak, writes[j].Target)
				}
			}
			for j := range writes {
				_ = os.Remove(writes[j].Tmp)
			}
			return err
		}
	}

	committed := 0
	for i := range writes {
		w := &writes[i]
		if err := os.Rename(w.Tmp, w.Target); err != nil {
			// Remove newly committed files, then restore all originals.
			for j := 0; j < committed; j++ {
				_ = os.Remove(writes[j].Target)
			}
			for j := range writes {
				if writes[j].HadOld {
					_ = os.Rename(writes[j].Bak, writes[j].Target)
				}
				_ = os.Remove(writes[j].Tmp)
			}
			return err
		}
		committed++
	}
	for i := range writes {
		_ = os.Remove(writes[i].Bak)
	}
	return nil
}

func (d *dataPBSDocument) save() error {
	if d.Path == "" {
		return fmt.Errorf("percorso dati non valido")
	}
	writes := []dataAtomicWrite{{Target: d.Path, Data: d.pbsBytes(), Mode: 0644}}
	if currentProject != "" {
		mirrorPath, mirrorBytes, err := dataPBSJSONMirrorPayload(d)
		if err != nil {
			return err
		}
		writes = append(writes, dataAtomicWrite{Target: mirrorPath, Data: mirrorBytes, Mode: 0644})
	}
	if err := writeDataAtomicSet(writes); err != nil {
		return err
	}
	d.Dirty = false
	return nil
}

type dataJSONField struct {
	Key    string `json:"key,omitempty"`
	Value  string `json:"value"`
	Raw    bool   `json:"raw,omitempty"`
	Indent string `json:"indent,omitempty"`
}
type dataJSONRecord struct {
	ID     string          `json:"id"`
	Fields []dataJSONField `json:"fields"`
}
type dataJSONMirror struct {
	Version int              `json:"version"`
	Schema  string           `json:"schema"`
	Source  string           `json:"source"`
	Records []dataJSONRecord `json:"records"`
}

func dataPBSJSONMirrorPayload(d *dataPBSDocument) (string, []byte, error) {
	if currentProject == "" {
		return "", nil, nil
	}
	out := dataJSONMirror{Version: 1, Schema: "plm.pbs.visual.v1", Source: filepath.Base(d.Path)}
	for i, r := range d.Records {
		rec := dataJSONRecord{ID: r.ID}
		for _, f := range d.fields(i) {
			if f.IsHeader {
				continue
			}
			rec.Fields = append(rec.Fields, dataJSONField{Key: f.Key, Value: f.Value, Raw: f.Raw, Indent: f.Indent})
		}
		out.Records = append(out.Records, rec)
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return "", nil, err
	}
	b = append(b, '\n')
	dir := filepath.Join(currentProject, "converted", "data", "pbs")
	name := strings.TrimSuffix(filepath.Base(d.Path), filepath.Ext(d.Path)) + ".json"
	return filepath.Join(dir, name), b, nil
}

func writeDataPBSJSONMirror(d *dataPBSDocument) error {
	path, b, err := dataPBSJSONMirrorPayload(d)
	if err != nil || path == "" {
		return err
	}
	return writeDataAtomicSet([]dataAtomicWrite{{Target: path, Data: b, Mode: 0644}})
}

func dataPBSRoot(project string) string { return projectPBSBaseRoot(project) }

// dataPBSFilePath always resolves against the project's canonical PBS tree.
// Generation-specific Essentials bundles are references for compatibility and
// must never silently replace imported/customized project data in Vista Dati.
func dataPBSFilePath(project, name string) string {
	state := loadMechanicsSettings(project)
	return mechanicsActivePBSFile(project, state.MechanicsGeneration, name)
}

func discoverDataPBSFiles(project string) []string {
	base := dataPBSRoot(project)
	entries, err := os.ReadDir(base)
	if err != nil {
		return nil
	}
	seen := map[string]string{}
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".txt") {
			continue
		}
		key := strings.ToLower(e.Name())
		if _, exists := seen[key]; !exists {
			seen[key] = e.Name()
		}
	}
	out := make([]string, 0, len(seen))
	for _, name := range seen {
		out = append(out, name)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i]) < strings.ToLower(out[j]) })
	return out
}

func readDataPBSLineCount(path string) int {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	n := 0
	for s.Scan() {
		n++
	}
	return n
}
