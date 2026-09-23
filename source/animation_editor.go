//go:build windows

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

// Battle Animation editor UI ported from Pokémon Essentials v20.1's
// AnimEditor_* scripts. PLM stores the converted PBAnimations object in JSON,
// preserving unknown fields verbatim so custom projects don't lose data.

type plmAnimationDoc struct {
	Root     map[string]any
	Items    []map[string]any
	Selected int
	Path     string
}

var (
	hwndAnimPanel                  syscall.Handle
	hwndAnimCanvas                 syscall.Handle
	hwndAnimList                   syscall.Handle
	hwndAnimSearch                 syscall.Handle
	hwndAnimFrame                  syscall.Handle
	hwndAnimFrameCount             syscall.Handle
	hwndAnimSheet                  syscall.Handle
	hwndAnimName                   syscall.Handle
	hwndAnimStatus                 syscall.Handle
	hwndAnimPattern                syscall.Handle
	hwndAnimPatternCanvas          syscall.Handle
	hwndAnimListButton             syscall.Handle
	hwndAnimSave                   syscall.Handle
	hwndAnimReload                 syscall.Handle
	hwndAnimSideButtons            []syscall.Handle
	animationDoc                   *plmAnimationDoc
	animationFiltered              []int
	animationFrame                 int
	animationPattern               int
	animationPatternStart          int
	animationListOverlay           bool
	animationResources             map[string]*PixelSurface
	animationCanvasClassRegistered bool
)

const (
	idAnimSearch     = 6100
	idAnimList       = 6101
	idAnimFrame      = 6102
	idAnimFrameCount = 6103
	idAnimSheet      = 6104
	idAnimListButton = 6105
	idAnimPattern    = 6106
	idAnimName       = 6107
	idAnimSave       = 6108
	idAnimReload     = 6109
	idAnimSideBase   = 6120
)

var animationSideLabels = []string{
	"SE e sfondo...", "Focus cella...", "", "Incolla ultimo", "Copia fotogramma...", "Pulisci fotogramma...",
	"Interpolazione...", "Modifica celle in serie...", "Sposta intera animazione...", "",
	"Riproduci animazione", "Riproduci anim. avversario", "Importa anim...", "Esporta anim...", "Aiuto",
}

func animationsViewPlaceholder() string { return "" }
func animationsCanvasMessage() string   { return "" }

func animationDataCandidates() []string {
	if currentProject == "" {
		return nil
	}
	return []string{
		filepath.Join(currentProject, "converted", "data", "PkmnAnimations.json"),
		filepath.Join(currentProject, "converted", "PkmnAnimations.json"),
		filepath.Join(currentProject, "Data", "PkmnAnimations.json"),
	}
}

func animationGraphicsRoots() []string {
	if currentProject == "" {
		return nil
	}
	out := []string{}
	for _, category := range []string{"UI/Debug", "Pictures/Debug", "Pictures"} {
		out = append(out, projectGraphicsCategoryDirs(currentProject, category)...)
	}
	return uniqueExistingDirs(out)
}

func findAnimationUIResource(base string) string {
	names := []string{base + ".png", base + ".PNG", base}
	for _, root := range animationGraphicsRoots() {
		for _, name := range names {
			p := filepath.Join(root, name)
			if exists(p) {
				return p
			}
		}
	}
	return ""
}

func findAnimationSheet(name string) string {
	name = strings.TrimSpace(name)
	if name == "" || currentProject == "" {
		return ""
	}
	candidates := []string{name, name + ".png", name + ".PNG"}
	for _, root := range projectGraphicsCategoryDirs(currentProject, "Animations") {
		for _, n := range candidates {
			p := filepath.Join(root, n)
			if exists(p) {
				return p
			}
		}
	}
	return ""
}

func loadAnimationEditorResources() {
	animationResources = make(map[string]*PixelSurface)
	for _, key := range []string{"testscreen", "testback", "testfront", "arrows", "animFrameIcon"} {
		if p := findAnimationUIResource(key); p != "" {
			if s, err := loadPNGSurface(p); err == nil {
				animationResources[key] = s
			}
		}
	}
}

func loadAnimationDoc() error {
	animationDoc = nil
	animationFiltered = nil
	for _, p := range animationDataCandidates() {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var root map[string]any
		if err := json.Unmarshal(b, &root); err != nil {
			return fmt.Errorf("PkmnAnimations.json non valido: %w", err)
		}
		raw, _ := root["array"].([]any)
		items := make([]map[string]any, 0, len(raw))
		for _, v := range raw {
			if m, ok := v.(map[string]any); ok {
				items = append(items, m)
			}
		}
		sel := intFromAny(root["selected"], 0)
		if sel < 0 || sel >= len(items) {
			sel = 0
		}
		animationDoc = &plmAnimationDoc{Root: root, Items: items, Selected: sel, Path: p}
		loadAnimationEditorResources()
		refreshAnimationList("")
		selectAnimationIndex(sel)
		return nil
	}
	return fmt.Errorf("PkmnAnimations.json non trovato. Importa prima un progetto Pokémon Essentials v20.1")
}

func saveAnimationDoc() error {
	if animationDoc == nil || animationDoc.Path == "" {
		return fmt.Errorf("nessun database animazioni caricato")
	}
	arr := make([]any, len(animationDoc.Items))
	for i := range animationDoc.Items {
		arr[i] = animationDoc.Items[i]
	}
	animationDoc.Root["array"] = arr
	animationDoc.Root["selected"] = animationDoc.Selected
	b, err := json.MarshalIndent(animationDoc.Root, "", "  ")
	if err != nil {
		return err
	}
	tmp := animationDoc.Path + ".tmp"
	if err := os.WriteFile(tmp, b, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, animationDoc.Path)
}

func intFromAny(v any, def int) int {
	switch x := v.(type) {
	case float64:
		return int(x)
	case int:
		return x
	case json.Number:
		if n, e := x.Int64(); e == nil {
			return int(n)
		}
	case string:
		if n, e := strconv.Atoi(strings.TrimSpace(x)); e == nil {
			return n
		}
	}
	return def
}

func animationName(m map[string]any, idx int) string {
	if s, ok := m["name"].(string); ok && strings.TrimSpace(s) != "" {
		return s
	}
	return fmt.Sprintf("Animazione %03d", idx)
}

func animationFrames(m map[string]any) []any {
	if a, ok := m["array"].([]any); ok {
		return a
	}
	return nil
}

func refreshAnimationList(filter string) {
	if hwndAnimList == 0 {
		return
	}
	pSendMessageW.Call(uintptr(hwndAnimList), LB_RESETCONTENT, 0, 0)
	animationFiltered = animationFiltered[:0]
	if animationDoc == nil {
		return
	}
	filter = strings.ToLower(strings.TrimSpace(filter))
	for i, m := range animationDoc.Items {
		name := animationName(m, i)
		if filter != "" && !strings.Contains(strings.ToLower(name), filter) {
			continue
		}
		animationFiltered = append(animationFiltered, i)
		u, _ := syscall.UTF16PtrFromString(fmt.Sprintf("%03d: %s", i, name))
		pSendMessageW.Call(uintptr(hwndAnimList), LB_ADDSTRING, 0, uintptr(unsafe.Pointer(u)))
	}
}

func currentAnimation() map[string]any {
	if animationDoc == nil || animationDoc.Selected < 0 || animationDoc.Selected >= len(animationDoc.Items) {
		return nil
	}
	return animationDoc.Items[animationDoc.Selected]
}

func selectAnimationIndex(idx int) {
	if animationDoc == nil || idx < 0 || idx >= len(animationDoc.Items) {
		return
	}
	animationDoc.Selected = idx
	animationDoc.Root["selected"] = idx
	animationFrame = 0
	animationPattern = 0
	animationPatternStart = 0
	m := currentAnimation()
	if hwndAnimName != 0 {
		setText(hwndAnimName, animationName(m, idx))
	}
	frames := animationFrames(m)
	if hwndAnimFrame != 0 {
		maxFrame := maxInt(0, len(frames)-1)
		pSendMessageW.Call(uintptr(hwndAnimFrame), TBM_SETRANGE, 1, uintptr(uint32(maxFrame)<<16))
		pSendMessageW.Call(uintptr(hwndAnimFrame), TBM_SETPOS, 1, 0)
	}
	if hwndAnimFrameCount != 0 {
		setText(hwndAnimFrameCount, fmt.Sprintf("Fotogramma: 1 / %d", maxInt(1, len(frames))))
	}
	if hwndAnimSheet != 0 {
		sheet, _ := m["graphic"].(string)
		setText(hwndAnimSheet, "Foglio: "+sheet)
	}
	if hwndAnimPattern != 0 {
		setText(hwndAnimPattern, "Riquadro: 0")
	}
	if hwndAnimStatus != 0 {
		setText(hwndAnimStatus, fmt.Sprintf("%s | posizione %d | tonalità %d | %d fotogrammi", animationName(m, idx), intFromAny(m["position"], 0), intFromAny(m["hue"], 0), len(frames)))
	}
	// Select the corresponding visible row if filter contains it.
	for row, real := range animationFiltered {
		if real == idx {
			pSendMessageW.Call(uintptr(hwndAnimList), LB_SETCURSEL, uintptr(row), 0)
			break
		}
	}
	invalidate(hwndAnimCanvas)
	invalidate(hwndAnimPatternCanvas)
}

func currentAnimationFrame() []any {
	m := currentAnimation()
	if m == nil {
		return nil
	}
	frames := animationFrames(m)
	if animationFrame < 0 || animationFrame >= len(frames) {
		return nil
	}
	f, _ := frames[animationFrame].([]any)
	return f
}

func drawSurfaceRect(hdc uintptr, s *PixelSurface, dx, dy, dw, dh int) {
	if s == nil || s.Width <= 0 || s.Height <= 0 || dw <= 0 || dh <= 0 {
		return
	}
	px := make([]uint32, s.Width*s.Height)
	for i, rgb := range s.Pixels {
		// DIB expects BGR bytes inside the uint32 little endian layout.
		r := (rgb >> 16) & 0xFF
		g := (rgb >> 8) & 0xFF
		b := rgb & 0xFF
		px[i] = b<<16 | g<<8 | r
	}
	bi := BITMAPINFO{BmiHeader: BITMAPINFOHEADER{BiSize: uint32(unsafe.Sizeof(BITMAPINFOHEADER{})), BiWidth: int32(s.Width), BiHeight: -int32(s.Height), BiPlanes: 1, BiBitCount: 32, BiCompression: BI_RGB}}
	pStretchDIBits.Call(hdc, uintptr(int32(dx)), uintptr(int32(dy)), uintptr(dw), uintptr(dh), 0, 0, uintptr(s.Width), uintptr(s.Height), uintptr(unsafe.Pointer(&px[0])), uintptr(unsafe.Pointer(&bi)), DIB_RGB_COLORS, SRCCOPY)
}

func drawAnimationCanvas(hwnd syscall.Handle) {
	var ps PAINTSTRUCT
	hdc, _, _ := pBeginPaint.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&ps)))
	defer pEndPaint.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&ps)))
	var rc RECT
	pGetClientRect.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&rc)))
	w, h := int(rc.Right), int(rc.Bottom)
	// Use the exact Essentials editor background when the imported project has it.
	if s := animationResources["testscreen"]; s != nil {
		drawSurfaceRect(hdc, s, 0, 0, w, h)
	}
	// Battler stand-ins from Essentials editor.
	if s := animationResources["testback"]; s != nil {
		drawSurfaceRect(hdc, s, 72, h-205, 160, 160)
	}
	if s := animationResources["testfront"]; s != nil {
		drawSurfaceRect(hdc, s, w-230, 60, 160, 160)
	}
	m := currentAnimation()
	if m == nil {
		return
	}
	sheetName, _ := m["graphic"].(string)
	sheetPath := findAnimationSheet(sheetName)
	sheet, _ := loadPNGSurface(sheetPath)
	frame := currentAnimationFrame()
	if sheet == nil || len(frame) == 0 {
		return
	}
	// RPG Maker/Essentials animation sheets use 192x192 patterns in 5 columns.
	const cell = 192
	for _, cv := range frame {
		cel, ok := cv.([]any)
		if !ok || len(cel) < 9 {
			continue
		}
		pat := intFromAny(cel[7], -99)
		if pat < 0 {
			continue
		} // -1/-2 are user/target battlers, already represented above.
		sx := (pat % 5) * cell
		sy := (pat / 5) * cell
		if sx >= sheet.Width || sy >= sheet.Height {
			continue
		}
		sw := minInt(cell, sheet.Width-sx)
		sh := minInt(cell, sheet.Height-sy)
		sub := &PixelSurface{Width: sw, Height: sh, Pixels: make([]uint32, sw*sh), Alpha: make([]byte, sw*sh)}
		for yy := 0; yy < sh; yy++ {
			copy(sub.Pixels[yy*sw:(yy+1)*sw], sheet.Pixels[(sy+yy)*sheet.Width+sx:(sy+yy)*sheet.Width+sx+sw])
			if len(sheet.Alpha) == len(sheet.Pixels) {
				copy(sub.Alpha[yy*sw:(yy+1)*sw], sheet.Alpha[(sy+yy)*sheet.Width+sx:(sy+yy)*sheet.Width+sx+sw])
			}
		}
		x := intFromAny(cel[0], 0)
		y := intFromAny(cel[1], 0)
		zoom := intFromAny(cel[2], 100)
		dw := maxInt(1, cell*zoom/100)
		dh := maxInt(1, cell*zoom/100)
		drawSurfaceRect(hdc, sub, x-dw/2, y-dh/2, dw, dh)
	}
}

func drawAnimationPatternCanvas(hwnd syscall.Handle) {
	var ps PAINTSTRUCT
	hdc, _, _ := pBeginPaint.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&ps)))
	defer pEndPaint.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&ps)))
	var rc RECT
	pGetClientRect.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&rc)))
	w, h := int(rc.Right), int(rc.Bottom)
	brush, _, _ := pCreateSolidBrush.Call(rgb(180, 180, 180))
	pFillRect.Call(hdc, uintptr(unsafe.Pointer(&rc)), brush)
	pDeleteObject.Call(brush)
	m := currentAnimation()
	if m == nil {
		return
	}
	sheetName, _ := m["graphic"].(string)
	sheetPath := findAnimationSheet(sheetName)
	sheet, _ := loadPNGSurface(sheetPath)
	const thumb = 96
	arrowW := maxInt(16, (w-(5*thumb))/2)
	if arrowW > 32 {
		arrowW = 32
	}
	if sheet != nil {
		const cell = 192
		maxPatterns := (sheet.Height / cell) * 5
		if maxPatterns < 1 {
			maxPatterns = 1
		}
		if animationPatternStart < 0 {
			animationPatternStart = 0
		}
		if animationPatternStart >= maxPatterns {
			animationPatternStart = maxInt(0, maxPatterns-1)
		}
		for i := 0; i < 5; i++ {
			pat := animationPatternStart + i
			if pat >= maxPatterns {
				break
			}
			sx := (pat % 5) * cell
			sy := (pat / 5) * cell
			sw := minInt(cell, sheet.Width-sx)
			sh := minInt(cell, sheet.Height-sy)
			if sw <= 0 || sh <= 0 {
				continue
			}
			sub := &PixelSurface{Width: sw, Height: sh, Pixels: make([]uint32, sw*sh), Alpha: make([]byte, sw*sh)}
			for yy := 0; yy < sh; yy++ {
				copy(sub.Pixels[yy*sw:(yy+1)*sw], sheet.Pixels[(sy+yy)*sheet.Width+sx:(sy+yy)*sheet.Width+sx+sw])
				if len(sheet.Alpha) == len(sheet.Pixels) {
					copy(sub.Alpha[yy*sw:(yy+1)*sw], sheet.Alpha[(sy+yy)*sheet.Width+sx:(sy+yy)*sheet.Width+sx+sw])
				}
			}
			drawSurfaceRect(hdc, sub, arrowW+i*thumb, 0, thumb, minInt(thumb, h))
			// neutral frame, red for selected pattern (same semantics as Essentials)
			col := rgb(100, 100, 100)
			thickness := 1
			if pat == animationPattern {
				col = rgb(255, 0, 0)
				thickness = 2
			}
			pen, _, _ := pCreatePen.Call(PS_SOLID, uintptr(thickness), col)
			old, _, _ := pSelectObject.Call(hdc, pen)
			pMoveToEx.Call(hdc, uintptr(arrowW+i*thumb), 0, 0)
			pLineTo.Call(hdc, uintptr(arrowW+(i+1)*thumb-1), 0)
			pLineTo.Call(hdc, uintptr(arrowW+(i+1)*thumb-1), uintptr(minInt(thumb, h)-1))
			pLineTo.Call(hdc, uintptr(arrowW+i*thumb), uintptr(minInt(thumb, h)-1))
			pLineTo.Call(hdc, uintptr(arrowW+i*thumb), 0)
			pSelectObject.Call(hdc, old)
			pDeleteObject.Call(pen)
		}
	}
	// draw arrow hit areas; use imported arrows.png when present
	if a := animationResources["arrows"]; a != nil {
		drawSurfaceRect(hdc, a, 0, 0, minInt(arrowW, a.Width), minInt(h, a.Height))
		drawSurfaceRect(hdc, a, w-arrowW, 0, minInt(arrowW, a.Width), minInt(h, a.Height))
	}
}

func handleAnimationPatternClick(hwnd syscall.Handle, l uintptr) {
	var rc RECT
	pGetClientRect.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&rc)))
	w := int(rc.Right)
	x := int(int16(l & 0xffff))
	y := int(int16((l >> 16) & 0xffff))
	if y < 0 || y >= 96 {
		return
	}
	const thumb = 96
	arrowW := maxInt(16, (w-(5*thumb))/2)
	if arrowW > 32 {
		arrowW = 32
	}
	m := currentAnimation()
	if m == nil {
		return
	}
	sheetName, _ := m["graphic"].(string)
	sheet, _ := loadPNGSurface(findAnimationSheet(sheetName))
	if sheet == nil {
		return
	}
	maxPatterns := (sheet.Height / 192) * 5
	if maxPatterns < 1 {
		return
	}
	if x < arrowW {
		animationPatternStart = maxInt(0, animationPatternStart-1)
		invalidate(hwnd)
		return
	}
	if x >= w-arrowW {
		animationPatternStart = minInt(maxInt(0, maxPatterns-1), animationPatternStart+1)
		invalidate(hwnd)
		return
	}
	idx := (x - arrowW) / thumb
	if idx >= 0 && idx < 5 {
		pat := animationPatternStart + idx
		if pat < maxPatterns {
			animationPattern = pat
			setText(hwndAnimPattern, fmt.Sprintf("Riquadro: %d", pat))
			invalidate(hwnd)
		}
	}
}

func setAnimationListOverlay(show bool) {
	animationListOverlay = show
	showControl(hwndAnimSearch, show)
	showControl(hwndAnimList, show)
	if show {
		pSetFocus.Call(uintptr(hwndAnimSearch))
	}
}

func animationCanvasWndProc(hwnd syscall.Handle, msg uint32, w, l uintptr) uintptr {
	switch msg {
	case WM_PAINT:
		if hwnd == hwndAnimPatternCanvas {
			drawAnimationPatternCanvas(hwnd)
		} else {
			drawAnimationCanvas(hwnd)
		}
		return 0
	case WM_LBUTTONDOWN:
		if hwnd == hwndAnimPatternCanvas {
			handleAnimationPatternClick(hwnd, l)
			return 0
		}
	}
	r, _, _ := pDefWindowProcW.Call(uintptr(hwnd), uintptr(msg), w, l)
	return r
}

func ensureAnimationCanvasClass() error {
	if animationCanvasClassRegistered {
		return nil
	}
	cn, _ := syscall.UTF16PtrFromString("PLMAnimationCanvas")
	cur, _, _ := pLoadCursorW.Call(0, IDC_ARROW)
	mh, _, _ := pGetModuleHandleW.Call(0)
	brush, _, _ := pCreateSolidBrush.Call(rgb(32, 32, 32))
	wc := WNDCLASSEX{cbSize: uint32(unsafe.Sizeof(WNDCLASSEX{})), lpfnWndProc: syscall.NewCallback(animationCanvasWndProc), hInstance: syscall.Handle(mh), hCursor: syscall.Handle(cur), hbrBackground: syscall.Handle(brush), lpszClassName: cn}
	if a, _, e := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); a == 0 {
		return fmt.Errorf("registrazione canvas animazioni fallita: %v", e)
	}
	animationCanvasClassRegistered = true
	return nil
}

func createAnimationEditor(h syscall.Handle) {
	if hwndAnimPanel != 0 {
		return
	}
	_ = ensureAnimationCanvasClass()
	hwndAnimPanel = createWindow("STATIC", "", WS_CHILD|WS_CLIPCHILDREN, 0, 0, 0, 0, hwndMain, 6090, h)
	hwndAnimSearch = createWindow("EDIT", "", WS_CHILD|WS_VISIBLE|WS_BORDER|0x0080, 0, 0, 0, 0, hwndAnimPanel, idAnimSearch, h)
	hwndAnimList = createWindow("LISTBOX", "", WS_CHILD|WS_VISIBLE|WS_BORDER|WS_VSCROLL|LBS_NOTIFY, 0, 0, 0, 0, hwndAnimPanel, idAnimList, h)
	hwndAnimCanvas = createWindow("PLMAnimationCanvas", "", WS_CHILD|WS_VISIBLE|WS_BORDER, 0, 0, 0, 0, hwndAnimPanel, 6091, h)
	hwndAnimPatternCanvas = createWindow("PLMAnimationCanvas", "", WS_CHILD|WS_VISIBLE|WS_BORDER, 0, 0, 0, 0, hwndAnimPanel, 6111, h)
	hwndAnimFrame = createWindow("msctls_trackbar32", "", WS_CHILD|WS_VISIBLE|TBS_AUTOTICKS, 0, 0, 0, 0, hwndAnimPanel, idAnimFrame, h)
	hwndAnimFrameCount = createWindow("STATIC", "Fotogramma: 1 / 1", WS_CHILD|WS_VISIBLE, 0, 0, 0, 0, hwndAnimPanel, idAnimFrameCount, h)
	hwndAnimSheet = createWindow("BUTTON", "Imposta foglio animazione", WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_PUSHBUTTON, 0, 0, 0, 0, hwndAnimPanel, idAnimSheet, h)
	hwndAnimListButton = createWindow("BUTTON", "Lista animazioni", WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_PUSHBUTTON, 0, 0, 0, 0, hwndAnimPanel, idAnimListButton, h)
	hwndAnimPattern = createWindow("STATIC", "Riquadro: 0", WS_CHILD|WS_VISIBLE|WS_BORDER, 0, 0, 0, 0, hwndAnimPanel, idAnimPattern, h)
	hwndAnimName = createWindow("EDIT", "", WS_CHILD|WS_VISIBLE|WS_BORDER|0x0080, 0, 0, 0, 0, hwndAnimPanel, idAnimName, h)
	hwndAnimStatus = createWindow("STATIC", "", WS_CHILD|WS_VISIBLE, 0, 0, 0, 0, hwndAnimPanel, 6110, h)
	hwndAnimSave = createWindow("BUTTON", "Salva", WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_PUSHBUTTON, 0, 0, 0, 0, hwndAnimPanel, idAnimSave, h)
	hwndAnimReload = createWindow("BUTTON", "Ricarica", WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_PUSHBUTTON, 0, 0, 0, 0, hwndAnimPanel, idAnimReload, h)
	for i, label := range animationSideLabels {
		if label == "" {
			hwndAnimSideButtons = append(hwndAnimSideButtons, 0)
			continue
		}
		b := createWindow("BUTTON", label, WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_PUSHBUTTON|BS_MULTILINE, 0, 0, 0, 0, hwndAnimPanel, uintptr(idAnimSideBase+i), h)
		hwndAnimSideButtons = append(hwndAnimSideButtons, b)
	}
	setAnimationListOverlay(false)
	showControl(hwndAnimPanel, false)
}

func showAnimationEditor(show bool) {
	if hwndAnimPanel == 0 {
		return
	}
	showControl(hwndAnimPanel, show)
	if show {
		if err := loadAnimationDoc(); err != nil {
			setText(hwndAnimStatus, err.Error())
		}
	}
}

func layoutAnimationEditor(x, y, w, h int32) {
	if hwndAnimPanel == 0 || w < 760 || h < 600 {
		return
	}
	moveControl(hwndAnimPanel, x, y, w, h)

	// Layout PLM più leggibile dell'originale v20.1: stessa struttura funzionale,
	// ma con spazio sufficiente per le etichette italiane e senza padding eccessivo.
	const outerPad int32 = 6
	const gap int32 = 6
	const bottomH int32 = 170
	const sideMin int32 = 230
	const sideMax int32 = 280

	sideW := w / 5
	if sideW < sideMin {
		sideW = sideMin
	}
	if sideW > sideMax {
		sideW = sideMax
	}
	mainW := w - sideW - outerPad*2 - gap
	if mainW < 500 {
		mainW = 500
		sideW = maxI32(200, w-mainW-outerPad*2-gap)
	}
	canvasH := h - bottomH - outerPad*2 - gap
	if canvasH < 360 {
		canvasH = 360
	}

	moveControl(hwndAnimCanvas, outerPad, outerPad, mainW, canvasH)

	// Menu verticale destro: pulsanti più larghi/alti per evitare testi tagliati.
	sx := outerPad + mainW + gap
	sy := outerPad
	for _, b := range hwndAnimSideButtons {
		if b == 0 {
			sy += 7
			continue
		}
		moveControl(b, sx, sy, sideW, 34)
		sy += 38
	}

	bottomY := outerPad + canvasH + gap
	leftControlsW := int32(310)
	if mainW < 720 {
		leftControlsW = 280
	}

	// Controlli fotogramma: l'etichetta non viene più compressa a 56 px.
	frameLabelW := int32(155)
	trackW := leftControlsW - frameLabelW - gap
	moveControl(hwndAnimFrame, outerPad, bottomY, trackW, 30)
	moveControl(hwndAnimFrameCount, outerPad+trackW+gap, bottomY+4, frameLabelW, 24)

	// Pulsanti principali su due colonne, con testo sempre leggibile.
	btnGap := int32(6)
	btnW := (leftControlsW - btnGap) / 2
	moveControl(hwndAnimSheet, outerPad, bottomY+36, leftControlsW, 32)
	moveControl(hwndAnimListButton, outerPad, bottomY+74, leftControlsW, 32)
	moveControl(hwndAnimSave, outerPad, bottomY+112, btnW, 32)
	moveControl(hwndAnimReload, outerPad+btnW+btnGap, bottomY+112, btnW, 32)

	// Foglio animazione e proprietà base.
	stripX := outerPad + leftControlsW + gap
	stripW := mainW - leftControlsW - gap
	if stripW < 260 {
		stripW = 260
	}
	moveControl(hwndAnimPatternCanvas, stripX, bottomY, stripW, 96)

	patternW := int32(135)
	moveControl(hwndAnimPattern, stripX, bottomY+102, patternW, 26)
	moveControl(hwndAnimName, stripX+patternW+gap, bottomY+102, maxI32(140, stripW-patternW-gap), 28)
	moveControl(hwndAnimStatus, stripX, bottomY+136, stripW, 28)

	// Lista animazioni: overlay temporaneo, con margini ridotti e più spazio utile.
	overlayW := maxI32(340, mainW/2)
	moveControl(hwndAnimSearch, outerPad+10, outerPad+10, overlayW-20, 30)
	moveControl(hwndAnimList, outerPad+10, outerPad+46, overlayW-20, maxI32(220, canvasH-56))
}

func maxI32(a, b int32) int32 {
	if a > b {
		return a
	}
	return b
}
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func updateAnimationFrameUI() {
	m := currentAnimation()
	if m == nil {
		return
	}
	frames := animationFrames(m)
	if len(frames) == 0 {
		animationFrame = 0
	} else {
		if animationFrame < 0 {
			animationFrame = 0
		}
		if animationFrame >= len(frames) {
			animationFrame = len(frames) - 1
		}
	}
	setText(hwndAnimFrameCount, fmt.Sprintf("Fotogramma: %d / %d", animationFrame+1, maxInt(1, len(frames))))
	invalidate(hwndAnimCanvas)
}

func handleAnimationEditorCommand(id, notify int) bool {
	if mode != "animations" {
		return false
	}
	switch id {
	case idAnimSearch:
		if notify == 0x0300 {
			refreshAnimationList(getText(hwndAnimSearch))
			return true
		}
	case idAnimList:
		if notify == LBN_SELCHANGE || notify == LBN_DBLCLK {
			r, _, _ := pSendMessageW.Call(uintptr(hwndAnimList), LB_GETCURSEL, 0, 0)
			row := int(r)
			if row >= 0 && row < len(animationFiltered) {
				selectAnimationIndex(animationFiltered[row])
				if notify == LBN_DBLCLK {
					setAnimationListOverlay(false)
				}
			}
			return true
		}
	case idAnimName:
		if notify == 0x0300 && currentAnimation() != nil {
			currentAnimation()["name"] = getText(hwndAnimName)
			return true
		}
	case idAnimSave:
		if err := saveAnimationDoc(); err != nil {
			msgbox("PML Studio - Animazioni", "Salvataggio non riuscito:\r\n"+err.Error(), MB_OK|MB_ICONERROR)
		} else {
			setText(hwndAnimStatus, "Animazioni salvate in "+animationDoc.Path)
		}
		return true
	case idAnimReload:
		_ = loadAnimationDoc()
		return true
	case idAnimSheet:
		// Sheet selection comes from the project's actual Graphics/Animations assets.
		sheets := listProjectAnimationSheets()
		if len(sheets) == 0 {
			msgbox("PML Studio - Animazioni", "Nessun foglio animazione trovato in assets/Graphics/Animations.", MB_OK|MB_ICONINFORMATION)
			return true
		}
		cur, _ := currentAnimation()["graphic"].(string)
		next := cycleString(sheets, cur)
		currentAnimation()["graphic"] = next
		setText(hwndAnimSheet, "Foglio: "+next)
		invalidate(hwndAnimCanvas)
		invalidate(hwndAnimPatternCanvas)
		return true
	case idAnimListButton:
		setAnimationListOverlay(!animationListOverlay)
		return true
	}
	if id >= idAnimSideBase && id < idAnimSideBase+len(animationSideLabels) {
		idx := id - idAnimSideBase
		handleAnimationSideAction(idx)
		return true
	}
	return false
}

func handleAnimationEditorScroll(scrollHwnd syscall.Handle) bool {
	if mode != "animations" || scrollHwnd != hwndAnimFrame {
		return false
	}
	pos, _, _ := pSendMessageW.Call(uintptr(hwndAnimFrame), TBM_GETPOS, 0, 0)
	animationFrame = int(pos)
	updateAnimationFrameUI()
	return true
}

func listProjectAnimationSheets() []string {
	if currentProject == "" {
		return nil
	}
	seen := map[string]bool{}
	out := []string{}
	for _, root := range projectGraphicsCategoryDirs(currentProject, "Animations") {
		entries, _ := os.ReadDir(root)
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			ext := strings.ToLower(filepath.Ext(e.Name()))
			if ext != ".png" && ext != ".gif" && ext != ".jpg" && ext != ".jpeg" {
				continue
			}
			n := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
			if !seen[strings.ToLower(n)] {
				seen[strings.ToLower(n)] = true
				out = append(out, n)
			}
		}
	}
	sort.Strings(out)
	return out
}

func cycleString(all []string, cur string) string {
	if len(all) == 0 {
		return ""
	}
	for i, s := range all {
		if strings.EqualFold(s, cur) {
			return all[(i+1)%len(all)]
		}
	}
	return all[0]
}

func handleAnimationSideAction(idx int) {
	switch idx {
	case 2, 9:
		return
	case 5: // Clear current frame
		m := currentAnimation()
		if m == nil {
			return
		}
		frames := animationFrames(m)
		if animationFrame >= 0 && animationFrame < len(frames) {
			frames[animationFrame] = []any{}
			m["array"] = frames
			invalidate(hwndAnimCanvas)
		}
	case 10, 11:
		// Preview uses the same canvas/resources and advances all frames. This is
		// intentionally editor-only; battle runtime playback stays in Python runtime.
		previewAnimationFrames()
	case 12:
		msgbox("PML Studio - Importa animazione", "L’importazione/esportazione PBAnimation usa il formato JSON convertito; per ora viene utilizzato PkmnAnimations.json del progetto.", MB_OK|MB_ICONINFORMATION)
	case 13:
		if err := saveAnimationDoc(); err != nil {
			msgbox("PML Studio - Animazioni", err.Error(), MB_OK|MB_ICONERROR)
		} else {
			msgbox("PML Studio - Animazioni", "Database animazioni esportato/salvato in:\r\n"+animationDoc.Path, MB_OK|MB_ICONINFORMATION)
		}
	case 14:
		msgbox("PML Studio - Editor Animazioni di Battaglia", "Interfaccia basata sull’Editor Animazioni di Battaglia di Pokémon Essentials v20.1.\r\n\r\nFotogramma: usa il cursore.\r\nFoglio animazione: usa gli asset reali del progetto.\r\nLe risorse Debug vengono cercate in Graphics/UI/Debug e Graphics/Pictures/Debug, con fallback su Pictures.", MB_OK|MB_ICONINFORMATION)
	default:
		msgbox("PML Studio - Editor Animazioni di Battaglia", animationSideLabels[idx]+" è presente nell’interfaccia v20.1. Il relativo editor avanzato verrà collegato ai dati PBAnimation senza alterare i campi personalizzati.", MB_OK|MB_ICONINFORMATION)
	}
}

func previewAnimationFrames() {
	m := currentAnimation()
	if m == nil {
		return
	}
	frames := animationFrames(m)
	if len(frames) == 0 {
		return
	}
	old := animationFrame
	for i := 0; i < len(frames); i++ {
		animationFrame = i
		updateAnimationFrameUI() // pump paint synchronously
		pUpdateWindow.Call(uintptr(hwndAnimCanvas))
		time.Sleep(50 * time.Millisecond)
	}
	animationFrame = old
	updateAnimationFrameUI()
}

// Trackbar constants used by the Essentials-style frame slider.
const (
	TBS_AUTOTICKS = 0x0001
	TBM_GETPOS    = 0x0400
	TBM_SETPOS    = 0x0405
	TBM_SETRANGE  = 0x0406
)
