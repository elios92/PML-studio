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

func TestPatchRuntimeFieldMoveConfirmDisplay(t *testing.T) {
    input := []byte(`from game.data_registry import registry, split_csv

def _show(scene, title: str, text: str) -> None:
    from game.debug_menu import show_message
    show_message(scene.graphics, title, text)

def _confirm_inline(scene, title: str, text: str) -> bool:
    # show_message chiude con un tasto; subito dopo proponiamo la scelta sì/no.
    _show(scene, title, text)
    return scene._show_choices(["Sì", "No"]) == 0

def _announce(scene, pokemon: dict[str, Any] | None, move_id: str) -> None:
    name = str((pokemon or {}).get("nickname") or scene.game_state.get("player_name", "Allenatore"))
    _show(scene, "Mossa da campo", f"{name} usa {move_name(scene, move_id)}!")

def start_surf(scene, pokemon=None):
    if not _confirm_inline(scene, "Surf", "L'acqua è di un blu intenso...\nVuoi usare Surf?"):
        return False`)
    gotBytes, err := patchRuntimeFieldMoveConfirmDisplay(input)
    if err != nil {
        t.Fatalf("patch field move confirm failed: %v", err)
    }
    got := string(gotBytes)
    for _, forbidden := range []string{
        "from game.debug_menu import show_message",
        `["Sì", "No"]`,
        "Mossa da campo",
        "Allenatore",
        "L'acqua è di un blu intenso",
        "Vuoi usare Surf?",
    } {
        if strings.Contains(got, forbidden) {
            t.Fatalf("legacy/translated field-move UI survived patch: %q in %s", forbidden, got)
        }
    }
    for _, want := range []string{
        "from game.message_system import intl",
        "scene._show_dialogue(text)",
        `scene._show_choices([intl("Yes"), intl("No")])`,
        `intl("{1} used {2}!", name, move_name(scene, move_id))`,
        `intl("The water is a deep blue color... Would you like to use Surf on it?")`,
    } {
        if !strings.Contains(got, want) {
            t.Fatalf("Essentials field-move presentation missing %q in %s", want, got)
        }
    }
}


func TestPatchRuntimeButtonEventScene(t *testing.T) {
    input := []byte(`        if "pbEventScreen(ButtonEventScene)" in script:
            self._show_text_screen("Comandi", "Frecce: muovi • INVIO: conferma • ESC: annulla • F9: debug")
            return
        show_map = re.search(r"pbShowMap", script)
        if show_map:
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
        `show_map = re.search(r"pbShowMap", script)`,
        "if show_map:",
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
        "scale = max(1, int(min(sw / 512.0, sh / 384.0)))",
        "dw, dh = 512 * scale, 384 * scale",
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
        "MALE_PRESET_NAMES=(\"Alex\",\"Sam\",\"Nico\",\"Ari\",\"Eli\")",
        "FEMALE_PRESET_NAMES=(\"Maya\",\"Luna\",\"Iris\",\"Zoe\",\"Nina\")",
        "Nome personalizzato",
        "Custom name",
        "presets=FEMALE_PRESET_NAMES if profile==2 else MALE_PRESET_NAMES",
        "scene._show_choices([custom_label,*presets])",
        "if selected==0:",
        "return show_name_entry(scene,None,1,10,\"\",1)",
        "k=max(1,int(min(w/512.0,h/384.0)))",
        "dw,dh=512*k,384*k",
        "if 1<=selected<=len(presets):",
        "return presets[selected-1]",
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
        "game/item_effects.py",
        "game/title_scene.py",
        "game/debug_menu.py",
        "game/map_select_scene.py",
        "game/pause_menu.py",
        "game/mart_scene.py",
        "game/field_moves.py",
        "main.py",
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
        "from game.name_entry_scene import choose_player_name",
        "name = choose_player_name(self)",
        "pbEnterText",
        "value = show_name_entry(self, helptext, minlength, maxlength, initial)",
        "numeric_args = re.sub",
        "for continuation in parts[1:]",
        "if continuation and not message.endswith(\" \")",
        "from game.options_system import event_action",
        "action = event_action(self.project_root, event)",
        "power green.ttf",
        "ui_scale = max(1, int(min(sw / 512.0, sh / 384.0)))",
        "viewport_w, viewport_h = 512 * ui_scale, 384 * ui_scale",
        "path = self.picture_catalog.find(picture[\"name\"])",
        "pygame.transform.scale(",
        "show_map = re.search",
        `load_windowskin(self.project_root, "menu"`,
        "draw_windowskin(logical, skin",
        `self.game_state.get("_last_message_rect")`,
        `self.game_state["player_profile"] = int(change.group(1))`,
        "_player_metadata_charsets",
        `self.project_root / "converted" / "PBS" / "metadata.txt"`,
        `values.get("runcharset") or walk`,
        `values.get("cyclecharset") or run`,
        `values.get("surfcharset") or cycle`,
        `values.get("divecharset") or surf`,
        `values.get("fishcharset") or walk`,
        `values.get("surffishcharset") or fish`,
        "self._refresh_player_charset(running=running)",
        "self._refresh_player_charset(running=False)",
    } {
        if !strings.Contains(mapText, want) {
            t.Fatalf("patched map scene missing %q", want)
        }
    }
    if strings.Contains(mapText, `prompt_text(self.graphics, "Come ti chiami?"`) {
        t.Fatal("pbTrainerName still uses the generic text prompt")
    }
    if strings.Contains(mapText, `self.game_state["player_profile"] = int(change.group(1)) + 1`) {
        t.Fatal("pbChangePlayer must preserve the Essentials PlayerMetadata ID exactly")
    }
    if strings.Contains(mapText, "border_radius=8") {
        t.Fatal("generic rounded PML choice panel survived instead of Essentials menu windowskin")
    }
    toneIndex := strings.Index(mapText, `tone = self.game_state.get("screen_tone")`)
    weatherIndex := strings.Index(mapText, `self._draw_overworld_weather()`)
    pictureIndex := strings.LastIndex(mapText, `self._draw_pictures()`)
    if toneIndex < 0 || weatherIndex < 0 || pictureIndex < 0 ||
        !(toneIndex < weatherIndex && weatherIndex < pictureIndex) {
        t.Fatalf("Essentials Pictures must render above map tone/weather; tone=%d weather=%d pictures=%d", toneIndex, weatherIndex, pictureIndex)
    }

    dialogueData, err := os.ReadFile(filepath.Join(root, "game", "options_dialogue.py"))
    if err != nil {
        t.Fatal(err)
    }
    dialogueText := string(dialogueData)
    for _, want := range []string{
        `linecount=2`,
        `\l\[(\d+)\]`,
        `"<ac>"`,
        `raw.split("\n")`,
        "scene._render_world()",
        `load_windowskin(scene.project_root,"speech"`,
        "BASE_W-left-right-4",
        "linecount*32",
        "blit_logical_overlay",
        "power green.ttf",
        `scene.game_state["_last_message_rect"]`,
        "draw_windowskin(logical,skin,box)",
    } {
        if !strings.Contains(dialogueText, want) {
            t.Fatalf("Essentials dialogue parser missing %q", want)
        }
    }

    uiData, err := os.ReadFile(filepath.Join(root, "game", "essentials_ui.py"))
    if err != nil {
        t.Fatal(err)
    }
    uiText := string(uiData)
    for _, want := range []string{
        "BASE_W=512",
        "BASE_H=384",
        "speech hgss 1",
        "choice 1",
        "def draw_windowskin",
        "pygame.transform.scale",
        "def essentials_font",
        "power green.ttf",
    } {
        if !strings.Contains(uiText, want) {
            t.Fatalf("shared Essentials UI renderer missing %q", want)
        }
    }

    debugData, err := os.ReadFile(filepath.Join(root, "game", "debug_menu.py"))
    if err != nil {
        t.Fatal(err)
    }
    debugText := string(debugData)
    for _, want := range []string{
        "from game.essentials_ui import essentials_font",
        `load_windowskin(resolved_root, "menu", 0)`,
        "draw_windowskin(logical, skin",
        "present_logical(graphics.screen, logical)",
    } {
        if !strings.Contains(debugText, want) {
            t.Fatalf("debug menu did not use Essentials UI: %q", want)
        }
    }
    for _, forbidden := range []string{"ui debug menu.png", "border_radius=_scaled", "pygame.font.Font(None"} {
        if strings.Contains(debugText, forbidden) {
            t.Fatalf("legacy PML debug renderer survived: %q", forbidden)
        }
    }

    for _, want := range []string{
        "resolved_root = Path(root) if root is not None else Path.cwd()",
        "while page < len(lines):",
        "for line in lines[page:page+3]:",
        "page += 3",
    } {
        if !strings.Contains(debugText, want) {
            t.Fatalf("debug UI regression guard missing %q", want)
        }
    }

    mapSelectData, err := os.ReadFile(filepath.Join(root, "game", "map_select_scene.py"))
    if err != nil {
        t.Fatal(err)
    }
    mapSelectText := string(mapSelectData)
    for _, want := range []string{
        "essentials_font(self.project_root",
        `load_windowskin(self.project_root,"menu",0)`,
        "present_logical(self.graphics.screen,logical)",
    } {
        if !strings.Contains(mapSelectText, want) {
            t.Fatalf("map selector did not use Essentials UI: %q", want)
        }
    }

    pauseData, err := os.ReadFile(filepath.Join(root, "game", "pause_menu.py"))
    if err != nil {
        t.Fatal(err)
    }
    pauseText := string(pauseData)
    for _, want := range []string{
        "essentials_font(self.root",
        `self.menu_skin=load_windowskin(self.root,"menu",0)`,
        "draw_windowskin(self.graphics.screen,self.menu_skin,rect)",
    } {
        if !strings.Contains(pauseText, want) {
            t.Fatalf("pause menu did not use Essentials UI: %q", want)
        }
    }

    martData, err := os.ReadFile(filepath.Join(root, "game", "mart_scene.py"))
    if err != nil {
        t.Fatal(err)
    }
    if strings.Contains(string(martData), "pygame.font.Font(None") {
        t.Fatal("Poké Mart still uses pygame system fallback font")
    }
    fieldData, err := os.ReadFile(filepath.Join(root, "game", "field_moves.py"))
    if err != nil {
        t.Fatal(err)
    }
    if strings.Contains(string(fieldData), "pygame.font.Font(None") {
        t.Fatal("field move UI still uses pygame system fallback font")
    }

    titleData, err := os.ReadFile(filepath.Join(root, "game", "title_scene.py"))
    if err != nil {
        t.Fatal(err)
    }
    titleText := string(titleData)
    for _, want := range []string{
        "from game.essentials_title_ui import show_splash",
        "from game.essentials_title_ui import title_wait",
        "from game.essentials_title_ui import draw_load_menu",
        "from game.essentials_title_ui import menu_entries",
        "from game.essentials_title_ui import choose",
    } {
        if !strings.Contains(titleText, want) {
            t.Fatalf("title scene did not route through Essentials UI: %q", want)
        }
    }

    titleUIData, err := os.ReadFile(filepath.Join(root, "game", "essentials_title_ui.py"))
    if err != nil {
        t.Fatal(err)
    }
    titleUIText := string(titleUIData)
    if strings.Contains(titleUIText, "trchar000") || strings.Contains(titleUIText, "trchar001") {
        t.Fatal("title UI must use the imported PlayerMetadata WalkCharset, not hard-coded player aliases")
    }
    for _, want := range []string{
        "(0,222 if selected else 0,408,222)",
        "(0,490 if selected else 444,408,46)",
        "(48,y)",
        "y+=224",
        "row*48",
        "_font(scene,27)",
        "present_logical",
        `intl("New Game")`,
        `intl("Continue")`,
        `action=event_action(scene.project_root,event)`,
        "def _walk_charset(scene,state):",
        `metadata=scene.project_root/"converted"/"PBS"/"metadata.txt"`,
        `if key.casefold()=="walkcharset":`,
    } {
        if !strings.Contains(titleUIText, want) {
            t.Fatalf("Essentials load menu geometry missing %q", want)
        }
    }

    mainData, err := os.ReadFile(filepath.Join(root, "main.py"))
    if err != nil {
        t.Fatal(err)
    }
    mainText := string(mainData)
    if !strings.Contains(mainText, "WINDOW_SIZE = (512, 384)") ||
        strings.Contains(mainText, "WINDOW_SIZE = (1336, 1000)") {
        t.Fatal("runtime host fallback is not locked to native Essentials 512x384")
    }

    optionsData, err := os.ReadFile(filepath.Join(root, "game", "options_system.py"))
    if err != nil {
        t.Fatal(err)
    }
    optionsText := string(optionsData)
    if !strings.Contains(optionsText, `"text_entry": "cursor"`) {
        t.Fatal("Essentials cursor text-entry default was not restored")
    }
    for _, want := range []string{
        `"essentials_1x": ("Essentials 1x — 512×384", (512, 384), False)`,
        `"essentials_2x": ("Essentials 2x — 1024×768", (1024, 768), False)`,
        `"window_mode": "essentials_1x"`,
        `WINDOW_PRESETS["essentials_1x"]`,
    } {
        if !strings.Contains(optionsText, want) {
            t.Fatalf("Essentials display policy missing %q", want)
        }
    }
    for _, forbidden := range []string{"window_1336", "window_800", "window_1280", "gba_1x"} {
        if strings.Contains(optionsText, forbidden) {
            t.Fatalf("arbitrary legacy display preset survived: %q", forbidden)
        }
    }
    if !strings.Contains(optionsText, `"language": "en"`) ||
        !strings.Contains(optionsText, `settings["language"] = "en"`) {
        t.Fatal("source Essentials language was not propagated to runtime options")
    }

    itemEffectsData, err := os.ReadFile(filepath.Join(root, "game", "item_effects.py"))
    if err != nil {
        t.Fatal(err)
    }
    itemEffectsText := string(itemEffectsData)
    for _, want := range []string{
        `state["bicycle"] = not bool(state.get("bicycle", False))`,
        `hasattr(scene, "_refresh_player_charset")`,
        `scene._refresh_player_charset(running=False)`,
    } {
        if !strings.Contains(itemEffectsText, want) {
            t.Fatalf("bicycle charset refresh missing %q", want)
        }
    }

    configData, err := os.ReadFile(filepath.Join(root, "config", "options.json"))
    if err != nil {
        t.Fatal(err)
    }
    configText := string(configData)
    if !strings.Contains(configText, `"text_entry": "cursor"`) ||
        !strings.Contains(configText, `"language": "en"`) ||
        !strings.Contains(configText, `"window_mode": "essentials_1x"`) {
        t.Fatal("source Essentials UI defaults were not propagated to config/options.json")
    }
}
