//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const plmEventToolsRuntimePythonArchivePath = "_plm_templates/plm_event_tools_runtime.py"

var (
	plmEventToolsRuntimePython        []byte
	plmEventToolsRuntimePythonLoadErr error
)

func init() {
	plmEventToolsRuntimePython, plmEventToolsRuntimePythonLoadErr = readEmbeddedRuntimeFile(plmEventToolsRuntimePythonArchivePath)
}

const (
	plmEventToolsHookBegin = "# <PLM_EVENT_TOOLS_RUNTIME>"
	plmEventToolsHookEnd   = "# </PLM_EVENT_TOOLS_RUNTIME>"
)

func eventToolsRuntimeHook() string {
	return `
# <PLM_EVENT_TOOLS_RUNTIME>
# Installed by PLM Studio. Deterministic movement routes, screen pictures and
# essential party commands created by the visual Event Editor.
try:
    try:
        from .plm_event_tools_runtime import install_namespace as _plm_install_event_tools
    except ImportError:
        from game.plm_event_tools_runtime import install_namespace as _plm_install_event_tools
    _plm_install_event_tools(globals())
except Exception as _plm_event_tools_error:
    print("[PLM][EVENT TOOLS] runtime bridge disabled:", _plm_event_tools_error)
# </PLM_EVENT_TOOLS_RUNTIME>
`
}

func ensureEventToolsRuntimeBridge(projectRoot string) error {
	root := strings.TrimSpace(projectRoot)
	if root == "" {
		return fmt.Errorf("progetto non aperto")
	}
	if plmEventToolsRuntimePythonLoadErr != nil {
		return fmt.Errorf("runtime Python plm_event_tools_runtime.py non disponibile: %w", plmEventToolsRuntimePythonLoadErr)
	}
	gameDir := filepath.Join(root, "game")
	mapScenePath := filepath.Join(gameDir, "map_scene.py")
	if info, err := os.Stat(mapScenePath); err != nil || info.IsDir() {
		return fmt.Errorf("runtime Python: game/map_scene.py non trovato")
	}
	if err := os.MkdirAll(gameDir, 0755); err != nil {
		return err
	}

	runtimePath := filepath.Join(gameDir, "plm_event_tools_runtime.py")
	if current, err := os.ReadFile(runtimePath); err != nil || string(current) != string(plmEventToolsRuntimePython) {
		tmp := runtimePath + ".tmp"
		if err := os.WriteFile(tmp, plmEventToolsRuntimePython, 0644); err != nil {
			return fmt.Errorf("scrittura runtime strumenti evento: %w", err)
		}
		if err := replaceFileAtomicWindows(tmp, runtimePath); err != nil {
			_ = os.Remove(tmp)
			return fmt.Errorf("installazione runtime strumenti evento: %w", err)
		}
	}

	source, err := os.ReadFile(mapScenePath)
	if err != nil {
		return err
	}
	text := string(source)
	if strings.Contains(text, plmEventToolsHookBegin) {
		start := strings.Index(text, plmEventToolsHookBegin)
		endRel := strings.Index(text[start:], plmEventToolsHookEnd)
		if endRel >= 0 {
			end := start + endRel + len(plmEventToolsHookEnd)
			text = strings.TrimRight(text[:start], "\r\n") + "\n" + strings.TrimSpace(eventToolsRuntimeHook()) + "\n" + strings.TrimLeft(text[end:], "\r\n")
		}
	} else {
		backupDir := filepath.Join(root, ".plm", "backups")
		if err := os.MkdirAll(backupDir, 0755); err == nil {
			backupPath := filepath.Join(backupDir, "map_scene.py.before_plm_event_tools")
			if _, statErr := os.Stat(backupPath); os.IsNotExist(statErr) {
				_ = os.WriteFile(backupPath, source, 0644)
			}
		}
		text = strings.TrimRight(text, "\r\n") + "\n\n" + strings.TrimSpace(eventToolsRuntimeHook()) + "\n"
	}
	if text != string(source) {
		tmp := mapScenePath + ".plm.tools.tmp"
		if err := os.WriteFile(tmp, []byte(text), 0644); err != nil {
			return fmt.Errorf("aggiornamento game/map_scene.py: %w", err)
		}
		if err := replaceFileAtomicWindows(tmp, mapScenePath); err != nil {
			_ = os.Remove(tmp)
			return fmt.Errorf("aggiornamento game/map_scene.py: %w", err)
		}
	}
	return nil
}
