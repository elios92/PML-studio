//go:build windows

package main

import "syscall"

func tabButton(hInst syscall.Handle, text string, id uintptr) syscall.Handle {
	return createWindow("BUTTON", text, WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_AUTORADIOBUTTON|BS_PUSHLIKE|BS_FLAT, 0, 0, 0, 0, hwndMain, id, hInst)
}

func createViewTabs(hInst syscall.Handle) {
	// Defensive guard: the main view tabs are singleton controls. If startup or
	// a future refactor accidentally calls createViewTabs twice, do not create a
	// second row of HWNDs with the same command IDs.
	if hwndTabsStrip != 0 || hwndModeMap != 0 || hwndModePass != 0 || hwndModeEvents != 0 ||
		hwndModeEncounters != 0 || hwndModeHeader != 0 || hwndModeConnections != 0 ||
		hwndModeAnimations != 0 || hwndModeDatabase != 0 {
		diagLogf("[UI][GUARD] createViewTabs skipped: view tabs already exist")
		return
	}

	hwndTabsStrip = createWindow("STATIC", "", WS_CHILD|WS_VISIBLE, 0, 0, 0, 0, hwndMain, 1699, hInst)
	hwndModeMap = tabButton(hInst, "Vista mappa", idViewMap)
	hwndModePass = tabButton(hInst, "Vista movimenti permessi", idViewPermissions)
	hwndModeEvents = tabButton(hInst, "Vista eventi", idViewEvents)
	hwndModeEncounters = tabButton(hInst, "Vista Pokémon selvatici", idViewEncounters)
	hwndModeHeader = tabButton(hInst, "Vista header", idViewHeader)
	hwndModeConnections = tabButton(hInst, "Connessioni", idViewConnections)
	hwndModeAnimations = tabButton(hInst, "Animazioni", idViewAnimations)
	hwndModeDatabase = tabButton(hInst, "Vista dati", idViewDatabase)

	// Creation integrity check. It does not alter runtime behavior; it leaves a
	// precise diagnostic if one of the Win32 controls could not be created.
	if hwndTabsStrip == 0 || hwndModeMap == 0 || hwndModePass == 0 || hwndModeEvents == 0 ||
		hwndModeEncounters == 0 || hwndModeHeader == 0 || hwndModeConnections == 0 ||
		hwndModeAnimations == 0 || hwndModeDatabase == 0 {
		diagLogf("[UI][ERROR] incomplete view-tab creation strip=%#x map=%#x pass=%#x events=%#x encounters=%#x header=%#x connections=%#x animations=%#x database=%#x",
			uintptr(hwndTabsStrip), uintptr(hwndModeMap), uintptr(hwndModePass), uintptr(hwndModeEvents),
			uintptr(hwndModeEncounters), uintptr(hwndModeHeader), uintptr(hwndModeConnections),
			uintptr(hwndModeAnimations), uintptr(hwndModeDatabase))
	}
	updateTabSelection()
}

func updateTabSelection() {
	pairs := []struct {
		h    syscall.Handle
		name string
	}{
		{hwndModeMap, "map"}, {hwndModePass, "permissions"}, {hwndModeEvents, "events"}, {hwndModeEncounters, "encounters"},
		{hwndModeHeader, "header"}, {hwndModeConnections, "connections"}, {hwndModeAnimations, "animations"}, {hwndModeDatabase, "database"},
	}
	for _, p := range pairs {
		state := uintptr(BST_UNCHECKED)
		if mode == p.name {
			state = BST_CHECKED
		}
		if p.h != 0 {
			pSendMessageW.Call(uintptr(p.h), BM_SETCHECK, state, 0)
		}
	}
}
