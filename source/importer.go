//go:build windows

package main

import (
	"bytes"
	"compress/zlib"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// EssentialsImportReport is written into the converted project so a conversion
// can always be audited later without touching the original RMXP project.
type EssentialsImportReport struct {
	Source                  string   `json:"source"`
	Destination             string   `json:"destination"`
	StartedAt               string   `json:"started_at"`
	CompletedAt             string   `json:"completed_at"`
	ProjectName             string   `json:"project_name"`
	ProjectType             string   `json:"project_type"`
	ReferenceProfile        string   `json:"reference_profile"`
	DetectedVersion         string   `json:"detected_version"`
	SourceClassification    string   `json:"source_classification"`
	BaselineNameMatch       float64  `json:"baseline_name_match_percent"`
	BaselineExactMatch      float64  `json:"baseline_exact_match_percent"`
	PBSReferenceProfile     string   `json:"pbs_reference_profile"`
	PBSBaselineStatus       string   `json:"pbs_baseline_status"`
	PBSBaselineExactMatch   float64  `json:"pbs_baseline_exact_match_percent"`
	PBSExactFiles           int      `json:"pbs_exact_files"`
	PBSModifiedFiles        []string `json:"pbs_modified_files,omitempty"`
	PBSMissingFiles         []string `json:"pbs_missing_files,omitempty"`
	PBSAddedFiles           []string `json:"pbs_added_files,omitempty"`
	PBSCopyVerified         bool     `json:"pbs_copy_verified_1_to_1"`
	MapsConverted           int      `json:"maps_converted"`
	ConnectionsConverted    int      `json:"connections_converted"`
	RegionsCreated          int      `json:"regions_created"`
	RegionalMapsOrganized   int      `json:"regional_maps_organized"`
	DataFilesConverted      int      `json:"data_files_converted"`
	CustomDataFiles         int      `json:"custom_data_files"`
	RubyScriptsExtracted    int      `json:"ruby_scripts_extracted"`
	CoreScriptsMatched      int      `json:"core_scripts_matched"`
	CoreScriptsModified     int      `json:"core_scripts_modified"`
	CustomScripts           int      `json:"custom_scripts"`
	PluginScriptsExtracted  int      `json:"plugin_scripts_extracted"`
	PluginSourceFilesCopied int      `json:"plugin_source_files_copied"`
	AssetsCopied            []string `json:"assets_copied"`
	CustomPBSFiles          []string `json:"custom_pbs_files,omitempty"`
	Warnings                []string `json:"warnings,omitempty"`
}

// EssentialsImportProgress describes one visible phase of the Essentials -> PLM
// conversion. Percent is intentionally monotonic and approximate: the importer
// prefers a responsive, truthful progress indicator over pretending that every
// source file has the same cost.
type EssentialsImportProgress struct {
	Percent int
	Phase   string
	Detail  string
}

type EssentialsImportProgressFunc func(EssentialsImportProgress)

func emitEssentialsImportProgress(cb EssentialsImportProgressFunc, percent int, phase, detail string) {
	if cb == nil {
		return
	}
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	cb(EssentialsImportProgress{Percent: percent, Phase: phase, Detail: detail})
}

type scriptAnalysis struct {
	DetectedVersion    string  `json:"detected_version"`
	ReferenceProfile   string  `json:"reference_profile"`
	NameMatchPercent   float64 `json:"name_match_percent"`
	ExactMatchPercent  float64 `json:"exact_match_percent"`
	Matched            int     `json:"matched"`
	Modified           int     `json:"modified"`
	Custom             int     `json:"custom"`
	SubstantiveScripts int     `json:"substantive_scripts"`
	BaselineScripts    int     `json:"baseline_scripts"`
	ExactArchive       bool    `json:"exact_archive"`
}

type scriptExtractionSummary struct {
	Total    int
	Matched  int
	Modified int
	Custom   int
}

func validateEssentialsProject(root string) error {
	root = filepath.Clean(root)
	if root == "" || !isDir(root) {
		return fmt.Errorf("cartella progetto non valida")
	}
	data := filepath.Join(root, "Data")
	if !isDir(data) {
		return fmt.Errorf("cartella Data non trovata")
	}
	if !exists(filepath.Join(data, "MapInfos.rxdata")) {
		return fmt.Errorf("Data\\MapInfos.rxdata non trovato: il progetto non sembra RPG Maker XP/Pokémon Essentials")
	}
	maps, _ := filepath.Glob(filepath.Join(data, "Map*.rxdata"))
	filtered := 0
	mapRE := regexp.MustCompile(`(?i)^Map\d+\.rxdata$`)
	for _, p := range maps {
		if mapRE.MatchString(filepath.Base(p)) {
			filtered++
		}
	}
	if filtered == 0 {
		return fmt.Errorf("nessuna MapXXX.rxdata trovata nella cartella Data")
	}
	if !isDir(filepath.Join(root, "Graphics")) {
		return fmt.Errorf("cartella Graphics non trovata")
	}
	if !exists(filepath.Join(data, "Scripts.rxdata")) {
		return fmt.Errorf("Data\\Scripts.rxdata non trovato: serve un progetto Pokémon Essentials completo")
	}
	pbs := filepath.Join(root, "PBS")
	if !isDir(pbs) || !exists(filepath.Join(pbs, "pokemon.txt")) || !exists(filepath.Join(pbs, "items.txt")) || !exists(filepath.Join(pbs, "moves.txt")) {
		return fmt.Errorf("PBS base non trovato o incompleto: il progetto non sembra Pokémon Essentials v20.1")
	}
	return nil
}

func destinationLooksNonEmpty(root string) bool {
	entries, err := os.ReadDir(root)
	return err == nil && len(entries) > 0
}

func copyTree(src, dst string) error {
	if !isDir(src) {
		return nil
	}
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
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
		info, err := d.Info()
		if err != nil {
			return err
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		tmp := target + ".plm_import_tmp"
		out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode().Perm())
		if err != nil {
			return err
		}
		_, cpErr := io.Copy(out, in)
		syncErr := out.Sync()
		closeErr := out.Close()
		if cpErr != nil {
			_ = os.Remove(tmp)
			return cpErr
		}
		if syncErr != nil {
			_ = os.Remove(tmp)
			return syncErr
		}
		if closeErr != nil {
			_ = os.Remove(tmp)
			return closeErr
		}
		_ = os.Remove(target)
		return os.Rename(tmp, target)
	})
}

func copyTreeWithProgress(src, dst string, startPercent, endPercent int, label string, cb EssentialsImportProgressFunc) error {
	if !isDir(src) {
		return nil
	}
	total := countFilesRecursive(src)
	if total <= 0 {
		emitEssentialsImportProgress(cb, endPercent, label, "Nessun file da copiare")
		return os.MkdirAll(dst, 0755)
	}
	copied := 0
	lastShownPercent := -1
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
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
		if err := copyFileAtomic(path, target); err != nil {
			return err
		}
		copied++
		span := endPercent - startPercent
		percent := startPercent
		if span > 0 {
			percent += (copied * span) / total
		}
		// Avoid flooding the Win32 message queue with thousands of tiny updates,
		// but always expose the first/last file and every visible percentage step.
		if copied == 1 || copied == total || percent != lastShownPercent {
			lastShownPercent = percent
			emitEssentialsImportProgress(cb, percent, label, filepath.ToSlash(rel))
		}
		return nil
	})
}

func copyFileAtomic(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	tmp := dst + ".plm_import_tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	syncErr := out.Sync()
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return copyErr
	}
	if syncErr != nil {
		_ = os.Remove(tmp)
		return syncErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return closeErr
	}
	_ = os.Remove(dst)
	return os.Rename(tmp, dst)
}

// --- Ruby Marshal 4.8 reader -------------------------------------------------
// RPG Maker XP .rxdata is Ruby Marshal data. The reader intentionally handles
// the complete subset used by normal RMXP/Essentials data files, preserving
// unknown user-defined payloads instead of discarding them.

type rubyObject struct {
	Class string
	IVars map[string]any
}

type rubyUserData struct {
	Class string
	Data  []byte
}

type rubyHashEntry struct{ Key, Value any }
type rubyHash struct {
	Entries    []rubyHashEntry
	Default    any
	HasDefault bool
}

type rubyMarshalReader struct {
	data    []byte
	pos     int
	objects []any
	symbols []string
}

func (r *rubyMarshalReader) readByte() (byte, error) {
	if r.pos >= len(r.data) {
		return 0, io.ErrUnexpectedEOF
	}
	b := r.data[r.pos]
	r.pos++
	return b, nil
}

func (r *rubyMarshalReader) readN(n int) ([]byte, error) {
	if n < 0 || r.pos+n > len(r.data) {
		return nil, io.ErrUnexpectedEOF
	}
	b := r.data[r.pos : r.pos+n]
	r.pos += n
	return b, nil
}

func (r *rubyMarshalReader) readLong() (int, error) {
	b, err := r.readByte()
	if err != nil {
		return 0, err
	}
	c := int(int8(b))
	switch {
	case c == 0:
		return 0, nil
	case c >= 5:
		return c - 5, nil
	case c <= -5:
		return c + 5, nil
	}
	n := c
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	raw, err := r.readN(n)
	if err != nil {
		return 0, err
	}
	v := int64(0)
	for i := 0; i < n; i++ {
		v |= int64(raw[i]) << (8 * i)
	}
	if neg {
		for i := n; i < 8; i++ {
			v |= int64(0xff) << (8 * i)
		}
	}
	return int(v), nil
}

func (r *rubyMarshalReader) readRawString() ([]byte, error) {
	n, err := r.readLong()
	if err != nil {
		return nil, err
	}
	return r.readN(n)
}

func (r *rubyMarshalReader) readSymbolRef() (string, error) {
	tag, err := r.readByte()
	if err != nil {
		return "", err
	}
	switch tag {
	case ':':
		raw, err := r.readRawString()
		if err != nil {
			return "", err
		}
		s := string(raw)
		r.symbols = append(r.symbols, s)
		return s, nil
	case ';':
		idx, err := r.readLong()
		if err != nil {
			return "", err
		}
		if idx < 0 || idx >= len(r.symbols) {
			return "", fmt.Errorf("symbol link fuori range: %d", idx)
		}
		return r.symbols[idx], nil
	default:
		return "", fmt.Errorf("atteso simbolo Ruby, trovato %q", tag)
	}
}

func (r *rubyMarshalReader) addObject(v any) int {
	r.objects = append(r.objects, v)
	return len(r.objects) - 1
}

func (r *rubyMarshalReader) readValue() (any, error) {
	tag, err := r.readByte()
	if err != nil {
		return nil, err
	}
	switch tag {
	case '0':
		return nil, nil
	case 'T':
		return true, nil
	case 'F':
		return false, nil
	case 'i':
		return r.readLong()
	case ':':
		raw, err := r.readRawString()
		if err != nil {
			return nil, err
		}
		s := string(raw)
		r.symbols = append(r.symbols, s)
		return map[string]any{"_symbol": s}, nil
	case ';':
		idx, err := r.readLong()
		if err != nil {
			return nil, err
		}
		if idx < 0 || idx >= len(r.symbols) {
			return nil, fmt.Errorf("symbol link fuori range: %d", idx)
		}
		return map[string]any{"_symbol": r.symbols[idx]}, nil
	case '@':
		idx, err := r.readLong()
		if err != nil {
			return nil, err
		}
		if idx < 0 || idx >= len(r.objects) {
			return nil, fmt.Errorf("object link fuori range: %d", idx)
		}
		return r.objects[idx], nil
	case '"':
		raw, err := r.readRawString()
		if err != nil {
			return nil, err
		}
		s := string(raw)
		r.addObject(s)
		return s, nil
	case 'f':
		raw, err := r.readRawString()
		if err != nil {
			return nil, err
		}
		f, _ := strconv.ParseFloat(string(raw), 64)
		r.addObject(f)
		return f, nil
	case '[':
		n, err := r.readLong()
		if err != nil {
			return nil, err
		}
		a := make([]any, n)
		r.addObject(a)
		for i := range a {
			a[i], err = r.readValue()
			if err != nil {
				return nil, err
			}
		}
		return a, nil
	case '{', '}':
		n, err := r.readLong()
		if err != nil {
			return nil, err
		}
		h := &rubyHash{Entries: make([]rubyHashEntry, 0, n)}
		r.addObject(h)
		for i := 0; i < n; i++ {
			k, e := r.readValue()
			if e != nil {
				return nil, e
			}
			v, e := r.readValue()
			if e != nil {
				return nil, e
			}
			h.Entries = append(h.Entries, rubyHashEntry{k, v})
		}
		if tag == '}' {
			h.Default, err = r.readValue()
			h.HasDefault = true
			if err != nil {
				return nil, err
			}
		}
		return h, nil
	case 'o':
		cls, err := r.readSymbolRef()
		if err != nil {
			return nil, err
		}
		obj := &rubyObject{Class: cls, IVars: map[string]any{}}
		r.addObject(obj)
		n, err := r.readLong()
		if err != nil {
			return nil, err
		}
		for i := 0; i < n; i++ {
			k, e := r.readSymbolRef()
			if e != nil {
				return nil, e
			}
			v, e := r.readValue()
			if e != nil {
				return nil, e
			}
			obj.IVars[strings.TrimPrefix(k, "@")] = v
		}
		return obj, nil
	case 'S':
		cls, err := r.readSymbolRef()
		if err != nil {
			return nil, err
		}
		obj := &rubyObject{Class: cls, IVars: map[string]any{}}
		r.addObject(obj)
		n, err := r.readLong()
		if err != nil {
			return nil, err
		}
		for i := 0; i < n; i++ {
			k, e := r.readSymbolRef()
			if e != nil {
				return nil, e
			}
			v, e := r.readValue()
			if e != nil {
				return nil, e
			}
			obj.IVars[k] = v
		}
		return obj, nil
	case 'I':
		v, err := r.readValue()
		if err != nil {
			return nil, err
		}
		n, err := r.readLong()
		if err != nil {
			return nil, err
		}
		iv := map[string]any{}
		for i := 0; i < n; i++ {
			k, e := r.readSymbolRef()
			if e != nil {
				return nil, e
			}
			x, e := r.readValue()
			if e != nil {
				return nil, e
			}
			iv[k] = x
		}
		// Encoding ivars belong to Ruby's string wrapper; the semantic value is
		// still the wrapped value. Preserve non-encoding metadata for audit.
		if o, ok := v.(*rubyObject); ok {
			for k, x := range iv {
				o.IVars[k] = x
			}
		}
		return v, nil
	case 'u':
		cls, err := r.readSymbolRef()
		if err != nil {
			return nil, err
		}
		raw, err := r.readRawString()
		if err != nil {
			return nil, err
		}
		u := &rubyUserData{Class: cls, Data: append([]byte(nil), raw...)}
		r.addObject(u)
		return u, nil
	case 'U':
		cls, err := r.readSymbolRef()
		if err != nil {
			return nil, err
		}
		x, err := r.readValue()
		if err != nil {
			return nil, err
		}
		obj := &rubyObject{Class: cls, IVars: map[string]any{"value": x}}
		r.addObject(obj)
		return obj, nil
	case 'e':
		_, err := r.readSymbolRef()
		if err != nil {
			return nil, err
		}
		return r.readValue()
	case 'C':
		_, err := r.readSymbolRef()
		if err != nil {
			return nil, err
		}
		return r.readValue()
	case 'c', 'm', 'M':
		raw, err := r.readRawString()
		if err != nil {
			return nil, err
		}
		s := string(raw)
		r.addObject(s)
		return map[string]any{"_class_ref": s}, nil
	case 'l':
		sign, err := r.readByte()
		if err != nil {
			return nil, err
		}
		words, err := r.readLong()
		if err != nil {
			return nil, err
		}
		raw, err := r.readN(words * 2)
		if err != nil {
			return nil, err
		}
		// RPG data virtually never needs bignums; preserve exact bytes.
		return map[string]any{"_bignum_sign": string([]byte{sign}), "_data_b64": base64.StdEncoding.EncodeToString(raw)}, nil
	default:
		return nil, fmt.Errorf("tipo Ruby Marshal non supportato %q a offset %d", tag, r.pos-1)
	}
}

func decodeRubyMarshalFile(path string) (any, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(b) < 2 || b[0] != 4 || b[1] != 8 {
		return nil, fmt.Errorf("header Ruby Marshal 4.8 non valido")
	}
	r := &rubyMarshalReader{data: b, pos: 2}
	v, err := r.readValue()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	return v, nil
}

func tableFromUserData(u *rubyUserData) (map[string]any, bool) {
	if u == nil || !strings.EqualFold(u.Class, "Table") || len(u.Data) < 20 {
		return nil, false
	}
	dim := int(int32(binary.LittleEndian.Uint32(u.Data[0:4])))
	xs := int(int32(binary.LittleEndian.Uint32(u.Data[4:8])))
	ys := int(int32(binary.LittleEndian.Uint32(u.Data[8:12])))
	zs := int(int32(binary.LittleEndian.Uint32(u.Data[12:16])))
	size := int(int32(binary.LittleEndian.Uint32(u.Data[16:20])))
	if xs < 0 || ys < 0 || zs < 0 || size < 0 {
		return nil, false
	}
	want := xs * maxIntImport(ys, 1) * maxIntImport(zs, 1)
	if size > 0 && want == 0 {
		want = size
	}
	if 20+want*2 > len(u.Data) {
		want = (len(u.Data) - 20) / 2
	}
	layers := make([][][]int, maxIntImport(zs, 1))
	off := 20
	for z := 0; z < len(layers); z++ {
		layers[z] = make([][]int, maxIntImport(ys, 1))
		for y := 0; y < len(layers[z]); y++ {
			layers[z][y] = make([]int, xs)
			for x := 0; x < xs; x++ {
				if off+2 <= len(u.Data) {
					layers[z][y][x] = int(binary.LittleEndian.Uint16(u.Data[off : off+2]))
					off += 2
				}
			}
		}
	}
	return map[string]any{"_class": "Table", "dimensions": dim, "width": xs, "height": ys, "depth": zs, "layers": layers}, true
}

func maxIntImport(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func marshalToJSON(v any) any {
	switch t := v.(type) {
	case nil, bool, int, float64, string:
		return t
	case []any:
		out := make([]any, len(t))
		for i, x := range t {
			out[i] = marshalToJSON(x)
		}
		return out
	case *rubyObject:
		out := map[string]any{"_class": t.Class}
		for k, x := range t.IVars {
			out[k] = marshalToJSON(x)
		}
		return out
	case *rubyUserData:
		if table, ok := tableFromUserData(t); ok {
			return table
		}
		return map[string]any{"_class": t.Class, "_data_b64": base64.StdEncoding.EncodeToString(t.Data)}
	case *rubyHash:
		// Most RPG hashes use integer/string/symbol keys. Encode as JSON object
		// where possible, otherwise preserve an ordered key/value array.
		obj := map[string]any{}
		objectOK := true
		for _, e := range t.Entries {
			var key string
			switch k := e.Key.(type) {
			case int:
				key = strconv.Itoa(k)
			case string:
				key = k
			case map[string]any:
				if s, ok := k["_symbol"].(string); ok {
					key = s
				} else {
					objectOK = false
				}
			default:
				objectOK = false
			}
			if !objectOK {
				break
			}
			obj[key] = marshalToJSON(e.Value)
		}
		if objectOK {
			if t.HasDefault {
				obj["_default"] = marshalToJSON(t.Default)
			}
			return obj
		}
		arr := make([]any, 0, len(t.Entries))
		for _, e := range t.Entries {
			arr = append(arr, map[string]any{"key": marshalToJSON(e.Key), "value": marshalToJSON(e.Value)})
		}
		out := map[string]any{"_hash": arr}
		if t.HasDefault {
			out["_default"] = marshalToJSON(t.Default)
		}
		return out
	case map[string]any:
		out := map[string]any{}
		for k, x := range t {
			out[k] = marshalToJSON(x)
		}
		return out
	default:
		return fmt.Sprint(t)
	}
}

func writeJSON(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0644); err != nil {
		return err
	}
	_ = os.Remove(path)
	return os.Rename(tmp, path)
}

func convertRXDataToJSON(src, dst string) error {
	v, err := decodeRubyMarshalFile(src)
	if err != nil {
		return err
	}
	return writeJSON(dst, marshalToJSON(v))
}

func sha256HexBytes(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func sha256HexFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func baselineScriptHashMatches(name, hash string) bool {
	for _, wanted := range essentialsV201CoreScriptHashes[name] {
		if strings.EqualFold(wanted, hash) {
			return true
		}
	}
	return false
}

func essentialsV201BaselineScriptCount() int {
	n := 0
	for _, hashes := range essentialsV201CoreScriptHashes {
		n += len(hashes)
	}
	return n
}

func analyzeEssentialsScriptsV201(src string) (scriptAnalysis, error) {
	out := scriptAnalysis{ReferenceProfile: essentialsV201ProfileID, BaselineScripts: essentialsV201BaselineScriptCount()}
	archiveHash, _ := sha256HexFile(src)
	out.ExactArchive = strings.EqualFold(archiveHash, essentialsV201ScriptArchiveSHA256)
	v, err := decodeRubyMarshalFile(src)
	if err != nil {
		return out, err
	}
	arr, ok := v.([]any)
	if !ok {
		return out, fmt.Errorf("Scripts.rxdata non contiene un array")
	}
	seenNames := map[string]bool{}
	matchedNames := map[string]bool{}
	for _, row := range arr {
		cols, ok := row.([]any)
		if !ok || len(cols) < 3 {
			continue
		}
		name, _ := cols[1].(string)
		name = strings.TrimSpace(name)
		compressed, ok := cols[2].(string)
		if !ok {
			continue
		}
		zr, err := zlib.NewReader(bytes.NewReader([]byte(compressed)))
		if err != nil {
			continue
		}
		body, readErr := io.ReadAll(zr)
		_ = zr.Close()
		if readErr != nil || len(body) == 0 || name == "" {
			continue
		}
		out.SubstantiveScripts++
		hash := sha256HexBytes(body)
		hashes, isCoreName := essentialsV201CoreScriptHashes[name]
		if !isCoreName {
			out.Custom++
			continue
		}
		seenNames[name] = true
		if baselineScriptHashMatches(name, hash) {
			out.Matched++
			matchedNames[name] = true
		} else if len(hashes) > 0 {
			out.Modified++
		}
	}
	baselineNames := len(essentialsV201CoreScriptHashes)
	if baselineNames > 0 {
		out.NameMatchPercent = 100 * float64(len(seenNames)) / float64(baselineNames)
	}
	if out.BaselineScripts > 0 {
		out.ExactMatchPercent = 100 * float64(out.Matched) / float64(out.BaselineScripts)
	}
	// A customized v20.1 project may legitimately modify many core scripts.
	// Version recognition therefore uses script-name coverage first, while the
	// exact hash ratio is reported separately as the customization signal.
	if out.ExactArchive || out.NameMatchPercent >= 80.0 {
		out.DetectedVersion = "Pokémon Essentials v20.1 (2022-06-20)"
	} else {
		out.DetectedVersion = "Essentials/RMXP non riconosciuto come base v20.1"
	}
	_ = matchedNames // retained for future per-module diagnostics
	return out, nil
}

func countFilesRecursive(root string) int {
	count := 0
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			count++
		}
		return nil
	})
	return count
}

func customPBSFiles(root string) []string {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() || strings.ToLower(filepath.Ext(e.Name())) != ".txt" {
			continue
		}
		if !essentialsV201StandardPBSFiles[strings.ToLower(e.Name())] {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

func extractScriptsRXData(src, dstDir, mode string) (scriptExtractionSummary, error) {
	var summary scriptExtractionSummary
	v, err := decodeRubyMarshalFile(src)
	if err != nil {
		return summary, err
	}
	arr, ok := v.([]any)
	if !ok {
		return summary, fmt.Errorf("Scripts.rxdata non contiene un array")
	}
	if err := os.MkdirAll(dstDir, 0755); err != nil {
		return summary, err
	}
	type scriptIndex struct {
		SourceIndex    int    `json:"source_index"`
		ID             int    `json:"id"`
		Name           string `json:"name"`
		File           string `json:"file"`
		SHA256         string `json:"sha256"`
		Classification string `json:"classification"`
	}
	index := []scriptIndex{}
	used := map[string]int{}
	cleanRE := regexp.MustCompile(`[^A-Za-z0-9._ -]+`)
	customDir := filepath.Join(dstDir, "custom_and_modified")
	for i, row := range arr {
		cols, ok := row.([]any)
		if !ok || len(cols) < 3 {
			continue
		}
		id, _ := cols[0].(int)
		if id == 0 {
			id = i + 1
		}
		originalName, _ := cols[1].(string)
		originalName = strings.TrimSpace(originalName)
		name := originalName
		if name == "" {
			name = fmt.Sprintf("Script_%03d", i+1)
		}
		compressed, ok := cols[2].(string)
		if !ok {
			continue
		}
		zr, err := zlib.NewReader(bytes.NewReader([]byte(compressed)))
		if err != nil {
			return summary, fmt.Errorf("script %d %q: stream zlib non valido: %w", i+1, originalName, err)
		}
		body, readErr := io.ReadAll(zr)
		closeErr := zr.Close()
		if readErr != nil {
			return summary, fmt.Errorf("script %d %q: lettura zlib fallita: %w", i+1, originalName, readErr)
		}
		if closeErr != nil {
			return summary, fmt.Errorf("script %d %q: chiusura zlib fallita: %w", i+1, originalName, closeErr)
		}
		hash := sha256HexBytes(body)
		classification := "structural"
		if len(body) > 0 {
			switch mode {
			case "plugin":
				classification = "plugin"
				summary.Custom++
			default:
				if _, exists := essentialsV201CoreScriptHashes[originalName]; exists {
					if baselineScriptHashMatches(originalName, hash) {
						classification = "core_v20_1"
						summary.Matched++
					} else {
						classification = "core_modified"
						summary.Modified++
					}
				} else {
					classification = "custom"
					summary.Custom++
				}
			}
		}
		safe := strings.Trim(cleanRE.ReplaceAllString(name, "_"), " ._")
		if safe == "" {
			safe = "Script"
		}
		base := fmt.Sprintf("%03d_%s.rb", i+1, safe)
		if n := used[strings.ToLower(base)]; n > 0 {
			base = fmt.Sprintf("%03d_%s_%d.rb", i+1, safe, n+1)
		}
		used[strings.ToLower(base)]++
		if err := os.WriteFile(filepath.Join(dstDir, base), body, 0644); err != nil {
			return summary, err
		}
		if classification == "core_modified" || classification == "custom" || classification == "plugin" {
			if err := os.MkdirAll(customDir, 0755); err != nil {
				return summary, err
			}
			if err := os.WriteFile(filepath.Join(customDir, base), body, 0644); err != nil {
				return summary, err
			}
		}
		index = append(index, scriptIndex{SourceIndex: i, ID: id, Name: originalName, File: base, SHA256: hash, Classification: classification})
		summary.Total++
	}
	if err := writeJSON(filepath.Join(dstDir, "index.json"), index); err != nil {
		return summary, err
	}
	return summary, nil
}

func importProjectName(source string) string {
	// Game.ini normally contains Title=... and is more useful than the folder name.
	if b, err := os.ReadFile(filepath.Join(source, "Game.ini")); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(strings.ToLower(line), "title=") {
				if n := strings.TrimSpace(strings.TrimPrefix(line, "Title=")); n != "" {
					return n
				}
			}
		}
	}
	return filepath.Base(filepath.Clean(source))
}

func writePythonBootstrap(dest, projectName string) error {
	// This is deliberately a small, valid Python project entry point. It gives
	// PLM a canonical main.py immediately after conversion and validates the
	// converted data. The full game runtime can then evolve independently from
	// the importer without ever falling back to RMXP.
	py := `from __future__ import annotations
import argparse, json
from pathlib import Path

ROOT = Path(__file__).resolve().parent

def map_files():
    return sorted((ROOT / "converted" / "maps").rglob("Map*.json"))

def main() -> int:
    parser = argparse.ArgumentParser(description="PLM/Python converted project")
    parser.add_argument("--debug", action="store_true")
    parser.add_argument("--map", type=int, default=0)
    parser.add_argument("--select-map", action="store_true")
    args = parser.parse_args()
    maps = map_files()
    if not maps:
        raise SystemExit("No converted maps found in converted/maps")
    target = maps[0]
    if args.map:
        wanted = f"Map{args.map:03d}.json".lower()
        target = next((p for p in maps if p.name.lower() == wanted), target)
    data = json.loads(target.read_text(encoding="utf-8"))
    print(f"PLM Python project: {ROOT.name}")
    print(f"Converted maps: {len(maps)} | selected: {target.name} | size: {data.get('width')}x{data.get('height')}")
    return 0

if __name__ == "__main__":
    raise SystemExit(main())
`
	if err := os.WriteFile(filepath.Join(dest, "main.py"), []byte(py), 0644); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(dest, "game"), 0755); err != nil {
		return err
	}
	init := "\"\"\"PLM/Python project package.\"\"\"\n"
	return os.WriteFile(filepath.Join(dest, "game", "__init__.py"), []byte(init), 0644)
}

func convertEssentialsProject(source, dest string) (*EssentialsImportReport, error) {
	return convertEssentialsProjectWithProgress(source, dest, nil)
}

func convertEssentialsProjectWithProgress(source, dest string, progress EssentialsImportProgressFunc) (*EssentialsImportReport, error) {
	source = filepath.Clean(source)
	dest = filepath.Clean(dest)
	emitEssentialsImportProgress(progress, 1, "Validazione progetto", "Controllo struttura Pokémon Essentials...")
	if err := validateEssentialsProject(source); err != nil {
		return nil, err
	}
	if strings.EqualFold(source, dest) {
		return nil, fmt.Errorf("la destinazione deve essere diversa dal progetto Essentials originale")
	}

	emitEssentialsImportProgress(progress, 3, "Analisi Pokémon Essentials v20.1", "Confronto Scripts.rxdata e PBS con la base canonica...")
	analysis, err := analyzeEssentialsScriptsV201(filepath.Join(source, "Data", "Scripts.rxdata"))
	if err != nil {
		return nil, fmt.Errorf("analisi versione Essentials: %w", err)
	}
	pbsAnalysis, err := analyzeEssentialsPBSV201(filepath.Join(source, "PBS"))
	if err != nil {
		return nil, fmt.Errorf("analisi PBS Essentials v20.1: %w", err)
	}
	// A v20.1 fangame may legitimately modify most core scripts. Version
	// recognition is therefore based on the presence/naming of the v20.1 core,
	// while hash differences are preserved and reported as customizations.
	if !analysis.ExactArchive && analysis.NameMatchPercent < 70.0 {
		return nil, fmt.Errorf(
			"il progetto non corrisponde abbastanza alla struttura Pokémon Essentials v20.1 2022-06-20 (nomi core %.1f%%). Importazione bloccata per evitare di trattare un'altra versione come v20.1",
			analysis.NameMatchPercent,
		)
	}

	emitEssentialsImportProgress(progress, 5, "Preparazione progetto Python", filepath.Base(dest))
	if err := os.MkdirAll(dest, 0755); err != nil {
		return nil, err
	}
	sourceClassification := "BASE_V20_1"
	if !analysis.ExactArchive || pbsAnalysis.Status != "BASE_V20_1" {
		sourceClassification = "MODIFIED_V20_1"
	}
	report := &EssentialsImportReport{
		Source:                source,
		Destination:           dest,
		StartedAt:             time.Now().Format(time.RFC3339),
		ProjectName:           importProjectName(source),
		ProjectType:           "Pokémon Essentials v20.1 / RPG Maker XP → PLM Python",
		ReferenceProfile:      essentialsV201ProfileID,
		DetectedVersion:       analysis.DetectedVersion,
		SourceClassification:  sourceClassification,
		BaselineNameMatch:     analysis.NameMatchPercent,
		BaselineExactMatch:    analysis.ExactMatchPercent,
		PBSReferenceProfile:   pbsAnalysis.ReferenceProfile,
		PBSBaselineStatus:     pbsAnalysis.Status,
		PBSBaselineExactMatch: pbsAnalysis.ExactMatchPercent,
		PBSExactFiles:         pbsAnalysis.ExactFiles,
		PBSModifiedFiles:      append([]string(nil), pbsAnalysis.ModifiedFiles...),
		PBSMissingFiles:       append([]string(nil), pbsAnalysis.MissingFiles...),
		PBSAddedFiles:         append([]string(nil), pbsAnalysis.AddedFiles...),
	}
	if !analysis.ExactArchive && analysis.ExactMatchPercent < 99.9 {
		report.Warnings = append(report.Warnings, fmt.Sprintf(
			"Core Essentials personalizzato rilevato: %.1f%% degli script core coincide byte-per-byte con la base originale. Gli script modificati vengono preservati separatamente.",
			analysis.ExactMatchPercent,
		))
	}
	if pbsAnalysis.Status != "BASE_V20_1" {
		report.Warnings = append(report.Warnings, fmt.Sprintf(
			"PBS personalizzati rilevati rispetto alla base v20.1: %d modificati, %d mancanti, %d aggiunti. L'intero albero PBS sorgente viene preservato 1:1.",
			len(pbsAnalysis.ModifiedFiles), len(pbsAnalysis.MissingFiles), len(pbsAnalysis.AddedFiles),
		))
	}

	// Never mutate the source project. Standard v20.1 assets are copied as the
	// source of truth; user customizations remain preserved alongside them.
	assetCopies := []struct {
		src, dst, label string
		start, end      int
	}{
		{filepath.Join(source, "Graphics"), filepath.Join(dest, "assets", "Graphics"), "Copia Graphics", 6, 23},
		{filepath.Join(source, "Audio"), filepath.Join(dest, "assets", "Audio"), "Copia Audio", 23, 30},
		{filepath.Join(source, "Fonts"), filepath.Join(dest, "assets", "Fonts"), "Copia Fonts", 30, 32},
		{filepath.Join(source, "PBS"), filepath.Join(dest, "converted", "PBS"), "Copia PBS", 32, 36},
	}
	for _, c := range assetCopies {
		if isDir(c.src) {
			emitEssentialsImportProgress(progress, c.start, c.label, "Preparazione...")
			if err := copyTreeWithProgress(c.src, c.dst, c.start, c.end, c.label, progress); err != nil {
				return nil, fmt.Errorf("copia %s: %w", c.label, err)
			}
			report.AssetsCopied = append(report.AssetsCopied, strings.TrimPrefix(c.label, "Copia "))
		}
	}
	// S0 non-destruction gate: the copied PBS tree must be byte-for-byte
	// identical to the imported project, including modified/custom files.
	if err := verifyTreeExact(filepath.Join(source, "PBS"), filepath.Join(dest, "converted", "PBS")); err != nil {
		return nil, fmt.Errorf("verifica copia PBS 1:1: %w", err)
	}
	report.PBSCopyVerified = true
	// From this point on the converted project has one canonical asset layout.
	// Every PLM editor resolves assets from assets/Graphics and assets/Audio first.
	if err := ensureCanonicalAssetLayout(dest); err != nil {
		return nil, fmt.Errorf("preparazione cartelle asset PLM: %w", err)
	}
	if err := writeAssetManifest(dest); err != nil {
		report.Warnings = append(report.Warnings, "asset_manifest.json: "+err.Error())
	}
	report.CustomPBSFiles = customPBSFiles(filepath.Join(source, "PBS"))

	// Preserve plugin source independently from the Essentials core. The stock
	// v20.1 reference has an empty Plugins folder; anything here is project-specific.
	if plugins := filepath.Join(source, "Plugins"); isDir(plugins) {
		dstPlugins := filepath.Join(dest, "converted", "plugins_ruby")
		emitEssentialsImportProgress(progress, 36, "Plugin Essentials", "Copia sorgenti plugin...")
		if err := copyTreeWithProgress(plugins, dstPlugins, 36, 40, "Plugin Essentials", progress); err != nil {
			return nil, fmt.Errorf("copia Plugins: %w", err)
		}
		report.PluginSourceFilesCopied = countFilesRecursive(plugins)
		if report.PluginSourceFilesCopied > 0 {
			report.AssetsCopied = append(report.AssetsCopied, "Plugins (sorgente Ruby)")
		}
	}

	convertedMaps := filepath.Join(dest, "converted", "maps")
	convertedData := filepath.Join(dest, "converted", "data")
	_ = os.MkdirAll(convertedMaps, 0755)
	_ = os.MkdirAll(convertedData, 0755)
	if err := writeJSON(filepath.Join(convertedData, "source_pbs_analysis.json"), pbsAnalysis); err != nil {
		return nil, fmt.Errorf("scrittura analisi PBS: %w", err)
	}

	// MapInfos is canonical for the left map tree.
	emitEssentialsImportProgress(progress, 40, "Conversione struttura mappe", "MapInfos.rxdata")
	if err := convertRXDataToJSON(filepath.Join(source, "Data", "MapInfos.rxdata"), filepath.Join(dest, "converted", "MapInfos.json")); err != nil {
		return nil, fmt.Errorf("conversione MapInfos: %w", err)
	}

	mapRE := regexp.MustCompile(`(?i)^Map(\d+)\.rxdata$`)
	entries, err := os.ReadDir(filepath.Join(source, "Data"))
	if err != nil {
		return nil, err
	}
	mapTotal := 0
	for _, e := range entries {
		if !e.IsDir() && mapRE.MatchString(e.Name()) {
			mapTotal++
		}
	}
	mapDone := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		m := mapRE.FindStringSubmatch(e.Name())
		if len(m) != 2 {
			continue
		}
		id, _ := strconv.Atoi(m[1])
		if id <= 0 {
			continue
		}
		mapDone++
		mapPercent := 42
		if mapTotal > 0 {
			mapPercent += (mapDone * 25) / mapTotal
		}
		emitEssentialsImportProgress(progress, mapPercent, "Conversione mappe", fmt.Sprintf("Map%03d.rxdata (%d/%d)", id, mapDone, mapTotal))
		dst := filepath.Join(convertedMaps, fmt.Sprintf("Map%03d.json", id))
		if err := convertRXDataToJSON(filepath.Join(source, "Data", e.Name()), dst); err != nil {
			return nil, fmt.Errorf("Map%03d: %w", id, err)
		}
		report.MapsConverted++
	}

	emitEssentialsImportProgress(progress, 66, "Organizzazione regioni", "Lettura town_map.txt e map_metadata.txt...")
	regionsCreated, regionalMaps, regionErr := organizeImportedMapsByRegions(dest)
	if regionErr != nil {
		report.Warnings = append(report.Warnings, "Organizzazione regioni: "+regionErr.Error())
	} else {
		report.RegionsCreated = regionsCreated
		report.RegionalMapsOrganized = regionalMaps
	}

	generic := []string{"Actors", "Animations", "PkmnAnimations", "Armors", "Classes", "CommonEvents", "Enemies", "Items", "Skills", "States", "System", "Tilesets", "Troops", "Weapons"}
	standardRX := map[string]bool{"mapinfos": true, "scripts": true, "pluginscripts": true}
	genericTotal := 0
	for _, name := range generic {
		if exists(filepath.Join(source, "Data", name+".rxdata")) {
			genericTotal++
		}
	}
	genericDone := 0
	for _, name := range generic {
		standardRX[strings.ToLower(name)] = true
		src := filepath.Join(source, "Data", name+".rxdata")
		if !exists(src) {
			continue
		}
		genericDone++
		percent := 67
		if genericTotal > 0 {
			percent += (genericDone * 8) / genericTotal
		}
		emitEssentialsImportProgress(progress, percent, "Conversione database Essentials", name+".rxdata")
		if err := convertRXDataToJSON(src, filepath.Join(convertedData, name+".json")); err != nil {
			report.Warnings = append(report.Warnings, fmt.Sprintf("%s.rxdata: %v", name, err))
			continue
		}
		report.DataFilesConverted++
	}

	// Preserve the exact compiled script archive as immutable conversion evidence.
	// Extracted .rb files are useful for analysis/migration, but the original
	// Scripts.rxdata is the authoritative record for ordering, removals and every
	// user modification made in RPG Maker XP.
	if scripts := filepath.Join(source, "Data", "Scripts.rxdata"); exists(scripts) {
		preservedScripts := filepath.Join(dest, "converted", "source_compiled_data", "Scripts.rxdata")
		if err := copyFileAtomic(scripts, preservedScripts); err != nil {
			return nil, fmt.Errorf("conservazione Scripts.rxdata originale: %w", err)
		}
	}

	// Extract the actual project Scripts.rxdata and classify each script against
	// the untouched v20.1 reference instead of assuming all source scripts are core.
	if scripts := filepath.Join(source, "Data", "Scripts.rxdata"); exists(scripts) {
		emitEssentialsImportProgress(progress, 77, "Estrazione script Ruby", "Scripts.rxdata")
		sum, err := extractScriptsRXData(scripts, filepath.Join(dest, "converted", "scripts_ruby"), "core")
		if err != nil {
			report.Warnings = append(report.Warnings, "Scripts.rxdata: "+err.Error())
		} else {
			report.RubyScriptsExtracted = sum.Total
			report.CoreScriptsMatched = sum.Matched
			report.CoreScriptsModified = sum.Modified
			report.CustomScripts = sum.Custom
		}
	}

	// Persist the imported mechanics generation as a PLM project setting.
	// The project PBS copied above remains the canonical editable data tree.
	// Gen 5..8 folders are retained only as untouched Essentials references and
	// never silently replace modified/custom PBS data.
	importedMechanicsGeneration := detectImportedMechanicsGeneration(dest)
	if _, mechanicsErr := saveMechanicsSettings(dest, importedMechanicsGeneration); mechanicsErr != nil {
		report.Warnings = append(report.Warnings, "Profilo meccaniche Pokémon: "+mechanicsErr.Error())
	}

	// PluginScripts.rxdata is empty in the untouched v20.1 reference. Any
	// extracted executable script here belongs to the imported project/plugins.
	if plugins := filepath.Join(source, "Data", "PluginScripts.rxdata"); exists(plugins) {
		emitEssentialsImportProgress(progress, 83, "Estrazione PluginScripts", "PluginScripts.rxdata")
		sum, err := extractScriptsRXData(plugins, filepath.Join(dest, "converted", "plugin_scripts_ruby"), "plugin")
		if err != nil {
			report.Warnings = append(report.Warnings, "PluginScripts.rxdata: "+err.Error())
		} else {
			report.PluginScriptsExtracted = sum.Total
		}
	}

	// Unknown/custom .rxdata created by plugins are not discarded. Try to decode
	// them as Ruby Marshal first; if that fails preserve the raw file verbatim.
	customJSONDir := filepath.Join(convertedData, "custom")
	customRawDir := filepath.Join(convertedData, "custom_raw")
	customTotal := 0
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".rxdata") || mapRE.MatchString(e.Name()) {
			continue
		}
		base := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
		if !standardRX[strings.ToLower(base)] {
			customTotal++
		}
	}
	customDone := 0
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".rxdata") || mapRE.MatchString(e.Name()) {
			continue
		}
		base := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
		if standardRX[strings.ToLower(base)] {
			continue
		}
		customDone++
		percent := 86
		if customTotal > 0 {
			percent += (customDone * 4) / customTotal
		}
		emitEssentialsImportProgress(progress, percent, "Dati custom/plugin", e.Name())
		src := filepath.Join(source, "Data", e.Name())
		if err := convertRXDataToJSON(src, filepath.Join(customJSONDir, base+".json")); err != nil {
			if err2 := os.MkdirAll(customRawDir, 0755); err2 != nil {
				return nil, err2
			}
			if err2 := copyFileAtomic(src, filepath.Join(customRawDir, e.Name())); err2 != nil {
				return nil, err2
			}
			report.Warnings = append(report.Warnings, fmt.Sprintf("Dati custom %s preservati raw: %v", e.Name(), err))
		}
		report.CustomDataFiles++
	}

	// Preserve compiled .dat files for custom/plugin migration and audit. PBS is
	// still the editable source of truth for standard Essentials data.
	compiledDst := filepath.Join(dest, "converted", "source_compiled_data")
	datTotal := 0
	for _, e := range entries {
		if !e.IsDir() && strings.EqualFold(filepath.Ext(e.Name()), ".dat") {
			datTotal++
		}
	}
	datDone := 0
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".dat") {
			continue
		}
		datDone++
		percent := 90
		if datTotal > 0 {
			percent += (datDone * 4) / datTotal
		}
		emitEssentialsImportProgress(progress, percent, "Conservazione dati compilati", e.Name())
		if err := os.MkdirAll(compiledDst, 0755); err != nil {
			return nil, err
		}
		if err := copyFileAtomic(filepath.Join(source, "Data", e.Name()), filepath.Join(compiledDst, e.Name())); err != nil {
			return nil, err
		}
	}

	// Preserve source configuration separately for diagnostics, never as runtime
	// dependencies of the converted Python project.
	emitEssentialsImportProgress(progress, 94, "Configurazione sorgente", "Game.ini / Game.rxproj / mkxp.json")
	configDst := filepath.Join(dest, "converted", "source_config")
	for _, name := range []string{"Game.ini", "Game.rxproj", "mkxp.json"} {
		src := filepath.Join(source, name)
		if !exists(src) {
			continue
		}
		if err := os.MkdirAll(configDst, 0755); err != nil {
			return nil, err
		}
		if err := copyFileAtomic(src, filepath.Join(configDst, name)); err != nil {
			return nil, err
		}
	}

	emitEssentialsImportProgress(progress, 96, "Finalizzazione progetto Python", "Creazione manifest e bootstrap...")
	manifest := ProjectManifest{FormatVersion: 1, Name: report.ProjectName, ProjectType: "pokemon-essentials-v20.1-python", Root: ".", CreatedBy: "PLM Studio", MapFolders: []string{"converted/maps"}}
	if err := writeJSON(filepath.Join(dest, "plm_project.json"), manifest); err != nil {
		return nil, err
	}
	sourceMeta := map[string]any{
		"engine":                           "RPG Maker XP / Pokémon Essentials",
		"source_root":                      source,
		"imported_at":                      time.Now().Format(time.RFC3339),
		"source_immutable":                 true,
		"reference_profile":                essentialsV201ProfileID,
		"detected_version":                 analysis.DetectedVersion,
		"baseline_name_match_percent":      analysis.NameMatchPercent,
		"baseline_exact_match_percent":     analysis.ExactMatchPercent,
		"source_classification":            sourceClassification,
		"pbs_reference_profile":            pbsAnalysis.ReferenceProfile,
		"pbs_baseline_status":              pbsAnalysis.Status,
		"pbs_baseline_exact_match_percent": pbsAnalysis.ExactMatchPercent,
		"pbs_copy_verified_1_to_1":         report.PBSCopyVerified,
	}
	if err := writeJSON(filepath.Join(convertedData, "source_project.json"), sourceMeta); err != nil {
		return nil, err
	}
	if err := writeJSON(filepath.Join(dest, "converted", "essentials_profile.json"), analysis); err != nil {
		return nil, err
	}
	// Install the project-agnostic PLM runtime only after all converted data/assets
	// have been generated. A successful conversion must be immediately runnable
	// and must expose both the normal launcher and the DEBUG launcher.
	emitEssentialsImportProgress(progress, 97, "Installazione runtime Python", "Creazione launcher Release e DEBUG...")
	if _, _, err := installRuntimeCore(dest, report.ProjectName); err != nil {
		return nil, fmt.Errorf("installazione runtime PLM: %w", err)
	}
	if err := writePythonBootstrap(dest, report.ProjectName); err != nil {
		return nil, err
	}
	emitEssentialsImportProgress(progress, 99, "Validazione progetto convertito", "Runtime, launcher, mappe e dati 1:1...")
	if err := validateConvertedEssentialsProject(source, dest, report); err != nil {
		return nil, fmt.Errorf("validazione conversione PLM: %w", err)
	}
	report.CompletedAt = time.Now().Format(time.RFC3339)
	if err := writeJSON(filepath.Join(dest, "converted", "conversion_report.json"), report); err != nil {
		return nil, err
	}
	emitEssentialsImportProgress(progress, 100, "Conversione completata", fmt.Sprintf("%d mappe, %d archivi dati, %d script Ruby", report.MapsConverted, report.DataFilesConverted, report.RubyScriptsExtracted))
	return report, nil
}
