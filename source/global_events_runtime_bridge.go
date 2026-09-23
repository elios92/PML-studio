//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const plmGlobalEventsRuntimePythonArchivePath = "_plm_templates/plm_global_events_runtime.py"

var (
	plmGlobalEventsRuntimePython        []byte
	plmGlobalEventsRuntimePythonLoadErr error
)

func init() {
	plmGlobalEventsRuntimePython, plmGlobalEventsRuntimePythonLoadErr = readEmbeddedRuntimeFile(plmGlobalEventsRuntimePythonArchivePath)
}

const (
	plmGlobalRuntimeHookBegin = "# <PLM_GLOBAL_EVENTS_RUNTIME>"
	plmGlobalRuntimeHookEnd   = "# </PLM_GLOBAL_EVENTS_RUNTIME>"
)

func globalEventsRuntimeHook() string {
	return `
# <PLM_GLOBAL_EVENTS_RUNTIME>
# Installed by PLM Studio. Keeps the existing MapScene event interpreter and
# adds only the PML global-event commands created by the visual event editor.
try:
    try:
        from .plm_global_events_runtime import install_namespace as _plm_install_global_events
    except ImportError:
        from game.plm_global_events_runtime import install_namespace as _plm_install_global_events
    _plm_install_global_events(globals())
except Exception as _plm_global_events_error:
    print("[PLM][GLOBAL EVENTS] runtime bridge disabled:", _plm_global_events_error)
# </PLM_GLOBAL_EVENTS_RUNTIME>
`
}

// ensureGlobalEventsRuntimeBridge installs the Python runtime support into the
// opened PLM project. It is deliberately additive: game/map_scene.py keeps its
// existing event engine and receives one idempotent hook at EOF.
func ensureGlobalEventsRuntimeBridge(projectRoot string) error {
	root := strings.TrimSpace(projectRoot)
	if root == "" {
		return fmt.Errorf("progetto non aperto")
	}
	if plmGlobalEventsRuntimePythonLoadErr != nil {
		return fmt.Errorf("runtime Python plm_global_events_runtime.py non disponibile: %w", plmGlobalEventsRuntimePythonLoadErr)
	}
	gameDir := filepath.Join(root, "game")
	mapScenePath := filepath.Join(gameDir, "map_scene.py")
	if info, err := os.Stat(mapScenePath); err != nil || info.IsDir() {
		return fmt.Errorf("runtime Python: game/map_scene.py non trovato")
	}
	if err := os.MkdirAll(gameDir, 0755); err != nil {
		return err
	}

	runtimePath := filepath.Join(gameDir, "plm_global_events_runtime.py")
	if current, err := os.ReadFile(runtimePath); err != nil || string(current) != string(plmGlobalEventsRuntimePython) {
		tmp := runtimePath + ".tmp"
		if err := os.WriteFile(tmp, plmGlobalEventsRuntimePython, 0644); err != nil {
			return fmt.Errorf("scrittura runtime eventi globali: %w", err)
		}
		if err := os.Rename(tmp, runtimePath); err != nil {
			_ = os.Remove(tmp)
			return fmt.Errorf("installazione runtime eventi globali: %w", err)
		}
	}

	source, err := os.ReadFile(mapScenePath)
	if err != nil {
		return err
	}
	text := string(source)
	if strings.Contains(text, plmGlobalRuntimeHookBegin) {
		// Upgrade an older PLM-managed hook in place without duplicating it.
		start := strings.Index(text, plmGlobalRuntimeHookBegin)
		endRel := strings.Index(text[start:], plmGlobalRuntimeHookEnd)
		if endRel >= 0 {
			end := start + endRel + len(plmGlobalRuntimeHookEnd)
			text = strings.TrimRight(text[:start], "\r\n") + "\n" + strings.TrimSpace(globalEventsRuntimeHook()) + "\n" + strings.TrimLeft(text[end:], "\r\n")
		}
	} else {
		// Keep a one-time safety copy outside game/ so it never becomes a runtime
		// import candidate and doesn't clutter the project package.
		backupDir := filepath.Join(root, ".plm", "backups")
		if err := os.MkdirAll(backupDir, 0755); err == nil {
			backupPath := filepath.Join(backupDir, "map_scene.py.before_plm_global_events")
			if _, statErr := os.Stat(backupPath); os.IsNotExist(statErr) {
				_ = os.WriteFile(backupPath, source, 0644)
			}
		}
		text = strings.TrimRight(text, "\r\n") + "\n\n" + strings.TrimSpace(globalEventsRuntimeHook()) + "\n"
	}

	if text != string(source) {
		tmp := mapScenePath + ".plm.tmp"
		if err := os.WriteFile(tmp, []byte(text), 0644); err != nil {
			return fmt.Errorf("aggiornamento game/map_scene.py: %w", err)
		}
		if err := os.Rename(tmp, mapScenePath); err != nil {
			_ = os.Remove(tmp)
			return fmt.Errorf("aggiornamento game/map_scene.py: %w", err)
		}
	}
	return nil
}
