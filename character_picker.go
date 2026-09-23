//go:build windows

package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

const (
	characterPickerClassName = "PLMStudioCharacterPicker05"
	idCharPickerList         = 7100
	idCharPickerImport       = 7101
	idCharPickerUse          = 7102
	idCharPickerNone         = 7103
	idCharPickerCancel       = 7104
)

var (
	characterPickerRegistered bool
	characterPickerOpen       bool
	characterPickerWindow     syscall.Handle
	characterPickerList       syscall.Handle
	characterPickerInfo       syscall.Handle
	characterPickerPath       syscall.Handle
	characterPickerNames      []string
	characterPickerSelected   string
	characterPickerSurface    *PixelSurface
)

func canonicalCharactersDir(root string) string {
	if strings.TrimSpace(root) == "" {
		return ""
	}
	preferred := filepath.Join(root, "assets", "Graphics", "Characters")
	if st, err := os.Stat(preferred); err == nil && st.IsDir() {
		return preferred
	}
	legacy := filepath.Join(root, "Graphics", "Characters")
	if st, err := os.Stat(legacy); err == nil && st.IsDir() {
		return legacy
	}
	return preferred
}

func importCharacterPNG(src string) (string, error) {
	if currentProject == "" {
		return "", fmt.Errorf("nessun progetto aperto")
	}
	if !strings.EqualFold(filepath.Ext(src), ".png") {
		return "", fmt.Errorf("il file deve essere un PNG")
	}
	if _, err := loadPNGSurface(src); err != nil {
		return "", fmt.Errorf("PNG non valido: %w", err)
	}
	dstDir := canonicalCharactersDir(currentProject)
	if dstDir == "" {
		return "", fmt.Errorf("cartella Characters non disponibile")
	}
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return "", fmt.Errorf("creazione Graphics/Characters: %w", err)
	}
	dst := filepath.Join(dstDir, filepath.Base(src))
	if samePath(src, dst) {
		return strings.TrimSuffix(filepath.Base(dst), filepath.Ext(dst)), nil
	}
	if _, err := os.Stat(dst); err == nil {
		if msgboxResult("PML Studio - Characters", "Esiste già uno sprite con questo nome. Vuoi sostituirlo?\n\n"+filepath.Base(dst), MB_YESNOCANCEL|MB_ICONINFORMATION) != IDYES {
			return "", fmt.Errorf("importazione annullata")
		}
	}
	in, err := os.Open(src)
	if err != nil {
		return "", err
	}
	defer in.Close()
	tmp := dst + ".plm_import_tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return "", err
	}
	_, copyErr := io.Copy(out, in)
	syncErr := out.Sync()
	closeErr := out.Close()
	if copyErr != nil || syncErr != nil || closeErr != nil {
		_ = os.Remove(tmp)
		if copyErr != nil {
			return "", copyErr
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
	if _, err := loadPNGSurface(dst); err != nil {
		return "", fmt.Errorf("verifica PNG importato: %w", err)
	}
	if err := rebuildCharacterCatalog(currentProject); err != nil {
		return "", err
	}
	return strings.TrimSuffix(filepath.Base(dst), filepath.Ext(dst)), nil
}

func refreshCharacterPickerList(preselect string) {
	ensureCharacterCatalog()
	characterPickerNames = characterSpriteNames()
	clearList(characterPickerList)
	sel := -1
	for i, name := range characterPickerNames {
		addList(characterPickerList, name)
		if preselect != "" && strings.EqualFold(name, preselect) {
			sel = i
		}
	}
	if sel < 0 && len(characterPickerNames) > 0 {
		sel = 0
	}
	if sel >= 0 {
		pSendMessageW.Call(uintptr(characterPickerList), LB_SETCURSEL, uintptr(sel), 0)
		selectCharacterPickerIndex(sel)
	} else {
		characterPickerSelected = ""
		characterPickerSurface = nil
		setText(characterPickerInfo, "Nessuno sprite disponibile")
		setText(characterPickerPath, "Usa Importa PNG... per aggiungere un file a Graphics/Characters")
		invalidate(characterPickerWindow)
	}
}

func selectCharacterPickerIndex(idx int) {
	if idx < 0 || idx >= len(characterPickerNames) {
		return
	}
	name := characterPickerNames[idx]
	characterPickerSelected = name
	surf, path, err := characterSpriteSurface(name)
	if err != nil {
		characterPickerSurface = nil
		setText(characterPickerInfo, name+" - errore caricamento")
		setText(characterPickerPath, err.Error())
	} else {
		characterPickerSurface = surf
		setText(characterPickerInfo, fmt.Sprintf("%s   %dx%d px", name, surf.Width, surf.Height))
		setText(characterPickerPath, path)
	}
	invalidate(characterPickerWindow)
}

func paintCharacterPicker(hwnd syscall.Handle) {
	var ps PAINTSTRUCT
	hdc, _, _ := pBeginPaint.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&ps)))
	if hdc == 0 {
		return
	}
	defer pEndPaint.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&ps)))

	// Preview frame.
	preview := RECT{Left: 365, Top: 62, Right: 685, Bottom: 382}
	fill(syscall.Handle(hdc), preview, rgb(245, 245, 245))
	pen, _, _ := pCreatePen.Call(0, 1, rgb(150, 150, 150))
	oldPen, _, _ := pSelectObject.Call(hdc, pen)
	pRectangle.Call(hdc, uintptr(preview.Left), uintptr(preview.Top), uintptr(preview.Right), uintptr(preview.Bottom))
	pSelectObject.Call(hdc, oldPen)
	pDeleteObject.Call(pen)

	s := characterPickerSurface
	if s == nil || s.Width <= 0 || s.Height <= 0 {
		return
	}
	maxW, maxH := int(preview.Right-preview.Left-20), int(preview.Bottom-preview.Top-20)
	dw, dh := s.Width, s.Height
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
	pixels := make([]uint32, dw*dh)
	for y := 0; y < dh; y++ {
		sy := y * s.Height / dh
		for x := 0; x < dw; x++ {
			sx := x * s.Width / dw
			si := sy*s.Width + sx
			// Checkerboard makes transparency immediately visible.
			bg := uint32(0x00E7E7E7)
			if ((x/8)+(y/8))%2 == 1 {
				bg = 0x00FFFFFF
			}
			a := byte(255)
			if len(s.Alpha) == len(s.Pixels) {
				a = s.Alpha[si]
			}
			pixels[y*dw+x] = blendBGR(bg, s.Pixels[si], a)
		}
	}
	bi := BITMAPINFO{BmiHeader: BITMAPINFOHEADER{BiSize: uint32(unsafe.Sizeof(BITMAPINFOHEADER{})), BiWidth: int32(dw), BiHeight: -int32(dh), BiPlanes: 1, BiBitCount: 32, BiCompression: BI_RGB}}
	dx := int(preview.Left) + (int(preview.Right-preview.Left)-dw)/2
	dy := int(preview.Top) + (int(preview.Bottom-preview.Top)-dh)/2
	pStretchDIBits.Call(hdc, uintptr(int32(dx)), uintptr(int32(dy)), uintptr(dw), uintptr(dh), 0, 0, uintptr(dw), uintptr(dh), uintptr(unsafe.Pointer(&pixels[0])), uintptr(unsafe.Pointer(&bi)), DIB_RGB_COLORS, SRCCOPY)
}

func characterPickerWndProcFn(hwnd syscall.Handle, msg uint32, w, l uintptr) uintptr {
	switch msg {
	case WM_COMMAND:
		id := int(loword(w))
		notify := int(hiword(w))
		if id == idCharPickerList && notify == LBN_SELCHANGE {
			r, _, _ := pSendMessageW.Call(uintptr(characterPickerList), LB_GETCURSEL, 0, 0)
			selectCharacterPickerIndex(int(r))
			return 0
		}
		if id == idCharPickerList && notify == LBN_DBLCLK {
			r, _, _ := pSendMessageW.Call(uintptr(characterPickerList), LB_GETCURSEL, 0, 0)
			selectCharacterPickerIndex(int(r))
			if strings.TrimSpace(characterPickerSelected) != "" {
				pDestroyWindow.Call(uintptr(hwnd))
			}
			return 0
		}
		switch id {
		case idCharPickerImport:
			src := browseCharacterPNG("Importa sprite evento", hwnd)
			if src == "" {
				return 0
			}
			opts, ok := showCharacterImportDialog(hwnd, src)
			if !ok {
				return 0
			}
			name, err := saveImportedCharacterPNG(src, opts)
			if err != nil {
				if !strings.Contains(strings.ToLower(err.Error()), "annullata") {
					msgbox("PML Studio - Characters", "Importazione non riuscita:\n"+err.Error(), MB_OK|MB_ICONERROR)
				}
				return 0
			}
			refreshCharacterPickerList(name)
			setToolbarStatus("Sprite importato in Graphics/Characters: " + name)
			return 0
		case idCharPickerUse:
			if strings.TrimSpace(characterPickerSelected) == "" {
				msgbox("PML Studio - Characters", "Seleziona prima uno sprite.", MB_OK|MB_ICONINFORMATION)
				return 0
			}
			pDestroyWindow.Call(uintptr(hwnd))
			return 0
		case idCharPickerNone:
			characterPickerSelected = ""
			pDestroyWindow.Call(uintptr(hwnd))
			return 0
		case idCharPickerCancel:
			characterPickerSelected = "\x00CANCEL"
			pDestroyWindow.Call(uintptr(hwnd))
			return 0
		}
	case WM_PAINT:
		paintCharacterPicker(hwnd)
		return 0
	case WM_CLOSE:
		characterPickerSelected = "\x00CANCEL"
		pDestroyWindow.Call(uintptr(hwnd))
		return 0
	case WM_DESTROY:
		characterPickerOpen = false
		characterPickerWindow = 0
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(uintptr(hwnd), uintptr(msg), w, l)
	return r
}

var characterPickerWndProc = syscall.NewCallback(characterPickerWndProcFn)

func ensureCharacterPickerClass() error {
	if characterPickerRegistered {
		return nil
	}
	h, _, _ := pGetModuleHandleW.Call(0)
	hInst := syscall.Handle(h)
	cursor, _, _ := pLoadCursorW.Call(0, IDC_ARROW)
	brush, _, _ := pCreateSolidBrush.Call(rgb(244, 244, 244))
	wc := WNDCLASSEX{cbSize: uint32(unsafe.Sizeof(WNDCLASSEX{})), lpfnWndProc: characterPickerWndProc, hInstance: hInst, hCursor: syscall.Handle(cursor), hbrBackground: syscall.Handle(brush), lpszClassName: wstr(characterPickerClassName)}
	r, _, err := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	if r == 0 {
		return fmt.Errorf("registrazione finestra Characters fallita: %v", err)
	}
	characterPickerRegistered = true
	return nil
}

// showCharacterPicker returns (name,true) when the user chooses a sprite,
// ("",true) for Nessuna grafica, and (_,false) when the dialog is cancelled.
func showCharacterPicker(owner syscall.Handle, current string) (string, bool) {
	if characterPickerOpen {
		return "", false
	}
	if err := ensureCharacterPickerClass(); err != nil {
		msgbox("PML Studio - Characters", err.Error(), MB_OK|MB_ICONERROR)
		return "", false
	}
	ensureCharacterCatalog()
	const ww, wh int32 = 720, 520
	x, y := centerOwnedWindow(ww, wh)
	h, _, _ := pGetModuleHandleW.Call(0)
	hInst := syscall.Handle(h)
	characterPickerSelected = current
	characterPickerSurface = nil
	characterPickerOpen = true
	characterPickerWindow = createWindow(characterPickerClassName, "Grafica evento - Graphics/Characters", WS_OVERLAPPEDWINDOW|WS_VISIBLE|WS_CLIPCHILDREN, x, y, ww, wh, owner, 0, hInst)
	if characterPickerWindow == 0 {
		characterPickerOpen = false
		return "", false
	}
	setWindowIcon(characterPickerWindow)

	createWindow("STATIC", "Sprite disponibili:", WS_CHILD|WS_VISIBLE, 18, 18, 220, 24, characterPickerWindow, 7110, hInst)
	characterPickerList = createWindow("LISTBOX", "", WS_CHILD|WS_VISIBLE|WS_BORDER|WS_VSCROLL|WS_TABSTOP|LBS_NOTIFY|LBS_NOINTEGRALHEIGHT, 18, 45, 320, 370, characterPickerWindow, idCharPickerList, hInst)
	createWindow("STATIC", "Anteprima", WS_CHILD|WS_VISIBLE, 365, 35, 160, 24, characterPickerWindow, 7111, hInst)
	characterPickerInfo = createWindow("STATIC", "", WS_CHILD|WS_VISIBLE, 365, 392, 320, 24, characterPickerWindow, 7112, hInst)
	characterPickerPath = createWindow("STATIC", "", WS_CHILD|WS_VISIBLE, 365, 418, 320, 42, characterPickerWindow, 7113, hInst)
	createWindow("BUTTON", "Importa PNG...", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 18, 430, 140, 34, characterPickerWindow, idCharPickerImport, hInst)
	createWindow("BUTTON", "Nessuna grafica", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 168, 430, 140, 34, characterPickerWindow, idCharPickerNone, hInst)
	createWindow("BUTTON", "Usa sprite", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 485, 465, 100, 34, characterPickerWindow, idCharPickerUse, hInst)
	createWindow("BUTTON", "Annulla", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 595, 465, 90, 34, characterPickerWindow, idCharPickerCancel, hInst)

	refreshCharacterPickerList(current)
	pEnableWindow.Call(uintptr(owner), 0)
	pShowWindow.Call(uintptr(characterPickerWindow), SW_SHOW)
	pUpdateWindow.Call(uintptr(characterPickerWindow))
	var m MSG
	repostQuit := false
	for characterPickerOpen {
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
	if characterPickerSelected == "\x00CANCEL" {
		return "", false
	}
	return characterPickerSelected, true
}
