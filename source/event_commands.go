//go:build windows

package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"unsafe"
)

const (
	eventCommandDialogClassName = "PLMStudioEventCommandDialog01"

	idEventCmdSearch    = 7500
	idEventCmdList      = 7501
	idEventCmdID        = 7502
	idEventCmdValue     = 7503
	idEventCmdOneShot   = 7504
	idEventCmdText      = 7505
	idEventCmdOK        = 7506
	idEventCmdCancel    = 7507
	idEventCmdLegendary = 7508
)

type eventCommandDialogMode int

const (
	eventCommandText eventCommandDialogMode = iota
	eventCommandHiddenItem
	eventCommandItemBall
	eventCommandFixedPokemon
	eventCommandGiveItem
)

type eventCommandChoice struct {
	ID   string
	Name string
}

// plmMovementStep is one deterministic step in a PLM movement sequence.
// The runtime executes steps strictly in order and waits for completion before
// advancing to the next event command.
type plmMovementStep struct {
	Op   string `json:"op"`
	A    int    `json:"a,omitempty"`
	B    int    `json:"b,omitempty"`
	Text string `json:"text,omitempty"`
}

type plmMapPoint struct {
	X int `json:"x"`
	Y int `json:"y"`
}

type plmManagedEventCommand struct {
	Type            string            `json:"type"`
	Item            string            `json:"item,omitempty"`
	Quantity        int               `json:"quantity,omitempty"`
	Hidden          bool              `json:"hidden,omitempty"`
	Species         string            `json:"species,omitempty"`
	Level           int               `json:"level,omitempty"`
	OneShot         bool              `json:"one_shot,omitempty"`
	MapID           int               `json:"map_id,omitempty"`
	X               int               `json:"x,omitempty"`
	Y               int               `json:"y,omitempty"`
	Direction       int               `json:"direction,omitempty"`
	SelfSwitch      string            `json:"self_switch,omitempty"`
	State           string            `json:"state,omitempty"`
	VariableID      int               `json:"variable_id,omitempty"`
	VariableName    string            `json:"variable_name,omitempty"`
	Operation       string            `json:"operation,omitempty"`
	Value           int               `json:"value,omitempty"`
	Choices         []string          `json:"choices,omitempty"`
	Graphic         string            `json:"graphic,omitempty"`
	CommonEventID   int               `json:"common_event_id,omitempty"`
	CommonEventName string            `json:"common_event_name,omitempty"`
	Text            string            `json:"text,omitempty"`
	Script          string            `json:"script,omitempty"`
	TargetEventID   int               `json:"target_event_id,omitempty"`
	Distance        int               `json:"distance,omitempty"`
	Speaker         string            `json:"speaker,omitempty"`
	Position        string            `json:"position,omitempty"`
	Action          string            `json:"action,omitempty"`
	PictureSlot     int               `json:"picture_slot,omitempty"`
	FieldMoveID     string            `json:"field_move_id,omitempty"`
	FieldMoveName   string            `json:"field_move_name,omitempty"`
	Move            string            `json:"move,omitempty"`
	Handler         string            `json:"handler,omitempty"`
	POIName         string            `json:"poi_name,omitempty"`
	RegionID        string            `json:"region_id,omitempty"`
	Unlocked        bool              `json:"unlocked,omitempty"`
	Red             int               `json:"red,omitempty"`
	Green           int               `json:"green,omitempty"`
	Blue            int               `json:"blue,omitempty"`
	Alpha           int               `json:"alpha,omitempty"`
	Gray            int               `json:"gray,omitempty"`
	Duration        int               `json:"duration,omitempty"`
	Power           int               `json:"power,omitempty"`
	Speed           int               `json:"speed,omitempty"`
	Tiles           int               `json:"tiles,omitempty"`
	Zoom            int               `json:"zoom,omitempty"`
	Wait            bool              `json:"wait,omitempty"`
	GlobalID        string            `json:"global_id,omitempty"`
	GlobalName      string            `json:"global_name,omitempty"`
	TicketID        string            `json:"ticket_id,omitempty"`
	TicketName      string            `json:"ticket_name,omitempty"`
	TicketKind      string            `json:"ticket_kind,omitempty"`
	RequiredTicket  string            `json:"required_ticket,omitempty"`
	Consume         bool              `json:"consume,omitempty"`
	Transport       string            `json:"transport,omitempty"`
	RouteID         string            `json:"route_id,omitempty"`
	RouteName       string            `json:"route_name,omitempty"`
	CooldownSteps   int               `json:"cooldown_steps,omitempty"`
	MaxUses         int               `json:"max_uses,omitempty"`
	RequiredGlobal  string            `json:"required_global,omitempty"`
	AccessID        string            `json:"access_id,omitempty"`
	RequirementType string            `json:"requirement_type,omitempty"`
	RequiredItem    string            `json:"required_item,omitempty"`
	ConsumeItem     bool              `json:"consume_item,omitempty"`
	Legendary       bool              `json:"legendary,omitempty"`
	Target          string            `json:"target,omitempty"`
	Moves           []plmMovementStep `json:"moves,omitempty"`
	RepeatCount     int               `json:"repeat_count,omitempty"`
	Origin          int               `json:"origin,omitempty"`
	ScaleX          int               `json:"scale_x,omitempty"`
	ScaleY          int               `json:"scale_y,omitempty"`
	Opacity         int               `json:"opacity,omitempty"`
	BlendMode       int               `json:"blend_mode,omitempty"`
	AudioName       string            `json:"audio_name,omitempty"`
	AudioKind       string            `json:"audio_kind,omitempty"`
	Volume          int               `json:"volume,omitempty"`
	Pitch           int               `json:"pitch,omitempty"`
	FadeSeconds     int               `json:"fade_seconds,omitempty"`
	Interaction     string            `json:"interaction,omitempty"`
	Destinations    []plmDestination  `json:"destinations,omitempty"`
	RequirementID   string            `json:"requirement_id,omitempty"`
	FailText        string            `json:"fail_text,omitempty"`
	Permanent       bool              `json:"permanent,omitempty"`
	StoryType       string            `json:"story_type,omitempty"`
	StoryID         string            `json:"story_id,omitempty"`
	StoryName       string            `json:"story_name,omitempty"`
	ChapterID       string            `json:"chapter_id,omitempty"`
	SceneID         string            `json:"scene_id,omitempty"`
	Stage           int               `json:"stage,omitempty"`
	Objective       string            `json:"objective,omitempty"`
	Description     string            `json:"description,omitempty"`
	ConditionType   string            `json:"condition_type,omitempty"`
	Compare         string            `json:"compare,omitempty"`
	Radius          int               `json:"radius,omitempty"`
	Width           int               `json:"width,omitempty"`
	Height          int               `json:"height,omitempty"`
	Timeout         int               `json:"timeout,omitempty"`
	PuzzleID        string            `json:"puzzle_id,omitempty"`
	Token           string            `json:"token,omitempty"`
	Sequence        []string          `json:"sequence,omitempty"`
	Path            []plmMapPoint     `json:"path,omitempty"`
	Threshold       int               `json:"threshold,omitempty"`
}

type plmDestination struct {
	Name      string `json:"name"`
	MapID     int    `json:"map_id"`
	X         int    `json:"x"`
	Y         int    `json:"y"`
	Direction int    `json:"direction,omitempty"`
}

var (
	eventCommandDialogRegistered bool
	eventCommandDialogOpen       bool
	eventCommandDialogWindow     syscall.Handle
	eventCommandDialogOwner      syscall.Handle
	eventCommandDialogSearch     syscall.Handle
	eventCommandDialogList       syscall.Handle
	eventCommandDialogID         syscall.Handle
	eventCommandDialogValue      syscall.Handle
	eventCommandDialogOneShot    syscall.Handle
	eventCommandDialogText       syscall.Handle
	eventCommandDialogLegendary  syscall.Handle

	eventCommandDialogModeValue       eventCommandDialogMode
	eventCommandDialogCatalog         []eventCommandChoice
	eventCommandDialogFiltered        []eventCommandChoice
	eventCommandDialogAccepted        bool
	eventCommandDialogResultID        string
	eventCommandDialogResultVal       int
	eventCommandDialogResultOne       bool
	eventCommandDialogResultTxt       string
	eventCommandDialogResultLegendary bool
	// -1 = legacy/manual checkbox, 0 = Wild Battle, 1 = fixed legendary battle.
	eventCommandDialogFixedPokemonMode int = -1

	eventItemCatalogProject string
	eventItemCatalogCache   []eventCommandChoice
)

func plmEventCommandMarker(c plmManagedEventCommand) string {
	b, _ := json.Marshal(c)
	return "PML_CMD:" + string(b)
}

func parsePLMEventCommandMarker(s string) (plmManagedEventCommand, bool) {
	var out plmManagedEventCommand
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "PML_CMD:") {
		return out, false
	}
	if err := json.Unmarshal([]byte(strings.TrimPrefix(s, "PML_CMD:")), &out); err != nil {
		return plmManagedEventCommand{}, false
	}
	return out, strings.TrimSpace(out.Type) != ""
}

func managedEventCommandLabel(c plmManagedEventCommand) string {
	switch c.Type {
	case "hidden_item":
		return fmt.Sprintf("◆ Oggetto nascosto: %s x%d", eventItemDisplayName(c.Item), maxIntEventCmd(c.Quantity, 1))
	case "item_ball":
		return fmt.Sprintf("◆ Poké Ball: %s x%d", eventItemDisplayName(c.Item), maxIntEventCmd(c.Quantity, 1))
	case "fly_poi":
		name := strings.TrimSpace(c.POIName)
		if name == "" {
			name = "Punto Volo"
		}
		state := "da sbloccare"
		if c.Unlocked {
			state = "sbloccato"
		}
		return fmt.Sprintf("◆ Punto Volo / POI: %s · regione %s · %s", name, strings.ToUpper(strings.TrimSpace(c.RegionID)), state)
	case "field_move":
		name := strings.TrimSpace(c.FieldMoveName)
		if name == "" {
			name = strings.TrimSpace(c.FieldMoveID)
		}
		move := strings.ToUpper(strings.TrimSpace(c.Move))
		if move != "" {
			return fmt.Sprintf("◆ Evento Campo/MN: %s [%s] · %s", name, strings.TrimSpace(c.FieldMoveID), move)
		}
		return fmt.Sprintf("◆ Evento Campo/MN: %s [%s]", name, strings.TrimSpace(c.FieldMoveID))
	case "fixed_pokemon":
		suffix := ""
		if c.OneShot {
			suffix = " · una sola volta"
		}
		kind := "Battaglia Pokémon"
		if c.Legendary {
			kind = "Pokémon leggendario fisso"
		}
		return fmt.Sprintf("◆ %s: %s Lv.%d%s", kind, speciesDisplayName(c.Species), maxIntEventCmd(c.Level, 1), suffix)
	case "warp":
		return fmt.Sprintf("◆ Warp: %03d - %s → (%d,%d) · %s", c.MapID, eventWarpMapName(c.MapID), c.X, c.Y, directionLabel(c.Direction))
	case "self_switch":
		state := strings.ToUpper(strings.TrimSpace(c.State))
		if state == "" {
			state = "ON"
		}
		return fmt.Sprintf("◆ Self Switch %s → %s", strings.TrimSpace(c.SelfSwitch), state)
	case "unlimited_self_switch":
		state := strings.ToUpper(strings.TrimSpace(c.State))
		if state == "" {
			state = "ON"
		}
		return fmt.Sprintf("◆ Unlimited Self Switch %s → %s", strings.TrimSpace(c.SelfSwitch), state)
	case "variable":
		name := strings.TrimSpace(c.VariableName)
		if name == "" {
			name = fmt.Sprintf("Variabile %04d", c.VariableID)
		}
		return fmt.Sprintf("◆ Variabile %04d %s %s %d", c.VariableID, name, variableOperationSymbol(c.Operation), c.Value)
	case "choices":
		return "◆ Mostra scelte: " + strings.Join(c.Choices, " / ")
	case "mugshot":
		action := strings.ToLower(strings.TrimSpace(c.Action))
		if action == "hide" {
			return fmt.Sprintf("◆ Nascondi mugshot · slot %d", maxIntEventCmd(c.PictureSlot, 50))
		}
		name := strings.TrimSpace(c.Speaker)
		if name == "" {
			name = strings.TrimSpace(c.Graphic)
		}
		pos := strings.TrimSpace(c.Position)
		if pos == "" {
			pos = "Sinistra"
		}
		return fmt.Sprintf("◆ Mugshot: %s · %s · %s", name, strings.TrimSpace(c.Graphic), pos)
	case "change_overworld":
		return "◆ Cambia overworld giocatore: " + strings.TrimSpace(c.Graphic)
	case "common_event":
		name := strings.TrimSpace(c.CommonEventName)
		if name == "" {
			name = fmt.Sprintf("Evento comune %d", c.CommonEventID)
		}
		return fmt.Sprintf("◆ Chiama evento comune %d: %s", c.CommonEventID, name)
	case "autorun":
		return "◆ Evento automatico (Autorun)"
	case "script_text":
		return "◆ Script da testo: " + compactEventCommandText(c.Text, 72)
	case "python_script":
		return "◆ Script avanzato Python: " + compactEventCommandText(c.Script, 72)
	case "temporary_event":
		return "◆ Evento temporaneo: cancella fino al ricaricamento mappa"
	case "give_item":
		return fmt.Sprintf("◆ Dai oggetto: %s x%d", eventItemDisplayName(c.Item), maxIntEventCmd(c.Quantity, 1))
	case "player_follow_npc":
		return fmt.Sprintf("◆ Giocatore segue NPC/evento #%d", c.TargetEventID)
	case "npc_follow_player":
		return fmt.Sprintf("◆ NPC/evento corrente segue giocatore · distanza %d", maxIntEventCmd(c.Distance, 1))
	case "global_flag":
		name := strings.TrimSpace(c.GlobalName)
		if name == "" {
			name = strings.TrimSpace(c.GlobalID)
		}
		action := strings.ToUpper(strings.TrimSpace(c.Action))
		if action == "" {
			action = "ON"
		}
		return fmt.Sprintf("◆ Evento globale: %s [%s] → %s", name, strings.TrimSpace(c.GlobalID), action)
	case "global_ticket":
		name := strings.TrimSpace(c.TicketName)
		if name == "" {
			name = eventItemDisplayName(c.Item)
		}
		action := globalTicketActionLabel(c.Action)
		return fmt.Sprintf("◆ Biglietto/Pass: %s [%s] · %s", name, strings.TrimSpace(c.TicketID), action)
	case "access_gate":
		name := strings.TrimSpace(c.GlobalName)
		if name == "" {
			name = strings.TrimSpace(c.AccessID)
		}
		req := strings.TrimSpace(c.TicketID)
		if strings.EqualFold(c.RequirementType, "item") {
			req = eventItemDisplayName(c.RequiredItem)
		}
		mode := "controlla"
		if c.ConsumeItem {
			mode = "controlla e consuma"
		}
		return fmt.Sprintf("◆ Accesso: %s · %s %s", name, mode, req)
	case "ship_travel":
		req := "nessun pass"
		if strings.TrimSpace(c.RequiredTicket) != "" {
			req = "richiede " + strings.TrimSpace(c.RequiredTicket)
		}
		return fmt.Sprintf("◆ Viaggio %s: %s → Map%03d (%d,%d) · %s", strings.TrimSpace(c.Transport), strings.TrimSpace(c.RouteName), c.MapID, c.X, c.Y, req)
	case "rebattle":
		return fmt.Sprintf("◆ Rebattle/Rematch: cooldown %d passi", maxIntEventCmd(c.CooldownSteps, 0))
	case "movement_route":
		target := "Giocatore"
		if strings.EqualFold(strings.TrimSpace(c.Target), "event") {
			if c.TargetEventID > 0 {
				target = fmt.Sprintf("NPC/Evento #%d", c.TargetEventID)
			} else {
				target = "NPC/Evento corrente"
			}
		}
		reps := maxIntEventCmd(c.RepeatCount, 1)
		return fmt.Sprintf("◆ Movimento %s: %d comandi · %d ciclo/i · attesa completa", target, len(c.Moves), reps)
	case "picture_show":
		return fmt.Sprintf("◆ Mostra immagine #%d: %s · (%d,%d)", maxIntEventCmd(c.PictureSlot, 1), strings.TrimSpace(c.Graphic), c.X, c.Y)
	case "picture_move":
		return fmt.Sprintf("◆ Muovi immagine #%d → (%d,%d) · %d frame", maxIntEventCmd(c.PictureSlot, 1), c.X, c.Y, maxIntEventCmd(c.Duration, 1))
	case "picture_hide":
		return fmt.Sprintf("◆ Nascondi immagine #%d", maxIntEventCmd(c.PictureSlot, 1))
	case "pokemon_give":
		return fmt.Sprintf("◆ Dai Pokémon: %s Lv.%d", speciesDisplayName(c.Species), maxIntEventCmd(c.Level, 1))
	case "pokemon_remove":
		return fmt.Sprintf("◆ Rimuovi Pokémon: %s", speciesDisplayName(c.Species))
	case "pokemon_egg":
		return fmt.Sprintf("◆ Dai Uovo: %s", speciesDisplayName(c.Species))
	case "pokemon_heal":
		return "◆ Cura completamente la squadra"
	case "cutscene_begin":
		return "◆ Inizio cutscene · blocca input e salva stato camera"
	case "cutscene_end":
		return "◆ Fine cutscene · ripristina input/camera"
	case "audio_play":
		return fmt.Sprintf("◆ Audio %s: %s · vol %d · pitch %d", strings.ToUpper(c.AudioKind), c.AudioName, maxIntEventCmd(c.Volume, 100), maxIntEventCmd(c.Pitch, 100))
	case "audio_stop":
		return "◆ Stop audio " + strings.ToUpper(c.AudioKind)
	case "audio_fade":
		return fmt.Sprintf("◆ Dissolvi %s · %d sec", strings.ToUpper(c.AudioKind), maxIntEventCmd(c.FadeSeconds, 1))
	case "map_interaction":
		return "◆ Interazione mappa: " + strings.TrimSpace(c.Interaction)
	case "progress_condition":
		return fmt.Sprintf("◆ Condizione progresso: %s [%s]", strings.TrimSpace(c.RequirementType), strings.TrimSpace(c.RequirementID))
	case "logic_if":
		return fmt.Sprintf("◆ SE: %s %s", strings.TrimSpace(c.ConditionType), strings.TrimSpace(c.RequirementID))
	case "logic_else":
		return "◆ ALTRIMENTI"
	case "logic_end":
		return "◆ FINE SE"
	case "area_trigger":
		if c.Radius > 0 {
			return fmt.Sprintf("◆ Trigger area: raggio %d", c.Radius)
		}
		return fmt.Sprintf("◆ Trigger area: rettangolo (%d,%d) %dx%d", c.X, c.Y, maxIntEventCmd(c.Width, 1), maxIntEventCmd(c.Height, 1))
	case "pathfind_move":
		return fmt.Sprintf("◆ Path NPC #%d → (%d,%d) · attesa completa", c.TargetEventID, c.X, c.Y)
	case "wait_until_zone":
		target := c.Target
		if strings.EqualFold(target, "event") {
			target = fmt.Sprintf("NPC #%d", c.TargetEventID)
		} else {
			target = "Giocatore"
		}
		return fmt.Sprintf("◆ Attendi %s in zona (%d,%d) %dx%d", target, c.X, c.Y, maxIntEventCmd(c.Width, 1), maxIntEventCmd(c.Height, 1))
	case "emote":
		target := "Giocatore"
		if strings.EqualFold(c.Target, "event") {
			target = fmt.Sprintf("NPC #%d", c.TargetEventID)
		}
		return fmt.Sprintf("◆ Emote %s → %s", strings.ToUpper(c.Action), target)
	case "puzzle_path":
		return fmt.Sprintf("◆ Puzzle percorso [%s]: %d caselle", c.PuzzleID, len(c.Path))
	case "puzzle_sequence_input":
		return fmt.Sprintf("◆ Puzzle sequenza [%s]: input %s", c.PuzzleID, c.Token)
	case "puzzle_condition":
		return fmt.Sprintf("◆ Richiedi puzzle completato [%s]", c.PuzzleID)
	case "puzzle_timer":
		return fmt.Sprintf("◆ Puzzle timer [%s] → %s %d sec", c.PuzzleID, strings.ToUpper(c.Action), maxIntEventCmd(c.Duration, 1))
	case "puzzle_counter":
		return fmt.Sprintf("◆ Puzzle contatore [%s] → %s / %d", c.PuzzleID, strings.ToUpper(c.Action), maxIntEventCmd(c.Threshold, 1))
	case "story_state":
		return fmt.Sprintf("◆ %s %s [%s] → %s", strings.Title(strings.TrimSpace(c.StoryType)), strings.TrimSpace(c.StoryName), strings.TrimSpace(c.StoryID), strings.ToUpper(strings.TrimSpace(c.Action)))
	case "scene_marker":
		return fmt.Sprintf("◆ Scena: %s [%s]", strings.TrimSpace(c.StoryName), strings.TrimSpace(c.SceneID))
	case "wait_frames":
		return fmt.Sprintf("◆ Attendi %d frame", maxIntEventCmd(c.Duration, 1))
	case "camera_pan":
		return fmt.Sprintf("◆ Camera: sposta %s · %d tile · velocità %d", directionLabel(c.Direction), maxIntEventCmd(c.Tiles, 1), maxIntEventCmd(c.Speed, 1))
	case "camera_shake":
		return fmt.Sprintf("◆ Camera: tremore · forza %d · velocità %d · %d frame", maxIntEventCmd(c.Power, 1), maxIntEventCmd(c.Speed, 1), maxIntEventCmd(c.Duration, 1))
	case "screen_flash":
		return fmt.Sprintf("◆ Flash schermo: RGB(%d,%d,%d) · intensità %d · %d frame", c.Red, c.Green, c.Blue, c.Alpha, maxIntEventCmd(c.Duration, 1))
	case "screen_tone":
		return fmt.Sprintf("◆ Tinta schermo: RGB(%d,%d,%d) · grigio %d · %d frame", c.Red, c.Green, c.Blue, c.Gray, maxIntEventCmd(c.Duration, 1))
	case "fade_black":
		return fmt.Sprintf("◆ Dissolvenza al nero · %d frame", maxIntEventCmd(c.Duration, 1))
	case "fade_in":
		return fmt.Sprintf("◆ Ritorno dal nero · %d frame", maxIntEventCmd(c.Duration, 1))
	case "camera_zoom":
		return fmt.Sprintf("◆ Zoom camera: %d%% · %d frame", maxIntEventCmd(c.Zoom, 100), maxIntEventCmd(c.Duration, 1))
	case "camera_reset":
		return "◆ Reset camera sul giocatore"
	}
	return "◆ Comando PLM: " + c.Type
}

func maxIntEventCmd(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func clampEventCommandInt(value, minValue, maxValue, defaultValue int) int {
	if value == 0 && defaultValue != 0 {
		value = defaultValue
	}
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func eventCommandNodesForManaged(c plmManagedEventCommand) []any {
	marker := map[string]any{"_class": "RPG::EventCommand", "code": 108, "indent": 0, "parameters": []any{plmEventCommandMarker(c)}}
	nodes := []any{marker}
	switch c.Type {
	case "hidden_item", "item_ball":
		qty := maxIntEventCmd(c.Quantity, 1)
		script := fmt.Sprintf("pbItemBall(:%s, %d)", strings.ToUpper(strings.TrimSpace(c.Item)), qty)
		if c.OneShot {
			// RPG Maker XP / Essentials native structure:
			// Conditional Branch (Script) -> Self Switch A = ON -> Branch End.
			// The Self Switch only changes if pbItemBall actually succeeded.
			nodes = append(nodes,
				map[string]any{"_class": "RPG::EventCommand", "code": 111, "indent": 0, "parameters": []any{12, script}},
				map[string]any{"_class": "RPG::EventCommand", "code": 123, "indent": 1, "parameters": []any{"A", 0}},
				map[string]any{"_class": "RPG::EventCommand", "code": 412, "indent": 0, "parameters": []any{}},
			)
		} else {
			nodes = append(nodes, map[string]any{"_class": "RPG::EventCommand", "code": 355, "indent": 0, "parameters": []any{script}})
		}
	case "field_move":
		// The PML_CMD marker is the source of truth for the Python runtime.
		// If the project configuration also provides a verified native script,
		// preserve it as an RPG Maker XP Script command for Essentials parity.
		if script := strings.TrimSpace(c.Script); script != "" {
			nodes = append(nodes, map[string]any{"_class": "RPG::EventCommand", "code": 355, "indent": 0, "parameters": []any{script}})
		}
	case "fixed_pokemon":
		level := maxIntEventCmd(c.Level, 1)
		script := fmt.Sprintf("WildBattle.start(:%s, %d)", strings.ToUpper(strings.TrimSpace(c.Species)), level)
		nodes = append(nodes, map[string]any{"_class": "RPG::EventCommand", "code": 355, "indent": 0, "parameters": []any{script}})
		if c.OneShot {
			// Standard Essentials event encounter: the overworld Pokémon disappears
			// after the battle by activating the blank Self Switch A page.
			nodes = append(nodes, map[string]any{"_class": "RPG::EventCommand", "code": 123, "indent": 0, "parameters": []any{"A", 0}})
		}
	case "access_gate":
		// Runtime-only gate. It validates the ticket/item for this execution and
		// then continues with the following event commands. It must NOT create a
		// permanent Self Switch page: event tickets are single-use, not permanent passes.
	case "warp":
		// Native RPG Maker XP Transfer Player command:
		// [direct_or_variable, map_id, x, y, direction, fade].
		nodes = append(nodes, map[string]any{"_class": "RPG::EventCommand", "code": 201, "indent": 0, "parameters": []any{0, c.MapID, c.X, c.Y, c.Direction, 0}})
	case "self_switch":
		ch := strings.ToUpper(strings.TrimSpace(c.SelfSwitch))
		if ch == "A" || ch == "B" || ch == "C" || ch == "D" {
			state := 0
			if strings.EqualFold(strings.TrimSpace(c.State), "OFF") {
				state = 1
			}
			nodes = append(nodes, map[string]any{"_class": "RPG::EventCommand", "code": 123, "indent": 0, "parameters": []any{ch, state}})
		}
	case "unlimited_self_switch":
		// PLM runtime command. It intentionally has no fake RGSS command: named
		// self switches are executed by the Python runtime and persist per
		// Map ID + Event ID + Name.
	case "variable":
		if c.VariableID > 0 {
			op := variableOperationCode(c.Operation)
			nodes = append(nodes, map[string]any{"_class": "RPG::EventCommand", "code": 122, "indent": 0, "parameters": []any{c.VariableID, c.VariableID, op, 0, c.Value}})
		}
	case "choices":
		if len(c.Choices) > 0 {
			choiceAny := make([]any, 0, len(c.Choices))
			for _, choice := range c.Choices {
				choiceAny = append(choiceAny, choice)
			}
			nodes = append(nodes, map[string]any{"_class": "RPG::EventCommand", "code": 102, "indent": 0, "parameters": []any{choiceAny, 0}})
			for i, choice := range c.Choices {
				nodes = append(nodes, map[string]any{"_class": "RPG::EventCommand", "code": 402, "indent": 0, "parameters": []any{i, choice}})
			}
			nodes = append(nodes, map[string]any{"_class": "RPG::EventCommand", "code": 404, "indent": 0, "parameters": []any{}})
		}
	case "common_event":
		if c.CommonEventID > 0 {
			nodes = append(nodes, map[string]any{"_class": "RPG::EventCommand", "code": 117, "indent": 0, "parameters": []any{c.CommonEventID}})
		}
	case "temporary_event":
		nodes = append(nodes, map[string]any{"_class": "RPG::EventCommand", "code": 116, "indent": 0, "parameters": []any{}})
	case "give_item":
		qty := maxIntEventCmd(c.Quantity, 1)
		script := fmt.Sprintf("pbReceiveItem(:%s, %d)", strings.ToUpper(strings.TrimSpace(c.Item)), qty)
		nodes = append(nodes, map[string]any{"_class": "RPG::EventCommand", "code": 355, "indent": 0, "parameters": []any{script}})
	case "wait_frames":
		nodes = append(nodes, map[string]any{"_class": "RPG::EventCommand", "code": 106, "indent": 0, "parameters": []any{maxIntEventCmd((maxIntEventCmd(c.Duration, 1)+1)/2, 1)}})
	case "audio_play":
		code := 250
		switch strings.ToLower(strings.TrimSpace(c.AudioKind)) {
		case "bgm":
			code = 241
		case "bgs":
			code = 245
		case "me":
			code = 249
		case "se":
			code = 250
		}
		audio := map[string]any{"_class": "RPG::AudioFile", "name": strings.TrimSpace(c.AudioName), "volume": clampEventCommandInt(c.Volume, 0, 100, 100), "pitch": clampEventCommandInt(c.Pitch, 50, 150, 100)}
		nodes = append(nodes, map[string]any{"_class": "RPG::EventCommand", "code": code, "indent": 0, "parameters": []any{audio}})
	case "audio_stop":
		code := 241
		switch strings.ToLower(strings.TrimSpace(c.AudioKind)) {
		case "bgs":
			code = 245
		case "me":
			code = 249
		case "se":
			code = 251
		}
		if code == 251 {
			nodes = append(nodes, map[string]any{"_class": "RPG::EventCommand", "code": 251, "indent": 0, "parameters": []any{}})
		} else {
			audio := map[string]any{"_class": "RPG::AudioFile", "name": "", "volume": 100, "pitch": 100}
			nodes = append(nodes, map[string]any{"_class": "RPG::EventCommand", "code": code, "indent": 0, "parameters": []any{audio}})
		}
	case "audio_fade":
		code := 242
		if strings.EqualFold(strings.TrimSpace(c.AudioKind), "bgs") {
			code = 246
		}
		nodes = append(nodes, map[string]any{"_class": "RPG::EventCommand", "code": code, "indent": 0, "parameters": []any{maxIntEventCmd(c.FadeSeconds, 1)}})
	case "camera_pan":
		// Native RPG Maker XP Scroll Map: direction, distance in tiles, speed (1..6).
		tiles := maxIntEventCmd(c.Tiles, 1)
		speed := maxIntEventCmd(c.Speed, 1)
		nodes = append(nodes, map[string]any{"_class": "RPG::EventCommand", "code": 203, "indent": 0, "parameters": []any{c.Direction, tiles, speed}})
		if c.Wait {
			// RMXP stores one tile as 128 scroll units and advances 2^speed
			// units/frame. Command 106 itself doubles its frame parameter.
			frames := (tiles*128 + (1 << speed) - 1) / (1 << speed)
			nodes = append(nodes, map[string]any{"_class": "RPG::EventCommand", "code": 106, "indent": 0, "parameters": []any{maxIntEventCmd((frames+1)/2, 1)}})
		}
	case "camera_shake":
		duration := maxIntEventCmd(c.Duration, 1)
		// Native RMXP Screen Shake uses half-duration units internally.
		nodes = append(nodes, map[string]any{"_class": "RPG::EventCommand", "code": 225, "indent": 0, "parameters": []any{maxIntEventCmd(c.Power, 1), maxIntEventCmd(c.Speed, 1), maxIntEventCmd((duration+1)/2, 1)}})
		if c.Wait {
			nodes = append(nodes, map[string]any{"_class": "RPG::EventCommand", "code": 106, "indent": 0, "parameters": []any{maxIntEventCmd((duration+1)/2, 1)}})
		}
	case "screen_flash":
		duration := maxIntEventCmd(c.Duration, 1)
		script := fmt.Sprintf("$game_screen.start_flash(Color.new(%d, %d, %d, %d), %d)", c.Red, c.Green, c.Blue, c.Alpha, duration)
		nodes = append(nodes, map[string]any{"_class": "RPG::EventCommand", "code": 355, "indent": 0, "parameters": []any{script}})
		if c.Wait {
			nodes = append(nodes, map[string]any{"_class": "RPG::EventCommand", "code": 106, "indent": 0, "parameters": []any{maxIntEventCmd((duration+1)/2, 1)}})
		}
	case "screen_tone":
		duration := maxIntEventCmd(c.Duration, 1)
		script := fmt.Sprintf("$game_screen.start_tone_change(Tone.new(%d, %d, %d, %d), %d)", c.Red, c.Green, c.Blue, c.Gray, duration)
		nodes = append(nodes, map[string]any{"_class": "RPG::EventCommand", "code": 355, "indent": 0, "parameters": []any{script}})
		if c.Wait {
			nodes = append(nodes, map[string]any{"_class": "RPG::EventCommand", "code": 106, "indent": 0, "parameters": []any{maxIntEventCmd((duration+1)/2, 1)}})
		}
	case "fade_black":
		duration := maxIntEventCmd(c.Duration, 1)
		script := fmt.Sprintf("$game_screen.start_tone_change(Tone.new(-255, -255, -255, 0), %d)", duration)
		nodes = append(nodes, map[string]any{"_class": "RPG::EventCommand", "code": 355, "indent": 0, "parameters": []any{script}})
		if c.Wait {
			nodes = append(nodes, map[string]any{"_class": "RPG::EventCommand", "code": 106, "indent": 0, "parameters": []any{maxIntEventCmd((duration+1)/2, 1)}})
		}
	case "fade_in":
		duration := maxIntEventCmd(c.Duration, 1)
		script := fmt.Sprintf("$game_screen.start_tone_change(Tone.new(0, 0, 0, 0), %d)", duration)
		nodes = append(nodes, map[string]any{"_class": "RPG::EventCommand", "code": 355, "indent": 0, "parameters": []any{script}})
		if c.Wait {
			nodes = append(nodes, map[string]any{"_class": "RPG::EventCommand", "code": 106, "indent": 0, "parameters": []any{maxIntEventCmd((duration+1)/2, 1)}})
		}
	case "camera_zoom", "camera_reset":
		// PLM-native camera commands. The marker above is intentionally the
		// only executable representation; default RGSS1 has no map-camera zoom.
	}
	return nodes
}

func ensureCurrentEventPageRaw() *eventPageDraft {
	if eventEditorPageIndex < 0 || eventEditorPageIndex >= len(eventEditorPages) {
		return nil
	}
	p := &eventEditorPages[eventEditorPageIndex]
	if p.Raw == nil {
		p.Raw = map[string]any{"_class": "RPG::Event::Page", "list": []any{map[string]any{"_class": "RPG::EventCommand", "code": 0, "indent": 0, "parameters": []any{}}}}
	}
	return p
}

func appendNodesToCurrentEventPage(nodes ...any) bool {
	p := ensureCurrentEventPageRaw()
	if p == nil {
		return false
	}
	commands := []any{}
	if lv, ok := anyMapValueCI(p.Raw, "list"); ok {
		if arr, ok := lv.([]any); ok {
			commands = append(commands, arr...)
		}
	}
	out := make([]any, 0, len(commands)+len(nodes)+1)
	for _, node := range commands {
		if m, ok := node.(map[string]any); ok && commandMapCode(m) == 0 {
			continue
		}
		out = append(out, node)
	}
	out = append(out, nodes...)
	out = append(out, map[string]any{"_class": "RPG::EventCommand", "code": 0, "indent": 0, "parameters": []any{}})
	setEventMapCI(p.Raw, "list", out)
	refreshEventEditorCommandList()
	return true
}

func prependManagedEventCommand(c plmManagedEventCommand) bool {
	p := ensureCurrentEventPageRaw()
	if p == nil {
		return false
	}
	commands := []any{}
	if lv, ok := anyMapValueCI(p.Raw, "list"); ok {
		if arr, ok := lv.([]any); ok {
			commands = append(commands, arr...)
		}
	}
	out := make([]any, 0, len(commands)+2)
	out = append(out, eventCommandNodesForManaged(c)...)
	for _, node := range commands {
		if m, ok := node.(map[string]any); ok && commandMapCode(m) == 0 {
			continue
		}
		out = append(out, node)
	}
	out = append(out, map[string]any{"_class": "RPG::EventCommand", "code": 0, "indent": 0, "parameters": []any{}})
	setEventMapCI(p.Raw, "list", out)
	refreshEventEditorCommandList()
	return true
}

func appendShowTextEventCommand(text string) bool {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = strings.TrimRight(text, "\n")
	if strings.TrimSpace(text) == "" {
		return false
	}
	lines := strings.Split(text, "\n")
	nodes := make([]any, 0, len(lines))
	for i, line := range lines {
		code := 401
		if i == 0 {
			code = 101
		}
		nodes = append(nodes, map[string]any{"_class": "RPG::EventCommand", "code": code, "indent": 0, "parameters": []any{line}})
	}
	return appendNodesToCurrentEventPage(nodes...)
}

func appendManagedEventCommand(c plmManagedEventCommand) bool {
	return appendNodesToCurrentEventPage(eventCommandNodesForManaged(c)...)
}

func ensureSimpleConsumedBlankPage() {
	// For the common one-page item/legendary event, create the equivalent of
	// RPG Maker's blank Self Switch A page automatically. Existing complex
	// multi-page events are never modified implicitly.
	if len(eventEditorPages) != 1 || eventEditorPageIndex != 0 {
		return
	}
	p := defaultEventPageDraft()
	p.SelfSwitchEnabled = true
	p.SelfSwitch = "A"
	p.Graphic = ""
	p.Trigger = 0
	eventEditorPages = append(eventEditorPages, p)
	refreshEventEditorPageButtons()
}

func addShowTextCommand() {
	text, ok := showEventCommandTextDialog(eventEditorWindow)
	if ok && appendShowTextEventCommand(text) {
		setToolbarStatus("Comando evento aggiunto: Mostra testo.")
	}
}

func addItemEventCommand(hidden bool) {
	mode := eventCommandItemBall
	if hidden {
		mode = eventCommandHiddenItem
	}
	id, qty, oneShot, ok := showEventCommandPickerDialog(eventEditorWindow, mode)
	if !ok {
		return
	}
	id = strings.ToUpper(strings.TrimSpace(id))
	if id == "" {
		return
	}
	c := plmManagedEventCommand{Type: "item_ball", Item: id, Quantity: maxIntEventCmd(qty, 1), OneShot: oneShot}
	if eventEditorPageIndex >= 0 && eventEditorPageIndex < len(eventEditorPages) {
		p := &eventEditorPages[eventEditorPageIndex]
		if hidden {
			c.Type = "hidden_item"
			c.Hidden = true
			p.Graphic = ""
			p.Through = true
			p.Trigger = 0
			name := strings.TrimSpace(getText(eventEditorName))
			if !strings.Contains(strings.ToLower(name), "hiddenitem") {
				if name == "" || strings.HasPrefix(strings.ToUpper(name), "EV") {
					setText(eventEditorName, "HiddenItem")
				} else {
					setText(eventEditorName, "HiddenItem "+name)
				}
			}
		} else if strings.TrimSpace(p.Graphic) == "" {
			if graphic := preferredItemBallGraphic(); graphic != "" {
				p.Graphic = graphic
			}
		}
	}
	if appendManagedEventCommand(c) {
		if oneShot {
			ensureSimpleConsumedBlankPage()
		}
		refreshEventEditorCommandList()
		if hidden {
			setToolbarStatus("Comando evento aggiunto: Oggetto nascosto.")
		} else {
			setToolbarStatus("Comando evento aggiunto: Poké Ball.")
		}
	}
}

func preferredItemBallGraphic() string {
	if len(eventEditorSprites) == 0 {
		eventEditorSprites = characterSpriteNames()
	}
	preferences := []string{"object ball", "item ball", "itemball", "poké ball", "poke ball", "pokeball"}
	for _, wanted := range preferences {
		for _, name := range eventEditorSprites {
			if strings.EqualFold(strings.TrimSpace(name), wanted) {
				return name
			}
		}
	}
	for _, name := range eventEditorSprites {
		l := strings.ToLower(name)
		if strings.Contains(l, "object") && strings.Contains(l, "ball") {
			return name
		}
	}
	return ""
}

func addPokemonBattleEventCommand(legendary bool) {
	if legendary {
		eventCommandDialogFixedPokemonMode = 1
	} else {
		eventCommandDialogFixedPokemonMode = 0
	}
	defer func() { eventCommandDialogFixedPokemonMode = -1 }()

	id, level, oneShot, ok := showEventCommandPickerDialog(eventEditorWindow, eventCommandFixedPokemon)
	if !ok {
		return
	}
	id = strings.ToUpper(strings.TrimSpace(id))
	if id == "" {
		return
	}
	if legendary {
		for _, existing := range currentEditorLegendarySpecies() {
			if strings.EqualFold(existing, id) {
				msgbox("PML Studio - Leggendario duplicato", fmt.Sprintf("%s è già presente nell'evento corrente.\r\n\r\nPLM Studio non permette di configurare due copie dello stesso leggendario.", speciesDisplayName(id)), MB_OK|MB_ICONINFORMATION)
				return
			}
		}
		if usage, duplicate := findLegendaryDuplicate(id, "", true); duplicate {
			msgbox("PML Studio - Leggendario duplicato", legendaryDuplicateMessage(id, usage), MB_OK|MB_ICONINFORMATION)
			return
		}
	}
	if appendManagedEventCommand(plmManagedEventCommand{Type: "fixed_pokemon", Species: id, Level: maxIntEventCmd(level, 1), OneShot: oneShot, Legendary: legendary}) {
		if oneShot {
			ensureSimpleConsumedBlankPage()
		}
		refreshEventEditorCommandList()
		if legendary {
			setToolbarStatus("Comando evento aggiunto: Pokémon leggendario fisso (Wild Battle) unico nel progetto.")
		} else {
			setToolbarStatus("Comando evento aggiunto: Battaglia Pokémon (Wild Battle).")
		}
	}
}

// Compatibility wrapper for old callers/data paths.
func addFixedPokemonEventCommand()          { addPokemonBattleEventCommand(false) }
func addLegendaryFixedPokemonEventCommand() { addPokemonBattleEventCommand(true) }

func eventPageHasManagedWarp(p eventPageDraft) bool {
	if p.Raw == nil {
		return false
	}
	lv, ok := anyMapValueCI(p.Raw, "list")
	if !ok {
		return false
	}
	arr, ok := lv.([]any)
	if !ok {
		return false
	}
	for _, node := range arr {
		m, ok := node.(map[string]any)
		if !ok || commandMapCode(m) != 108 {
			continue
		}
		if cmd, ok := parsePLMEventCommandMarker(commandFirstParam(m)); ok && cmd.Type == "warp" {
			return true
		}
	}
	return false
}

func eventEditorHasManagedWarp() bool {
	for _, p := range eventEditorPages {
		if eventPageHasManagedWarp(p) {
			return true
		}
	}
	return false
}

func containsManagedWarpMarker(v any) bool {
	switch t := v.(type) {
	case map[string]any:
		if commandMapCode(t) == 108 {
			if cmd, ok := parsePLMEventCommandMarker(commandFirstParam(t)); ok && cmd.Type == "warp" {
				return true
			}
		}
		for _, child := range t {
			if containsManagedWarpMarker(child) {
				return true
			}
		}
	case []any:
		for _, child := range t {
			if containsManagedWarpMarker(child) {
				return true
			}
		}
	}
	return false
}

func editorEventHasManagedWarp(e EditorEvent) bool {
	if len(e.NativeRaw) == 0 {
		return false
	}
	v, err := decodeJSONAny(e.NativeRaw)
	if err != nil {
		return false
	}
	return containsManagedWarpMarker(v)
}

func ensureWarpSourceMovementPermission(x, y int) error {
	if currentMap == nil || currentMapDoc == nil {
		return fmt.Errorf("nessuna mappa caricata")
	}
	if x < 0 || y < 0 || x >= currentMapW || y >= currentMapH {
		return fmt.Errorf("casella sorgente warp fuori dalla mappa: %d,%d", x, y)
	}
	key := cellKey(x, y)
	old := strings.ToUpper(strings.TrimSpace(resolvedMovementPermission(x, y)))
	base := strings.ToUpper(strings.TrimSpace(derivedPermissions[key]))
	if base == "" {
		base = "C"
	}
	if base == "00" {
		delete(permissions, key)
	} else {
		permissions[key] = "00"
	}
	if old != "00" {
		permissionDataDirty = true
	}
	if err := syncPermissionDataIntoCurrentMap(); err != nil {
		return err
	}
	if err := persistPermissionDataToRealMap(); err != nil {
		return err
	}
	invalidate(hwndCanvas)
	return nil
}

func addWarpEventCommand() {
	mapID, x, y, dir, ok := showEventWarpDialog(eventEditorWindow)
	if !ok || mapID <= 0 {
		return
	}
	if eventEditorPageIndex < 0 || eventEditorPageIndex >= len(eventEditorPages) {
		return
	}
	// A warp door/stair must be physically walkable. The source tile gets the
	// Movement Permissions transition code 00 when the event is committed.
	// Here we configure the page itself as a player-touch, non-blocking event.
	p := &eventEditorPages[eventEditorPageIndex]
	p.Trigger = 1
	p.Through = true
	p.AlwaysTop = false
	if appendManagedEventCommand(plmManagedEventCommand{Type: "warp", MapID: mapID, X: x, Y: y, Direction: dir}) {
		loadEventEditorPageToControls()
		setToolbarStatus("Warp aggiunto: l'evento sarà calpestabile e la casella sorgente userà il movimento 00 (transizione).")
	}
}

func loadEventItemCatalog() []eventCommandChoice {
	if strings.TrimSpace(currentProject) == "" {
		return nil
	}
	projectKey := filepath.Clean(currentProject)
	if strings.EqualFold(eventItemCatalogProject, projectKey) && eventItemCatalogCache != nil {
		return eventItemCatalogCache
	}
	byID := map[string]eventCommandChoice{}
	for _, rel := range []string{
		filepath.Join("PBS", "items.txt"), filepath.Join("pbs", "items.txt"),
		filepath.Join("converted", "PBS", "items.txt"), filepath.Join("converted", "pbs", "items.txt"),
	} {
		parseEventItemsPBS(filepath.Join(currentProject, rel), byID)
	}
	for _, rel := range []string{
		filepath.Join("converted", "data", "items.json"), filepath.Join("converted", "items.json"), filepath.Join("data", "items.json"),
	} {
		parseEventItemsJSON(filepath.Join(currentProject, rel), byID)
	}
	out := make([]eventCommandChoice, 0, len(byID))
	for _, v := range byID {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool {
		ni, nj := strings.ToLower(out[i].Name), strings.ToLower(out[j].Name)
		if ni == nj {
			return out[i].ID < out[j].ID
		}
		return ni < nj
	})
	eventItemCatalogProject = projectKey
	eventItemCatalogCache = out
	return out
}

func parseEventItemsPBS(path string, dst map[string]eventCommandChoice) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	section, name := "", ""
	flush := func() {
		id := strings.ToUpper(strings.TrimSpace(section))
		if id == "" {
			return
		}
		n := strings.TrimSpace(name)
		if n == "" {
			n = id
		}
		dst[id] = eventCommandChoice{ID: id, Name: n}
	}
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := strings.TrimSpace(stripPBSComment(s.Text()))
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			flush()
			section = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "["), "]"))
			name = ""
			continue
		}
		if section == "" {
			continue
		}
		if p := strings.Index(line, "="); p >= 0 {
			key := strings.TrimSpace(line[:p])
			value := strings.TrimSpace(line[p+1:])
			if strings.EqualFold(key, "Name") {
				name = value
			}
		}
	}
	flush()
}

func parseEventItemsJSON(path string, dst map[string]eventCommandChoice) {
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	v, err := decodeJSONAny(b)
	if err != nil {
		return
	}
	var walk func(any, string)
	walk = func(node any, keyHint string) {
		switch t := node.(type) {
		case []any:
			for _, x := range t {
				walk(x, "")
			}
		case map[string]any:
			id := strings.TrimSpace(jsonStringCI(t, "id", "internal_name", "internalname", "item"))
			if id == "" && keyHint != "" {
				id = keyHint
			}
			name := strings.TrimSpace(jsonStringCI(t, "name", "display_name", "displayname"))
			if id != "" && (name != "" || len(t) > 1) {
				id = strings.ToUpper(id)
				if name == "" {
					name = id
				}
				dst[id] = eventCommandChoice{ID: id, Name: name}
			}
			for k, x := range t {
				if _, ok := x.(map[string]any); ok {
					walk(x, k)
				}
				if _, ok := x.([]any); ok {
					walk(x, "")
				}
			}
		}
	}
	walk(v, "")
}

func eventItemDisplayName(id string) string {
	id = strings.ToUpper(strings.TrimSpace(id))
	if id == "" {
		return "?"
	}
	for _, v := range loadEventItemCatalog() {
		if strings.EqualFold(v.ID, id) {
			if strings.TrimSpace(v.Name) != "" {
				return v.Name + " [" + v.ID + "]"
			}
			return v.ID
		}
	}
	return id
}

func loadEventSpeciesChoices() []eventCommandChoice {
	src := loadEncounterSpeciesCatalog()
	out := make([]eventCommandChoice, 0, len(src))
	for _, sp := range src {
		id := strings.ToUpper(strings.TrimSpace(sp.ID))
		if id == "" {
			continue
		}
		name := strings.TrimSpace(sp.Name)
		if name == "" {
			name = id
		}
		out = append(out, eventCommandChoice{ID: id, Name: name})
	}
	sort.Slice(out, func(i, j int) bool {
		ni, nj := strings.ToLower(out[i].Name), strings.ToLower(out[j].Name)
		if ni == nj {
			return out[i].ID < out[j].ID
		}
		return ni < nj
	})
	return out
}

func eventCommandDialogSelectedIndex() int {
	if eventCommandDialogList == 0 {
		return -1
	}
	r, _, _ := pSendMessageW.Call(uintptr(eventCommandDialogList), LB_GETCURSEL, 0, 0)
	if int32(r) < 0 {
		return -1
	}
	return int(r)
}

func refreshEventCommandDialogFilter() {
	if eventCommandDialogList == 0 {
		return
	}
	q := strings.ToLower(strings.TrimSpace(getText(eventCommandDialogSearch)))
	eventCommandDialogFiltered = eventCommandDialogFiltered[:0]
	clearList(eventCommandDialogList)
	for _, c := range eventCommandDialogCatalog {
		hay := strings.ToLower(c.ID + " " + c.Name)
		if q != "" && !strings.Contains(hay, q) {
			continue
		}
		eventCommandDialogFiltered = append(eventCommandDialogFiltered, c)
		addList(eventCommandDialogList, fmt.Sprintf("%s   [%s]", c.Name, c.ID))
	}
}

func selectEventCommandDialogChoice() {
	idx := eventCommandDialogSelectedIndex()
	if idx < 0 || idx >= len(eventCommandDialogFiltered) {
		return
	}
	setText(eventCommandDialogID, eventCommandDialogFiltered[idx].ID)
}

func commitEventCommandDialog() bool {
	if eventCommandDialogModeValue == eventCommandText {
		text := getText(eventCommandDialogText)
		if strings.TrimSpace(text) == "" {
			msgbox("PML Studio - Testo evento", "Scrivi almeno una riga di testo.", MB_OK|MB_ICONINFORMATION)
			return false
		}
		eventCommandDialogResultTxt = text
		eventCommandDialogAccepted = true
		return true
	}
	id := strings.ToUpper(strings.TrimSpace(getText(eventCommandDialogID)))
	if id == "" {
		msgbox("PML Studio - Comando evento", "Seleziona una voce dal progetto oppure inserisci l'ID interno.", MB_OK|MB_ICONINFORMATION)
		return false
	}
	value := intField(eventCommandDialogValue, 1)
	if value < 1 {
		value = 1
	}
	if eventCommandDialogModeValue == eventCommandFixedPokemon && value > 100 {
		value = 100
	}
	eventCommandDialogResultID = id
	eventCommandDialogResultVal = value
	eventCommandDialogResultOne = checked(eventCommandDialogOneShot)
	if eventCommandDialogModeValue == eventCommandFixedPokemon {
		switch eventCommandDialogFixedPokemonMode {
		case 0:
			eventCommandDialogResultLegendary = false
		case 1:
			eventCommandDialogResultLegendary = true
		default:
			eventCommandDialogResultLegendary = eventCommandDialogLegendary != 0 && checked(eventCommandDialogLegendary)
		}
	} else {
		eventCommandDialogResultLegendary = false
	}
	eventCommandDialogAccepted = true
	return true
}

func eventCommandDialogWndProcFn(hwnd syscall.Handle, msg uint32, w, l uintptr) uintptr {
	switch msg {
	case WM_COMMAND:
		id := int(loword(w))
		notify := int(hiword(w))
		if id == idEventCmdSearch && notify == 0x0300 { // EN_CHANGE
			refreshEventCommandDialogFilter()
			return 0
		}
		if id == idEventCmdList && notify == LBN_SELCHANGE {
			selectEventCommandDialogChoice()
			return 0
		}
		if id == idEventCmdList && notify == LBN_DBLCLK {
			selectEventCommandDialogChoice()
			if commitEventCommandDialog() {
				pDestroyWindow.Call(uintptr(hwnd))
			}
			return 0
		}
		switch id {
		case idEventCmdOK:
			if commitEventCommandDialog() {
				pDestroyWindow.Call(uintptr(hwnd))
			}
			return 0
		case idEventCmdCancel:
			pDestroyWindow.Call(uintptr(hwnd))
			return 0
		}
	case WM_CLOSE:
		pDestroyWindow.Call(uintptr(hwnd))
		return 0
	case WM_DESTROY:
		eventCommandDialogOpen = false
		eventCommandDialogWindow = 0
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(uintptr(hwnd), uintptr(msg), w, l)
	return r
}

var eventCommandDialogWndProc = syscall.NewCallback(eventCommandDialogWndProcFn)

func ensureEventCommandDialogClass() error {
	if eventCommandDialogRegistered {
		return nil
	}
	h, _, _ := pGetModuleHandleW.Call(0)
	hInst := syscall.Handle(h)
	cursor, _, _ := pLoadCursorW.Call(0, IDC_ARROW)
	brush, _, _ := pCreateSolidBrush.Call(rgb(244, 244, 244))
	wc := WNDCLASSEX{cbSize: uint32(unsafe.Sizeof(WNDCLASSEX{})), lpfnWndProc: eventCommandDialogWndProc, hInstance: hInst, hCursor: syscall.Handle(cursor), hbrBackground: syscall.Handle(brush), lpszClassName: wstr(eventCommandDialogClassName)}
	r, _, err := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	if r == 0 {
		return fmt.Errorf("registrazione dialogo comando evento fallita: %v", err)
	}
	eventCommandDialogRegistered = true
	return nil
}

func runEventCommandDialog(owner syscall.Handle, title string, ww, wh int32, create func(hInst syscall.Handle)) bool {
	if eventCommandDialogOpen {
		return false
	}
	if err := ensureEventCommandDialogClass(); err != nil {
		msgbox("PML Studio - Comando evento", err.Error(), MB_OK|MB_ICONERROR)
		return false
	}
	x, y := centerOwnedWindow(ww, wh)
	h, _, _ := pGetModuleHandleW.Call(0)
	hInst := syscall.Handle(h)
	eventCommandDialogAccepted = false
	eventCommandDialogResultID = ""
	eventCommandDialogResultVal = 0
	eventCommandDialogResultOne = false
	eventCommandDialogResultTxt = ""
	eventCommandDialogResultLegendary = false
	eventCommandDialogOwner = owner
	eventCommandDialogOpen = true
	eventCommandDialogWindow = createWindow(eventCommandDialogClassName, title, WS_OVERLAPPEDWINDOW|WS_VISIBLE|WS_CLIPCHILDREN, x, y, ww, wh, owner, 0, hInst)
	if eventCommandDialogWindow == 0 {
		eventCommandDialogOpen = false
		return false
	}
	setWindowIcon(eventCommandDialogWindow)
	create(hInst)
	pEnableWindow.Call(uintptr(owner), 0)
	pShowWindow.Call(uintptr(eventCommandDialogWindow), SW_SHOW)
	pUpdateWindow.Call(uintptr(eventCommandDialogWindow))
	var m MSG
	repostQuit := false
	for eventCommandDialogOpen {
		r, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) == -1 {
			break
		}
		if r == 0 {
			repostQuit = true
			break
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		pDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
	pEnableWindow.Call(uintptr(owner), 1)
	pSetFocus.Call(uintptr(owner))
	if repostQuit {
		pPostQuitMessage.Call(0)
	}
	return eventCommandDialogAccepted
}

func showEventCommandTextDialog(owner syscall.Handle) (string, bool) {
	eventCommandDialogModeValue = eventCommandText
	ok := runEventCommandDialog(owner, "Mostra testo", 760, 520, func(hInst syscall.Handle) {
		createWindow("STATIC", "Testo mostrato al giocatore:", WS_CHILD|WS_VISIBLE, 20, 20, 690, 26, eventCommandDialogWindow, 7510, hInst)
		eventCommandDialogText = createWindow("EDIT", "", WS_CHILD|WS_VISIBLE|WS_BORDER|WS_VSCROLL|WS_TABSTOP|ES_MULTILINE|ES_AUTOVSCROLL|ES_WANTRETURN, 20, 52, 705, 350, eventCommandDialogWindow, idEventCmdText, hInst)
		createWindow("STATIC", "Puoi scrivere più righe. PLM Studio le salva come comandi Testo nativi della pagina evento.", WS_CHILD|WS_VISIBLE, 20, 412, 700, 28, eventCommandDialogWindow, 7511, hInst)
		createWindow("BUTTON", "Conferma", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 505, 445, 105, 36, eventCommandDialogWindow, idEventCmdOK, hInst)
		createWindow("BUTTON", "Annulla", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 620, 445, 105, 36, eventCommandDialogWindow, idEventCmdCancel, hInst)
	})
	return eventCommandDialogResultTxt, ok
}

func showEventCommandPickerDialog(owner syscall.Handle, mode eventCommandDialogMode) (string, int, bool, bool) {
	eventCommandDialogModeValue = mode
	title := "Seleziona oggetto"
	valueLabel := "Quantità:"
	eventCommandDialogCatalog = loadEventItemCatalog()
	if mode == eventCommandHiddenItem {
		title = "Oggetto nascosto"
	}
	if mode == eventCommandItemBall {
		title = "Poké Ball a terra"
	}
	if mode == eventCommandFixedPokemon {
		switch eventCommandDialogFixedPokemonMode {
		case 0:
			title = "Battaglia Pokémon (Wild Battle)"
		case 1:
			title = "Pokémon leggendario fisso"
		default:
			title = "Battaglia Pokémon / Leggendario"
		}
		valueLabel = "Livello:"
		eventCommandDialogCatalog = loadEventSpeciesChoices()
	}
	if mode == eventCommandGiveItem {
		title = "Dai oggetto"
		valueLabel = "Quantità:"
	}
	eventCommandDialogFiltered = nil
	ok := runEventCommandDialog(owner, title, 900, 675, func(hInst syscall.Handle) {
		createWindow("STATIC", "Cerca nel progetto:", WS_CHILD|WS_VISIBLE, 18, 18, 370, 24, eventCommandDialogWindow, 7520, hInst)
		eventCommandDialogSearch = createWindow("EDIT", "", WS_CHILD|WS_VISIBLE|WS_BORDER|WS_TABSTOP, 18, 45, 400, 32, eventCommandDialogWindow, idEventCmdSearch, hInst)
		eventCommandDialogList = createWindow("LISTBOX", "", WS_CHILD|WS_VISIBLE|WS_BORDER|WS_VSCROLL|WS_TABSTOP|LBS_NOTIFY|LBS_NOINTEGRALHEIGHT, 18, 86, 400, 445, eventCommandDialogWindow, idEventCmdList, hInst)
		createWindow("STATIC", "ID interno:", WS_CHILD|WS_VISIBLE, 455, 88, 380, 24, eventCommandDialogWindow, 7521, hInst)
		eventCommandDialogID = createWindow("EDIT", "", WS_CHILD|WS_VISIBLE|WS_BORDER|WS_TABSTOP, 455, 116, 385, 32, eventCommandDialogWindow, idEventCmdID, hInst)
		createWindow("STATIC", valueLabel, WS_CHILD|WS_VISIBLE, 455, 175, 230, 24, eventCommandDialogWindow, 7522, hInst)
		defaultValue := "1"
		if mode == eventCommandFixedPokemon {
			defaultValue = "50"
		}
		eventCommandDialogValue = createWindow("EDIT", defaultValue, WS_CHILD|WS_VISIBLE|WS_BORDER|WS_TABSTOP, 455, 203, 150, 32, eventCommandDialogWindow, idEventCmdValue, hInst)
		oneLabel := "Raccoglibile una sola volta (Self Switch A)"
		if mode == eventCommandFixedPokemon {
			oneLabel = "Evento una sola volta (Self Switch A)"
		}
		eventCommandDialogLegendary = 0
		if mode == eventCommandGiveItem {
			eventCommandDialogOneShot = 0
			createWindow("STATIC", "Il comando usa il messaggio standard di ricezione oggetto.", WS_CHILD|WS_VISIBLE, 455, 264, 385, 32, eventCommandDialogWindow, 7524, hInst)
		} else {
			eventCommandDialogOneShot = createWindow("BUTTON", oneLabel, WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_AUTOCHECKBOX, 455, 260, 385, 32, eventCommandDialogWindow, idEventCmdOneShot, hInst)
			pSendMessageW.Call(uintptr(eventCommandDialogOneShot), BM_SETCHECK, BST_CHECKED, 0)
		}
		if mode == eventCommandFixedPokemon && eventCommandDialogFixedPokemonMode < 0 {
			eventCommandDialogLegendary = createWindow("BUTTON", "Leggendario / unico nel progetto", WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_AUTOCHECKBOX, 455, 298, 385, 32, eventCommandDialogWindow, idEventCmdLegendary, hInst)
		}
		info := "Seleziona un oggetto reale del progetto oppure inserisci manualmente il suo ID interno."
		if mode == eventCommandHiddenItem {
			info = "L'oggetto nascosto non usa una grafica. PLM lo salva come comando ad alto livello e genera anche la compatibilità Essentials."
		}
		if mode == eventCommandItemBall {
			info = "La Poké Ball usa l'oggetto selezionato. La grafica dell'evento resta configurabile nel riquadro Grafica."
		}
		if mode == eventCommandFixedPokemon {
			switch eventCommandDialogFixedPokemonMode {
			case 0:
				info = "Crea una battaglia Pokémon con WildBattle.start. Se devi creare un leggendario fisso usa il comando dedicato, che applica anche il controllo anti-duplicato globale."
			case 1:
				info = "Crea un leggendario fisso con WildBattle.start. PLM controlla eventi fissi e aree random: la stessa specie leggendaria non può essere registrata due volte nel progetto."
			default:
				info = "Scegli specie e livello. Se è un leggendario, attiva ‘Leggendario / unico nel progetto’: PLM controllerà tutte le mappe e le aree random e bloccherà ogni duplicato."
			}
		}
		if mode == eventCommandGiveItem {
			info = "Aggiunge direttamente l'oggetto selezionato all'inventario. È un comando generico, separato dalla Poké Ball a terra."
		}
		createWindow("STATIC", info, WS_CHILD|WS_VISIBLE, 455, 350, 385, 120, eventCommandDialogWindow, 7523, hInst)
		createWindow("BUTTON", "Conferma", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 610, 565, 105, 36, eventCommandDialogWindow, idEventCmdOK, hInst)
		createWindow("BUTTON", "Annulla", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 725, 565, 105, 36, eventCommandDialogWindow, idEventCmdCancel, hInst)
		refreshEventCommandDialogFilter()
	})
	return eventCommandDialogResultID, eventCommandDialogResultVal, eventCommandDialogResultOne, ok
}

// -----------------------------------------------------------------------------
// Warp / Transfer Player command
// -----------------------------------------------------------------------------

const (
	eventWarpDialogClassName = "PLMStudioEventWarpDialog01"

	idEventWarpMap       = 7560
	idEventWarpX         = 7561
	idEventWarpY         = 7562
	idEventWarpDirection = 7563
	idEventWarpInfo      = 7564
	idEventWarpOK        = 7565
	idEventWarpCancel    = 7566
)

var (
	eventWarpDialogRegistered bool
	eventWarpDialogOpen       bool
	eventWarpDialogWindow     syscall.Handle
	eventWarpDialogMap        syscall.Handle
	eventWarpDialogX          syscall.Handle
	eventWarpDialogY          syscall.Handle
	eventWarpDialogDirection  syscall.Handle
	eventWarpDialogInfo       syscall.Handle
	eventWarpDialogMapIDs     []int
	eventWarpDialogAccepted   bool
	eventWarpDialogResultMap  int
	eventWarpDialogResultX    int
	eventWarpDialogResultY    int
	eventWarpDialogResultDir  int
)

func eventWarpMapName(id int) string {
	for _, m := range maps {
		if m.ID == id {
			name := strings.TrimSpace(m.Name)
			if name != "" {
				return name
			}
			break
		}
	}
	return "Mappa"
}

func selectedEventWarpMap() (MapEntry, bool) {
	idx := comboSel(eventWarpDialogMap)
	if idx < 0 || idx >= len(eventWarpDialogMapIDs) {
		return MapEntry{}, false
	}
	id := eventWarpDialogMapIDs[idx]
	for _, m := range maps {
		if m.ID == id {
			return m, true
		}
	}
	return MapEntry{}, false
}

func refreshEventWarpInfo() {
	m, ok := selectedEventWarpMap()
	if !ok {
		setText(eventWarpDialogInfo, "Seleziona una mappa di destinazione.")
		return
	}
	w, h := loadDims(m)
	if w <= 0 || h <= 0 {
		w, h = 1, 1
	}
	region := "NON ASSEGNATA"
	if strings.TrimSpace(m.RegionID) != "" {
		region = strings.ToUpper(strings.TrimSpace(m.RegionID))
	}
	setText(eventWarpDialogInfo, fmt.Sprintf("Destinazione: Map%03d - %s [%s]\r\nCoordinate valide: X 0..%d · Y 0..%d\r\nLa casella sorgente verrà impostata automaticamente su Movimento 00 (transizione/porta).", m.ID, m.Name, region, w-1, h-1))
}

func commitEventWarpDialog() bool {
	m, ok := selectedEventWarpMap()
	if !ok {
		msgbox("PML Studio - Warp", "Seleziona una mappa di destinazione.", MB_OK|MB_ICONINFORMATION)
		return false
	}
	x := intField(eventWarpDialogX, -1)
	y := intField(eventWarpDialogY, -1)
	w, h := loadDims(m)
	if w <= 0 || h <= 0 {
		w, h = 1, 1
	}
	if x < 0 || y < 0 || x >= w || y >= h {
		msgbox("PML Studio - Warp", fmt.Sprintf("Coordinate fuori dalla mappa.\r\n\r\nMap%03d misura %dx%d.\r\nX: 0..%d\r\nY: 0..%d", m.ID, w, h, w-1, h-1), MB_OK|MB_ICONERROR)
		return false
	}
	dir := directionFromComboIndex(comboSel(eventWarpDialogDirection))
	eventWarpDialogResultMap = m.ID
	eventWarpDialogResultX = x
	eventWarpDialogResultY = y
	eventWarpDialogResultDir = dir
	eventWarpDialogAccepted = true
	return true
}

func eventWarpDialogWndProcFn(hwnd syscall.Handle, msg uint32, w, l uintptr) uintptr {
	switch msg {
	case WM_COMMAND:
		id := int(loword(w))
		notify := int(hiword(w))
		if id == idEventWarpMap && notify == 1 { // CBN_SELCHANGE
			refreshEventWarpInfo()
			return 0
		}
		switch id {
		case idEventWarpOK:
			if commitEventWarpDialog() {
				pDestroyWindow.Call(uintptr(hwnd))
			}
			return 0
		case idEventWarpCancel:
			pDestroyWindow.Call(uintptr(hwnd))
			return 0
		}
	case WM_CLOSE:
		pDestroyWindow.Call(uintptr(hwnd))
		return 0
	case WM_DESTROY:
		eventWarpDialogOpen = false
		eventWarpDialogWindow = 0
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(uintptr(hwnd), uintptr(msg), w, l)
	return r
}

var eventWarpDialogWndProc = syscall.NewCallback(eventWarpDialogWndProcFn)

func ensureEventWarpDialogClass() error {
	if eventWarpDialogRegistered {
		return nil
	}
	h, _, _ := pGetModuleHandleW.Call(0)
	hInst := syscall.Handle(h)
	cursor, _, _ := pLoadCursorW.Call(0, IDC_ARROW)
	brush, _, _ := pCreateSolidBrush.Call(rgb(244, 244, 244))
	wc := WNDCLASSEX{
		cbSize:        uint32(unsafe.Sizeof(WNDCLASSEX{})),
		lpfnWndProc:   eventWarpDialogWndProc,
		hInstance:     hInst,
		hCursor:       syscall.Handle(cursor),
		hbrBackground: syscall.Handle(brush),
		lpszClassName: wstr(eventWarpDialogClassName),
	}
	r, _, err := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	if r == 0 {
		return fmt.Errorf("registrazione finestra Warp fallita: %v", err)
	}
	eventWarpDialogRegistered = true
	return nil
}

func showEventWarpDialog(owner syscall.Handle) (mapID, x, y, dir int, ok bool) {
	if eventWarpDialogOpen {
		return 0, 0, 0, 0, false
	}
	if len(maps) == 0 {
		msgbox("PML Studio - Warp", "Nel progetto non risultano mappe disponibili.", MB_OK|MB_ICONERROR)
		return 0, 0, 0, 0, false
	}
	if err := ensureEventWarpDialogClass(); err != nil {
		msgbox("PML Studio - Warp", err.Error(), MB_OK|MB_ICONERROR)
		return 0, 0, 0, 0, false
	}

	const ww int32 = 690
	const wh int32 = 430
	wx, wy := centerOwnedWindow(ww, wh)
	h, _, _ := pGetModuleHandleW.Call(0)
	hInst := syscall.Handle(h)

	eventWarpDialogAccepted = false
	eventWarpDialogResultMap = 0
	eventWarpDialogResultX = 0
	eventWarpDialogResultY = 0
	eventWarpDialogResultDir = 0
	eventWarpDialogOpen = true
	eventWarpDialogWindow = createWindow(eventWarpDialogClassName, "Warp / Trasferisci giocatore", WS_OVERLAPPEDWINDOW|WS_VISIBLE|WS_CLIPCHILDREN, wx, wy, ww, wh, owner, 0, hInst)
	if eventWarpDialogWindow == 0 {
		eventWarpDialogOpen = false
		return 0, 0, 0, 0, false
	}
	setWindowIcon(eventWarpDialogWindow)

	createWindow("STATIC", "Mappa di destinazione:", WS_CHILD|WS_VISIBLE, 28, 28, 185, 26, eventWarpDialogWindow, 7567, hInst)
	eventWarpDialogMap = createWindow("COMBOBOX", "", WS_CHILD|WS_VISIBLE|WS_TABSTOP|CBS_DROPDOWNLIST|WS_VSCROLL, 215, 23, 425, 260, eventWarpDialogWindow, idEventWarpMap, hInst)
	eventWarpDialogMapIDs = eventWarpDialogMapIDs[:0]
	entries := append([]MapEntry(nil), maps...)
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })
	selectIdx := 0
	for _, m := range entries {
		label := fmt.Sprintf("%03d - %s", m.ID, m.Name)
		if strings.TrimSpace(m.RegionID) != "" {
			label += " [" + strings.ToUpper(strings.TrimSpace(m.RegionID)) + "]"
		}
		comboAdd(eventWarpDialogMap, label)
		eventWarpDialogMapIDs = append(eventWarpDialogMapIDs, m.ID)
		if currentMap != nil && m.ID == currentMap.ID {
			selectIdx = len(eventWarpDialogMapIDs) - 1
		}
	}
	comboSelectIndex(eventWarpDialogMap, selectIdx)

	createWindow("BUTTON", "Coordinate destinazione", WS_CHILD|WS_VISIBLE|BS_GROUPBOX, 28, 78, 612, 100, eventWarpDialogWindow, 7568, hInst)
	createWindow("STATIC", "X:", WS_CHILD|WS_VISIBLE, 48, 115, 25, 25, eventWarpDialogWindow, 7569, hInst)
	eventWarpDialogX = createWindow("EDIT", "0", WS_CHILD|WS_VISIBLE|WS_BORDER|WS_TABSTOP, 75, 108, 90, 32, eventWarpDialogWindow, idEventWarpX, hInst)
	createWindow("STATIC", "Y:", WS_CHILD|WS_VISIBLE, 190, 115, 25, 25, eventWarpDialogWindow, 7570, hInst)
	eventWarpDialogY = createWindow("EDIT", "0", WS_CHILD|WS_VISIBLE|WS_BORDER|WS_TABSTOP, 217, 108, 90, 32, eventWarpDialogWindow, idEventWarpY, hInst)
	createWindow("STATIC", "Direzione:", WS_CHILD|WS_VISIBLE, 340, 115, 82, 25, eventWarpDialogWindow, 7571, hInst)
	eventWarpDialogDirection = createWindow("COMBOBOX", "", WS_CHILD|WS_VISIBLE|WS_TABSTOP|CBS_DROPDOWNLIST, 425, 108, 190, 190, eventWarpDialogWindow, idEventWarpDirection, hInst)
	setComboFromStrings(eventWarpDialogDirection, []string{"Mantieni", "Giù", "Sinistra", "Destra", "Su"}, 0)

	createWindow("BUTTON", "Collegamento automatico", WS_CHILD|WS_VISIBLE|BS_GROUPBOX, 28, 194, 612, 112, eventWarpDialogWindow, 7572, hInst)
	eventWarpDialogInfo = createWindow("STATIC", "", WS_CHILD|WS_VISIBLE, 46, 220, 575, 78, eventWarpDialogWindow, idEventWarpInfo, hInst)
	refreshEventWarpInfo()

	createWindow("BUTTON", "Crea Warp", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 405, 326, 110, 38, eventWarpDialogWindow, idEventWarpOK, hInst)
	createWindow("BUTTON", "Annulla", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 530, 326, 110, 38, eventWarpDialogWindow, idEventWarpCancel, hInst)

	pEnableWindow.Call(uintptr(owner), 0)
	pShowWindow.Call(uintptr(eventWarpDialogWindow), SW_SHOW)
	pUpdateWindow.Call(uintptr(eventWarpDialogWindow))
	var m MSG
	repostQuit := false
	for eventWarpDialogOpen {
		r, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) == -1 {
			break
		}
		if r == 0 {
			repostQuit = true
			break
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		pDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
	pEnableWindow.Call(uintptr(owner), 1)
	pSetFocus.Call(uintptr(owner))
	if repostQuit {
		pPostQuitMessage.Call(0)
	}
	return eventWarpDialogResultMap, eventWarpDialogResultX, eventWarpDialogResultY, eventWarpDialogResultDir, eventWarpDialogAccepted
}

// -----------------------------------------------------------------------------
// Event command palette
// -----------------------------------------------------------------------------

const (
	eventCommandPaletteClassName = "PLMStudioEventCommandPalette03"

	idEventPaletteButtonBase  = 7580
	idEventPaletteBack        = 7620
	idEventPaletteSaveCompile = 7621
	idEventPaletteTest        = 7622
	idEventPalettePrevPage    = 7623
	idEventPaletteNextPage    = 7624
)

type eventCommandPaletteChoice int

const (
	eventPaletteNone eventCommandPaletteChoice = iota
	eventPaletteShowText
	eventPaletteMugshot
	eventPaletteHiddenItem
	eventPaletteItemBall
	eventPaletteFieldMove
	eventPaletteFlyPOI
	eventPaletteFixedPokemon
	eventPaletteLegendaryFixed
	eventPaletteLegendaryArea
	eventPaletteTrainer
	eventPaletteRebattle
	eventPaletteWarp
	eventPaletteSelfSwitch
	eventPaletteUnlimitedSelfSwitch
	eventPaletteVariable
	eventPaletteChoices
	eventPaletteChangeOverworld
	eventPaletteCommonEvent
	eventPaletteAutorun
	eventPaletteScriptFromText
	eventPaletteAdvancedScript
	eventPaletteTemporaryEvent
	eventPaletteGiveItem
	eventPalettePlayerFollowsNPC
	eventPaletteNPCFollowsPlayer
	eventPaletteGlobalEvents
	eventPaletteCameraEffects
	eventPalettePlayerMovement
	eventPaletteNPCMovement
	eventPaletteScreenGraphics
	eventPalettePokemonEventTools
	eventPaletteCutscene
	eventPaletteAudio
	eventPaletteMapInteractions
	eventPaletteProgressConditions
	eventPaletteEventLogic
	eventPaletteEmote
	eventPalettePuzzles
	eventPaletteNarrative
	eventPaletteTranslateProject
	eventPaletteSaveCompile
	eventPaletteTestEvent
	eventPalettePrevSection
	eventPaletteNextSection
)

type eventCommandPaletteEntry struct {
	Title       string
	Description string
	Choice      eventCommandPaletteChoice
}

type eventCommandPaletteSection struct {
	Title       string
	Description string
	Entries     []eventCommandPaletteEntry
}

var (
	eventCommandPaletteRegistered   bool
	eventCommandPaletteOpen         bool
	eventCommandPaletteWindow       syscall.Handle
	eventCommandPaletteAccepted     bool
	eventCommandPaletteResult       eventCommandPaletteChoice
	eventCommandPaletteSectionIndex int
)

// Ogni comando compare in una sola sezione. Questo evita che due pulsanti
// differenti finiscano per pilotare lo stesso handler e rende la palette
// controllabile automaticamente prima di essere mostrata.
var eventCommandPaletteSections = []eventCommandPaletteSection{
	{
		Title:       "Dialoghi / Logica",
		Description: "Testi, scelte, Self Switch, variabili e logica condizionale degli eventi.",
		Entries: []eventCommandPaletteEntry{
			{Title: "Mostra testo", Description: "Mostra un dialogo al giocatore.", Choice: eventPaletteShowText},
			{Title: "Mugshot", Description: "Mostra/nasconde un ritratto durante dialoghi e scene.", Choice: eventPaletteMugshot},
			{Title: "Scelte", Description: "Crea una scelta con più opzioni.", Choice: eventPaletteChoices},
			{Title: "Self Switch A/B/C/D", Description: "Self Switch classiche locali dell'evento.", Choice: eventPaletteSelfSwitch},
			{Title: "Unlimited Self Switch", Description: "Self Switch locali nominate e illimitate.", Choice: eventPaletteUnlimitedSelfSwitch},
			{Title: "Variabile", Description: "Seleziona o crea una variabile reale del progetto.", Choice: eventPaletteVariable},
			{Title: "Logica / Trigger", Description: "Se/Altrimenti, switch globali, trigger area e attese.", Choice: eventPaletteEventLogic},
		},
	},
	{
		Title:       "Battaglie",
		Description: "Tutti i comandi che avviano o configurano battaglie Pokémon e rematch.",
		Entries: []eventCommandPaletteEntry{
			{Title: "Battaglia Allenatore", Description: "TrainerBattle: battaglia contro un allenatore.", Choice: eventPaletteTrainer},
			{Title: "Battaglia Pokémon", Description: "WildBattle: battaglia Pokémon evento normale.", Choice: eventPaletteFixedPokemon},
			{Title: "Leggendario fisso", Description: "WildBattle leggendaria con controllo anti-duplicato globale.", Choice: eventPaletteLegendaryFixed},
			{Title: "Leggendari random / area", Description: "Pool di leggendari casuali limitato a un'area.", Choice: eventPaletteLegendaryArea},
			{Title: "Rebattle / Rematch", Description: "Rende una battaglia ripetibile con cooldown persistente.", Choice: eventPaletteRebattle},
		},
	},
	{
		Title:       "Movimento",
		Description: "Percorsi bloccanti e relazioni di movimento tra giocatore e NPC.",
		Entries: []eventCommandPaletteEntry{
			{Title: "Movimento giocatore", Description: "Sequenza ordinata; l'evento attende la fine completa.", Choice: eventPalettePlayerMovement},
			{Title: "Movimento NPC", Description: "Sequenza personalizzata per un NPC/evento.", Choice: eventPaletteNPCMovement},
			{Title: "Giocatore segue NPC", Description: "Il giocatore segue un NPC/evento.", Choice: eventPalettePlayerFollowsNPC},
			{Title: "NPC segue giocatore", Description: "L'NPC corrente segue il giocatore.", Choice: eventPaletteNPCFollowsPlayer},
		},
	},
	{
		Title:       "Audio",
		Description: "Musica e suoni usati esclusivamente durante eventi e cutscene.",
		Entries: []eventCommandPaletteEntry{
			{Title: "Audio evento", Description: "BGM, BGS, ME, SE, stop e dissolvenze audio.", Choice: eventPaletteAudio},
		},
	},
	{
		Title:       "Grafica / Camera",
		Description: "Immagini, camera, effetti schermo, overworld ed emote.",
		Entries: []eventCommandPaletteEntry{
			{Title: "Grafica schermo", Description: "Mostra, muove o nasconde immagini.", Choice: eventPaletteScreenGraphics},
			{Title: "Camera / Effetti", Description: "Camera, tremore, flash, tinta, zoom e dissolvenze.", Choice: eventPaletteCameraEffects},
			{Title: "Cambia overworld", Description: "Cambia lo sprite overworld del giocatore.", Choice: eventPaletteChangeOverworld},
			{Title: "Emote", Description: "Mostra !, ?, cuore, rabbia e altre emote.", Choice: eventPaletteEmote},
		},
	},
	{
		Title:       "Oggetti / Pokémon evento",
		Description: "Oggetti raccoglibili, Field Move e azioni Pokémon usate dagli eventi.",
		Entries: []eventCommandPaletteEntry{
			{Title: "Dai oggetto", Description: "Aggiunge un oggetto all'inventario.", Choice: eventPaletteGiveItem},
			{Title: "Oggetto nascosto", Description: "Crea un oggetto invisibile raccoglibile.", Choice: eventPaletteHiddenItem},
			{Title: "Poké Ball", Description: "Crea una Poké Ball/oggetto visibile a terra.", Choice: eventPaletteItemBall},
			{Title: "Pokémon evento", Description: "Dai/Rimuovi Pokémon, Uovo e Cura squadra.", Choice: eventPalettePokemonEventTools},
			{Title: "Evento Campo / MN", Description: "Interazione basata su MN/Field Move configurate.", Choice: eventPaletteFieldMove},
		},
	},
	{
		Title:       "Mappa / Trasporti",
		Description: "Warp, punti Volo, interazioni di mappa e sistemi globali di accesso/viaggio.",
		Entries: []eventCommandPaletteEntry{
			{Title: "Warp", Description: "Trasferisce il giocatore e collega Movimento 00.", Choice: eventPaletteWarp},
			{Title: "Punto Volo / POI", Description: "Punto di atterraggio limitato alla regione corrente.", Choice: eventPaletteFlyPOI},
			{Title: "Interazioni mappa", Description: "Leve, pulsanti, ascensori, scale e blocchi NPC.", Choice: eventPaletteMapInteractions},
			{Title: "Eventi globali / Viaggi", Description: "Flag, Pass, biglietti, accessi e nave/trasporti.", Choice: eventPaletteGlobalEvents},
		},
	},
	{
		Title:       "Eventi / Automazioni",
		Description: "Comandi che controllano l'esecuzione e il ciclo di vita dell'evento.",
		Entries: []eventCommandPaletteEntry{
			{Title: "Evento comune", Description: "Richiama un Common Event del progetto.", Choice: eventPaletteCommonEvent},
			{Title: "Evento automatico", Description: "Imposta la pagina come Autorun.", Choice: eventPaletteAutorun},
			{Title: "Evento temporaneo", Description: "Cancella l'evento fino al ricaricamento mappa.", Choice: eventPaletteTemporaryEvent},
		},
	},
	{
		Title:       "Missioni / Scene",
		Description: "Cutscene, progresso, puzzle e struttura narrativa del progetto.",
		Entries: []eventCommandPaletteEntry{
			{Title: "Cutscene / Scene", Description: "Inizio/fine cutscene e scene da sceneggiatura.", Choice: eventPaletteCutscene},
			{Title: "Condizioni progresso", Description: "Medaglie, Pokédex, ginnasi, quest e missioni.", Choice: eventPaletteProgressConditions},
			{Title: "Puzzle", Description: "Percorsi, sequenze, timer, contatori e porte collegate.", Choice: eventPalettePuzzles},
			{Title: "Narrativa / Missioni", Description: "Capitoli, scene, missioni e quest persistenti.", Choice: eventPaletteNarrative},
		},
	},
	{
		Title:       "Script / Strumenti",
		Description: "Strumenti avanzati quando i comandi visuali non bastano.",
		Entries: []eventCommandPaletteEntry{
			{Title: "Traduci testi", Description: "Trova e traduce testi di eventi, script e database.", Choice: eventPaletteTranslateProject},
			{Title: "Script da testo", Description: "Descrizione strutturata da compilare nel runtime PLM.", Choice: eventPaletteScriptFromText},
			{Title: "Script Python", Description: "Codice Python avanzato per casi speciali.", Choice: eventPaletteAdvancedScript},
		},
	},
}

func eventPaletteButtonID(index int) int { return idEventPaletteButtonBase + index }

func currentEventCommandPaletteSection() eventCommandPaletteSection {
	if len(eventCommandPaletteSections) == 0 {
		return eventCommandPaletteSection{}
	}
	idx := eventCommandPaletteSectionIndex
	if idx < 0 {
		idx = 0
	}
	if idx >= len(eventCommandPaletteSections) {
		idx = len(eventCommandPaletteSections) - 1
	}
	return eventCommandPaletteSections[idx]
}

func validateEventCommandPaletteSections() error {
	seenChoice := map[eventCommandPaletteChoice]string{}
	seenTitle := map[string]string{}
	for _, section := range eventCommandPaletteSections {
		if strings.TrimSpace(section.Title) == "" {
			return fmt.Errorf("sezione Comandi evento senza titolo")
		}
		for _, entry := range section.Entries {
			if entry.Choice == eventPaletteNone {
				return fmt.Errorf("comando %q senza handler", entry.Title)
			}
			if prev, ok := seenChoice[entry.Choice]; ok {
				return fmt.Errorf("handler duplicato: %q compare sia in %s sia in %s", entry.Title, prev, section.Title)
			}
			seenChoice[entry.Choice] = section.Title
			key := strings.ToLower(strings.TrimSpace(entry.Title))
			if prev, ok := seenTitle[key]; ok {
				return fmt.Errorf("comando duplicato: %q compare sia in %s sia in %s", entry.Title, prev, section.Title)
			}
			seenTitle[key] = section.Title
		}
	}

	expected := []eventCommandPaletteChoice{
		eventPaletteShowText, eventPaletteMugshot, eventPaletteHiddenItem, eventPaletteItemBall,
		eventPaletteFieldMove, eventPaletteFlyPOI, eventPaletteFixedPokemon, eventPaletteLegendaryFixed,
		eventPaletteLegendaryArea, eventPaletteTrainer, eventPaletteRebattle, eventPaletteWarp,
		eventPaletteSelfSwitch, eventPaletteUnlimitedSelfSwitch, eventPaletteVariable, eventPaletteChoices,
		eventPaletteChangeOverworld, eventPaletteCommonEvent, eventPaletteAutorun, eventPaletteScriptFromText,
		eventPaletteAdvancedScript, eventPaletteTemporaryEvent, eventPaletteGiveItem, eventPalettePlayerFollowsNPC,
		eventPaletteNPCFollowsPlayer, eventPaletteGlobalEvents, eventPaletteCameraEffects, eventPalettePlayerMovement,
		eventPaletteNPCMovement, eventPaletteScreenGraphics, eventPalettePokemonEventTools, eventPaletteCutscene,
		eventPaletteAudio, eventPaletteMapInteractions, eventPaletteProgressConditions, eventPaletteEventLogic,
		eventPaletteEmote, eventPalettePuzzles, eventPaletteNarrative, eventPaletteTranslateProject,
	}
	for _, choice := range expected {
		if _, ok := seenChoice[choice]; !ok {
			return fmt.Errorf("handler Comandi evento non raggiungibile dalla UI: %d", int(choice))
		}
	}
	if len(seenChoice) != len(expected) {
		return fmt.Errorf("palette Comandi evento incoerente: %d handler visuali, attesi %d", len(seenChoice), len(expected))
	}
	return nil
}

func eventCommandPaletteWndProcFn(hwnd syscall.Handle, msg uint32, w, l uintptr) uintptr {
	switch msg {
	case WM_COMMAND:
		id := int(loword(w))
		section := currentEventCommandPaletteSection()
		if id >= idEventPaletteButtonBase && id < idEventPaletteButtonBase+len(section.Entries) {
			idx := id - idEventPaletteButtonBase
			eventCommandPaletteResult = section.Entries[idx].Choice
			eventCommandPaletteAccepted = true
			pDestroyWindow.Call(uintptr(hwnd))
			return 0
		}
		switch id {
		case idEventPaletteBack:
			pDestroyWindow.Call(uintptr(hwnd))
			return 0
		case idEventPalettePrevPage:
			eventCommandPaletteResult = eventPalettePrevSection
			eventCommandPaletteAccepted = true
			pDestroyWindow.Call(uintptr(hwnd))
			return 0
		case idEventPaletteNextPage:
			eventCommandPaletteResult = eventPaletteNextSection
			eventCommandPaletteAccepted = true
			pDestroyWindow.Call(uintptr(hwnd))
			return 0
		case idEventPaletteSaveCompile:
			eventCommandPaletteResult = eventPaletteSaveCompile
			eventCommandPaletteAccepted = true
			pDestroyWindow.Call(uintptr(hwnd))
			return 0
		case idEventPaletteTest:
			eventCommandPaletteResult = eventPaletteTestEvent
			eventCommandPaletteAccepted = true
			pDestroyWindow.Call(uintptr(hwnd))
			return 0
		}
	case WM_CLOSE:
		pDestroyWindow.Call(uintptr(hwnd))
		return 0
	case WM_DESTROY:
		eventCommandPaletteOpen = false
		eventCommandPaletteWindow = 0
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(uintptr(hwnd), uintptr(msg), w, l)
	return r
}

var eventCommandPaletteWndProc = syscall.NewCallback(eventCommandPaletteWndProcFn)

func ensureEventCommandPaletteClass() error {
	if eventCommandPaletteRegistered {
		return nil
	}
	h, _, _ := pGetModuleHandleW.Call(0)
	hInst := syscall.Handle(h)
	cursor, _, _ := pLoadCursorW.Call(0, IDC_ARROW)
	brush, _, _ := pCreateSolidBrush.Call(rgb(244, 244, 244))
	wc := WNDCLASSEX{
		cbSize:        uint32(unsafe.Sizeof(WNDCLASSEX{})),
		lpfnWndProc:   eventCommandPaletteWndProc,
		hInstance:     hInst,
		hCursor:       syscall.Handle(cursor),
		hbrBackground: syscall.Handle(brush),
		lpszClassName: wstr(eventCommandPaletteClassName),
	}
	r, _, err := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	if r == 0 {
		return fmt.Errorf("registrazione finestra Comandi evento fallita: %v", err)
	}
	eventCommandPaletteRegistered = true
	return nil
}

func showEventCommandPaletteDialog(owner syscall.Handle, sectionIndex int) (eventCommandPaletteChoice, bool) {
	if eventCommandPaletteOpen {
		return eventPaletteNone, false
	}
	if err := validateEventCommandPaletteSections(); err != nil {
		msgbox("PML Studio - Comandi evento", "Controllo anti-duplicati fallito:\r\n\r\n"+err.Error(), MB_OK|MB_ICONERROR)
		return eventPaletteNone, false
	}
	if err := ensureEventCommandPaletteClass(); err != nil {
		msgbox("PML Studio - Comandi evento", err.Error(), MB_OK|MB_ICONERROR)
		return eventPaletteNone, false
	}
	if len(eventCommandPaletteSections) == 0 {
		return eventPaletteNone, false
	}
	if sectionIndex < 0 {
		sectionIndex = 0
	}
	if sectionIndex >= len(eventCommandPaletteSections) {
		sectionIndex = len(eventCommandPaletteSections) - 1
	}
	eventCommandPaletteSectionIndex = sectionIndex
	section := currentEventCommandPaletteSection()

	const ww int32 = 930
	const wh int32 = 640
	const cols = 3
	const buttonW int32 = 266
	const buttonH int32 = 58
	const descH int32 = 38
	const cellH int32 = 112
	const gapX int32 = 18
	const startX int32 = 34
	const startY int32 = 126
	x, y := centerOwnedWindow(ww, wh)
	h, _, _ := pGetModuleHandleW.Call(0)
	hInst := syscall.Handle(h)

	eventCommandPaletteAccepted = false
	eventCommandPaletteResult = eventPaletteNone
	eventCommandPaletteOpen = true
	eventCommandPaletteWindow = createWindow(eventCommandPaletteClassName, "Comandi evento - "+section.Title, WS_OVERLAPPEDWINDOW|WS_VISIBLE|WS_CLIPCHILDREN, x, y, ww, wh, owner, 0, hInst)
	if eventCommandPaletteWindow == 0 {
		eventCommandPaletteOpen = false
		return eventPaletteNone, false
	}
	setWindowIcon(eventCommandPaletteWindow)

	createWindow("STATIC", section.Title, WS_CHILD|WS_VISIBLE, 34, 20, 600, 30, eventCommandPaletteWindow, 7640, hInst)
	createWindow("STATIC", fmt.Sprintf("Pagina %d / %d", sectionIndex+1, len(eventCommandPaletteSections)), WS_CHILD|WS_VISIBLE, 738, 22, 150, 26, eventCommandPaletteWindow, 7641, hInst)
	createWindow("STATIC", section.Description, WS_CHILD|WS_VISIBLE, 34, 58, 850, 44, eventCommandPaletteWindow, 7642, hInst)

	for i, entry := range section.Entries {
		col := int32(i % cols)
		row := int32(i / cols)
		bx := startX + col*(buttonW+gapX)
		by := startY + row*cellH
		createWindow("BUTTON", entry.Title, WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_PUSHBUTTON|BS_MULTILINE, bx, by, buttonW, buttonH, eventCommandPaletteWindow, uintptr(eventPaletteButtonID(i)), hInst)
		createWindow("STATIC", entry.Description, WS_CHILD|WS_VISIBLE, bx+4, by+64, buttonW-8, descH, eventCommandPaletteWindow, uintptr(7650+i), hInst)
	}

	const footerY int32 = 548
	createWindow("BUTTON", "← Pagina", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 34, footerY, 118, 42, eventCommandPaletteWindow, idEventPalettePrevPage, hInst)
	createWindow("BUTTON", "Indietro", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 164, footerY, 118, 42, eventCommandPaletteWindow, idEventPaletteBack, hInst)
	createWindow("BUTTON", "Pagina →", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 294, footerY, 118, 42, eventCommandPaletteWindow, idEventPaletteNextPage, hInst)
	createWindow("BUTTON", "Salva / Compila evento", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 464, footerY, 226, 42, eventCommandPaletteWindow, idEventPaletteSaveCompile, hInst)
	createWindow("BUTTON", "▶ Test evento", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 708, footerY, 176, 42, eventCommandPaletteWindow, idEventPaletteTest, hInst)

	pEnableWindow.Call(uintptr(owner), 0)
	pShowWindow.Call(uintptr(eventCommandPaletteWindow), SW_SHOW)
	pUpdateWindow.Call(uintptr(eventCommandPaletteWindow))
	var m MSG
	repostQuit := false
	for eventCommandPaletteOpen {
		r, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) == -1 {
			break
		}
		if r == 0 {
			repostQuit = true
			break
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		pDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
	pEnableWindow.Call(uintptr(owner), 1)
	pSetFocus.Call(uintptr(owner))
	if repostQuit {
		pPostQuitMessage.Call(0)
	}
	return eventCommandPaletteResult, eventCommandPaletteAccepted
}

func canCompileCurrentEventNow() bool {
	return eventEditorEditingIndex >= 0 || (eventEditorTargetX >= 0 && eventEditorTargetY >= 0)
}

func saveCompileCurrentEventFromPalette() bool {
	if !canCompileCurrentEventNow() {
		msgbox("PML Studio - Evento", "Questo è un nuovo evento non ancora posizionato.\r\n\r\nPremi Indietro, poi OK nella finestra Crea evento e clicca sulla casella della mappa. Dopo il posizionamento potrai salvarlo/compilarlo e testarlo.", MB_OK|MB_ICONINFORMATION)
		return false
	}
	if !commitEventEditor(false) {
		return false
	}
	// Compilazione PLM: installa/aggiorna il runtime dei comandi visuali.
	// Il salvataggio evento è già avvenuto; se il runtime non è disponibile
	// fermiamo il test invece di fingere che il comando sia eseguibile.
	if err := ensureGlobalEventsRuntimeBridge(currentProject); err != nil {
		msgbox("PML Studio - Compila evento", "Evento salvato, ma il runtime degli Eventi globali non è stato compilato:\r\n\r\n"+err.Error(), MB_OK|MB_ICONERROR)
		setToolbarStatus("Compila evento: runtime globale non disponibile - " + err.Error())
		return false
	}
	if err := ensureEventToolsRuntimeBridge(currentProject); err != nil {
		msgbox("PML Studio - Compila evento", "Evento salvato, ma il runtime dei comandi evento non è stato compilato:\r\n\r\n"+err.Error(), MB_OK|MB_ICONERROR)
		setToolbarStatus("Compila evento: runtime non disponibile - " + err.Error())
		return false
	}
	if err := validateProjectEventRuntimeCoverage(currentProject); err != nil {
		msgbox("PML Studio - Verifica runtime evento", err.Error(), MB_OK|MB_ICONERROR)
		setToolbarStatus("Compila evento bloccata: comando senza runtime verificato.")
		return false
	}
	setToolbarStatus("Evento salvato, runtime installato e copertura comandi verificata.")
	return true
}

func testCurrentEventFromPalette() bool {
	if !saveCompileCurrentEventFromPalette() {
		return false
	}
	if err := startPlaytest(); err != nil {
		msgbox("PML Studio - Test evento", "Impossibile avviare il test evento:\r\n\r\n"+err.Error(), MB_OK|MB_ICONERROR)
		setToolbarStatus("Test evento: " + err.Error())
		return false
	}
	setToolbarStatus(fmt.Sprintf("Test evento avviato sulla mappa %03d in modalità Debug.", currentMap.ID))
	return true
}

func executeEventPaletteChoice(owner syscall.Handle, choice eventCommandPaletteChoice) {
	switch choice {
	case eventPaletteShowText:
		addShowTextCommand()
	case eventPaletteMugshot:
		addMugshotEventCommand()
	case eventPaletteHiddenItem:
		addItemEventCommand(true)
		loadEventEditorPageToControls()
	case eventPaletteItemBall:
		addItemEventCommand(false)
		loadEventEditorPageToControls()
	case eventPaletteFieldMove:
		addFieldMoveEventCommand()
	case eventPaletteFlyPOI:
		addFlyPOIEventCommand()
	case eventPaletteFixedPokemon:
		addPokemonBattleEventCommand(false)
	case eventPaletteLegendaryFixed:
		addLegendaryFixedPokemonEventCommand()
	case eventPaletteLegendaryArea:
		addLegendaryAreaCommand()
	case eventPaletteTrainer:
		if tr, ok := showTrainerCommandDialog(owner); ok {
			copyTr := tr
			eventEditorPages[eventEditorPageIndex].Trainer = &copyTr
			eventEditorPages[eventEditorPageIndex].TrainerTouched = true
			refreshEventEditorCommandList()
			setToolbarStatus("Comando evento aggiunto: Battaglia Allenatore (Trainer Battle).")
		}
	case eventPaletteRebattle:
		addRebattleCommand()
	case eventPaletteWarp:
		addWarpEventCommand()
	case eventPaletteSelfSwitch:
		addSelfSwitchEventCommand()
	case eventPaletteUnlimitedSelfSwitch:
		addUnlimitedSelfSwitchEventCommand()
	case eventPaletteVariable:
		addVariableEventCommand()
	case eventPaletteChoices:
		addChoicesEventCommand()
	case eventPaletteChangeOverworld:
		addChangeOverworldEventCommand()
	case eventPaletteCommonEvent:
		addCommonEventCommand()
	case eventPaletteAutorun:
		addAutorunEventCommand()
	case eventPaletteScriptFromText:
		addScriptFromTextEventCommand()
	case eventPaletteAdvancedScript:
		addAdvancedPythonEventCommand()
	case eventPaletteTemporaryEvent:
		addTemporaryEventCommand()
	case eventPaletteGiveItem:
		addGiveItemEventCommand()
	case eventPalettePlayerFollowsNPC:
		addPlayerFollowsNPCCommand()
	case eventPaletteNPCFollowsPlayer:
		addNPCFollowsPlayerCommand()
	case eventPaletteGlobalEvents:
		addGlobalEventsCommand()
	case eventPaletteCameraEffects:
		addCameraEffectsEventCommand()
	case eventPalettePlayerMovement:
		addMovementRouteEventCommand(false)
	case eventPaletteNPCMovement:
		addMovementRouteEventCommand(true)
	case eventPaletteScreenGraphics:
		addScreenGraphicsEventCommand()
	case eventPalettePokemonEventTools:
		addPokemonEventToolsCommand()
	case eventPaletteCutscene:
		addCutsceneEventCommand()
	case eventPaletteAudio:
		addAudioEventCommand()
	case eventPaletteMapInteractions:
		addMapInteractionsEventCommand()
	case eventPaletteProgressConditions:
		addProgressConditionEventCommand()
	case eventPaletteEventLogic:
		addEventLogicCommand()
	case eventPaletteEmote:
		addEmoteEventCommand()
	case eventPalettePuzzles:
		addPuzzleEventCommand()
	case eventPaletteNarrative:
		addNarrativeEventCommand()
	case eventPaletteTranslateProject:
		addProjectTranslationCommand()
	}
}

func openEventCommandPaletteAndInsert(owner syscall.Handle) {
	// Mantiene la sezione corrente mentre si inseriscono più comandi. Avanti e
	// Indietro sfogliano le categorie senza uscire dall'editor evento.
	sectionIndex := 0
	for {
		choice, ok := showEventCommandPaletteDialog(owner, sectionIndex)
		if !ok {
			return
		}
		switch choice {
		case eventPalettePrevSection:
			sectionIndex--
			if sectionIndex < 0 {
				sectionIndex = len(eventCommandPaletteSections) - 1
			}
			continue
		case eventPaletteNextSection:
			sectionIndex++
			if sectionIndex >= len(eventCommandPaletteSections) {
				sectionIndex = 0
			}
			continue
		case eventPaletteSaveCompile:
			saveCompileCurrentEventFromPalette()
			continue
		case eventPaletteTestEvent:
			testCurrentEventFromPalette()
			continue
		default:
			executeEventPaletteChoice(owner, choice)
			refreshEventEditorCommandList()
		}
	}
}
