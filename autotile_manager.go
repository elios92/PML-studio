//go:build windows

package main

// Tabella canonica RPG Maker XP/RGSS1. I valori originali sono 1-based;
// qui sono convertiti a 0-based per indicizzare i quarter-tile 16x16.
var xpAutotilePatterns = [48][4]int{
	{26, 27, 32, 33}, {4, 27, 32, 33}, {26, 5, 32, 33}, {4, 5, 32, 33},
	{26, 27, 32, 11}, {4, 27, 32, 11}, {26, 5, 32, 11}, {4, 5, 32, 11},
	{26, 27, 10, 33}, {4, 27, 10, 33}, {26, 5, 10, 33}, {4, 5, 10, 33},
	{26, 27, 10, 11}, {4, 27, 10, 11}, {26, 5, 10, 11}, {4, 5, 10, 11},
	{24, 25, 30, 31}, {24, 5, 30, 31}, {24, 25, 30, 11}, {24, 5, 30, 11},
	{14, 15, 20, 21}, {14, 15, 20, 11}, {14, 15, 10, 21}, {14, 15, 10, 11},
	{28, 29, 34, 35}, {28, 29, 10, 35}, {4, 29, 34, 35}, {4, 29, 10, 35},
	{38, 39, 44, 45}, {4, 39, 44, 45}, {38, 5, 44, 45}, {4, 5, 44, 45},
	{24, 29, 30, 35}, {14, 15, 44, 45}, {12, 13, 18, 19}, {12, 13, 18, 11},
	{16, 17, 22, 23}, {16, 17, 10, 23}, {40, 41, 46, 47}, {4, 41, 46, 47},
	{36, 37, 42, 43}, {36, 5, 42, 43}, {12, 17, 18, 23}, {12, 13, 42, 43},
	{36, 41, 42, 47}, {16, 17, 46, 47}, {12, 17, 42, 47}, {12, 17, 42, 47},
}

type autotileRenderInfo struct {
	Surface    *PixelSurface
	Pattern    [4]int
	Simple     bool
	SimpleSrcX int
	SimpleSrcY int
	FrameX     int
}

func autotileRenderParts(tileID int) (info autotileRenderInfo, ok bool) {
	if currentTileset == nil || tileID < 48 || tileID >= 384 {
		return info, false
	}

	slot := tileID/48 - 1
	variant := tileID % 48
	if slot < 0 || slot >= len(currentTileset.Autotiles) || variant < 0 || variant >= len(xpAutotilePatterns) {
		return info, false
	}

	s := currentTileset.Autotiles[slot]
	if s == nil || s.Width <= 0 || s.Height <= 0 {
		return info, false
	}

	info.Surface = s
	info.Pattern = xpAutotilePatterns[variant]

	// Formati semplici già composti: un tile 32x32 oppure una strip di frame
	// 32x32. In questo caso non va applicata la tabella RMXP dei quarter-tile.
	if s.Height == 32 && s.Width >= 32 {
		info.Simple = true
		info.SimpleSrcX = 0 // frame 0 nell'editor
		info.SimpleSrcY = 0
		return info, true
	}
	if s.Width == 32 && s.Height >= 32 && s.Height%32 == 0 {
		info.Simple = true
		info.SimpleSrcX = 0
		info.SimpleSrcY = 0
		return info, true
	}

	// RPG Maker XP: ogni frame dell'autotile è largo 96 pixel. La tabella
	// AUTO_INDEX indirizza direttamente una griglia 6x8 di quarter-tile 16x16
	// all'interno di ciascun frame 96x128. Non va aggiunto un offset Y fisso.
	if s.Width >= 96 && s.Height >= 128 {
		info.FrameX = 0 // frame animazione 0; il runtime potrà animarlo in seguito
		return info, true
	}

	// Alcuni converter esportano già il template senza le ultime righe vuote,
	// in 96x96. La stessa tabella resta valida per i pattern che vi ricadono.
	if s.Width >= 96 && s.Height >= 96 {
		info.FrameX = 0
		return info, true
	}

	return info, false
}

func autotileParts(tileID int) (surface *PixelSurface, pattern [4]int, ok bool) {
	info, ok := autotileRenderParts(tileID)
	if !ok || info.Simple {
		return nil, pattern, false
	}
	return info.Surface, info.Pattern, true
}
