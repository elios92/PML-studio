//go:build windows

package main

import (
    "os"
    "path/filepath"
    "strings"
    "testing"
)

func TestNormalizeRuntimeCanonicalPBSPaths(t *testing.T) {
    input := []byte(`a=root/"PBS"; b=root / "PBS" / "town_map.txt"; c=self.project_root / "PBS" / "metadata.txt"; d=project_root / "PBS"`)
    got := string(normalizeRuntimeCanonicalPBSPaths(input))
    for _, legacy := range []string{`root/"PBS"`, `root / "PBS"`, `project_root / "PBS"`} {
        if strings.Contains(got, legacy) {
            t.Fatalf("legacy root PBS path survived normalization: %s", got)
        }
    }
    for _, want := range []string{`root/"converted"/"PBS"`, `root / "converted" / "PBS"`, `self.project_root / "converted" / "PBS"`, `project_root / "converted" / "PBS"`} {
        if !strings.Contains(got, want) {
            t.Fatalf("missing canonical PBS path %q in %s", want, got)
        }
    }
}


func TestPatchRuntimeButtonEventScene(t *testing.T) {
    input := []byte(`        if "pbEventScreen(ButtonEventScene)" in script:
            self._show_text_screen("Comandi", "Frecce: muovi • INVIO: conferma • ESC: annulla • F9: debug")
            return
        if "pbShowMap" in script:
            return`)
    gotBytes, err := patchRuntimeButtonEventScene(input)
    if err != nil {
        t.Fatalf("patch ButtonEventScene failed: %v", err)
    }
    got := string(gotBytes)
    if strings.Contains(got, "Frecce: muovi") || strings.Contains(got, "F9: debug") {
        t.Fatalf("legacy fixed-key help survived patch: %s", got)
    }
    for _, want := range []string{
        "from game.controls_help import show_controls_help",
        "show_controls_help(self.graphics, self.project_root)",
        `if "pbShowMap" in script:`,
    } {
        if !strings.Contains(got, want) {
            t.Fatalf("missing %q after patch: %s", want, got)
        }
    }
}

func TestRuntimeControlsHelpUsesImportedEssentialsAsset(t *testing.T) {
    src := runtimeControlsHelpPython
    for _, want := range []string{
        `assets.image("Controls help/help_bg")`,
        "fully customizable in Game Settings",
        "completamente configurabili nelle Impostazioni di gioco",
    } {
        if !strings.Contains(src, want) {
            t.Fatalf("controls help patch missing %q", want)
        }
    }
    for _, forbidden := range []string{"F1", "F8", "Frecce: muovi", "F9: debug"} {
        if strings.Contains(src, forbidden) {
            t.Fatalf("controls help must not expose legacy Essentials fixed-key text %q", forbidden)
        }
    }
}


func TestDetectEssentialsProjectLanguage(t *testing.T) {
    rows := make([]any, 25)
    rows[24] = map[string]any{"Yes": "Yes", "Cancel": "Cancel", "Your name?": "Your name?"}
    if got := detectEssentialsProjectLanguage(rows); got != "en" {
        t.Fatalf("English Essentials catalog detected as %q", got)
    }
    rows[24] = map[string]any{"Yes": "Sì", "Cancel": "Annulla", "Your name?": "Il tuo nome?"}
    if got := detectEssentialsProjectLanguage(rows); got != "it" {
        t.Fatalf("Italian Essentials catalog detected as %q", got)
    }
}

func TestRuntimeNameEntryUsesImportedEssentialsAssets(t *testing.T) {
    src := runtimeNameEntryPython
    for _, want := range []string{
        "Naming/bg",
        "Naming/overlay_controls",
        "Naming/overlay_tab_",
        "Naming/cursor_1",
        "Naming/cursor_2",
        "Naming/cursor_3",
        "Naming/icon_mode",
        "Naming/icon_shadow",
        "power green.ttf",
        "event_action(root,e)",
        "_txt(tab,font,ch,44+col*32,24+row*38,True)",
        "action=event_action(root,e)",
    } {
        if !strings.Contains(src, want) {
            t.Fatalf("Essentials naming UI missing %q", want)
        }
    }
    for _, forbidden := range []string{"prompt_text", "Come ti chiami?", "elif minlength==0:return ''"} {
        if strings.Contains(src, forbidden) {
            t.Fatalf("generic PML text-entry path survived in naming UI: %q", forbidden)
        }
    }
}

func TestRuntimeEventUICompatibilityPatch(t *testing.T) {
    root := t.TempDir()
    for _, rel := range []string{
        "game/map_scene.py",
        "game/options_dialogue.py",
        "game/options_system.py",
        "config/options.json",
    } {
        data, err := readEmbeddedRuntimeFile(rel)
        if err != nil {
            t.Fatalf("read embedded %s: %v", rel, err)
        }
        path := filepath.Join(root, filepath.FromSlash(rel))
        if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
            t.Fatal(err)
        }
        if err := os.WriteFile(path, data, 0644); err != nil {
            t.Fatal(err)
        }
    }
    if err := os.MkdirAll(filepath.Join(root, "converted"), 0755); err != nil {
        t.Fatal(err)
    }
    if err := os.WriteFile(
        filepath.Join(root, "converted", "messages.json"),
        []byte("{\"detected_language\":\"en\",\"message_types\":[]}"),
        0644,
    ); err != nil {
        t.Fatal(err)
    }

    if err := installRuntimeUICompatibilityPatch(root); err != nil {
        t.Fatalf("runtime UI compatibility patch failed: %v", err)
    }

    mapData, err := os.ReadFile(filepath.Join(root, "game", "map_scene.py"))
    if err != nil {
        t.Fatal(err)
    }
    mapText := string(mapData)
    for _, want := range []string{
        "show_controls_help(self.graphics, self.project_root)",
        `show_name_entry(self, None, 0, 10, "", 1)`,
        "pbEnterText",
        "value = show_name_entry(self, helptext, minlength, maxlength, initial)",
        "numeric_args = re.sub",
        "for continuation in parts[1:]",
        "if continuation and not message.endswith(\" \")",
    } {
        if !strings.Contains(mapText, want) {
            t.Fatalf("patched map scene missing %q", want)
        }
    }
    if strings.Contains(mapText, `prompt_text(self.graphics, "Come ti chiami?"`) {
        t.Fatal("pbTrainerName still uses the generic text prompt")
    }

    dialogueData, err := os.ReadFile(filepath.Join(root, "game", "options_dialogue.py"))
    if err != nil {
        t.Fatal(err)
    }
    dialogueText := string(dialogueData)
    for _, want := range []string{`\l\[(\d+)\]`, `"<ac>"`, `text.split("\n")`, "ui_scale", "line_height", "power green.ttf"} {
        if !strings.Contains(dialogueText, want) {
            t.Fatalf("Essentials dialogue parser missing %q", want)
        }
    }

    optionsData, err := os.ReadFile(filepath.Join(root, "game", "options_system.py"))
    if err != nil {
        t.Fatal(err)
    }
    optionsText := string(optionsData)
    if !strings.Contains(optionsText, `"text_entry": "cursor"`) {
        t.Fatal("Essentials cursor text-entry default was not restored")
    }
    if !strings.Contains(optionsText, `"language": "en"`) ||
        !strings.Contains(optionsText, `settings["language"] = "en"`) {
        t.Fatal("source Essentials language was not propagated to runtime options")
    }

    configData, err := os.ReadFile(filepath.Join(root, "config", "options.json"))
    if err != nil {
        t.Fatal(err)
    }
    configText := string(configData)
    if !strings.Contains(configText, `"text_entry": "cursor"`) ||
        !strings.Contains(configText, `"language": "en"`) {
        t.Fatal("source Essentials UI defaults were not propagated to config/options.json")
    }
}
