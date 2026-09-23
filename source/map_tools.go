//go:build windows

package main

import "fmt"

type MapTool int

const (
	ToolSelect MapTool = iota
	ToolPencil
	ToolRectangle
	ToolFill
	ToolEyedropper
	ToolEraser
)

type TileChange struct {
	Layer  int
	X, Y   int
	Before int
	After  int
}

type MapEditOperation struct {
	Changes []TileChange
	Name    string
}

var (
	activeMapTool  MapTool = ToolPencil
	activeLayer            = 0
	selectedTileID         = 384
	// selectedTileBrush conserva una selezione rettangolare della palette.
	// Un solo tile equivale a un brush 1x1. Serve per poter selezionare un
	// oggetto composto (albero, edificio, ecc.) trascinando col tasto destro.
	selectedTileBrush      = []int{384}
	selectedTileBrushW     = 1
	selectedTileBrushH     = 1
	mapDirty               bool
	undoStack              []MapEditOperation
	redoStack              []MapEditOperation
	currentStroke          *MapEditOperation
	rectStartX, rectStartY int
	rectDragging           bool
	mouseMapX, mouseMapY   = -1, -1
	selectionX, selectionY = -1, -1
)

func setSelectedTileBrush(ids []int, w, h int) {
	if w <= 0 || h <= 0 || len(ids) < w*h {
		selectedTileBrush = []int{selectedTileID}
		selectedTileBrushW, selectedTileBrushH = 1, 1
		return
	}
	selectedTileBrush = append(selectedTileBrush[:0], ids[:w*h]...)
	selectedTileBrushW, selectedTileBrushH = w, h
	if len(selectedTileBrush) > 0 && selectedTileBrush[0] > 0 {
		selectedTileID = selectedTileBrush[0]
	}
}

func setSingleSelectedTile(id int) {
	selectedTileID = id
	selectedTileBrush = []int{id}
	selectedTileBrushW, selectedTileBrushH = 1, 1
}

func stampSelectedTileBrush(x, y int) {
	if selectedTileBrushW <= 1 || selectedTileBrushH <= 1 || len(selectedTileBrush) < selectedTileBrushW*selectedTileBrushH {
		appendTileChange(activeLayer, x, y, selectedTileID)
		return
	}
	for by := 0; by < selectedTileBrushH; by++ {
		for bx := 0; bx < selectedTileBrushW; bx++ {
			tx, ty := x+bx, y+by
			if tx < 0 || ty < 0 || tx >= currentMapW || ty >= currentMapH {
				continue
			}
			id := selectedTileBrush[by*selectedTileBrushW+bx]
			// La palette non contiene l'ID 0: uno zero qui indica solamente una
			// cella fuori dal rettangolo valido dell'ultima riga, non una gomma.
			if id <= 0 {
				continue
			}
			appendTileChange(activeLayer, tx, ty, id)
		}
	}
}

func mapTile(layer, x, y int) int {
	if currentMapDoc == nil || layer < 0 || layer >= len(currentMapDoc.Table.Layers) || y < 0 || y >= currentMapDoc.Table.Height || x < 0 || x >= currentMapDoc.Table.Width {
		return 0
	}
	return currentMapDoc.Table.Layers[layer][y][x]
}

func setMapTileRaw(layer, x, y, value int) bool {
	if currentMapDoc == nil || layer < 0 || layer >= len(currentMapDoc.Table.Layers) || y < 0 || y >= currentMapDoc.Table.Height || x < 0 || x >= currentMapDoc.Table.Width {
		return false
	}
	currentMapDoc.Table.Layers[layer][y][x] = value
	// La collisione/Terrain Tag di base dipende dai tile reali della cella.
	// Manteniamo quindi la vista Movimenti/Terrain sincronizzata anche mentre
	// si disegna la mappa, senza aspettare un reload completo.
	refreshDerivedPermissionCell(x, y)
	return true
}

func markMapDirty() {
	mapDirty = true
	updateMapEditorStatus()
}

func clearMapHistory() {
	undoStack = nil
	redoStack = nil
	currentStroke = nil
	mapDirty = false
	rectDragging = false
}

func beginMapOperation(name string) {
	currentStroke = &MapEditOperation{Name: name}
}

func appendTileChange(layer, x, y, after int) {
	before := mapTile(layer, x, y)
	if before == after {
		return
	}
	if currentStroke == nil {
		beginMapOperation("Modifica")
	}
	// If this cell is touched repeatedly during one drag, keep the original
	// value and only update the final value.
	for i := range currentStroke.Changes {
		c := &currentStroke.Changes[i]
		if c.Layer == layer && c.X == x && c.Y == y {
			c.After = after
			setMapTileRaw(layer, x, y, after)
			refreshMapSurfaceCell(x, y)
			markMapDirty()
			return
		}
	}
	currentStroke.Changes = append(currentStroke.Changes, TileChange{Layer: layer, X: x, Y: y, Before: before, After: after})
	setMapTileRaw(layer, x, y, after)
	refreshMapSurfaceCell(x, y)
	markMapDirty()
}

func commitMapOperation() {
	if currentStroke == nil {
		return
	}
	if len(currentStroke.Changes) > 0 {
		undoStack = append(undoStack, *currentStroke)
		if len(undoStack) > 256 {
			undoStack = undoStack[len(undoStack)-256:]
		}
		redoStack = nil
	}
	currentStroke = nil
	updateMapEditorStatus()
}

func undoMapEdit() {
	if currentStroke != nil {
		commitMapOperation()
	}
	if len(undoStack) == 0 {
		return
	}
	op := undoStack[len(undoStack)-1]
	undoStack = undoStack[:len(undoStack)-1]
	for i := len(op.Changes) - 1; i >= 0; i-- {
		c := op.Changes[i]
		setMapTileRaw(c.Layer, c.X, c.Y, c.Before)
		refreshMapSurfaceCell(c.X, c.Y)
	}
	redoStack = append(redoStack, op)
	mapDirty = true
	updateMapEditorStatus()
	invalidate(hwndCanvas)
}

func redoMapEdit() {
	if len(redoStack) == 0 {
		return
	}
	op := redoStack[len(redoStack)-1]
	redoStack = redoStack[:len(redoStack)-1]
	for _, c := range op.Changes {
		setMapTileRaw(c.Layer, c.X, c.Y, c.After)
		refreshMapSurfaceCell(c.X, c.Y)
	}
	undoStack = append(undoStack, op)
	mapDirty = true
	updateMapEditorStatus()
	invalidate(hwndCanvas)
}

func floodFillMap(x, y int, replacement int) {
	if currentMapDoc == nil {
		return
	}
	target := mapTile(activeLayer, x, y)
	if target == replacement {
		return
	}
	beginMapOperation("Secchiello")
	type pt struct{ x, y int }
	queue := make([]pt, 0, 256)
	queue = append(queue, pt{x, y})
	seen := make([]bool, currentMapW*currentMapH)
	for len(queue) > 0 {
		p := queue[len(queue)-1]
		queue = queue[:len(queue)-1]
		if p.x < 0 || p.y < 0 || p.x >= currentMapW || p.y >= currentMapH {
			continue
		}
		idx := p.y*currentMapW + p.x
		if seen[idx] {
			continue
		}
		seen[idx] = true
		if mapTile(activeLayer, p.x, p.y) != target {
			continue
		}
		appendTileChange(activeLayer, p.x, p.y, replacement)
		queue = append(queue, pt{p.x - 1, p.y}, pt{p.x + 1, p.y}, pt{p.x, p.y - 1}, pt{p.x, p.y + 1})
	}
	commitMapOperation()
	invalidate(hwndCanvas)
}

func fillRectangleMap(x1, y1, x2, y2, value int) {
	if x1 > x2 {
		x1, x2 = x2, x1
	}
	if y1 > y2 {
		y1, y2 = y2, y1
	}
	beginMapOperation("Rettangolo")
	for y := y1; y <= y2; y++ {
		for x := x1; x <= x2; x++ {
			appendTileChange(activeLayer, x, y, value)
		}
	}
	commitMapOperation()
	invalidate(hwndCanvas)
}

func sampleTileAt(x, y int) {
	setSingleSelectedTile(mapTile(activeLayer, x, y))
	setPaletteSingleSelection(selectedTileID)
	ensureSelectedTileVisible()
	updatePaletteSelection()
	setText(hwndStatus, fmt.Sprintf("Tile campionato: %d | Layer %d | %d,%d", selectedTileID, activeLayer+1, x, y))
}

// sampleVisibleTileAt seleziona il tile realmente visibile nel punto cliccato.
// Cerca dall'ultimo layer verso il primo, ma NON cambia il layer di destinazione
// scelto dall'utente. In questo modo il tasto destro può campionare un tile dai
// layer superiori senza fare sì che il successivo click sinistro disegni, per
// errore, su un layer diverso. Con Layer 0 selezionato, il tile campionato viene
// quindi sempre disegnato su Layer 0 finché l'utente non cambia layer a mano.
func sampleVisibleTileAt(x, y int) {
	sourceLayer := activeLayer
	id := 0
	if currentMapDoc != nil {
		for l := len(currentMapDoc.Table.Layers) - 1; l >= 0; l-- {
			v := mapTile(l, x, y)
			if v != 0 {
				sourceLayer, id = l, v
				break
			}
		}
	}
	setSingleSelectedTile(id)
	setPaletteSingleSelection(id)
	ensureSelectedTileVisible()
	updatePaletteSelection()
	setText(hwndStatus, fmt.Sprintf("Tile copiato: %d | sorgente Layer %d | destinazione Layer %d | %d,%d", selectedTileID, sourceLayer+1, activeLayer+1, x, y))
}

func mapToolName(tool MapTool) string {
	switch tool {
	case ToolSelect:
		return "Selezione"
	case ToolPencil:
		return "Matita"
	case ToolRectangle:
		return "Rettangolo"
	case ToolFill:
		return "Secchiello"
	case ToolEyedropper:
		return "Contagocce"
	case ToolEraser:
		return "Gomma"
	default:
		return "Mappa"
	}
}

func updateMapEditorStatus() {
	if currentMap == nil {
		return
	}
	dirty := "salvata"
	if mapDirty {
		dirty = "MODIFICATA"
	}
	coords := ""
	if mouseMapX >= 0 && mouseMapY >= 0 {
		coords = fmt.Sprintf(" | X:%d Y:%d", mouseMapX, mouseMapY)
	}
	region := mapRegionDisplayName(currentMap.RegionID)
	setText(hwndStatus, fmt.Sprintf("Mappa %03d | %s | %s | Layer %d | Tile %d | %s%s", currentMap.ID, region, mapToolName(activeMapTool), activeLayer+1, selectedTileID, dirty, coords))
}
