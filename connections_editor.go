//go:build windows

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
)

// ConnectionDocument is deliberately independent from the physical map path.
// Map IDs are global and remain stable even when a map is moved between
// converted/maps and converted/maps/regions/<REGION>/... .
//
// Il formato è lo stesso consumato dal runtime Python:
// schema plm.map_connections v1, ID stringa e due endpoint A/B.
// Ogni record è bidirezionale; non si salva un duplicato reciproco.
type ConnectionEndpoint struct {
	MapID  int    `json:"map_id"`
	Edge   string `json:"edge"`
	Offset int    `json:"offset"`
}

type MapConnection struct {
	ID      string             `json:"id"`
	Order   int                `json:"order"`
	Enabled bool               `json:"enabled"`
	A       ConnectionEndpoint `json:"a"`
	B       ConnectionEndpoint `json:"b"`
}

type ConnectionDocument struct {
	Schema      string          `json:"schema"`
	Version     int             `json:"version"`
	Meta        map[string]any  `json:"meta,omitempty"`
	Connections []MapConnection `json:"connections"`
}

type effectiveConnection struct {
	RecordIndex int
	RecordID    string
	TargetMapID int
	Direction   string
	Offset      int
	Reverse     bool
}

const (
	connectionSchema  = "plm.map_connections"
	connectionVersion = 1

	connectionClassName = "PLMStudioConnections05"

	idConnNumber     = 2300
	idConnDirection  = 2301
	idConnOffset     = 2302
	idConnTarget     = 2303
	idConnList       = 2304
	idConnNorth      = 2305
	idConnWest       = 2306
	idConnEast       = 2307
	idConnSouth      = 2308
	idConnAdd        = 2309
	idConnRemove     = 2310
	idConnSave       = 2311
	idConnClose      = 2312
	idConnCount      = 2313
	idConnTargetID   = 2314
	idConnTargetReg  = 2315
	idConnCurrentMap = 2316
)

var (
	connectionClassRegistered bool
	connectionWindow          syscall.Handle
	connectionOpen            bool
	connectionDoc             ConnectionDocument

	hwndConnNumber, hwndConnDirection, hwndConnOffset syscall.Handle
	hwndConnTarget, hwndConnList, hwndConnCount       syscall.Handle
	hwndConnTargetID, hwndConnTargetRegion            syscall.Handle
	hwndConnNorth, hwndConnWest, hwndConnEast         syscall.Handle
	hwndConnSouth, hwndConnCurrentMap                 syscall.Handle

	hwndConnGroupPreview, hwndConnGroupData       syscall.Handle
	hwndConnListLabel                             syscall.Handle
	hwndConnLabelCount, hwndConnLabelNumber       syscall.Handle
	hwndConnLabelDirection, hwndConnLabelOffset   syscall.Handle
	hwndConnLabelTarget, hwndConnLabelTargetID    syscall.Handle
	hwndConnLabelTargetRegion                     syscall.Handle
	hwndConnNoteOffset, hwndConnNoteBidirectional syscall.Handle
	hwndConnAdd, hwndConnRemove, hwndConnSave     syscall.Handle
	connectionInlineHandles                       []syscall.Handle
	connectionInlineCreated                       bool
	connectionLoadedMapID                         int

	connTargetMapIDs     []int
	connEffective        []effectiveConnection
	connSelectedRecord   = -1
	connSelectedReverse  bool
	connFieldsRefreshing bool

	connRuntimeScanProject string
	connRuntimeReaders     []string
	connRuntimeScanErr     string
)

func connectionDataPath() string {
	if currentProject == "" {
		return ""
	}
	return filepath.Join(currentProject, "converted", "map_connections.json")
}

func connectionProjectIsEssentialsSource() bool {
	if currentProject == "" {
		return false
	}
	if strings.EqualFold(currentProjectType, "RPG Maker XP / Pokémon Essentials") {
		return true
	}
	return exists(filepath.Join(currentProject, "Data", "MapInfos.rxdata")) &&
		(isDir(filepath.Join(currentProject, "PBS")) || isDir(filepath.Join(currentProject, "pbs")))
}

func connectionProjectIsConverted() bool {
	if currentProject == "" || connectionProjectIsEssentialsSource() {
		return false
	}
	return isDir(filepath.Join(currentProject, "converted")) ||
		exists(filepath.Join(currentProject, "plm_project.json")) ||
		exists(filepath.Join(currentProject, "main.py"))
}

func defaultConnectionDocument() ConnectionDocument {
	return ConnectionDocument{Version: connectionVersion, Schema: connectionSchema, Meta: map[string]any{}, Connections: []MapConnection{}}
}

type connectionPBSIssue struct {
	Path    string
	Line    int
	Message string
}

type connectionSourceInfo struct {
	Mode                   string
	Primary                string
	PBSFiles               []string
	DATPath                string
	PBSIssues              []connectionPBSIssue
	DATIssues              []connectionPBSIssue
	PBSCount               int
	DATCount               int
	Migrated               bool
	Fallback               string
	CompareMsg             string
	CanonicalCompareMsg    string
	CanonicalCompareSource string
}

var lastConnectionSourceInfo connectionSourceInfo

func connectionPBSFilesAt(root string) []string {
	if strings.TrimSpace(root) == "" {
		return nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	paths := make([]string, 0)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := strings.ToLower(e.Name())
		if !strings.HasPrefix(name, "map_connections") || !strings.EqualFold(filepath.Ext(name), ".txt") {
			continue
		}
		paths = append(paths, filepath.Join(root, e.Name()))
	}
	sort.Slice(paths, func(i, j int) bool { return strings.ToLower(paths[i]) < strings.ToLower(paths[j]) })
	return paths
}

func connectionPBSFilesForProject(converted bool) []string {
	if currentProject == "" {
		return nil
	}
	if converted {
		for _, root := range []string{filepath.Join(currentProject, "converted", "PBS"), filepath.Join(currentProject, "converted", "pbs"), filepath.Join(currentProject, "PBS"), filepath.Join(currentProject, "pbs")} {
			if paths := connectionPBSFilesAt(root); len(paths) > 0 {
				return paths
			}
		}
		return nil
	}
	for _, root := range []string{filepath.Join(currentProject, "PBS"), filepath.Join(currentProject, "pbs")} {
		if paths := connectionPBSFilesAt(root); len(paths) > 0 {
			return paths
		}
	}
	return nil
}

func connectionDATPathForProject(converted bool) string {
	if currentProject == "" {
		return ""
	}
	candidates := []string{}
	if converted {
		candidates = append(candidates,
			filepath.Join(currentProject, "converted", "source_compiled_data", "map_connections.dat"),
			filepath.Join(currentProject, "converted", "source_compiled_data", "Map_Connections.dat"),
			filepath.Join(currentProject, "Data", "map_connections.dat"),
			filepath.Join(currentProject, "data", "map_connections.dat"),
		)
	} else {
		candidates = append(candidates, filepath.Join(currentProject, "Data", "map_connections.dat"), filepath.Join(currentProject, "data", "map_connections.dat"))
	}
	for _, path := range candidates {
		if exists(path) {
			return path
		}
	}
	if len(candidates) > 0 {
		return candidates[0]
	}
	return ""
}

func connectionPBSPathLabel(path string) string {
	if currentProject != "" {
		if rel, err := filepath.Rel(currentProject, path); err == nil {
			return filepath.ToSlash(rel)
		}
	}
	return filepath.ToSlash(path)
}

func connectionPBSDirection(edge string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(edge)) {
	case "n", "north":
		return "up", true
	case "s", "south":
		return "down", true
	case "w", "west":
		return "left", true
	case "e", "east":
		return "right", true
	default:
		return "", false
	}
}
func connectionDirectionEdge(direction string) string {
	switch strings.ToLower(strings.TrimSpace(direction)) {
	case "up":
		return "NORTH"
	case "down":
		return "SOUTH"
	case "left":
		return "WEST"
	case "right":
		return "EAST"
	}
	return ""
}
func connectionEdgeDirection(edge string) (string, bool) { return connectionPBSDirection(edge) }
func connectionOppositeEdge(edge string) string {
	switch strings.ToUpper(strings.TrimSpace(edge)) {
	case "NORTH":
		return "SOUTH"
	case "SOUTH":
		return "NORTH"
	case "WEST":
		return "EAST"
	case "EAST":
		return "WEST"
	}
	return ""
}

func connectionAnyInt(v any) (int, bool) {
	switch t := v.(type) {
	case int:
		return t, true
	case int32:
		return int(t), true
	case int64:
		return int(t), true
	case float64:
		return int(t), true
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(t))
		return n, err == nil
	}
	return 0, false
}
func connectionAnyString(v any) string {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case int:
		return strconv.Itoa(t)
	case float64:
		return strconv.Itoa(int(t))
	case map[string]any:
		if sym, ok := t["_symbol"].(string); ok {
			return strings.TrimSpace(sym)
		}
	}
	return strings.TrimSpace(fmt.Sprint(v))
}

func appendConnectionRecord(doc *ConnectionDocument, nextID int, raw [6]any, path string, line int, issues *[]connectionPBSIssue) bool {
	map1, ok1 := connectionAnyInt(raw[0])
	pos1, okPos1 := connectionAnyInt(raw[2])
	map2, ok2 := connectionAnyInt(raw[3])
	pos2, okPos2 := connectionAnyInt(raw[5])
	if !ok1 || !ok2 || !okPos1 || !okPos2 || map1 <= 0 || map2 <= 0 {
		*issues = append(*issues, connectionPBSIssue{Path: path, Line: line, Message: "Map ID/offset non numerico o non valido"})
		return false
	}
	edge1Raw := connectionAnyString(raw[1])
	edge2Raw := connectionAnyString(raw[4])
	dir1, dirOK1 := connectionPBSDirection(edge1Raw)
	dir2, dirOK2 := connectionPBSDirection(edge2Raw)
	if !dirOK1 || !dirOK2 {
		if _, n1 := connectionAnyInt(raw[1]); n1 {
			if _, n2 := connectionAnyInt(raw[4]); n2 {
				*issues = append(*issues, connectionPBSIssue{Path: path, Line: line, Message: "connessione a coordinate Essentials non rappresentabile dal modello direzionale della Vista Connessioni"})
				return false
			}
		}
		*issues = append(*issues, connectionPBSIssue{Path: path, Line: line, Message: fmt.Sprintf("bordo non riconosciuto (%q / %q)", edge1Raw, edge2Raw)})
		return false
	}
	if oppositeDirection(dir1) != dir2 {
		*issues = append(*issues, connectionPBSIssue{Path: path, Line: line, Message: fmt.Sprintf("bordi incompatibili: %s deve collegarsi al bordo opposto, non a %s", edge1Raw, edge2Raw)})
		return false
	}
	doc.Connections = append(doc.Connections, MapConnection{ID: fmt.Sprintf("legacy-%04d", nextID), Order: nextID - 1, Enabled: true, A: ConnectionEndpoint{MapID: map1, Edge: connectionDirectionEdge(dir1), Offset: pos1}, B: ConnectionEndpoint{MapID: map2, Edge: connectionDirectionEdge(dir2), Offset: pos2}})
	return true
}

func loadConnectionPBSPaths(paths []string) (ConnectionDocument, []connectionPBSIssue) {
	doc := defaultConnectionDocument()
	issues := make([]connectionPBSIssue, 0)
	nextID := 1
	for _, path := range paths {
		b, err := os.ReadFile(path)
		if err != nil {
			issues = append(issues, connectionPBSIssue{Path: path, Message: "lettura fallita: " + err.Error()})
			continue
		}
		lines := strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n")
		for lineIndex, sourceLine := range lines {
			line := strings.TrimSpace(sourceLine)
			// I PBS ufficiali di Pokémon Essentials v20.1 possono iniziare con
			// BOM UTF-8. Se non viene rimosso, la prima riga commentata viene
			// scambiata per un record con una sola colonna.
			line = strings.TrimPrefix(line, "\uFEFF")
			if hash := strings.Index(line, "#"); hash >= 0 {
				line = strings.TrimSpace(line[:hash])
			}
			if line == "" {
				continue
			}
			parts := strings.Split(line, ",")
			if len(parts) != 6 {
				issues = append(issues, connectionPBSIssue{Path: path, Line: lineIndex + 1, Message: fmt.Sprintf("attese 6 colonne, trovate %d", len(parts))})
				continue
			}
			var raw [6]any
			for i := range parts {
				raw[i] = strings.TrimSpace(parts[i])
			}
			if appendConnectionRecord(&doc, nextID, raw, path, lineIndex+1, &issues) {
				nextID++
			}
		}
	}
	return doc, issues
}

func loadConnectionDAT(path string) (ConnectionDocument, []connectionPBSIssue) {
	doc := defaultConnectionDocument()
	issues := make([]connectionPBSIssue, 0)
	if path == "" || !exists(path) {
		return doc, issues
	}
	value, err := decodeRubyMarshalFile(path)
	if err != nil {
		issues = append(issues, connectionPBSIssue{Path: path, Message: "Ruby Marshal non leggibile: " + err.Error()})
		return doc, issues
	}
	records, ok := value.([]any)
	if !ok {
		issues = append(issues, connectionPBSIssue{Path: path, Message: "formato compilato inatteso: atteso array di connessioni"})
		return doc, issues
	}
	nextID := 1
	for i, entry := range records {
		parts, ok := entry.([]any)
		if !ok || len(parts) != 6 {
			issues = append(issues, connectionPBSIssue{Path: path, Line: i + 1, Message: "record compilato non valido: attesi 6 valori"})
			continue
		}
		var raw [6]any
		copy(raw[:], parts[:6])
		if appendConnectionRecord(&doc, nextID, raw, path, i+1, &issues) {
			nextID++
		}
	}
	return doc, issues
}

func connectionEndpointKey(e ConnectionEndpoint) string {
	return fmt.Sprintf("%d|%s|%d", e.MapID, strings.ToUpper(strings.TrimSpace(e.Edge)), e.Offset)
}
func connectionLogicalKey(c MapConnection) string {
	forward := connectionEndpointKey(c.A) + "<>" + connectionEndpointKey(c.B)
	reverse := connectionEndpointKey(c.B) + "<>" + connectionEndpointKey(c.A)
	if reverse < forward {
		return reverse
	}
	return forward
}
func compareConnectionDocuments(a, b ConnectionDocument) string {
	if len(a.Connections) != len(b.Connections) {
		return fmt.Sprintf("numero connessioni diverso: PBS=%d, DAT=%d", len(a.Connections), len(b.Connections))
	}
	ak := make([]string, 0, len(a.Connections))
	bk := make([]string, 0, len(b.Connections))
	for _, c := range a.Connections {
		ak = append(ak, connectionLogicalKey(c))
	}
	for _, c := range b.Connections {
		bk = append(bk, connectionLogicalKey(c))
	}
	sort.Strings(ak)
	sort.Strings(bk)
	for i := range ak {
		if ak[i] != bk[i] {
			return "contenuto PBS e Data/map_connections.dat non coincide"
		}
	}
	return ""
}

func writeConnectionJSONFile(path string, doc ConnectionDocument) error {
	doc.Version = connectionVersion
	doc.Schema = connectionSchema
	if doc.Meta == nil {
		doc.Meta = map[string]any{}
	}
	if doc.Connections == nil {
		doc.Connections = []MapConnection{}
	}
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0644); err != nil {
		return err
	}
	_ = os.Remove(path)
	return os.Rename(tmp, path)
}

func convertedConnectionFallback(info *connectionSourceInfo) (ConnectionDocument, bool) {
	info.PBSFiles = connectionPBSFilesForProject(true)
	pbsDoc, pbsIssues := loadConnectionPBSPaths(info.PBSFiles)
	info.PBSIssues = pbsIssues
	info.PBSCount = len(pbsDoc.Connections)
	info.DATPath = connectionDATPathForProject(true)
	datDoc, datIssues := loadConnectionDAT(info.DATPath)
	info.DATIssues = datIssues
	info.DATCount = len(datDoc.Connections)
	doc := defaultConnectionDocument()
	if len(info.PBSFiles) > 0 {
		doc = pbsDoc
		info.Fallback = connectionPBSPathLabel(info.PBSFiles[0])
		if doc.Meta == nil {
			doc.Meta = map[string]any{}
		}
		doc.Meta["imported_from"] = info.Fallback
	} else if info.DATPath != "" && exists(info.DATPath) {
		doc = datDoc
		info.Fallback = connectionPBSPathLabel(info.DATPath)
		if doc.Meta == nil {
			doc.Meta = map[string]any{}
		}
		doc.Meta["imported_from"] = info.Fallback
	}
	if len(info.PBSFiles) > 0 && info.DATPath != "" && exists(info.DATPath) {
		info.CompareMsg = compareConnectionDocuments(pbsDoc, datDoc)
	}
	canRepair := info.Fallback != "" && len(info.PBSIssues) == 0 && len(info.DATIssues) == 0 && info.CompareMsg == ""
	return doc, canRepair
}

type legacyEditorConnection struct {
	ID            int    `json:"id"`
	SourceMapID   int    `json:"source_map_id"`
	TargetMapID   int    `json:"target_map_id"`
	Direction     string `json:"direction"`
	Offset        int    `json:"offset"`
	Bidirectional bool   `json:"bidirectional"`
}
type legacyEditorConnectionDocument struct {
	Version     int                      `json:"version"`
	Schema      string                   `json:"schema"`
	Connections []legacyEditorConnection `json:"connections"`
}

func convertLegacyEditorConnectionJSON(data []byte) (ConnectionDocument, bool, error) {
	var legacy legacyEditorConnectionDocument
	if err := json.Unmarshal(data, &legacy); err != nil {
		return defaultConnectionDocument(), false, err
	}
	if strings.TrimSpace(legacy.Schema) != "pml.map_connections.v1" {
		return defaultConnectionDocument(), false, nil
	}
	doc := defaultConnectionDocument()
	doc.Meta["migrated_from_schema"] = legacy.Schema
	for i, row := range legacy.Connections {
		edge := connectionDirectionEdge(row.Direction)
		if edge == "" || row.SourceMapID <= 0 || row.TargetMapID <= 0 {
			return defaultConnectionDocument(), true, fmt.Errorf("record legacy %d non convertibile", i+1)
		}
		id := fmt.Sprintf("conn-%04d", row.ID)
		if row.ID <= 0 {
			id = fmt.Sprintf("legacy-editor-%04d", i+1)
		}
		doc.Connections = append(doc.Connections, MapConnection{ID: id, Order: i, Enabled: true, A: ConnectionEndpoint{MapID: row.SourceMapID, Edge: edge, Offset: 0}, B: ConnectionEndpoint{MapID: row.TargetMapID, Edge: connectionOppositeEdge(edge), Offset: row.Offset}})
	}
	return doc, true, nil
}

func loadConvertedConnectionDocument() (ConnectionDocument, error) {
	doc := defaultConnectionDocument()
	info := connectionSourceInfo{Mode: "converted", Primary: connectionDataPath()}
	path := info.Primary
	b, readErr := os.ReadFile(path)
	if readErr == nil {
		if err := json.Unmarshal(b, &doc); err == nil && strings.TrimSpace(doc.Schema) == connectionSchema && doc.Version == connectionVersion {
			if doc.Meta == nil {
				doc.Meta = map[string]any{}
			}
			if doc.Connections == nil {
				doc.Connections = []MapConnection{}
			}
			if err := validateConnectionDocument(doc); err != nil {
				lastConnectionSourceInfo = info
				return doc, fmt.Errorf("map_connections.json non valido: %w", err)
			}
			referenceDoc, _ := convertedConnectionFallback(&info)
			if info.Fallback != "" {
				info.CanonicalCompareSource = info.Fallback
				info.CanonicalCompareMsg = compareConnectionDocuments(doc, referenceDoc)
				info.Fallback = ""
			}
			lastConnectionSourceInfo = info
			return doc, nil
		}
		if migrated, recognized, convErr := convertLegacyEditorConnectionJSON(b); recognized {
			if convErr != nil {
				lastConnectionSourceInfo = info
				return doc, fmt.Errorf("map_connections.json legacy riconosciuto ma non convertibile: %w", convErr)
			}
			_ = os.WriteFile(path+".legacy_editor.bak", b, 0644)
			if err := writeConnectionJSONFile(path, migrated); err != nil {
				lastConnectionSourceInfo = info
				return doc, fmt.Errorf("migrazione map_connections.json legacy: %w", err)
			}
			info.Migrated = true
			info.Fallback = "schema editor legacy pml.map_connections.v1"
			lastConnectionSourceInfo = info
			return migrated, nil
		}
		fallbackDoc, canRepair := convertedConnectionFallback(&info)
		if canRepair {
			_ = os.WriteFile(path+".invalid.bak", b, 0644)
			if err := writeConnectionJSONFile(path, fallbackDoc); err != nil {
				lastConnectionSourceInfo = info
				return fallbackDoc, fmt.Errorf("riparazione map_connections.json: %w", err)
			}
			info.Migrated = true
			lastConnectionSourceInfo = info
			return fallbackDoc, nil
		}
		var syntaxErr error
		var raw any
		if err := json.Unmarshal(b, &raw); err != nil {
			syntaxErr = err
		} else {
			syntaxErr = fmt.Errorf("schema/struttura incompatibile con %s v%d", connectionSchema, connectionVersion)
		}
		lastConnectionSourceInfo = info
		return fallbackDoc, fmt.Errorf("map_connections.json non valido: %w; recupero automatico non sicuro", syntaxErr)
	}
	if !os.IsNotExist(readErr) {
		lastConnectionSourceInfo = info
		return doc, readErr
	}
	fallbackDoc, canMigrate := convertedConnectionFallback(&info)
	if canMigrate {
		if err := writeConnectionJSONFile(path, fallbackDoc); err != nil {
			lastConnectionSourceInfo = info
			return fallbackDoc, fmt.Errorf("creazione converted/map_connections.json: %w", err)
		}
		info.Migrated = true
	}
	lastConnectionSourceInfo = info
	return fallbackDoc, nil
}

func loadEssentialsSourceConnectionDocument() (ConnectionDocument, error) {
	info := connectionSourceInfo{Mode: "essentials"}
	info.PBSFiles = connectionPBSFilesForProject(false)
	pbsDoc, pbsIssues := loadConnectionPBSPaths(info.PBSFiles)
	info.PBSIssues = pbsIssues
	info.PBSCount = len(pbsDoc.Connections)
	info.DATPath = connectionDATPathForProject(false)
	datDoc, datIssues := loadConnectionDAT(info.DATPath)
	info.DATIssues = datIssues
	info.DATCount = len(datDoc.Connections)
	if len(info.PBSFiles) > 0 {
		info.Primary = info.PBSFiles[0]
		if info.DATPath != "" && exists(info.DATPath) {
			info.CompareMsg = compareConnectionDocuments(pbsDoc, datDoc)
		}
		lastConnectionSourceInfo = info
		return pbsDoc, nil
	}
	if info.DATPath != "" && exists(info.DATPath) {
		info.Primary = info.DATPath
		info.Fallback = "Data/map_connections.dat"
		lastConnectionSourceInfo = info
		return datDoc, nil
	}
	lastConnectionSourceInfo = info
	return defaultConnectionDocument(), nil
}

func buildConnectionDocumentFromEssentialsRoot(root string) (ConnectionDocument, []connectionPBSIssue, string) {
	pbsPaths := []string{}
	for _, pbsRoot := range []string{filepath.Join(root, "PBS"), filepath.Join(root, "pbs")} {
		if paths := connectionPBSFilesAt(pbsRoot); len(paths) > 0 {
			pbsPaths = paths
			break
		}
	}
	if len(pbsPaths) > 0 {
		doc, issues := loadConnectionPBSPaths(pbsPaths)
		if doc.Meta == nil {
			doc.Meta = map[string]any{}
		}
		doc.Meta["imported_from"] = "PBS/map_connections.txt"
		return doc, issues, "PBS"
	}
	for _, datPath := range []string{filepath.Join(root, "Data", "map_connections.dat"), filepath.Join(root, "data", "map_connections.dat")} {
		if exists(datPath) {
			doc, issues := loadConnectionDAT(datPath)
			return doc, issues, "DAT"
		}
	}
	return defaultConnectionDocument(), nil, ""
}

func loadConnectionDocument() (ConnectionDocument, error) {
	if currentProject == "" {
		return defaultConnectionDocument(), fmt.Errorf("nessun progetto aperto")
	}
	if connectionProjectIsEssentialsSource() {
		return loadEssentialsSourceConnectionDocument()
	}
	return loadConvertedConnectionDocument()
}

func validateConnectionDocument(doc ConnectionDocument) error {
	if strings.TrimSpace(doc.Schema) != connectionSchema {
		return fmt.Errorf("schema connessioni non valido: %q (atteso %q)", doc.Schema, connectionSchema)
	}
	if doc.Version != connectionVersion {
		return fmt.Errorf("versione connessioni non valida: %d (attesa %d)", doc.Version, connectionVersion)
	}
	seenID := map[string]bool{}
	for i, c := range doc.Connections {
		id := strings.TrimSpace(c.ID)
		if id == "" {
			return fmt.Errorf("connessione %d: ID interno vuoto", i+1)
		}
		if seenID[id] {
			return fmt.Errorf("ID connessione duplicato: %s", id)
		}
		seenID[id] = true
		if c.A.MapID <= 0 || c.B.MapID <= 0 {
			return fmt.Errorf("connessione %s: Map ID non valido", id)
		}
		if c.A.MapID == c.B.MapID {
			return fmt.Errorf("connessione %s: una mappa non può collegarsi a se stessa", id)
		}
		if findMapEntryByID(c.A.MapID) == nil {
			return fmt.Errorf("connessione %s: mappa %03d non trovata", id, c.A.MapID)
		}
		if findMapEntryByID(c.B.MapID) == nil {
			return fmt.Errorf("connessione %s: mappa %03d non trovata", id, c.B.MapID)
		}
		dirA, okA := connectionEdgeDirection(c.A.Edge)
		dirB, okB := connectionEdgeDirection(c.B.Edge)
		if !okA || !okB {
			return fmt.Errorf("connessione %s: bordo non valido (%q / %q)", id, c.A.Edge, c.B.Edge)
		}
		if oppositeDirection(dirA) != dirB {
			return fmt.Errorf("connessione %s: bordi non opposti (%s / %s)", id, c.A.Edge, c.B.Edge)
		}
	}
	return nil
}

func saveConnectionDocument(doc ConnectionDocument) error {
	if currentProject == "" {
		return fmt.Errorf("nessun progetto aperto")
	}
	if err := ensureProjectMapFilesystemSafe("Salvataggio connessioni"); err != nil {
		return err
	}
	doc.Version = connectionVersion
	doc.Schema = connectionSchema
	if err := validateConnectionDocument(doc); err != nil {
		return err
	}
	path := connectionDataPath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	_ = os.Remove(tmp)
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	if _, err = f.Write(b); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if old, readErr := os.ReadFile(path); readErr == nil {
		_ = os.WriteFile(path+".bak", old, 0644)
	}
	_ = os.Remove(path)
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func findMapEntryByID(id int) *MapEntry {
	for i := range maps {
		if maps[i].ID == id {
			return &maps[i]
		}
	}
	return nil
}
func mapDisplayName(id int) string {
	if m := findMapEntryByID(id); m != nil {
		return fmt.Sprintf("%03d - %s", m.ID, m.Name)
	}
	return fmt.Sprintf("%03d", id)
}
func mapRegionDisplay(id int) string {
	m := findMapEntryByID(id)
	if m == nil || strings.TrimSpace(m.RegionID) == "" {
		return "NON ASSEGNATA"
	}
	return strings.ToUpper(m.RegionID)
}
func oppositeDirection(d string) string {
	switch d {
	case "up":
		return "down"
	case "down":
		return "up"
	case "left":
		return "right"
	case "right":
		return "left"
	}
	return d
}
func directionDisplay(d string) string {
	switch d {
	case "up":
		return "Su"
	case "down":
		return "Giù"
	case "left":
		return "Sinistra"
	case "right":
		return "Destra"
	}
	return d
}
func directionIndex(d string) int {
	switch d {
	case "up":
		return 0
	case "down":
		return 1
	case "left":
		return 2
	case "right":
		return 3
	}
	return 1
}
func directionFromIndex(i int) string {
	switch i {
	case 0:
		return "up"
	case 1:
		return "down"
	case 2:
		return "left"
	case 3:
		return "right"
	}
	return "down"
}

func effectiveConnectionsForMap(doc ConnectionDocument, mapID int) []effectiveConnection {
	out := make([]effectiveConnection, 0)
	for i, c := range doc.Connections {
		if !c.Enabled {
			continue
		}
		if c.A.MapID == mapID {
			d, ok := connectionEdgeDirection(c.A.Edge)
			if !ok {
				continue
			}
			out = append(out, effectiveConnection{RecordIndex: i, RecordID: c.ID, TargetMapID: c.B.MapID, Direction: d, Offset: c.B.Offset - c.A.Offset})
		} else if c.B.MapID == mapID {
			d, ok := connectionEdgeDirection(c.B.Edge)
			if !ok {
				continue
			}
			out = append(out, effectiveConnection{RecordIndex: i, RecordID: c.ID, TargetMapID: c.A.MapID, Direction: d, Offset: c.A.Offset - c.B.Offset, Reverse: true})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		ci, cj := doc.Connections[out[i].RecordIndex], doc.Connections[out[j].RecordIndex]
		if ci.Order != cj.Order {
			return ci.Order < cj.Order
		}
		return out[i].RecordID < out[j].RecordID
	})
	return out
}
func nextConnectionID(doc ConnectionDocument) string {
	used := map[string]bool{}
	for _, c := range doc.Connections {
		used[c.ID] = true
	}
	for i := 1; ; i++ {
		id := fmt.Sprintf("conn-%04d", i)
		if !used[id] {
			return id
		}
	}
}
func nextConnectionOrder(doc ConnectionDocument) int {
	maxOrder := -1
	for _, c := range doc.Connections {
		if c.Order > maxOrder {
			maxOrder = c.Order
		}
	}
	return maxOrder + 1
}
func connectionCountForCurrentMap() int {
	if currentMap == nil || currentProject == "" {
		return 0
	}
	doc, err := loadConnectionDocument()
	if err != nil {
		return 0
	}
	return len(effectiveConnectionsForMap(doc, currentMap.ID))
}
func connectionsViewPlaceholder() string {
	if currentMap == nil {
		return "Connessioni\r\n\r\nSeleziona una mappa, poi usa il pulsante con le quattro frecce."
	}
	return fmt.Sprintf("Connessioni mappa %03d - %s\r\n\r\nConnessioni configurate: %d\r\n\r\nIl gestore supporta più connessioni anche sulla stessa direzione.", currentMap.ID, currentMap.Name, connectionCountForCurrentMap())
}
func connectionsCanvasMessage() string { return "Gestore connessioni Advance Map-style." }
func connectionTargetIndexByMapID(id int) int {
	for i, mapID := range connTargetMapIDs {
		if mapID == id {
			return i
		}
	}
	return -1
}
func connectionEffectiveIndexByRecord(recordIndex int) int {
	for i, c := range connEffective {
		if c.RecordIndex == recordIndex {
			return i
		}
	}
	return -1
}

func populateConnectionTargetCombo() {
	pSendMessageW.Call(uintptr(hwndConnTarget), CB_RESETCONTENT, 0, 0)
	connTargetMapIDs = connTargetMapIDs[:0]
	entries := append([]MapEntry(nil), maps...)
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })
	for _, m := range entries {
		if currentMap != nil && m.ID == currentMap.ID {
			continue
		}
		label := fmt.Sprintf("%03d - %s", m.ID, m.Name)
		if strings.TrimSpace(m.RegionID) != "" {
			label += " [" + strings.ToUpper(m.RegionID) + "]"
		} else {
			label += " [NON ASSEGNATA]"
		}
		comboAdd(hwndConnTarget, label)
		connTargetMapIDs = append(connTargetMapIDs, m.ID)
	}
}

func selectedTargetMapID() int {
	i := comboSel(hwndConnTarget)
	if i < 0 || i >= len(connTargetMapIDs) {
		return 0
	}
	return connTargetMapIDs[i]
}

func updateConnectionTargetInfo() {
	id := selectedTargetMapID()
	if id <= 0 {
		setText(hwndConnTargetID, "-")
		setText(hwndConnTargetRegion, "-")
		return
	}
	setText(hwndConnTargetID, fmt.Sprintf("%03d", id))
	setText(hwndConnTargetRegion, mapRegionDisplay(id))
}

func setConnectionDirectionButtons(direction string) {
	pairs := []struct {
		h syscall.Handle
		d string
	}{{hwndConnNorth, "up"}, {hwndConnSouth, "down"}, {hwndConnWest, "left"}, {hwndConnEast, "right"}}
	for _, p := range pairs {
		state := uintptr(BST_UNCHECKED)
		if p.d == direction {
			state = BST_CHECKED
		}
		pSendMessageW.Call(uintptr(p.h), BM_SETCHECK, state, 0)
	}
}

func connectionRuntimeProjectRoots() []string {
	if strings.TrimSpace(currentProject) == "" {
		return nil
	}

	seen := map[string]bool{}
	roots := make([]string, 0, 6)
	add := func(path string) {
		path = filepath.Clean(strings.TrimSpace(path))
		if path == "" || !isDir(path) {
			return
		}
		abs, err := filepath.Abs(path)
		if err == nil {
			path = filepath.Clean(abs)
		}
		key := strings.ToLower(path)
		if seen[key] {
			return
		}
		seen[key] = true
		roots = append(roots, path)
	}
	looksLikeRuntimeRoot := func(path string) bool {
		if strings.TrimSpace(path) == "" || !isDir(path) {
			return false
		}
		return exists(filepath.Join(path, "converted", "map_connections.json")) ||
			exists(filepath.Join(path, "game", "map_connections.py")) ||
			exists(filepath.Join(path, "game", "map_connections_backend.py")) ||
			exists(filepath.Join(path, "main.py")) ||
			exists(filepath.Join(path, "plm_project.json"))
	}

	clean := filepath.Clean(currentProject)
	add(clean)

	// Usa anche il Root dichiarato dal manifest PLM. Questo copre i progetti
	// già convertiti nei quali la cartella aperta dall'editor è un contenitore
	// ma il runtime vive nel root dichiarato dal progetto.
	manifestPath := filepath.Join(clean, "plm_project.json")
	if b, err := os.ReadFile(manifestPath); err == nil {
		var manifest struct {
			Root string `json:"Root"`
		}
		if json.Unmarshal(b, &manifest) == nil && strings.TrimSpace(manifest.Root) != "" {
			root := strings.TrimSpace(manifest.Root)
			if !filepath.IsAbs(root) {
				root = filepath.Join(clean, root)
			}
			add(root)
		}
	}

	// Se l'editor è stato aperto da converted/, converted/maps/ o da una
	// sottocartella equivalente, risali solo fino a quattro livelli e aggiungi
	// esclusivamente directory che mostrano marker reali del runtime/progetto.
	cursor := clean
	for i := 0; i < 4; i++ {
		parent := filepath.Dir(cursor)
		if parent == cursor {
			break
		}
		if looksLikeRuntimeRoot(parent) {
			add(parent)
		}
		cursor = parent
	}

	// Alcuni wrapper/progetti di lavoro contengono il gioco convertito in una
	// singola sottocartella. Esamina soltanto i figli immediati e solo se hanno
	// marker forti, evitando scansioni ricorsive del disco.
	if entries, err := os.ReadDir(clean); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			child := filepath.Join(clean, entry.Name())
			if looksLikeRuntimeRoot(child) &&
				(exists(filepath.Join(child, "converted", "map_connections.json")) ||
					exists(filepath.Join(child, "game", "map_connections.py")) ||
					exists(filepath.Join(child, "game", "map_connections_backend.py"))) {
				add(child)
			}
		}
	}

	return roots
}

func connectionRuntimeFileReferencesConnections(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || info.Size() > 2*1024*1024 {
		return false
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	text := strings.ToLower(string(b))
	// Accetta sia il path/schema canonico sia i moduli e le API runtime che
	// consumano il registro connessioni. È una verifica di collegamento, non un
	// sostituto del test runtime vero.
	return strings.Contains(text, "map_connections.json") ||
		strings.Contains(text, "plm.map_connections") ||
		strings.Contains(text, "map_connections_backend") ||
		strings.Contains(text, "map_connection_registry") ||
		strings.Contains(text, "from game.map_connections") ||
		strings.Contains(text, "import game.map_connections")
}

func connectionRuntimeReaderFiles() ([]string, string) {
	if strings.TrimSpace(currentProject) == "" {
		return nil, "nessun progetto aperto"
	}

	// Rifai sempre il controllo: durante lo sviluppo i file runtime possono
	// cambiare senza che cambi il path del progetto.
	connRuntimeScanProject = currentProject
	connRuntimeReaders = nil
	connRuntimeScanErr = ""

	roots := connectionRuntimeProjectRoots()
	if len(roots) == 0 {
		return nil, "root progetto non disponibile"
	}

	seenFiles := map[string]bool{}
	addFile := func(projectRoot, path string) {
		if !strings.EqualFold(filepath.Ext(path), ".py") || !connectionRuntimeFileReferencesConnections(path) {
			return
		}
		clean := filepath.Clean(path)
		abs, err := filepath.Abs(clean)
		if err == nil {
			clean = filepath.Clean(abs)
		}
		key := strings.ToLower(clean)
		if seenFiles[key] {
			return
		}
		seenFiles[key] = true
		rel, err := filepath.Rel(projectRoot, clean)
		if err != nil || strings.HasPrefix(rel, "..") {
			rel = clean
		}
		connRuntimeReaders = append(connRuntimeReaders, filepath.ToSlash(rel))
	}

	// Controllo diretto dei consumer canonici. Questo viene eseguito prima del
	// Walk ed elimina falsi negativi dovuti alla struttura del progetto.
	directCandidates := []string{
		filepath.Join("game", "map_connections.py"),
		filepath.Join("game", "map_connections_backend.py"),
		filepath.Join("game", "map_scene.py"),
		"main.py",
		filepath.Join("runtime", "map_connections.py"),
		filepath.Join("engine", "map_connections.py"),
	}
	for _, root := range roots {
		for _, rel := range directCandidates {
			path := filepath.Join(root, rel)
			if exists(path) {
				addFile(root, path)
			}
		}
	}

	codeDirs := []string{"game", "runtime", "engine", "src", "scripts"}
	skipDir := map[string]bool{
		".git": true, "__pycache__": true, "assets": true, "graphics": true,
		"audio": true, "dist": true, "build": true, ".venv": true, "venv": true,
		"lib": true, "site-packages": true,
	}

	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			if connRuntimeScanErr == "" {
				connRuntimeScanErr = err.Error()
			}
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".py") {
				continue
			}
			addFile(root, filepath.Join(root, entry.Name()))
		}

		for _, dirName := range codeDirs {
			codeRoot := filepath.Join(root, dirName)
			if !isDir(codeRoot) {
				continue
			}
			walkErr := filepath.Walk(codeRoot, func(path string, info os.FileInfo, walkErr error) error {
				if walkErr != nil {
					return nil
				}
				if info == nil {
					return nil
				}
				if info.IsDir() {
					if path != codeRoot && skipDir[strings.ToLower(info.Name())] {
						return filepath.SkipDir
					}
					return nil
				}
				addFile(root, path)
				return nil
			})
			if walkErr != nil && connRuntimeScanErr == "" {
				connRuntimeScanErr = walkErr.Error()
			}
		}
	}

	sort.Strings(connRuntimeReaders)
	if len(connRuntimeReaders) > 0 {
		// Un reader trovato rende irrilevanti eventuali errori non bloccanti su
		// root alternative esaminate durante la ricerca.
		connRuntimeScanErr = ""
	}
	return append([]string(nil), connRuntimeReaders...), connRuntimeScanErr
}

func connectionMapDimensions(mapID int) (int, int, string) {
	mapsRoot := canonicalConvertedMapsPath()
	if mapsRoot == "" {
		return 0, 0, "cartella converted/maps non disponibile"
	}
	path, err := resolveMapPathByID(mapsRoot, mapID)
	if err != nil {
		return 0, 0, err.Error()
	}
	doc, err := loadMapDocument(path)
	if err != nil {
		return 0, 0, fmt.Sprintf("%s: %v", filepath.Base(path), err)
	}
	return doc.Table.Width, doc.Table.Height, ""
}

func connectionSharedBorderOverlap(c MapConnection) (int, string) {
	aw, ah, aerr := connectionMapDimensions(c.A.MapID)
	if aerr != "" {
		return 0, fmt.Sprintf("Map%03d: %s", c.A.MapID, aerr)
	}
	bw, bh, berr := connectionMapDimensions(c.B.MapID)
	if berr != "" {
		return 0, fmt.Sprintf("Map%03d: %s", c.B.MapID, berr)
	}
	dirA, okA := connectionEdgeDirection(c.A.Edge)
	dirB, okB := connectionEdgeDirection(c.B.Edge)
	if !okA || !okB || oppositeDirection(dirA) != dirB {
		return 0, fmt.Sprintf("bordi non validi/non opposti: %s <-> %s", c.A.Edge, c.B.Edge)
	}
	aLen, bLen := aw, bw
	if dirA == "left" || dirA == "right" {
		aLen, bLen = ah, bh
	}
	// Il runtime trasforma la coordinata lungo il bordo come:
	// target = source + (B.offset - A.offset).
	// Calcoliamo quindi quanta parte dei due bordi cade realmente in comune.
	delta := c.B.Offset - c.A.Offset
	start := 0
	if -delta > start {
		start = -delta
	}
	end := aLen
	if bLen-delta < end {
		end = bLen - delta
	}
	if end <= start {
		return 0, ""
	}
	return end - start, ""
}

func connectionIssueDiagnosticLines(prefix string, issues []connectionPBSIssue) []string {
	out := make([]string, 0, len(issues))
	for _, issue := range issues {
		where := connectionPBSPathLabel(issue.Path)
		if issue.Line > 0 {
			where += fmt.Sprintf(":%d", issue.Line)
		}
		out = append(out, fmt.Sprintf("[ERRORE] %s %s: %s", prefix, where, issue.Message))
	}
	return out
}

// connectionIsLegacyImported identifica i record prodotti da import/migrazione.
// I record legacy/layout possono descrivere il posizionamento senza esporre
// necessariamente un tratto di bordo attraversabile.
func connectionIsLegacyImported(c MapConnection) bool {
	id := strings.ToLower(strings.TrimSpace(c.ID))
	return strings.HasPrefix(id, "legacy-")
}

func connectionDocumentDiagnosticLines(doc ConnectionDocument) []string {
	lines := make([]string, 0, 48)
	info := lastConnectionSourceInfo

	if connectionProjectIsEssentialsSource() {
		lines = append(lines, "[SORGENTE] Progetto Pokémon Essentials non convertito.")
		if len(info.PBSFiles) > 0 {
			for _, path := range info.PBSFiles {
				lines = append(lines, "[FILE PBS] "+connectionPBSPathLabel(path))
			}
			lines = append(lines, fmt.Sprintf("[OK] Connessioni leggibili dai PBS: %d.", info.PBSCount))
		} else {
			lines = append(lines, "[ATTENZIONE] PBS/map_connections.txt non trovato.")
		}
		lines = append(lines, connectionIssueDiagnosticLines("PBS", info.PBSIssues)...)
		if info.DATPath != "" && exists(info.DATPath) {
			lines = append(lines, "[FILE DAT] "+connectionPBSPathLabel(info.DATPath))
			lines = append(lines, fmt.Sprintf("[OK] Connessioni leggibili dal DAT compilato: %d.", info.DATCount))
			lines = append(lines, connectionIssueDiagnosticLines("DAT", info.DATIssues)...)
			if info.CompareMsg == "" && len(info.PBSFiles) > 0 && len(info.PBSIssues) == 0 && len(info.DATIssues) == 0 {
				lines = append(lines, "[OK] PBS e Data/map_connections.dat coincidono.")
			} else if info.CompareMsg != "" {
				lines = append(lines, "[ERRORE] PBS e Data/map_connections.dat non coincidono: "+info.CompareMsg+".")
			}
		} else {
			lines = append(lines, "[ATTENZIONE] Data/map_connections.dat non trovato: il PBS non può essere confrontato con il dato compilato.")
		}
	} else {
		path := connectionDataPath()
		lines = append(lines, "[SORGENTE] Progetto PLM convertito.")
		if path == "" {
			return append(lines, "[ERRORE] Percorso configurazione connessioni non disponibile.")
		}
		if rel, err := filepath.Rel(currentProject, path); err == nil {
			lines = append(lines, "[FILE JSON] "+filepath.ToSlash(rel))
		}
		if _, err := os.Stat(path); err == nil {
			if strings.TrimSpace(doc.Schema) == connectionSchema {
				lines = append(lines, "[OK] Schema condiviso Editor/Runtime: "+connectionSchema+".")
			} else {
				lines = append(lines, fmt.Sprintf("[ERRORE] Schema file=%q, atteso=%q.", doc.Schema, connectionSchema))
			}
			if doc.Version == connectionVersion {
				lines = append(lines, fmt.Sprintf("[OK] Versione map_connections.json: %d.", doc.Version))
			} else {
				lines = append(lines, fmt.Sprintf("[ERRORE] Versione file=%d, codice editor/runtime=%d.", doc.Version, connectionVersion))
			}
		} else if os.IsNotExist(err) {
			lines = append(lines, "[ERRORE] converted/map_connections.json non esiste.")
		} else {
			lines = append(lines, "[ERRORE] Lettura map_connections.json: "+err.Error())
		}
		if info.Migrated {
			lines = append(lines, "[OK] JSON canonico ricostruito dai dati Essentials conservati, dopo backup del file incompatibile/assente.")
		}
		if info.Fallback != "" && !info.Migrated {
			lines = append(lines, "[ATTENZIONE] Lettura di recupero da "+info.Fallback+"; il JSON non è stato sovrascritto perché la ricostruzione non era verificabile.")
		}
		for _, p := range info.PBSFiles {
			lines = append(lines, "[FALLBACK PBS] "+connectionPBSPathLabel(p))
		}
		if info.DATPath != "" && exists(info.DATPath) {
			lines = append(lines, "[FALLBACK DAT] "+connectionPBSPathLabel(info.DATPath))
		}
		lines = append(lines, connectionIssueDiagnosticLines("PBS", info.PBSIssues)...)
		lines = append(lines, connectionIssueDiagnosticLines("DAT", info.DATIssues)...)
		if info.CompareMsg != "" {
			lines = append(lines, "[ERRORE] Dati Essentials conservati non coerenti: "+info.CompareMsg+".")
		}
		if info.CanonicalCompareSource != "" {
			if info.CanonicalCompareMsg == "" {
				lines = append(lines, "[OK] JSON canonico e "+info.CanonicalCompareSource+" descrivono le stesse connessioni.")
			} else {
				lines = append(lines, "[ERRORE] JSON canonico e "+info.CanonicalCompareSource+" non sono sincronizzati: "+info.CanonicalCompareMsg+".")
			}
		}
	}

	seenID := map[string]int{}
	logical := map[string]string{}
	for i, c := range doc.Connections {
		row := i + 1
		id := strings.TrimSpace(c.ID)
		if id == "" {
			lines = append(lines, fmt.Sprintf("[ERRORE] Riga %d: ID connessione vuoto.", row))
			id = fmt.Sprintf("riga-%d", row)
		} else if previous, ok := seenID[id]; ok {
			lines = append(lines, fmt.Sprintf("[ERRORE] ID %s duplicato (righe %d e %d).", id, previous, row))
		} else {
			seenID[id] = row
		}
		if c.A.MapID <= 0 || c.B.MapID <= 0 {
			lines = append(lines, fmt.Sprintf("[ERRORE] Connessione %s: Map ID non valido (%d <-> %d).", id, c.A.MapID, c.B.MapID))
			continue
		}
		if c.A.MapID == c.B.MapID {
			lines = append(lines, fmt.Sprintf("[ERRORE] Connessione %s: la mappa %03d collega se stessa.", id, c.A.MapID))
		}
		if findMapEntryByID(c.A.MapID) == nil {
			lines = append(lines, fmt.Sprintf("[ERRORE] Connessione %s: Map%03d non indicizzata dal progetto.", id, c.A.MapID))
		}
		if findMapEntryByID(c.B.MapID) == nil {
			lines = append(lines, fmt.Sprintf("[ERRORE] Connessione %s: Map%03d non indicizzata dal progetto.", id, c.B.MapID))
		}
		dirA, okA := connectionEdgeDirection(c.A.Edge)
		dirB, okB := connectionEdgeDirection(c.B.Edge)
		if !okA || !okB {
			lines = append(lines, fmt.Sprintf("[ERRORE] Connessione %s: bordo non riconosciuto (%q / %q).", id, c.A.Edge, c.B.Edge))
			continue
		}
		if oppositeDirection(dirA) != dirB {
			lines = append(lines, fmt.Sprintf("[ERRORE] Connessione %s: bordi non opposti (%s / %s).", id, c.A.Edge, c.B.Edge))
		}
		key := connectionLogicalKey(c)
		if previous, ok := logical[key]; ok {
			lines = append(lines, fmt.Sprintf("[ERRORE] Connessioni %s e %s descrivono lo stesso collegamento bidirezionale.", previous, id))
		} else {
			logical[key] = id
		}
		if !c.Enabled {
			lines = append(lines, fmt.Sprintf("[INFO] Connessione %s disabilitata: il runtime la ignora.", id))
		}
		if !connectionProjectIsEssentialsSource() && c.Enabled {
			overlap, overlapErr := connectionSharedBorderOverlap(c)
			if overlapErr != "" {
				lines = append(lines, fmt.Sprintf("[ERRORE] Connessione %s: %s", id, overlapErr))
			} else if overlap <= 0 {
				delta := c.B.Offset - c.A.Offset
				if connectionIsLegacyImported(c) {
					lines = append(lines, fmt.Sprintf("[INFO] Connessione %s: offset relativo %+d non espone un tratto attraversabile tra Map%03d e Map%03d; record legacy/layout mantenuto senza bloccare altre connessioni sullo stesso lato.", id, delta, c.A.MapID, c.B.MapID))
				} else {
					lines = append(lines, fmt.Sprintf("[ATTENZIONE] Connessione %s: offset relativo %+d non espone un tratto attraversabile tra Map%03d e Map%03d. La relazione resta consentita; verifica l'offset solo se deve essere attraversabile nel gioco.", id, delta, c.A.MapID, c.B.MapID))
				}
			}
		}
	}

	if !connectionProjectIsEssentialsSource() {
		readers, scanErr := connectionRuntimeReaderFiles()
		if scanErr != "" {
			lines = append(lines, "[ATTENZIONE] Controllo runtime Python non completato: "+scanErr)
		} else if len(doc.Connections) > 0 && len(readers) == 0 {
			lines = append(lines, "[ATTENZIONE] Nessun sorgente Python del progetto contiene un riferimento a map_connections.json/map_connections.")
		} else if len(readers) > 0 {
			shown := readers
			if len(shown) > 3 {
				shown = shown[:3]
			}
			lines = append(lines, "[OK] Lettore/runtime connessioni rilevato in: "+strings.Join(shown, ", "))
		}
	}
	return lines
}

func connectionSummary(e effectiveConnection, ordinal int) string {
	reverse := ""
	if e.Reverse {
		reverse = " [derivata dal verso opposto]"
	}
	return fmt.Sprintf("[CONNESSA] %d. %s -> %s | offset %+d%s", ordinal+1, directionDisplay(e.Direction), mapDisplayName(e.TargetMapID), e.Offset, reverse)
}

func rebuildConnectionLists(selectRecord int) {
	if currentMap == nil {
		return
	}
	connFieldsRefreshing = true
	defer func() { connFieldsRefreshing = false }()
	connEffective = effectiveConnectionsForMap(connectionDoc, currentMap.ID)
	clearList(hwndConnList)
	pSendMessageW.Call(uintptr(hwndConnNumber), CB_RESETCONTENT, 0, 0)
	for i, e := range connEffective {
		addList(hwndConnList, connectionSummary(e, i))
		comboAdd(hwndConnNumber, fmt.Sprintf("Connessione n° %d", i+1))
	}
	if len(connEffective) == 0 {
		addList(hwndConnList, "[INFO] Nessuna connessione salvata per questa mappa.")
	}
	addList(hwndConnList, "---------------- DIAGNOSTICA FILE / CODICE ----------------")
	for _, line := range connectionDocumentDiagnosticLines(connectionDoc) {
		addList(hwndConnList, line)
	}
	setText(hwndConnCount, strconv.Itoa(len(connEffective)))

	selected := 0
	if selectRecord >= 0 {
		if i := connectionEffectiveIndexByRecord(selectRecord); i >= 0 {
			selected = i
		}
	}
	if len(connEffective) == 0 {
		connSelectedRecord = -1
		connSelectedReverse = false
		setText(hwndConnOffset, "0")
		setText(hwndConnTargetID, "-")
		setText(hwndConnTargetRegion, "-")
		return
	}
	pSendMessageW.Call(uintptr(hwndConnNumber), CB_SETCURSEL, uintptr(selected), 0)
	pSendMessageW.Call(uintptr(hwndConnList), LB_SETCURSEL, uintptr(selected), 0)
	showEffectiveConnection(selected)
}

func showEffectiveConnection(index int) {
	if index < 0 || index >= len(connEffective) {
		return
	}
	connFieldsRefreshing = true
	defer func() { connFieldsRefreshing = false }()
	e := connEffective[index]
	connSelectedRecord = e.RecordIndex
	connSelectedReverse = e.Reverse
	pSendMessageW.Call(uintptr(hwndConnNumber), CB_SETCURSEL, uintptr(index), 0)
	pSendMessageW.Call(uintptr(hwndConnList), LB_SETCURSEL, uintptr(index), 0)
	pSendMessageW.Call(uintptr(hwndConnDirection), CB_SETCURSEL, uintptr(directionIndex(e.Direction)), 0)
	setText(hwndConnOffset, strconv.Itoa(e.Offset))
	if ti := connectionTargetIndexByMapID(e.TargetMapID); ti >= 0 {
		pSendMessageW.Call(uintptr(hwndConnTarget), CB_SETCURSEL, uintptr(ti), 0)
	}
	setConnectionDirectionButtons(e.Direction)
	updateConnectionTargetInfo()
}

func commitConnectionEditorFields() error {
	if connFieldsRefreshing || connSelectedRecord < 0 || connSelectedRecord >= len(connectionDoc.Connections) || currentMap == nil {
		return nil
	}
	targetID := selectedTargetMapID()
	if targetID <= 0 {
		return fmt.Errorf("seleziona una mappa destinazione")
	}
	if targetID == currentMap.ID {
		return fmt.Errorf("la mappa destinazione non può essere la mappa corrente")
	}
	dir := directionFromIndex(comboSel(hwndConnDirection))
	edge := connectionDirectionEdge(dir)
	if edge == "" {
		return fmt.Errorf("direzione non valida")
	}
	offset, err := strconv.Atoi(strings.TrimSpace(getText(hwndConnOffset)))
	if err != nil {
		return fmt.Errorf("compensazione non valida")
	}
	rec := &connectionDoc.Connections[connSelectedRecord]
	if rec.A.MapID == currentMap.ID {
		base := rec.A.Offset
		rec.A.MapID = currentMap.ID
		rec.A.Edge = edge
		rec.B.MapID = targetID
		rec.B.Edge = connectionOppositeEdge(edge)
		rec.B.Offset = base + offset
	} else if rec.B.MapID == currentMap.ID {
		base := rec.B.Offset
		rec.B.MapID = currentMap.ID
		rec.B.Edge = edge
		rec.A.MapID = targetID
		rec.A.Edge = connectionOppositeEdge(edge)
		rec.A.Offset = base + offset
	} else {
		rec.A = ConnectionEndpoint{MapID: currentMap.ID, Edge: edge, Offset: 0}
		rec.B = ConnectionEndpoint{MapID: targetID, Edge: connectionOppositeEdge(edge), Offset: offset}
	}
	rec.Enabled = true
	connSelectedReverse = rec.B.MapID == currentMap.ID
	return nil
}

func addConnectionDraft() {
	if currentMap == nil {
		return
	}
	_ = commitConnectionEditorFields()
	target := 0
	if len(connTargetMapIDs) > 0 {
		target = connTargetMapIDs[0]
	}
	if target == 0 {
		msgbox("PML Studio", "Non esistono altre mappe da collegare.", MB_OK|MB_ICONINFORMATION)
		return
	}
	connectionDoc.Connections = append(connectionDoc.Connections, MapConnection{
		ID: nextConnectionID(connectionDoc), Order: nextConnectionOrder(connectionDoc), Enabled: true,
		A: ConnectionEndpoint{MapID: currentMap.ID, Edge: "SOUTH", Offset: 0},
		B: ConnectionEndpoint{MapID: target, Edge: "NORTH", Offset: 0},
	})
	rebuildConnectionLists(len(connectionDoc.Connections) - 1)
}

func removeSelectedConnection() {
	if connSelectedRecord < 0 || connSelectedRecord >= len(connectionDoc.Connections) {
		return
	}
	id := connectionDoc.Connections[connSelectedRecord].ID
	if msgboxResult("PML Studio", fmt.Sprintf("Rimuovere la connessione %s?", id), MB_YESNOCANCEL) != IDYES {
		return
	}
	connectionDoc.Connections = append(connectionDoc.Connections[:connSelectedRecord], connectionDoc.Connections[connSelectedRecord+1:]...)
	rebuildConnectionLists(-1)
}

func saveConnectionDialogData() error {
	if err := commitConnectionEditorFields(); err != nil {
		return err
	}
	if err := saveConnectionDocument(connectionDoc); err != nil {
		return err
	}
	rebuildConnectionLists(connSelectedRecord)
	setToolbarStatus(fmt.Sprintf("Connessioni salvate per Map %03d: %d | diagnostica aggiornata", currentMap.ID, len(effectiveConnectionsForMap(connectionDoc, currentMap.ID))))
	return nil
}

func selectConnectionByEffectiveIndex(i int) {
	if i < 0 || i >= len(connEffective) {
		return
	}
	if err := commitConnectionEditorFields(); err != nil {
		msgbox("PML Studio", err.Error(), MB_OK|MB_ICONERROR)
		return
	}
	rebuildConnectionLists(connEffective[i].RecordIndex)
}

func setConnectionDirectionFromButton(d string) {
	idx := directionIndex(d)
	pSendMessageW.Call(uintptr(hwndConnDirection), CB_SETCURSEL, uintptr(idx), 0)
	setConnectionDirectionButtons(d)
}

// handleConnectionsCommand routes WM_COMMAND from the inline connection editor.
// All controls are children of the main PLM Studio window, so no nested/modal
// message loop is required.
func handleConnectionsCommand(id uint16, notify uint16) bool {
	if !connectionInlineCreated {
		return false
	}
	switch id {
	case idConnNumber:
		if notify == 1 && !connFieldsRefreshing { // CBN_SELCHANGE
			selectConnectionByEffectiveIndex(comboSel(hwndConnNumber))
		}
	case idConnList:
		if notify == LBN_SELCHANGE && !connFieldsRefreshing {
			sel, _, _ := pSendMessageW.Call(uintptr(hwndConnList), LB_GETCURSEL, 0, 0)
			if int(sel) >= 0 && int(sel) < len(connEffective) {
				selectConnectionByEffectiveIndex(int(sel))
			}
		}
	case idConnTarget:
		if notify == 1 { // CBN_SELCHANGE
			updateConnectionTargetInfo()
		}
	case idConnDirection:
		if notify == 1 {
			setConnectionDirectionButtons(directionFromIndex(comboSel(hwndConnDirection)))
		}
	case idConnNorth:
		setConnectionDirectionFromButton("up")
	case idConnSouth:
		setConnectionDirectionFromButton("down")
	case idConnWest:
		setConnectionDirectionFromButton("left")
	case idConnEast:
		setConnectionDirectionFromButton("right")
	case idConnAdd:
		addConnectionDraft()
	case idConnRemove:
		removeSelectedConnection()
	case idConnSave:
		if err := saveConnectionDialogData(); err != nil {
			msgbox("PML Studio", "Impossibile salvare le connessioni:\r\n\r\n"+err.Error(), MB_OK|MB_ICONERROR)
		} else {
			setToolbarStatus("Connessioni salvate correttamente.")
		}
	case idConnClose:
		// Legacy ID kept for file-format/source compatibility. The inline view
		// has no Close button; changing tab replaces the old modal close action.
		setMode("map")
	default:
		return false
	}
	return true
}

func configureConnectionCombo(hwnd syscall.Handle) {
	if hwnd == 0 {
		return
	}
	// Segoe UI 16px richiede più altezza rispetto ai vecchi controlli Win32.
	// -1 regola la parte selezionata; 0 gli elementi della lista.
	pSendMessageW.Call(uintptr(hwnd), CB_SETITEMHEIGHT, ^uintptr(0), 26)
	pSendMessageW.Call(uintptr(hwnd), CB_SETITEMHEIGHT, 0, 26)
}

func appendConnectionInlineHandle(h syscall.Handle) syscall.Handle {
	if h != 0 {
		connectionInlineHandles = append(connectionInlineHandles, h)
	}
	return h
}

// createConnectionsEditor creates the Advance Map-style connection manager as
// a permanent page of the main editor. It is initially hidden and shown only
// when mode == "connections".
func createConnectionsEditor(hInst syscall.Handle) {
	if connectionInlineCreated {
		return
	}
	parent := hwndMain
	mk := func(class, text string, style uint32, id uintptr) syscall.Handle {
		return appendConnectionInlineHandle(createWindow(class, text, style, 0, 0, 0, 0, parent, id, hInst))
	}

	hwndConnGroupPreview = mk("BUTTON", "Anteprima connessioni", WS_CHILD|BS_GROUPBOX, 2320)
	hwndConnNorth = mk("BUTTON", "▲  Su", WS_CHILD|WS_TABSTOP|WS_GROUP|BS_AUTORADIOBUTTON|BS_PUSHLIKE|BS_FLAT, idConnNorth)
	hwndConnWest = mk("BUTTON", "◀  Sinistra", WS_CHILD|WS_TABSTOP|BS_AUTORADIOBUTTON|BS_PUSHLIKE|BS_FLAT, idConnWest)
	hwndConnEast = mk("BUTTON", "Destra  ▶", WS_CHILD|WS_TABSTOP|BS_AUTORADIOBUTTON|BS_PUSHLIKE|BS_FLAT, idConnEast)
	hwndConnCurrentMap = mk("STATIC", "Mappa corrente", WS_CHILD|WS_BORDER, idConnCurrentMap)
	hwndConnSouth = mk("BUTTON", "▼  Giù", WS_CHILD|WS_TABSTOP|BS_AUTORADIOBUTTON|BS_PUSHLIKE|BS_FLAT, idConnSouth)
	hwndConnListLabel = mk("STATIC", "Connessioni reali lette dal progetto + diagnostica file/codice", WS_CHILD, 2321)
	hwndConnList = mk("LISTBOX", "", WS_CHILD|WS_BORDER|WS_VSCROLL|WS_HSCROLL|WS_TABSTOP|LBS_NOTIFY|LBS_NOINTEGRALHEIGHT, idConnList)
	pSendMessageW.Call(uintptr(hwndConnList), 0x0194, 1800, 0)

	hwndConnGroupData = mk("BUTTON", "Dati connessione", WS_CHILD|BS_GROUPBOX, 2330)
	hwndConnLabelCount = mk("STATIC", "Connessioni:", WS_CHILD, 2331)
	hwndConnCount = mk("STATIC", "0", WS_CHILD|WS_BORDER, idConnCount)
	hwndConnLabelNumber = mk("STATIC", "Connessione n°:", WS_CHILD, 2332)
	hwndConnNumber = mk("COMBOBOX", "", WS_CHILD|WS_TABSTOP|CBS_DROPDOWNLIST, idConnNumber)
	configureConnectionCombo(hwndConnNumber)
	hwndConnLabelDirection = mk("STATIC", "Direzione:", WS_CHILD, 2333)
	hwndConnDirection = mk("COMBOBOX", "", WS_CHILD|WS_TABSTOP|CBS_DROPDOWNLIST, idConnDirection)
	configureConnectionCombo(hwndConnDirection)
	for _, name := range []string{"Su", "Giù", "Sinistra", "Destra"} {
		comboAdd(hwndConnDirection, name)
	}
	hwndConnLabelOffset = mk("STATIC", "Compensazione:", WS_CHILD, 2334)
	hwndConnOffset = mk("EDIT", "0", WS_CHILD|WS_BORDER|WS_TABSTOP, idConnOffset)
	hwndConnLabelTarget = mk("STATIC", "Mappa destinazione:", WS_CHILD, 2335)
	hwndConnTarget = mk("COMBOBOX", "", WS_CHILD|WS_TABSTOP|CBS_DROPDOWNLIST|WS_VSCROLL, idConnTarget)
	configureConnectionCombo(hwndConnTarget)
	hwndConnLabelTargetID = mk("STATIC", "Map ID:", WS_CHILD, 2336)
	hwndConnTargetID = mk("STATIC", "-", WS_CHILD|WS_BORDER, idConnTargetID)
	hwndConnLabelTargetRegion = mk("STATIC", "Regione:", WS_CHILD, 2337)
	hwndConnTargetRegion = mk("STATIC", "-", WS_CHILD|WS_BORDER, idConnTargetReg)
	hwndConnNoteOffset = mk("STATIC", "La compensazione è l'offset lungo il bordo condiviso. Sono ammessi valori negativi.", WS_CHILD, 2338)
	hwndConnNoteBidirectional = mk("STATIC", "Le connessioni sono bidirezionali: il runtime deriva direzione opposta e offset inverso.", WS_CHILD, 2339)
	hwndConnAdd = mk("BUTTON", "Aggiungi", WS_CHILD|WS_TABSTOP|BS_PUSHBUTTON, idConnAdd)
	hwndConnRemove = mk("BUTTON", "Rimuovi", WS_CHILD|WS_TABSTOP|BS_PUSHBUTTON, idConnRemove)
	hwndConnSave = mk("BUTTON", "Salva", WS_CHILD|WS_TABSTOP|BS_PUSHBUTTON, idConnSave)

	connectionInlineCreated = true
	showConnectionsEditor(false)
}

func showConnectionsEditor(show bool) {
	for _, h := range connectionInlineHandles {
		showControl(h, show)
	}
	if show {
		loadConnectionsEditorForCurrentMap()
	}
}

func loadConnectionsEditorForCurrentMap() {
	if !connectionInlineCreated {
		return
	}
	if currentProject == "" || currentMap == nil {
		connectionLoadedMapID = 0
		connectionDoc = defaultConnectionDocument()
		connEffective = nil
		clearList(hwndConnList)
		setText(hwndConnCount, "0")
		setText(hwndConnCurrentMap, "Nessuna mappa selezionata")
		return
	}
	if knownMapDuplicateConflict() {
		setText(hwndConnCurrentMap, fmt.Sprintf("Map %03d - %s\r\nDuplicati Map ID da risolvere", currentMap.ID, currentMap.Name))
		return
	}
	doc, err := loadConnectionDocument()
	if err != nil {
		connectionDoc = defaultConnectionDocument()
		connEffective = nil
		clearList(hwndConnList)
		addList(hwndConnList, "[ERRORE] "+err.Error())
		addList(hwndConnList, "Percorso: "+connectionDataPath())
		setText(hwndConnCount, "0")
		setToolbarStatus("Connessioni: " + err.Error())
		return
	}
	connectionDoc = doc
	connectionLoadedMapID = currentMap.ID
	connSelectedRecord = -1
	connSelectedReverse = false
	setText(hwndConnCurrentMap, fmt.Sprintf("Mappa corrente\r\n%03d - %s", currentMap.ID, currentMap.Name))
	populateConnectionTargetCombo()
	rebuildConnectionLists(-1)
	if len(connEffective) == 0 {
		pSendMessageW.Call(uintptr(hwndConnDirection), CB_SETCURSEL, 1, 0)
		if len(connTargetMapIDs) > 0 {
			pSendMessageW.Call(uintptr(hwndConnTarget), CB_SETCURSEL, 0, 0)
		}
		setConnectionDirectionButtons("down")
		updateConnectionTargetInfo()
		setText(hwndConnOffset, "0")
	}
}

// layoutConnectionsEditor lays the connection manager directly into the
// central page. It adapts to the current editor width without opening another
// native window.
func layoutConnectionsEditor(x, y, w, h int32) {
	if !connectionInlineCreated || w <= 0 || h <= 0 {
		return
	}

	// La Vista Connessioni usa Segoe UI 16px. Le vecchie misure da 22/24px
	// tagliavano testo, caret e valori delle combo. Manteniamo lo stesso
	// layout Advance Map-style, ma con spaziature coerenti con il tema moderno.
	const (
		pad        int32 = 16
		gap        int32 = 16
		labelH     int32 = 24
		editH      int32 = 30
		buttonH    int32 = 34
		rowGap     int32 = 9
		groupInset int32 = 20
	)

	innerW := w - pad*2
	innerH := h - pad*2
	if innerW < 760 {
		innerW = 760
	}
	if innerH < 480 {
		innerH = 480
	}

	leftW := innerW * 56 / 100
	if leftW < 390 {
		leftW = 390
	}
	rightW := innerW - leftW - gap
	if rightW < 350 {
		rightW = 350
		leftW = innerW - rightW - gap
	}

	lx := x + pad
	ry := y + pad
	rx := lx + leftW + gap

	// --- Anteprima + lista connessioni ---
	moveControl(hwndConnGroupPreview, lx, ry, leftW, innerH)
	previewTop := ry + 34
	centerW := leftW * 44 / 100
	if centerW < 170 {
		centerW = 170
	}
	sideW := (leftW - centerW - 64) / 2
	if sideW < 104 {
		sideW = 104
	}
	centerX := lx + (leftW-centerW)/2
	mapBoxH := int32(116)

	moveControl(hwndConnNorth, centerX, previewTop+4, centerW, 38)
	moveControl(hwndConnWest, lx+groupInset, previewTop+58, sideW, 150)
	moveControl(hwndConnEast, lx+leftW-groupInset-sideW, previewTop+58, sideW, 150)
	moveControl(hwndConnCurrentMap, centerX, previewTop+70, centerW, mapBoxH)
	moveControl(hwndConnSouth, centerX, previewTop+202, centerW, 38)

	listLabelY := previewTop + 260
	moveControl(hwndConnListLabel, lx+groupInset, listLabelY, leftW-groupInset*2, labelH)
	listY := listLabelY + labelH + 6
	listH := ry + innerH - groupInset - listY
	if listH < 100 {
		listH = 100
	}
	moveControl(hwndConnList, lx+groupInset, listY, leftW-groupInset*2, listH)

	// --- Dati connessione ---
	moveControl(hwndConnGroupData, rx, ry, rightW, innerH)
	labelX := rx + groupInset
	contentW := rightW - groupInset*2
	// Più spazio alle etichette lunghe (Connessione n°, Compensazione, ecc.).
	labelW := contentW * 46 / 100
	if labelW < 148 {
		labelW = 148
	}
	fieldX := labelX + labelW + 10
	fieldW := rx + rightW - groupInset - fieldX
	if fieldW < 130 {
		fieldW = 130
	}

	rowY := ry + 38
	placeRow := func(label, field syscall.Handle, combo bool) {
		moveControl(label, labelX, rowY+4, labelW, labelH)
		h := editH
		if combo {
			// Altezza totale sufficiente per la lista dropdown; la parte chiusa
			// usa CB_SETITEMHEIGHT configurato in createConnectionsEditor.
			h = 220
		}
		moveControl(field, fieldX, rowY, fieldW, h)
		rowY += editH + rowGap
	}

	placeRow(hwndConnLabelCount, hwndConnCount, false)
	placeRow(hwndConnLabelNumber, hwndConnNumber, true)
	placeRow(hwndConnLabelDirection, hwndConnDirection, true)
	placeRow(hwndConnLabelOffset, hwndConnOffset, false)

	// La mappa destinazione occupa tutta la larghezza per evitare nomi tagliati.
	moveControl(hwndConnLabelTarget, labelX, rowY+2, contentW, labelH)
	rowY += labelH + 4
	moveControl(hwndConnTarget, labelX, rowY, contentW, 250)
	rowY += editH + rowGap

	placeRow(hwndConnLabelTargetID, hwndConnTargetID, false)
	placeRow(hwndConnLabelTargetRegion, hwndConnTargetRegion, false)

	buttonY := ry + innerH - groupInset - buttonH
	noteY := rowY + 4
	maxNoteBottom := buttonY - 10
	if noteY+92 > maxNoteBottom {
		noteY = maxNoteBottom - 92
	}
	moveControl(hwndConnNoteOffset, labelX, noteY, contentW, 42)
	moveControl(hwndConnNoteBidirectional, labelX, noteY+44, contentW, 46)

	btnGap := int32(8)
	btnW := (contentW - btnGap*2) / 3
	moveControl(hwndConnAdd, labelX, buttonY, btnW, buttonH)
	moveControl(hwndConnRemove, labelX+btnW+btnGap, buttonY, btnW, buttonH)
	moveControl(hwndConnSave, labelX+(btnW+btnGap)*2, buttonY, btnW, buttonH)
}

// showConnectionsManager is kept as the public entry point used by the toolbar
// and menu, but it no longer opens a modal window. It switches to the inline
// Connessioni page and loads the current map data there.
func showConnectionsManager() {
	if currentProject == "" {
		msgbox("PML Studio", "Apri prima un progetto.", MB_OK|MB_ICONINFORMATION)
		return
	}
	if currentMap == nil {
		msgbox("PML Studio", "Seleziona prima una mappa.", MB_OK|MB_ICONINFORMATION)
		return
	}
	if knownMapDuplicateConflict() {
		msgbox("PML Studio", "Risolvi prima i Map ID duplicati. Il gestore connessioni usa il filesystem come fonte di verità.", MB_OK|MB_ICONERROR)
		return
	}
	if mode != "connections" {
		setMode("connections")
	} else {
		showConnectionsEditor(true)
		loadConnectionsEditorForCurrentMap()
		layout(hwndMain)
	}
}
