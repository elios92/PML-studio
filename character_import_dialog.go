//go:build windows

package main

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

const (
	characterImportClassName     = "PLMStudioCharacterImport05"
	idCharImportZoom1            = 7301
	idCharImportZoom2            = 7302
	idCharImportZoom4            = 7303
	idCharImportClearTransparent = 7304
	idCharImportClearSemi        = 7305
	idCharImportOK               = 7306
	idCharImportCancel           = 7307
)

type characterImportOptions struct {
	TransparentSet bool
	TransparentRGB uint32
	SemiSet        bool
	SemiRGB        uint32
}

var (
	characterImportRegistered                                                              bool
	characterImportOpen                                                                    bool
	characterImportWindow                                                                  syscall.Handle
	characterImportSurface                                                                 *PixelSurface
	characterImportSource                                                                  string
	characterImportZoom                                                                    = 1
	characterImportOptionsValue                                                            characterImportOptions
	characterImportAccepted                                                                bool
	characterImportDrawX, characterImportDrawY, characterImportDrawW, characterImportDrawH int
)

func surfaceToNRGBA(s *PixelSurface, opts characterImportOptions) *image.NRGBA {
	if s == nil {
		return nil
	}
	img := image.NewNRGBA(image.Rect(0, 0, s.Width, s.Height))
	for y := 0; y < s.Height; y++ {
		for x := 0; x < s.Width; x++ {
			i := y*s.Width + x
			rgbv := s.Pixels[i] & 0xFFFFFF
			a := byte(255)
			if len(s.Alpha) == len(s.Pixels) {
				a = s.Alpha[i]
			}
			if opts.TransparentSet && rgbv == opts.TransparentRGB {
				a = 0
			}
			if opts.SemiSet && rgbv == opts.SemiRGB && a != 0 {
				a = 128
			}
			img.SetNRGBA(x, y, color.NRGBA{R: byte(rgbv >> 16), G: byte(rgbv >> 8), B: byte(rgbv), A: a})
		}
	}
	return img
}

func saveImportedCharacterPNG(src string, opts characterImportOptions) (string, error) {
	if currentProject == "" {
		return "", fmt.Errorf("nessun progetto aperto")
	}
	s, err := loadPNGSurface(src)
	if err != nil {
		return "", fmt.Errorf("PNG non valido: %w", err)
	}
	img := surfaceToNRGBA(s, opts)
	if img == nil {
		return "", fmt.Errorf("immagine non valida")
	}
	dstDir := canonicalCharactersDir(currentProject)
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return "", err
	}
	dst := filepath.Join(dstDir, filepath.Base(src))
	if !samePath(src, dst) {
		if _, err := os.Stat(dst); err == nil {
			if msgboxResult("PML Studio - Characters", "Esiste già uno sprite con questo nome. Vuoi sostituirlo?\n\n"+filepath.Base(dst), MB_YESNOCANCEL|MB_ICONINFORMATION) != IDYES {
				return "", fmt.Errorf("importazione annullata")
			}
		}
	}
	tmp := dst + ".plm_import_tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return "", err
	}
	encErr := png.Encode(f, img)
	syncErr := f.Sync()
	closeErr := f.Close()
	if encErr != nil || syncErr != nil || closeErr != nil {
		_ = os.Remove(tmp)
		if encErr != nil {
			return "", encErr
		}
		if syncErr != nil {
			return "", syncErr
		}
		return "", closeErr
	}
	_ = os.Remove(dst)
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	if err := rebuildCharacterCatalog(currentProject); err != nil {
		return "", err
	}
	return strings.TrimSuffix(filepath.Base(dst), filepath.Ext(dst)), nil
}

func characterImportColorText(set bool, rgbv uint32) string {
	if !set {
		return "Nessun colore selezionato"
	}
	return fmt.Sprintf("RGB(%d, %d, %d)", byte(rgbv>>16), byte(rgbv>>8), byte(rgbv))
}

func paintColorSwatch(hdc uintptr, r RECT, set bool, rgbv uint32) {
	c := rgb(245, 245, 245)
	if set {
		c = rgb(byte(rgbv>>16), byte(rgbv>>8), byte(rgbv))
	}
	fill(syscall.Handle(hdc), r, c)
	pen, _, _ := pCreatePen.Call(0, 1, rgb(110, 110, 110))
	old, _, _ := pSelectObject.Call(hdc, pen)
	pRectangle.Call(hdc, uintptr(r.Left), uintptr(r.Top), uintptr(r.Right), uintptr(r.Bottom))
	pSelectObject.Call(hdc, old)
	pDeleteObject.Call(pen)
}

func paintCharacterImport(hwnd syscall.Handle) {
	var ps PAINTSTRUCT
	hdc, _, _ := pBeginPaint.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&ps)))
	if hdc == 0 {
		return
	}
	defer pEndPaint.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&ps)))
	preview := RECT{Left: 26, Top: 105, Right: 750, Bottom: 605}
	fill(syscall.Handle(hdc), preview, rgb(255, 255, 255))
	pen, _, _ := pCreatePen.Call(0, 1, rgb(140, 140, 140))
	old, _, _ := pSelectObject.Call(hdc, pen)
	pRectangle.Call(hdc, uintptr(preview.Left), uintptr(preview.Top), uintptr(preview.Right), uintptr(preview.Bottom))
	pSelectObject.Call(hdc, old)
	pDeleteObject.Call(pen)
	s := characterImportSurface
	characterImportDrawX, characterImportDrawY, characterImportDrawW, characterImportDrawH = 0, 0, 0, 0
	if s != nil && s.Width > 0 && s.Height > 0 {
		dw, dh := s.Width*characterImportZoom, s.Height*characterImportZoom
		maxW, maxH := int(preview.Right-preview.Left-10), int(preview.Bottom-preview.Top-10)
		// At x1 fit very large sprites into the preview; x2/x4 preserve the selected enlargement as much as possible.
		if dw > maxW || dh > maxH {
			scaleW := float64(maxW) / float64(dw)
			scaleH := float64(maxH) / float64(dh)
			scale := scaleW
			if scaleH < scale {
				scale = scaleH
			}
			dw = maxInt(1, int(float64(dw)*scale))
			dh = maxInt(1, int(float64(dh)*scale))
		}
		dx := int(preview.Left) + (int(preview.Right-preview.Left)-dw)/2
		dy := int(preview.Top) + (int(preview.Bottom-preview.Top)-dh)/2
		pixels := make([]uint32, dw*dh)
		for y := 0; y < dh; y++ {
			sy := y * s.Height / dh
			for x := 0; x < dw; x++ {
				sx := x * s.Width / dw
				i := sy*s.Width + sx
				bg := uint32(0x00FFFFFF)
				if ((x/10)+(y/10))%2 == 1 {
					bg = 0x00E8E8E8
				}
				a := byte(255)
				if len(s.Alpha) == len(s.Pixels) {
					a = s.Alpha[i]
				}
				if characterImportOptionsValue.TransparentSet && (s.Pixels[i]&0xffffff) == characterImportOptionsValue.TransparentRGB {
					a = 0
				}
				if characterImportOptionsValue.SemiSet && (s.Pixels[i]&0xffffff) == characterImportOptionsValue.SemiRGB && a != 0 {
					a = 128
				}
				pixels[y*dw+x] = blendBGR(bg, s.Pixels[i], a)
			}
		}
		bi := BITMAPINFO{BmiHeader: BITMAPINFOHEADER{BiSize: uint32(unsafe.Sizeof(BITMAPINFOHEADER{})), BiWidth: int32(dw), BiHeight: -int32(dh), BiPlanes: 1, BiBitCount: 32, BiCompression: BI_RGB}}
		pStretchDIBits.Call(hdc, uintptr(int32(dx)), uintptr(int32(dy)), uintptr(dw), uintptr(dh), 0, 0, uintptr(dw), uintptr(dh), uintptr(unsafe.Pointer(&pixels[0])), uintptr(unsafe.Pointer(&bi)), DIB_RGB_COLORS, SRCCOPY)
		characterImportDrawX, characterImportDrawY, characterImportDrawW, characterImportDrawH = dx, dy, dw, dh
	}
	// The color status labels use transparent text rendering. The import dialog
	// invalidates itself without erasing the background, therefore the previous
	// RGB string must be cleared explicitly before drawing the new value.
	// Otherwise repeated color picks are painted one over another.
	fill(syscall.Handle(hdc), RECT{Left: 40, Top: 620, Right: 370, Bottom: 728}, rgb(244, 244, 244))
	fill(syscall.Handle(hdc), RECT{Left: 395, Top: 620, Right: 760, Bottom: 728}, rgb(244, 244, 244))

	paintColorSwatch(hdc, RECT{Left: 45, Top: 650, Right: 220, Bottom: 690}, characterImportOptionsValue.TransparentSet, characterImportOptionsValue.TransparentRGB)
	paintColorSwatch(hdc, RECT{Left: 400, Top: 650, Right: 575, Bottom: 690}, characterImportOptionsValue.SemiSet, characterImportOptionsValue.SemiRGB)
	text(syscall.Handle(hdc), 45, 625, "Colore trasparente (clic sinistro)", rgb(30, 30, 30))
	text(syscall.Handle(hdc), 400, 625, "Colore semi-trasparente (clic destro)", rgb(30, 30, 30))
	text(syscall.Handle(hdc), 45, 697, characterImportColorText(characterImportOptionsValue.TransparentSet, characterImportOptionsValue.TransparentRGB), rgb(60, 60, 60))
	text(syscall.Handle(hdc), 400, 697, characterImportColorText(characterImportOptionsValue.SemiSet, characterImportOptionsValue.SemiRGB), rgb(60, 60, 60))
}

func characterImportPickColor(l uintptr, semi bool) {
	if characterImportSurface == nil || characterImportDrawW <= 0 || characterImportDrawH <= 0 {
		return
	}
	x := int(int16(loword(l)))
	y := int(int16(hiword(l)))
	if x < characterImportDrawX || y < characterImportDrawY || x >= characterImportDrawX+characterImportDrawW || y >= characterImportDrawY+characterImportDrawH {
		return
	}
	sx := (x - characterImportDrawX) * characterImportSurface.Width / characterImportDrawW
	sy := (y - characterImportDrawY) * characterImportSurface.Height / characterImportDrawH
	if sx < 0 || sy < 0 || sx >= characterImportSurface.Width || sy >= characterImportSurface.Height {
		return
	}
	rgbv := characterImportSurface.Pixels[sy*characterImportSurface.Width+sx] & 0xffffff
	if semi {
		characterImportOptionsValue.SemiSet = true
		characterImportOptionsValue.SemiRGB = rgbv
	} else {
		characterImportOptionsValue.TransparentSet = true
		characterImportOptionsValue.TransparentRGB = rgbv
	}
	invalidate(characterImportWindow)
}

func characterImportWndProcFn(hwnd syscall.Handle, msg uint32, w, l uintptr) uintptr {
	switch msg {
	case WM_COMMAND:
		switch int(loword(w)) {
		case idCharImportZoom1:
			characterImportZoom = 1
			invalidate(hwnd)
			return 0
		case idCharImportZoom2:
			characterImportZoom = 2
			invalidate(hwnd)
			return 0
		case idCharImportZoom4:
			characterImportZoom = 4
			invalidate(hwnd)
			return 0
		case idCharImportClearTransparent:
			characterImportOptionsValue.TransparentSet = false
			invalidate(hwnd)
			return 0
		case idCharImportClearSemi:
			characterImportOptionsValue.SemiSet = false
			invalidate(hwnd)
			return 0
		case idCharImportOK:
			characterImportAccepted = true
			pDestroyWindow.Call(uintptr(hwnd))
			return 0
		case idCharImportCancel:
			characterImportAccepted = false
			pDestroyWindow.Call(uintptr(hwnd))
			return 0
		}
	case WM_LBUTTONDOWN:
		characterImportPickColor(l, false)
		return 0
	case WM_RBUTTONDOWN:
		characterImportPickColor(l, true)
		return 0
	case WM_PAINT:
		paintCharacterImport(hwnd)
		return 0
	case WM_CLOSE:
		characterImportAccepted = false
		pDestroyWindow.Call(uintptr(hwnd))
		return 0
	case WM_DESTROY:
		characterImportOpen = false
		characterImportWindow = 0
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(uintptr(hwnd), uintptr(msg), w, l)
	return r
}

var characterImportWndProc = syscall.NewCallback(characterImportWndProcFn)

func ensureCharacterImportClass() error {
	if characterImportRegistered {
		return nil
	}
	h, _, _ := pGetModuleHandleW.Call(0)
	hInst := syscall.Handle(h)
	cursor, _, _ := pLoadCursorW.Call(0, IDC_ARROW)
	brush, _, _ := pCreateSolidBrush.Call(rgb(244, 244, 244))
	wc := WNDCLASSEX{cbSize: uint32(unsafe.Sizeof(WNDCLASSEX{})), style: CS_DBLCLKS, lpfnWndProc: characterImportWndProc, hInstance: hInst, hCursor: syscall.Handle(cursor), hbrBackground: syscall.Handle(brush), lpszClassName: wstr(characterImportClassName)}
	r, _, err := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	if r == 0 {
		return fmt.Errorf("registrazione Import PNG fallita: %v", err)
	}
	characterImportRegistered = true
	return nil
}

func showCharacterImportDialog(owner syscall.Handle, src string) (characterImportOptions, bool) {
	if characterImportOpen {
		return characterImportOptions{}, false
	}
	if err := ensureCharacterImportClass(); err != nil {
		msgbox("PML Studio - Import PNG", err.Error(), MB_OK|MB_ICONERROR)
		return characterImportOptions{}, false
	}
	surf, err := loadPNGSurface(src)
	if err != nil {
		msgbox("PML Studio - Import PNG", "PNG non valido:\n"+err.Error(), MB_OK|MB_ICONERROR)
		return characterImportOptions{}, false
	}
	characterImportSurface = surf
	characterImportSource = src
	characterImportZoom = 1
	characterImportOptionsValue = characterImportOptions{}
	characterImportAccepted = false
	characterImportOpen = true
	const ww, wh int32 = 800, 810
	x, y := centerOwnedWindow(ww, wh)
	h, _, _ := pGetModuleHandleW.Call(0)
	hInst := syscall.Handle(h)
	title := "Importa - " + filepath.Base(src)
	characterImportWindow = createWindow(characterImportClassName, title, WS_OVERLAPPEDWINDOW|WS_VISIBLE|WS_CLIPCHILDREN, x, y, ww, wh, owner, 0, hInst)
	if characterImportWindow == 0 {
		characterImportOpen = false
		return characterImportOptions{}, false
	}
	setWindowIcon(characterImportWindow)
	createWindow("BUTTON", "x1", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 335, 35, 90, 44, characterImportWindow, idCharImportZoom1, hInst)
	createWindow("BUTTON", "x2", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 435, 35, 90, 44, characterImportWindow, idCharImportZoom2, hInst)
	createWindow("BUTTON", "x4", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 535, 35, 90, 44, characterImportWindow, idCharImportZoom4, hInst)
	createWindow("BUTTON", "Pulisci", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 235, 650, 90, 38, characterImportWindow, idCharImportClearTransparent, hInst)
	createWindow("BUTTON", "Pulisci", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 590, 650, 90, 38, characterImportWindow, idCharImportClearSemi, hInst)
	createWindow("BUTTON", "OK", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 515, 735, 110, 38, characterImportWindow, idCharImportOK, hInst)
	createWindow("BUTTON", "Annulla", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 640, 735, 110, 38, characterImportWindow, idCharImportCancel, hInst)
	pEnableWindow.Call(uintptr(owner), 0)
	pShowWindow.Call(uintptr(characterImportWindow), SW_SHOW)
	pUpdateWindow.Call(uintptr(characterImportWindow))
	var m MSG
	repostQuit := false
	for characterImportOpen {
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
	return characterImportOptionsValue, characterImportAccepted
}
