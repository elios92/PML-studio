//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

const (
	splitterThickness int32 = 5

	splitterLeftVertical = iota + 1
	splitterRightVertical
	splitterLeftHorizontal
	splitterPaletteBorder
	splitterPaletteAutotile
)

var (
	hwndSplitLeftV    syscall.Handle
	hwndSplitRightV   syscall.Handle
	hwndSplitLeftH    syscall.Handle
	hwndSplitPalette1 syscall.Handle
	hwndSplitPalette2 syscall.Handle

	activeSplitterKind int
	cursorResizeWE     syscall.Handle
	cursorResizeNS     syscall.Handle

	// Valori calcolati dall'ultimo layout. Servono ai drag verticali senza
	// duplicare la geometria della UI nel wndproc dello splitter.
	resizeLeftW          int32
	resizeRightW         int32
	resizeContentBottom  int32
	resizeLeftTreeTop    int32
	resizePaletteY       int32
	resizePaletteAvail   int32
	resizePaletteBorderH int32
	resizePaletteAutoY   int32
)

func ensureResizablePanelDefaults() {
	if settings.LeftPanelWidth <= 0 {
		settings.LeftPanelWidth = 194
	}
	if settings.RightPanelWidth <= 0 {
		settings.RightPanelWidth = 188
	}
	if settings.PermissionPanelWidth <= 0 {
		settings.PermissionPanelWidth = 344
	}
	if settings.LeftDetailsHeight <= 0 {
		settings.LeftDetailsHeight = 103
	}
	if settings.PaletteBorderHeight <= 0 {
		settings.PaletteBorderHeight = 190
	}
	if settings.PaletteAutotileHeight <= 0 {
		settings.PaletteAutotileHeight = 122
	}
}

func configuredRightPanelWidth() int32 {
	ensureResizablePanelDefaults()
	if mode == "permissions" {
		return int32(settings.PermissionPanelWidth)
	}
	return int32(settings.RightPanelWidth)
}

func setConfiguredRightPanelWidth(v int32) {
	if mode == "permissions" {
		settings.PermissionPanelWidth = int(v)
	} else {
		settings.RightPanelWidth = int(v)
	}
}

func clampI32(v, lo, hi int32) int32 {
	if hi < lo {
		hi = lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func createResizeSplitters(hInst syscall.Handle) {
	style := uint32(WS_CHILD | WS_VISIBLE)
	hwndSplitLeftV = createWindow("PLMStudioSplitter05", "", style, 0, 0, 0, 0, hwndMain, 1901, hInst)
	hwndSplitRightV = createWindow("PLMStudioSplitter05", "", style, 0, 0, 0, 0, hwndMain, 1902, hInst)
	hwndSplitLeftH = createWindow("PLMStudioSplitter05", "", style, 0, 0, 0, 0, hwndMain, 1903, hInst)
	hwndSplitPalette1 = createWindow("PLMStudioSplitter05", "", style, 0, 0, 0, 0, hwndMain, 1904, hInst)
	hwndSplitPalette2 = createWindow("PLMStudioSplitter05", "", style, 0, 0, 0, 0, hwndMain, 1905, hInst)
}

func splitterKind(hwnd syscall.Handle) int {
	switch hwnd {
	case hwndSplitLeftV:
		return splitterLeftVertical
	case hwndSplitRightV:
		return splitterRightVertical
	case hwndSplitLeftH:
		return splitterLeftHorizontal
	case hwndSplitPalette1:
		return splitterPaletteBorder
	case hwndSplitPalette2:
		return splitterPaletteAutotile
	default:
		return 0
	}
}

func splitterIsVertical(kind int) bool {
	return kind == splitterLeftVertical || kind == splitterRightVertical
}

func splitterCursor(kind int) syscall.Handle {
	if splitterIsVertical(kind) {
		if cursorResizeWE == 0 {
			c, _, _ := pLoadCursorW.Call(0, IDC_SIZEWE)
			cursorResizeWE = syscall.Handle(c)
		}
		return cursorResizeWE
	}
	if cursorResizeNS == 0 {
		c, _, _ := pLoadCursorW.Call(0, IDC_SIZENS)
		cursorResizeNS = syscall.Handle(c)
	}
	return cursorResizeNS
}

func cursorInMainClient() POINT {
	var pt POINT
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	if hwndMain != 0 {
		pScreenToClient.Call(uintptr(hwndMain), uintptr(unsafe.Pointer(&pt)))
	}
	return pt
}

func applySplitterDrag(kind int) {
	if hwndMain == 0 {
		return
	}
	ensureResizablePanelDefaults()
	pt := cursorInMainClient()
	var rc RECT
	pGetClientRect.Call(uintptr(hwndMain), uintptr(unsafe.Pointer(&rc)))
	scale := int32(max(100, settings.UIScale))
	pt.X = pt.X * 100 / scale
	pt.Y = pt.Y * 100 / scale
	clientW := rc.Right * 100 / scale

	const centralMin int32 = 430
	switch kind {
	case splitterLeftVertical:
		maxLeft := clientW - resizeRightW - splitterThickness*2 - centralMin
		v := clampI32(pt.X, 150, maxLeft)
		settings.LeftPanelWidth = int(v)
	case splitterRightVertical:
		minRight := int32(188)
		if mode == "permissions" {
			minRight = 300
		}
		maxRight := clientW - resizeLeftW - splitterThickness*2 - centralMin
		v := clampI32(clientW-pt.X, minRight, maxRight)
		setConfiguredRightPanelWidth(v)
	case splitterLeftHorizontal:
		// Il pannello dettagli occupa la parte bassa della sidebar sinistra.
		maxDetails := resizeContentBottom - resizeLeftTreeTop - 80 - splitterThickness
		v := clampI32(resizeContentBottom-pt.Y, 72, maxDetails)
		settings.LeftDetailsHeight = int(v)
	case splitterPaletteBorder:
		// Blocco bordi e' deliberatamente fisso: nessun drag puo' cambiarne
		// l'altezza. Lo splitter relativo resta solo come handle legacy interno
		// ma viene sempre nascosto dal layout.
		return
	case splitterPaletteAutotile:
		const minAuto int32 = 68
		const minTiles int32 = 96
		borderH := resizePaletteBorderH
		maxAuto := resizePaletteAvail - borderH - minTiles - splitterThickness*2
		v := clampI32(pt.Y-resizePaletteAutoY, minAuto, maxAuto)
		settings.PaletteAutotileHeight = int(v)
	}
	layout(hwndMain)
}

func splitterWndProc(hwnd syscall.Handle, msg uint32, w, l uintptr) uintptr {
	kind := splitterKind(hwnd)
	switch msg {
	case WM_SETCURSOR:
		if c := splitterCursor(kind); c != 0 {
			pSetCursor.Call(uintptr(c))
			return 1
		}
	case WM_LBUTTONDOWN:
		activeSplitterKind = kind
		pSetCapture.Call(uintptr(hwnd))
		if c := splitterCursor(kind); c != 0 {
			pSetCursor.Call(uintptr(c))
		}
		return 0
	case WM_MOUSEMOVE:
		if activeSplitterKind == kind && kind != 0 {
			applySplitterDrag(kind)
			return 0
		}
	case WM_LBUTTONUP:
		if activeSplitterKind == kind && kind != 0 {
			applySplitterDrag(kind)
			activeSplitterKind = 0
			pReleaseCapture.Call()
			saveSettings()
			return 0
		}
	case WM_CAPTURECHANGED:
		if activeSplitterKind == kind {
			activeSplitterKind = 0
			saveSettings()
		}
		return 0
	case WM_PAINT:
		var ps PAINTSTRUCT
		hdc, _, _ := pBeginPaint.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&ps)))
		if hdc != 0 {
			var rc RECT
			pGetClientRect.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&rc)))
			brush, _, _ := pCreateSolidBrush.Call(uintptr(rgb(themeBorderR, themeBorderG, themeBorderB)))
			if brush != 0 {
				pFillRect.Call(hdc, uintptr(unsafe.Pointer(&rc)), brush)
				pDeleteObject.Call(brush)
			}
		}
		pEndPaint.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&ps)))
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(uintptr(hwnd), uintptr(msg), w, l)
	return r
}
