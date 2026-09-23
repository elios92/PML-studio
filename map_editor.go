//go:build windows

package main

import (
	"fmt"
	"strings"
	"syscall"
	"unsafe"
)

var mapMouseDown bool

func drawOutlineRect(hdc uintptr, left, top, right, bottom int32) {
	if hdc == 0 || right <= left || bottom <= top {
		return
	}
	pMoveToEx.Call(hdc, uintptr(left), uintptr(top), 0)
	pLineTo.Call(hdc, uintptr(right), uintptr(top))
	pLineTo.Call(hdc, uintptr(right), uintptr(bottom))
	pLineTo.Call(hdc, uintptr(left), uintptr(bottom))
	pLineTo.Call(hdc, uintptr(left), uintptr(top))
}

// tintPermissionCell sovrappone una tinta leggera ai pixel della tile senza
// nascondere la grafica sottostante. color e' un COLORREF Win32 (0x00BBGGRR),
// mentre i pixel della DIB sono memorizzati come 0x00RRGGBB.
func tintPermissionCell(px []uint32, color uintptr, alpha byte) {
	if len(px) < 32*32 || alpha == 0 {
		return
	}
	r := uint32(color & 0xff)
	g := uint32((color >> 8) & 0xff)
	b := uint32((color >> 16) & 0xff)
	tint := r<<16 | g<<8 | b
	for i := 0; i < 32*32; i++ {
		px[i] = blendBGR(px[i], tint, alpha)
	}
}

func eventGraphicState(e EditorEvent) int {
	// 2 = renderizzabile, 1 = grafica dichiarata ma file mancante, 0 = nessuna grafica.
	if strings.TrimSpace(e.CharacterName) != "" {
		if characterSpritePath(e.CharacterName) != "" {
			return 2
		}
		return 1
	}
	if e.GraphicTileID > 0 {
		return 2
	}
	return 0
}

func drawEventTypeMarker(hdc syscall.Handle, dx, dy int32, tile int, kind string) bool {
	if hdc == 0 || tile <= 0 {
		return false
	}
	kind = strings.ToLower(strings.TrimSpace(kind))
	label := ""
	color := uintptr(0)
	textColor := rgb(255, 255, 255)
	switch kind {
	case "warp":
		label = "W"
		color = rgb(132, 62, 168) // viola: Transfer Player / Warp
	case "script":
		label = "S"
		color = rgb(58, 96, 145) // blu acciaio: script puro
	default:
		return false
	}
	margin := int32(2)
	if tile < 12 {
		margin = 1
	}
	r := RECT{Left: dx + margin, Top: dy + margin, Right: dx + int32(tile) - margin, Bottom: dy + int32(tile) - margin}
	brush := createSolidBrush(color)
	if brush != 0 {
		fillWithBrush(hdc, r, brush)
		deleteGDIObject(brush)
	}
	// Posizione leggibile anche con zoom diversi. Il helper text usa fondo
	// trasparente, quindi la lettera resta sopra la casella colorata.
	tx := dx + int32(tile/2) - 5
	ty := dy + int32(tile/2) - 9
	if tx < dx+2 {
		tx = dx + 2
	}
	if ty < dy+2 {
		ty = dy + 2
	}
	text(hdc, tx, ty, label, textColor)
	return true
}

func drawEventGraphicsOverlay(hdc syscall.Handle, startX, startY, visibleCols, visibleRows, offsetX, offsetY, tile int) {
	if hdc == 0 || tile <= 0 || currentMapDoc == nil {
		return
	}
	cell := make([]uint32, 32*32)
	for _, e := range events {
		if e.X < startX || e.X >= startX+visibleCols || e.Y < startY || e.Y >= startY+visibleRows {
			continue
		}
		if e.X < 0 || e.Y < 0 || e.X >= currentMapW || e.Y >= currentMapH {
			continue
		}
		composeMapCell(e.X, e.Y, cell)
		drawn := false
		if strings.TrimSpace(e.CharacterName) != "" {
			drawn = blendEventCharacterOnCell(cell, e)
		} else if e.GraphicTileID > 0 {
			if e.GraphicTileID >= 384 {
				drawn = composeNormalTile(e.GraphicTileID, cell)
			} else {
				drawn = composeAutotile(e.GraphicTileID, cell)
			}
		}
		if !drawn {
			continue
		}
		dx := int32(offsetX + (e.X-startX)*tile)
		dy := int32(offsetY + (e.Y-startY)*tile)
		stretchTileBuffer(hdc, cell, dx, dy, int32(tile))
	}
}

func permissionOverlayTextColor(x, y int) uintptr {
	// Testo scuro come Advance Map; sui codici molto scuri usa bianco.
	c := permissionOverlayColor(x, y)
	r := int(c & 0xff)
	g := int((c >> 8) & 0xff)
	b := int((c >> 16) & 0xff)
	if r+g+b < 210 {
		return rgb(245, 245, 245)
	}
	return rgb(20, 20, 20)
}

func canvasPaint(hwnd syscall.Handle) {
	ds := diagFunctionStart("UI", "canvasPaint()")
	defer diagFunctionEnd("UI", "canvasPaint()", ds)
	var ps PAINTSTRUCT
	hdc, _, _ := pBeginPaint.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&ps)))
	if hdc == 0 {
		return
	}
	defer pEndPaint.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&ps)))

	var r RECT
	pGetClientRect.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&r)))
	bg := createSolidBrush(rgb(248, 248, 248))
	defer deleteGDIObject(bg)
	fillWithBrush(syscall.Handle(hdc), r, bg)

	if mapLoading.Load() {
		drawMapLoadingOverlay(syscall.Handle(hdc), r)
		return
	}

	if mode != "map" && mode != "permissions" && mode != "events" {
		text(syscall.Handle(hdc), 18, 48, viewCanvasMessage(mode), rgb(70, 70, 70))
		return
	}
	if currentMapDoc == nil {
		return
	}

	tile := mapDisplayTileSize()
	if tile <= 0 {
		return
	}
	startX := mapScrollX / tile
	startY := mapScrollY / tile
	offsetX := -(mapScrollX % tile)
	offsetY := int(gridOriginY) - (mapScrollY % tile)
	visibleCols := int(r.Right)/tile + 2
	visibleRows := int(r.Bottom-gridOriginY)/tile + 2

	pen, _, _ := pCreatePen.Call(PS_SOLID, 1, rgb(175, 180, 185))
	var old uintptr
	if pen != 0 {
		old, _, _ = pSelectObject.Call(hdc, pen)
	}
	if pen != 0 {
		defer func() {
			if old != 0 {
				pSelectObject.Call(hdc, old)
			}
			pDeleteObject.Call(pen)
		}()
	}

	// In Vista movimenti/Terrain disegniamo la mappa direttamente dalle stesse
	// celle usate dalla Vista mappa. In questo modo l'overlay non dipende dalla
	// superficie precomposta e la grafica della mappa resta sempre leggibile.
	if mode == "permissions" {
		cell := make([]uint32, 32*32)
		for yy := 0; yy < visibleRows; yy++ {
			my := startY + yy
			if my < 0 || my >= currentMapH {
				continue
			}
			for xx := 0; xx < visibleCols; xx++ {
				mx := startX + xx
				if mx < 0 || mx >= currentMapW {
					continue
				}
				dx := int32(offsetX + xx*tile)
				dy := int32(offsetY + yy*tile)
				composeMapCell(mx, my, cell)
				tintPermissionCell(cell, permissionOverlayColor(mx, my), 72)
				stretchTileBuffer(syscall.Handle(hdc), cell, dx, dy, int32(tile))
			}
		}
	} else {
		// Le altre viste continuano a usare la superficie composita pronta in RAM.
		drawMapSurfaceViewport(syscall.Handle(hdc), int(r.Right-r.Left), int(r.Bottom-gridOriginY))
		if mode == "events" {
			// Sovrapponi la grafica reale degli eventi dalla cartella Graphics/Characters.
			// Il frame viene letto dal graphic della pagina evento senza alterare il PNG.
			drawEventGraphicsOverlay(syscall.Handle(hdc), startX, startY, visibleCols, visibleRows, offsetX, offsetY, tile)
		}
	}

	// Griglia: viene tracciata ESCLUSIVAMENTE sopra il rettangolo fisico della
	// mappa. L'area di lavoro libera resta pulita anche se il canvas è molto
	// più grande della mappa (comportamento RPG Maker/Advance Map).
	if gridEnabled && pen != 0 {
		mapLeft := int32(-mapScrollX)
		mapTop := gridOriginY - int32(mapScrollY)
		mapRight := mapLeft + int32(currentMapW*tile)
		mapBottom := mapTop + int32(currentMapH*tile)
		clipLeft := mapLeft
		if clipLeft < 0 {
			clipLeft = 0
		}
		clipTop := mapTop
		if clipTop < gridOriginY {
			clipTop = gridOriginY
		}
		clipRight := mapRight
		if clipRight > r.Right {
			clipRight = r.Right
		}
		clipBottom := mapBottom
		if clipBottom > r.Bottom {
			clipBottom = r.Bottom
		}

		if clipRight > clipLeft && clipBottom > clipTop {
			for xx := 0; xx <= visibleCols; xx++ {
				x := int32(offsetX + xx*tile)
				if x >= clipLeft && x <= clipRight {
					pMoveToEx.Call(hdc, uintptr(x), uintptr(clipTop), 0)
					pLineTo.Call(hdc, uintptr(x), uintptr(clipBottom))
				}
			}
			for yy := 0; yy <= visibleRows; yy++ {
				y := int32(offsetY + yy*tile)
				if y >= clipTop && y <= clipBottom {
					pMoveToEx.Call(hdc, uintptr(clipLeft), uintptr(y), 0)
					pLineTo.Call(hdc, uintptr(clipRight), uintptr(y))
				}
			}
		}
	}

	if mode == "permissions" || mode == "events" {
		for yy := 0; yy < visibleRows; yy++ {
			my := startY + yy
			if my < 0 || my >= currentMapH {
				continue
			}
			for xx := 0; xx < visibleCols; xx++ {
				mx := startX + xx
				if mx < 0 || mx >= currentMapW {
					continue
				}
				dx := int32(offsetX + xx*tile)
				dy := int32(offsetY + yy*tile)
				if mode == "permissions" {
					v := permissionOverlayValue(mx, my)
					text(syscall.Handle(hdc), dx+2, dy+2, v, permissionOverlayTextColor(mx, my))
				} else if ei := eventAt(mx, my); ei >= 0 {
					e := events[ei]
					if drawEventTypeMarker(syscall.Handle(hdc), dx, dy, tile, e.EventKind) {
						continue
					}
					state := eventGraphicState(e)
					if state == 0 {
						text(syscall.Handle(hdc), dx+2, dy+2, "E", rgb(180, 80, 0))
					} else if state == 1 {
						text(syscall.Handle(hdc), dx+2, dy+2, "!", rgb(210, 45, 45))
					}
				}
			}
		}
	}

	// In Vista eventi il punto di ingresso del giocatore è un dato di progetto
	// distinto dagli eventi della mappa. Lo mostriamo come marker P sulla vera
	// mappa iniziale; può essere modificato da Strumenti -> Punto iniziale.
	if mode == "events" && currentMap != nil && currentMap.ID == gameStart.MapID {
		sx, sy := gameStart.X, gameStart.Y
		if sx >= startX && sx < startX+visibleCols && sy >= startY && sy < startY+visibleRows && sx >= 0 && sy >= 0 && sx < currentMapW && sy < currentMapH {
			dx := int32(offsetX + (sx-startX)*tile)
			dy := int32(offsetY + (sy-startY)*tile)
			text(syscall.Handle(hdc), dx+2, dy+2, "P", rgb(20, 90, 220))
		}
	}

	if mode == "map" && selectionX >= 0 && selectionY >= 0 {
		dx := int32(selectionX*tile - mapScrollX)
		dy := gridOriginY + int32(selectionY*tile-mapScrollY)
		if dx+int32(tile) >= 0 && dy+int32(tile) >= gridOriginY && dx < r.Right && dy < r.Bottom {
			selPen, _, _ := pCreatePen.Call(PS_SOLID, 2, rgb(25, 90, 220))
			if selPen != 0 {
				prev, _, _ := pSelectObject.Call(hdc, selPen)
				drawOutlineRect(hdc, dx, dy, dx+int32(tile), dy+int32(tile))
				if prev != 0 {
					pSelectObject.Call(hdc, prev)
				}
				pDeleteObject.Call(selPen)
			}
		}
	}

	drawPuzzlePathCaptureOverlay(syscall.Handle(hdc))

	if rectDragging && mode == "map" && mouseMapX >= 0 && mouseMapY >= 0 {
		x1, x2 := rectStartX, mouseMapX
		y1, y2 := rectStartY, mouseMapY
		if x1 > x2 {
			x1, x2 = x2, x1
		}
		if y1 > y2 {
			y1, y2 = y2, y1
		}
		dx := int32(x1*tile - mapScrollX)
		dy := gridOriginY + int32(y1*tile-mapScrollY)
		dw := int32((x2 - x1 + 1) * tile)
		dh := int32((y2 - y1 + 1) * tile)
		drawOutlineRect(hdc, dx, dy, dx+dw, dy+dh)
	}
}

func canvasClickLegacy(x, y int, right bool) {
	if mode != "permissions" {
		return
	}
	if right {
		samplePermissionAt(x, y)
	} else {
		applyPermissionAt(x, y)
		savePermissions()
	}
	invalidate(hwndCanvas)
}

func mapToolDown(x, y int) {
	switch activeMapTool {
	case ToolSelect:
		selectionX, selectionY = x, y
		sampleVisibleTileAt(x, y)
	case ToolPencil:
		beginMapOperation("Matita")
		stampSelectedTileBrush(x, y)
	case ToolRectangle:
		rectStartX, rectStartY = x, y
		rectDragging = true
	case ToolFill:
		floodFillMap(x, y, selectedTileID)
	case ToolEyedropper:
		sampleTileAt(x, y)
	case ToolEraser:
		beginMapOperation("Gomma")
		appendTileChange(activeLayer, x, y, 0)
	}
	invalidate(hwndCanvas)
}

func mapToolMove(x, y int) {
	mouseMapX, mouseMapY = x, y
	updateMapEditorStatus()
	if !mapMouseDown {
		return
	}
	switch activeMapTool {
	case ToolPencil:
		stampSelectedTileBrush(x, y)
	case ToolEraser:
		appendTileChange(activeLayer, x, y, 0)
	case ToolRectangle:
		invalidate(hwndCanvas)
	}
}

func mapToolUp(x, y int) {
	switch activeMapTool {
	case ToolPencil, ToolEraser:
		commitMapOperation()
	case ToolRectangle:
		if rectDragging {
			rectDragging = false
			fillRectangleMap(rectStartX, rectStartY, x, y, selectedTileID)
		}
	}
}

func canvasWndProc(hwnd syscall.Handle, msg uint32, w, l uintptr) uintptr {
	done := diagEnterUI(fmt.Sprintf("canvasWndProc msg=%#x", msg))
	defer done()
	switch msg {
	case WM_PAINT:
		dn, ds := diagWMPaintStart("canvas")
		canvasPaint(hwnd)
		diagWMPaintEnd(dn, "canvas", ds)
		return 0
	case WM_SIZE:
		dn, ds := diagWMSizeStart()
		updateCanvasScrollbars(hwnd)
		diagWMSizeEnd(dn, ds)
		return 0
	case WM_HSCROLL:
		handleCanvasScroll(hwnd, SB_HORZ, w)
		return 0
	case WM_VSCROLL:
		handleCanvasScroll(hwnd, SB_VERT, w)
		return 0
	case WM_LBUTTONDBLCLK:
		xpx := int32(int16(loword(l)))
		yp := int32(int16(hiword(l)))
		if x, y, ok := canvasMapCoord(xpx, yp); ok && puzzlePathCaptureActive {
			puzzlePathCanvasClick(x, y, false)
			return 0
		}
		xpx = int32(int16(loword(l)))
		ypx := int32(int16(hiword(l)))
		x, y, ok := canvasMapCoord(xpx, ypx)
		if !ok || mode != "events" {
			return 0
		}
		eventLeftMenuPending = false
		idx := eventAt(x, y)
		if idx >= 0 && idx < len(events) {
			selectedEvent = idx
			showEvent()
			invalidate(hwndCanvas)
			showEditEventEditorDialog(idx)
		} else {
			selectedEvent = -1
			showEvent()
			invalidate(hwndCanvas)
			showCreateEventEditorDialogAt(x, y)
		}
		return 0
	case WM_LBUTTONDOWN:
		xpx := int32(int16(loword(l)))
		ypx := int32(int16(hiword(l)))
		x, y, ok := canvasMapCoord(xpx, ypx)
		if !ok {
			return 0
		}
		if puzzlePathCaptureActive {
			puzzlePathCanvasClick(x, y, false)
			return 0
		}
		if mode == "map" {
			mapMouseDown = true
			pSetCapture.Call(uintptr(hwnd))
			mapToolDown(x, y)
		} else if mode == "permissions" {
			mapMouseDown = true
			pSetCapture.Call(uintptr(hwnd))
			applyPermissionAt(x, y)
			invalidate(hwndCanvas)
		} else if mode == "events" {
			eventCanvasLeftDown(x, y)
		} else {
			canvasClickLegacy(x, y, false)
		}
		return 0
	case WM_MOUSEMOVE:
		xpx := int32(int16(loword(l)))
		ypx := int32(int16(hiword(l)))
		if x, y, ok := canvasMapCoord(xpx, ypx); ok {
			mouseMapX, mouseMapY = x, y
			if mode == "events" && eventRightDragActive {
				updateEventRightDrag(x, y)
			} else if mode == "map" {
				mapToolMove(x, y)
			} else if mode == "permissions" && mapMouseDown {
				applyPermissionAt(x, y)
				invalidate(hwndCanvas)
			}
		} else {
			mouseMapX, mouseMapY = -1, -1
		}
		return 0
	case WM_LBUTTONUP:
		if mode == "map" && mapMouseDown {
			mapMouseDown = false
			pReleaseCapture.Call()
			xpx := int32(int16(loword(l)))
			ypx := int32(int16(hiword(l)))
			if x, y, ok := canvasMapCoord(xpx, ypx); ok {
				mapToolUp(x, y)
			} else {
				commitMapOperation()
				rectDragging = false
			}
			invalidate(hwnd)
		} else if mode == "permissions" && mapMouseDown {
			mapMouseDown = false
			pReleaseCapture.Call()
			savePermissions()
			invalidate(hwnd)
		} else if mode == "events" {
			eventCanvasLeftUp()
		}
		return 0
	case WM_RBUTTONDOWN:
		xpx := int32(int16(loword(l)))
		ypx := int32(int16(hiword(l)))
		if x, y, ok := canvasMapCoord(xpx, ypx); ok {
			if puzzlePathCaptureActive {
				puzzlePathCanvasClick(x, y, true)
				return 0
			}
			if mode == "map" {
				sampleVisibleTileAt(x, y)
				invalidate(hwnd)
			} else if mode == "events" {
				eventLeftMenuPending = false
				if beginEventRightDrag(x, y) {
					pSetCapture.Call(uintptr(hwnd))
				}
			} else {
				canvasClickLegacy(x, y, true)
			}
		}
		return 0
	case WM_RBUTTONUP:
		if mode == "events" && eventRightDragActive {
			finishEventRightDrag()
		}
		return 0
	case WM_CAPTURECHANGED:
		if eventRightDragActive {
			cancelEventRightDrag()
		}
		return 0
	case WM_KEYDOWN:
		if puzzlePathCanvasKey(w) {
			return 0
		}
	}
	r, _, _ := pDefWindowProcW.Call(uintptr(hwnd), uintptr(msg), w, l)
	return r
}
