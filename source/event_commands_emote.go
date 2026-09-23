//go:build windows

package main

import "strings"

func addEmoteEventCommand() {
	v, ok := showSimpleCommandForm(eventEditorWindow, "Emote evento", "Mostra un'emote sopra il giocatore o un NPC. Il runtime deve completare l'animazione prima di proseguire se 'Attendi' è attivo.", []simpleFormField{
		{Key: "target", Label: "Bersaglio", Kind: simpleFormCombo, Options: []string{"NPC / Evento", "Giocatore"}},
		{Key: "event", Label: "ID NPC (0 = evento corrente)", Kind: simpleFormNumber, Initial: "0"},
		{Key: "emote", Label: "Emote", Kind: simpleFormCombo, Options: []string{"! Esclamazione", "? Domanda", "... Pensiero", "♥ Cuore", "♪ Musica", "Rabbia", "Sudore", "Idea"}},
		{Key: "duration", Label: "Durata (frame)", Kind: simpleFormNumber, Initial: "30"}, {Key: "wait", Label: "Attendi fine emote", Kind: simpleFormCheck, Initial: "true"},
	})
	if !ok {
		return
	}
	target := "event"
	if strings.EqualFold(v["target"], "Giocatore") {
		target = "player"
	}
	em := strings.TrimSpace(v["emote"])
	if i := strings.Index(em, " "); i > 0 {
		em = em[:i]
	}
	if em == "" {
		return
	}
	c := plmManagedEventCommand{Type: "emote", Target: target, TargetEventID: formInt(v, "event", 0), Action: em, Duration: maxIntEventCmd(formInt(v, "duration", 30), 1), Wait: formBool(v, "wait")}
	if appendManagedEventCommand(c) {
		setToolbarStatus("Emote aggiunta: " + em)
	}
}
