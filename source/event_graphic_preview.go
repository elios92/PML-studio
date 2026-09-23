//go:build windows

package main

import (
	"fmt"
	"strings"
	"syscall"
	"unsafe"
)

const eventGraphicPreviewClassName = "PLMStudioEventGraphicPreview05"

var eventGraphicPreviewRegistered bool

func chooseEventGraphicFromPreview() {
	if eventEditorWindow == 0 || eventEditorPageIndex < 0 || eventEditorPageIndex >= len(eventEditorPages) {
		return
	}
	saveEventEditorControlsToPage()
	current := eventEditorPages[eventEditorPageIndex].Graphic
	selected, ok := showCharacterPicker(eventEditorWindow, current)
	if ok {
		eventEditorPages[eventEditorPageIndex].Graphic = selected
		// The PNG itself carries transparency after import; this legacy page flag is no longer needed for new selections.
		eventEditorPages[eventEditorPageIndex].RemoveTransparency = false
	}
	eventEditorSprites = characterSpriteNames()
	invalidate(eventEditorGraphic)
}

func paintEventGraphicPreview(hwnd syscall.Handle) {
	var ps PAINTSTRUCT
	hdc, _, _ := pBeginPaint.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&ps)))
	if hdc == 0 {
		return
	}
	defer pEndPaint.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&ps)))
	var rc RECT
	pGetClientRect.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&rc)))
	fill(syscall.Handle(hdc), rc, rgb(255, 255, 255))
	pen, _, _ := pCreatePen.Call(0, 1, rgb(145, 145, 145))
	old, _, _ := pSelectObject.Call(hdc, pen)
	pRectangle.Call(hdc, uintptr(rc.Left), uintptr(rc.Top), uintptr(rc.Right), uintptr(rc.Bottom))
	pSelectObject.Call(hdc, old)
	pDeleteObject.Call(pen)
	if eventEditorPageIndex < 0 || eventEditorPageIndex >= len(eventEditorPages) {
		return
	}
	name := strings.TrimSpace(eventEditorPages[eventEditorPageIndex].Graphic)
	if name == "" {
		text(syscall.Handle(hdc), 18, 58, "Doppio click per scegliere la grafica", rgb(95, 95, 95))
		return
	}
	s, _, err := characterSpriteSurface(name)
	if err != nil || s == nil {
		text(syscall.Handle(hdc), 16, 52, "Sprite non trovato", rgb(180, 35, 35))
		text(syscall.Handle(hdc), 16, 76, name, rgb(80, 80, 80))
		return
	}
	fw, fh := s.Width/4, s.Height/4
	if fw <= 0 || fh <= 0 {
		fw, fh = s.Width, s.Height
	}
	sx, sy := 0, 0
	if s.Width >= 4 && s.Height >= 4 {
		sx = 0
		sy = 0
	}
	maxW, maxH := int(rc.Right-rc.Left-16), int(rc.Bottom-rc.Top-38)
	dw, dh := fw, fh
	scale := 1.0
	if dw > maxW {
		scale = float64(maxW) / float64(dw)
	}
	if float64(dh)*scale > float64(maxH) {
		scale = float64(maxH) / float64(dh)
	}
	if scale < 1 {
		dw = maxInt(1, int(float64(dw)*scale))
		dh = maxInt(1, int(float64(dh)*scale))
	}
	pixels := make([]uint32, dw*dh)
	for y := 0; y < dh; y++ {
		srcY := sy + y*fh/dh
		for x := 0; x < dw; x++ {
			srcX := sx + x*fw/dw
			i := srcY*s.Width + srcX
			bg := uint32(0x00FFFFFF)
			if ((x/8)+(y/8))%2 == 1 {
				bg = 0x00EAEAEA
			}
			a := byte(255)
			if len(s.Alpha) == len(s.Pixels) {
				a = s.Alpha[i]
			}
			pixels[y*dw+x] = blendBGR(bg, s.Pixels[i], a)
		}
	}
	bi := BITMAPINFO{BmiHeader: BITMAPINFOHEADER{BiSize: uint32(unsafe.Sizeof(BITMAPINFOHEADER{})), BiWidth: int32(dw), BiHeight: -int32(dh), BiPlanes: 1, BiBitCount: 32, BiCompression: BI_RGB}}
	dx := (int(rc.Right-rc.Left) - dw) / 2
	dy := 8 + (maxH-dh)/2
	if len(pixels) > 0 {
		pStretchDIBits.Call(hdc, uintptr(int32(dx)), uintptr(int32(dy)), uintptr(dw), uintptr(dh), 0, 0, uintptr(dw), uintptr(dh), uintptr(unsafe.Pointer(&pixels[0])), uintptr(unsafe.Pointer(&bi)), DIB_RGB_COLORS, SRCCOPY)
	}
	label := name
	if len(label) > 24 {
		label = label[:21] + "..."
	}
	text(syscall.Handle(hdc), 8, rc.Bottom-26, label, rgb(45, 45, 45))
}

func eventGraphicPreviewWndProcFn(hwnd syscall.Handle, msg uint32, w, l uintptr) uintptr {
	switch msg {
	case WM_LBUTTONDBLCLK:
		chooseEventGraphicFromPreview()
		return 0
	case WM_PAINT:
		paintEventGraphicPreview(hwnd)
		return 0
	case WM_SETCURSOR:
		cursor, _, _ := pLoadCursorW.Call(0, IDC_HAND)
		pSetCursor.Call(cursor)
		return 1
	}
	r, _, _ := pDefWindowProcW.Call(uintptr(hwnd), uintptr(msg), w, l)
	return r
}

var eventGraphicPreviewWndProc = syscall.NewCallback(eventGraphicPreviewWndProcFn)

func ensureEventGraphicPreviewClass() error {
	if eventGraphicPreviewRegistered {
		return nil
	}
	h, _, _ := pGetModuleHandleW.Call(0)
	hInst := syscall.Handle(h)
	cursor, _, _ := pLoadCursorW.Call(0, IDC_HAND)
	brush, _, _ := pCreateSolidBrush.Call(rgb(255, 255, 255))
	wc := WNDCLASSEX{cbSize: uint32(unsafe.Sizeof(WNDCLASSEX{})), style: CS_DBLCLKS, lpfnWndProc: eventGraphicPreviewWndProc, hInstance: hInst, hCursor: syscall.Handle(cursor), hbrBackground: syscall.Handle(brush), lpszClassName: wstr(eventGraphicPreviewClassName)}
	r, _, err := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	if r == 0 {
		return fmt.Errorf("registrazione anteprima grafica evento fallita: %v", err)
	}
	eventGraphicPreviewRegistered = true
	return nil
}
