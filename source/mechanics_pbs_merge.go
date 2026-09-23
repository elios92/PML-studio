package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// These are the PBS databases that may be imported additively from an
// Essentials generation pack. Existing project records always win.
// encounters.txt and berry_plants.txt are intentionally excluded because they
// are project/gameplay configuration, not required definitions for a Pokémon.
var mechanicsAdditivePBSFiles = []string{
	"pokemon.txt",
	"pokemon_forms.txt",
	"pokemon_metrics.txt",
	"abilities.txt",
	"moves.txt",
	"items.txt",
	"types.txt",
}

type mechanicsPBSFileMerge struct {
	File            string   `json:"file"`
	AddedIDs        []string `json:"added_ids,omitempty"`
	ExistingRecords int      `json:"existing_records"`
	SourceRecords   int      `json:"source_records"`
}

type mechanicsPBSMergeReport struct {
	Version      int                     `json:"version"`
	Schema       string                  `json:"schema"`
	Generation   int                     `json:"generation"`
	Mode         string                  `json:"mode"`
	SourceRoot   string                  `json:"source_root,omitempty"`
	TargetRoot   string                  `json:"target_root"`
	AddedPokemon []string                `json:"added_pokemon,omitempty"`
	Files        []mechanicsPBSFileMerge `json:"files,omitempty"`
	CompletedAt  string                  `json:"completed_at"`
}

func (r mechanicsPBSMergeReport) AddedPokemonCount() int { return len(r.AddedPokemon) }

func mechanicsMergeReportPath(project string) string {
	return filepath.Join(project, "converted", "data", "plm_mechanics_last_merge.json")
}

func parsePBSRecordStarts(lines []string) ([]int, []string) {
	starts := make([]int, 0)
	ids := make([]string, 0)
	for i, line := range lines {
		t := strings.TrimSpace(line)
		if !strings.HasPrefix(t, "[") {
			continue
		}
		end := strings.Index(t, "]")
		if end <= 1 {
			continue
		}
		id := strings.TrimSpace(t[1:end])
		if id == "" {
			continue
		}
		starts = append(starts, i)
		ids = append(ids, id)
	}
	return starts, ids
}

func splitPBSBytes(b []byte) (lines []string, newline string, bom bool) {
	newline = "\n"
	if len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF {
		bom = true
		b = b[3:]
	}
	s := string(b)
	if strings.Contains(s, "\r\n") {
		newline = "\r\n"
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return strings.Split(s, "\n"), newline, bom
}

func trimPBSRecordTail(lines []string) []string {
	end := len(lines)
	for end > 0 {
		t := strings.TrimSpace(lines[end-1])
		if t == "" || t == "#-------------------------------" {
			end--
			continue
		}
		break
	}
	return lines[:end]
}

func writePBSAtomicReplace(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	if mode == 0 {
		mode = 0644
	}
	tmp := path + ".plm_genmerge_tmp"
	bak := path + ".plm_genmerge_bak"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	_ = os.Remove(bak)
	hadOld := false
	if _, err := os.Stat(path); err == nil {
		if err := os.Rename(path, bak); err != nil {
			_ = os.Remove(tmp)
			return err
		}
		hadOld = true
	}
	if err := os.Rename(tmp, path); err != nil {
		if hadOld {
			_ = os.Rename(bak, path)
		}
		_ = os.Remove(tmp)
		return err
	}
	_ = os.Remove(bak)
	return nil
}

// mergeMissingPBSRecords appends only records whose internal ID is absent from
// targetPath. Existing target bytes are kept byte-for-byte as the prefix of the
// new file, so custom records/fields are never rewritten by this merge.
func mergeMissingPBSRecords(targetPath, sourcePath string) (mechanicsPBSFileMerge, error) {
	res := mechanicsPBSFileMerge{File: filepath.Base(targetPath)}
	sourceBytes, err := os.ReadFile(sourcePath)
	if err != nil {
		return res, err
	}
	sourceLines, _, _ := splitPBSBytes(sourceBytes)
	sourceStarts, sourceIDs := parsePBSRecordStarts(sourceLines)
	res.SourceRecords = len(sourceIDs)
	if len(sourceIDs) == 0 {
		return res, nil
	}

	targetBytes, err := os.ReadFile(targetPath)
	if os.IsNotExist(err) {
		res.AddedIDs = append(res.AddedIDs, sourceIDs...)
		mode := os.FileMode(0644)
		if st, statErr := os.Stat(sourcePath); statErr == nil {
			mode = st.Mode().Perm()
		}
		if err := writePBSAtomicReplace(targetPath, sourceBytes, mode); err != nil {
			return res, err
		}
		return res, nil
	}
	if err != nil {
		return res, err
	}

	targetLines, targetNewline, _ := splitPBSBytes(targetBytes)
	_, targetIDs := parsePBSRecordStarts(targetLines)
	res.ExistingRecords = len(targetIDs)
	existing := make(map[string]struct{}, len(targetIDs))
	for _, id := range targetIDs {
		existing[strings.ToUpper(strings.TrimSpace(id))] = struct{}{}
	}

	appendLines := make([]string, 0)
	for i, id := range sourceIDs {
		key := strings.ToUpper(strings.TrimSpace(id))
		if _, ok := existing[key]; ok {
			continue
		}
		start := sourceStarts[i]
		end := len(sourceLines)
		if i+1 < len(sourceStarts) {
			end = sourceStarts[i+1]
		}
		block := trimPBSRecordTail(append([]string(nil), sourceLines[start:end]...))
		if len(block) == 0 {
			continue
		}
		appendLines = append(appendLines, "#-------------------------------")
		appendLines = append(appendLines, block...)
		res.AddedIDs = append(res.AddedIDs, id)
		existing[key] = struct{}{}
	}
	if len(res.AddedIDs) == 0 {
		return res, nil
	}

	// Preserve the original target file byte-for-byte; only append new blocks.
	out := append([]byte(nil), targetBytes...)
	if len(out) > 0 && out[len(out)-1] != '\n' && out[len(out)-1] != '\r' {
		out = append(out, []byte(targetNewline)...)
	}
	if len(out) > 0 {
		out = append(out, []byte(targetNewline)...)
	}
	out = append(out, []byte(strings.Join(appendLines, targetNewline))...)
	out = append(out, []byte(targetNewline)...)

	mode := os.FileMode(0644)
	if st, statErr := os.Stat(targetPath); statErr == nil {
		mode = st.Mode().Perm()
	}
	if err := writePBSAtomicReplace(targetPath, out, mode); err != nil {
		return res, err
	}
	return res, nil
}

// mergeGenerationPBSAdditive uses PBS/Gen N only as an import source. The
// project's base PBS remains the source of truth. Existing IDs are preserved;
// missing definitions are appended. The Pokémon count reported to the UI comes
// only from pokemon.txt, while the related DB files are merged additively so a
// newly imported Pokémon does not point to missing official moves/abilities.
func mergeGenerationPBSAdditive(project string, gen int) (mechanicsPBSMergeReport, error) {
	gen = clampMechanicsGeneration(gen)
	base := projectPBSBaseRoot(project)
	report := mechanicsPBSMergeReport{
		Version:     1,
		Schema:      "plm.mechanics.pbs_merge.v1",
		Generation:  gen,
		Mode:        "add_missing_only",
		TargetRoot:  filepath.ToSlash(base),
		CompletedAt: time.Now().Format(time.RFC3339),
	}

	source, ok := mechanicsGenerationPBSDir(project, gen)
	if !ok || strings.EqualFold(filepath.Clean(source), filepath.Clean(base)) {
		_ = writeMechanicsPBSMergeReport(project, report)
		return report, nil
	}
	report.SourceRoot = filepath.ToSlash(source)

	for _, name := range mechanicsAdditivePBSFiles {
		sourcePath := filepath.Join(source, name)
		if st, err := os.Stat(sourcePath); err != nil || st.IsDir() {
			continue
		}
		targetPath := filepath.Join(base, name)
		fileResult, err := mergeMissingPBSRecords(targetPath, sourcePath)
		if err != nil {
			return report, fmt.Errorf("merge %s: %w", name, err)
		}
		report.Files = append(report.Files, fileResult)
		if strings.EqualFold(name, "pokemon.txt") {
			report.AddedPokemon = append(report.AddedPokemon, fileResult.AddedIDs...)
		}
	}
	sort.Strings(report.AddedPokemon)
	report.CompletedAt = time.Now().Format(time.RFC3339)
	if err := writeMechanicsPBSMergeReport(project, report); err != nil {
		return report, err
	}
	return report, nil
}

func writeMechanicsPBSMergeReport(project string, report mechanicsPBSMergeReport) error {
	if strings.TrimSpace(project) == "" {
		return nil
	}
	path := mechanicsMergeReportPath(project)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
