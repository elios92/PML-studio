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
	audioPaletteClass = "PLMStudioAudioPalette01"
	idAudioBGM        = 9500
	idAudioBGS        = 9501
	idAudioME         = 9502
	idAudioSE         = 9503
	idAudioStopBGM    = 9504
	idAudioFadeBGM    = 9505
	idAudioFadeBGS    = 9506
	idAudioBack       = 9507
)

var audioPaletteRegistered, audioPaletteOpen, audioPaletteAccepted bool
var audioPaletteWindow syscall.Handle
var audioPaletteChoice int

func audioPaletteWndProcFn(hwnd syscall.Handle, msg uint32, w, l uintptr) uintptr {
	if msg == WM_COMMAND {
		id := int(loword(w))
		if id >= idAudioBGM && id <= idAudioFadeBGS {
			audioPaletteChoice = id
			audioPaletteAccepted = true
			pDestroyWindow.Call(uintptr(hwnd))
			return 0
		}
		if id == idAudioBack {
			pDestroyWindow.Call(uintptr(hwnd))
			return 0
		}
	}
	if msg == WM_CLOSE {
		pDestroyWindow.Call(uintptr(hwnd))
		return 0
	}
	if msg == WM_DESTROY {
		audioPaletteOpen = false
		audioPaletteWindow = 0
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(uintptr(hwnd), uintptr(msg), w, l)
	return r
}

var audioPaletteWndProc = syscall.NewCallback(audioPaletteWndProcFn)

func ensureAudioPaletteClass() error {
	if audioPaletteRegistered {
		return nil
	}
	h, _, _ := pGetModuleHandleW.Call(0)
	hi := syscall.Handle(h)
	cur, _, _ := pLoadCursorW.Call(0, IDC_ARROW)
	br, _, _ := pCreateSolidBrush.Call(rgb(244, 244, 244))
	wc := WNDCLASSEX{cbSize: uint32(unsafe.Sizeof(WNDCLASSEX{})), lpfnWndProc: audioPaletteWndProc, hInstance: hi, hCursor: syscall.Handle(cur), hbrBackground: syscall.Handle(br), lpszClassName: wstr(audioPaletteClass)}
	r, _, e := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	if r == 0 {
		return fmt.Errorf("registrazione Audio: %v", e)
	}
	audioPaletteRegistered = true
	return nil
}
func showAudioPalette(owner syscall.Handle) (int, bool) {
	if audioPaletteOpen {
		return 0, false
	}
	if err := ensureAudioPaletteClass(); err != nil {
		msgbox("PML Studio - Audio", err.Error(), MB_OK|MB_ICONERROR)
		return 0, false
	}
	const ww, wh int32 = 690, 470
	x, y := centerOwnedWindow(ww, wh)
	h, _, _ := pGetModuleHandleW.Call(0)
	hi := syscall.Handle(h)
	audioPaletteOpen = true
	audioPaletteAccepted = false
	audioPaletteChoice = 0
	audioPaletteWindow = createWindow(audioPaletteClass, "Audio evento", WS_OVERLAPPEDWINDOW|WS_VISIBLE|WS_CLIPCHILDREN, x, y, ww, wh, owner, 0, hi)
	if audioPaletteWindow == 0 {
		audioPaletteOpen = false
		return 0, false
	}
	setWindowIcon(audioPaletteWindow)
	createWindow("STATIC", "Audio per scene ed eventi. I comandi vengono salvati anche nel formato evento nativo quando disponibile.", WS_CHILD|WS_VISIBLE, 28, 22, 620, 42, audioPaletteWindow, 9510, hi)
	buttons := []struct {
		id   int
		t    string
		x, y int32
	}{{idAudioBGM, "Riproduci BGM", 45, 82}, {idAudioBGS, "Riproduci BGS", 255, 82}, {idAudioME, "Riproduci ME", 465, 82}, {idAudioSE, "Riproduci SE", 45, 164}, {idAudioStopBGM, "Stop BGM", 255, 164}, {idAudioFadeBGM, "Dissolvi BGM", 465, 164}, {idAudioFadeBGS, "Dissolvi BGS", 45, 246}, {idAudioBack, "← Indietro", 465, 326}}
	for _, b := range buttons {
		createWindow("BUTTON", b.t, WS_CHILD|WS_VISIBLE|WS_TABSTOP, b.x, b.y, 180, 56, audioPaletteWindow, uintptr(b.id), hi)
	}
	createWindow("STATIC", "BGM=musica · BGS=ambiente · ME=jingle · SE=effetto sonoro", WS_CHILD|WS_VISIBLE, 255, 258, 380, 42, audioPaletteWindow, 9511, hi)
	pEnableWindow.Call(uintptr(owner), 0)
	modalLoop(&audioPaletteOpen)
	pEnableWindow.Call(uintptr(owner), 1)
	pSetFocus.Call(uintptr(owner))
	return audioPaletteChoice, audioPaletteAccepted
}

func projectAudioCatalog(kind string) []string {
	root := strings.TrimSpace(currentProject)
	if root == "" {
		return nil
	}
	kind = strings.ToUpper(strings.TrimSpace(kind))
	seen := map[string]bool{}
	out := []string{}
	dirs := projectAudioCategoryDirs(root, kind)
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			ext := strings.ToLower(filepath.Ext(e.Name()))
			if ext != ".ogg" && ext != ".mp3" && ext != ".wav" && ext != ".mid" && ext != ".midi" {
				continue
			}
			name := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
			if !seen[strings.ToLower(name)] {
				seen[strings.ToLower(name)] = true
				out = append(out, name)
			}
		}
	}
	sort.Strings(out)
	return out
}
func configureAudioPlay(owner syscall.Handle, kind string) (plmManagedEventCommand, bool) {
	opts := projectAudioCatalog(kind)
	nameField := simpleFormField{Key: "name", Label: "File audio", Kind: simpleFormText}
	if len(opts) > 0 {
		nameField.Kind = simpleFormCombo
		nameField.Options = opts
	}
	vals, ok := showSimpleCommandForm(owner, "Riproduci "+strings.ToUpper(kind), "Scegli un audio reale del progetto. Se la cartella Audio non è indicizzabile, puoi scrivere manualmente il nome del file senza estensione.", []simpleFormField{nameField, {Key: "volume", Label: "Volume 0..100", Kind: simpleFormNumber, Initial: "100"}, {Key: "pitch", Label: "Pitch 50..150", Kind: simpleFormNumber, Initial: "100"}})
	if !ok {
		return plmManagedEventCommand{}, false
	}
	name := strings.TrimSpace(vals["name"])
	if name == "" {
		msgbox("PML Studio - Audio", "Nessun file audio selezionato.", MB_OK|MB_ICONINFORMATION)
		return plmManagedEventCommand{}, false
	}
	vol := formInt(vals, "volume", 100)
	if vol < 0 {
		vol = 0
	}
	if vol > 100 {
		vol = 100
	}
	pitch := formInt(vals, "pitch", 100)
	if pitch < 50 {
		pitch = 50
	}
	if pitch > 150 {
		pitch = 150
	}
	return plmManagedEventCommand{Type: "audio_play", AudioKind: kind, AudioName: name, Volume: vol, Pitch: pitch}, true
}
func addAudioEventCommand() {
	for {
		choice, ok := showAudioPalette(eventEditorWindow)
		if !ok {
			return
		}
		var c plmManagedEventCommand
		var good bool
		switch choice {
		case idAudioBGM:
			c, good = configureAudioPlay(eventEditorWindow, "bgm")
		case idAudioBGS:
			c, good = configureAudioPlay(eventEditorWindow, "bgs")
		case idAudioME:
			c, good = configureAudioPlay(eventEditorWindow, "me")
		case idAudioSE:
			c, good = configureAudioPlay(eventEditorWindow, "se")
		case idAudioStopBGM:
			c = plmManagedEventCommand{Type: "audio_stop", AudioKind: "bgm"}
			good = true
		case idAudioFadeBGM, idAudioFadeBGS:
			kind := "bgm"
			if choice == idAudioFadeBGS {
				kind = "bgs"
			}
			vals, ok2 := showSimpleCommandForm(eventEditorWindow, "Dissolvi "+strings.ToUpper(kind), "La dissolvenza termina l'audio gradualmente.", []simpleFormField{{Key: "seconds", Label: "Durata (secondi)", Kind: simpleFormNumber, Initial: "2"}})
			if ok2 {
				sec := formInt(vals, "seconds", 2)
				if sec < 1 {
					sec = 1
				}
				c = plmManagedEventCommand{Type: "audio_fade", AudioKind: kind, FadeSeconds: sec}
				good = true
			}
		}
		if good && appendManagedEventCommand(c) {
			setToolbarStatus("Comando Audio aggiunto: " + managedEventCommandLabel(c))
		}
	}
}
