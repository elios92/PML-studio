//go:build windows

package main

import (
	"archive/zip"
	"crypto/sha256"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func normalizeEditorPreferences(s *Settings) {
	if s.PreferencesVersion < 1 {
		s.PreferencesVersion = 1
		s.Language, s.Startup, s.Theme, s.DefaultTool = "it", "empty", "light", "pencil"
		s.UIScale = 100
		s.PlaytestDebug, s.PlaytestCurrentMap = true, true
	}
	// Other languages need a complete translation catalogue before activation.
	s.Language = "it"
	if s.Startup != "last" && s.Startup != "open" {
		s.Startup = "empty"
	}
	if s.Theme != "dark" && s.Theme != "system" {
		s.Theme = "light"
	}
	if s.UIScale != 100 && s.UIScale != 110 && s.UIScale != 125 {
		s.UIScale = 100
	}
	if s.AutosaveMinutes != 1 && s.AutosaveMinutes != 5 && s.AutosaveMinutes != 10 {
		s.AutosaveMinutes = 0
	}
	if s.DefaultTool != "select" && s.DefaultTool != "rectangle" {
		s.DefaultTool = "pencil"
	}
}

func persistEditorPreferences(s Settings) error {
	normalizeEditorPreferences(&s)
	if err := validateExistingPreferences(filepath.Join(appDataDir(), "settings.json")); err != nil {
		return err
	}
	if err := os.MkdirAll(appDataDir(), 0755); err != nil {
		return err
	}
	return writeJSONAtomic(filepath.Join(appDataDir(), "settings.json"), s)
}

func changeEditorPreferences(change func(*Settings)) {
	previous := settings
	next := settings
	change(&next)
	normalizeEditorPreferences(&next)
	if err := persistEditorPreferences(next); err != nil {
		msgbox("Impostazioni", "Modifica non salvata: "+err.Error(), MB_OK|MB_ICONERROR)
		return
	}
	settings = next
	gridEnabled = settings.GridDefault
	if gridEnabled {
		setText(hwndGridToggle, "Griglia ON")
	} else {
		setText(hwndGridToggle, "Griglia OFF")
	}
	if previous.DefaultTool != next.DefaultTool {
		applyDefaultEditorTool()
	}
	if previous.Theme != next.Theme || previous.UISkin != next.UISkin || previous.UIFontSize != next.UIFontSize || previous.UIScale != next.UIScale || previous.AccentColor != next.AccentColor || previous.SelectionColor != next.SelectionColor || previous.SecondaryColor != next.SecondaryColor || previous.PanelColor != next.PanelColor || previous.BorderColor != next.BorderColor {
		applyLiveEditorTheme()
	}
	configureEditorAutosave()
	createMainMenu(hwndMain)
	updateMapToolButtonStates()
	invalidate(hwndCanvas)
}

func applyDefaultEditorTool() {
	switch settings.DefaultTool {
	case "select":
		activeMapTool = ToolSelect
	case "rectangle":
		activeMapTool = ToolRectangle
	default:
		activeMapTool = ToolPencil
	}
}

// Archive editable source/data only: assets and generated builds are not copies
// of user edits. Never follow reparse points or include an archive recursively.
func backupProjectData(root, destination string) (result string, err error) {
	root, err = filepath.Abs(root)
	if err != nil {
		return "", err
	}
	if !isDir(root) {
		return "", fmt.Errorf("progetto non valido")
	}
	if destination == "" {
		destination = filepath.Join(appDataDir(), "Backups")
	}
	key := sha256.Sum256([]byte(strings.ToLower(root)))
	destination = filepath.Join(destination, fmt.Sprintf("%s-%x", filepath.Base(root), key[:6]))
	destination, err = filepath.Abs(destination)
	if err != nil {
		return "", err
	}
	if err = os.MkdirAll(destination, 0755); err != nil {
		return "", err
	}
	f, err := os.CreateTemp(destination, "backup-*.tmp")
	if err != nil {
		return "", err
	}
	tmp := f.Name()
	defer func() {
		f.Close()
		if err != nil {
			os.Remove(tmp)
		}
	}()
	z := zip.NewWriter(f)
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		if strings.EqualFold(path, destination) {
			return filepath.SkipDir
		}
		rel, e := filepath.Rel(root, path)
		if e != nil {
			return e
		}
		if d.Type()&os.ModeSymlink != 0 {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			name := strings.ToLower(d.Name())
			if name == ".git" || name == ".venv" || name == "venv" || name == "__pycache__" || name == "backups" || name == "build" || name == "dist" || name == "graphics" || name == "audio" || name == "python" || name == "runtime" {
				return filepath.SkipDir
			}
			return nil
		}
		switch strings.ToLower(filepath.Ext(path)) {
		case ".json", ".txt", ".py", ".rxdata":
		default:
			return nil
		}
		in, e := os.Open(path)
		if e != nil {
			return e
		}
		defer in.Close()
		out, e := z.Create(filepath.ToSlash(rel))
		if e != nil {
			return e
		}
		_, e = io.Copy(out, in)
		return e
	})
	if err != nil {
		z.Close()
		return "", err
	}
	if err = z.Close(); err != nil {
		return "", err
	}
	if err = f.Sync(); err != nil {
		return "", err
	}
	if err = f.Close(); err != nil {
		return "", err
	}
	result = filepath.Join(destination, "PML-data-"+time.Now().Format("20060102-150405.000000000")+".zip")
	err = os.Rename(tmp, result)
	return result, err
}

const editorAutosaveTimer = 31001

func configureEditorAutosave() {
	if hwndMain == 0 {
		return
	}
	user32.NewProc("KillTimer").Call(uintptr(hwndMain), editorAutosaveTimer)
	if settings.AutosaveMinutes > 0 {
		user32.NewProc("SetTimer").Call(uintptr(hwndMain), editorAutosaveTimer, uintptr(settings.AutosaveMinutes*60000), 0)
	}
}

func runEditorAutosave() {
	if currentProject == "" || mapLoading.Load() || hwndUISettings != 0 || !hasUnsavedProjectChanges() {
		return
	}
	capture, _, _ := user32.NewProc("GetCapture").Call()
	enabled, _, _ := user32.NewProc("IsWindowEnabled").Call(uintptr(hwndMain))
	active, _, _ := user32.NewProc("GetActiveWindow").Call()
	// Do not interrupt a drag, modal dialog, or a partially typed data value.
	if capture != 0 || enabled == 0 || active != uintptr(hwndMain) || dataValueEdited || dataRawEdited || dataSettingsRawEdited || dataSettingsStandardEdited {
		return
	}
	if saveAllProjectChanges() {
		setToolbarStatus("Salvataggio automatico completato.")
	}
}
