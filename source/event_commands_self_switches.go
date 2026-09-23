//go:build windows

package main

import (
	"fmt"
	"regexp"
	"strings"
)

// Unlimited Self Switches are local to a single event, exactly like classic
// A/B/C/D Self Switches, but their key is an arbitrary author-defined name.
// The canonical page-condition format intentionally matches the historical
// Essentials USS convention so imported projects and the Python runtime share
// the same representation:  Switch: NAME: on/off

type plmNamedSelfSwitchCondition struct {
	Name string
	On   bool
}

var unlimitedSelfSwitchLineRE = regexp.MustCompile(`(?i)^\s*Switch\s*:\s*([^:]+?)\s*(?::\s*(on|off))?\s*$`)

func normalizeUnlimitedSelfSwitchName(name string) string {
	name = strings.TrimSpace(name)
	name = strings.Join(strings.Fields(name), "_")
	return strings.ToUpper(name)
}

func isClassicSelfSwitchName(name string) bool {
	switch strings.ToUpper(strings.TrimSpace(name)) {
	case "A", "B", "C", "D":
		return true
	}
	return false
}

func parseUnlimitedSelfSwitchLine(text string) (plmNamedSelfSwitchCondition, bool) {
	m := unlimitedSelfSwitchLineRE.FindStringSubmatch(strings.TrimSpace(text))
	if len(m) == 0 {
		return plmNamedSelfSwitchCondition{}, false
	}
	name := normalizeUnlimitedSelfSwitchName(m[1])
	if name == "" || isClassicSelfSwitchName(name) {
		return plmNamedSelfSwitchCondition{}, false
	}
	on := true // legacy "Switch: NAME" means ON
	if len(m) > 2 && strings.EqualFold(strings.TrimSpace(m[2]), "off") {
		on = false
	}
	return plmNamedSelfSwitchCondition{Name: name, On: on}, true
}

func unlimitedSelfSwitchConditionText(c plmNamedSelfSwitchCondition) string {
	state := "on"
	if !c.On {
		state = "off"
	}
	return fmt.Sprintf("Switch: %s: %s", normalizeUnlimitedSelfSwitchName(c.Name), state)
}

func unlimitedSelfSwitchConditionsFromCommands(commands []any) []plmNamedSelfSwitchCondition {
	out := []plmNamedSelfSwitchCondition{}
	seen := map[string]bool{}
	// USS conditions are only page-header comments. Stop at the first command
	// that is not a comment, matching Essentials' page-condition behavior.
	for _, raw := range commands {
		m, ok := raw.(map[string]any)
		if !ok {
			break
		}
		code := commandMapCode(m)
		if code != 108 && code != 408 {
			break
		}
		pv, ok := anyMapValueCI(m, "parameters")
		if !ok {
			continue
		}
		arr, ok := pv.([]any)
		if !ok || len(arr) == 0 {
			continue
		}
		c, ok := parseUnlimitedSelfSwitchLine(anyString(arr[0]))
		if !ok {
			continue
		}
		key := c.Name + fmt.Sprintf(":%t", c.On)
		if !seen[key] {
			seen[key] = true
			out = append(out, c)
		}
	}
	return out
}

func stripUnlimitedSelfSwitchConditionComments(commands []any) []any {
	out := make([]any, 0, len(commands))
	for _, raw := range commands {
		m, ok := raw.(map[string]any)
		if !ok {
			out = append(out, raw)
			continue
		}
		code := commandMapCode(m)
		if code == 108 || code == 408 {
			if pv, ok := anyMapValueCI(m, "parameters"); ok {
				if arr, ok := pv.([]any); ok && len(arr) > 0 {
					if _, matched := parseUnlimitedSelfSwitchLine(anyString(arr[0])); matched {
						continue
					}
				}
			}
		}
		out = append(out, raw)
	}
	return out
}

func prependUnlimitedSelfSwitchConditionComments(commands []any, conditions []plmNamedSelfSwitchCondition) []any {
	commands = stripUnlimitedSelfSwitchConditionComments(commands)
	if len(conditions) == 0 {
		return commands
	}
	prefix := make([]any, 0, len(conditions))
	for i, c := range conditions {
		c.Name = normalizeUnlimitedSelfSwitchName(c.Name)
		if c.Name == "" || isClassicSelfSwitchName(c.Name) {
			continue
		}
		code := 108
		if i > 0 {
			code = 408
		}
		prefix = append(prefix, map[string]any{
			"_class":     "RPG::EventCommand",
			"code":       code,
			"indent":     0,
			"parameters": []any{unlimitedSelfSwitchConditionText(c)},
		})
	}
	if len(prefix) == 0 {
		return commands
	}
	return append(prefix, commands...)
}

func formatUnlimitedSelfSwitchConditions(conditions []plmNamedSelfSwitchCondition) string {
	lines := make([]string, 0, len(conditions))
	for _, c := range conditions {
		state := "ON"
		if !c.On {
			state = "OFF"
		}
		lines = append(lines, normalizeUnlimitedSelfSwitchName(c.Name)+" = "+state)
	}
	return strings.Join(lines, "\r\n")
}

func parseUnlimitedSelfSwitchConditionsEditor(text string) ([]plmNamedSelfSwitchCondition, string) {
	out := []plmNamedSelfSwitchCondition{}
	seen := map[string]bool{}
	for lineNo, line := range strings.Split(strings.ReplaceAll(text, "\r", ""), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Sprintf("Riga %d: usa il formato NOME = ON oppure NOME = OFF.", lineNo+1)
		}
		name := normalizeUnlimitedSelfSwitchName(parts[0])
		if name == "" {
			return nil, fmt.Sprintf("Riga %d: nome Unlimited Self Switch mancante.", lineNo+1)
		}
		if isClassicSelfSwitchName(name) {
			return nil, fmt.Sprintf("Riga %d: %s è una Self Switch classica. Usa il controllo A/B/C/D dedicato.", lineNo+1, name)
		}
		state := strings.ToUpper(strings.TrimSpace(parts[1]))
		if state != "ON" && state != "OFF" {
			return nil, fmt.Sprintf("Riga %d: lo stato deve essere ON oppure OFF.", lineNo+1)
		}
		key := name + ":" + state
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, plmNamedSelfSwitchCondition{Name: name, On: state == "ON"})
	}
	return out, ""
}

func editCurrentPageUnlimitedSelfSwitchConditions() {
	if eventEditorPageIndex < 0 || eventEditorPageIndex >= len(eventEditorPages) {
		return
	}
	saveEventEditorControlsToPage()
	p := &eventEditorPages[eventEditorPageIndex]
	initial := formatUnlimitedSelfSwitchConditions(p.UnlimitedSelfSwitches)
	text, ok := showExtendedMultilineDialog(
		eventEditorWindow,
		"Unlimited Self Switch - Condizioni pagina",
		"Una condizione per riga:",
		"Formato: NOME = ON oppure NOME = OFF. Puoi inserirne più di una: devono risultare tutte vere. A/B/C/D restano Self Switch classiche separate.",
		initial,
	)
	if !ok {
		return
	}
	parsed, problem := parseUnlimitedSelfSwitchConditionsEditor(text)
	if problem != "" {
		msgbox("PML Studio - Unlimited Self Switch", problem, MB_OK|MB_ICONINFORMATION)
		return
	}
	p.UnlimitedSelfSwitches = parsed
	refreshUnlimitedSelfSwitchConditionStatus()
	if len(parsed) == 0 {
		setToolbarStatus("Condizioni Unlimited Self Switch rimosse dalla pagina.")
	} else {
		setToolbarStatus(fmt.Sprintf("Pagina: %d condizioni Unlimited Self Switch.", len(parsed)))
	}
}

func refreshUnlimitedSelfSwitchConditionStatus() {
	if eventEditorUnlimitedSelfSwitchState == 0 || eventEditorPageIndex < 0 || eventEditorPageIndex >= len(eventEditorPages) {
		return
	}
	n := len(eventEditorPages[eventEditorPageIndex].UnlimitedSelfSwitches)
	if n == 0 {
		setText(eventEditorUnlimitedSelfSwitchState, "Nessuna")
	} else if n == 1 {
		c := eventEditorPages[eventEditorPageIndex].UnlimitedSelfSwitches[0]
		state := "ON"
		if !c.On {
			state = "OFF"
		}
		setText(eventEditorUnlimitedSelfSwitchState, c.Name+" = "+state)
	} else {
		setText(eventEditorUnlimitedSelfSwitchState, fmt.Sprintf("%d condizioni", n))
	}
}

func addClassicSelfSwitchEventCommand() {
	v, ok := showSimpleCommandForm(eventEditorWindow, "Self Switch classica", "Le Self Switch classiche appartengono al singolo evento e sono esattamente A, B, C e D come in RPG Maker XP.", []simpleFormField{
		{Key: "switch", Label: "Self Switch", Kind: simpleFormCombo, Options: []string{"A", "B", "C", "D"}},
		{Key: "state", Label: "Stato", Kind: simpleFormCombo, Options: []string{"ON", "OFF"}},
	})
	if !ok {
		return
	}
	sw := strings.ToUpper(strings.TrimSpace(v["switch"]))
	if !isClassicSelfSwitchName(sw) {
		return
	}
	state := strings.ToUpper(strings.TrimSpace(v["state"]))
	if state != "OFF" {
		state = "ON"
	}
	if appendManagedEventCommand(plmManagedEventCommand{Type: "self_switch", SelfSwitch: sw, State: state}) {
		setToolbarStatus("Comando aggiunto: Self Switch classica " + sw + " → " + state + ".")
	}
}

func addUnlimitedSelfSwitchEventCommand() {
	v, ok := showSimpleCommandForm(eventEditorWindow, "Unlimited Self Switch", "Crea o modifica una Self Switch locale all'evento con un nome libero. Puoi crearne quante ne servono senza consumare A/B/C/D o Switch globali.", []simpleFormField{
		{Key: "name", Label: "Nome", Kind: simpleFormText, Initial: "REBATTLE_READY"},
		{Key: "state", Label: "Azione", Kind: simpleFormCombo, Options: []string{"ON", "OFF", "TOGGLE"}},
	})
	if !ok {
		return
	}
	name := normalizeUnlimitedSelfSwitchName(v["name"])
	if name == "" {
		msgbox("PML Studio - Unlimited Self Switch", "Inserisci un nome.", MB_OK|MB_ICONINFORMATION)
		return
	}
	if isClassicSelfSwitchName(name) {
		msgbox("PML Studio - Unlimited Self Switch", "A/B/C/D sono Self Switch classiche. Usa il comando dedicato.", MB_OK|MB_ICONINFORMATION)
		return
	}
	state := strings.ToUpper(strings.TrimSpace(v["state"]))
	if state != "OFF" && state != "TOGGLE" {
		state = "ON"
	}
	if appendManagedEventCommand(plmManagedEventCommand{Type: "unlimited_self_switch", SelfSwitch: name, State: state}) {
		setToolbarStatus("Comando aggiunto: Unlimited Self Switch " + name + " → " + state + ".")
	}
}
