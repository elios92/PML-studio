//go:build windows

package main

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type PixelSurface struct {
	Width, Height int
	Pixels        []uint32 // 0x00RRGGBB, top-down
	Alpha         []byte
}

type TilesetDescriptor struct {
	ID            int
	Name          string
	TilesetName   string
	AutotileNames []string
	Passages      []int
	Priorities    []int
	TerrainTags   []int
	Tileset       *PixelSurface
	Autotiles     []*PixelSurface
	MetadataPath  string
	TilesetPath   string
}

type tilesetMetadataJSON struct {
	ID                  int      `json:"id"`
	Name                string   `json:"name"`
	TilesetName         string   `json:"tileset_name"`
	AutotileNames       []string `json:"autotile_names"`
	SourceTilesetName   string   `json:"source_tileset_name,omitempty"`
	SourceAutotileNames []string `json:"source_autotile_names,omitempty"`
	Passages            []int    `json:"passages,omitempty"`
	Priorities          []int    `json:"priorities,omitempty"`
	TerrainTags         []int    `json:"terrain_tags,omitempty"`
}

type tilesetMetadataRecord struct {
	Data tilesetMetadataJSON
	Path string
}

var (
	currentTileset *TilesetDescriptor

	assetIndexProject string
	assetFilesByBase  map[string][]string
	assetGraphicsDirs []string

	tilesetIndexProject string
	tilesetMetadataByID map[int]tilesetMetadataRecord

	tilesetDescriptorCache = make(map[string]*TilesetDescriptor)
)

func loadPNGSurface(path string) (*PixelSurface, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	img, _, err := image.Decode(f)
	if err != nil {
		return nil, err
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return nil, fmt.Errorf("immagine vuota")
	}
	px := make([]uint32, w*h)
	alpha := make([]byte, w*h)

	// image.Decode(PNG) restituisce normalmente RGBA/NRGBA. Leggere Pix
	// direttamente evita milioni di chiamate all'interfaccia img.At() sui
	// tileset molto alti di RPG Maker XP.
	switch src := img.(type) {
	case *image.NRGBA:
		for y := 0; y < h; y++ {
			si := (b.Min.Y+y-src.Rect.Min.Y)*src.Stride + (b.Min.X-src.Rect.Min.X)*4
			di := y * w
			for x := 0; x < w; x++ {
				i := si + x*4
				px[di+x] = uint32(src.Pix[i])<<16 | uint32(src.Pix[i+1])<<8 | uint32(src.Pix[i+2])
				alpha[di+x] = src.Pix[i+3]
			}
		}
	case *image.RGBA:
		for y := 0; y < h; y++ {
			si := (b.Min.Y+y-src.Rect.Min.Y)*src.Stride + (b.Min.X-src.Rect.Min.X)*4
			di := y * w
			for x := 0; x < w; x++ {
				i := si + x*4
				a := src.Pix[i+3]
				alpha[di+x] = a
				if a == 0 || a == 255 {
					px[di+x] = uint32(src.Pix[i])<<16 | uint32(src.Pix[i+1])<<8 | uint32(src.Pix[i+2])
				} else {
					// image.RGBA usa componenti premoltiplicate. Riporta il
					// colore a RGB diritto, perché il compositor applica alpha.
					aa := uint32(a)
					r := minUint32(uint32(src.Pix[i])*255/aa, 255)
					g := minUint32(uint32(src.Pix[i+1])*255/aa, 255)
					bl := minUint32(uint32(src.Pix[i+2])*255/aa, 255)
					px[di+x] = r<<16 | g<<8 | bl
				}
			}
		}
	default:
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				r, g, bl, a := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
				alpha[y*w+x] = byte(a >> 8)
				px[y*w+x] = uint32(r>>8)<<16 | uint32(g>>8)<<8 | uint32(bl>>8)
			}
		}
	}
	return &PixelSurface{Width: w, Height: h, Pixels: px, Alpha: alpha}, nil
}

func minUint32(a, b uint32) uint32 {
	if a < b {
		return a
	}
	return b
}

func tilesetCacheKey(project string, id int) string {
	return strings.ToLower(filepath.Clean(project)) + "|" + strconv.Itoa(id)
}

func clearTilesetDescriptorCache() {
	tilesetDescriptorCache = make(map[string]*TilesetDescriptor)
}

// resetTilesetProjectCaches invalidates every project-scoped tileset/asset
// cache. It must be called whenever a project is opened/reopened, including
// immediately after conversion into a destination path that was already open.
// Without this reset, a graphics-only fallback descriptor (0 autotiles) can be
// reused even after the converter has generated the real tileset metadata.
func resetTilesetProjectCaches() {
	assetIndexProject = ""
	assetFilesByBase = nil
	assetGraphicsDirs = nil
	tilesetIndexProject = ""
	tilesetMetadataByID = nil
	clearTilesetDescriptorCache()
}

func shouldSkipProjectDir(name string) bool {
	switch strings.ToLower(name) {
	case ".git", ".svn", "bin", "dist", "build", "source", ".plm", "save", "saves", "lib", "__pycache__", ".venv", "venv", "node_modules":
		return true
	}
	return false
}

func uniqueExistingDirs(paths []string) []string {
	seen := make(map[string]bool)
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		p = filepath.Clean(p)
		if !isDir(p) {
			continue
		}
		key := strings.ToLower(p)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, p)
	}
	return out
}

func discoverGraphicsDirs(project string) []string {
	project = filepath.Clean(project)
	candidates := []string{
		filepath.Join(project, "assets", "Graphics"),
		filepath.Join(project, "Graphics"),
		filepath.Join(project, "game", "Graphics"),
		filepath.Join(project, "game", "assets", "Graphics"),
		filepath.Join(project, "converted", "Graphics"),
		filepath.Join(project, "converted", "assets", "Graphics"),
	}

	// Runtime conversions are not all laid out identically. Discover additional
	// Graphics roots once when the project is opened, never during WM_PAINT.
	_ = filepath.WalkDir(project, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		if path != project && shouldSkipProjectDir(d.Name()) {
			return filepath.SkipDir
		}
		if strings.EqualFold(d.Name(), "Graphics") {
			candidates = append(candidates, path)
			return filepath.SkipDir
		}
		return nil
	})
	return uniqueExistingDirs(candidates)
}

func rebuildAssetIndex(project string) {
	project = filepath.Clean(project)
	assetIndexProject = project
	assetFilesByBase = make(map[string][]string)
	assetGraphicsDirs = discoverGraphicsDirs(project)

	for _, root := range assetGraphicsDirs {
		_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				return nil
			}
			ext := strings.ToLower(filepath.Ext(d.Name()))
			if ext != ".png" && ext != ".jpg" && ext != ".jpeg" && ext != ".gif" {
				return nil
			}
			base := strings.ToLower(strings.TrimSuffix(d.Name(), filepath.Ext(d.Name())))
			assetFilesByBase[base] = append(assetFilesByBase[base], path)
			return nil
		})
	}
}

func ensureAssetIndex(project string) {
	project = filepath.Clean(project)
	if assetFilesByBase == nil || !strings.EqualFold(assetIndexProject, project) {
		rebuildAssetIndex(project)
	}
}

func isCanonicalNumericAssetName(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	for _, r := range name {
		if (r < '0' || r > '9') && r != '_' {
			return false
		}
	}
	return true
}

func canonicalGraphicsAsset(project, group, name string) string {
	name = strings.TrimSpace(strings.ReplaceAll(name, "\\", "/"))
	if name == "" {
		return ""
	}
	root := filepath.Join(filepath.Clean(project), "assets", "Graphics", group)
	if !isDir(root) {
		return ""
	}
	rel := filepath.FromSlash(strings.TrimLeft(name, "/"))
	if filepath.Ext(rel) != "" {
		p := filepath.Join(root, rel)
		if exists(p) {
			return p
		}
	} else {
		for _, ext := range []string{".png", ".jpg", ".jpeg", ".gif"} {
			p := filepath.Join(root, rel+ext)
			if exists(p) {
				return p
			}
		}
	}

	// Fallback case-insensitive/relative-path for projects created on Windows
	// and inspected on a case-sensitive filesystem. The canonical root remains
	// assets/Graphics/<group>; we do not leave that tree during this lookup.
	wanted := strings.ToLower(strings.TrimSuffix(filepath.ToSlash(rel), filepath.Ext(rel)))
	var found string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || found != "" {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(d.Name()))
		if ext != ".png" && ext != ".jpg" && ext != ".jpeg" && ext != ".gif" {
			return nil
		}
		r, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}
		key := strings.ToLower(strings.TrimSuffix(filepath.ToSlash(r), filepath.Ext(r)))
		if key == wanted {
			found = path
		}
		return nil
	})
	return found
}

func graphicsAsset(project, group, name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}

	// PLM canonical asset location. This must always win over legacy copies.
	if p := canonicalGraphicsAsset(project, group, name); p != "" {
		return p
	}

	ensureAssetIndex(project)

	base := strings.ToLower(strings.TrimSuffix(filepath.Base(name), filepath.Ext(name)))
	paths := append([]string(nil), assetFilesByBase[base]...)
	if len(paths) == 0 {
		return ""
	}
	wanted := "/" + strings.ToLower(group) + "/"
	preferGenerated := isCanonicalNumericAssetName(base)
	sort.SliceStable(paths, func(i, j int) bool {
		iPath := strings.ToLower(filepath.ToSlash(paths[i]))
		jPath := strings.ToLower(filepath.ToSlash(paths[j]))
		iIn := strings.Contains(iPath, wanted)
		jIn := strings.Contains(jPath, wanted)
		if iIn != jIn {
			return iIn
		}
		if preferGenerated {
			iGenerated := strings.Contains(iPath, "/_plm_id/")
			jGenerated := strings.Contains(jPath, "/_plm_id/")
			if iGenerated != jGenerated {
				return iGenerated
			}
		}
		return len(paths[i]) < len(paths[j])
	})
	return paths[0]
}

func asInt(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case json.Number:
		i, _ := strconv.Atoi(n.String())
		return i
	case int:
		return n
	case string:
		i, _ := strconv.Atoi(strings.TrimSpace(n))
		return i
	}
	return 0
}

func asString(v any) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

func stringSlice(v any) []string {
	switch arr := v.(type) {
	case []any:
		out := make([]string, len(arr))
		for i, item := range arr {
			out[i] = asString(item)
		}
		return out
	case []string:
		return append([]string(nil), arr...)
	}
	return nil
}

// intTableSlice decodifica le tabelle RPG Maker XP convertite in JSON.
// I converter incontrati nel progetto possono rappresentare RPG::Table come
// array semplice, oggetto {data:[...]}, oppure come table multidimensionale
// con layers. Qui normalizziamo tutto in un vettore indicizzato per tile ID.
func intTableSlice(v any) []int {
	var out []int
	var walk func(any)
	walk = func(x any) {
		switch t := x.(type) {
		case nil:
			return
		case float64:
			out = append(out, int(t))
		case json.Number:
			i, err := strconv.Atoi(t.String())
			if err == nil {
				out = append(out, i)
			}
		case int:
			out = append(out, t)
		case string:
			if i, err := strconv.Atoi(strings.TrimSpace(t)); err == nil {
				out = append(out, i)
			}
		case []any:
			for _, item := range t {
				walk(item)
			}
		case []int:
			out = append(out, t...)
		case map[string]any:
			// Preferiamo i payload espliciti delle Table convertite, evitando
			// di includere width/height/dimensions tra i valori della tabella.
			for _, key := range []string{"data", "values", "layers", "table"} {
				if child := mapField(t, key); child != nil {
					walk(child)
					return
				}
			}
			// Alcuni exporter serializzano una tabella 1D come {"0":0,"1":15,...}.
			idx := make([]int, 0, len(t))
			byIndex := make(map[int]any, len(t))
			for k, item := range t {
				i, err := strconv.Atoi(k)
				if err != nil {
					continue
				}
				idx = append(idx, i)
				byIndex[i] = item
			}
			if len(idx) > 0 {
				sort.Ints(idx)
				last := -1
				for _, i := range idx {
					for last+1 < i {
						out = append(out, 0)
						last++
					}
					walk(byIndex[i])
					last = i
				}
			}
		}
	}
	walk(v)
	return out
}

func mapField(m map[string]any, names ...string) any {
	for _, name := range names {
		for k, v := range m {
			if strings.EqualFold(k, name) {
				return v
			}
		}
	}
	return nil
}

func metadataFromMap(m map[string]any, hintedID int) (tilesetMetadataJSON, bool) {
	id := asInt(mapField(m, "id", "tileset_id", "tilesetId", "ID"))
	if id == 0 {
		id = hintedID
	}
	name := asString(mapField(m, "name", "Name"))
	tilesetName := asString(mapField(m, "tileset_name", "tilesetName", "TilesetName", "tileset", "graphic", "filename"))
	if tilesetName == "" && name != "" {
		tilesetName = name
	}
	if id <= 0 || tilesetName == "" {
		return tilesetMetadataJSON{}, false
	}
	autotiles := stringSlice(mapField(m, "autotile_names", "autotileNames", "AutotileNames", "autotiles"))
	sourceTilesetName := asString(mapField(m, "source_tileset_name", "sourceTilesetName", "SourceTilesetName"))
	sourceAutotiles := stringSlice(mapField(m, "source_autotile_names", "sourceAutotileNames", "SourceAutotileNames"))
	passages := intTableSlice(mapField(m, "passages", "Passages", "passage", "passage_table", "passability"))
	priorities := intTableSlice(mapField(m, "priorities", "Priorities", "priority", "priority_table"))
	terrainTags := intTableSlice(mapField(m, "terrain_tags", "terrainTags", "TerrainTags", "terrain_tag", "terrainTag", "terrain_table"))
	return tilesetMetadataJSON{
		ID:                  id,
		Name:                name,
		TilesetName:         tilesetName,
		AutotileNames:       autotiles,
		SourceTilesetName:   sourceTilesetName,
		SourceAutotileNames: sourceAutotiles,
		Passages:            passages,
		Priorities:          priorities,
		TerrainTags:         terrainTags,
	}, true
}

func collectTilesetMetadata(v any, hintedID int, path string, out map[int]tilesetMetadataRecord) {
	switch t := v.(type) {
	case map[string]any:
		if md, ok := metadataFromMap(t, hintedID); ok {
			if _, exists := out[md.ID]; !exists {
				out[md.ID] = tilesetMetadataRecord{Data: md, Path: path}
			}
		}
		for k, child := range t {
			next := 0
			if n, err := strconv.Atoi(k); err == nil {
				next = n
			}
			collectTilesetMetadata(child, next, path, out)
		}
	case []any:
		for i, child := range t {
			collectTilesetMetadata(child, i, path, out)
		}
	}
}

func decodeTilesetMetadataFile(path string, hintedID int, out map[int]tilesetMetadataRecord) {
	b, err := os.ReadFile(path)
	if err != nil || len(b) == 0 {
		return
	}
	var root any
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.UseNumber()
	if err := dec.Decode(&root); err != nil {
		return
	}
	collectTilesetMetadata(root, hintedID, path, out)
}

func discoverTilesetMetadataDirs(project string) []string {
	candidates := []string{
		filepath.Join(project, "converted", "tilesets"),
		filepath.Join(project, "converted", "Tilesets"),
		filepath.Join(project, "converted", "data", "tilesets"),
		filepath.Join(project, "data", "tilesets"),
		filepath.Join(project, "Data", "Tilesets"),
		filepath.Join(project, "game", "data", "tilesets"),
		filepath.Join(project, "game", "converted", "tilesets"),
		filepath.Join(project, "tilesets"),
	}
	_ = filepath.WalkDir(project, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		if path != project && shouldSkipProjectDir(d.Name()) {
			return filepath.SkipDir
		}
		if strings.EqualFold(d.Name(), "tilesets") {
			candidates = append(candidates, path)
			return filepath.SkipDir
		}
		return nil
	})
	return uniqueExistingDirs(candidates)
}

func mergeTilesetMetadata(primary, fallback tilesetMetadataJSON) tilesetMetadataJSON {
	if primary.ID <= 0 {
		primary.ID = fallback.ID
	}
	if strings.TrimSpace(primary.Name) == "" {
		primary.Name = fallback.Name
	}
	if strings.TrimSpace(primary.TilesetName) == "" {
		primary.TilesetName = fallback.TilesetName
	}
	if strings.TrimSpace(primary.SourceTilesetName) == "" {
		primary.SourceTilesetName = fallback.SourceTilesetName
	}
	if len(primary.AutotileNames) == 0 {
		primary.AutotileNames = append([]string(nil), fallback.AutotileNames...)
	}
	if len(primary.SourceAutotileNames) == 0 {
		primary.SourceAutotileNames = append([]string(nil), fallback.SourceAutotileNames...)
	}
	if len(primary.Passages) == 0 {
		primary.Passages = append([]int(nil), fallback.Passages...)
	}
	if len(primary.Priorities) == 0 {
		primary.Priorities = append([]int(nil), fallback.Priorities...)
	}
	if len(primary.TerrainTags) == 0 {
		primary.TerrainTags = append([]int(nil), fallback.TerrainTags...)
	}
	return primary
}

// discoverEssentialsTilesetsRXData returns the authoritative RMXP/Essentials
// Tilesets.rxdata copies that may exist in a project. Converted PLM JSON is the
// primary source, but older/imported projects can legitimately lack it. In that
// case this compiled Essentials file is the only reliable source for the seven
// autotile slots of each tileset.
func discoverEssentialsTilesetsRXData(project string) []string {
	project = filepath.Clean(project)
	explicit := []string{
		filepath.Join(project, "Data", "Tilesets.rxdata"),
		filepath.Join(project, "data", "Tilesets.rxdata"),
		filepath.Join(project, "converted", "source_essentials", "Data", "Tilesets.rxdata"),
		filepath.Join(project, "source_essentials", "Data", "Tilesets.rxdata"),
	}

	seen := make(map[string]bool)
	out := make([]string, 0, len(explicit))
	add := func(p string) {
		p = filepath.Clean(p)
		if !exists(p) {
			return
		}
		key := strings.ToLower(p)
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, p)
	}
	for _, p := range explicit {
		add(p)
	}
	if len(out) > 0 {
		return out
	}

	// Some historical PLM imports kept the original project under an auxiliary
	// directory. Only if the canonical locations are missing, search for the
	// exact file name. Never descend into source/build/cache folders.
	_ = filepath.WalkDir(project, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if path != project && shouldSkipProjectDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.EqualFold(d.Name(), "Tilesets.rxdata") {
			add(path)
		}
		return nil
	})
	return out
}

func tilesetRubyObjectInt(obj *rubyObject, key string) int {
	if obj == nil {
		return 0
	}
	return asInt(obj.IVars[key])
}

func tilesetRubyObjectString(obj *rubyObject, key string) string {
	if obj == nil {
		return ""
	}
	switch v := obj.IVars[key].(type) {
	case string:
		return strings.TrimSpace(v)
	case nil:
		return ""
	default:
		return strings.TrimSpace(fmt.Sprint(v))
	}
}

func tilesetRubyObjectStringSlice(obj *rubyObject, key string) []string {
	if obj == nil {
		return nil
	}
	raw, ok := obj.IVars[key].([]any)
	if !ok {
		return nil
	}
	out := make([]string, len(raw))
	for i, item := range raw {
		if item != nil {
			out[i] = strings.TrimSpace(fmt.Sprint(item))
		}
	}
	return out
}

func tilesetRubyTableFlatInts(value any) []int {
	u, ok := value.(*rubyUserData)
	if !ok || u == nil || !strings.EqualFold(u.Class, "Table") || len(u.Data) < 20 {
		return nil
	}
	xs := int(int32(binary.LittleEndian.Uint32(u.Data[4:8])))
	ys := int(int32(binary.LittleEndian.Uint32(u.Data[8:12])))
	zs := int(int32(binary.LittleEndian.Uint32(u.Data[12:16])))
	size := int(int32(binary.LittleEndian.Uint32(u.Data[16:20])))
	if ys < 1 {
		ys = 1
	}
	if zs < 1 {
		zs = 1
	}
	count := xs * ys * zs
	if size > 0 {
		count = size
	}
	available := (len(u.Data) - 20) / 2
	if count <= 0 || count > available {
		count = available
	}
	out := make([]int, count)
	off := 20
	for i := 0; i < count && off+2 <= len(u.Data); i++ {
		out[i] = int(int16(binary.LittleEndian.Uint16(u.Data[off : off+2])))
		off += 2
	}
	return out
}

func tilesetMetadataFromRubyObject(obj *rubyObject, hintedID int) (tilesetMetadataJSON, bool) {
	if obj == nil {
		return tilesetMetadataJSON{}, false
	}
	id := tilesetRubyObjectInt(obj, "id")
	if id <= 0 {
		id = hintedID
	}
	if id <= 0 {
		return tilesetMetadataJSON{}, false
	}
	tilesetName := tilesetRubyObjectString(obj, "tileset_name")
	autotiles := tilesetRubyObjectStringSlice(obj, "autotile_names")
	return tilesetMetadataJSON{
		ID:                  id,
		Name:                tilesetRubyObjectString(obj, "name"),
		TilesetName:         tilesetName,
		AutotileNames:       append([]string(nil), autotiles...),
		SourceTilesetName:   tilesetName,
		SourceAutotileNames: append([]string(nil), autotiles...),
		Passages:            tilesetRubyTableFlatInts(obj.IVars["passages"]),
		Priorities:          tilesetRubyTableFlatInts(obj.IVars["priorities"]),
		TerrainTags:         tilesetRubyTableFlatInts(obj.IVars["terrain_tags"]),
	}, true
}

func mergeTilesetMetadataFromRXData(path string, out map[int]tilesetMetadataRecord) error {
	v, err := decodeRubyMarshalFile(path)
	if err != nil {
		return err
	}
	rows, ok := v.([]any)
	if !ok {
		return fmt.Errorf("Tilesets.rxdata: struttura inattesa")
	}
	merged := 0
	for i, row := range rows {
		obj, ok := row.(*rubyObject)
		if !ok || obj == nil {
			continue
		}
		md, ok := tilesetMetadataFromRubyObject(obj, i)
		if !ok {
			continue
		}
		if rec, exists := out[md.ID]; exists {
			rec.Data = mergeTilesetMetadata(rec.Data, md)
			if strings.TrimSpace(rec.Path) == "" {
				rec.Path = path
			}
			out[md.ID] = rec
		} else {
			out[md.ID] = tilesetMetadataRecord{Data: md, Path: path}
		}
		merged++
	}
	if merged > 0 {
		mapLogf("[TILESET] recuperati/arricchiti %d tileset da %s", merged, path)
	}
	return nil
}

func rebuildTilesetMetadataIndex(project string) {
	project = filepath.Clean(project)
	tilesetIndexProject = project
	tilesetMetadataByID = make(map[int]tilesetMetadataRecord)

	for _, dir := range discoverTilesetMetadataDirs(project) {
		ents, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range ents {
			if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".json") {
				continue
			}
			stem := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
			hint := 0
			if n, err := strconv.Atoi(strings.TrimLeft(stem, "0")); err == nil {
				hint = n
			}
			decodeTilesetMetadataFile(filepath.Join(dir, e.Name()), hint, tilesetMetadataByID)
		}
	}

	// Also support aggregate JSON files placed directly in converted/data.
	for _, path := range []string{
		filepath.Join(project, "converted", "Tilesets.json"),
		filepath.Join(project, "converted", "tilesets.json"),
		// Historical/current generic Essentials conversion path.  This file
		// contains the same RPG::Tileset records but was previously ignored by
		// the map editor, causing the tileset image to load with zero autotiles.
		filepath.Join(project, "converted", "data", "Tilesets.json"),
		filepath.Join(project, "converted", "data", "tilesets.json"),
		filepath.Join(project, "data", "Tilesets.json"),
		filepath.Join(project, "game", "data", "Tilesets.json"),
	} {
		if exists(path) {
			decodeTilesetMetadataFile(path, 0, tilesetMetadataByID)
		}
	}

	// Recovery/compatibility path: a real Essentials project defines the
	// tileset -> autotile association in Data/Tilesets.rxdata. JSON remains
	// authoritative when present; RXDATA only fills missing records/fields.
	for _, path := range discoverEssentialsTilesetsRXData(project) {
		if err := mergeTilesetMetadataFromRXData(path, tilesetMetadataByID); err != nil {
			mapLogf("[TILESET] impossibile leggere %s: %v", path, err)
		}
	}
}

func ensureTilesetMetadataIndex(project string) {
	project = filepath.Clean(project)
	if tilesetMetadataByID == nil || !strings.EqualFold(tilesetIndexProject, project) {
		rebuildTilesetMetadataIndex(project)
	}
}

// recoverTilesetMetadataFromNumericAssets reconstructs only the deterministic
// PLM mapping that can be proven from canonical numeric aliases.  It is used
// when a converted project has its map graphics but the converted/tilesets
// metadata file is missing/incomplete.  No guessed association with arbitrary
// PNG names is ever made: slot N is accepted only from <tileset>_<slot>.
func recoverTilesetMetadataFromNumericAssets(project string, id int) (tilesetMetadataJSON, string, bool) {
	if id <= 0 {
		return tilesetMetadataJSON{}, "", false
	}

	canonicalTilesetName := fmt.Sprintf("%03d", id)
	tilesetPath := canonicalGraphicsAsset(project, "Tilesets", canonicalTilesetName)
	if tilesetPath == "" {
		// A historical converted project can still have the original tileset
		// filename while already containing deterministic numeric autotiles.
		tilesetPath = fallbackTilesetImage(project, id)
	}
	if tilesetPath == "" {
		return tilesetMetadataJSON{}, "", false
	}

	autotiles := make([]string, 7)
	foundAutotile := false
	for slot := 1; slot <= 7; slot++ {
		name := fmt.Sprintf("%03d_%02d", id, slot)
		if p := canonicalGraphicsAsset(project, "Autotiles", name); p != "" {
			autotiles[slot-1] = name
			foundAutotile = true
		}
	}
	if !foundAutotile {
		return tilesetMetadataJSON{}, "", false
	}

	name := strings.TrimSuffix(filepath.Base(tilesetPath), filepath.Ext(tilesetPath))
	md := tilesetMetadataJSON{
		ID:            id,
		Name:          name,
		TilesetName:   name,
		AutotileNames: autotiles,
	}
	metadataOrigin := filepath.Join(filepath.Clean(project), "assets", "Graphics", "Autotiles")
	mapLogf("[TILESET] ID %d: metadati mancanti, recuperati %d slot dagli alias numerici canonici in %s", id, func() int {
		n := 0
		for _, v := range autotiles {
			if v != "" {
				n++
			}
		}
		return n
	}(), metadataOrigin)
	return md, metadataOrigin, true
}

func findTilesetMetadata(project string, id int) (tilesetMetadataJSON, string, error) {
	ensureTilesetMetadataIndex(project)
	if rec, ok := tilesetMetadataByID[id]; ok {
		return rec.Data, rec.Path, nil
	}

	// Files may have been generated by the converter while the same project is
	// already open. Rebuild once before trying deterministic recovery.
	rebuildTilesetMetadataIndex(project)
	if rec, ok := tilesetMetadataByID[id]; ok {
		return rec.Data, rec.Path, nil
	}

	// Critical recovery for converted projects: the converter assigns stable
	// names 030.png and 030_01..030_07.png. If the JSON metadata was lost, that
	// numeric mapping is sufficient to restore the autotile slots without
	// guessing or modifying any project file.
	if md, origin, ok := recoverTilesetMetadataFromNumericAssets(project, id); ok {
		tilesetMetadataByID[id] = tilesetMetadataRecord{Data: md, Path: origin}
		return md, origin, nil
	}

	return tilesetMetadataJSON{}, "", fmt.Errorf("metadati tileset ID %d non trovati: assenti JSON/RXDATA e alias numerici %03d_01..%03d_07", id, id, id)
}

func fallbackTilesetImage(project string, id int) string {
	// Le tavole importate da Strumenti -> Inserisci palette sono tileset
	// numerati reali. Se esiste pallette <id>.png, quella grafica ha
	// precedenza sul vecchio asset omonimo, mentre passages/priorities/
	// terrain_tags continuano a provenire dai metadati del Tileset ID.
	ensurePaletteCatalog(project)
	if p := paletteCatalogByID[id]; p != nil && strings.TrimSpace(p.File) != "" {
		abs := filepath.Join(filepath.Clean(project), filepath.FromSlash(p.File))
		if exists(abs) {
			return abs
		}
	}

	ensureAssetIndex(project)
	candidates := []string{
		fmt.Sprintf("pallette %d", id),
		fmt.Sprintf("tileset %d", id),
		fmt.Sprintf("tileset%d", id),
		fmt.Sprintf("tileset%02d", id),
		fmt.Sprintf("tileset%03d", id),
		strconv.Itoa(id),
		fmt.Sprintf("%02d", id),
		fmt.Sprintf("%03d", id),
	}
	for _, c := range candidates {
		if p := graphicsAsset(project, "Tilesets", c); p != "" {
			return p
		}
	}
	return ""
}

func loadTilesetDescriptor(project string, id int) (*TilesetDescriptor, error) {
	done := diagEnterWorker("loadTileset()")
	defer done()
	diagLogf("[WORKER] loadTileset id=%d project=%s", id, project)
	key := tilesetCacheKey(project, id)
	if cached := tilesetDescriptorCache[key]; cached != nil {
		mapLogf("[CACHE] tileset id=%d reused", id)
		return cached, nil
	}

	raw, metadataPath, metadataErr := findTilesetMetadata(project, id)
	if metadataErr != nil {
		if p := fallbackTilesetImage(project, id); p != "" {
			surf, err := loadPNGSurface(p)
			if err != nil {
				return nil, err
			}
			d := &TilesetDescriptor{ID: id, Name: filepath.Base(p), TilesetName: filepath.Base(p), Tileset: surf, TilesetPath: p}
			tilesetDescriptorCache[key] = d
			return d, nil
		}
		return nil, metadataErr
	}

	if raw.ID == 0 {
		raw.ID = id
	}
	if strings.TrimSpace(raw.TilesetName) == "" {
		raw.TilesetName = raw.Name
	}
	d := &TilesetDescriptor{
		ID:            raw.ID,
		Name:          raw.Name,
		TilesetName:   raw.TilesetName,
		AutotileNames: append([]string(nil), raw.AutotileNames...),
		Passages:      append([]int(nil), raw.Passages...),
		Priorities:    append([]int(nil), raw.Priorities...),
		TerrainTags:   append([]int(nil), raw.TerrainTags...),
		MetadataPath:  metadataPath,
	}

	// Una pallette numerata esplicitamente importata dall'utente sostituisce
	// la tavola grafica del corrispondente Tileset ID, ma NON i metadati
	// (collisioni, priorità, terrain tag, autotile).
	path := ""
	ensurePaletteCatalog(project)
	if p := paletteCatalogByID[id]; p != nil && strings.TrimSpace(p.File) != "" {
		path = filepath.Join(filepath.Clean(project), filepath.FromSlash(p.File))
	}
	if path == "" || !exists(path) {
		path = graphicsAsset(project, "Tilesets", raw.TilesetName)
	}
	// I progetti convertiti recenti usano l'alias numerico nel percorso canonico
	// assets/Graphics/Tilesets, ma mantengono il nome Essentials originale nei
	// metadati. Se l'alias non esiste, carica comunque la grafica originale.
	if (path == "" || !exists(path)) && strings.TrimSpace(raw.SourceTilesetName) != "" {
		path = graphicsAsset(project, "Tilesets", raw.SourceTilesetName)
	}
	if path == "" {
		path = fallbackTilesetImage(project, id)
	}
	if path == "" {
		return nil, fmt.Errorf("tileset %d: grafica %q non trovata (metadata: %s)", id, raw.TilesetName, metadataPath)
	}
	d.TilesetPath = path
	surf, err := loadPNGSurface(path)
	if err != nil {
		return nil, fmt.Errorf("tileset %q non leggibile: %w", path, err)
	}
	d.Tileset = surf

	autotileDone := diagEnterWorker("loadAutotiles()")
	slotCount := len(raw.AutotileNames)
	if len(raw.SourceAutotileNames) > slotCount {
		slotCount = len(raw.SourceAutotileNames)
	}
	// RPG Maker XP/Essentials prevede fino a 7 slot autotile. Non forziamo
	// slot aggiuntivi se i metadati non li dichiarano, ma quando esistono nomi
	// legacy e canonici li risolviamo entrambi.
	effectiveNames := make([]string, slotCount)
	for slot := 0; slot < slotCount; slot++ {
		canonicalName := ""
		if slot < len(raw.AutotileNames) {
			canonicalName = strings.TrimSpace(raw.AutotileNames[slot])
		}
		sourceName := ""
		if slot < len(raw.SourceAutotileNames) {
			sourceName = strings.TrimSpace(raw.SourceAutotileNames[slot])
		}
		numericName := fmt.Sprintf("%03d_%02d", raw.ID, slot+1)

		// Ordine: nome dichiarato nel metadata corrente -> nome Essentials
		// originale -> alias numerico deterministico. Il resolver prova prima
		// assets/Graphics/Autotiles, poi soltanto i percorsi legacy.
		candidates := []string{canonicalName, sourceName, numericName}
		seen := map[string]bool{}
		var at *PixelSurface
		resolvedName := ""
		resolvedPath := ""
		for _, candidate := range candidates {
			candidate = strings.TrimSpace(candidate)
			if candidate == "" {
				continue
			}
			key := strings.ToLower(candidate)
			if seen[key] {
				continue
			}
			seen[key] = true
			p := graphicsAsset(project, "Autotiles", candidate)
			if p == "" {
				continue
			}
			loaded, loadErr := loadPNGSurface(p)
			if loadErr != nil || loaded == nil {
				mapLogf("[ERROR] Autotile slot=%d candidate=%q non leggibile: %v", slot+1, candidate, loadErr)
				continue
			}
			at = loaded
			resolvedName = candidate
			resolvedPath = p
			break
		}

		if at != nil {
			effectiveNames[slot] = resolvedName
			mapLogf("[AUTOTILE] slot=%d name=%q path=%s size=%dx%d", slot+1, resolvedName, resolvedPath, at.Width, at.Height)
		} else {
			// Mantieni il nome dichiarato per la diagnostica/UI anche quando il
			// file manca; non inventare uno slot diverso.
			if canonicalName != "" {
				effectiveNames[slot] = canonicalName
			} else if sourceName != "" {
				effectiveNames[slot] = sourceName
			} else {
				effectiveNames[slot] = numericName
			}
			mapLogf("[ERROR] Autotile slot=%d non trovato; provati=%q,%q,%q", slot+1, canonicalName, sourceName, numericName)
		}
		d.Autotiles = append(d.Autotiles, at)
	}
	d.AutotileNames = effectiveNames
	autotileDone()
	// Manteniamo una cache contenuta: la maggior parte dei progetti usa pochi
	// tileset durante una singola sessione. Se cresce troppo, azzeriamo solo la
	// cache dei descriptor; currentTileset resta valido perché possiede i dati.
	if len(tilesetDescriptorCache) >= 12 {
		clearTilesetDescriptorCache()
	}
	tilesetDescriptorCache[key] = d
	return d, nil
}

func currentMapMaximumNormalTileID() int {
	if currentMapDoc == nil {
		return 0
	}
	maxID := 0
	for _, layer := range currentMapDoc.Table.Layers {
		for _, row := range layer {
			for _, id := range row {
				if id >= 384 && id > maxID {
					maxID = id
				}
			}
		}
	}
	return maxID
}

func loadCurrentTileset() {
	currentTileset = nil
	if currentMapDoc == nil || currentProject == "" {
		return
	}
	mapLogf("[TILESET] resolving id=%d project=%s", currentMapDoc.TilesetID, currentProject)
	d, err := loadTilesetDescriptor(currentProject, currentMapDoc.TilesetID)
	if err != nil {
		mapLogf("[ERROR] Tileset %d referenced by map was not found: %s", currentMapDoc.TilesetID, err)
		setText(hwndStatus, fmt.Sprintf("Mappa %03d caricata | tileset_id=%d | ERRORE: %s", currentMap.ID, currentMapDoc.TilesetID, err.Error()))
		populatePaletteControls()
		return
	}
	currentTileset = d
	resetMapRenderCacheForTileset(d)
	mapLogf("[TILESET] id=%d name=%q path=%s size=%dx%d metadata=%s", d.ID, d.TilesetName, d.TilesetPath, d.Tileset.Width, d.Tileset.Height, d.MetadataPath)
	loadedAT := 0
	for _, at := range d.Autotiles {
		if at != nil {
			loadedAT++
		}
	}
	mapLogf("[AUTOTILE] loaded %d/%d resources", loadedAT, len(d.Autotiles))

	maxID := currentMapMaximumNormalTileID()
	if maxID >= 384 {
		needed := maxID - 384 + 1
		available := normalTileCount()
		if available < needed {
			mapLogf("[ERROR] Tileset troppo corto: tile richiesto max=%d, tiles disponibili=%d, richiesti=%d", maxID, available, needed)
		}
	}
	populatePaletteControls()
	mapLogf("[RENDER] ready")
	setText(hwndStatus, fmt.Sprintf("Mappa %03d | Tileset %d: %s | autotile %d/%d", currentMap.ID, d.ID, d.TilesetName, loadedAT, len(d.Autotiles)))
}

func normalTileCount() int {
	if currentTileset == nil || currentTileset.Tileset == nil || currentTileset.Tileset.Width < 32 || currentTileset.Tileset.Height < 32 {
		return 0
	}
	cols := currentTileset.Tileset.Width / 32
	rows := currentTileset.Tileset.Height / 32
	return cols * rows
}

func normalTileIDAt(index int) int { return 384 + index }

func normalTileIndex(tileID int) int {
	if tileID < 384 {
		return -1
	}
	return tileID - 384
}

func tileDebugName(id int) string {
	if id == 0 {
		return "Vuoto"
	}
	if id >= 48 && id < 384 {
		return "Autotile " + strconv.Itoa((id-48)/48+1)
	}
	if id >= 384 {
		return "Tile " + strconv.Itoa(id)
	}
	return "Speciale " + strconv.Itoa(id)
}
