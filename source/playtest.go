//go:build windows

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
)

var (
	playtestMu  sync.Mutex
	playtestCmd *exec.Cmd
)

func playtestBorderBlock(value any) ([4]int, bool) {
	var out [4]int

	// Canonical PLM format: [[a,b],[c,d]]. Legacy-compatible readers may also
	// encounter a flat [a,b,c,d] array or {"tiles": ...}/{"block": ...}.
	switch v := value.(type) {
	case map[string]any:
		if nested, ok := v["tiles"]; ok {
			return playtestBorderBlock(nested)
		}
		if nested, ok := v["block"]; ok {
			return playtestBorderBlock(nested)
		}
		return out, false
	case []any:
		if len(v) >= 2 {
			row0, ok0 := v[0].([]any)
			row1, ok1 := v[1].([]any)
			if ok0 && ok1 && len(row0) >= 2 && len(row1) >= 2 {
				vals := []any{row0[0], row0[1], row1[0], row1[1]}
				for i, raw := range vals {
					n, ok := playtestBorderTileID(raw)
					if !ok {
						return out, false
					}
					out[i] = n
				}
				return out, true
			}
		}
		if len(v) >= 4 {
			for i := 0; i < 4; i++ {
				n, ok := playtestBorderTileID(v[i])
				if !ok {
					return out, false
				}
				out[i] = n
			}
			return out, true
		}
	}
	return out, false
}

func playtestBorderTileID(value any) (int, bool) {
	switch v := value.(type) {
	case float64:
		n := int(v)
		return n, v == float64(n) && n >= 0
	case json.Number:
		n, err := v.Int64()
		return int(n), err == nil && n >= 0
	case int:
		return v, v >= 0
	}
	return 0, false
}

func verifySavedBorderBlock() error {
	if currentMap == nil {
		return nil
	}
	path := filepath.Join(sideDir(), "permissions", fmt.Sprintf("Map%03d.json", currentMap.ID))
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("sidecar mappa non leggibile dopo il salvataggio: %w", err)
	}

	// Deliberately decode the border payload independently of PermissionDoc and
	// editor globals. The border editor evolved from one shared 2x2 block to four
	// directional blocks; Playtest must validate the persisted format, not depend
	// on whichever UI representation happens to be compiled in this version.
	var doc map[string]any
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.UseNumber()
	if err := decoder.Decode(&doc); err != nil {
		return fmt.Errorf("sidecar mappa non valido: %w", err)
	}

	if raw, ok := doc["border_blocks"]; ok {
		blocks, ok := raw.(map[string]any)
		if !ok {
			return fmt.Errorf("border_blocks non valido: atteso oggetto con i lati top/right/bottom/left")
		}
		for _, side := range []string{"top", "right", "bottom", "left"} {
			rawBlock, present := blocks[side]
			if !present {
				// I lati mancanti sono ammessi: il runtime usa il blocco nero di
				// fallback. Non inventiamo dati solo per superare il Playtest.
				continue
			}
			if _, valid := playtestBorderBlock(rawBlock); !valid {
				return fmt.Errorf("blocco bordi %s non valido: atteso blocco 2x2 con 4 tile ID non negativi", side)
			}
		}
		return nil
	}

	// Compatibilità non distruttiva con i sidecar precedenti. Se il progetto non
	// ha alcun bordo configurato, non c'è nulla da verificare.
	for _, key := range []string{"border", "border_tiles", "border_block"} {
		if raw, ok := doc[key]; ok {
			if _, valid := playtestBorderBlock(raw); !valid {
				return fmt.Errorf("%s non valido: atteso blocco 2x2 con 4 tile ID non negativi", key)
			}
			return nil
		}
	}
	return nil
}

func resolvePlaytestPython(root string) (exePath string, prefix []string, err error) {
	candidates := []string{
		"pythonw.exe",
		"python.exe",
		filepath.Join("python", "pythonw.exe"),
		filepath.Join("python", "python.exe"),
		filepath.Join("runtime", "pythonw.exe"),
		filepath.Join("runtime", "python.exe"),
		filepath.Join(".venv", "Scripts", "pythonw.exe"),
		filepath.Join(".venv", "Scripts", "python.exe"),
		filepath.Join("venv", "Scripts", "pythonw.exe"),
		filepath.Join("venv", "Scripts", "python.exe"),
	}
	for _, rel := range candidates {
		if settings.PlaytestConsole && strings.Contains(rel, "pythonw") {
			continue
		}
		p := filepath.Join(root, rel)
		if info, statErr := os.Stat(p); statErr == nil && !info.IsDir() {
			return p, nil, nil
		}
	}
	for _, name := range []string{"pythonw", "python"} {
		if settings.PlaytestConsole && name == "pythonw" {
			continue
		}
		if p, lookErr := exec.LookPath(name); lookErr == nil {
			return p, nil, nil
		}
	}
	if p, lookErr := exec.LookPath("py"); lookErr == nil {
		return p, []string{"-3"}, nil
	}
	return "", nil, fmt.Errorf("runtime Python non trovato nel progetto né nel PATH")
}

func projectHasRegionalMaps() bool {
	if currentProject == "" {
		return false
	}
	copies, err := scanMapCopies(canonicalConvertedMapsPath())
	if err != nil {
		return false
	}
	for _, paths := range copies {
		if len(paths) == 1 && regionForMapPath(currentProject, paths[0]) != "" {
			return true
		}
	}
	return false
}

// The current converted Python runtime historically opened only
// converted/maps/MapXXX.json. During Playtest we must not recreate duplicate
// compatibility files. When regional maps exist, run Python with -S and a tiny
// bootstrap that redirects Path.is_file/read_text/open for flat MapXXX paths to
// the filesystem-derived .plm/map_index.json. This changes no project framework
// file and keeps transfers/saves based only on Map ID.
func playtestMapResolverBootstrap() string {
	return `import json, os, pathlib, re, runpy, site, sys
root = pathlib.Path(os.environ["PLM_PROJECT_ROOT"]).resolve()
for candidate in (root / "Lib" / "site-packages", root / ".venv" / "Lib" / "site-packages", root / "venv" / "Lib" / "site-packages"):
    if candidate.is_dir():
        site.addsitedir(str(candidate))
if str(root) not in sys.path:
    sys.path.insert(0, str(root))
index_path = pathlib.Path(os.environ["PLM_MAP_INDEX"])
index_doc = json.loads(index_path.read_text(encoding="utf-8"))
map_paths = index_doc.get("map_paths", {})
flat_root = (root / "converted" / "maps").resolve()
_rx = re.compile(r"(?i)^Map0*([0-9]+)\.json$")
_orig_is_file = pathlib.Path.is_file
_orig_exists = pathlib.Path.exists
_orig_read_text = pathlib.Path.read_text
_orig_open = pathlib.Path.open
def _plm_redirect(path):
    p = pathlib.Path(path)
    try:
        if p.parent.resolve() == flat_root:
            m = _rx.match(p.name)
            if m:
                rel = map_paths.get(str(int(m.group(1))))
                if rel:
                    return (root / pathlib.Path(rel)).resolve()
    except Exception:
        pass
    return p
pathlib.Path.is_file = lambda self: _orig_is_file(_plm_redirect(self))
pathlib.Path.exists = lambda self: _orig_exists(_plm_redirect(self))
pathlib.Path.read_text = lambda self, *a, **kw: _orig_read_text(_plm_redirect(self), *a, **kw)
pathlib.Path.open = lambda self, *a, **kw: _orig_open(_plm_redirect(self), *a, **kw)
main_path = root / "main.py"
sys.argv = [str(main_path)] + sys.argv[1:]
runpy.run_path(str(main_path), run_name="__main__")`
}

func startPlaytest() error {
	if currentProject == "" {
		return fmt.Errorf("apri prima un progetto")
	}
	if currentMap == nil {
		return fmt.Errorf("seleziona prima una mappa")
	}
	mainPath := filepath.Join(currentProject, "main.py")
	if _, err := os.Stat(mainPath); err != nil {
		return fmt.Errorf("main.py non trovato in %s", currentProject)
	}

	playtestMu.Lock()
	if playtestCmd != nil && playtestCmd.Process != nil && playtestCmd.ProcessState == nil {
		playtestMu.Unlock()
		return fmt.Errorf("un playtest è già in esecuzione")
	}
	playtestMu.Unlock()

	if settings.PlaytestConfirm && msgboxResult("Playtest", "Salvare i dati e avviare il gioco?", MB_YESNO|MB_ICONINFORMATION) != IDYES {
		return fmt.Errorf("avvio annullato")
	}
	// Il playtest è consentito solo con un filesystem mappa non ambiguo.
	// Copie divergenti dello stesso Map ID devono essere risolte prima.
	if err := ensureProjectMapFilesystemSafe("Playtest"); err != nil {
		return err
	}
	// IMPORTANT: save first. Runtime validation must inspect the exact event data
	// that the user is about to test, not the previous on-disk revision.
	// Il playtest deve vedere esattamente lo stato appena editato: prima salva
	// mappa reale e sidecar editor, poi verifica il blocco bordi su disco.
	if !saveCurrentMap() {
		return fmt.Errorf("salvataggio della mappa annullato o non riuscito")
	}
	savePermissions()
	if err := verifySavedPermissionData(); err != nil {
		return fmt.Errorf("verifica Movimenti/Terrain Tags nella mappa reale: %w", err)
	}
	if err := saveEvents(); err != nil {
		return fmt.Errorf("salvataggio eventi: %w", err)
	}
	if err := saveEncounterData(); err != nil {
		return fmt.Errorf("salvataggio incontri selvatici: %w", err)
	}
	if err := verifySavedBorderBlock(); err != nil {
		return err
	}

	// Install/refresh PLM runtime bridges only after map/events were saved.
	// This keeps the project runtime and the just-compiled event data in sync.
	if err := ensureGlobalEventsRuntimeBridge(currentProject); err != nil {
		return fmt.Errorf("runtime Eventi globali: %w", err)
	}
	if err := ensureEventToolsRuntimeBridge(currentProject); err != nil {
		return fmt.Errorf("runtime strumenti Evento: %w", err)
	}
	// Coverage validation is a diagnostic guard, not a launch-kill switch.
	// Older imported maps may contain legacy markers that are harmless to the
	// Python interpreter. Blocking the whole Playtest made the Play button unusable.
	// We still report the exact problem in diagnostics/status so it can be fixed.
	if err := validateProjectEventRuntimeCoverage(currentProject); err != nil {
		diagLogf("[PLAYTEST][RUNTIME WARNING] %v", err)
		setText(hwndStatus, "Playtest: avviso runtime eventi (vedi log), avvio comunque...")
	}

	manifestData, err := os.ReadFile(filepath.Join(currentProject, "converted", "runtime_install.json"))
	if err != nil {
		return fmt.Errorf("manifest runtime non trovato: %w", err)
	}
	var runtimeManifest runtimeInstallManifest
	if err := json.Unmarshal(manifestData, &runtimeManifest); err != nil {
		return fmt.Errorf("manifest runtime non valido: %w", err)
	}
	exePath := filepath.Join(currentProject, runtimeManifest.DebugEXE)
	if runtimeManifest.DebugEXE == "" || !exists(exePath) {
		return fmt.Errorf("EXE DEBUG del progetto non trovato: %s", runtimeManifest.DebugEXE)
	}
	args := playtestPreferenceArgs(settings, currentMap.ID)
	cmd := exec.Command(exePath, args...)
	cmd.Dir = currentProject
	mapIndexPath := filepath.Join(currentProject, ".plm", "map_index.json")
	cmd.Env = append(os.Environ(),
		"PYTHONUTF8=1",
		"PLM_PROJECT_ROOT="+currentProject,
		"PLM_MAP_INDEX="+mapIndexPath,
		"PLM_MAP_REGISTRY="+filepath.Join(currentProject, "plm_regions.py"),
	)
	// Evita il terminale nero di python.exe; la finestra Pygame del gioco resta
	// una normale top-level window visibile.
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: !settings.PlaytestConsole}

	logDir := filepath.Join(currentProject, ".plm")
	if settings.PlaytestLogsPath != "" {
		logDir = filepath.Join(settings.PlaytestLogsPath, filepath.Base(currentProject))
	}
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return err
	}
	logPath := filepath.Join(logDir, "playtest.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("impossibile creare il log playtest: %w", err)
	}
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return fmt.Errorf("avvio playtest fallito: %w", err)
	}
	playtestMu.Lock()
	playtestCmd = cmd
	playtestMu.Unlock()

	mapID := currentMap.ID
	diagLogf("[PLAYTEST] START map=%03d exe=%s args=%s", mapID, exePath, strings.Join(args, " "))
	go func(c *exec.Cmd, f *os.File, id int) {
		err := c.Wait()
		_ = f.Close()
		playtestMu.Lock()
		if playtestCmd == c {
			playtestCmd = nil
		}
		playtestMu.Unlock()
		if err != nil {
			diagLogf("[PLAYTEST] END map=%03d error=%v log=%s", id, err, logPath)
		} else {
			diagLogf("[PLAYTEST] END map=%03d ok log=%s", id, logPath)
		}
	}(cmd, logFile, mapID)
	return nil
}

func stopPlaytest() error {
	playtestMu.Lock()
	cmd := playtestCmd
	playtestMu.Unlock()
	if cmd == nil || cmd.Process == nil || cmd.ProcessState != nil {
		return fmt.Errorf("nessun playtest in esecuzione")
	}
	if err := cmd.Process.Kill(); err != nil {
		return fmt.Errorf("impossibile arrestare il playtest: %w", err)
	}
	return nil
}

func playtestPreferenceArgs(s Settings, mapID int) []string {
	var args []string
	if s.PlaytestDebug {
		args = append(args, "--debug")
	}
	if s.PlaytestCurrentMap {
		args = append(args, "--map", fmt.Sprint(mapID))
	}
	return args
}
