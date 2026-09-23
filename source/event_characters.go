//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var (
	characterCatalogProject string
	characterCatalog        = map[string]string{}
	characterSurfaceCache   = map[string]*PixelSurface{}
)

func characterGraphicsDirs(root string) []string {
	if strings.TrimSpace(root) == "" {
		return nil
	}
	candidates := []string{
		filepath.Join(root, "assets", "Graphics", "Characters"),
		filepath.Join(root, "Graphics", "Characters"),
		filepath.Join(root, "converted", "Graphics", "Characters"),
		filepath.Join(root, "converted", "assets", "Graphics", "Characters"),
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(candidates))
	for _, p := range candidates {
		clean := filepath.Clean(p)
		key := strings.ToLower(clean)
		if seen[key] {
			continue
		}
		if st, err := os.Stat(clean); err == nil && st.IsDir() {
			seen[key] = true
			out = append(out, clean)
		}
	}
	return out
}

func characterKey(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\\", "/"))
	s = strings.TrimPrefix(s, "./")
	ext := filepath.Ext(s)
	if ext != "" {
		s = strings.TrimSuffix(s, ext)
	}
	return strings.ToLower(s)
}

func addCharacterCatalogPath(baseDir, path string) {
	rel, err := filepath.Rel(baseDir, path)
	if err != nil {
		return
	}
	rel = filepath.ToSlash(rel)
	withoutExt := strings.TrimSuffix(rel, filepath.Ext(rel))
	base := strings.TrimSuffix(filepath.Base(rel), filepath.Ext(rel))
	for _, key := range []string{characterKey(withoutExt), characterKey(base)} {
		if key != "" {
			if _, exists := characterCatalog[key]; !exists {
				characterCatalog[key] = path
			}
		}
	}
}

// rebuildCharacterCatalog indexes the project's real Graphics/Characters PNGs.
// It does not copy or convert assets: the imported game graphics remain the source of truth.
func rebuildCharacterCatalog(root string) error {
	characterCatalogProject = filepath.Clean(root)
	characterCatalog = map[string]string{}
	characterSurfaceCache = map[string]*PixelSurface{}

	dirs := characterGraphicsDirs(root)
	for _, dir := range dirs {
		err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				return nil
			}
			if !strings.EqualFold(filepath.Ext(d.Name()), ".png") {
				return nil
			}
			addCharacterCatalogPath(dir, path)
			return nil
		})
		if err != nil {
			return err
		}
	}
	mapLogf("[CHARACTERS] indicizzati %d sprite da %d cartelle Graphics/Characters", len(characterCatalog), len(dirs))
	return nil
}

func ensureCharacterCatalog() {
	if currentProject == "" {
		return
	}
	if !strings.EqualFold(filepath.Clean(currentProject), characterCatalogProject) {
		_ = rebuildCharacterCatalog(currentProject)
	}
}

func characterSpritePath(name string) string {
	ensureCharacterCatalog()
	if strings.TrimSpace(name) == "" {
		return ""
	}
	if p := characterCatalog[characterKey(name)]; p != "" {
		return p
	}
	// Some converters keep an extension or a leading Graphics/Characters prefix.
	n := strings.ReplaceAll(strings.TrimSpace(name), "\\", "/")
	n = strings.TrimPrefix(strings.ToLower(n), "graphics/characters/")
	if p := characterCatalog[characterKey(n)]; p != "" {
		return p
	}
	return ""
}

func characterSpriteSurface(name string) (*PixelSurface, string, error) {
	path := characterSpritePath(name)
	if path == "" {
		return nil, "", fmt.Errorf("sprite Characters non trovato: %s", name)
	}
	key := strings.ToLower(filepath.Clean(path))
	if s := characterSurfaceCache[key]; s != nil {
		return s, path, nil
	}
	s, err := loadPNGSurface(path)
	if err != nil {
		return nil, path, err
	}
	characterSurfaceCache[key] = s
	return s, path, nil
}

func directionRow(direction int) int {
	switch direction {
	case 4:
		return 1
	case 6:
		return 2
	case 8:
		return 3
	default:
		return 0 // 2 = giù
	}
}

// blendEventCharacterOnCell draws one RMXP 4x4 character frame over a 32x32
// map-cell buffer. Large sprites are proportionally fitted in the tile for the
// event overview; the original PNG is never modified.
func blendEventCharacterOnCell(dst []uint32, e EditorEvent) bool {
	if len(dst) < 32*32 || strings.TrimSpace(e.CharacterName) == "" {
		return false
	}
	s, _, err := characterSpriteSurface(e.CharacterName)
	if err != nil || s == nil || s.Width < 4 || s.Height < 4 {
		return false
	}
	fw, fh := s.Width/4, s.Height/4
	if fw <= 0 || fh <= 0 {
		return false
	}
	pattern := e.CharacterPattern
	if pattern < 0 {
		pattern = 0
	}
	pattern %= 4
	row := directionRow(e.CharacterDirection)
	sx, sy := pattern*fw, row*fh
	if sx+fw > s.Width || sy+fh > s.Height {
		return false
	}

	dw, dh := fw, fh
	if dw > 32 || dh > 32 {
		scaleW := 32.0 / float64(dw)
		scaleH := 32.0 / float64(dh)
		scale := scaleW
		if scaleH < scale {
			scale = scaleH
		}
		dw = int(float64(dw) * scale)
		dh = int(float64(dh) * scale)
		if dw < 1 {
			dw = 1
		}
		if dh < 1 {
			dh = 1
		}
	}
	dx0 := (32 - dw) / 2
	dy0 := 32 - dh
	for dy := 0; dy < dh; dy++ {
		srcY := sy + dy*fh/dh
		for dx := 0; dx < dw; dx++ {
			srcX := sx + dx*fw/dw
			si := srcY*s.Width + srcX
			if si < 0 || si >= len(s.Pixels) {
				continue
			}
			a := byte(255)
			if len(s.Alpha) == len(s.Pixels) {
				a = s.Alpha[si]
			}
			if a == 0 {
				continue
			}
			di := (dy0+dy)*32 + dx0 + dx
			dst[di] = blendBGR(dst[di], s.Pixels[si], a)
		}
	}
	return true
}

func characterSpriteNames() []string {
	ensureCharacterCatalog()
	seen := map[string]bool{}
	out := []string{}
	for _, path := range characterCatalog {
		base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		key := strings.ToLower(strings.TrimSpace(base))
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, base)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i]) < strings.ToLower(out[j]) })
	return out
}
