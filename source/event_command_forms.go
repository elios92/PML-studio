//go:build windows

package main

import (
	"fmt"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

const (
	simpleFormClass  = "PLMStudioSimpleCommandForm01"
	simpleFormOK     = 9380
	simpleFormCancel = 9381
	simpleFormBase   = 9400
)

type simpleFormKind int

const (
	simpleFormText simpleFormKind = iota
	simpleFormNumber
	simpleFormCombo
	simpleFormCheck
	simpleFormMultiline
)

type simpleFormField struct {
	Key     string
	Label   string
	Kind    simpleFormKind
	Initial string
	Options []string
}

var simpleFormRegistered, simpleFormOpen, simpleFormAccepted bool
var simpleFormWindow syscall.Handle
var simpleFormControls map[string]syscall.Handle
var simpleFormSpecs []simpleFormField
var simpleFormResult map[string]string

func simpleFormWndProcFn(hwnd syscall.Handle, msg uint32, w, l uintptr) uintptr {
	switch msg {
	case WM_COMMAND:
		switch int(loword(w)) {
		case simpleFormOK:
			out := map[string]string{}
			for _, spec := range simpleFormSpecs {
				h := simpleFormControls[spec.Key]
				switch spec.Kind {
				case simpleFormCombo:
					idx := comboSel(h)
					if idx >= 0 && idx < len(spec.Options) {
						out[spec.Key] = spec.Options[idx]
					}
				case simpleFormCheck:
					if checked(h) {
						out[spec.Key] = "true"
					} else {
						out[spec.Key] = "false"
					}
				default:
					out[spec.Key] = strings.TrimSpace(getText(h))
				}
			}
			simpleFormResult = out
			simpleFormAccepted = true
			pDestroyWindow.Call(uintptr(hwnd))
			return 0
		case simpleFormCancel:
			pDestroyWindow.Call(uintptr(hwnd))
			return 0
		}
	case WM_CLOSE:
		pDestroyWindow.Call(uintptr(hwnd))
		return 0
	case WM_DESTROY:
		simpleFormOpen = false
		simpleFormWindow = 0
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(uintptr(hwnd), uintptr(msg), w, l)
	return r
}

var simpleFormWndProc = syscall.NewCallback(simpleFormWndProcFn)

func ensureSimpleFormClass() error {
	if simpleFormRegistered {
		return nil
	}
	h, _, _ := pGetModuleHandleW.Call(0)
	hi := syscall.Handle(h)
	cur, _, _ := pLoadCursorW.Call(0, IDC_ARROW)
	brush, _, _ := pCreateSolidBrush.Call(rgb(244, 244, 244))
	wc := WNDCLASSEX{cbSize: uint32(unsafe.Sizeof(WNDCLASSEX{})), lpfnWndProc: simpleFormWndProc, hInstance: hi, hCursor: syscall.Handle(cur), hbrBackground: syscall.Handle(brush), lpszClassName: wstr(simpleFormClass)}
	r, _, err := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	if r == 0 {
		return fmt.Errorf("registrazione form comando: %v", err)
	}
	simpleFormRegistered = true
	return nil
}

func showSimpleCommandForm(owner syscall.Handle, title, info string, specs []simpleFormField) (map[string]string, bool) {
	if simpleFormOpen || len(specs) == 0 {
		return nil, false
	}
	if err := ensureSimpleFormClass(); err != nil {
		msgbox("PML Studio", err.Error(), MB_OK|MB_ICONERROR)
		return nil, false
	}
	height := int32(150)
	for _, s := range specs {
		if s.Kind == simpleFormMultiline {
			height += 150
		} else {
			height += 54
		}
	}
	height += 92
	if height < 360 {
		height = 360
	}
	if height > 820 {
		height = 820
	}
	const ww int32 = 760
	x, y := centerOwnedWindow(ww, height)
	h, _, _ := pGetModuleHandleW.Call(0)
	hi := syscall.Handle(h)
	simpleFormOpen = true
	simpleFormAccepted = false
	simpleFormResult = nil
	simpleFormSpecs = append([]simpleFormField(nil), specs...)
	simpleFormControls = map[string]syscall.Handle{}
	simpleFormWindow = createWindow(simpleFormClass, title, WS_OVERLAPPEDWINDOW|WS_VISIBLE|WS_CLIPCHILDREN, x, y, ww, height, owner, 0, hi)
	if simpleFormWindow == 0 {
		simpleFormOpen = false
		return nil, false
	}
	setWindowIcon(simpleFormWindow)
	yy := int32(22)
	if strings.TrimSpace(info) != "" {
		createWindow("STATIC", info, WS_CHILD|WS_VISIBLE, 24, yy, 690, 44, simpleFormWindow, 9390, hi)
		yy += 52
	}
	for i, spec := range specs {
		id := uintptr(simpleFormBase + i)
		if spec.Kind == simpleFormCheck {
			hctrl := createWindow("BUTTON", spec.Label, WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_AUTOCHECKBOX, 28, yy, 660, 26, simpleFormWindow, id, hi)
			if strings.EqualFold(spec.Initial, "true") || spec.Initial == "1" {
				pSendMessageW.Call(uintptr(hctrl), BM_SETCHECK, BST_CHECKED, 0)
			}
			simpleFormControls[spec.Key] = hctrl
			yy += 44
			continue
		}
		createWindow("STATIC", spec.Label, WS_CHILD|WS_VISIBLE, 28, yy, 210, 24, simpleFormWindow, 9391+uintptr(i), hi)
		switch spec.Kind {
		case simpleFormCombo:
			hctrl := createWindow("COMBOBOX", "", WS_CHILD|WS_VISIBLE|WS_TABSTOP|CBS_DROPDOWNLIST, 242, yy-4, 460, 220, simpleFormWindow, id, hi)
			sel := 0
			for j, opt := range spec.Options {
				comboAdd(hctrl, opt)
				if strings.EqualFold(strings.TrimSpace(opt), strings.TrimSpace(spec.Initial)) {
					sel = j
				}
			}
			pSendMessageW.Call(uintptr(hctrl), CB_SETCURSEL, uintptr(sel), 0)
			simpleFormControls[spec.Key] = hctrl
			yy += 54
		case simpleFormMultiline:
			hctrl := createWindow("EDIT", spec.Initial, WS_CHILD|WS_VISIBLE|WS_BORDER|WS_VSCROLL|WS_TABSTOP|ES_MULTILINE|ES_AUTOVSCROLL|ES_WANTRETURN, 242, yy-4, 460, 132, simpleFormWindow, id, hi)
			simpleFormControls[spec.Key] = hctrl
			yy += 150
		default:
			hctrl := createWindow("EDIT", spec.Initial, WS_CHILD|WS_VISIBLE|WS_BORDER|WS_TABSTOP, 242, yy-4, 460, 30, simpleFormWindow, id, hi)
			simpleFormControls[spec.Key] = hctrl
			yy += 54
		}
	}
	bottom := height - 82
	createWindow("BUTTON", "Conferma", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 490, bottom, 105, 38, simpleFormWindow, simpleFormOK, hi)
	createWindow("BUTTON", "Annulla", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 610, bottom, 105, 38, simpleFormWindow, simpleFormCancel, hi)
	pEnableWindow.Call(uintptr(owner), 0)
	modalLoop(&simpleFormOpen)
	pEnableWindow.Call(uintptr(owner), 1)
	pSetFocus.Call(uintptr(owner))
	return simpleFormResult, simpleFormAccepted
}

func formInt(values map[string]string, key string, def int) int {
	if values == nil {
		return def
	}
	v, err := strconv.Atoi(strings.TrimSpace(values[key]))
	if err != nil {
		return def
	}
	return v
}
func formBool(values map[string]string, key string) bool {
	return values != nil && strings.EqualFold(values[key], "true")
}
