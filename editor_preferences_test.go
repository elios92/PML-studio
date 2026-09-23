//go:build windows

package main

import (
	"archive/zip"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"syscall"
	"testing"
	"unsafe"
)

func TestPreferenceWindowsUI(t *testing.T) {
	if os.Getenv("PML_UI_TEST_CHILD") != "1" {
		exe, e := os.Executable()
		if e != nil {
			t.Fatal(e)
		}
		cmd := exec.Command(exe, "-test.run=^TestPreferenceWindowsUI$", "-test.v")
		cmd.Env = append(os.Environ(), "PML_UI_TEST_CHILD=1", "PML_SETTINGS_DIR="+t.TempDir())
		if b, e := cmd.CombinedOutput(); e != nil {
			t.Fatalf("native UI test: %v\n%s", e, b)
		} else {
			t.Log(string(b))
		}
		return
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	settings = Settings{}
	normalizeUISettings()
	initModernThemeResources()
	defer releaseModernThemeResources()
	hi, _, _ := pGetModuleHandleW.Call(0)
	class := wstr("PMLPreferencesIntegrationTest")
	wc := WNDCLASSEX{cbSize: uint32(unsafe.Sizeof(WNDCLASSEX{})), lpfnWndProc: syscall.NewCallback(wndProc), hInstance: syscall.Handle(hi), lpszClassName: class, hbrBackground: themeBrushWindow}
	if r, _, e := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		t.Fatal(e)
	}
	hwndMain = createWindow("PMLPreferencesIntegrationTest", "Preferences test", WS_OVERLAPPEDWINDOW, 0, 0, 1500, 860, 0, 0, syscall.Handle(hi))
	if hwndMain == 0 {
		t.Fatal("native window creation failed")
	}
	defer pDestroyWindow.Call(uintptr(hwndMain))
	registerUISettingsWindowClass(syscall.Handle(hi), 0)
	createMainControls(syscall.Handle(hi))
	layout(hwndMain)
	menu, _, _ := user32.NewProc("GetMenu").Call(uintptr(hwndMain))
	sub, _, _ := user32.NewProc("GetSubMenu").Call(menu, 1)
	count, _, _ := user32.NewProc("GetMenuItemCount").Call(sub)
	if count != 8 {
		t.Fatalf("expected 8 settings sections, got %d", count)
	}
	for i, want := range []string{"Generale", "Interfaccia", "Editor", "Playtest", "Percorsi", "Modalità PML Studio", "Plugin", "Aggiornamenti"} {
		var buf [100]uint16
		user32.NewProc("GetMenuStringW").Call(sub, uintptr(i), uintptr(unsafe.Pointer(&buf[0])), 100, 0x400)
		if got := syscall.UTF16ToString(buf[:]); got != want {
			t.Fatalf("menu %d: %q", i, got)
		}
	}
	var findCommand func(uintptr, string) uintptr
	findCommand = func(menu uintptr, label string) uintptr {
		n, _, _ := user32.NewProc("GetMenuItemCount").Call(menu)
		for i := uintptr(0); i < n; i++ {
			var b [260]uint16
			user32.NewProc("GetMenuStringW").Call(menu, i, uintptr(unsafe.Pointer(&b[0])), 260, 0x400)
			if syscall.UTF16ToString(b[:]) == label {
				id, _, _ := user32.NewProc("GetMenuItemID").Call(menu, i)
				return id
			}
			child, _, _ := user32.NewProc("GetSubMenu").Call(menu, i)
			if child != 0 {
				if id := findCommand(child, label); id != 0 {
					return id
				}
			}
		}
		return 0
	}
	clickMenu := func(label string) {
		t.Helper()
		m, _, _ := user32.NewProc("GetMenu").Call(uintptr(hwndMain))
		id := findCommand(m, label)
		if id == 0 {
			t.Fatalf("missing menu command %q", label)
		}
		pSendMessageW.Call(uintptr(hwndMain), WM_COMMAND, id, 0)
	}
	clickMenu("Riapri ultimo progetto")
	if settings.Startup != "last" {
		t.Fatal("startup menu disconnected")
	}
	clickMenu("Rettangolo")
	if activeMapTool != ToolRectangle {
		t.Fatal("tool menu disconnected")
	}
	clickMenu("Griglia")
	if settings.GridDefault || gridEnabled {
		t.Fatal("grid menu disconnected")
	}
	clickMenu("Ogni 5 minuti")
	if settings.AutosaveMinutes != 5 {
		t.Fatal("autosave menu disconnected")
	}
	clickMenu("Modalità debug")
	if settings.PlaytestDebug {
		t.Fatal("debug menu disconnected")
	}
	clickMenu("Mostra console Python")
	if !settings.PlaytestConsole {
		t.Fatal("console menu disconnected")
	}
	clickMenu("Scuro")
	if settings.Theme != "dark" {
		t.Fatal("theme menu disconnected")
	}
	clickMenu("125%")
	clickMenu("Grigio professionale")
	if settings.UIScale != 125 {
		t.Fatal("skin changed UI geometry")
	}
	// Real command callbacks update controls immediately and survive JSON reload.
	for _, scale := range []int{100, 110, 125} {
		changeEditorPreferences(func(s *Settings) { s.UIScale = scale; s.Theme = "dark"; s.AccentColor = "#8355C5" })
		var rect RECT
		pGetWindowRect.Call(uintptr(hwndToolPencil), uintptr(unsafe.Pointer(&rect)))
		if got := rect.Right - rect.Left; got != int32(40*scale/100) {
			t.Fatalf("toolbar scale %d: width %d", scale, got)
		}
	}
	changeEditorPreferences(resetAppearance)
	before := settings
	showUISettingsDialog()
	if hwndUISettings == 0 {
		t.Fatal("skin dialog failed")
	}
	setText(skinColorEdits[0], "#112233")
	if settings.AccentColor != "#112233" || themeAccentR != 0x11 {
		t.Fatal("preview did not update palette")
	}
	pSendMessageW.Call(uintptr(hwndUISettings), WM_COMMAND, 2672, 0)
	if !reflect.DeepEqual(settings, before) {
		t.Fatal("cancel failed to restore appearance")
	}
	showUISettingsDialog()
	setText(skinColorEdits[0], "#334455")
	pSendMessageW.Call(uintptr(hwndUISettings), WM_COMMAND, 2671, 0)
	if hwndUISettings != 0 {
		t.Fatal("save did not close dialog")
	}
	loadSettings()
	if settings.AccentColor != "#334455" {
		t.Fatal("skin did not persist")
	}
	showUISettingsDialog()
	setText(skinColorEdits[0], "#invalid")
	pSendMessageW.Call(uintptr(hwndUISettings), WM_COMMAND, 2671, 0)
	if hwndUISettings == 0 {
		t.Fatal("invalid color accepted")
	}
	pSendMessageW.Call(uintptr(hwndUISettings), WM_COMMAND, 2672, 0)
	for i := 0; i < 30; i++ {
		settings.Theme = "dark"
		applyLiveEditorTheme()
		settings.Theme = "light"
		applyLiveEditorTheme()
	}
	t.Log("8 menus; native controls 100/110/125%; live colors; cancel/save; invalid input; 60 theme changes PASS")
}

func TestModeChangePreservesManifest(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "plm_project.json")
	initial := `{"format_version":1,"Name":"Test","custom_plugin":{"data":[1,2,3]},"mega_evolutions_enabled":true}`
	if e := os.WriteFile(p, []byte(initial), 0600); e != nil {
		t.Fatal(e)
	}
	if e := saveProjectEditorMode(root, projectModeAdvanced); e != nil {
		t.Fatal(e)
	}
	b, _ := os.ReadFile(p)
	var raw map[string]json.RawMessage
	if e := json.Unmarshal(b, &raw); e != nil {
		t.Fatal(e)
	}
	if string(raw["custom_plugin"]) == "" || string(raw["mega_evolutions_enabled"]) != "true" {
		t.Fatal("mode change lost manifest fields")
	}
	if projectEditorModeFromManifest(root) != projectModeAdvanced {
		t.Fatal("mode not persisted")
	}
	if e := os.WriteFile(p, []byte("broken"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := saveProjectEditorMode(root, projectModeStandard); e == nil {
		t.Fatal("invalid manifest silently replaced")
	}
	b, _ = os.ReadFile(p)
	if string(b) != "broken" {
		t.Fatal("invalid manifest was modified")
	}
}

func TestEditorPreferencesMigrationAndPersistence(t *testing.T) {
	old := settings
	t.Cleanup(func() { settings = old })
	t.Setenv("PML_SETTINGS_DIR", t.TempDir())
	settings = Settings{UISettingsVersion: 1, UISkin: "windows10_gray", UIFontSize: 18, GridDefault: false}
	normalizeUISettings()
	if settings.GridDefault || settings.UISkin != "windows10_gray" || settings.UIFontSize != 18 || !settings.PlaytestDebug || !settings.PlaytestCurrentMap {
		t.Fatal("migration changed existing settings or launch defaults")
	}
	settings.Theme = "dark"
	settings.UIScale = 125
	settings.AutosaveMinutes = 5
	settings.BackupBeforeSave = true
	settings.Startup = "last"
	settings.PlaytestDebug = false
	settings.ProjectsPath = filepath.Join(t.TempDir(), "progetti con spazi")
	settings.AccentColor = "#8355C5"
	if e := persistEditorPreferences(settings); e != nil {
		t.Fatal(e)
	}
	want := settings
	settings = Settings{}
	loadSettings()
	if !reflect.DeepEqual(settings, want) {
		t.Fatalf("settings lost on reopen: %+v", settings)
	}
	settings.Startup = "bad"
	settings.UIScale = 999
	settings.AutosaveMinutes = -1
	settings.Theme = "bad"
	normalizeUISettings()
	if settings.Startup != "empty" || settings.UIScale != 100 || settings.AutosaveMinutes != 0 || settings.Theme != "light" {
		t.Fatal("invalid settings not normalized")
	}
}

func TestEditorPreferencesAtomicFailure(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PML_SETTINGS_DIR", dir)
	s := Settings{}
	normalizeEditorPreferences(&s)
	if e := persistEditorPreferences(s); e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(dir, "settings.json")
	before, _ := os.ReadFile(path)
	if e := os.Mkdir(path+".tmp", 0700); e != nil {
		t.Fatal(e)
	}
	s.Theme = "dark"
	if e := persistEditorPreferences(s); e == nil {
		t.Fatal("expected write failure")
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("failed save damaged prior settings")
	}
}

func TestEditorBackupDataAndIsolation(t *testing.T) {
	root := t.TempDir()
	destination := t.TempDir()
	for _, name := range []string{"plm_project.json", "converted/maps/Map001.json", "PBS/pokemon.txt", "game/main.py", "Graphics/excluded.txt", ".git/excluded.txt"} {
		path := filepath.Join(root, name)
		if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
			t.Fatal(e)
		}
		if e := os.WriteFile(path, []byte(name), 0600); e != nil {
			t.Fatal(e)
		}
	}
	path, e := backupProjectData(root, destination)
	if e != nil {
		t.Fatal(e)
	}
	z, e := zip.OpenReader(path)
	if e != nil {
		t.Fatal(e)
	}
	defer z.Close()
	if len(z.File) != 4 {
		t.Fatalf("wrong archive inventory: %v", z.File)
	}
	for _, f := range z.File {
		if f.Name == "Graphics/excluded.txt" || f.Name == ".git/excluded.txt" {
			t.Fatal("excluded content in backup")
		}
	}
	second, e := backupProjectData(root, destination)
	if e != nil {
		t.Fatal(e)
	}
	if path == second {
		t.Fatal("backup overwritten")
	}
	if _, e := backupProjectData(filepath.Join(root, "absent"), destination); e == nil {
		t.Fatal("missing root accepted")
	}
}

func TestPlaytestPreferencesArguments(t *testing.T) {
	for _, tc := range []struct {
		s    Settings
		want []string
	}{
		{Settings{}, nil},
		{Settings{PlaytestDebug: true}, []string{"--debug"}},
		{Settings{PlaytestCurrentMap: true}, []string{"--map", "42"}},
		{Settings{PlaytestDebug: true, PlaytestCurrentMap: true}, []string{"--debug", "--map", "42"}},
	} {
		if got := playtestPreferenceArgs(tc.s, 42); !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("got %v want %v", got, tc.want)
		}
	}
}

func TestThemeColorsAndReset(t *testing.T) {
	old := settings
	t.Cleanup(func() { settings = old; selectThemePalette() })
	for _, v := range []string{"#AABBCC", "aabbcc", "#000000"} {
		if _, _, _, e := parseThemeColor(v); e != nil {
			t.Fatal(e)
		}
	}
	for _, v := range []string{"#ABC", "blue", "#GG0000", "#00000000"} {
		if _, _, _, e := parseThemeColor(v); e == nil {
			t.Fatalf("invalid color %q accepted", v)
		}
	}
	settings = Settings{Theme: "dark", UISkin: "dark", AccentColor: "#123456"}
	selectThemePalette()
	if themeAccentR != 0x12 || themeAccentG != 0x34 || themeAccentB != 0x56 || themePanelR > 60 {
		t.Fatal("dark/accent palette not applied")
	}
	settings.GridDefault = false
	settings.Startup = "last"
	resetAppearance(&settings)
	if settings.Theme != "light" || settings.UISkin != "windows10" || settings.AccentColor != "" || settings.GridDefault || settings.Startup != "last" {
		t.Fatal("appearance reset affected unrelated preferences")
	}
	selectThemePalette()
	if themePanelR != 255 {
		t.Fatal("reset palette not restored")
	}
}

func TestPreferenceFieldsArePersisted(t *testing.T) {
	s := Settings{PreferencesVersion: 1, Theme: "system", Startup: "open", ConfirmExit: true, BackupPath: "backup", PlaytestLogsPath: "logs", SecondaryColor: "#112233", PanelColor: "#223344", BorderColor: "#334455", SelectionColor: "#445566", PlaytestConsole: true, PlaytestConfirm: true, DefaultTool: "rectangle"}
	b, e := json.Marshal(s)
	if e != nil {
		t.Fatal(e)
	}
	var out Settings
	if e = json.Unmarshal(b, &out); e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(s, out) {
		t.Fatal("preference round trip failed")
	}
}
