//go:build windows

package main

import (
	"encoding/json"
	"syscall"
)

type ProjectManifest struct {
	FormatVersion                      int `json:"format_version"`
	Name, ProjectType, Root, CreatedBy string
	EditorMode                         string   `json:"editor_mode,omitempty"`
	MapFolders                         []string `json:"map_folders,omitempty"`
	// Legacy options read when migrating to battle_settings.json.
	MegaEvolutionsEnabled bool `json:"mega_evolutions_enabled,omitempty"`
	AwakeningsEnabled     bool `json:"awakenings_enabled,omitempty"`
}
type Settings struct {
	PreferencesVersion    int      `json:"preferences_version,omitempty"`
	Language              string   `json:"language,omitempty"`
	Startup               string   `json:"startup,omitempty"`
	ConfirmExit           bool     `json:"confirm_exit"`
	Theme                 string   `json:"theme,omitempty"`
	AccentColor           string   `json:"accent_color,omitempty"`
	SecondaryColor        string   `json:"secondary_color,omitempty"`
	PanelColor            string   `json:"panel_color,omitempty"`
	BorderColor           string   `json:"border_color,omitempty"`
	SelectionColor        string   `json:"selection_color,omitempty"`
	UIScale               int      `json:"ui_scale,omitempty"`
	AutosaveMinutes       int      `json:"autosave_minutes,omitempty"`
	BackupBeforeSave      bool     `json:"backup_before_save"`
	DefaultTool           string   `json:"default_tool,omitempty"`
	PlaytestDebug         bool     `json:"playtest_debug"`
	PlaytestCurrentMap    bool     `json:"playtest_current_map"`
	PlaytestConfirm       bool     `json:"playtest_confirm"`
	PlaytestConsole       bool     `json:"playtest_console"`
	ProjectsPath          string   `json:"projects_path,omitempty"`
	BackupPath            string   `json:"backup_path,omitempty"`
	PlaytestLogsPath      string   `json:"playtest_logs_path,omitempty"`
	RecentProjects        []string `json:"recent_projects"`
	LastProject           string   `json:"last_project"`
	LeftPanelWidth        int      `json:"left_panel_width,omitempty"`
	RightPanelWidth       int      `json:"right_panel_width,omitempty"`
	PermissionPanelWidth  int      `json:"permission_panel_width,omitempty"`
	LeftDetailsHeight     int      `json:"left_details_height,omitempty"`
	PaletteBorderHeight   int      `json:"palette_border_height,omitempty"`
	PaletteAutotileHeight int      `json:"palette_autotile_height,omitempty"`

	// Preferenze puramente UI. Non modificano mai dati o runtime del progetto.
	UISettingsVersion int    `json:"ui_settings_version,omitempty"`
	UISkin            string `json:"ui_skin,omitempty"`
	UIFontSize        int    `json:"ui_font_size,omitempty"`
	GridDefault       bool   `json:"grid_default"`
}
type MapEntry struct {
	ID              int
	Name            string
	ParentID, Order int
	Expanded        bool
	File            string
	RegionID        string
}
type MovementBehaviorSpec struct {
	Code        string `json:"code"`
	Kind        string `json:"kind"`
	Passable    bool   `json:"passable,omitempty"`
	Blocked     bool   `json:"blocked,omitempty"`
	Surf        bool   `json:"surf,omitempty"`
	Elevation   int    `json:"elevation"`
	Transition  bool   `json:"transition,omitempty"`
	MultiLevel  bool   `json:"multi_level,omitempty"`
	Description string `json:"description"`
}
type TerrainTagSpec struct {
	Tag               int    `json:"tag"`
	Kind              string `json:"kind"`
	Description       string `json:"description"`
	FieldMove         string `json:"field_move,omitempty"`
	Activation        string `json:"activation,omitempty"`
	RuntimeAction     string `json:"runtime_action,omitempty"`
	RequiresSurf      bool   `json:"requires_surf,omitempty"`
	RequiresFieldMove bool   `json:"requires_field_move,omitempty"`
	BlocksWithoutMove bool   `json:"blocks_without_move,omitempty"`
	TemporaryEffect   bool   `json:"temporary_effect,omitempty"`
	CustomMN          bool   `json:"custom_mn,omitempty"`
	Extended          bool   `json:"extended,omitempty"`
}
type PermissionDoc struct {
	Version             int                             `json:"version"`
	MapID               int                             `json:"map_id"`
	Width               int                             `json:"width"`
	Height              int                             `json:"height"`
	Cells               map[string]string               `json:"cells,omitempty"` // compatibilita' PLM 0.4
	MovementPermissions map[string]string               `json:"movement_permissions,omitempty"`
	TerrainTags         map[string]int                  `json:"terrain_tags,omitempty"`
	MovementSchema      string                          `json:"movement_schema,omitempty"`
	TerrainSchema       string                          `json:"terrain_schema,omitempty"`
	MovementBehaviors   map[string]MovementBehaviorSpec `json:"movement_behaviors,omitempty"`
	TerrainBehaviors    map[string]TerrainTagSpec       `json:"terrain_behaviors,omitempty"`
	// Border is the canonical 2x2 Advance Map-style border block in row-major
	// order: top-left, top-right, bottom-left, bottom-right. BorderBlock and
	// BorderTiles are written as compatibility/self-describing aliases for
	// older/newer runtime readers, while Border remains the authoritative field.
	Border       []int   `json:"border,omitempty"`
	BorderBlock  [][]int `json:"border_block,omitempty"`
	BorderTiles  []int   `json:"border_tiles,omitempty"`
	BorderWidth  int     `json:"border_width,omitempty"`
	BorderHeight int     `json:"border_height,omitempty"`
	// Directional 2x2 borders used by the current editor. Keep the legacy
	// fields above for existing documents and consumers.
	BorderSchema          string             `json:"border_schema,omitempty"`
	BorderDirections      map[string][]int   `json:"border_directions,omitempty"`
	BorderDirectionBlocks map[string][][]int `json:"border_direction_blocks,omitempty"`
	BorderDirectionWidth  int                `json:"border_direction_width,omitempty"`
	BorderDirectionHeight int                `json:"border_direction_height,omitempty"`
}
type EditorEvent struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	X        int    `json:"x"`
	Y        int    `json:"y"`
	Trigger  string `json:"trigger"`
	Movement string `json:"movement"`
	Dialog   string `json:"dialog"`

	// Graphic metadata is read from the real RPG Maker XP event page.
	// It is used only by the editor preview; the complete native page remains
	// authoritative inside NativeRaw/MapXXX.json.
	CharacterName      string `json:"character_name,omitempty"`
	CharacterHue       int    `json:"character_hue,omitempty"`
	CharacterDirection int    `json:"character_direction,omitempty"`
	CharacterPattern   int    `json:"character_pattern,omitempty"`
	GraphicTileID      int    `json:"graphic_tile_id,omitempty"`
	EventKind          string `json:"event_kind,omitempty"`

	// Trainer reference is lightweight metadata for Pokémon trainer events.
	// The actual trainer/team lives in converted/data/plm_trainers.json.
	TrainerType    string `json:"trainer_type,omitempty"`
	TrainerName    string `json:"trainer_name,omitempty"`
	TrainerVersion int    `json:"trainer_version,omitempty"`

	// Native* preserves the complete converted RPG Maker event while the
	// lightweight Vista eventi reads/edits its common fields. These fields are
	// never serialized to the legacy .plm sidecar.
	Native    bool            `json:"-"`
	NativeKey string          `json:"-"`
	NativeRaw json.RawMessage `json:"-"`
}
type EventDoc struct {
	Version int           `json:"version"`
	MapID   int           `json:"map_id"`
	Events  []EditorEvent `json:"events"`
}

var (
	hwndMain, hwndSidebar, hwndMapList, hwndCanvas, hwndStatus, hwndProjectInfo syscall.Handle
	hwndMapQuickInfo, hwndMapRegionLabel, hwndMapRegionCombo                    syscall.Handle
	hwndToolbarStrip, hwndTabsStrip                                             syscall.Handle
	hwndSortLabel, hwndSortCombo, hwndSearchBox                                 syscall.Handle
	hwndBtnNew, hwndBtnOpen, hwndBtnRecent, hwndBtnSave                         syscall.Handle
	hwndBtnUndo, hwndBtnRedo, hwndBtnFolder, hwndBtnMap                         syscall.Handle
	hwndBtnConnections, hwndBtnZoomOut, hwndBtnZoomIn                           syscall.Handle
	hwndBtnSearch, hwndBtnHelp, hwndBtnPlay, hwndBtnStop                        syscall.Handle
	hwndEventCreate, hwndEventCopy, hwndEventModify, hwndEventDelete            syscall.Handle
	hwndEventMoveMap, hwndEventDuplicate, hwndEventRename                       syscall.Handle

	hwndModeMap, hwndModePass, hwndModeEvents syscall.Handle
	hwndModeEncounters, hwndModeHeader        syscall.Handle
	hwndModeConnections, hwndModeAnimations   syscall.Handle
	hwndModeDatabase                          syscall.Handle

	hwndTopMapBar                                syscall.Handle
	hwndTilesetLabel, hwndTilesetCombo           syscall.Handle
	hwndZoomLabel, hwndZoomCombo                 syscall.Handle
	hwndGridToggle                               syscall.Handle
	hwndLevelLabel, hwndLevelCombo               syscall.Handle
	hwndInspector                                syscall.Handle
	hwndPaletteTilesets, hwndPaletteTilesetList  syscall.Handle
	hwndPaletteAutotile, hwndPaletteAutotileList syscall.Handle
	hwndPaletteBorders, hwndPaletteBorderArea    syscall.Handle
	hwndViewPlaceholder                          syscall.Handle

	hwndEncounterPanel, hwndEncounterList, hwndEncounterSpecies      syscall.Handle
	hwndEncounterRate, hwndEncounterMin, hwndEncounterMax            syscall.Handle
	hwndEncounterSave, hwndEncounterExpand                           syscall.Handle
	hwndHeaderPanel, hwndHeaderName, hwndHeaderMusic, hwndHeaderType syscall.Handle
	hwndHeaderWeather, hwndHeaderBattleBG, hwndHeaderRegion          syscall.Handle
	hwndHeaderNotes, hwndHeaderSave                                  syscall.Handle
	hwndPermModeMovement, hwndPermModeTerrain                        syscall.Handle
	hwndPermSelected                                                 syscall.Handle
	hwndPermHelp                                                     syscall.Handle
	hwndPermissionsPalette                                           syscall.Handle
	hwndMovementButtons                                              []syscall.Handle
	hwndTerrainButtons                                               []syscall.Handle
	hwndEventName, hwndEventTrigger, hwndEventMove                   syscall.Handle
	hwndEventDialog, hwndEventInfo                                   syscall.Handle
	hwndAddEvent, hwndSaveEvent, hwndDeleteEvent                     syscall.Handle
	currentProject, currentProjectType                               string
	settings                                                         Settings
	maps                                                             []MapEntry
	currentMap                                                       *MapEntry
	currentMapW, currentMapH                                         int    = 20, 15
	mode                                                             string = "map"
	permissionEditorMode                                             string = "movement"
	selectedPerm                                                     string = "C"
	selectedTerrainTag                                               int    = 0
	permissions                                                             = map[string]string{}
	terrainTags                                                             = map[string]int{}
	events                                                           []EditorEvent
	selectedEvent                                                    int = -1
	placeEvent                                                       bool
	eventClipboard                                                   *EditorEvent
	pendingEventDuplicate                                            *EditorEvent
	cellSize                                                         int32 = 28
	gridOriginX                                                      int32 = 18
	gridOriginY                                                      int32 = 0
	gridEnabled                                                      bool  = true
)
