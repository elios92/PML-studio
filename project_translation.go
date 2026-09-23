//go:build windows

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

// Project Translation Manager centralises every user-facing string we can
// safely identify without guessing. The first version deliberately edits only
// well-defined event commands, recognised script message calls and PBS text
// fields. This avoids blind search/replace inside executable code.

const (
	translationManagerClassName = "PLMStudioProjectTranslation01"

	idTranslationSection  = 7940
	idTranslationSearch   = 7941
	idTranslationList     = 7942
	idTranslationOriginal = 7943
	idTranslationItalian  = 7944
	idTranslationSource   = 7945
	idTranslationSaveOne  = 7946
	idTranslationSaveAll  = 7947
	idTranslationRestore  = 7948
	idTranslationRescan   = 7949
	idTranslationClose    = 7950
	idTranslationStatus   = 7951
)

const esReadOnly = 0x0800

type projectTranslationKind string

const (
	translationEventText   projectTranslationKind = "event_text"
	translationEventChoice projectTranslationKind = "event_choice"
	translationEventScript projectTranslationKind = "event_script"
	translationScript      projectTranslationKind = "script"
	translationPBS         projectTranslationKind = "pbs"
)

type projectTranslationEntry struct {
	Section     string
	Kind        projectTranslationKind
	File        string
	SourceLabel string
	Original    string
	Translation string

	// Event locator.
	EventID      int
	PageIndex    int
	CommandIndex int
	ChoiceIndex  int
	ScriptMatch  int

	// Script/PBS locator.
	LineIndex  int
	MatchIndex int
	PBSKey     string
}

type scriptLiteralMatch struct {
	Start, End int // content only, not quotes
	Quote      byte
	Text       string
}

var (
	translationManagerRegistered bool
	translationManagerOpen       bool
	translationManagerWindow     syscall.Handle
	translationManagerSection    syscall.Handle
	translationManagerSearch     syscall.Handle
	translationManagerList       syscall.Handle
	translationManagerOriginal   syscall.Handle
	translationManagerItalian    syscall.Handle
	translationManagerSource     syscall.Handle
	translationManagerStatus     syscall.Handle

	translationEntries  []projectTranslationEntry
	translationFiltered []int
	translationSelected = -1
)

var (
	scriptMessageDoubleRE = regexp.MustCompile(`(?i)(?:\b_INTL|\b_I|\bpbMessage|\bpbConfirmMessage|\bpbDisplay|\bpbDisplayPaused|\bpbEnterText|\bgettext|\btr|\bshow_message|\bmessage|\b_)\s*\(\s*"((?:\\.|[^"\\])*)"`)
	scriptMessageSingleRE = regexp.MustCompile(`(?i)(?:\b_INTL|\b_I|\bpbMessage|\bpbConfirmMessage|\bpbDisplay|\bpbDisplayPaused|\bpbEnterText|\bgettext|\btr|\bshow_message|\bmessage|\b_)\s*\(\s*'((?:\\.|[^'\\])*)'`)
	pbsTranslatableRE     = regexp.MustCompile(`(?i)^\s*(Name|RealName|DisplayName|Description|Message|Text)\s*=\s*(.*)$`)
)

func decodeScriptLiteral(s string, quote byte) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 >= len(s) {
			b.WriteByte(s[i])
			continue
		}
		n := s[i+1]
		switch n {
		case 'n':
			b.WriteByte('\n')
			i++
		case 'r':
			b.WriteByte('\r')
			i++
		case 't':
			b.WriteByte('\t')
			i++
		case '\\':
			b.WriteByte('\\')
			i++
		case '"':
			if quote == '"' {
				b.WriteByte('"')
				i++
			} else {
				b.WriteByte('\\')
			}
		case '\'':
			if quote == '\'' {
				b.WriteByte('\'')
				i++
			} else {
				b.WriteByte('\\')
			}
		default:
			// RPG Maker control sequences such as \\C[1], \\n[Player], etc.
			// are intentionally preserved verbatim.
			b.WriteByte('\\')
		}
	}
	return b.String()
}

func encodeScriptLiteral(s string, quote byte) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\r", "\\r")
	s = strings.ReplaceAll(s, "\n", "\\n")
	s = strings.ReplaceAll(s, "\t", "\\t")
	if quote == '\'' {
		s = strings.ReplaceAll(s, "'", "\\'")
	} else {
		s = strings.ReplaceAll(s, "\"", "\\\"")
	}
	return s
}

func scriptLiteralMatches(line string) []scriptLiteralMatch {
	type rawMatch struct {
		start, end int
		quote      byte
		text       string
	}
	raw := []rawMatch{}
	for _, spec := range []struct {
		re    *regexp.Regexp
		quote byte
	}{{scriptMessageDoubleRE, '"'}, {scriptMessageSingleRE, '\''}} {
		for _, idx := range spec.re.FindAllStringSubmatchIndex(line, -1) {
			if len(idx) < 4 || idx[2] < 0 || idx[3] < idx[2] {
				continue
			}
			raw = append(raw, rawMatch{start: idx[2], end: idx[3], quote: spec.quote, text: line[idx[2]:idx[3]]})
		}
	}
	sort.Slice(raw, func(i, j int) bool { return raw[i].start < raw[j].start })
	out := make([]scriptLiteralMatch, 0, len(raw))
	lastEnd := -1
	for _, m := range raw {
		if m.start < lastEnd {
			continue
		}
		out = append(out, scriptLiteralMatch{Start: m.start, End: m.end, Quote: m.quote, Text: decodeScriptLiteral(m.text, m.quote)})
		lastEnd = m.end
	}
	return out
}

func translationCompact(s string, n int) string {
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

func translationRelPath(path string) string {
	if currentProject != "" {
		if rel, err := filepath.Rel(currentProject, path); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return filepath.ToSlash(rel)
		}
	}
	return filepath.Base(path)
}

func mapEventByID(eventsAny any, eventID int) map[string]any {
	switch t := eventsAny.(type) {
	case map[string]any:
		if v, ok := t[strconv.Itoa(eventID)]; ok {
			if m, ok := v.(map[string]any); ok {
				return m
			}
		}
		for _, v := range t {
			if m, ok := v.(map[string]any); ok {
				if idv, ok := anyMapValueCI(m, "id", "event_id", "eventId"); ok && anyInt(idv) == eventID {
					return m
				}
			}
		}
	case []any:
		if eventID >= 0 && eventID < len(t) {
			if m, ok := t[eventID].(map[string]any); ok {
				return m
			}
		}
		for _, v := range t {
			if m, ok := v.(map[string]any); ok {
				if idv, ok := anyMapValueCI(m, "id", "event_id", "eventId"); ok && anyInt(idv) == eventID {
					return m
				}
			}
		}
	}
	return nil
}

func eventPageAndCommand(eventsAny any, e projectTranslationEntry) (map[string]any, map[string]any, bool) {
	ev := mapEventByID(eventsAny, e.EventID)
	if ev == nil {
		return nil, nil, false
	}
	pv, ok := anyMapValueCI(ev, "pages")
	if !ok {
		return nil, nil, false
	}
	pages, ok := pv.([]any)
	if !ok || e.PageIndex < 0 || e.PageIndex >= len(pages) {
		return nil, nil, false
	}
	page, ok := pages[e.PageIndex].(map[string]any)
	if !ok {
		return nil, nil, false
	}
	lv, ok := anyMapValueCI(page, "list")
	if !ok {
		return page, nil, false
	}
	list, ok := lv.([]any)
	if !ok || e.CommandIndex < 0 || e.CommandIndex >= len(list) {
		return page, nil, false
	}
	cmd, ok := list[e.CommandIndex].(map[string]any)
	return page, cmd, ok
}

func eventCommandParameters(cmd map[string]any) ([]any, bool) {
	pv, ok := anyMapValueCI(cmd, "parameters", "params")
	if !ok {
		return nil, false
	}
	params, ok := pv.([]any)
	return params, ok
}

func scanMapTranslations(path string, mapID int, out *[]projectTranslationEntry) {
	doc, err := loadMapDocument(path)
	if err != nil || doc.Raw == nil {
		return
	}
	raw, ok := doc.Raw["events"]
	if !ok {
		return
	}
	eventsAny, err := decodeJSONAny(raw)
	if err != nil {
		return
	}
	collectEvent := func(ev map[string]any, fallbackID int) {
		eventID := fallbackID
		if v, ok := anyMapValueCI(ev, "id", "event_id", "eventId"); ok && anyInt(v) > 0 {
			eventID = anyInt(v)
		}
		name := fmt.Sprintf("Evento %d", eventID)
		if v, ok := anyMapValueCI(ev, "name"); ok && strings.TrimSpace(anyString(v)) != "" {
			name = strings.TrimSpace(anyString(v))
		}
		pv, ok := anyMapValueCI(ev, "pages")
		if !ok {
			return
		}
		pages, ok := pv.([]any)
		if !ok {
			return
		}
		for pi, p := range pages {
			page, ok := p.(map[string]any)
			if !ok {
				continue
			}
			lv, ok := anyMapValueCI(page, "list")
			if !ok {
				continue
			}
			list, ok := lv.([]any)
			if !ok {
				continue
			}
			for ci, node := range list {
				cmd, ok := node.(map[string]any)
				if !ok {
					continue
				}
				code := commandMapCode(cmd)
				params, _ := eventCommandParameters(cmd)
				switch code {
				case 101, 401:
					if len(params) > 0 {
						if text, ok := params[0].(string); ok && strings.TrimSpace(text) != "" {
							*out = append(*out, projectTranslationEntry{
								Section: "Eventi - Dialoghi", Kind: translationEventText, File: path,
								SourceLabel: fmt.Sprintf("Map%03d · %s · Pagina %d · comando %d", mapID, name, pi+1, ci+1),
								Original:    text, Translation: text, EventID: eventID, PageIndex: pi, CommandIndex: ci,
							})
						}
					}
				case 102:
					if len(params) > 0 {
						if choices, ok := params[0].([]any); ok {
							for xi, cv := range choices {
								text, _ := cv.(string)
								if strings.TrimSpace(text) == "" {
									continue
								}
								*out = append(*out, projectTranslationEntry{
									Section: "Eventi - Scelte", Kind: translationEventChoice, File: path,
									SourceLabel: fmt.Sprintf("Map%03d · %s · Pagina %d · scelta %d", mapID, name, pi+1, xi+1),
									Original:    text, Translation: text, EventID: eventID, PageIndex: pi, CommandIndex: ci, ChoiceIndex: xi,
								})
							}
						}
					}
				case 355, 655:
					if len(params) > 0 {
						if script, ok := params[0].(string); ok {
							matches := scriptLiteralMatches(script)
							for mi, sm := range matches {
								if strings.TrimSpace(sm.Text) == "" {
									continue
								}
								*out = append(*out, projectTranslationEntry{
									Section: "Eventi - Script", Kind: translationEventScript, File: path,
									SourceLabel: fmt.Sprintf("Map%03d · %s · Pagina %d · script %d", mapID, name, pi+1, ci+1),
									Original:    sm.Text, Translation: sm.Text, EventID: eventID, PageIndex: pi, CommandIndex: ci, ScriptMatch: mi,
								})
							}
						}
					}
				}
			}
		}
	}
	switch t := eventsAny.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			ev, ok := t[k].(map[string]any)
			if !ok {
				continue
			}
			id, _ := strconv.Atoi(k)
			collectEvent(ev, id)
		}
	case []any:
		for i, v := range t {
			ev, ok := v.(map[string]any)
			if ok {
				collectEvent(ev, i)
			}
		}
	}
}

func translationScriptRoots(project string) []string {
	candidates := []string{
		"Scripts", "scripts", "Plugins", "plugins", filepath.Join("converted", "scripts"),
		filepath.Join("converted", "Scripts"), "src", "game", "runtime",
	}
	seen := map[string]bool{}
	out := []string{}
	for _, rel := range candidates {
		p := filepath.Join(project, rel)
		if isDir(p) {
			key := strings.ToLower(filepath.Clean(p))
			if !seen[key] {
				seen[key] = true
				out = append(out, p)
			}
		}
	}
	return out
}

func shouldSkipTranslationDir(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case ".git", ".svn", ".hg", ".idea", ".vscode", "__pycache__", "site-packages", "node_modules", "dist", "build", "save", "graphics", "audio", "vendor", "venv", ".venv", "lib":
		return true
	}
	return false
}

func scanScriptFileTranslations(path string, out *[]projectTranslationEntry) {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || info.Size() > 8*1024*1024 {
		return
	}
	b, err := os.ReadFile(path)
	if err != nil || bytes.IndexByte(b, 0) >= 0 {
		return
	}
	lines := strings.Split(string(b), "\n")
	for li, line := range lines {
		matches := scriptLiteralMatches(line)
		for mi, sm := range matches {
			text := strings.TrimSpace(sm.Text)
			if text == "" {
				continue
			}
			*out = append(*out, projectTranslationEntry{
				Section: "Script Ruby/Python", Kind: translationScript, File: path,
				SourceLabel: fmt.Sprintf("%s · riga %d", translationRelPath(path), li+1),
				Original:    sm.Text, Translation: sm.Text, LineIndex: li, MatchIndex: mi,
			})
		}
	}
}

func scanScriptTranslations(project string, out *[]projectTranslationEntry) {
	seen := map[string]bool{}
	for _, root := range translationScriptRoots(project) {
		_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil || d == nil {
				return nil
			}
			if d.IsDir() {
				if path != root && shouldSkipTranslationDir(d.Name()) {
					return filepath.SkipDir
				}
				return nil
			}
			ext := strings.ToLower(filepath.Ext(d.Name()))
			if ext != ".rb" && ext != ".py" {
				return nil
			}
			key := strings.ToLower(filepath.Clean(path))
			if seen[key] {
				return nil
			}
			seen[key] = true
			scanScriptFileTranslations(path, out)
			return nil
		})
	}
	// Root-level scripts are common in small PLM projects.
	entries, _ := os.ReadDir(project)
	for _, d := range entries {
		if d.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(d.Name()))
		if ext != ".rb" && ext != ".py" {
			continue
		}
		p := filepath.Join(project, d.Name())
		key := strings.ToLower(filepath.Clean(p))
		if !seen[key] {
			seen[key] = true
			scanScriptFileTranslations(p, out)
		}
	}
}

func scanPBSTranslations(project string, out *[]projectTranslationEntry) {
	roots := []string{filepath.Join(project, "PBS"), filepath.Join(project, "pbs"), filepath.Join(project, "converted", "PBS"), filepath.Join(project, "converted", "pbs")}
	seen := map[string]bool{}
	for _, root := range roots {
		if !isDir(root) {
			continue
		}
		_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil || d == nil {
				return nil
			}
			if d.IsDir() {
				return nil
			}
			if strings.ToLower(filepath.Ext(d.Name())) != ".txt" {
				return nil
			}
			keyPath := strings.ToLower(filepath.Clean(path))
			if seen[keyPath] {
				return nil
			}
			seen[keyPath] = true
			b, err := os.ReadFile(path)
			if err != nil || bytes.IndexByte(b, 0) >= 0 {
				return nil
			}
			lines := strings.Split(string(b), "\n")
			for li, line := range lines {
				clean := strings.TrimSuffix(line, "\r")
				m := pbsTranslatableRE.FindStringSubmatch(clean)
				if len(m) != 3 {
					continue
				}
				field := strings.TrimSpace(m[1])
				text := strings.TrimSpace(m[2])
				if text == "" {
					continue
				}
				*out = append(*out, projectTranslationEntry{
					Section: "Database / PBS", Kind: translationPBS, File: path,
					SourceLabel: fmt.Sprintf("%s · riga %d · %s", translationRelPath(path), li+1, field),
					Original:    text, Translation: text, LineIndex: li, PBSKey: field,
				})
			}
			return nil
		})
	}
}

func scanProjectTranslations(project string) []projectTranslationEntry {
	out := []projectTranslationEntry{}
	idx := projectMapFileIndex(project)
	ids := make([]int, 0, len(idx))
	for id := range idx {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	for _, id := range ids {
		scanMapTranslations(idx[id], id, &out)
	}
	scanScriptTranslations(project, &out)
	scanPBSTranslations(project, &out)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Section != out[j].Section {
			return out[i].Section < out[j].Section
		}
		if out[i].File != out[j].File {
			return strings.ToLower(out[i].File) < strings.ToLower(out[j].File)
		}
		return out[i].SourceLabel < out[j].SourceLabel
	})
	return out
}

func setCommandParameters(cmd map[string]any, params []any) {
	for k := range cmd {
		normalized := strings.TrimSpace(strings.TrimPrefix(k, "@"))
		if strings.EqualFold(normalized, "parameters") || strings.EqualFold(normalized, "params") {
			cmd[k] = params
			return
		}
	}
	cmd["parameters"] = params
}

func saveEventTranslation(e projectTranslationEntry) error {
	doc, err := loadMapDocument(e.File)
	if err != nil {
		return err
	}
	raw, ok := doc.Raw["events"]
	if !ok {
		return fmt.Errorf("campo events assente")
	}
	eventsAny, err := decodeJSONAny(raw)
	if err != nil {
		return err
	}
	_, cmd, ok := eventPageAndCommand(eventsAny, e)
	if !ok {
		return fmt.Errorf("comando evento non più trovato")
	}
	params, ok := eventCommandParameters(cmd)
	if !ok || len(params) == 0 {
		return fmt.Errorf("parametri comando non validi")
	}

	switch e.Kind {
	case translationEventText:
		params[0] = e.Translation
	case translationEventChoice:
		choices, ok := params[0].([]any)
		if !ok || e.ChoiceIndex < 0 || e.ChoiceIndex >= len(choices) {
			return fmt.Errorf("scelta non più trovata")
		}
		choices[e.ChoiceIndex] = e.Translation
		params[0] = choices
	case translationEventScript:
		script, ok := params[0].(string)
		if !ok {
			return fmt.Errorf("script evento non valido")
		}
		matches := scriptLiteralMatches(script)
		if e.ScriptMatch < 0 || e.ScriptMatch >= len(matches) {
			return fmt.Errorf("testo script non più trovato")
		}
		m := matches[e.ScriptMatch]
		script = script[:m.Start] + encodeScriptLiteral(e.Translation, m.Quote) + script[m.End:]
		params[0] = script
	default:
		return fmt.Errorf("tipo evento non supportato")
	}
	setCommandParameters(cmd, params)
	b, err := json.Marshal(eventsAny)
	if err != nil {
		return err
	}
	doc.Raw["events"] = b
	if err := saveMapDocument(doc); err != nil {
		return err
	}
	syncCurrentMapAfterTranslation(e.File)
	return nil
}

func saveScriptTranslation(e projectTranslationEntry) error {
	b, err := os.ReadFile(e.File)
	if err != nil {
		return err
	}
	lines := strings.Split(string(b), "\n")
	if e.LineIndex < 0 || e.LineIndex >= len(lines) {
		return fmt.Errorf("riga script non più trovata")
	}
	line := lines[e.LineIndex]
	matches := scriptLiteralMatches(line)
	if e.MatchIndex < 0 || e.MatchIndex >= len(matches) {
		return fmt.Errorf("testo script non più trovato")
	}
	m := matches[e.MatchIndex]
	lines[e.LineIndex] = line[:m.Start] + encodeScriptLiteral(e.Translation, m.Quote) + line[m.End:]
	return os.WriteFile(e.File, []byte(strings.Join(lines, "\n")), 0644)
}

func savePBSTranslation(e projectTranslationEntry) error {
	b, err := os.ReadFile(e.File)
	if err != nil {
		return err
	}
	lines := strings.Split(string(b), "\n")
	if e.LineIndex < 0 || e.LineIndex >= len(lines) {
		return fmt.Errorf("riga PBS non più trovata")
	}
	old := lines[e.LineIndex]
	cr := ""
	clean := old
	if strings.HasSuffix(clean, "\r") {
		clean = strings.TrimSuffix(clean, "\r")
		cr = "\r"
	}
	m := pbsTranslatableRE.FindStringSubmatch(clean)
	if len(m) != 3 {
		return fmt.Errorf("campo PBS non più riconosciuto")
	}
	eq := strings.Index(clean, "=")
	if eq < 0 {
		return fmt.Errorf("campo PBS non valido")
	}
	lines[e.LineIndex] = clean[:eq+1] + e.Translation + cr
	return os.WriteFile(e.File, []byte(strings.Join(lines, "\n")), 0644)
}

func saveTranslationEntry(e projectTranslationEntry) error {
	if strings.TrimSpace(e.File) == "" {
		return fmt.Errorf("origine testo non valida")
	}
	switch e.Kind {
	case translationEventText, translationEventChoice, translationEventScript:
		return saveEventTranslation(e)
	case translationScript:
		return saveScriptTranslation(e)
	case translationPBS:
		return savePBSTranslation(e)
	}
	return fmt.Errorf("tipo traduzione non supportato")
}

func syncCurrentMapAfterTranslation(path string) {
	if currentMapDoc == nil || currentMapDoc.Path == "" || !strings.EqualFold(filepath.Clean(currentMapDoc.Path), filepath.Clean(path)) {
		return
	}
	editedID := 0
	oldPage := eventEditorPageIndex
	if eventEditorEditingIndex >= 0 && eventEditorEditingIndex < len(events) {
		editedID = events[eventEditorEditingIndex].ID
	}
	fresh, err := loadMapDocument(path)
	if err != nil {
		return
	}
	currentMapDoc = fresh
	_, _ = loadEventsFromRealMap()
	if editedID > 0 {
		eventEditorEditingIndex = -1
		selectedEvent = -1
		for i := range events {
			if events[i].ID == editedID {
				eventEditorEditingIndex = i
				selectedEvent = i
				break
			}
		}
		if eventEditorOpen && eventEditorEditingIndex >= 0 {
			e := events[eventEditorEditingIndex]
			setText(eventEditorName, e.Name)
			eventEditorPages = eventDraftsFromEditorEvent(e)
			if oldPage < 0 {
				oldPage = 0
			}
			if oldPage >= len(eventEditorPages) {
				oldPage = len(eventEditorPages) - 1
			}
			if oldPage < 0 {
				oldPage = 0
			}
			eventEditorPageIndex = oldPage
			loadEventEditorPageToControls()
			refreshEventEditorPageButtons()
		}
	}
	showEvent()
	invalidate(hwndCanvas)
}

func translationStoreCurrentEdit() {
	if translationSelected >= 0 && translationSelected < len(translationEntries) && translationManagerItalian != 0 {
		translationEntries[translationSelected].Translation = getText(translationManagerItalian)
	}
}

func translationEntryDirty(i int) bool {
	return i >= 0 && i < len(translationEntries) && translationEntries[i].Translation != translationEntries[i].Original
}

func translationHasDirty() bool {
	translationStoreCurrentEdit()
	for i := range translationEntries {
		if translationEntryDirty(i) {
			return true
		}
	}
	return false
}

func translationSectionNames() []string {
	return []string{"Tutto", "Eventi - Dialoghi", "Eventi - Scelte", "Eventi - Script", "Script Ruby/Python", "Database / PBS"}
}

func refreshTranslationList(selectFirst bool) {
	translationStoreCurrentEdit()
	clearList(translationManagerList)
	translationFiltered = translationFiltered[:0]
	section := "Tutto"
	si := comboSel(translationManagerSection)
	sections := translationSectionNames()
	if si >= 0 && si < len(sections) {
		section = sections[si]
	}
	q := strings.ToLower(strings.TrimSpace(getText(translationManagerSearch)))
	for i, e := range translationEntries {
		if section != "Tutto" && e.Section != section {
			continue
		}
		hay := strings.ToLower(e.SourceLabel + " " + e.Original + " " + e.Translation)
		if q != "" && !strings.Contains(hay, q) {
			continue
		}
		mark := "  "
		if translationEntryDirty(i) {
			mark = "* "
		}
		addList(translationManagerList, mark+"["+e.Section+"] "+translationCompact(e.Original, 72))
		translationFiltered = append(translationFiltered, i)
	}
	translationSelected = -1
	setText(translationManagerOriginal, "")
	setText(translationManagerItalian, "")
	setText(translationManagerSource, "")
	if selectFirst && len(translationFiltered) > 0 {
		pSendMessageW.Call(uintptr(translationManagerList), LB_SETCURSEL, 0, 0)
		loadSelectedTranslation()
	}
	dirty := 0
	for i := range translationEntries {
		if translationEntryDirty(i) {
			dirty++
		}
	}
	setText(translationManagerStatus, fmt.Sprintf("%d testi trovati · %d nella sezione · %d modificati non salvati", len(translationEntries), len(translationFiltered), dirty))
}

func loadSelectedTranslation() {
	translationStoreCurrentEdit()
	r, _, _ := pSendMessageW.Call(uintptr(translationManagerList), LB_GETCURSEL, 0, 0)
	idx := int(r)
	if idx < 0 || idx >= len(translationFiltered) {
		translationSelected = -1
		return
	}
	translationSelected = translationFiltered[idx]
	e := translationEntries[translationSelected]
	setText(translationManagerOriginal, e.Original)
	setText(translationManagerItalian, e.Translation)
	setText(translationManagerSource, e.SourceLabel+"\r\n"+translationRelPath(e.File))
}

func saveSelectedTranslation() bool {
	translationStoreCurrentEdit()
	if translationSelected < 0 || translationSelected >= len(translationEntries) {
		msgbox("PML Studio - Traduzione", "Seleziona prima un testo.", MB_OK|MB_ICONINFORMATION)
		return false
	}
	e := translationEntries[translationSelected]
	if e.Translation == e.Original {
		setToolbarStatus("Traduzione: nessuna modifica da salvare.")
		return true
	}
	if err := saveTranslationEntry(e); err != nil {
		msgbox("PML Studio - Traduzione", "Errore durante il salvataggio:\r\n\r\n"+err.Error(), MB_OK|MB_ICONERROR)
		return false
	}
	translationEntries[translationSelected].Original = e.Translation
	translationEntries[translationSelected].Translation = e.Translation
	refreshTranslationList(false)
	setToolbarStatus("Traduzione italiana salvata: " + e.SourceLabel)
	return true
}

func saveAllTranslations() bool {
	translationStoreCurrentEdit()
	changed := 0
	for i := range translationEntries {
		if !translationEntryDirty(i) {
			continue
		}
		e := translationEntries[i]
		if err := saveTranslationEntry(e); err != nil {
			msgbox("PML Studio - Traduzione", fmt.Sprintf("Salvataggio interrotto su:\r\n%s\r\n\r\n%s", e.SourceLabel, err.Error()), MB_OK|MB_ICONERROR)
			refreshTranslationList(false)
			return false
		}
		translationEntries[i].Original = e.Translation
		translationEntries[i].Translation = e.Translation
		changed++
	}
	refreshTranslationList(false)
	setToolbarStatus(fmt.Sprintf("Traduzione italiana: %d testi salvati.", changed))
	if changed > 0 {
		msgbox("PML Studio - Traduzione", fmt.Sprintf("Salvataggio completato.\r\n\r\n%d testi aggiornati nei dati reali del progetto.", changed), MB_OK|MB_ICONINFORMATION)
	}
	return true
}

func closeTranslationManager(hwnd syscall.Handle) bool {
	if translationHasDirty() {
		r := msgboxResult("PML Studio - Traduzione", "Ci sono traduzioni non salvate.\r\n\r\nVuoi salvarle prima di chiudere?", MB_YESNOCANCEL|MB_ICONINFORMATION)
		if r == IDCANCEL {
			return false
		}
		if r == IDYES && !saveAllTranslations() {
			return false
		}
	}
	pDestroyWindow.Call(uintptr(hwnd))
	return true
}

func translationManagerWndProcFn(hwnd syscall.Handle, msg uint32, w, l uintptr) uintptr {
	switch msg {
	case WM_COMMAND:
		id := int(loword(w))
		notify := int(hiword(w))
		if id == idTranslationList && notify == LBN_SELCHANGE {
			loadSelectedTranslation()
			return 0
		}
		if id == idTranslationSection && notify == 1 { // CBN_SELCHANGE
			refreshTranslationList(true)
			return 0
		}
		if id == idTranslationSearch && notify == 0x0300 { // EN_CHANGE
			refreshTranslationList(false)
			return 0
		}
		switch id {
		case idTranslationSaveOne:
			saveSelectedTranslation()
			return 0
		case idTranslationSaveAll:
			saveAllTranslations()
			return 0
		case idTranslationRestore:
			if translationSelected >= 0 && translationSelected < len(translationEntries) {
				setText(translationManagerItalian, translationEntries[translationSelected].Original)
				translationStoreCurrentEdit()
				refreshTranslationList(false)
			}
			return 0
		case idTranslationRescan:
			if translationHasDirty() {
				r := msgboxResult("PML Studio - Traduzione", "Prima della nuova scansione ci sono modifiche non salvate.\r\n\r\nSalvarle ora?", MB_YESNOCANCEL|MB_ICONINFORMATION)
				if r == IDCANCEL {
					return 0
				}
				if r == IDYES && !saveAllTranslations() {
					return 0
				}
			}
			setText(translationManagerStatus, "Scansione testi del progetto in corso...")
			translationEntries = scanProjectTranslations(currentProject)
			refreshTranslationList(true)
			return 0
		case idTranslationClose:
			closeTranslationManager(hwnd)
			return 0
		}
	case WM_CLOSE:
		closeTranslationManager(hwnd)
		return 0
	case WM_DESTROY:
		translationManagerOpen = false
		translationManagerWindow = 0
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(uintptr(hwnd), uintptr(msg), w, l)
	return r
}

var translationManagerWndProc = syscall.NewCallback(translationManagerWndProcFn)

func ensureTranslationManagerClass() error {
	if translationManagerRegistered {
		return nil
	}
	h, _, _ := pGetModuleHandleW.Call(0)
	hi := syscall.Handle(h)
	cur, _, _ := pLoadCursorW.Call(0, IDC_ARROW)
	brush, _, _ := pCreateSolidBrush.Call(rgb(244, 244, 244))
	wc := WNDCLASSEX{cbSize: uint32(unsafe.Sizeof(WNDCLASSEX{})), lpfnWndProc: translationManagerWndProc, hInstance: hi, hCursor: syscall.Handle(cur), hbrBackground: syscall.Handle(brush), lpszClassName: wstr(translationManagerClassName)}
	r, _, err := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	if r == 0 {
		return fmt.Errorf("registrazione Traduttore progetto: %v", err)
	}
	translationManagerRegistered = true
	return nil
}

func showProjectTranslationManager(owner syscall.Handle) {
	if translationManagerOpen {
		return
	}
	if strings.TrimSpace(currentProject) == "" {
		msgbox("PML Studio - Traduzione", "Apri prima un progetto.", MB_OK|MB_ICONINFORMATION)
		return
	}
	if err := ensureTranslationManagerClass(); err != nil {
		msgbox("PML Studio - Traduzione", err.Error(), MB_OK|MB_ICONERROR)
		return
	}

	const ww, wh int32 = 1230, 820
	x, y := centerOwnedWindow(ww, wh)
	h, _, _ := pGetModuleHandleW.Call(0)
	hi := syscall.Handle(h)
	translationEntries = scanProjectTranslations(currentProject)
	translationFiltered = nil
	translationSelected = -1
	translationManagerOpen = true
	translationManagerWindow = createWindow(translationManagerClassName, "Traduzione testi progetto - Italiano", WS_OVERLAPPEDWINDOW|WS_VISIBLE|WS_CLIPCHILDREN, x, y, ww, wh, owner, 0, hi)
	if translationManagerWindow == 0 {
		translationManagerOpen = false
		return
	}
	setWindowIcon(translationManagerWindow)

	createWindow("STATIC", "Sezione:", WS_CHILD|WS_VISIBLE, 22, 22, 70, 24, translationManagerWindow, 7960, hi)
	translationManagerSection = createWindow("COMBOBOX", "", WS_CHILD|WS_VISIBLE|WS_TABSTOP|CBS_DROPDOWNLIST|WS_VSCROLL, 95, 17, 260, 240, translationManagerWindow, idTranslationSection, hi)
	setComboFromStrings(translationManagerSection, translationSectionNames(), 0)

	createWindow("STATIC", "Cerca:", WS_CHILD|WS_VISIBLE, 385, 22, 55, 24, translationManagerWindow, 7961, hi)
	translationManagerSearch = createWindow("EDIT", "", WS_CHILD|WS_VISIBLE|WS_BORDER|WS_TABSTOP, 445, 17, 420, 30, translationManagerWindow, idTranslationSearch, hi)
	createWindow("BUTTON", "Riscansiona", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 890, 15, 135, 34, translationManagerWindow, idTranslationRescan, hi)
	createWindow("STATIC", "Lingua destinazione: Italiano (IT)", WS_CHILD|WS_VISIBLE, 1040, 22, 165, 24, translationManagerWindow, 7962, hi)

	translationManagerList = createWindow("LISTBOX", "", WS_CHILD|WS_VISIBLE|WS_BORDER|WS_VSCROLL|WS_TABSTOP|LBS_NOTIFY|LBS_NOINTEGRALHEIGHT, 22, 66, 510, 618, translationManagerWindow, idTranslationList, hi)

	createWindow("STATIC", "Origine:", WS_CHILD|WS_VISIBLE, 555, 66, 90, 24, translationManagerWindow, 7963, hi)
	translationManagerSource = createWindow("EDIT", "", WS_CHILD|WS_VISIBLE|WS_BORDER|WS_VSCROLL|esReadOnly|ES_MULTILINE|ES_AUTOVSCROLL, 555, 92, 640, 70, translationManagerWindow, idTranslationSource, hi)

	createWindow("STATIC", "Testo originale:", WS_CHILD|WS_VISIBLE, 555, 180, 150, 24, translationManagerWindow, 7964, hi)
	translationManagerOriginal = createWindow("EDIT", "", WS_CHILD|WS_VISIBLE|WS_BORDER|WS_VSCROLL|esReadOnly|ES_MULTILINE|ES_AUTOVSCROLL, 555, 207, 640, 175, translationManagerWindow, idTranslationOriginal, hi)

	createWindow("STATIC", "Traduzione italiana:", WS_CHILD|WS_VISIBLE, 555, 400, 170, 24, translationManagerWindow, 7965, hi)
	translationManagerItalian = createWindow("EDIT", "", WS_CHILD|WS_VISIBLE|WS_BORDER|WS_VSCROLL|WS_TABSTOP|ES_MULTILINE|ES_AUTOVSCROLL|ES_WANTRETURN, 555, 427, 640, 205, translationManagerWindow, idTranslationItalian, hi)

	createWindow("BUTTON", "Ripristina originale", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 555, 650, 165, 38, translationManagerWindow, idTranslationRestore, hi)
	createWindow("BUTTON", "Salva testo", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 820, 650, 150, 38, translationManagerWindow, idTranslationSaveOne, hi)
	createWindow("BUTTON", "Salva tutte", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 980, 650, 150, 38, translationManagerWindow, idTranslationSaveAll, hi)

	translationManagerStatus = createWindow("STATIC", "", WS_CHILD|WS_VISIBLE, 22, 705, 830, 28, translationManagerWindow, idTranslationStatus, hi)
	createWindow("STATIC", "Gli script vengono modificati solo nelle chiamate di testo riconosciute; PLM non sostituisce stringhe di codice alla cieca.", WS_CHILD|WS_VISIBLE, 22, 738, 920, 28, translationManagerWindow, 7966, hi)
	createWindow("BUTTON", "Chiudi", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 1040, 720, 155, 40, translationManagerWindow, idTranslationClose, hi)

	refreshTranslationList(true)

	pEnableWindow.Call(uintptr(owner), 0)
	pShowWindow.Call(uintptr(translationManagerWindow), SW_SHOW)
	pUpdateWindow.Call(uintptr(translationManagerWindow))
	var m MSG
	repostQuit := false
	for translationManagerOpen {
		r, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) == -1 {
			break
		}
		if r == 0 {
			repostQuit = true
			break
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		pDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
	pEnableWindow.Call(uintptr(owner), 1)
	pSetFocus.Call(uintptr(owner))
	if repostQuit {
		pPostQuitMessage.Call(0)
	}
}

func addProjectTranslationCommand() {
	// When editing an existing event, persist its current draft first. This
	// prevents the translation manager from editing an older on-disk copy that
	// would later be overwritten by the event editor.
	if eventEditorOpen && eventEditorEditingIndex >= 0 {
		if !commitEventEditor(false) {
			return
		}
	}
	showProjectTranslationManager(eventEditorWindow)
}
