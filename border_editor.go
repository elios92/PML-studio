//go:build windows

package main

import (
	"fmt"
	"syscall"
	"unsafe"
)

const (
	paletteCellSize  = 32
	paletteFixedCols = 8 // RPG Maker XP: il tileset normale e' largo 8 tile.
)

var (
	tilePaletteScroll             int
	autotilePaletteScroll         int
	tilePaletteWheelAccum         int
	autotilePaletteWheelAccum     int
	paletteScrollUpdateInProgress bool

	// Selezione rettangolare della palette con trascinamento.
	// Il tasto sinistro e' quello principale; il destro resta compatibile.
	// Gli indici sono relativi alla palette corrente (Tiles/Autotile).
	paletteSelectionKind       string
	paletteSelectionStartIndex = -1
	paletteSelectionEndIndex   = -1
	paletteRangeSelecting      bool
	paletteRangeSelectHWND     syscall.Handle

	// Blocco bordi direzionale: Alto, Destra, Basso, Sinistra. Ogni direzione
	// contiene un oggetto 2x2 completo (4 tile) in ordine row-major.
	borderDirectionTiles    = [4][4]int{}
	borderDirectionDefined  = [4]bool{}
	selectedBorderDirection = -1
	selectedBorderSlot      = -1
)

func paletteKind(hwnd syscall.Handle) string {
	switch hwnd {
	case hwndPaletteTilesetList:
		return "tiles"
	case hwndPaletteAutotileList:
		return "autotiles"
	case hwndPaletteBorderArea:
		return "border"
	case hwndPermissionsPalette:
		return "permissions"
	}
	return ""
}

func paletteCellSizeFor(hwnd syscall.Handle, count int) int {
	if paletteKind(hwnd) != "autotiles" {
		return paletteCellSize
	}
	// Gli autotile sono una striscia unica. Riduciamo automaticamente lo zoom
	// delle miniature per far entrare tutti gli slot reali in una sola riga.
	var r RECT
	pGetClientRect.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&r)))
	if count <= 0 {
		return 24
	}
	cell := int(r.Right) / count
	if cell > paletteCellSize {
		cell = paletteCellSize
	}
	if cell < 6 {
		cell = 6
	}
	return cell
}

func paletteColumnsFor(kind string, count int) int {
	if kind == "autotiles" {
		if count < 1 {
			return 1
		}
		return count // una sola riga, un'anteprima per ogni autotile
	}
	return paletteFixedCols
}

func paletteMetrics(hwnd syscall.Handle, count int) (cols, rowsVisible, totalRows int) {
	var r RECT
	pGetClientRect.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&r)))
	kind := paletteKind(hwnd)
	if kind == "tiles" {
		cols = paletteFixedCols
	} else if kind == "autotiles" {
		cols = paletteColumnsFor(kind, count)
	} else {
		cols = int(r.Right) / paletteCellSize
		if cols < 1 {
			cols = 1
		}
	}
	cell := paletteCellSizeFor(hwnd, count)
	rowsVisible = int(r.Bottom) / cell
	if rowsVisible < 1 {
		rowsVisible = 1
	}
	totalRows = (count + cols - 1) / cols
	return
}

func paletteCount(kind string) int {
	switch kind {
	case "tiles":
		return normalTileCount()
	case "autotiles":
		if currentTileset == nil {
			return 0
		}
		// La UI espone gli slot autotile, non le 48 varianti interne di ogni
		// slot. Le varianti restano un dettaglio del formato/runtime.
		return len(currentTileset.Autotiles)
	case "border":
		return 4
	}
	return 0
}

func paletteTileID(kind string, index int) int {
	switch kind {
	case "tiles":
		return 384 + index
	case "autotiles":
		return 48 + index*48
	}
	return 0
}

func paletteIndexAt(hwnd syscall.Handle, l uintptr) (int, bool) {
	kind := paletteKind(hwnd)
	if kind != "tiles" && kind != "autotiles" {
		return -1, false
	}
	count := paletteCount(kind)
	if count <= 0 {
		return -1, false
	}
	x := int(int16(loword(l)))
	y := int(int16(hiword(l)))
	if x < 0 || y < 0 {
		return -1, false
	}
	cols, _, _ := paletteMetrics(hwnd, count)
	cell := paletteCellSizeFor(hwnd, count)
	col := x / cell
	row := y / cell
	if col < 0 || col >= cols {
		return -1, false
	}
	scrollRow := tilePaletteScroll
	if kind == "autotiles" {
		scrollRow = autotilePaletteScroll
	}
	idx := (row+scrollRow)*cols + col
	if idx < 0 || idx >= count {
		return -1, false
	}
	return idx, true
}

func setPaletteSingleSelection(tileID int) {
	paletteSelectionStartIndex = -1
	paletteSelectionEndIndex = -1
	paletteSelectionKind = ""
	if tileID >= 384 {
		idx := tileID - 384
		if idx >= 0 && idx < paletteCount("tiles") {
			paletteSelectionKind = "tiles"
			paletteSelectionStartIndex = idx
			paletteSelectionEndIndex = idx
		}
		return
	}
	if tileID >= 48 {
		idx := (tileID - 48) / 48
		if idx >= 0 && idx < paletteCount("autotiles") {
			paletteSelectionKind = "autotiles"
			paletteSelectionStartIndex = idx
			paletteSelectionEndIndex = idx
		}
	}
}

func paletteSelectionBounds(kind string) (minCol, minRow, maxCol, maxRow int, ok bool) {
	if kind == "" || kind != paletteSelectionKind || paletteSelectionStartIndex < 0 || paletteSelectionEndIndex < 0 {
		return 0, 0, 0, 0, false
	}
	cols := paletteColumnsFor(kind, paletteCount(kind))
	sc, sr := paletteSelectionStartIndex%cols, paletteSelectionStartIndex/cols
	ec, er := paletteSelectionEndIndex%cols, paletteSelectionEndIndex/cols
	if sc > ec {
		sc, ec = ec, sc
	}
	if sr > er {
		sr, er = er, sr
	}
	return sc, sr, ec, er, true
}

func paletteIndexSelected(kind string, idx int) bool {
	minCol, minRow, maxCol, maxRow, ok := paletteSelectionBounds(kind)
	if !ok {
		return false
	}
	cols := paletteColumnsFor(kind, paletteCount(kind))
	col, row := idx%cols, idx/cols
	return col >= minCol && col <= maxCol && row >= minRow && row <= maxRow
}

func applyPaletteRangeSelection(kind string, startIdx, endIdx int) {
	count := paletteCount(kind)
	if count <= 0 || startIdx < 0 || endIdx < 0 || startIdx >= count || endIdx >= count {
		return
	}
	paletteSelectionKind = kind
	paletteSelectionStartIndex = startIdx
	paletteSelectionEndIndex = endIdx
	minCol, minRow, maxCol, maxRow, _ := paletteSelectionBounds(kind)
	w := maxCol - minCol + 1
	h := maxRow - minRow + 1
	ids := make([]int, w*h)
	for by := 0; by < h; by++ {
		for bx := 0; bx < w; bx++ {
			cols := paletteColumnsFor(kind, count)
			idx := (minRow+by)*cols + (minCol + bx)
			if idx >= 0 && idx < count {
				ids[by*w+bx] = paletteTileID(kind, idx)
			}
		}
	}
	if len(ids) > 0 && ids[0] > 0 {
		setSelectedTileBrush(ids, w, h)
		activeMapTool = ToolPencil
		updateMapToolButtonStates()
	}
	updatePaletteSelection()
	setText(hwndStatus, fmt.Sprintf("Selezione %s: %dx%d tile | ID iniziale %d", kind, w, h, selectedTileID))
}

func ensureSelectedTileVisible() {
	var hwnd syscall.Handle
	kind := ""
	idx := -1
	if selectedTileID >= 384 {
		hwnd, kind, idx = hwndPaletteTilesetList, "tiles", selectedTileID-384
	} else if selectedTileID >= 48 {
		hwnd, kind, idx = hwndPaletteAutotileList, "autotiles", (selectedTileID-48)/48
	}
	if hwnd == 0 || idx < 0 || idx >= paletteCount(kind) {
		return
	}
	_, rowsVisible, totalRows := paletteMetrics(hwnd, paletteCount(kind))
	cols, _, _ := paletteMetrics(hwnd, paletteCount(kind))
	row := idx / cols
	pos := &tilePaletteScroll
	if kind == "autotiles" {
		pos = &autotilePaletteScroll
	}
	if row < *pos {
		*pos = row
	} else if row >= *pos+rowsVisible {
		*pos = row - rowsVisible + 1
	}
	maxPos := totalRows - rowsVisible
	if maxPos < 0 {
		maxPos = 0
	}
	if *pos < 0 {
		*pos = 0
	}
	if *pos > maxPos {
		*pos = maxPos
	}
	updatePaletteScroll(hwnd)
	invalidate(hwnd)
}

func beginPaletteRangeSelection(hwnd syscall.Handle, l uintptr) bool {
	kind := paletteKind(hwnd)
	idx, ok := paletteIndexAt(hwnd, l)
	if !ok || (kind != "tiles" && kind != "autotiles") {
		return false
	}
	paletteRangeSelecting = true
	paletteRangeSelectHWND = hwnd
	paletteSelectionKind = kind
	paletteSelectionStartIndex = idx
	paletteSelectionEndIndex = idx
	applyPaletteRangeSelection(kind, idx, idx)
	pSetCapture.Call(uintptr(hwnd))
	invalidate(hwnd)
	return true
}

func updatePaletteRangeSelection(hwnd syscall.Handle, l uintptr) bool {
	if !paletteRangeSelecting || paletteRangeSelectHWND != hwnd {
		return false
	}
	idx, ok := paletteIndexAt(hwnd, l)
	if !ok {
		return true
	}
	if idx != paletteSelectionEndIndex {
		paletteSelectionEndIndex = idx
		applyPaletteRangeSelection(paletteSelectionKind, paletteSelectionStartIndex, paletteSelectionEndIndex)
		invalidate(hwnd)
	}
	return true
}

func endPaletteRangeSelection(hwnd syscall.Handle, l uintptr) bool {
	if !paletteRangeSelecting || paletteRangeSelectHWND != hwnd {
		return false
	}
	if idx, ok := paletteIndexAt(hwnd, l); ok {
		paletteSelectionEndIndex = idx
	}
	applyPaletteRangeSelection(paletteSelectionKind, paletteSelectionStartIndex, paletteSelectionEndIndex)
	paletteRangeSelecting = false
	paletteRangeSelectHWND = 0
	pReleaseCapture.Call()
	invalidate(hwnd)
	return true
}

func updatePaletteScroll(hwnd syscall.Handle) {
	if hwnd == 0 || paletteScrollUpdateInProgress {
		return
	}
	kind := paletteKind(hwnd)
	if kind == "" || kind == "border" {
		return
	}
	if kind == "permissions" {
		updatePermissionPaletteScroll(hwnd)
		return
	}

	paletteScrollUpdateInProgress = true
	defer func() { paletteScrollUpdateInProgress = false }()

	count := paletteCount(kind)
	_, rowsVisible, totalRows := paletteMetrics(hwnd, count)
	pos := tilePaletteScroll
	if kind == "autotiles" {
		pos = autotilePaletteScroll
	}

	maxPos := totalRows - rowsVisible
	if maxPos < 0 {
		maxPos = 0
	}
	if pos < 0 {
		pos = 0
	}
	if pos > maxPos {
		pos = maxPos
	}

	var current SCROLLINFO
	current.CbSize = uint32(unsafe.Sizeof(SCROLLINFO{}))
	current.FMask = SIF_RANGE | SIF_PAGE | SIF_POS
	pGetScrollInfo.Call(uintptr(hwnd), SB_VERT, uintptr(unsafe.Pointer(&current)))

	wantMin := int32(0)
	wantMax := int32(maxInt(totalRows-1, 0))
	wantPage := uint32(rowsVisible)
	wantPos := int32(pos)

	if current.NMin != wantMin || current.NMax != wantMax || current.NPage != wantPage || current.NPos != wantPos {
		si := SCROLLINFO{
			CbSize: uint32(unsafe.Sizeof(SCROLLINFO{})),
			FMask:  SIF_RANGE | SIF_PAGE | SIF_POS,
			NMin:   wantMin,
			NMax:   wantMax,
			NPage:  wantPage,
			NPos:   wantPos,
		}
		// redraw=0 is intentional. SetScrollInfo may change the non-client
		// area and synchronously generate WM_SIZE. Redrawing from inside
		// WM_SIZE can re-enter paletteWndProc indefinitely.
		pSetScrollInfo.Call(uintptr(hwnd), SB_VERT, uintptr(unsafe.Pointer(&si)), 0)
	}

	if kind == "tiles" {
		tilePaletteScroll = pos
	} else {
		autotilePaletteScroll = pos
	}
}

const (
	borderDirectionTop = iota
	borderDirectionRight
	borderDirectionBottom
	borderDirectionLeft
	borderDirectionCount
	borderGridSize  = 2
	borderGridCells = borderGridSize * borderGridSize
)

var borderDirectionKeys = [borderDirectionCount]string{"top", "right", "bottom", "left"}
var borderDirectionLabels = [borderDirectionCount]string{"ALTO", "DESTRA", "BASSO", "SINISTRA"}

func resetBorderBlock() {
	borderDirectionTiles = [4][4]int{}
	borderDirectionDefined = [4]bool{}
	selectedBorderDirection = -1
	selectedBorderSlot = -1
}

func loadBorderBlock(doc PermissionDoc) {
	resetBorderBlock()

	// PML Studio usa esclusivamente il formato direzionale 2x2: quattro tile
	// native da 32x32 per ogni direzione, quindi un blocco 64x64. Nessun
	// fallback 4x4: i dati di un formato diverso restano volutamente neri.
	if doc.BorderSchema != "directional_2x2_v1" || doc.BorderDirectionWidth != 2 || doc.BorderDirectionHeight != 2 {
		return
	}
	for dir, key := range borderDirectionKeys {
		if vals, ok := doc.BorderDirections[key]; ok && len(vals) >= borderGridCells {
			for i := 0; i < borderGridCells; i++ {
				borderDirectionTiles[dir][i] = vals[i]
			}
			borderDirectionDefined[dir] = true
			continue
		}
		if block, ok := doc.BorderDirectionBlocks[key]; ok && len(block) >= borderGridSize {
			valid := true
			for row := 0; row < borderGridSize; row++ {
				if len(block[row]) < borderGridSize {
					valid = false
					break
				}
			}
			if valid {
				for row := 0; row < borderGridSize; row++ {
					for col := 0; col < borderGridSize; col++ {
						borderDirectionTiles[dir][row*borderGridSize+col] = block[row][col]
					}
				}
				borderDirectionDefined[dir] = true
			}
		}
	}
}

func hasDirectionalBorders() bool {
	for _, defined := range borderDirectionDefined {
		if defined {
			return true
		}
	}
	return false
}

func directionalBorderDocValues() (flat map[string][]int, blocks map[string][][]int) {
	if !hasDirectionalBorders() {
		return nil, nil
	}
	flat = map[string][]int{}
	blocks = map[string][][]int{}
	for dir, key := range borderDirectionKeys {
		if !borderDirectionDefined[dir] {
			continue
		}
		vals := make([]int, borderGridCells)
		copy(vals, borderDirectionTiles[dir][:])
		flat[key] = vals
		rows := make([][]int, borderGridSize)
		for row := 0; row < borderGridSize; row++ {
			rows[row] = append([]int(nil), vals[row*borderGridSize:(row+1)*borderGridSize]...)
		}
		blocks[key] = rows
	}
	return flat, blocks
}

func borderDirectionGridGeometry(hwnd syscall.Handle, dir int) (left, top, cell int32, ok bool) {
	if dir < 0 || dir >= borderDirectionCount {
		return 0, 0, 0, false
	}
	var r RECT
	pGetClientRect.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&r)))
	w, h := r.Right-r.Left, r.Bottom-r.Top
	const (
		gap    int32 = 8
		labelH int32 = 14
		// Ogni cella corrisponde a una tile nativa RMXP/PLM da 32x32 px.
		// Ogni direzione e' un blocco 2x2: esattamente 64x64 px, come un
		// oggetto composto da quattro tile. La misura non viene ridimensionata.
		cellPx int32 = 32
	)
	cell = cellPx
	gridW := cell * borderGridSize
	blockH := labelH + cell*borderGridSize
	totalW := gridW*2 + gap
	totalH := blockH*2 + gap
	originX := (w - totalW) / 2
	originY := (h - totalH) / 2
	if originX < 2 {
		originX = 2
	}
	if originY < 2 {
		originY = 2
	}
	col := int32(dir % 2)
	row := int32(dir / 2)
	left = originX + col*(gridW+gap)
	top = originY + row*(blockH+gap) + labelH
	return left, top, cell, true
}

func drawDirectionalBorderGrid(hwnd syscall.Handle, hdc syscall.Handle, dir int) {
	left, top, size, ok := borderDirectionGridGeometry(hwnd, dir)
	if !ok {
		return
	}
	labelY := top - 13
	text(hdc, left, labelY, borderDirectionLabels[dir], rgb(45, 55, 65))
	for i := 0; i < borderGridCells; i++ {
		x := left + int32(i%borderGridSize)*size
		y := top + int32(i/borderGridSize)*size
		tileID := 0
		if borderDirectionDefined[dir] {
			tileID = borderDirectionTiles[dir][i]
		}
		if tileID > 0 {
			drawSingleTile(hdc, tileID, x, y, size)
		} else {
			// Nessun fallback legacy: una cella 2x2 non configurata è nera.
			br := createSolidBrush(rgb(0, 0, 0))
			fillWithBrush(hdc, RECT{Left: x, Top: y, Right: x + size, Bottom: y + size}, br)
			deleteGDIObject(br)
		}
	}

	pen, _, _ := pCreatePen.Call(PS_SOLID, 1, rgb(105, 115, 125))
	old, _, _ := pSelectObject.Call(uintptr(hdc), pen)
	gridPx := size * borderGridSize
	drawOutlineRect(uintptr(hdc), left, top, left+gridPx, top+gridPx)
	for i := int32(1); i < borderGridSize; i++ {
		pMoveToEx.Call(uintptr(hdc), uintptr(left+i*size), uintptr(top), 0)
		pLineTo.Call(uintptr(hdc), uintptr(left+i*size), uintptr(top+gridPx))
		pMoveToEx.Call(uintptr(hdc), uintptr(left), uintptr(top+i*size), 0)
		pLineTo.Call(uintptr(hdc), uintptr(left+gridPx), uintptr(top+i*size))
	}
	if old != 0 {
		pSelectObject.Call(uintptr(hdc), old)
	}
	if pen != 0 {
		pDeleteObject.Call(pen)
	}

	if selectedBorderDirection == dir && selectedBorderSlot >= 0 && selectedBorderSlot < borderGridCells {
		i := selectedBorderSlot
		x := left + int32(i%borderGridSize)*size
		y := top + int32(i/borderGridSize)*size
		selPen, _, _ := pCreatePen.Call(PS_SOLID, 2, rgb(25, 105, 205))
		oldSel, _, _ := pSelectObject.Call(uintptr(hdc), selPen)
		drawOutlineRect(uintptr(hdc), x+1, y+1, x+size-1, y+size-1)
		if oldSel != 0 {
			pSelectObject.Call(uintptr(hdc), oldSel)
		}
		if selPen != 0 {
			pDeleteObject.Call(selPen)
		}
	}
}

func borderPalettePaint(hwnd syscall.Handle, hdc syscall.Handle, r RECT) {
	for dir := 0; dir < borderDirectionCount; dir++ {
		drawDirectionalBorderGrid(hwnd, hdc, dir)
	}
}

func borderDirectionName(dir int) string {
	if dir >= 0 && dir < borderDirectionCount {
		return borderDirectionLabels[dir]
	}
	return "?"
}

// borderTileLooksEmpty treats only genuinely blank tiles as empty. This avoids
// creating a visible white square when a border object has unused cells.
func borderTileLooksEmpty(tileID int) bool {
	if tileID <= 0 {
		return true
	}
	if currentTileset == nil || currentTileset.Tileset == nil || tileID < 384 {
		return false
	}
	s := currentTileset.Tileset
	idx := tileID - 384
	const cols = 8
	sx := (idx % cols) * 32
	sy := (idx / cols) * 32
	if sx < 0 || sy < 0 || sx+32 > s.Width || sy+32 > s.Height {
		return true
	}
	visible := 0
	nonBlank := 0
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			c, a, ok := surfacePixel(s, sx+x, sy+y)
			if !ok || a == 0 {
				continue
			}
			visible++
			r := (c >> 16) & 0xff
			g := (c >> 8) & 0xff
			b := c & 0xff
			if r < 248 || g < 248 || b < 248 {
				nonBlank++
			}
		}
	}
	if visible == 0 {
		return true
	}
	return nonBlank == 0
}

func selectedBrushAsBorder2x2() ([4]int, bool) {
	var out [4]int
	if selectedTileBrushW != borderGridSize || selectedTileBrushH != borderGridSize || len(selectedTileBrush) < borderGridCells {
		return out, false
	}
	for i := 0; i < borderGridCells; i++ {
		id := selectedTileBrush[i]
		if id <= 0 || borderTileLooksEmpty(id) {
			out[i] = 0
		} else {
			out[i] = id
		}
	}
	return out, true
}

func borderPaletteHitTest(hwnd syscall.Handle, l uintptr) (dir, slot int, ok bool) {
	x := int32(int16(loword(l)))
	y := int32(int16(hiword(l)))
	for d := 0; d < borderDirectionCount; d++ {
		left, top, size, valid := borderDirectionGridGeometry(hwnd, d)
		if !valid {
			continue
		}
		gridPx := size * borderGridSize
		if x < left || y < top || x >= left+gridPx || y >= top+gridPx {
			continue
		}
		col := int((x - left) / size)
		row := int((y - top) / size)
		return d, row*borderGridSize + col, true
	}
	return -1, -1, false
}

func borderPaletteClick(hwnd syscall.Handle, l uintptr) {
	if currentMap == nil || currentTileset == nil {
		return
	}
	dir, slot, ok := borderPaletteHitTest(hwnd, l)
	if !ok {
		return
	}

	// Due modalita' distinte e prevedibili:
	//   - brush 2x2: copia l'oggetto completo (4 tile) nel blocco direzionale;
	//   - tile singola: modifica SOLO la cella cliccata, permettendo di
	//     comporre liberamente 4 tile uguali oppure 4 tile diverse.
	// Non viene mai ricostruito automaticamente un 2x2 da una tile singola.
	if brush, ok := selectedBrushAsBorder2x2(); ok {
		borderDirectionTiles[dir] = brush
		borderDirectionDefined[dir] = true
	} else {
		if !borderDirectionDefined[dir] {
			borderDirectionTiles[dir] = [4]int{}
			borderDirectionDefined[dir] = true
		}
		if borderTileLooksEmpty(selectedTileID) {
			borderDirectionTiles[dir][slot] = 0
		} else {
			borderDirectionTiles[dir][slot] = selectedTileID
		}
	}

	selectedBorderDirection = dir
	selectedBorderSlot = slot
	permissionDataDirty = true
	savePermissions()
	invalidate(hwndPaletteBorderArea)
	if selectedTileBrushW == borderGridSize && selectedTileBrushH == borderGridSize {
		setText(hwndStatus, fmt.Sprintf("Bordo %s: oggetto 2x2 applicato | 4 tile", borderDirectionName(dir)))
	} else {
		setText(hwndStatus, fmt.Sprintf("Bordo %s: cella %d/4 aggiornata | Tile %d", borderDirectionName(dir), slot+1, selectedTileID))
	}
}

func palettePaint(hwnd syscall.Handle) {
	if paletteKind(hwnd) == "permissions" {
		permissionPalettePaint(hwnd)
		return
	}
	var ps PAINTSTRUCT
	hdc, _, _ := pBeginPaint.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&ps)))
	if hdc == 0 {
		return
	}
	defer pEndPaint.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&ps)))
	var r RECT
	pGetClientRect.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&r)))
	br := createSolidBrush(rgb(245, 245, 245))
	defer deleteGDIObject(br)
	fillWithBrush(syscall.Handle(hdc), r, br)

	kind := paletteKind(hwnd)
	if kind == "border" {
		borderPalettePaint(hwnd, syscall.Handle(hdc), r)
		return
	}
	count := paletteCount(kind)
	if count == 0 {
		text(syscall.Handle(hdc), 5, 5, "Nessun asset", rgb(90, 90, 90))
		return
	}
	cols, rowsVisible, _ := paletteMetrics(hwnd, count)
	scrollRow := 0
	if kind == "tiles" {
		scrollRow = tilePaletteScroll
	}
	if kind == "autotiles" {
		scrollRow = autotilePaletteScroll
	}
	start := scrollRow * cols
	end := start + (rowsVisible+1)*cols
	if end > count {
		end = count
	}

	pen, _, _ := pCreatePen.Call(PS_SOLID, 2, rgb(25, 105, 205))
	old, _, _ := pSelectObject.Call(hdc, pen)
	defer func() {
		if old != 0 {
			pSelectObject.Call(hdc, old)
		}
		if pen != 0 {
			pDeleteObject.Call(pen)
		}
	}()

	cellSize := paletteCellSizeFor(hwnd, count)
	for i := start; i < end; i++ {
		local := i - start
		cx := local % cols
		cy := local / cols
		x := int32(cx * cellSize)
		y := int32(cy * cellSize)
		id := paletteTileID(kind, i)
		drawSingleTile(syscall.Handle(hdc), id, x, y, int32(cellSize))
		if paletteIndexSelected(kind, i) {
			// Solo contorno: non usare Rectangle(), perche' il brush GDI
			// riempirebbe il tile di bianco nascondendo cio' che e' selezionato.
			drawOutlineRect(hdc, x, y, x+int32(cellSize), y+int32(cellSize))
		}
	}
}

func paletteClick(hwnd syscall.Handle, l uintptr) {
	kind := paletteKind(hwnd)
	if kind == "permissions" {
		permissionPaletteClick(hwnd, l)
		return
	}
	if kind == "border" {
		borderPaletteClick(hwnd, l)
		return
	}
	idx, ok := paletteIndexAt(hwnd, l)
	if !ok {
		return
	}
	id := paletteTileID(kind, idx)
	if kind == "autotiles" && selectedTileID >= 48 && selectedTileID < 384 && (selectedTileID-48)/48 == idx {
		id = selectedTileID // conserva l'ultima variante scelta per questo slot
	}
	setSingleSelectedTile(id)
	activeMapTool = ToolPencil
	updateMapToolButtonStates()
	paletteSelectionKind = kind
	paletteSelectionStartIndex = idx
	paletteSelectionEndIndex = idx
	updatePaletteSelection()
	if kind == "autotiles" {
		variant := (selectedTileID-48)%48 + 1
		setText(hwndStatus, fmt.Sprintf("Autotile %d selezionato | variante %d/48 | doppio click per scegliere la variante", idx+1, variant))
	} else {
		setText(hwndStatus, fmt.Sprintf("%s selezionato: tile ID %d", tileDebugName(selectedTileID), selectedTileID))
	}
}

func paletteDoubleClick(hwnd syscall.Handle, l uintptr) {
	if paletteKind(hwnd) != "autotiles" {
		return
	}
	idx, ok := paletteIndexAt(hwnd, l)
	if !ok {
		return
	}
	showAutotileVariantDialog(hwndMain, idx)
}

// paletteWheelScroll gestisce la rotella del mouse sulle palette Tiles e
// Autotile. Lo scroll e' espresso in righe della griglia della palette, non in
// pixel: in questo modo il comportamento resta stabile anche se il pannello
// destro viene ridimensionato.
func paletteWheelScroll(hwnd syscall.Handle, w uintptr) bool {
	kind := paletteKind(hwnd)
	if kind != "tiles" && kind != "autotiles" {
		return false
	}
	count := paletteCount(kind)
	if count <= 0 {
		return true
	}
	if kind == "autotiles" {
		// Una sola riga: non c'e' niente da scorrere verticalmente.
		autotilePaletteScroll = 0
		return true
	}

	// HIWORD(wParam) contiene un delta signed. Manteniamo il resto per mouse
	// ad alta risoluzione/touchpad che possono inviare valori inferiori a 120.
	delta := int(int16(hiword(w)))
	if delta == 0 {
		return true
	}
	accum := &tilePaletteWheelAccum
	pos := &tilePaletteScroll
	if kind == "autotiles" {
		accum = &autotilePaletteWheelAccum
		pos = &autotilePaletteScroll
	}
	*accum += delta

	const wheelDelta = 120
	const rowsPerNotch = 3
	changed := false
	for *accum >= wheelDelta {
		*pos -= rowsPerNotch
		*accum -= wheelDelta
		changed = true
	}
	for *accum <= -wheelDelta {
		*pos += rowsPerNotch
		*accum += wheelDelta
		changed = true
	}
	if !changed {
		return true
	}

	_, rowsVisible, totalRows := paletteMetrics(hwnd, count)
	maxPos := totalRows - rowsVisible
	if maxPos < 0 {
		maxPos = 0
	}
	if *pos < 0 {
		*pos = 0
	}
	if *pos > maxPos {
		*pos = maxPos
	}
	updatePaletteScroll(hwnd)
	invalidate(hwnd)
	return true
}

// routePaletteMouseWheel viene chiamata anche dalla finestra principale.
// WM_MOUSEWHEEL normalmente segue il focus della tastiera; instradandolo in
// base alle coordinate del puntatore la palette scorre sempre quando il mouse
// e' sopra Tiles/Autotile, senza richiedere prima un click sul pannello.
func routePaletteMouseWheel(w, l uintptr) bool {
	x := int32(int16(loword(l)))
	y := int32(int16(hiword(l)))
	for _, h := range []syscall.Handle{hwndPaletteTilesetList, hwndPaletteAutotileList} {
		if h == 0 {
			continue
		}
		var r RECT
		pGetWindowRect.Call(uintptr(h), uintptr(unsafe.Pointer(&r)))
		if x >= r.Left && x < r.Right && y >= r.Top && y < r.Bottom {
			return paletteWheelScroll(h, w)
		}
	}
	return false
}

func paletteScroll(hwnd syscall.Handle, w uintptr) {
	kind := paletteKind(hwnd)
	if kind == "permissions" {
		permissionPaletteScrollBy(hwnd, w)
		return
	}
	if kind == "border" {
		return
	}
	if kind == "autotiles" {
		autotilePaletteScroll = 0
		updatePaletteScroll(hwnd)
		invalidate(hwnd)
		return
	}
	count := paletteCount(kind)
	_, rowsVisible, totalRows := paletteMetrics(hwnd, count)
	pos := tilePaletteScroll
	if kind == "autotiles" {
		pos = autotilePaletteScroll
	}
	switch int(loword(w)) {
	case SB_LINEUP:
		pos--
	case SB_LINEDOWN:
		pos++
	case SB_PAGEUP:
		pos -= rowsVisible
	case SB_PAGEDOWN:
		pos += rowsVisible
	case SB_THUMBTRACK, SB_THUMBPOSITION:
		var si SCROLLINFO
		si.CbSize = uint32(unsafe.Sizeof(SCROLLINFO{}))
		si.FMask = SIF_TRACKPOS
		pGetScrollInfo.Call(uintptr(hwnd), SB_VERT, uintptr(unsafe.Pointer(&si)))
		pos = int(si.NTrackPos)
	}
	maxPos := totalRows - rowsVisible
	if maxPos < 0 {
		maxPos = 0
	}
	if pos < 0 {
		pos = 0
	}
	if pos > maxPos {
		pos = maxPos
	}
	if kind == "tiles" {
		tilePaletteScroll = pos
	} else {
		autotilePaletteScroll = pos
	}
	updatePaletteScroll(hwnd)
	invalidate(hwnd)
}

func paletteWndProc(hwnd syscall.Handle, msg uint32, w, l uintptr) uintptr {
	done := diagEnterUI(fmt.Sprintf("paletteWndProc msg=%#x", msg))
	defer done()
	switch msg {
	case WM_PAINT:
		dn, ds := diagWMPaintStart("palette")
		palettePaint(hwnd)
		diagWMPaintEnd(dn, "palette", ds)
		return 0
	case WM_SIZE:
		dn, ds := diagWMSizeStart()
		updatePaletteScroll(hwnd)
		diagWMSizeEnd(dn, ds)
		return 0
	case WM_LBUTTONDOWN:
		// Tiles/Autotile: click = una tile; trascinamento = brush rettangolare.
		if beginPaletteRangeSelection(hwnd, l) {
			return 0
		}
		paletteClick(hwnd, l)
		return 0
	case WM_LBUTTONUP:
		if endPaletteRangeSelection(hwnd, l) {
			return 0
		}
	case WM_LBUTTONDBLCLK:
		if paletteRangeSelecting && paletteRangeSelectHWND == hwnd {
			endPaletteRangeSelection(hwnd, l)
		}
		paletteDoubleClick(hwnd, l)
		return 0
	case WM_RBUTTONDOWN:
		// Manteniamo anche il vecchio drag destro per compatibilita'.
		if beginPaletteRangeSelection(hwnd, l) {
			return 0
		}
	case WM_RBUTTONUP:
		if endPaletteRangeSelection(hwnd, l) {
			return 0
		}
	case WM_MOUSEMOVE:
		if updatePaletteRangeSelection(hwnd, l) {
			return 0
		}
		if paletteKind(hwnd) == "permissions" {
			permissionPaletteHover(hwnd, l)
			return 0
		}
	case WM_CAPTURECHANGED:
		if paletteRangeSelecting && paletteRangeSelectHWND == hwnd {
			paletteRangeSelecting = false
			paletteRangeSelectHWND = 0
		}
	case WM_MOUSELEAVE:
		if paletteKind(hwnd) == "permissions" {
			hidePermissionBehaviorHint()
			return 0
		}
	case WM_MOUSEWHEEL:
		if paletteWheelScroll(hwnd, w) {
			return 0
		}
	case WM_VSCROLL:
		paletteScroll(hwnd, w)
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(uintptr(hwnd), uintptr(msg), w, l)
	return r
}

func populatePaletteControls() {
	populateTilesetSelector()
	tilePaletteScroll = 0
	autotilePaletteScroll = 0
	tilePaletteWheelAccum = 0
	autotilePaletteWheelAccum = 0
	setPaletteSingleSelection(selectedTileID)
	for _, h := range []syscall.Handle{hwndPaletteBorderArea, hwndPaletteAutotileList, hwndPaletteTilesetList} {
		if h != 0 {
			updatePaletteScroll(h)
			invalidate(h)
		}
	}
}

func updatePaletteSelection() {
	for _, h := range []syscall.Handle{hwndPaletteBorderArea, hwndPaletteAutotileList, hwndPaletteTilesetList} {
		invalidate(h)
	}
	updateMapEditorStatus()
}
