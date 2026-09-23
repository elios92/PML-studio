//go:build windows

package main

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

const (
	eventEditorClassName = "PLMStudioEventEditor05"

	idEvName                = 2800
	idEvNewPage             = 2801
	idEvCopyPage            = 2802
	idEvPastePage           = 2803
	idEvDeletePage          = 2804
	idEvClearPage           = 2805
	idEvSwitch1             = 2810
	idEvSwitch1ID           = 2811
	idEvSwitch2             = 2812
	idEvSwitch2ID           = 2813
	idEvVariable            = 2814
	idEvVariableID          = 2815
	idEvVariableValue       = 2816
	idEvSelfSwitch          = 2817
	idEvSelfSwitchValue     = 2818
	idEvUnlimitedSelfSwitch = 2819
	idEvGraphic             = 2820
	idEvRemoveTransparency  = 2821
	idEvLightEffect         = 2822
	idEvChooseGraphic       = 2823
	idEvMoveType            = 2830
	idEvMoveRoute           = 2831
	idEvMoveSpeed           = 2832
	idEvMoveFrequency       = 2833
	idEvWalkAnim            = 2840
	idEvStepAnim            = 2841
	idEvDirectionFix        = 2842
	idEvThrough             = 2843
	idEvAlwaysTop           = 2844
	idEvTriggerAction       = 2850
	idEvTriggerPlayerTouch  = 2851
	idEvTriggerEventTouch   = 2852
	idEvTriggerAutorun      = 2853
	idEvTriggerParallel     = 2854
	idEvCommandList         = 2860
	idEvTrainerCommand      = 2861
	idEvTextCommand         = 2862
	idEvHiddenItemCommand   = 2863
	idEvItemBallCommand     = 2864
	idEvFixedPokemonCommand = 2865
	idEvOK                  = 2870
	idEvCancel              = 2871
	idEvApply               = 2872
	idEvPageBase            = 2900
)

type eventPageDraft struct {
	// Raw preserves the complete native RPG Maker XP page so opening and
	// saving an existing event never destroys commands that PLM Studio does
	// not edit yet. Managed fields below are patched back into this payload.
	Raw map[string]any

	Switch1Enabled        bool
	Switch1ID             int
	Switch1Name           string
	Switch1On             bool
	Switch2Enabled        bool
	Switch2ID             int
	Switch2Name           string
	Switch2On             bool
	VariableEnabled       bool
	VariableID            int
	VariableName          string
	VariableValue         int
	SelfSwitchEnabled     bool
	SelfSwitch            string
	UnlimitedSelfSwitches []plmNamedSelfSwitchCondition

	Graphic            string
	RemoveTransparency bool
	LightEffect        bool

	MoveType      int
	MoveSpeed     int
	MoveFrequency int
	WalkAnim      bool
	StepAnim      bool
	DirectionFix  bool
	Through       bool
	AlwaysTop     bool
	Trigger       int

	Trainer        *TrainerRecord
	TrainerTouched bool
}

var (
	eventEditorRegistered bool
	eventEditorOpen       bool
	eventEditorWindow     syscall.Handle

	eventEditorName                                                                                             syscall.Handle
	eventEditorPageButtons                                                                                      []syscall.Handle
	eventEditorSwitch1, eventEditorSwitch1ID, eventEditorSwitch1State                                           syscall.Handle
	eventEditorSwitch2, eventEditorSwitch2ID, eventEditorSwitch2State                                           syscall.Handle
	eventEditorVariable, eventEditorVariableID, eventEditorVariableValue                                        syscall.Handle
	eventEditorSelfSwitch, eventEditorSelfSwitchValue                                                           syscall.Handle
	eventEditorUnlimitedSelfSwitch, eventEditorUnlimitedSelfSwitchState                                         syscall.Handle
	eventEditorGraphic                                                                                          syscall.Handle
	eventEditorRemoveTransparency, eventEditorLightEffect                                                       syscall.Handle
	eventEditorMoveType, eventEditorMoveRoute, eventEditorMoveSpeed, eventEditorMoveFrequency                   syscall.Handle
	eventEditorWalkAnim, eventEditorStepAnim, eventEditorDirectionFix, eventEditorThrough, eventEditorAlwaysTop syscall.Handle
	eventEditorTriggerAction, eventEditorTriggerPlayerTouch, eventEditorTriggerEventTouch                       syscall.Handle
	eventEditorTriggerAutorun, eventEditorTriggerParallel                                                       syscall.Handle
	eventEditorCommands                                                                                         syscall.Handle

	eventEditorPages         []eventPageDraft
	eventEditorPageIndex     int
	eventEditorPageClipboard *eventPageDraft
	eventEditorSprites       []string
	eventEditorAppliedName   string
	eventEditorEditingIndex  = -1
	eventEditorTargetX       = -1
	eventEditorTargetY       = -1

	pendingGenericEvent *EditorEvent
)

func checked(h syscall.Handle) bool {
	if h == 0 {
		return false
	}
	r, _, _ := pSendMessageW.Call(uintptr(h), BM_GETCHECK, 0, 0)
	return r == BST_CHECKED
}

func intField(h syscall.Handle, def int) int {
	v, err := strconv.Atoi(strings.TrimSpace(getText(h)))
	if err != nil {
		return def
	}
	return v
}

func setIntField(h syscall.Handle, v int) { setText(h, strconv.Itoa(v)) }

func defaultEventPageDraft() eventPageDraft {
	return eventPageDraft{Switch1On: true, Switch2On: true, SelfSwitch: "A", MoveType: 0, MoveSpeed: 3, MoveFrequency: 3, WalkAnim: true, Trigger: 0}
}

func eventAnyBool(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case json.Number:
		i, _ := t.Int64()
		return i != 0
	case float64:
		return t != 0
	case int:
		return t != 0
	case string:
		t = strings.ToLower(strings.TrimSpace(t))
		return t == "true" || t == "1" || t == "yes" || t == "on"
	}
	return false
}

func cloneEventMap(src map[string]any) map[string]any {
	if src == nil {
		return nil
	}
	b, err := json.Marshal(src)
	if err != nil {
		return nil
	}
	v, err := decodeJSONAny(b)
	if err != nil {
		return nil
	}
	m, _ := v.(map[string]any)
	return m
}

func setEventMapCI(m map[string]any, name string, value any) {
	for k := range m {
		n := strings.TrimSpace(strings.TrimPrefix(k, "@"))
		if strings.EqualFold(n, name) {
			m[k] = value
			return
		}
	}
	m[name] = value
}

func nativeEventPages(m map[string]any) []map[string]any {
	v, ok := anyMapValueCI(m, "pages")
	if !ok {
		return nil
	}
	out := []map[string]any{}
	switch pages := v.(type) {
	case []any:
		for _, item := range pages {
			if pm, ok := item.(map[string]any); ok {
				out = append(out, pm)
			}
		}
	case map[string]any:
		keys := make([]int, 0, len(pages))
		byKey := map[int]map[string]any{}
		for k, item := range pages {
			if pm, ok := item.(map[string]any); ok {
				n, err := strconv.Atoi(k)
				if err != nil {
					continue
				}
				keys = append(keys, n)
				byKey[n] = pm
			}
		}
		for i := 0; i < len(keys); i++ {
			for j := i + 1; j < len(keys); j++ {
				if keys[j] < keys[i] {
					keys[i], keys[j] = keys[j], keys[i]
				}
			}
		}
		for _, k := range keys {
			out = append(out, byKey[k])
		}
	}
	return out
}

func eventPageDraftFromNative(pm map[string]any) eventPageDraft {
	p := defaultEventPageDraft()
	p.Raw = cloneEventMap(pm)
	if cv, ok := anyMapValueCI(pm, "condition"); ok {
		if c, ok := cv.(map[string]any); ok {
			if v, ok := anyMapValueCI(c, "switch1_valid"); ok {
				p.Switch1Enabled = eventAnyBool(v)
			}
			if v, ok := anyMapValueCI(c, "switch2_valid"); ok {
				p.Switch2Enabled = eventAnyBool(v)
			}
			if v, ok := anyMapValueCI(c, "variable_valid"); ok {
				p.VariableEnabled = eventAnyBool(v)
			}
			if v, ok := anyMapValueCI(c, "self_switch_valid"); ok {
				p.SelfSwitchEnabled = eventAnyBool(v)
			}
			if v, ok := anyMapValueCI(c, "switch1_id"); ok {
				p.Switch1ID = anyInt(v)
			}
			if v, ok := anyMapValueCI(c, "plm_switch1_name", "switch1_name"); ok {
				p.Switch1Name = strings.TrimSpace(anyString(v))
			}
			if v, ok := anyMapValueCI(c, "plm_switch1_state", "switch1_state"); ok {
				p.Switch1On = !strings.EqualFold(strings.TrimSpace(anyString(v)), "OFF")
			}
			if v, ok := anyMapValueCI(c, "switch2_id"); ok {
				p.Switch2ID = anyInt(v)
			}
			if v, ok := anyMapValueCI(c, "plm_switch2_name", "switch2_name"); ok {
				p.Switch2Name = strings.TrimSpace(anyString(v))
			}
			if v, ok := anyMapValueCI(c, "plm_switch2_state", "switch2_state"); ok {
				p.Switch2On = !strings.EqualFold(strings.TrimSpace(anyString(v)), "OFF")
			}
			if v, ok := anyMapValueCI(c, "variable_id"); ok {
				p.VariableID = anyInt(v)
			}
			if v, ok := anyMapValueCI(c, "plm_variable_name", "variable_name"); ok {
				p.VariableName = strings.TrimSpace(anyString(v))
			}
			if v, ok := anyMapValueCI(c, "variable_value"); ok {
				p.VariableValue = anyInt(v)
			}
			if v, ok := anyMapValueCI(c, "self_switch_ch"); ok {
				p.SelfSwitch = strings.TrimSpace(anyString(v))
			}
		}
	}
	if p.Switch1ID > 0 && p.Switch1Name == "" {
		p.Switch1Name = projectSwitchName(p.Switch1ID)
	}
	if p.Switch2ID > 0 && p.Switch2Name == "" {
		p.Switch2Name = projectSwitchName(p.Switch2ID)
	}
	if p.VariableID > 0 && p.VariableName == "" {
		p.VariableName = projectVariableName(p.VariableID)
	}
	if lv, ok := anyMapValueCI(pm, "list"); ok {
		if arr, ok := lv.([]any); ok {
			p.UnlimitedSelfSwitches = unlimitedSelfSwitchConditionsFromCommands(arr)
		}
	}
	if gv, ok := anyMapValueCI(pm, "graphic"); ok {
		if g, ok := gv.(map[string]any); ok {
			if v, ok := anyMapValueCI(g, "character_name"); ok {
				p.Graphic = strings.TrimSpace(anyString(v))
			}
			if v, ok := anyMapValueCI(g, "plm_remove_transparency"); ok {
				p.RemoveTransparency = eventAnyBool(v)
			}
			if v, ok := anyMapValueCI(g, "plm_light_effect"); ok {
				p.LightEffect = eventAnyBool(v)
			}
		}
	}
	if v, ok := anyMapValueCI(pm, "move_type"); ok {
		p.MoveType = anyInt(v)
	}
	if v, ok := anyMapValueCI(pm, "move_speed"); ok {
		p.MoveSpeed = anyInt(v)
	}
	if v, ok := anyMapValueCI(pm, "move_frequency"); ok {
		p.MoveFrequency = anyInt(v)
	}
	if v, ok := anyMapValueCI(pm, "walk_anime", "walk_animation"); ok {
		p.WalkAnim = eventAnyBool(v)
	}
	if v, ok := anyMapValueCI(pm, "step_anime", "step_animation"); ok {
		p.StepAnim = eventAnyBool(v)
	}
	if v, ok := anyMapValueCI(pm, "direction_fix"); ok {
		p.DirectionFix = eventAnyBool(v)
	}
	if v, ok := anyMapValueCI(pm, "through"); ok {
		p.Through = eventAnyBool(v)
	}
	if v, ok := anyMapValueCI(pm, "always_on_top"); ok {
		p.AlwaysTop = eventAnyBool(v)
	}
	if v, ok := anyMapValueCI(pm, "trigger"); ok {
		p.Trigger = anyInt(v)
	}
	if tt, tn, tv, ok := findTrainerReference(pm); ok {
		tr := TrainerRecord{TrainerType: tt, Name: tn, Version: tv}
		if found, ok := findTrainerRecord(tt, tn, tv); ok {
			tr = found
		}
		p.Trainer = &tr
	}
	return p
}

func findTrainerRecord(trainerType, name string, version int) (TrainerRecord, bool) {
	ensureTrainerCatalog()
	for _, tr := range trainerCatalog {
		if strings.EqualFold(tr.TrainerType, trainerType) && strings.EqualFold(tr.Name, name) && tr.Version == version {
			return tr, true
		}
	}
	return TrainerRecord{}, false
}

func eventDraftsFromEditorEvent(e EditorEvent) []eventPageDraft {
	if len(e.NativeRaw) == 0 {
		return []eventPageDraft{defaultEventPageDraft()}
	}
	v, err := decodeJSONAny(e.NativeRaw)
	if err != nil {
		return []eventPageDraft{defaultEventPageDraft()}
	}
	m, ok := v.(map[string]any)
	if !ok {
		return []eventPageDraft{defaultEventPageDraft()}
	}
	pages := nativeEventPages(m)
	if len(pages) == 0 {
		return []eventPageDraft{defaultEventPageDraft()}
	}
	out := make([]eventPageDraft, 0, len(pages))
	for _, pm := range pages {
		out = append(out, eventPageDraftFromNative(pm))
	}
	return out
}

func saveEventEditorControlsToPage() {
	if eventEditorPageIndex < 0 || eventEditorPageIndex >= len(eventEditorPages) {
		return
	}
	p := &eventEditorPages[eventEditorPageIndex]
	p.Switch1Enabled = checked(eventEditorSwitch1) && p.Switch1ID > 0
	p.Switch2Enabled = checked(eventEditorSwitch2) && p.Switch2ID > 0
	p.VariableEnabled = checked(eventEditorVariable) && p.VariableID > 0
	p.SelfSwitchEnabled = checked(eventEditorSelfSwitch)
	idx := comboSel(eventEditorSelfSwitchValue)
	if idx >= 0 && idx < 4 {
		p.SelfSwitch = []string{"A", "B", "C", "D"}[idx]
	}
	// La grafica viene scelta dal riquadro anteprima con doppio click e
	// memorizzata direttamente nella pagina corrente. La trasparenza viene
	// applicata al PNG durante l'importazione, non come flag UI separato.
	p.LightEffect = checked(eventEditorLightEffect)
	p.MoveType = comboSel(eventEditorMoveType)
	p.MoveSpeed = comboSel(eventEditorMoveSpeed) + 1
	if p.MoveSpeed < 1 {
		p.MoveSpeed = 3
	}
	p.MoveFrequency = comboSel(eventEditorMoveFrequency) + 1
	if p.MoveFrequency < 1 {
		p.MoveFrequency = 3
	}
	p.WalkAnim = checked(eventEditorWalkAnim)
	p.StepAnim = checked(eventEditorStepAnim)
	p.DirectionFix = checked(eventEditorDirectionFix)
	p.Through = checked(eventEditorThrough)
	p.AlwaysTop = checked(eventEditorAlwaysTop)
	switch {
	case checked(eventEditorTriggerPlayerTouch):
		p.Trigger = 1
	case checked(eventEditorTriggerEventTouch):
		p.Trigger = 2
	case checked(eventEditorTriggerAutorun):
		p.Trigger = 3
	case checked(eventEditorTriggerParallel):
		p.Trigger = 4
	default:
		p.Trigger = 0
	}
}

func loadEventEditorPageToControls() {
	if eventEditorPageIndex < 0 || eventEditorPageIndex >= len(eventEditorPages) {
		return
	}
	p := eventEditorPages[eventEditorPageIndex]
	setChecked(eventEditorSwitch1, p.Switch1Enabled)
	setChecked(eventEditorSwitch2, p.Switch2Enabled)
	setChecked(eventEditorVariable, p.VariableEnabled)
	refreshEventEditorConditionControls()
	setChecked(eventEditorSelfSwitch, p.SelfSwitchEnabled)
	ssIdx := 0
	for i, s := range []string{"A", "B", "C", "D"} {
		if strings.EqualFold(s, p.SelfSwitch) {
			ssIdx = i
		}
	}
	comboSelectIndex(eventEditorSelfSwitchValue, ssIdx)
	refreshUnlimitedSelfSwitchConditionStatus()
	invalidate(eventEditorGraphic)
	setChecked(eventEditorLightEffect, p.LightEffect)
	comboSelectIndex(eventEditorMoveType, clampInt(p.MoveType, 0, 3))
	comboSelectIndex(eventEditorMoveSpeed, clampInt(p.MoveSpeed-1, 0, 5))
	comboSelectIndex(eventEditorMoveFrequency, clampInt(p.MoveFrequency-1, 0, 4))
	setChecked(eventEditorWalkAnim, p.WalkAnim)
	setChecked(eventEditorStepAnim, p.StepAnim)
	setChecked(eventEditorDirectionFix, p.DirectionFix)
	setChecked(eventEditorThrough, p.Through)
	setChecked(eventEditorAlwaysTop, p.AlwaysTop)
	setChecked(eventEditorTriggerAction, p.Trigger == 0)
	setChecked(eventEditorTriggerPlayerTouch, p.Trigger == 1)
	setChecked(eventEditorTriggerEventTouch, p.Trigger == 2)
	setChecked(eventEditorTriggerAutorun, p.Trigger == 3)
	setChecked(eventEditorTriggerParallel, p.Trigger == 4)
	refreshEventEditorCommandList()
	refreshEventEditorPageButtons()
}

func refreshEventEditorConditionControls() {
	if eventEditorPageIndex < 0 || eventEditorPageIndex >= len(eventEditorPages) {
		return
	}
	p := &eventEditorPages[eventEditorPageIndex]

	switch1Enabled := p.Switch1Enabled && p.Switch1ID > 0
	if switch1Enabled {
		if strings.TrimSpace(p.Switch1Name) == "" {
			p.Switch1Name = projectSwitchName(p.Switch1ID)
		}
		setText(eventEditorSwitch1ID, eventConditionDisplayName(p.Switch1ID, p.Switch1Name, "switch"))
		if p.Switch1On {
			setText(eventEditorSwitch1State, "ON")
		} else {
			setText(eventEditorSwitch1State, "OFF")
		}
		pEnableWindow.Call(uintptr(eventEditorSwitch1ID), 1)
	} else {
		setText(eventEditorSwitch1ID, "Seleziona...")
		setText(eventEditorSwitch1State, "")
		pEnableWindow.Call(uintptr(eventEditorSwitch1ID), 0)
	}

	switch2Enabled := p.Switch2Enabled && p.Switch2ID > 0
	if switch2Enabled {
		if strings.TrimSpace(p.Switch2Name) == "" {
			p.Switch2Name = projectSwitchName(p.Switch2ID)
		}
		setText(eventEditorSwitch2ID, eventConditionDisplayName(p.Switch2ID, p.Switch2Name, "switch"))
		if p.Switch2On {
			setText(eventEditorSwitch2State, "ON")
		} else {
			setText(eventEditorSwitch2State, "OFF")
		}
		pEnableWindow.Call(uintptr(eventEditorSwitch2ID), 1)
	} else {
		setText(eventEditorSwitch2ID, "Seleziona...")
		setText(eventEditorSwitch2State, "")
		pEnableWindow.Call(uintptr(eventEditorSwitch2ID), 0)
	}

	variableEnabled := p.VariableEnabled && p.VariableID > 0
	if variableEnabled {
		if strings.TrimSpace(p.VariableName) == "" {
			p.VariableName = projectVariableName(p.VariableID)
		}
		setText(eventEditorVariableID, eventConditionDisplayName(p.VariableID, p.VariableName, "variable"))
		setText(eventEditorVariableValue, fmt.Sprintf(">= %d", p.VariableValue))
		pEnableWindow.Call(uintptr(eventEditorVariableID), 1)
	} else {
		setText(eventEditorVariableID, "Seleziona...")
		setText(eventEditorVariableValue, "")
		pEnableWindow.Call(uintptr(eventEditorVariableID), 0)
	}
}

func editEventSwitchCondition(which int, forceOpen bool) {
	if eventEditorPageIndex < 0 || eventEditorPageIndex >= len(eventEditorPages) {
		return
	}
	p := &eventEditorPages[eventEditorPageIndex]
	var check, summary syscall.Handle
	id, name, on := p.Switch1ID, p.Switch1Name, p.Switch1On
	if which == 2 {
		check, summary = eventEditorSwitch2, eventEditorSwitch2ID
		id, name, on = p.Switch2ID, p.Switch2Name, p.Switch2On
	} else {
		check, summary = eventEditorSwitch1, eventEditorSwitch1ID
	}

	if !forceOpen && !checked(check) {
		if which == 2 {
			p.Switch2Enabled = false
			p.Switch2ID = 0
			p.Switch2Name = ""
			p.Switch2On = true
		} else {
			p.Switch1Enabled = false
			p.Switch1ID = 0
			p.Switch1Name = ""
			p.Switch1On = true
		}
		refreshEventEditorConditionControls()
		return
	}

	resultID, resultName, resultOn, _, ok := showEventConditionPicker(eventEditorWindow, conditionPickerSwitch, id, name, on, 0)
	if !ok {
		if id <= 0 {
			setChecked(check, false)
		}
		refreshEventEditorConditionControls()
		return
	}
	if which == 2 {
		p.Switch2Enabled = true
		p.Switch2ID = resultID
		p.Switch2Name = resultName
		p.Switch2On = resultOn
	} else {
		p.Switch1Enabled = true
		p.Switch1ID = resultID
		p.Switch1Name = resultName
		p.Switch1On = resultOn
	}
	setChecked(check, true)
	pEnableWindow.Call(uintptr(summary), 1)
	refreshEventEditorConditionControls()
}

func editEventVariableCondition(forceOpen bool) {
	if eventEditorPageIndex < 0 || eventEditorPageIndex >= len(eventEditorPages) {
		return
	}
	p := &eventEditorPages[eventEditorPageIndex]
	if !forceOpen && !checked(eventEditorVariable) {
		p.VariableEnabled = false
		p.VariableID = 0
		p.VariableName = ""
		p.VariableValue = 0
		refreshEventEditorConditionControls()
		return
	}
	resultID, resultName, _, resultValue, ok := showEventConditionPicker(eventEditorWindow, conditionPickerVariable, p.VariableID, p.VariableName, true, p.VariableValue)
	if !ok {
		if p.VariableID <= 0 {
			setChecked(eventEditorVariable, false)
		}
		refreshEventEditorConditionControls()
		return
	}
	p.VariableEnabled = true
	p.VariableID = resultID
	p.VariableName = resultName
	p.VariableValue = resultValue
	setChecked(eventEditorVariable, true)
	refreshEventEditorConditionControls()
}

func clampInt(v, a, b int) int {
	if v < a {
		return a
	}
	if v > b {
		return b
	}
	return v
}

func refreshEventEditorCommandList() {
	clearList(eventEditorCommands)
	if eventEditorPageIndex < 0 || eventEditorPageIndex >= len(eventEditorPages) {
		return
	}
	p := eventEditorPages[eventEditorPageIndex]
	if p.Raw != nil {
		if lv, ok := anyMapValueCI(p.Raw, "list"); ok {
			if arr, ok := lv.([]any); ok {
				skipManagedNodes := 0
				for _, c := range arr {
					m, ok := c.(map[string]any)
					if !ok || commandMapCode(m) == 0 || isTrainerCommandMap(m) {
						continue
					}
					if skipManagedNodes > 0 {
						skipManagedNodes--
						continue
					}
					code := commandMapCode(m)
					param := commandFirstParam(m)
					if code == 108 {
						if managed, ok := parsePLMEventCommandMarker(param); ok {
							addList(eventEditorCommands, managedEventCommandLabel(managed))
							switch managed.Type {
							case "hidden_item", "item_ball":
								if managed.OneShot {
									skipManagedNodes = 3 // branch + self switch + branch end
								} else {
									skipManagedNodes = 1 // compatibility script
								}
							case "fixed_pokemon":
								if managed.OneShot {
									skipManagedNodes = 2 // battle script + self switch
								} else {
									skipManagedNodes = 1
								}
							case "access_gate":
								skipManagedNodes = 1 // Self Switch A after successful validation
							case "warp":
								skipManagedNodes = 1 // native Transfer Player command
							case "self_switch":
								ch := strings.ToUpper(strings.TrimSpace(managed.SelfSwitch))
								if ch == "A" || ch == "B" || ch == "C" || ch == "D" {
									skipManagedNodes = 1
								}
							case "variable", "common_event", "temporary_event", "give_item":
								skipManagedNodes = 1
							case "choices":
								skipManagedNodes = len(managed.Choices) + 2
							}
							continue
						}
					}
					switch code {
					case 101:
						addList(eventEditorCommands, "◆ Testo: "+param)
					case 401:
						addList(eventEditorCommands, "   "+param)
					case 201:
						addList(eventEditorCommands, "◆ Trasferisci giocatore")
					case 230:
						addList(eventEditorCommands, "◆ Attendi")
					case 250:
						addList(eventEditorCommands, "◆ Riproduci effetto sonoro")
					case 355, 655:
						if len(param) > 90 {
							param = param[:87] + "..."
						}
						addList(eventEditorCommands, "◆ Script: "+param)
					case 108, 408:
						addList(eventEditorCommands, "◆ Commento: "+param)
					default:
						addList(eventEditorCommands, fmt.Sprintf("◆ Comando esistente [codice %d]", code))
					}
				}
			}
		}
	}
	if p.Trainer != nil {
		tr := *p.Trainer
		suffix := ""
		if tr.Version > 0 {
			suffix = fmt.Sprintf(" [v%d]", tr.Version)
		}
		addList(eventEditorCommands, fmt.Sprintf("◆ Lotta Allenatore Pokémon: %s / %s%s", tr.TrainerType, tr.Name, suffix))
		addList(eventEditorCommands, "  → "+trainerBattleScript(tr))
	}
	addList(eventEditorCommands, "◆")
}

func refreshEventEditorPageButtons() {
	for _, h := range eventEditorPageButtons {
		if h != 0 {
			pDestroyWindow.Call(uintptr(h))
		}
	}
	eventEditorPageButtons = eventEditorPageButtons[:0]
	if eventEditorWindow == 0 {
		return
	}
	h, _, _ := pGetModuleHandleW.Call(0)
	hInst := syscall.Handle(h)
	x := int32(22)
	for i := range eventEditorPages {
		style := uint32(WS_CHILD | WS_VISIBLE | WS_TABSTOP | BS_AUTORADIOBUTTON | BS_PUSHLIKE | BS_FLAT)
		if i == 0 {
			style |= WS_GROUP
		}
		ph := createWindow("BUTTON", strconv.Itoa(i+1), style, x, 112, 48, 27, eventEditorWindow, uintptr(idEvPageBase+i), hInst)
		if i == eventEditorPageIndex {
			pSendMessageW.Call(uintptr(ph), BM_SETCHECK, BST_CHECKED, 0)
		}
		eventEditorPageButtons = append(eventEditorPageButtons, ph)
		x += 50
	}
}

func newEventPage() {
	saveEventEditorControlsToPage()
	eventEditorPages = append(eventEditorPages, defaultEventPageDraft())
	eventEditorPageIndex = len(eventEditorPages) - 1
	loadEventEditorPageToControls()
}
func copyEventPage() {
	saveEventEditorControlsToPage()
	if eventEditorPageIndex >= 0 && eventEditorPageIndex < len(eventEditorPages) {
		p := eventEditorPages[eventEditorPageIndex]
		eventEditorPageClipboard = &p
	}
}
func pasteEventPage() {
	if eventEditorPageClipboard == nil {
		return
	}
	saveEventEditorControlsToPage()
	p := *eventEditorPageClipboard
	if eventEditorPageIndex >= 0 && eventEditorPageIndex < len(eventEditorPages) {
		eventEditorPages[eventEditorPageIndex] = p
	}
	loadEventEditorPageToControls()
}
func deleteEventPage() {
	if len(eventEditorPages) <= 1 {
		clearEventPage()
		return
	}
	saveEventEditorControlsToPage()
	eventEditorPages = append(eventEditorPages[:eventEditorPageIndex], eventEditorPages[eventEditorPageIndex+1:]...)
	if eventEditorPageIndex >= len(eventEditorPages) {
		eventEditorPageIndex = len(eventEditorPages) - 1
	}
	loadEventEditorPageToControls()
}
func clearEventPage() {
	if eventEditorPageIndex >= 0 && eventEditorPageIndex < len(eventEditorPages) {
		eventEditorPages[eventEditorPageIndex] = defaultEventPageDraft()
		loadEventEditorPageToControls()
	}
}

func commandMapCode(m map[string]any) int {
	if v, ok := anyMapValueCI(m, "code"); ok {
		return anyInt(v)
	}
	return 0
}

func commandFirstParam(m map[string]any) string {
	if v, ok := anyMapValueCI(m, "parameters", "params"); ok {
		if arr, ok := v.([]any); ok && len(arr) > 0 {
			return strings.TrimSpace(anyString(arr[0]))
		}
	}
	return ""
}

func isTrainerCommandMap(m map[string]any) bool {
	code := commandMapCode(m)
	p := commandFirstParam(m)
	if code == 108 && strings.HasPrefix(strings.ToUpper(p), "PML_TRAINER_REF:") {
		return true
	}
	return code == 355 && trainerBattleRefRE.MatchString(p)
}

func stripTrainerCommands(commands []any) []any {
	out := make([]any, 0, len(commands))
	for _, c := range commands {
		if m, ok := c.(map[string]any); ok && isTrainerCommandMap(m) {
			continue
		}
		out = append(out, c)
	}
	return out
}

func eventPageToNative(p eventPageDraft) map[string]any {
	page := cloneEventMap(p.Raw)
	if page == nil {
		page = map[string]any{"_class": "RPG::Event::Page"}
	}

	condition := map[string]any{"_class": "RPG::Event::Page::Condition"}
	if cv, ok := anyMapValueCI(page, "condition"); ok {
		if existing, ok := cv.(map[string]any); ok {
			condition = cloneEventMap(existing)
		}
	}
	setEventMapCI(condition, "switch1_valid", p.Switch1Enabled && p.Switch1ID > 0)
	setEventMapCI(condition, "switch2_valid", p.Switch2Enabled && p.Switch2ID > 0)
	setEventMapCI(condition, "variable_valid", p.VariableEnabled && p.VariableID > 0)
	setEventMapCI(condition, "self_switch_valid", p.SelfSwitchEnabled)
	setEventMapCI(condition, "switch1_id", p.Switch1ID)
	setEventMapCI(condition, "plm_switch1_name", strings.TrimSpace(p.Switch1Name))
	if p.Switch1On {
		setEventMapCI(condition, "plm_switch1_state", "ON")
	} else {
		setEventMapCI(condition, "plm_switch1_state", "OFF")
	}
	setEventMapCI(condition, "switch2_id", p.Switch2ID)
	setEventMapCI(condition, "plm_switch2_name", strings.TrimSpace(p.Switch2Name))
	if p.Switch2On {
		setEventMapCI(condition, "plm_switch2_state", "ON")
	} else {
		setEventMapCI(condition, "plm_switch2_state", "OFF")
	}
	setEventMapCI(condition, "variable_id", p.VariableID)
	setEventMapCI(condition, "plm_variable_name", strings.TrimSpace(p.VariableName))
	setEventMapCI(condition, "variable_value", p.VariableValue)
	setEventMapCI(condition, "self_switch_ch", p.SelfSwitch)
	setEventMapCI(page, "condition", condition)

	graphic := map[string]any{"_class": "RPG::Event::Page::Graphic", "tile_id": 0, "character_hue": 0, "direction": 2, "pattern": 0, "opacity": 255, "blend_type": 0}
	if gv, ok := anyMapValueCI(page, "graphic"); ok {
		if existing, ok := gv.(map[string]any); ok {
			graphic = cloneEventMap(existing)
		}
	}
	setEventMapCI(graphic, "character_name", p.Graphic)
	setEventMapCI(graphic, "plm_remove_transparency", p.RemoveTransparency)
	setEventMapCI(graphic, "plm_light_effect", p.LightEffect)
	setEventMapCI(page, "graphic", graphic)

	setEventMapCI(page, "move_type", p.MoveType)
	setEventMapCI(page, "move_speed", p.MoveSpeed)
	setEventMapCI(page, "move_frequency", p.MoveFrequency)
	setEventMapCI(page, "walk_anime", p.WalkAnim)
	setEventMapCI(page, "step_anime", p.StepAnim)
	setEventMapCI(page, "direction_fix", p.DirectionFix)
	setEventMapCI(page, "through", p.Through)
	setEventMapCI(page, "always_on_top", p.AlwaysTop)
	setEventMapCI(page, "trigger", p.Trigger)

	commands := []any{}
	if lv, ok := anyMapValueCI(page, "list"); ok {
		if arr, ok := lv.([]any); ok {
			commands = append(commands, arr...)
		}
	}
	commands = prependUnlimitedSelfSwitchConditionComments(commands, p.UnlimitedSelfSwitches)
	if p.TrainerTouched || (p.Raw == nil && p.Trainer != nil) {
		commands = stripTrainerCommands(commands)
		// Keep terminator at the end while inserting the managed trainer command.
		withoutEnd := make([]any, 0, len(commands))
		for _, c := range commands {
			if m, ok := c.(map[string]any); ok && commandMapCode(m) == 0 {
				continue
			}
			withoutEnd = append(withoutEnd, c)
		}
		commands = withoutEnd
		if p.Trainer != nil {
			tr := *p.Trainer
			marker := fmt.Sprintf("PML_TRAINER_REF:%s|%s|%d", tr.TrainerType, tr.Name, tr.Version)
			commands = append(commands, map[string]any{"_class": "RPG::EventCommand", "code": 108, "indent": 0, "parameters": []any{marker}})
			commands = append(commands, map[string]any{"_class": "RPG::EventCommand", "code": 355, "indent": 0, "parameters": []any{trainerBattleScript(tr)}})
		}
		commands = append(commands, map[string]any{"_class": "RPG::EventCommand", "code": 0, "indent": 0, "parameters": []any{}})
	} else if len(commands) == 0 {
		commands = []any{map[string]any{"_class": "RPG::EventCommand", "code": 0, "indent": 0, "parameters": []any{}}}
	}
	setEventMapCI(page, "list", commands)
	return page
}

func buildEditedEvent(name string, id, x, y int, base *EditorEvent) EditorEvent {
	pages := make([]any, 0, len(eventEditorPages))
	for _, p := range eventEditorPages {
		pages = append(pages, eventPageToNative(p))
	}
	var rawMap map[string]any
	key := strconv.Itoa(id)
	if base != nil {
		if base.NativeKey != "" {
			key = base.NativeKey
		}
		if len(base.NativeRaw) > 0 {
			if v, err := decodeJSONAny(base.NativeRaw); err == nil {
				rawMap, _ = v.(map[string]any)
				rawMap = cloneEventMap(rawMap)
			}
		}
	}
	if rawMap == nil {
		rawMap = map[string]any{"_class": "RPG::Event"}
	}
	setEventMapCI(rawMap, "id", id)
	setEventMapCI(rawMap, "name", name)
	setEventMapCI(rawMap, "x", x)
	setEventMapCI(rawMap, "y", y)
	setEventMapCI(rawMap, "pages", pages)
	if e, ok := editorEventFromNative(rawMap, key); ok {
		return e
	}
	raw, _ := json.Marshal(rawMap)
	return EditorEvent{ID: id, Name: name, X: x, Y: y, Trigger: "Interazione", Movement: "Fermo", EventKind: "Evento logico", Native: true, NativeKey: key, NativeRaw: raw}
}

func commitEventEditor(closeAfter bool) bool {
	saveEventEditorControlsToPage()
	name := strings.TrimSpace(getText(eventEditorName))
	if name == "" {
		msgbox("PML Studio - Evento", "Il nome dell'evento è obbligatorio.", MB_OK|MB_ICONERROR)
		return false
	}
	if len(eventEditorPages) == 0 {
		msgbox("PML Studio - Evento", "L'evento deve avere almeno una pagina.", MB_OK|MB_ICONERROR)
		return false
	}
	for i, p := range eventEditorPages {
		if p.Switch1Enabled && p.Switch1ID <= 0 {
			msgbox("PML Studio - Evento", fmt.Sprintf("Pagina %d: seleziona il primo interruttore oppure disattiva la condizione.", i+1), MB_OK|MB_ICONERROR)
			return false
		}
		if p.Switch2Enabled && p.Switch2ID <= 0 {
			msgbox("PML Studio - Evento", fmt.Sprintf("Pagina %d: seleziona il secondo interruttore oppure disattiva la condizione.", i+1), MB_OK|MB_ICONERROR)
			return false
		}
		if p.VariableEnabled && p.VariableID <= 0 {
			msgbox("PML Studio - Evento", fmt.Sprintf("Pagina %d: seleziona la variabile oppure disattiva la condizione.", i+1), MB_OK|MB_ICONERROR)
			return false
		}
		for _, uss := range p.UnlimitedSelfSwitches {
			if normalizeUnlimitedSelfSwitchName(uss.Name) == "" || isClassicSelfSwitchName(uss.Name) {
				msgbox("PML Studio - Evento", fmt.Sprintf("Pagina %d: condizione Unlimited Self Switch non valida.", i+1), MB_OK|MB_ICONERROR)
				return false
			}
		}
	}
	if !validateCurrentEventLegendaryUniqueness() {
		return false
	}

	if eventEditorEditingIndex >= 0 && eventEditorEditingIndex < len(events) {
		hasManagedWarp := eventEditorHasManagedWarp()
		base := events[eventEditorEditingIndex]
		e := buildEditedEvent(name, base.ID, base.X, base.Y, &base)
		events[eventEditorEditingIndex] = e
		selectedEvent = eventEditorEditingIndex
		if err := saveEventsIntoRealMap(); err != nil {
			msgbox("PML Studio - Evento", "Errore salvataggio evento: "+err.Error(), MB_OK|MB_ICONERROR)
			return false
		}
		if hasManagedWarp {
			if err := ensureWarpSourceMovementPermission(e.X, e.Y); err != nil {
				msgbox("PML Studio - Warp", "Evento salvato, ma non è stato possibile collegare la casella alla Vista Movimenti Permessi:\r\n\r\n"+err.Error(), MB_OK|MB_ICONERROR)
				return false
			}
		}
		showEvent()
		invalidate(hwndCanvas)
		setToolbarStatus(fmt.Sprintf("Evento modificato: #%d %s", e.ID, e.Name))
		eventEditorAppliedName = name
		return true
	}

	id := nextEventID()
	x, y := eventEditorTargetX, eventEditorTargetY
	hasManagedWarp := eventEditorHasManagedWarp()
	e := buildEditedEvent(name, id, x, y, nil)
	if x >= 0 && y >= 0 {
		events = append(events, e)
		selectedEvent = len(events) - 1
		if err := saveEventsIntoRealMap(); err != nil {
			msgbox("PML Studio - Evento", "Errore creazione evento: "+err.Error(), MB_OK|MB_ICONERROR)
			return false
		}
		if hasManagedWarp {
			if err := ensureWarpSourceMovementPermission(e.X, e.Y); err != nil {
				msgbox("PML Studio - Warp", "Evento creato, ma non è stato possibile collegare la casella alla Vista Movimenti Permessi:\r\n\r\n"+err.Error(), MB_OK|MB_ICONERROR)
				return false
			}
		}
		eventEditorEditingIndex = selectedEvent
		showEvent()
		invalidate(hwndCanvas)
		setToolbarStatus(fmt.Sprintf("Evento creato: #%d %s", e.ID, e.Name))
		eventEditorAppliedName = name
		return true
	}

	// Toolbar "Crea evento": no coordinate are known yet, therefore keep the
	// existing two-step flow (dialog first, then one click on the map).
	e.X, e.Y = 0, 0
	pendingGenericEvent = &e
	eventEditorAppliedName = name
	setToolbarStatus("Evento pronto: clicca sulla casella della mappa dove posizionarlo.")
	return true
}

func prepareGenericEventFromEditor() bool {
	return commitEventEditor(true)
}

func placePendingGenericEvent(x, y int) bool {
	if pendingGenericEvent == nil {
		return false
	}
	e := *pendingGenericEvent
	e.ID = nextEventID()
	e.X = x
	e.Y = y
	e.NativeKey = strconv.Itoa(e.ID)
	e.NativeRaw = patchNativeEventRaw(e)
	events = append(events, e)
	selectedEvent = len(events) - 1
	pendingGenericEvent = nil
	if err := saveEventsIntoRealMap(); err != nil {
		setToolbarStatus("Errore creazione evento: " + err.Error())
		return true
	}
	if editorEventHasManagedWarp(e) {
		if err := ensureWarpSourceMovementPermission(e.X, e.Y); err != nil {
			setToolbarStatus("Warp creato, ma errore collegamento Movimento 00: " + err.Error())
			return true
		}
	}
	showEvent()
	invalidate(hwndCanvas)
	setToolbarStatus(fmt.Sprintf("Evento creato: #%d %s", e.ID, e.Name))
	return true
}

func eventEditorWndProcFn(hwnd syscall.Handle, msg uint32, w, l uintptr) uintptr {
	switch msg {
	case WM_COMMAND:
		id := int(loword(w))
		notify := int(hiword(w))
		if id == idEvCommandList && notify == LBN_DBLCLK {
			saveEventEditorControlsToPage()
			openEventCommandPaletteAndInsert(hwnd)
			return 0
		}
		if id >= idEvPageBase && id < idEvPageBase+64 {
			idx := id - idEvPageBase
			if idx >= 0 && idx < len(eventEditorPages) {
				saveEventEditorControlsToPage()
				eventEditorPageIndex = idx
				loadEventEditorPageToControls()
			}
			return 0
		}
		switch id {
		case idEvSwitch1:
			if notify == BN_CLICKED {
				editEventSwitchCondition(1, false)
			}
			return 0
		case idEvSwitch1ID:
			if notify == BN_CLICKED && checked(eventEditorSwitch1) {
				editEventSwitchCondition(1, true)
			}
			return 0
		case idEvSwitch2:
			if notify == BN_CLICKED {
				editEventSwitchCondition(2, false)
			}
			return 0
		case idEvSwitch2ID:
			if notify == BN_CLICKED && checked(eventEditorSwitch2) {
				editEventSwitchCondition(2, true)
			}
			return 0
		case idEvVariable:
			if notify == BN_CLICKED {
				editEventVariableCondition(false)
			}
			return 0
		case idEvVariableID:
			if notify == BN_CLICKED && checked(eventEditorVariable) {
				editEventVariableCondition(true)
			}
			return 0
		case idEvUnlimitedSelfSwitch:
			if notify == BN_CLICKED {
				editCurrentPageUnlimitedSelfSwitchConditions()
			}
			return 0
		case idEvNewPage:
			newEventPage()
			return 0
		case idEvCopyPage:
			copyEventPage()
			return 0
		case idEvPastePage:
			pasteEventPage()
			return 0
		case idEvDeletePage:
			deleteEventPage()
			return 0
		case idEvClearPage:
			clearEventPage()
			return 0
		case idEvChooseGraphic:
			chooseEventGraphicFromPreview()
			return 0
		case idEvOK:
			if commitEventEditor(true) {
				pDestroyWindow.Call(uintptr(hwnd))
			}
			return 0
		case idEvApply:
			if eventEditorEditingIndex >= 0 || (eventEditorTargetX >= 0 && eventEditorTargetY >= 0) {
				commitEventEditor(false)
			} else {
				saveEventEditorControlsToPage()
				eventEditorAppliedName = strings.TrimSpace(getText(eventEditorName))
				setToolbarStatus("Modifiche evento applicate nella finestra; posizionalo con OK e poi clic sulla mappa.")
			}
			return 0
		case idEvCancel:
			pDestroyWindow.Call(uintptr(hwnd))
			return 0
		}
	case WM_CLOSE:
		pDestroyWindow.Call(uintptr(hwnd))
		return 0
	case WM_DESTROY:
		eventEditorOpen = false
		eventEditorWindow = 0
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(uintptr(hwnd), uintptr(msg), w, l)
	return r
}

var eventEditorWndProc = syscall.NewCallback(eventEditorWndProcFn)

func ensureEventEditorClass() error {
	if eventEditorRegistered {
		return nil
	}
	h, _, _ := pGetModuleHandleW.Call(0)
	hInst := syscall.Handle(h)
	cursor, _, _ := pLoadCursorW.Call(0, IDC_ARROW)
	brush, _, _ := pCreateSolidBrush.Call(rgb(244, 244, 244))
	wc := WNDCLASSEX{cbSize: uint32(unsafe.Sizeof(WNDCLASSEX{})), lpfnWndProc: eventEditorWndProc, hInstance: hInst, hCursor: syscall.Handle(cursor), hbrBackground: syscall.Handle(brush), lpszClassName: wstr(eventEditorClassName)}
	r, _, err := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	if r == 0 {
		return fmt.Errorf("registrazione editor evento fallita: %v", err)
	}
	eventEditorRegistered = true
	return nil
}

func showCreateEventEditorDialog() { showEventEditorDialog(-1, -1, -1) }

func showCreateEventEditorDialogAt(x, y int) { showEventEditorDialog(-1, x, y) }

func showEditEventEditorDialog(index int) { showEventEditorDialog(index, -1, -1) }

func showEventEditorDialog(editIndex, targetX, targetY int) {
	if currentMap == nil || currentMapDoc == nil {
		msgbox("PML Studio - Eventi", "Apri prima una mappa.", MB_OK|MB_ICONINFORMATION)
		return
	}
	if eventEditorOpen {
		return
	}
	if err := ensureEventEditorClass(); err != nil {
		msgbox("PML Studio - Eventi", err.Error(), MB_OK|MB_ICONERROR)
		return
	}
	if err := ensureEventGraphicPreviewClass(); err != nil {
		msgbox("PML Studio - Eventi", err.Error(), MB_OK|MB_ICONERROR)
		return
	}
	ensureCharacterCatalog()
	invalidateEventConditionCatalogCache()
	eventEditorSprites = characterSpriteNames()
	eventEditorEditingIndex = -1
	eventEditorTargetX, eventEditorTargetY = targetX, targetY
	eventEditorPageIndex = 0
	eventEditorPageClipboard = nil
	eventEditorAppliedName = ""
	initialName := fmt.Sprintf("EV%03d", nextEventID())
	windowID := nextEventID()
	if editIndex >= 0 && editIndex < len(events) {
		eventEditorEditingIndex = editIndex
		e := events[editIndex]
		initialName = e.Name
		windowID = e.ID
		eventEditorPages = eventDraftsFromEditorEvent(e)
	} else {
		eventEditorPages = []eventPageDraft{defaultEventPageDraft()}
	}
	const ww, wh int32 = 1180, 930
	x, y := centerOwnedWindow(ww, wh)
	h, _, _ := pGetModuleHandleW.Call(0)
	hInst := syscall.Handle(h)
	eventEditorOpen = true
	eventEditorWindow = createWindow(eventEditorClassName, fmt.Sprintf("Modifica evento - ID:%03d", windowID), WS_OVERLAPPEDWINDOW|WS_VISIBLE|WS_CLIPCHILDREN, x, y, ww, wh, hwndMain, 0, hInst)
	if eventEditorWindow == 0 {
		eventEditorOpen = false
		return
	}
	setWindowIcon(eventEditorWindow)

	createWindow("STATIC", "Nome:", WS_CHILD|WS_VISIBLE, 18, 20, 60, 26, eventEditorWindow, 3000, hInst)
	eventEditorName = createWindow("EDIT", initialName, WS_CHILD|WS_VISIBLE|WS_BORDER|WS_TABSTOP, 82, 15, 220, 32, eventEditorWindow, idEvName, hInst)
	createWindow("BUTTON", "Nuova\npagina evento", WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_MULTILINE, 330, 10, 125, 54, eventEditorWindow, idEvNewPage, hInst)
	createWindow("BUTTON", "Copia\npagina evento", WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_MULTILINE, 465, 10, 125, 54, eventEditorWindow, idEvCopyPage, hInst)
	createWindow("BUTTON", "Incolla\npagina evento", WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_MULTILINE, 600, 10, 125, 54, eventEditorWindow, idEvPastePage, hInst)
	createWindow("BUTTON", "Elimina\npagina evento", WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_MULTILINE, 735, 10, 125, 54, eventEditorWindow, idEvDeletePage, hInst)
	createWindow("BUTTON", "Svuota\npagina evento", WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_MULTILINE, 870, 10, 125, 54, eventEditorWindow, idEvClearPage, hInst)
	createWindow("BUTTON", "Pagina evento", WS_CHILD|WS_VISIBLE|BS_GROUPBOX, 12, 96, 1138, 748, eventEditorWindow, 3001, hInst)

	// Condizioni: nessun numero predefinito visibile. La checkbox abilita la
	// condizione e apre il selettore dei dati reali del progetto.
	createWindow("BUTTON", "Condizioni", WS_CHILD|WS_VISIBLE|BS_GROUPBOX, 30, 155, 420, 270, eventEditorWindow, 3010, hInst)
	eventEditorSwitch1 = createWindow("BUTTON", "Interruttore 1", WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_AUTOCHECKBOX, 45, 185, 125, 30, eventEditorWindow, idEvSwitch1, hInst)
	eventEditorSwitch1ID = createWindow("BUTTON", "Seleziona...", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 175, 182, 190, 32, eventEditorWindow, idEvSwitch1ID, hInst)
	eventEditorSwitch1State = createWindow("STATIC", "", WS_CHILD|WS_VISIBLE, 375, 189, 55, 24, eventEditorWindow, 3011, hInst)
	eventEditorSwitch2 = createWindow("BUTTON", "Interruttore 2", WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_AUTOCHECKBOX, 45, 220, 125, 30, eventEditorWindow, idEvSwitch2, hInst)
	eventEditorSwitch2ID = createWindow("BUTTON", "Seleziona...", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 175, 217, 190, 32, eventEditorWindow, idEvSwitch2ID, hInst)
	eventEditorSwitch2State = createWindow("STATIC", "", WS_CHILD|WS_VISIBLE, 375, 224, 55, 24, eventEditorWindow, 3012, hInst)
	eventEditorVariable = createWindow("BUTTON", "Variabile", WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_AUTOCHECKBOX, 45, 255, 125, 30, eventEditorWindow, idEvVariable, hInst)
	eventEditorVariableID = createWindow("BUTTON", "Seleziona...", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 175, 252, 190, 32, eventEditorWindow, idEvVariableID, hInst)
	eventEditorVariableValue = createWindow("STATIC", "", WS_CHILD|WS_VISIBLE, 375, 259, 65, 24, eventEditorWindow, idEvVariableValue, hInst)
	createWindow("STATIC", "La variabile si può provare direttamente nel selettore.", WS_CHILD|WS_VISIBLE, 45, 291, 380, 26, eventEditorWindow, 3014, hInst)
	eventEditorSelfSwitch = createWindow("BUTTON", "Self Switch", WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_AUTOCHECKBOX, 45, 332, 135, 30, eventEditorWindow, idEvSelfSwitch, hInst)
	eventEditorSelfSwitchValue = createWindow("COMBOBOX", "", WS_CHILD|WS_VISIBLE|WS_TABSTOP|CBS_DROPDOWNLIST, 190, 328, 105, 180, eventEditorWindow, idEvSelfSwitchValue, hInst)
	setComboFromStrings(eventEditorSelfSwitchValue, []string{"A", "B", "C", "D"}, 0)
	pSendMessageW.Call(uintptr(eventEditorSelfSwitchValue), CB_SETITEMHEIGHT, ^uintptr(0), 24)
	pSendMessageW.Call(uintptr(eventEditorSelfSwitchValue), CB_SETITEMHEIGHT, 0, 24)
	createWindow("STATIC", "è ON", WS_CHILD|WS_VISIBLE, 310, 337, 65, 24, eventEditorWindow, 3015, hInst)
	eventEditorUnlimitedSelfSwitch = createWindow("BUTTON", "Unlimited Switch...", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 45, 370, 145, 32, eventEditorWindow, idEvUnlimitedSelfSwitch, hInst)
	eventEditorUnlimitedSelfSwitchState = createWindow("STATIC", "Nessuna", WS_CHILD|WS_VISIBLE, 200, 376, 225, 24, eventEditorWindow, 3016, hInst)

	// Grafica: come RPG Maker XP, il riquadro si apre con doppio click.
	createWindow("BUTTON", "Grafica", WS_CHILD|WS_VISIBLE|BS_GROUPBOX, 30, 430, 210, 230, eventEditorWindow, 3020, hInst)
	eventEditorGraphic = createWindow(eventGraphicPreviewClassName, "", WS_CHILD|WS_VISIBLE|WS_BORDER|WS_TABSTOP, 42, 460, 186, 146, eventEditorWindow, idEvGraphic, hInst)
	createWindow("STATIC", "Doppio click: scegli/importa", WS_CHILD|WS_VISIBLE, 42, 612, 186, 24, eventEditorWindow, 3021, hInst)
	eventEditorRemoveTransparency = 0
	eventEditorLightEffect = createWindow("BUTTON", "Effetto luce", WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_AUTOCHECKBOX, 42, 634, 175, 28, eventEditorWindow, idEvLightEffect, hInst)

	createWindow("BUTTON", "Movimento autonomo", WS_CHILD|WS_VISIBLE|BS_GROUPBOX, 250, 430, 220, 230, eventEditorWindow, 3030, hInst)
	createWindow("STATIC", "Tipo:", WS_CHILD|WS_VISIBLE, 265, 460, 55, 24, eventEditorWindow, 3031, hInst)
	eventEditorMoveType = createWindow("COMBOBOX", "", WS_CHILD|WS_VISIBLE|WS_TABSTOP|CBS_DROPDOWNLIST, 325, 455, 130, 190, eventEditorWindow, idEvMoveType, hInst)
	setComboFromStrings(eventEditorMoveType, []string{"Fisso", "Casuale", "Avvicina", "Personalizzato"}, 0)
	pSendMessageW.Call(uintptr(eventEditorMoveType), CB_SETITEMHEIGHT, ^uintptr(0), 24)
	pSendMessageW.Call(uintptr(eventEditorMoveType), CB_SETITEMHEIGHT, 0, 24)
	eventEditorMoveRoute = createWindow("BUTTON", "Percorso movimento...", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 265, 495, 190, 34, eventEditorWindow, idEvMoveRoute, hInst)
	createWindow("STATIC", "Velocità:", WS_CHILD|WS_VISIBLE, 265, 540, 70, 24, eventEditorWindow, 3032, hInst)
	eventEditorMoveSpeed = createWindow("COMBOBOX", "", WS_CHILD|WS_VISIBLE|WS_TABSTOP|CBS_DROPDOWNLIST, 340, 535, 115, 190, eventEditorWindow, idEvMoveSpeed, hInst)
	setComboFromStrings(eventEditorMoveSpeed, []string{"1: Molto lenta", "2: Lenta", "3: Normale", "4: Veloce", "5: Molto veloce", "6: Massima"}, 2)
	pSendMessageW.Call(uintptr(eventEditorMoveSpeed), CB_SETITEMHEIGHT, ^uintptr(0), 24)
	pSendMessageW.Call(uintptr(eventEditorMoveSpeed), CB_SETITEMHEIGHT, 0, 24)
	createWindow("STATIC", "Frequenza:", WS_CHILD|WS_VISIBLE, 265, 580, 80, 24, eventEditorWindow, 3033, hInst)
	eventEditorMoveFrequency = createWindow("COMBOBOX", "", WS_CHILD|WS_VISIBLE|WS_TABSTOP|CBS_DROPDOWNLIST, 350, 575, 105, 190, eventEditorWindow, idEvMoveFrequency, hInst)
	setComboFromStrings(eventEditorMoveFrequency, []string{"1: Minima", "2: Bassa", "3: Normale", "4: Alta", "5: Massima"}, 2)
	pSendMessageW.Call(uintptr(eventEditorMoveFrequency), CB_SETITEMHEIGHT, ^uintptr(0), 24)
	pSendMessageW.Call(uintptr(eventEditorMoveFrequency), CB_SETITEMHEIGHT, 0, 24)

	createWindow("BUTTON", "Opzioni", WS_CHILD|WS_VISIBLE|BS_GROUPBOX, 30, 670, 210, 150, eventEditorWindow, 3040, hInst)
	eventEditorWalkAnim = createWindow("BUTTON", "Animazione movimento", WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_AUTOCHECKBOX, 42, 695, 190, 26, eventEditorWindow, idEvWalkAnim, hInst)
	eventEditorStepAnim = createWindow("BUTTON", "Animazione da fermo", WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_AUTOCHECKBOX, 42, 723, 190, 26, eventEditorWindow, idEvStepAnim, hInst)
	eventEditorDirectionFix = createWindow("BUTTON", "Direzione fissa", WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_AUTOCHECKBOX, 42, 751, 190, 26, eventEditorWindow, idEvDirectionFix, hInst)
	eventEditorThrough = createWindow("BUTTON", "Attraversabile", WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_AUTOCHECKBOX, 42, 779, 190, 26, eventEditorWindow, idEvThrough, hInst)
	eventEditorAlwaysTop = createWindow("BUTTON", "Sempre in primo piano", WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_AUTOCHECKBOX, 42, 807, 190, 26, eventEditorWindow, idEvAlwaysTop, hInst)

	createWindow("BUTTON", "Attivazione", WS_CHILD|WS_VISIBLE|BS_GROUPBOX, 250, 670, 220, 150, eventEditorWindow, 3050, hInst)
	eventEditorTriggerAction = createWindow("BUTTON", "Tasto azione", WS_CHILD|WS_VISIBLE|WS_TABSTOP|WS_GROUP|BS_AUTORADIOBUTTON, 265, 693, 190, 26, eventEditorWindow, idEvTriggerAction, hInst)
	eventEditorTriggerPlayerTouch = createWindow("BUTTON", "Contatto giocatore", WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_AUTORADIOBUTTON, 265, 721, 190, 26, eventEditorWindow, idEvTriggerPlayerTouch, hInst)
	eventEditorTriggerEventTouch = createWindow("BUTTON", "Contatto evento", WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_AUTORADIOBUTTON, 265, 749, 190, 26, eventEditorWindow, idEvTriggerEventTouch, hInst)
	eventEditorTriggerAutorun = createWindow("BUTTON", "Avvio automatico", WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_AUTORADIOBUTTON, 265, 777, 190, 26, eventEditorWindow, idEvTriggerAutorun, hInst)
	eventEditorTriggerParallel = createWindow("BUTTON", "Processo parallelo", WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_AUTORADIOBUTTON, 265, 805, 190, 26, eventEditorWindow, idEvTriggerParallel, hInst)

	// Commands - high level editor. Double-clicking this list opens the
	// dedicated command palette. This keeps the event editor compact and gives
	// us one scalable place for every future RPG Maker/Essentials command.
	createWindow("STATIC", "Lista comandi evento:", WS_CHILD|WS_VISIBLE, 500, 160, 210, 26, eventEditorWindow, 3060, hInst)
	eventEditorCommands = createWindow("LISTBOX", "", WS_CHILD|WS_VISIBLE|WS_BORDER|WS_VSCROLL|WS_TABSTOP|LBS_NOTIFY|LBS_NOINTEGRALHEIGHT, 500, 190, 620, 610, eventEditorWindow, idEvCommandList, hInst)
	createWindow("STATIC", "Doppio clic sulla lista per aprire Comandi evento e inserire una nuova azione.", WS_CHILD|WS_VISIBLE, 500, 808, 620, 28, eventEditorWindow, 3061, hInst)

	createWindow("BUTTON", "OK", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 850, 865, 95, 38, eventEditorWindow, idEvOK, hInst)
	createWindow("BUTTON", "Annulla", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 955, 865, 100, 38, eventEditorWindow, idEvCancel, hInst)
	createWindow("BUTTON", "Applica", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 1065, 865, 85, 38, eventEditorWindow, idEvApply, hInst)

	loadEventEditorPageToControls()
	refreshEventEditorPageButtons()
	pEnableWindow.Call(uintptr(hwndMain), 0)
	pShowWindow.Call(uintptr(eventEditorWindow), SW_SHOW)
	pUpdateWindow.Call(uintptr(eventEditorWindow))
	var m MSG
	repostQuit := false
	for eventEditorOpen {
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
	pEnableWindow.Call(uintptr(hwndMain), 1)
	pSetFocus.Call(uintptr(hwndMain))
	if repostQuit {
		pPostQuitMessage.Call(0)
	}
}
