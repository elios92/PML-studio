//go:build windows

package main

import (
	"fmt"

	"syscall"
)

var settingsMenuActions map[uint16]func()

func populateSettingsMenu(menu syscall.Handle) {
	settingsMenuActions = map[uint16]func(){}
	next := uint16(30000)
	sub := func(parent syscall.Handle, label string) syscall.Handle {
		h, _, _ := pCreatePopupMenu.Call()
		appendPopup(parent, syscall.Handle(h), label)
		return syscall.Handle(h)
	}
	item := func(parent syscall.Handle, label string, checked bool, action func()) {
		flags := uintptr(MF_STRING)
		if checked {
			flags |= 8
		}
		if action == nil {
			flags |= 1
		}
		appendMenu(parent, flags, uintptr(next), label)
		if action != nil {
			settingsMenuActions[next] = action
		}
		next++
	}
	toggle := func(parent syscall.Handle, label string, value bool, set func(*Settings, bool)) {
		item(parent, label, value, func() { changeEditorPreferences(func(s *Settings) { set(s, !value) }) })
	}
	g := sub(menu, "Generale")
	l := sub(g, "Lingua dell'editor")
	item(l, "Italiano", true, nil)
	item(l, "Altre lingue: cataloghi non disponibili", false, nil)
	l = sub(g, "All'avvio")
	for _, o := range []struct{ label, value string }{{"Editor vuoto", "empty"}, {"Riapri ultimo progetto", "last"}, {"Scegli progetto", "open"}} {
		o := o
		item(l, o.label, settings.Startup == o.value, func() { changeEditorPreferences(func(s *Settings) { s.Startup = o.value }) })
	}
	toggle(g, "Conferma uscita dall'editor", settings.ConfirmExit, func(s *Settings, v bool) { s.ConfirmExit = v })
	item(g, "Modifiche non salvate: conferma sempre attiva", true, nil)
	g = sub(menu, "Interfaccia")
	l = sub(g, "Tema")
	for _, o := range []struct{ label, value string }{{"Chiaro", "light"}, {"Scuro", "dark"}, {"Sistema", "system"}} {
		o := o
		item(l, o.label, settings.Theme == o.value, func() { changeEditorPreferences(func(s *Settings) { s.Theme = o.value }) })
	}
	l = sub(g, "Skin")
	for _, o := range []struct{ label, value string }{{"Classica Windows 10", "windows10"}, {"Scura", "dark"}, {"Blu", "blue"}, {"Grigio professionale", "windows10_gray"}, {"Verde tenue", "windows10_soft"}} {
		o := o
		item(l, o.label, settings.UISkin == o.value, func() {
			changeEditorPreferences(func(s *Settings) {
				font, scale := s.UIFontSize, s.UIScale
				resetAppearance(s)
				s.UIFontSize, s.UIScale = font, scale
				s.UISkin = o.value
				if o.value == "dark" {
					s.Theme = "dark"
				}
			})
		})
	}
	l = sub(g, "Colore principale")
	for _, o := range []struct{ label, value string }{{"Della skin", ""}, {"Blu", "#2F6FED"}, {"Verde", "#16856B"}, {"Viola", "#8355C5"}, {"Arancio", "#C76717"}, {"Rosso", "#BD4050"}} {
		o := o
		item(l, o.label, settings.AccentColor == o.value, func() { changeEditorPreferences(func(s *Settings) { s.AccentColor = o.value; s.SelectionColor = "" }) })
	}
	l = sub(g, "Dimensione testo")
	for _, n := range []int{14, 16, 18} {
		n := n
		item(l, fmt.Sprintf("%d px", n), settings.UIFontSize == n, func() { changeEditorPreferences(func(s *Settings) { s.UIFontSize = n }) })
	}
	l = sub(g, "Dimensione interfaccia")
	for _, n := range []int{100, 110, 125} {
		n := n
		item(l, fmt.Sprintf("%d%%", n), settings.UIScale == n, func() { changeEditorPreferences(func(s *Settings) { s.UIScale = n }) })
	}
	item(g, "Personalizza skin...", false, showUISettingsDialog)
	item(g, "Ripristina layout pannelli", false, resetResizableUILayout)
	item(g, "Ripristina skin ufficiale", false, func() { changeEditorPreferences(resetAppearance) })
	g = sub(menu, "Editor")
	toggle(g, "Griglia", settings.GridDefault, func(s *Settings, v bool) { s.GridDefault = v })
	l = sub(g, "Salvataggio automatico")
	for _, n := range []int{0, 1, 5, 10} {
		n := n
		label := "Disattivato"
		if n > 0 {
			label = fmt.Sprintf("Ogni %d minuti", n)
		}
		item(l, label, settings.AutosaveMinutes == n, func() { changeEditorPreferences(func(s *Settings) { s.AutosaveMinutes = n }) })
	}
	toggle(g, "Backup dati prima di Salva progetto", settings.BackupBeforeSave, func(s *Settings, v bool) { s.BackupBeforeSave = v })
	item(g, "Crea backup dati adesso...", false, func() {
		if currentProject == "" {
			msgbox("Backup", "Apri prima un progetto.", MB_OK)
			return
		}
		p, e := backupProjectData(currentProject, settings.BackupPath)
		if e != nil {
			msgbox("Backup", e.Error(), MB_OK|MB_ICONERROR)
		} else {
			msgbox("Backup dati", "Mappe, database e script salvati in:\r\n"+p+"\r\n\r\nImmagini e audio esclusi.", MB_OK)
		}
	})
	l = sub(g, "Strumento predefinito")
	for _, o := range []struct{ label, value string }{{"Matita", "pencil"}, {"Selezione", "select"}, {"Rettangolo", "rectangle"}} {
		o := o
		item(l, o.label, settings.DefaultTool == o.value, func() { changeEditorPreferences(func(s *Settings) { s.DefaultTool = o.value }) })
	}
	g = sub(menu, "Playtest")
	toggle(g, "Modalità debug", settings.PlaytestDebug, func(s *Settings, v bool) { s.PlaytestDebug = v })
	toggle(g, "Avvia dalla mappa corrente", settings.PlaytestCurrentMap, func(s *Settings, v bool) { s.PlaytestCurrentMap = v })
	toggle(g, "Conferma prima dell'avvio", settings.PlaytestConfirm, func(s *Settings, v bool) { s.PlaytestConfirm = v })
	toggle(g, "Mostra console Python", settings.PlaytestConsole, func(s *Settings, v bool) { s.PlaytestConsole = v })
	item(g, "Salva e verifica dati prima del test: sempre", true, nil)
	g = sub(menu, "Percorsi")
	for _, o := range []struct {
		label, value string
		set          func(*Settings, string)
	}{
		{"Progetti", settings.ProjectsPath, func(s *Settings, v string) { s.ProjectsPath = v }},
		{"Backup dati", settings.BackupPath, func(s *Settings, v string) { s.BackupPath = v }},
		{"Log playtest", settings.PlaytestLogsPath, func(s *Settings, v string) { s.PlaytestLogsPath = v }},
	} {
		o := o
		pathMenu := sub(g, o.label)
		value := o.value
		if value == "" {
			value = "Automatico"
		}
		item(pathMenu, value, false, nil)
		item(pathMenu, "Scegli cartella...", false, func() {
			p := browseFolder("Cartella " + o.label)
			if p != "" {
				changeEditorPreferences(func(s *Settings) { o.set(s, p) })
			}
		})
		item(pathMenu, "Ripristina percorso automatico", o.value == "", func() { changeEditorPreferences(func(s *Settings) { o.set(s, "") }) })
	}
	item(g, "Build: generatore non ancora disponibile", false, nil)
	g = sub(menu, "Modalità PML Studio")
	for _, m := range []string{projectModeStandard, projectModeAdvanced} {
		m := m
		item(g, projectEditorModeLabel(m), currentProjectEditorMode == m, func() {
			if currentProject == "" {
				msgbox("Modalità", "Apri un progetto per cambiarne la modalità.", MB_OK)
				return
			}
			if !confirmUnsavedProjectChanges("cambiare modalità") {
				return
			}
			if e := saveProjectEditorMode(currentProject, m); e != nil {
				msgbox("Modalità", e.Error(), MB_OK|MB_ICONERROR)
				return
			}
			applyProjectEditorMode(currentProject)
			setText(hwndMain, "PML Studio - "+projectName(currentProject)+" ["+projectEditorModeLabel(m)+"]")
			layout(hwndMain)
			createMainMenu(hwndMain)
		})
	}
	g = sub(menu, "Plugin")
	item(g, "Gestore plugin editor: non disponibile", false, nil)
	item(g, "Stato sistema plugin...", false, func() {
		msgbox("Plugin", "Questa sorgente non contiene un loader di plugin per l'editor.\r\nLe opzioni di caricamento e aggiornamento saranno attivate quando sarà presente.\r\nI plugin del gioco importati sono un sistema distinto.", MB_OK)
	})
	g = sub(menu, "Aggiornamenti")
	item(g, "Versione installata: "+currentAppVersion(), false, nil)
	item(g, "Canale: GitHub Releases ufficiali", true, nil)
	item(g, "Controllo automatico all'avvio: attivo", true, nil)
	item(g, "Controlla aggiornamenti / Aggiorna...", false, func() {
		beginUpdateCheck(true)
	})

}

func handleSettingsMenuCommand(id uint16) bool {
	if action, ok := settingsMenuActions[id]; ok {
		action()
		return true
	}
	return false
}
