//go:build windows

package main

import (
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
        "show_controls_help(self.graphics, self.root)",
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
    } {
        if !strings.Contains(src, want) {
            t.Fatalf("controls help patch missing %q", want)
        }
    }
    // Check only user-visible fixed-key labels. Comments intentionally mention
    // the removed RPG Maker F1/F8 help to document why it must not return.
    for _, forbidden := range []string{"Frecce: muovi", "F9: debug"} {
        if strings.Contains(src, forbidden) {
            t.Fatalf("controls help must not expose fixed RPG Maker key text %q", forbidden)
        }
    }
}
