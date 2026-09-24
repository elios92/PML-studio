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
	"regexp"
	"strings"
	"syscall"
	"time"
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

func installRuntimeCore(dest, projectName string) (releaseExe, debugExe string, err error) {
	if len(plmRuntimeCoreZip) == 0 {
		return "", "", fmt.Errorf("template runtime PLM non incorporato")
	}
	zr, err := zip.NewReader(bytes.NewReader(plmRuntimeCoreZip), int64(len(plmRuntimeCoreZip)))
	if err != nil {
		return "", "", fmt.Errorf("template runtime PLM non leggibile: %w", err)
	}

	launcherBytes, err := readEmbeddedRuntimeFile(plmGameLauncherArchivePath)
	if err != nil {
		return "", "", fmt.Errorf("launcher PLM non disponibile: %w", err)
	}
	for _, entry := range zr.File {
		archiveName := filepath.ToSlash(entry.Name)
		if strings.HasPrefix(archiveName, "_plm_templates/") {
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
		mode := os.FileMode(0644)
		if strings.EqualFold(filepath.Ext(target), ".exe") {
			mode = 0755
		}
		if err := writeBytesAtomic(target, data, mode); err != nil {
			return "", "", err
		}
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
	from, err := syscall.UTF16PtrFromString(tmp)
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	to, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	const moveFileReplaceExisting = 0x1
	const moveFileWriteThrough = 0x8
	if err := syscall.MoveFileEx(from, to, moveFileReplaceExisting|moveFileWriteThrough); err != nil {
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
	// Il launcher incorporato espone PLM_TEST_MODE per un probe senza UI.
	// Eseguiamo entrambi i file, così una conversione non viene dichiarata valida
	// se uno dei due EXE non è realmente avviabile su Windows.
	for _, exe := range []string{manifest.ReleaseEXE, manifest.DebugEXE} {
		cmd := exec.Command(filepath.Join(dest, exe))
		cmd.Dir = dest
		cmd.Env = append(os.Environ(), "PLM_TEST_MODE=1", "PYTHONUTF8=1")
		output, err := cmd.CombinedOutput()
		if err != nil {
			text := strings.TrimSpace(string(output))
			if text == "" {
				text = err.Error()
			}
			return fmt.Errorf("launcher %s non avviabile: %s", exe, text)
		}
	}
	return nil
}

func validateRuntimePythonImports(dest string) error {
	python := filepath.Join(dest, "pythonw.exe")
	mainPath := filepath.Join(dest, "main.py")
	if !exists(python) || !exists(mainPath) {
		return fmt.Errorf("runtime Python incompleto")
	}
	cmd := exec.Command(python, mainPath, "--help")
	cmd.Dir = dest
	cmd.Env = append(os.Environ(), "PYTHONUTF8=1")
	output, err := cmd.CombinedOutput()
	if err != nil {
		text := strings.TrimSpace(string(output))
		if text == "" {
			text = err.Error()
		}
		return fmt.Errorf("runtime Python non importabile: %s", text)
	}
	return nil
}
func validateRuntimeInstall(dest string) error {
	required := []string{
		"main.py",
		filepath.Join("game", "map_scene.py"),
		filepath.Join("game", "battle_scene.py"),
		filepath.Join("game", "data_registry.py"),
		"pythonw.exe",
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
	// Il launcher incorporato decide la modalità dal proprio nome file e contiene
	// il percorso esplicito --debug/PLM_DEBUG=1. Verifichiamo il contratto prima
	// di accettare una conversione come eseguibile: evita due EXE solo nominali.
	launcherTemplate, err := readEmbeddedRuntimeFile(plmGameLauncherArchivePath)
	if err != nil {
		return err
	}
	if !bytes.Contains(launcherTemplate, []byte("--debug")) || !bytes.Contains(launcherTemplate, []byte("PLM_DEBUG=1")) {
		return fmt.Errorf("launcher runtime privo del contratto DEBUG")
	}
	return nil
}
