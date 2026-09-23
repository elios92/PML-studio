//go:build windows

package main

import (
	"fmt"
	"syscall"
	"unsafe"
)

const autotileVariantDialogClass = "PLMStudioAutotileVariantDialog01"

var (
	autotileVariantDialogRegistered bool
	autotileVariantDialogOpen       bool
	autotileVariantDialogWindow     syscall.Handle
	autotileVariantDialogOwner      syscall.Handle
	autotileVariantDialogSlot       = -1
	autotileVariantDialogHover      = -1
)

const (
	autotileVariantCols = 8
	autotileVariantRows = 6
	autotileVariantCell = 32
	autotileVariantPadX = 18
	autotileVariantPadY = 44
)

func autotileVariantDialogTitle(slot int) string {
	name := ""
	if currentTileset != nil && slot >= 0 && slot < len(currentTileset.AutotileNames) {
		name = currentTileset.AutotileNames[slot]
	}
	if name == "" {
		return fmt.Sprintf("Varianti Autotile %d", slot+1)
	}
	return fmt.Sprintf("Varianti Autotile %d - %s", slot+1, name)
}

func autotileVariantIndexAt(l uintptr) (int, bool) {
	x := int(int16(loword(l))) - autotileVariantPadX
	y := int(int16(hiword(l))) - autotileVariantPadY
	if x < 0 || y < 0 {
		return -1, false
	}
	col := x / autotileVariantCell
	row := y / autotileVariantCell
	if col < 0 || col >= autotileVariantCols || row < 0 || row >= autotileVariantRows {
		return -1, false
	}
	idx := row*autotileVariantCols + col
	return idx, idx >= 0 && idx < 48
}

func currentAutotileVariantForSlot(slot int) int {
	if selectedTileID < 48 {
		return -1
	}
	selectedSlot := (selectedTileID - 48) / 48
	if selectedSlot != slot {
		return -1
	}
	return (selectedTileID - 48) % 48
}

func paintAutotileVariantDialog(hwnd syscall.Handle) {
	var ps PAINTSTRUCT
	hdc, _, _ := pBeginPaint.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&ps)))
	if hdc == 0 {
		return
	}
	defer pEndPaint.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&ps)))

	var r RECT
	pGetClientRect.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&r)))
	bg := createSolidBrush(rgb(245, 245, 245))
	fillWithBrush(syscall.Handle(hdc), r, bg)
	deleteGDIObject(bg)

	text(syscall.Handle(hdc), 18, 14, "Scegli una variante. Un click la seleziona e torna alla mappa.", rgb(55, 55, 55))
	selectedVariant := currentAutotileVariantForSlot(autotileVariantDialogSlot)

	selectedPen, _, _ := pCreatePen.Call(PS_SOLID, 2, rgb(25, 105, 205))
	hoverPen, _, _ := pCreatePen.Call(PS_SOLID, 1, rgb(95, 160, 235))
	defer func() {
		if selectedPen != 0 {
			pDeleteObject.Call(selectedPen)
		}
		if hoverPen != 0 {
			pDeleteObject.Call(hoverPen)
		}
	}()

	for variant := 0; variant < 48; variant++ {
		col := variant % autotileVariantCols
		row := variant / autotileVariantCols
		x := int32(autotileVariantPadX + col*autotileVariantCell)
		y := int32(autotileVariantPadY + row*autotileVariantCell)
		tileID := 48 + autotileVariantDialogSlot*48 + variant
		drawSingleTile(syscall.Handle(hdc), tileID, x, y, autotileVariantCell)

		if variant == selectedVariant && selectedPen != 0 {
			old, _, _ := pSelectObject.Call(hdc, selectedPen)
			drawOutlineRect(hdc, x, y, x+autotileVariantCell, y+autotileVariantCell)
			if old != 0 {
				pSelectObject.Call(hdc, old)
			}
		} else if variant == autotileVariantDialogHover && hoverPen != 0 {
			old, _, _ := pSelectObject.Call(hdc, hoverPen)
			drawOutlineRect(hdc, x, y, x+autotileVariantCell, y+autotileVariantCell)
			if old != 0 {
				pSelectObject.Call(hdc, old)
			}
		}
	}
}

func autotileVariantDialogWndProcFn(hwnd syscall.Handle, msg uint32, w, l uintptr) uintptr {
	switch msg {
	case WM_PAINT:
		paintAutotileVariantDialog(hwnd)
		return 0
	case WM_MOUSEMOVE:
		idx, ok := autotileVariantIndexAt(l)
		if !ok {
			idx = -1
		}
		if idx != autotileVariantDialogHover {
			autotileVariantDialogHover = idx
			invalidate(hwnd)
		}
		return 0
	case WM_LBUTTONDOWN:
		variant, ok := autotileVariantIndexAt(l)
		if !ok || autotileVariantDialogSlot < 0 {
			return 0
		}
		tileID := 48 + autotileVariantDialogSlot*48 + variant
		setSingleSelectedTile(tileID)
		setPaletteSingleSelection(tileID)
		updatePaletteSelection()
		setText(hwndStatus, fmt.Sprintf("Autotile %d | variante %d/48 | Tile ID %d", autotileVariantDialogSlot+1, variant+1, tileID))
		pDestroyWindow.Call(uintptr(hwnd))
		return 0
	case WM_CLOSE:
		pDestroyWindow.Call(uintptr(hwnd))
		return 0
	case WM_DESTROY:
		autotileVariantDialogOpen = false
		autotileVariantDialogWindow = 0
		autotileVariantDialogSlot = -1
		autotileVariantDialogHover = -1
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(uintptr(hwnd), uintptr(msg), w, l)
	return r
}

var autotileVariantDialogWndProc = syscall.NewCallback(autotileVariantDialogWndProcFn)

func ensureAutotileVariantDialogClass() error {
	if autotileVariantDialogRegistered {
		return nil
	}
	h, _, _ := pGetModuleHandleW.Call(0)
	hi := syscall.Handle(h)
	cur, _, _ := pLoadCursorW.Call(0, IDC_ARROW)
	brush, _, _ := pCreateSolidBrush.Call(rgb(245, 245, 245))
	wc := WNDCLASSEX{
		cbSize:        uint32(unsafe.Sizeof(WNDCLASSEX{})),
		lpfnWndProc:   autotileVariantDialogWndProc,
		hInstance:     hi,
		hCursor:       syscall.Handle(cur),
		hbrBackground: syscall.Handle(brush),
		lpszClassName: wstr(autotileVariantDialogClass),
	}
	r, _, err := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	if r == 0 {
		return fmt.Errorf("registrazione selettore varianti autotile: %v", err)
	}
	autotileVariantDialogRegistered = true
	return nil
}

func showAutotileVariantDialog(owner syscall.Handle, slot int) {
	if currentTileset == nil || slot < 0 || slot >= len(currentTileset.Autotiles) {
		return
	}
	if autotileVariantDialogOpen {
		if autotileVariantDialogWindow != 0 {
			pSetFocus.Call(uintptr(autotileVariantDialogWindow))
		}
		return
	}
	if err := ensureAutotileVariantDialogClass(); err != nil {
		msgbox("PML Studio - Autotile", err.Error(), MB_OK|MB_ICONERROR)
		return
	}

	const clientW = autotileVariantPadX*2 + autotileVariantCols*autotileVariantCell
	const clientH = autotileVariantPadY + autotileVariantRows*autotileVariantCell + 20
	const windowW int32 = int32(clientW + 18)
	const windowH int32 = int32(clientH + 42)
	x, y := centerOwnedWindow(windowW, windowH)
	h, _, _ := pGetModuleHandleW.Call(0)
	hi := syscall.Handle(h)

	autotileVariantDialogOpen = true
	autotileVariantDialogOwner = owner
	autotileVariantDialogSlot = slot
	autotileVariantDialogHover = -1
	autotileVariantDialogWindow = createWindow(
		autotileVariantDialogClass,
		autotileVariantDialogTitle(slot),
		WS_OVERLAPPEDWINDOW|WS_VISIBLE|WS_CLIPCHILDREN,
		x, y, windowW, windowH,
		owner, 0, hi,
	)
	if autotileVariantDialogWindow == 0 {
		autotileVariantDialogOpen = false
		autotileVariantDialogSlot = -1
		return
	}
	setWindowIcon(autotileVariantDialogWindow)
	pEnableWindow.Call(uintptr(owner), 0)
	pShowWindow.Call(uintptr(autotileVariantDialogWindow), SW_SHOW)
	pUpdateWindow.Call(uintptr(autotileVariantDialogWindow))

	var m MSG
	repostQuit := false
	for autotileVariantDialogOpen {
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
	pEnableWindow.Call(uintptr(owner), 1)
	pSetFocus.Call(uintptr(owner))
	if repostQuit {
		pPostQuitMessage.Call(0)
	}
}
