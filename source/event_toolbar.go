//go:build windows

package main

import (
	"fmt"
	"syscall"
	"unsafe"
)

const (
	eventCellCmdModify    = 0x7F01
	eventCellCmdCopy      = 0x7F02
	eventCellCmdPaste     = 0x7F03
	eventCellCmdDuplicate = 0x7F04
)

var (
	eventLeftMenuPending bool
	eventLeftMenuX       int
	eventLeftMenuY       int

	eventRightDragActive   bool
	eventRightDragIndex    = -1
	eventRightDragEventID  int
	eventRightDragStartX   int
	eventRightDragStartY   int
	eventRightDragLastX    int
	eventRightDragLastY    int
	eventRightDragHasMoved bool
)

func selectedEventPtr() *EditorEvent {
	if selectedEvent < 0 || selectedEvent >= len(events) {
		return nil
	}
	return &events[selectedEvent]
}

func requireSelectedEvent(action string) *EditorEvent {
	e := selectedEventPtr()
	if e == nil {
		msgbox("PML Studio - Eventi", "Seleziona prima un evento sulla mappa per "+action+".", MB_OK|MB_ICONINFORMATION)
		return nil
	}
	return e
}

func cloneEditorEvent(e EditorEvent) EditorEvent {
	c := e
	if len(e.NativeRaw) > 0 {
		c.NativeRaw = append(c.NativeRaw[:0:0], e.NativeRaw...)
	}
	return c
}

func copySelectedEvent() {
	e := requireSelectedEvent("copiarlo")
	if e == nil {
		return
	}
	c := cloneEditorEvent(*e)
	eventClipboard = &c
	setToolbarStatus(fmt.Sprintf("Evento copiato: #%d %s. Clicca una cella e scegli Incolla.", e.ID, e.Name))
}

func beginDuplicateSelectedEvent() {
	e := requireSelectedEvent("duplicarlo")
	if e == nil {
		return
	}
	c := cloneEditorEvent(*e)
	pendingEventDuplicate = &c
	setToolbarStatus("Duplica evento: clicca sulla cella della mappa dove creare la copia.")
}

func nextEventID() int {
	mx := 0
	for _, e := range events {
		if e.ID > mx {
			mx = e.ID
		}
	}
	return mx + 1
}

func selectEventByID(id int) {
	selectedEvent = -1
	for i := range events {
		if events[i].ID == id {
			selectedEvent = i
			return
		}
	}
}

func placeEventCopy(source *EditorEvent, x, y int, action string) bool {
	if source == nil || currentMap == nil || x < 0 || y < 0 || x >= currentMapW || y >= currentMapH {
		return false
	}
	c := cloneEditorEvent(*source)
	c.ID = nextEventID()
	c.X, c.Y = x, y
	if c.Name == "" {
		c.Name = fmt.Sprintf("Evento %d", c.ID)
	} else {
		c.Name += " - copia"
	}
	// Preserve the complete native page/command payload but give the copy its
	// own identity and coordinates before serialising it back to MapXXX.json.
	if c.Native || (currentMapDoc != nil && currentMapDoc.Raw != nil) {
		c.Native = true
		c.NativeKey = fmt.Sprintf("%d", c.ID)
		c.NativeRaw = patchNativeEventRaw(c)
	}
	oldSelected := selectedEvent
	events = append(events, c)
	selectedEvent = len(events) - 1
	if err := saveEvents(); err != nil {
		events = events[:len(events)-1]
		selectedEvent = oldSelected
		msgbox("PML Studio - Eventi", "Impossibile "+action+" l'evento:\r\n\r\n"+err.Error(), MB_OK|MB_ICONERROR)
		setToolbarStatus("Operazione evento non salvata: " + err.Error())
		return true
	}
	selectEventByID(c.ID)
	showEvent()
	invalidate(hwndCanvas)
	setToolbarStatus(fmt.Sprintf("Evento %s: #%d %s in %d,%d", action, c.ID, c.Name, x, y))
	return true
}

func placePendingEventDuplicate(x, y int) bool {
	if pendingEventDuplicate == nil {
		return false
	}
	if !placeEventCopy(pendingEventDuplicate, x, y, "duplicato") {
		return false
	}
	pendingEventDuplicate = nil
	return true
}

func pasteEventClipboardAt(x, y int) bool {
	if eventClipboard == nil {
		return false
	}
	return placeEventCopy(eventClipboard, x, y, "incollato")
}

func deleteSelectedEventFromToolbar() {
	e := requireSelectedEvent("eliminarlo")
	if e == nil {
		return
	}
	if msgboxResult("PML Studio - Eventi", fmt.Sprintf("Eliminare l'evento #%d \"%s\"?", e.ID, e.Name), MB_YESNOCANCEL|MB_ICONINFORMATION) != IDYES {
		return
	}
	deleteSelectedEvent()
	setToolbarStatus("Evento eliminato.")
}

func showEventCellMenu(x, y int) {
	idx := eventAt(x, y)
	if idx >= 0 && idx < len(events) {
		selectedEvent = idx
		showEvent()
		invalidate(hwndCanvas)
	}
	if idx < 0 && eventClipboard == nil {
		return
	}

	menu, _, _ := pCreatePopupMenu.Call()
	if menu == 0 {
		return
	}
	defer pDestroyMenu.Call(menu)

	eventFlags := uintptr(MF_STRING)
	if idx < 0 {
		eventFlags |= MF_GRAYED
	}
	pasteFlags := uintptr(MF_STRING)
	if eventClipboard == nil {
		pasteFlags |= MF_GRAYED
	}
	appendMenu(syscall.Handle(menu), eventFlags, eventCellCmdModify, "Modifica")
	appendMenu(syscall.Handle(menu), eventFlags, eventCellCmdCopy, "Copia")
	appendMenu(syscall.Handle(menu), pasteFlags, eventCellCmdPaste, "Incolla")
	appendMenu(syscall.Handle(menu), eventFlags, eventCellCmdDuplicate, "Duplica")

	var pt POINT
	if r, _, _ := pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt))); r == 0 {
		return
	}
	pSetForegroundWindow.Call(uintptr(hwndMain))
	cmd, _, _ := pTrackPopupMenu.Call(
		menu,
		TPM_LEFTALIGN|TPM_TOPALIGN|TPM_RETURNCMD|TPM_RIGHTBUTTON,
		uintptr(int64(pt.X)), uintptr(int64(pt.Y)), 0, uintptr(hwndMain), 0,
	)
	switch cmd {
	case eventCellCmdModify:
		if idx >= 0 && idx < len(events) {
			showEditEventEditorDialog(idx)
		}
	case eventCellCmdCopy:
		if idx >= 0 && idx < len(events) {
			selectedEvent = idx
			copySelectedEvent()
		}
	case eventCellCmdPaste:
		pasteEventClipboardAt(x, y)
	case eventCellCmdDuplicate:
		if idx >= 0 && idx < len(events) {
			selectedEvent = idx
			beginDuplicateSelectedEvent()
		}
	}
}

func eventCanvasLeftDown(x, y int) {
	eventLeftMenuPending = false
	if placePendingGenericEvent(x, y) {
		return
	}
	if placePendingTrainerEvent(x, y) {
		return
	}
	if placePendingEventDuplicate(x, y) {
		return
	}
	if placeEvent {
		addEventAt(x, y)
		return
	}
	selectedEvent = eventAt(x, y)
	showEvent()
	invalidate(hwndCanvas)
	eventLeftMenuX, eventLeftMenuY = x, y
	eventLeftMenuPending = selectedEvent >= 0 || eventClipboard != nil
}

func eventCanvasLeftUp() {
	if !eventLeftMenuPending {
		return
	}
	x, y := eventLeftMenuX, eventLeftMenuY
	eventLeftMenuPending = false
	showEventCellMenu(x, y)
}

func beginEventRightDrag(x, y int) bool {
	idx := eventAt(x, y)
	if idx < 0 || idx >= len(events) {
		return false
	}
	selectedEvent = idx
	e := events[idx]
	eventRightDragActive = true
	eventRightDragIndex = idx
	eventRightDragEventID = e.ID
	eventRightDragStartX, eventRightDragStartY = e.X, e.Y
	eventRightDragLastX, eventRightDragLastY = e.X, e.Y
	eventRightDragHasMoved = false
	showEvent()
	invalidate(hwndCanvas)
	setToolbarStatus(fmt.Sprintf("Sposta evento #%d: tieni premuto il tasto destro e trascina.", e.ID))
	return true
}

func updateEventRightDrag(x, y int) bool {
	if !eventRightDragActive || eventRightDragIndex < 0 || eventRightDragIndex >= len(events) {
		return false
	}
	if x < 0 || y < 0 || x >= currentMapW || y >= currentMapH {
		return true
	}
	if x == eventRightDragLastX && y == eventRightDragLastY {
		return true
	}
	events[eventRightDragIndex].X = x
	events[eventRightDragIndex].Y = y
	eventRightDragLastX, eventRightDragLastY = x, y
	eventRightDragHasMoved = x != eventRightDragStartX || y != eventRightDragStartY
	// No disk I/O here: repaint only when the pointer enters a different map
	// cell. The actual save is performed once, on RMB release.
	invalidate(hwndCanvas)
	return true
}

func cancelEventRightDrag() {
	if !eventRightDragActive {
		return
	}
	if eventRightDragIndex >= 0 && eventRightDragIndex < len(events) {
		events[eventRightDragIndex].X = eventRightDragStartX
		events[eventRightDragIndex].Y = eventRightDragStartY
	}
	eventRightDragActive = false
	eventRightDragIndex = -1
	eventRightDragEventID = 0
	eventRightDragHasMoved = false
	invalidate(hwndCanvas)
}

func finishEventRightDrag() bool {
	if !eventRightDragActive {
		return false
	}
	idx := eventRightDragIndex
	eventID := eventRightDragEventID
	startX, startY := eventRightDragStartX, eventRightDragStartY
	endX, endY := eventRightDragLastX, eventRightDragLastY
	moved := eventRightDragHasMoved

	// Mark the gesture inactive before ReleaseCapture, otherwise the resulting
	// WM_CAPTURECHANGED would interpret a successful release as a cancellation.
	eventRightDragActive = false
	eventRightDragIndex = -1
	eventRightDragEventID = 0
	eventRightDragHasMoved = false
	pReleaseCapture.Call()

	if !moved {
		showEvent()
		invalidate(hwndCanvas)
		return true
	}
	if idx < 0 || idx >= len(events) {
		return true
	}
	if err := saveEvents(); err != nil {
		// saveEvents writes atomically. On a failed write restore the in-memory
		// coordinates so the canvas cannot claim a move that was not persisted.
		if idx >= 0 && idx < len(events) && events[idx].ID == eventID {
			events[idx].X, events[idx].Y = startX, startY
		} else {
			selectEventByID(eventID)
			if selectedEvent >= 0 && selectedEvent < len(events) {
				events[selectedEvent].X, events[selectedEvent].Y = startX, startY
			}
		}
		msgbox("PML Studio - Eventi", "Impossibile spostare l'evento:\r\n\r\n"+err.Error(), MB_OK|MB_ICONERROR)
		setToolbarStatus("Spostamento evento annullato: " + err.Error())
		showEvent()
		invalidate(hwndCanvas)
		return true
	}
	selectEventByID(eventID)
	showEvent()
	invalidate(hwndCanvas)
	setToolbarStatus(fmt.Sprintf("Evento #%d spostato da %d,%d a %d,%d.", eventID, startX, startY, endX, endY))
	return true
}

func handleEventToolbarCommand(id uintptr) bool {
	switch id {
	case idEventCreate:
		showCreateEventEditorDialog()
	case idEventCopy:
		copySelectedEvent()
	case idEventModify:
		if e := requireSelectedEvent("modificarlo"); e != nil {
			_ = e
			showEditEventEditorDialog(selectedEvent)
		}
	case idEventDelete:
		deleteSelectedEventFromToolbar()
	case idEventDuplicate:
		beginDuplicateSelectedEvent()
	case idEventRename:
		if e := requireSelectedEvent("rinominarlo"); e != nil {
			_ = e
			showEditEventEditorDialog(selectedEvent)
		}
	default:
		return false
	}
	return true
}
