//go:build windows

package main

import (
	"archive/zip"
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"context"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
	"unsafe"
)

// The converted project must be runnable immediately after import.  The
// embedded runtime is the current PLM/Python engine plus a portable CPython
// distribution.  It is project-agnostic: project assets/data are always copied
// from the selected Essentials source and the project title placeholder is
// personalized during extraction.
//
//go:embed runtime_templates/plm_runtime_core.zip
var plmRuntimeCoreZip []byte

// The game launcher is stored inside plm_runtime_core.zip under
// _plm_templates/PLM_Game_Launcher.exe. Keeping it inside the runtime archive
// avoids a second //go:embed dependency that can be lost/quarantined when a
// source patch is extracted on Windows.
const plmGameLauncherArchivePath = "_plm_templates/PLM_Game_Launcher.exe"

const runtimeTemplateVersion = 2
const runtimeProjectTitlePlaceholder = "__PLM_PROJECT_TITLE__"

type runtimeInstallManifest struct {
	Schema          string `json:"schema"`
	Version         int    `json:"version"`
	TemplateVersion int    `json:"template_version"`
	ProjectName     string `json:"project_name"`
	ReleaseEXE      string `json:"release_exe"`
	DebugEXE        string `json:"debug_exe"`
	InstalledAt     string `json:"installed_at"`
}

func safeGameExecutableBaseName(name string) string {
	name = strings.TrimSpace(name)
	// Keep Windows executable filenames ASCII-stable. In particular, normalize
	// Pokemon spellings with accented/corrupted e (Pokémon/Pokèmon/Pok�mon)
	// to "Pokemon" so Explorer, scripts and launch manifests do not display
	// mojibake or inconsistent filenames.
	replacer := strings.NewReplacer(
		"Pokémon", "Pokemon", "POKÉMON", "POKEMON", "pokémon", "pokemon",
		"Pokèmon", "Pokemon", "POKÈMON", "POKEMON", "pokèmon", "pokemon",
		"Pok�mon", "Pokemon", "POK�MON", "POKEMON", "pok�mon", "pokemon",
	)
	name = replacer.Replace(name)
	// If the source title was already decoded with replacement characters,
	// normalize the common "Pok?mon/Pok�mon" shape without relying on the exact
	// Unicode replacement rune.
	pokemonBroken := regexp.MustCompile(`(?i)pok[^a-zA-Z0-9]mon`)
	name = pokemonBroken.ReplaceAllString(name, "Pokemon")
	if name == "" {
		name = "Pokemon Game"
	}
	// Windows-invalid filename characters and ASCII control characters.
	invalid := regexp.MustCompile(`[<>:"/\\|?*\x00-\x1F]`)
	name = invalid.ReplaceAllString(name, " ")
	name = strings.Join(strings.Fields(name), " ")
	name = strings.Trim(name, " .")
	if name == "" {
		name = "Pokemon Game"
	}
	upper := strings.ToUpper(name)
	reserved := map[string]bool{
		"CON": true, "PRN": true, "AUX": true, "NUL": true,
		"COM1": true, "COM2": true, "COM3": true, "COM4": true, "COM5": true,
		"COM6": true, "COM7": true, "COM8": true, "COM9": true,
		"LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true, "LPT5": true,
		"LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true,
	}
	if reserved[upper] {
		name += " Game"
	}
	return name
}

func isRuntimeTextFile(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".py", ".txt", ".json", ".md", ".ini", ".cfg":
		return true
	default:
		return false
	}
}

func runtimePathInsideDestination(dest, archiveName string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(archiveName))
	if clean == "." || clean == "" || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("percorso runtime non sicuro: %q", archiveName)
	}
	root := filepath.Clean(dest)
	target := filepath.Join(root, clean)
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("percorso runtime fuori progetto: %q", archiveName)
	}
	return target, nil
}

func readEmbeddedRuntimeFile(archiveName string) ([]byte, error) {
	if len(plmRuntimeCoreZip) == 0 {
		return nil, fmt.Errorf("template runtime PLM non incorporato")
	}
	zr, err := zip.NewReader(bytes.NewReader(plmRuntimeCoreZip), int64(len(plmRuntimeCoreZip)))
	if err != nil {
		return nil, fmt.Errorf("template runtime PLM non leggibile: %w", err)
	}
	wanted := filepath.ToSlash(archiveName)
	for _, entry := range zr.File {
		if filepath.ToSlash(entry.Name) != wanted {
			continue
		}
		if entry.FileInfo().IsDir() {
			return nil, fmt.Errorf("template runtime %s e' una cartella", wanted)
		}
		rc, err := entry.Open()
		if err != nil {
			return nil, fmt.Errorf("template runtime %s non leggibile: %w", wanted, err)
		}
		data, readErr := io.ReadAll(rc)
		_ = rc.Close()
		if readErr != nil {
			return nil, fmt.Errorf("template runtime %s non leggibile: %w", wanted, readErr)
		}
		if len(data) == 0 {
			return nil, fmt.Errorf("template runtime %s vuoto", wanted)
		}
		return data, nil
	}
	return nil, fmt.Errorf("template runtime mancante nell'archivio: %s", wanted)
}

func normalizeRuntimeCanonicalPBSPaths(data []byte) []byte {
	replacements := [][2][]byte{
		{[]byte(`root/"PBS"`), []byte(`root/"converted"/"PBS"`)},
		{[]byte(`root / "PBS"`), []byte(`root / "converted" / "PBS"`)},
		{[]byte(`project_root / "PBS"`), []byte(`project_root / "converted" / "PBS"`)},
		{[]byte(`self.project_root / "PBS"`), []byte(`self.project_root / "converted" / "PBS"`)},
	}
	for _, pair := range replacements {
		data = bytes.ReplaceAll(data, pair[0], pair[1])
	}
	return data
}


const runtimeControlsHelpPython = `"""PML controls help using the imported Pokémon Essentials UI asset."""
from __future__ import annotations

from pathlib import Path
from typing import Any
import pygame

from game.ui_assets import UIAssets


def _font(root: Path, size: int) -> pygame.font.Font:
    for name in ("power clear.ttf", "Power Clear.ttf"):
        path = root / "assets" / "Fonts" / name
        if path.is_file():
            return pygame.font.Font(str(path), size)
    return pygame.font.Font(None, size)


def _wrap(font: pygame.font.Font, text: str, width: int) -> list[str]:
    words = text.split()
    lines: list[str] = []
    current = ""
    for word in words:
        trial = word if not current else current + " " + word
        if current and font.size(trial)[0] > width:
            lines.append(current)
            current = word
        else:
            current = trial
    if current:
        lines.append(current)
    return lines


def show_controls_help(graphics: Any, project_root: Path) -> None:
    root = Path(project_root)
    assets = UIAssets(root)
    original = assets.image("Controls help/help_bg")
    if original is None:
        raise RuntimeError(
            "UI Essentials mancante: assets/Graphics/Pictures/Controls help/help_bg"
        )

    # This is intentionally not the RPG Maker F1/F8 help. PML input bindings
    # are configured from the in-game Options menu.
    message = "Keyboard and controller controls are fully customizable in Game Settings."

    while True:
        for event in pygame.event.get():
            if event.type == pygame.QUIT:
                raise SystemExit(0)
            if event.type == pygame.KEYDOWN:
                return

        screen = graphics.screen
        sw, sh = screen.get_size()
        screen.fill((0, 0, 0))
        ow, oh = original.get_size()

        # Keep the imported Essentials board intact. For this compatibility
        # phase there is no arbitrary responsive stretching.
        scale = min(sw / ow, sh / oh)
        if scale >= 1.0:
            scale = max(1.0, float(int(scale)))
        dw, dh = max(1, round(ow * scale)), max(1, round(oh * scale))
        surface = original if (dw, dh) == (ow, oh) else pygame.transform.scale(original, (dw, dh))
        ox, oy = (sw - dw) // 2, (sh - dh) // 2
        screen.blit(surface, (ox, oy))

        logical_scale = dw / 512.0
        text_font = _font(root, max(16, round(22 * logical_scale)))
        left = ox + round(128 * logical_scale)
        top = oy + round(118 * logical_scale)
        width = max(120, dw - round(170 * logical_scale))
        line_h = text_font.get_height() + max(2, round(3 * logical_scale))
        for line in _wrap(text_font, message, width):
            screen.blit(text_font.render(line, True, (72, 72, 72)), (left, top))
            top += line_h
        graphics.update()
`

func patchRuntimeButtonEventScene(data []byte) ([]byte, error) {
	text := string(data)
	marker := `if "pbEventScreen(ButtonEventScene)" in script:`
	pos := strings.Index(text, marker)
	if pos < 0 {
		return nil, fmt.Errorf("runtime map_scene.py: ButtonEventScene non trovata")
	}
	lineStart := strings.LastIndex(text[:pos], "\n") + 1
	indent := text[lineStart:pos]
	nextMarker := "\n" + indent + "if "
	nextRel := strings.Index(text[pos+len(marker):], nextMarker)
	if nextRel < 0 {
		return nil, fmt.Errorf("runtime map_scene.py: fine blocco ButtonEventScene non trovata")
	}
	end := pos + len(marker) + nextRel
	replacement := marker + "\n" + indent + "    from game.controls_help import show_controls_help\n" +
		indent + "    show_controls_help(self.graphics, self.root)\n" +
		indent + "    return"
	return []byte(text[:pos] + replacement + text[end:]), nil
}

func installRuntimeUICompatibilityPatch(dest string) error {
	controlsPath := filepath.Join(dest, "game", "controls_help.py")
	if err := writeBytesAtomic(controlsPath, []byte(runtimeControlsHelpPython), 0644); err != nil {
		return fmt.Errorf("installazione UI controlli PML: %w", err)
	}
	mapPath := filepath.Join(dest, "game", "map_scene.py")
	data, err := os.ReadFile(mapPath)
	if err != nil {
		return fmt.Errorf("lettura runtime map_scene.py: %w", err)
	}
	patched, err := patchRuntimeButtonEventScene(data)
	if err != nil {
		return err
	}
	if err := writeBytesAtomic(mapPath, patched, 0644); err != nil {
		return fmt.Errorf("aggiornamento ButtonEventScene runtime: %w", err)
	}
	return nil
}

func installRuntimeCore(dest, projectName string) (releaseExe, debugExe string, err error) {
	if len(plmRuntimeCoreZip) == 0 {
		return "", "", fmt.Errorf("template runtime PLM non incorporato")
	}
	zr, err := zip.NewReader(bytes.NewReader(plmRuntimeCoreZip), int64(len(plmRuntimeCoreZip)))
	if err != nil {
		return "", "", fmt.Errorf("template runtime PLM non leggibile: %w", err)
	}

	// The converted game executable is the already-compiled native PML Studio
	// runtime binary itself, switched to game-host mode by runtime_install.json.
	// This avoids a second generic launcher process and, critically, removes the
	// dependency on pythonw.exe as the visible/owning game process.
	selfPath, err := os.Executable()
	if err != nil {
		return "", "", fmt.Errorf("eseguibile PML Studio corrente non disponibile: %w", err)
	}
	launcherBytes, err := os.ReadFile(selfPath)
	if err != nil || len(launcherBytes) == 0 {
		return "", "", fmt.Errorf("eseguibile PML Studio corrente non leggibile: %w", err)
	}
	for _, entry := range zr.File {
		archiveName := filepath.ToSlash(entry.Name)
		if strings.HasPrefix(archiveName, "_plm_templates/") {
			continue
		}
		// CPython is hosted in-process by <Nome progetto>.exe. Never publish a
		// pythonw.exe beside the converted game: it would create a second,
		// misleading executable identity and a child process that can hang.
		if strings.EqualFold(filepath.Base(filepath.FromSlash(archiveName)), "pythonw.exe") {
			continue
		}

		target, pathErr := runtimePathInsideDestination(dest, entry.Name)
		if pathErr != nil {
			return "", "", pathErr
		}
		if entry.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0755); err != nil {
				return "", "", err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return "", "", err
		}
		rc, openErr := entry.Open()
		if openErr != nil {
			return "", "", openErr
		}
		data, readErr := io.ReadAll(rc)
		_ = rc.Close()
		if readErr != nil {
			return "", "", readErr
		}
		if isRuntimeTextFile(target) && bytes.Contains(data, []byte(runtimeProjectTitlePlaceholder)) {
			data = bytes.ReplaceAll(data, []byte(runtimeProjectTitlePlaceholder), []byte(projectName))
		}
		// converted/PBS is the single canonical PBS tree for PLM projects.
		// Older runtime templates still referenced a root-level PBS folder,
		// which made New Game fail immediately although conversion had correctly
		// preserved the source PBS under converted/PBS. Normalize those legacy
		// Python paths while installing the embedded runtime; do not duplicate PBS.
		if strings.EqualFold(filepath.Ext(target), ".py") {
			data = normalizeRuntimeCanonicalPBSPaths(data)
		}
		mode := os.FileMode(0644)
		if strings.EqualFold(filepath.Ext(target), ".exe") {
			mode = 0755
		}
		if err := writeBytesAtomic(target, data, mode); err != nil {
			return "", "", err
		}
	}

	// Compatibility patch: preserve the imported Essentials controls UI while
	// routing input configuration to PML's real in-game Options system.
	if err := installRuntimeUICompatibilityPatch(dest); err != nil {
		return "", "", err
	}

	base := safeGameExecutableBaseName(projectName)
	releaseExe = base + ".exe"
	debugExe = base + " DEBUG.exe"
	if len(launcherBytes) == 0 {
		return "", "", fmt.Errorf("launcher Windows PLM mancante nel template runtime (%s)", plmGameLauncherArchivePath)
	}
	if err := writeBytesAtomic(filepath.Join(dest, releaseExe), launcherBytes, 0755); err != nil {
		return "", "", fmt.Errorf("creazione %s: %w", releaseExe, err)
	}
	if err := writeBytesAtomic(filepath.Join(dest, debugExe), launcherBytes, 0755); err != nil {
		return "", "", fmt.Errorf("creazione %s: %w", debugExe, err)
	}

	manifest := runtimeInstallManifest{
		Schema:          "plm.runtime.install",
		Version:         1,
		TemplateVersion: runtimeTemplateVersion,
		ProjectName:     projectName,
		ReleaseEXE:      releaseExe,
		DebugEXE:        debugExe,
		InstalledAt:     time.Now().Format(time.RFC3339),
	}
	if err := writeJSON(filepath.Join(dest, "converted", "runtime_install.json"), manifest); err != nil {
		return "", "", err
	}
	return releaseExe, debugExe, nil
}

func replaceFileAtomicWindows(tmp, path string) error {
	r, _, callErr := pMoveFileExW.Call(
		uintptr(unsafe.Pointer(wstr(tmp))),
		uintptr(unsafe.Pointer(wstr(path))),
		MOVEFILE_REPLACE_EXISTING|MOVEFILE_WRITE_THROUGH,
	)
	if r == 0 {
		return fmt.Errorf("sostituzione atomica fallita: %v", callErr)
	}
	return nil
}

func writeBytesAtomic(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	if err := os.Chmod(tmp, mode); err != nil && !os.IsPermission(err) {
		_ = os.Remove(tmp)
		return err
	}
	// Do not delete a valid destination before publishing the complete temp
	// file. MoveFileExW gives Windows replace-existing semantics and asks the OS
	// to flush the replacement before returning.
	if err := replaceFileAtomicWindows(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func validateLauncherExecution(dest string) error {
	manifestData, err := os.ReadFile(filepath.Join(dest, "converted", "runtime_install.json"))
	if err != nil {
		return fmt.Errorf("manifest runtime mancante: %w", err)
	}
	var manifest runtimeInstallManifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		return fmt.Errorf("manifest runtime non valido: %w", err)
	}
	// Probe the complete launcher -> bundled Python -> main.py chain without
	// entering the game loop. The launcher forwards command-line arguments, and
	// main.py handles --help in argparse before pygame/scene initialization.
	// PLM_TEST_MODE is kept as an additional signal for launcher versions that
	// support it, but --help is the deterministic termination contract.
	for _, exe := range []string{manifest.ReleaseEXE, manifest.DebugEXE} {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		cmd := exec.CommandContext(ctx, filepath.Join(dest, exe), "--help")
		cmd.Dir = dest
		cmd.Env = append(os.Environ(), "PLM_TEST_MODE=1", "PYTHONUTF8=1")
		output, runErr := cmd.CombinedOutput()
		timedOut := ctx.Err() == context.DeadlineExceeded
		cancel()
		if timedOut {
			return fmt.Errorf("launcher %s non termina il probe --help entro 15 secondi", exe)
		}
		if runErr != nil {
			text := strings.TrimSpace(string(output))
			if text == "" {
				text = runErr.Error()
			}
			return fmt.Errorf("launcher %s non avviabile: %s", exe, text)
		}
	}
	return nil
}

func validateRuntimePythonImports(dest string) error {
	mainPath := filepath.Join(dest, "main.py")
	pythonDLL := filepath.Join(dest, "python311.dll")
	if !exists(pythonDLL) || !exists(mainPath) {
		return fmt.Errorf("runtime Python incorporabile incompleto")
	}
	// PYTHONUTF8 affects Python I/O but does not change how Python 3 decodes a
	// source file that has no encoding declaration. The runtime template can
	// contain legacy Windows-1252 bytes (for example 0xE9 in Italian text), so
	// normalize every Python source to UTF-8 before the import probe.
	if err := normalizeRuntimePythonSourcesUTF8(dest); err != nil {
		return fmt.Errorf("normalizzazione sorgenti Python runtime: %w", err)
	}
	// The executable probe is performed by validateLauncherExecution after the
	// runtime sources are normalized. There is intentionally no pythonw.exe
	// subprocess anymore: CPython is loaded from python311.dll by the project EXE.
	return nil
}

func normalizeRuntimePythonSourcesUTF8(root string) error {
	return filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.EqualFold(filepath.Ext(path), ".py") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if utf8.Valid(data) {
			return nil
		}
		// CP1252 is the legacy encoding used by the Windows-authored runtime
		// templates. Decode the defined 0x80-0x9F characters explicitly and use
		// the byte's Unicode code point for the remaining 0xA0-0xFF range.
		var out strings.Builder
		out.Grow(len(data))
		for _, b := range data {
			if b < 0x80 {
				out.WriteByte(b)
				continue
			}
			if r, ok := cp1252Rune(b); ok {
				out.WriteRune(r)
			} else {
				out.WriteRune(rune(b))
			}
		}
		return writeBytesAtomic(path, []byte(out.String()), 0644)
	})
}

func cp1252Rune(b byte) (rune, bool) {
	switch b {
	case 0x80: return '€', true
	case 0x82: return '‚', true
	case 0x83: return 'ƒ', true
	case 0x84: return '„', true
	case 0x85: return '…', true
	case 0x86: return '†', true
	case 0x87: return '‡', true
	case 0x88: return 'ˆ', true
	case 0x89: return '‰', true
	case 0x8A: return 'Š', true
	case 0x8B: return '‹', true
	case 0x8C: return 'Œ', true
	case 0x8E: return 'Ž', true
	case 0x91: return '‘', true
	case 0x92: return '’', true
	case 0x93: return '“', true
	case 0x94: return '”', true
	case 0x95: return '•', true
	case 0x96: return '–', true
	case 0x97: return '—', true
	case 0x98: return '˜', true
	case 0x99: return '™', true
	case 0x9A: return 'š', true
	case 0x9B: return '›', true
	case 0x9C: return 'œ', true
	case 0x9E: return 'ž', true
	case 0x9F: return 'Ÿ', true
	default: return 0, false
	}
}
func validateRuntimeInstall(dest string) error {
	required := []string{
		"main.py",
		filepath.Join("game", "map_scene.py"),
		filepath.Join("game", "battle_scene.py"),
		filepath.Join("game", "data_registry.py"),
		filepath.Join("config", "options.json"),
		"python311.dll",
		filepath.Join("Lib", "site-packages", "pygame", "__init__.py"),
	}
	for _, rel := range required {
		path := filepath.Join(dest, rel)
		info, err := os.Stat(path)
		if err != nil || info.IsDir() || info.Size() == 0 {
			return fmt.Errorf("runtime convertito incompleto: %s", filepath.ToSlash(rel))
		}
	}
	manifestPath := filepath.Join(dest, "converted", "runtime_install.json")
	b, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("manifest runtime mancante: %w", err)
	}
	var manifest runtimeInstallManifest
	if err := json.Unmarshal(b, &manifest); err != nil {
		return fmt.Errorf("manifest runtime non valido: %w", err)
	}
	for _, exe := range []string{manifest.ReleaseEXE, manifest.DebugEXE} {
		if strings.TrimSpace(exe) == "" || !exists(filepath.Join(dest, exe)) {
			return fmt.Errorf("launcher progetto mancante: %s", exe)
		}
	}
	if strings.EqualFold(manifest.ReleaseEXE, manifest.DebugEXE) {
		return fmt.Errorf("launcher Release e DEBUG non possono avere lo stesso nome")
	}
	if !strings.Contains(strings.ToUpper(filepath.Base(manifest.DebugEXE)), "DEBUG") {
		return fmt.Errorf("launcher DEBUG non identificabile dal nome: %s", manifest.DebugEXE)
	}
	// Release and DEBUG are copies of the same native PML binary; game-host
	// mode is selected from runtime_install.json and the executable name.
	return nil
}
