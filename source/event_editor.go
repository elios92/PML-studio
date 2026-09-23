//go:build windows

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

func saveEvents() error {
	if currentMap == nil {
		return nil
	}

	// A converted/native map owns its events. Do not keep writing only the old
	// .plm sidecar, otherwise toolbar actions (delete/duplicate/rename) can look
	// successful in the editor but disappear after the map is reloaded.
	if currentMapDoc != nil && currentMapDoc.Raw != nil {
		if _, hasNativeEvents := currentMapDoc.Raw["events"]; hasNativeEvents {
			return saveEventsIntoRealMap()
		}
		for _, e := range events {
			if e.Native {
				return saveEventsIntoRealMap()
			}
		}
	}

	// Compatibility fallback for projects authored by the earliest 0.5 builds.
	// The legacy sidecar is still persisted atomically so a failed write cannot
	// truncate the last valid event document.
	d := sideDir()
	if d == "" {
		return fmt.Errorf("progetto non disponibile")
	}
	dir := filepath.Join(d, "events")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	doc := EventDoc{Version: 2, MapID: currentMap.ID, Events: events}
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	path := filepath.Join(dir, fmt.Sprintf("Map%03d.json", currentMap.ID))
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		_ = f.Close()
		if !ok {
			_ = os.Remove(tmp)
		}
	}()
	if _, err = f.Write(b); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	r, _, callErr := pMoveFileExW.Call(
		uintptr(unsafe.Pointer(wstr(tmp))),
		uintptr(unsafe.Pointer(wstr(path))),
		MOVEFILE_REPLACE_EXISTING|MOVEFILE_WRITE_THROUGH,
	)
	if r == 0 {
		return fmt.Errorf("sostituzione atomica eventi fallita: %v", callErr)
	}
	ok = true
	return nil
}

func comboAdd(h syscall.Handle, s string) {
	pSendMessageW.Call(uintptr(h), CB_ADDSTRING, 0, uintptr(unsafe.Pointer(wstr(s))))
}

func comboSel(h syscall.Handle) int {
	r, _, _ := pSendMessageW.Call(uintptr(h), CB_GETCURSEL, 0, 0)
	return int(r)
}

func triggerNames() []string {
	return []string{"Interazione", "Contatto giocatore", "Contatto evento", "Autorun", "Parallelo"}
}

func moveNames() []string {
	return []string{"Fermo", "Casuale", "Verticale", "Orizzontale", "Percorso definito"}
}

func setButtonCheck(h syscall.Handle, checked bool) {
	if h == 0 {
		return
	}
	value := uintptr(BST_UNCHECKED)
	if checked {
		value = BST_CHECKED
	}
	pSendMessageW.Call(uintptr(h), BM_SETCHECK, value, 0)
}

func buttonChecked(h syscall.Handle) bool {
	if h == 0 {
		return false
	}
	r, _, _ := pSendMessageW.Call(uintptr(h), BM_GETCHECK, 0, 0)
	return r == BST_CHECKED
}

func showEvent() {
	if selectedEvent < 0 || selectedEvent >= len(events) {
		setText(hwndEventInfo, "Nessun evento selezionato.\r\nPremi + Evento e clicca una casella.")
		setText(hwndEventName, "")
		setText(hwndEventDialog, "")
		return
	}
	e := events[selectedEvent]
	setText(hwndEventInfo, fmt.Sprintf("Evento #%d   Posizione: %d,%d", e.ID, e.X, e.Y))
	setText(hwndEventName, e.Name)
	setText(hwndEventDialog, e.Dialog)
	for i, s := range triggerNames() {
		if s == e.Trigger {
			pSendMessageW.Call(uintptr(hwndEventTrigger), CB_SETCURSEL, uintptr(i), 0)
		}
	}
	for i, s := range moveNames() {
		if s == e.Movement {
			pSendMessageW.Call(uintptr(hwndEventMove), CB_SETCURSEL, uintptr(i), 0)
		}
	}
}

func saveSelectedEvent() {
	if selectedEvent < 0 || selectedEvent >= len(events) {
		return
	}
	idx := selectedEvent
	original := events[idx]
	e := &events[idx]
	e.Name = getText(hwndEventName)
	e.Dialog = getText(hwndEventDialog)
	ti := comboSel(hwndEventTrigger)
	mi := comboSel(hwndEventMove)
	if ti >= 0 && ti < len(triggerNames()) {
		e.Trigger = triggerNames()[ti]
	}
	if mi >= 0 && mi < len(moveNames()) {
		e.Movement = moveNames()[mi]
	}
	if err := saveEvents(); err != nil {
		events[idx] = original
		msgbox("PML Studio - Eventi", "Impossibile salvare l'evento:\r\n\r\n"+err.Error(), MB_OK|MB_ICONERROR)
		setText(hwndStatus, "Errore salvataggio evento: "+err.Error())
		showEvent()
		return
	}
	showEvent()
	invalidate(hwndCanvas)
	setText(hwndStatus, "Evento salvato: "+events[idx].Name)
}

func deleteSelectedEvent() {
	if selectedEvent < 0 || selectedEvent >= len(events) {
		return
	}
	oldEvents := append([]EditorEvent(nil), events...)
	oldSelected := selectedEvent
	events = append(events[:selectedEvent], events[selectedEvent+1:]...)
	selectedEvent = -1
	if err := saveEvents(); err != nil {
		events = oldEvents
		selectedEvent = oldSelected
		msgbox("PML Studio - Eventi", "Impossibile eliminare l'evento:\r\n\r\n"+err.Error(), MB_OK|MB_ICONERROR)
		setText(hwndStatus, "Eliminazione evento non salvata: "+err.Error())
		showEvent()
		return
	}
	showEvent()
	invalidate(hwndCanvas)
}

func eventAt(x, y int) int {
	for i, e := range events {
		if e.X == x && e.Y == y {
			return i
		}
	}
	return -1
}

func addEventAt(x, y int) {
	mx := 0
	for _, e := range events {
		if e.ID > mx {
			mx = e.ID
		}
	}
	oldSelected := selectedEvent
	e := EditorEvent{ID: mx + 1, Name: fmt.Sprintf("Evento %d", mx+1), X: x, Y: y, Trigger: "Interazione", Movement: "Fermo"}
	if currentMapDoc != nil && currentMapDoc.Raw != nil {
		if _, native := currentMapDoc.Raw["events"]; native {
			e.Native = true
			e.NativeKey = fmt.Sprintf("%d", e.ID)
			e.NativeRaw = patchNativeEventRaw(e)
		}
	}
	events = append(events, e)
	selectedEvent = len(events) - 1
	placeEvent = false
	if err := saveEvents(); err != nil {
		events = events[:len(events)-1]
		selectedEvent = oldSelected
		msgbox("PML Studio - Eventi", "Impossibile creare l'evento:\r\n\r\n"+err.Error(), MB_OK|MB_ICONERROR)
		setText(hwndStatus, "Creazione evento non salvata: "+err.Error())
		showEvent()
		return
	}
	showEvent()
	invalidate(hwndCanvas)
}
