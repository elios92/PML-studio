//go:build windows

package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Canonical post-conversion asset layout. A converted PLM project always uses
// <project>/assets/Graphics and <project>/assets/Audio as its primary runtime
// asset roots. Legacy Essentials/RMXP paths are accepted only as read fallbacks.
var canonicalGraphicsCategories = []string{
	"Animations",
	"Autotiles",
	"Battle animations",
	"Battlebacks",
	"Characters",
	"Fogs",
	"Gameovers",
	"Icons",
	"Items",
	"Panoramas",
	"Pictures",
	"Pokemon",
	"Tilesets",
	"Titles",
	"Trainers",
	"Transitions",
	"Weather",
	"Windowskins",
}

var canonicalAudioCategories = []string{"BGM", "BGS", "ME", "SE"}

func canonicalGraphicsRoot(project string) string {
	project = filepath.Clean(strings.TrimSpace(project))
	if project == "" || project == "." {
		return ""
	}
	return filepath.Join(project, "assets", "Graphics")
}

func canonicalAudioRoot(project string) string {
	project = filepath.Clean(strings.TrimSpace(project))
	if project == "" || project == "." {
		return ""
	}
	return filepath.Join(project, "assets", "Audio")
}

func canonicalGraphicsDir(project, category string) string {
	root := canonicalGraphicsRoot(project)
	if root == "" {
		return ""
	}
	category = strings.Trim(strings.TrimSpace(category), `/\\`)
	if category == "" {
		return root
	}
	return filepath.Join(root, filepath.FromSlash(category))
}

func canonicalAudioDir(project, category string) string {
	root := canonicalAudioRoot(project)
	if root == "" {
		return ""
	}
	category = strings.Trim(strings.TrimSpace(category), `/\\`)
	if category == "" {
		return root
	}
	return filepath.Join(root, filepath.FromSlash(category))
}

// projectGraphicsCategoryDirs returns canonical-first search locations. The
// first entry is always assets/Graphics/<category>; remaining entries exist
// only for compatibility with unconverted or older project layouts.
func projectGraphicsCategoryDirs(project, category string) []string {
	project = filepath.Clean(strings.TrimSpace(project))
	category = strings.Trim(strings.TrimSpace(category), `/\\`)
	candidates := []string{
		canonicalGraphicsDir(project, category),
		filepath.Join(project, "Graphics", filepath.FromSlash(category)),
		filepath.Join(project, "game", "assets", "Graphics", filepath.FromSlash(category)),
		filepath.Join(project, "game", "Graphics", filepath.FromSlash(category)),
		filepath.Join(project, "converted", "assets", "Graphics", filepath.FromSlash(category)),
		filepath.Join(project, "converted", "Graphics", filepath.FromSlash(category)),
	}
	return uniqueExistingDirs(candidates)
}

func projectAudioCategoryDirs(project, category string) []string {
	project = filepath.Clean(strings.TrimSpace(project))
	category = strings.Trim(strings.TrimSpace(category), `/\\`)
	candidates := []string{
		canonicalAudioDir(project, category),
		filepath.Join(project, "Audio", filepath.FromSlash(category)),
		filepath.Join(project, "game", "assets", "Audio", filepath.FromSlash(category)),
		filepath.Join(project, "game", "Audio", filepath.FromSlash(category)),
		filepath.Join(project, "audio", strings.ToLower(category)),
	}
	return uniqueExistingDirs(candidates)
}

func ensureCanonicalAssetLayout(project string) error {
	gRoot := canonicalGraphicsRoot(project)
	aRoot := canonicalAudioRoot(project)
	if gRoot == "" || aRoot == "" {
		return nil
	}
	if err := os.MkdirAll(gRoot, 0755); err != nil {
		return err
	}
	for _, category := range canonicalGraphicsCategories {
		if err := os.MkdirAll(filepath.Join(gRoot, category), 0755); err != nil {
			return err
		}
	}
	// Debug resources are optional in Essentials, but these canonical folders
	// make PLM editors deterministic without requiring special-case paths.
	for _, rel := range []string{filepath.Join("UI", "Debug"), filepath.Join("Pictures", "Debug")} {
		if err := os.MkdirAll(filepath.Join(gRoot, rel), 0755); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(aRoot, 0755); err != nil {
		return err
	}
	for _, category := range canonicalAudioCategories {
		if err := os.MkdirAll(filepath.Join(aRoot, category), 0755); err != nil {
			return err
		}
	}
	return nil
}

type AssetManifest struct {
	Version      int            `json:"version"`
	GraphicsRoot string         `json:"graphics_root"`
	AudioRoot    string         `json:"audio_root"`
	Graphics     map[string]int `json:"graphics"`
	Audio        map[string]int `json:"audio"`
}

func countFilesFlat(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if !e.IsDir() {
			n++
		}
	}
	return n
}

func writeAssetManifest(project string) error {
	m := AssetManifest{
		Version:      1,
		GraphicsRoot: filepath.ToSlash(filepath.Join("assets", "Graphics")),
		AudioRoot:    filepath.ToSlash(filepath.Join("assets", "Audio")),
		Graphics:     map[string]int{},
		Audio:        map[string]int{},
	}
	cats := append([]string(nil), canonicalGraphicsCategories...)
	sort.Strings(cats)
	for _, c := range cats {
		m.Graphics[c] = countFilesFlat(canonicalGraphicsDir(project, c))
	}
	for _, c := range canonicalAudioCategories {
		m.Audio[c] = countFilesFlat(canonicalAudioDir(project, c))
	}
	return writeJSON(filepath.Join(project, "converted", "asset_manifest.json"), m)
}
