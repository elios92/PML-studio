//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"unsafe"
)

const (
	mugshotCmdClassName = "PLMStudioMugshotCommand01"
	idMugshotAction     = 7810
	idMugshotGraphic    = 7811
	idMugshotSpeaker    = 7812
	idMugshotPosition   = 7813
	idMugshotSlot       = 7814
	idMugshotOK         = 7815
	idMugshotCancel     = 7816
)

var (
	mugshotCmdRegistered bool
	mugshotCmdOpen       bool
	mugshotCmdAccepted   bool
	mugshotCmdWindow     syscall.Handle
	mugshotCmdAction     syscall.Handle
	mugshotCmdGraphic    syscall.Handle
	mugshotCmdSpeaker    syscall.Handle
	mugshotCmdPosition   syscall.Handle
	mugshotCmdSlot       syscall.Handle
	mugshotCmdCatalog    []string
	mugshotCmdResult     plmManagedEventCommand
)

func mugshotAssetCatalog(project string) []string {
	project = strings.TrimSpace(project)
	if project == "" {
		return nil
	}
	roots := discoverGraphicsDirs(project)
	allowed := map[string]bool{"pictures": true, "mugshots": true, "portraits": true}
	seen := map[string]bool{}
	out := []string{}
	for _, root := range roots {
		_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil || d == nil {
				return nil
			}
			if d.IsDir() {
				return nil
			}
			ext := strings.ToLower(filepath.Ext(d.Name()))
			if ext != ".png" && ext != ".jpg" && ext != ".jpeg" && ext != ".webp" {
				return nil
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return nil
			}
			relSlash := filepath.ToSlash(rel)
			parts := strings.Split(relSlash, "/")
			if len(parts) < 2 || !allowed[strings.ToLower(parts[0])] {
				return nil
			}
			relNoExt := strings.TrimSuffix(relSlash, filepath.Ext(relSlash))
			key := strings.ToLower(relNoExt)
			if seen[key] {
				return nil
			}
			seen[key] = true
			out = append(out, relNoExt)
			return nil
		})
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i]) < strings.ToLower(out[j]) })
	return out
}

func mugshotCmdWndProcFn(hwnd syscall.Handle, msg uint32, w, l uintptr) uintptr {
	switch msg {
	case WM_COMMAND:
		switch int(loword(w)) {
		case idMugshotOK:
			action := "show"
			if comboSel(mugshotCmdAction) == 1 {
				action = "hide"
			}
			slot := intField(mugshotCmdSlot, 50)
			if slot <= 0 {
				slot = 50
			}
			c := plmManagedEventCommand{Type: "mugshot", Action: action, PictureSlot: slot}
			if action == "show" {
				graphic := strings.TrimSpace(getText(mugshotCmdGraphic))
				if graphic == "" {
					idx := comboSel(mugshotCmdGraphic)
					if idx >= 0 && idx < len(mugshotCmdCatalog) {
						graphic = mugshotCmdCatalog[idx]
					}
				}
				if graphic == "" {
					msgbox("PML Studio - Mugshot", "Seleziona o scrivi il nome di un'immagine Mugshot/Portrait/Pictures.", MB_OK|MB_ICONINFORMATION)
					return 0
				}
				pos := "Sinistra"
				switch comboSel(mugshotCmdPosition) {
				case 1:
					pos = "Centro"
				case 2:
					pos = "Destra"
				}
				c.Graphic = graphic
				c.Speaker = strings.TrimSpace(getText(mugshotCmdSpeaker))
				c.Position = pos
			}
			mugshotCmdResult = c
			mugshotCmdAccepted = true
			pDestroyWindow.Call(uintptr(hwnd))
			return 0
		case idMugshotCancel:
			pDestroyWindow.Call(uintptr(hwnd))
			return 0
		}
	case WM_CLOSE:
		pDestroyWindow.Call(uintptr(hwnd))
		return 0
	case WM_DESTROY:
		mugshotCmdOpen = false
		mugshotCmdWindow = 0
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(uintptr(hwnd), uintptr(msg), w, l)
	return r
}

var mugshotCmdWndProc = syscall.NewCallback(mugshotCmdWndProcFn)

func ensureMugshotCmdClass() error {
	if mugshotCmdRegistered {
		return nil
	}
	h, _, _ := pGetModuleHandleW.Call(0)
	hi := syscall.Handle(h)
	cur, _, _ := pLoadCursorW.Call(0, IDC_ARROW)
	brush, _, _ := pCreateSolidBrush.Call(rgb(244, 244, 244))
	wc := WNDCLASSEX{
		cbSize: uint32(unsafe.Sizeof(WNDCLASSEX{})), lpfnWndProc: mugshotCmdWndProc,
		hInstance: hi, hCursor: syscall.Handle(cur), hbrBackground: syscall.Handle(brush),
		lpszClassName: wstr(mugshotCmdClassName),
	}
	r, _, err := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	if r == 0 {
		return fmt.Errorf("registrazione finestra Mugshot: %v", err)
	}
	mugshotCmdRegistered = true
	return nil
}

func showMugshotCommandDialog(owner syscall.Handle) (plmManagedEventCommand, bool) {
	if mugshotCmdOpen {
		return plmManagedEventCommand{}, false
	}
	if err := ensureMugshotCmdClass(); err != nil {
		msgbox("PML Studio - Mugshot", err.Error(), MB_OK|MB_ICONERROR)
		return plmManagedEventCommand{}, false
	}

	const ww, wh int32 = 720, 500
	x, y := centerOwnedWindow(ww, wh)
	h, _, _ := pGetModuleHandleW.Call(0)
	hi := syscall.Handle(h)
	mugshotCmdAccepted = false
	mugshotCmdResult = plmManagedEventCommand{}
	mugshotCmdCatalog = mugshotAssetCatalog(currentProject)
	mugshotCmdOpen = true
	mugshotCmdWindow = createWindow(mugshotCmdClassName, "Mugshot", WS_OVERLAPPEDWINDOW|WS_VISIBLE|WS_CLIPCHILDREN, x, y, ww, wh, owner, 0, hi)
	if mugshotCmdWindow == 0 {
		mugshotCmdOpen = false
		return plmManagedEventCommand{}, false
	}
	setWindowIcon(mugshotCmdWindow)

	createWindow("STATIC", "Azione:", WS_CHILD|WS_VISIBLE, 28, 32, 150, 24, mugshotCmdWindow, 7820, hi)
	mugshotCmdAction = createWindow("COMBOBOX", "", WS_CHILD|WS_VISIBLE|WS_TABSTOP|CBS_DROPDOWNLIST, 190, 26, 240, 200, mugshotCmdWindow, idMugshotAction, hi)
	setComboFromStrings(mugshotCmdAction, []string{"Mostra mugshot", "Nascondi mugshot"}, 0)

	createWindow("STATIC", "Immagine:", WS_CHILD|WS_VISIBLE, 28, 86, 150, 24, mugshotCmdWindow, 7821, hi)
	mugshotCmdGraphic = createWindow("COMBOBOX", "", WS_CHILD|WS_VISIBLE|WS_TABSTOP|0x0002|WS_VSCROLL, 190, 80, 465, 280, mugshotCmdWindow, idMugshotGraphic, hi)
	setComboFromStrings(mugshotCmdGraphic, mugshotCmdCatalog, 0)

	createWindow("STATIC", "Nome personaggio:", WS_CHILD|WS_VISIBLE, 28, 140, 150, 24, mugshotCmdWindow, 7822, hi)
	mugshotCmdSpeaker = createWindow("EDIT", "", WS_CHILD|WS_VISIBLE|WS_BORDER|WS_TABSTOP, 190, 134, 300, 32, mugshotCmdWindow, idMugshotSpeaker, hi)

	createWindow("STATIC", "Posizione:", WS_CHILD|WS_VISIBLE, 28, 194, 150, 24, mugshotCmdWindow, 7823, hi)
	mugshotCmdPosition = createWindow("COMBOBOX", "", WS_CHILD|WS_VISIBLE|WS_TABSTOP|CBS_DROPDOWNLIST, 190, 188, 240, 180, mugshotCmdWindow, idMugshotPosition, hi)
	setComboFromStrings(mugshotCmdPosition, []string{"Sinistra", "Centro", "Destra"}, 0)

	createWindow("STATIC", "Slot immagine:", WS_CHILD|WS_VISIBLE, 28, 248, 150, 24, mugshotCmdWindow, 7824, hi)
	mugshotCmdSlot = createWindow("EDIT", "50", WS_CHILD|WS_VISIBLE|WS_BORDER|WS_TABSTOP, 190, 242, 100, 32, mugshotCmdWindow, idMugshotSlot, hi)

	info := "PLM cerca automaticamente immagini in Graphics/Pictures, Graphics/Mugshots e Graphics/Portraits. Il comando viene salvato in formato PML_CMD per il runtime Python; 'Nascondi' usa lo stesso slot immagine."
	createWindow("STATIC", info, WS_CHILD|WS_VISIBLE, 28, 306, 625, 72, mugshotCmdWindow, 7825, hi)

	createWindow("BUTTON", "Conferma", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 435, 398, 105, 38, mugshotCmdWindow, idMugshotOK, hi)
	createWindow("BUTTON", "Annulla", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 550, 398, 105, 38, mugshotCmdWindow, idMugshotCancel, hi)

	pEnableWindow.Call(uintptr(owner), 0)
	modalLoop(&mugshotCmdOpen)
	pEnableWindow.Call(uintptr(owner), 1)
	pSetFocus.Call(uintptr(owner))
	return mugshotCmdResult, mugshotCmdAccepted
}

func addMugshotEventCommand() {
	c, ok := showMugshotCommandDialog(eventEditorWindow)
	if !ok {
		return
	}
	if appendManagedEventCommand(c) {
		if strings.EqualFold(c.Action, "hide") {
			setToolbarStatus("Comando PLM aggiunto: nascondi mugshot.")
		} else {
			setToolbarStatus("Comando PLM aggiunto: mostra mugshot " + c.Graphic + ".")
		}
	}
}
