//go:build windows

package main

import (
	"bytes"
	"embed"
	"image"
	_ "image/png"
	"syscall"
	"unsafe"
)

//go:embed toolbar_assets/*.png
var toolbarAssets embed.FS

var toolbarBitmaps []syscall.Handle

func bitmapColor(r, g, b byte) uint32 {
	return uint32(b)<<16 | uint32(g)<<8 | uint32(r)
}

func putPixel(px []uint32, x, y int, c uint32) {
	if x < 0 || y < 0 || x >= 16 || y >= 16 {
		return
	}
	px[y*16+x] = c
}

func lineH(px []uint32, x1, x2, y int, c uint32) {
	if x1 > x2 {
		x1, x2 = x2, x1
	}
	for x := x1; x <= x2; x++ {
		putPixel(px, x, y, c)
	}
}

func lineV(px []uint32, x, y1, y2 int, c uint32) {
	if y1 > y2 {
		y1, y2 = y2, y1
	}
	for y := y1; y <= y2; y++ {
		putPixel(px, x, y, c)
	}
}

func fillRectIcon(px []uint32, x1, y1, x2, y2 int, c uint32) {
	for y := y1; y <= y2; y++ {
		lineH(px, x1, x2, y, c)
	}
}

func kindAssetName(kind string) string {
	// Icone ricavate dai riferimenti RPG Maker XP / Advance Map forniti per
	// PML Studio. Le funzioni senza controparte grafica diretta continuano a
	// usare il fallback vettoriale interno.
	switch kind {
	case "new", "open", "save", "undo", "redo", "select", "pencil",
		"rectangle", "circle", "fill", "eyedropper", "eraser", "layer1", "layer2", "layer3", "alllevels",
		"folder", "map", "conn", "zoomout", "zoomin", "search",
		"help", "play", "stop", "recent", "cut", "copy", "paste":
		return "toolbar_assets/" + kind + ".png"
	default:
		return ""
	}
}

func bitmapFromImage(img image.Image) syscall.Handle {
	if img == nil {
		return 0
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return 0
	}
	px := make([]uint32, w*h)
	bgR, bgG, bgB := uint32(themeToolbarR), uint32(themeToolbarG), uint32(themeToolbarB)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r16, g16, b16, a16 := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
			a := uint32(a16 >> 8)
			sr, sg, sb := uint32(r16>>8), uint32(g16>>8), uint32(b16>>8)
			r := byte((sr*a + bgR*(255-a)) / 255)
			g := byte((sg*a + bgG*(255-a)) / 255)
			bl := byte((sb*a + bgB*(255-a)) / 255)
			px[y*w+x] = bitmapColor(r, g, bl)
		}
	}
	bmp, _, _ := pCreateBitmap.Call(uintptr(w), uintptr(h), 1, 32, uintptr(unsafe.Pointer(&px[0])))
	hbmp := syscall.Handle(bmp)
	if hbmp != 0 {
		toolbarBitmaps = append(toolbarBitmaps, hbmp)
	}
	return hbmp
}

func toolbarBitmapFromAsset(kind string) syscall.Handle {
	name := kindAssetName(kind)
	if name == "" {
		return 0
	}
	data, err := toolbarAssets.ReadFile(name)
	if err != nil {
		return 0
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return 0
	}
	return bitmapFromImage(img)
}

func toolbarBitmapFallback(kind string) syscall.Handle {
	bg := bitmapColor(themeToolbarR, themeToolbarG, themeToolbarB)
	black := bitmapColor(35, 35, 35)
	blue := bitmapColor(54, 108, 190)
	red := bitmapColor(205, 62, 50)
	green := bitmapColor(55, 150, 75)
	yellow := bitmapColor(235, 180, 55)
	purple := bitmapColor(125, 72, 170)
	gray := bitmapColor(115, 115, 115)
	px := make([]uint32, 16*16)
	for i := range px {
		px[i] = bg
	}

	switch kind {
	case "select":
		// Freccia di selezione.
		for y := 2; y <= 12; y++ {
			maxx := 2 + (y-2)/2
			for x := 2; x <= maxx; x++ {
				putPixel(px, x, y, black)
			}
		}
		lineH(px, 6, 10, 10, black)
		lineV(px, 9, 10, 14, black)
	case "pencil":
		for i := 0; i < 9; i++ {
			putPixel(px, 3+i, 12-i, yellow)
			putPixel(px, 4+i, 12-i, yellow)
		}
		putPixel(px, 2, 13, black)
		putPixel(px, 3, 13, black)
		putPixel(px, 12, 3, red)
		putPixel(px, 13, 2, red)
	case "rectangle":
		lineH(px, 3, 12, 3, blue)
		lineH(px, 3, 12, 12, blue)
		lineV(px, 3, 3, 12, blue)
		lineV(px, 12, 3, 12, blue)
	case "fill":
		fillRectIcon(px, 4, 4, 10, 9, blue)
		lineH(px, 3, 11, 10, black)
		lineV(px, 3, 5, 10, black)
		putPixel(px, 11, 11, blue)
		putPixel(px, 12, 12, blue)
	case "eyedropper":
		for i := 0; i < 8; i++ {
			putPixel(px, 4+i, 11-i, purple)
			putPixel(px, 5+i, 11-i, purple)
		}
		fillRectIcon(px, 2, 11, 4, 13, blue)
	case "eraser":
		fillRectIcon(px, 4, 5, 11, 11, bitmapColor(245, 145, 165))
		lineH(px, 4, 11, 5, black)
		lineH(px, 4, 11, 11, black)
		lineV(px, 4, 5, 11, black)
		lineV(px, 11, 5, 11, black)
	case "layer1", "layer2", "layer3":
		fillRectIcon(px, 3, 4, 12, 11, bitmapColor(235, 235, 235))
		lineH(px, 3, 12, 4, black)
		lineH(px, 3, 12, 11, black)
		lineV(px, 3, 4, 11, black)
		lineV(px, 12, 4, 11, black)
		n := 1
		if kind == "layer2" {
			n = 2
		}
		if kind == "layer3" {
			n = 3
		}
		x0 := 6 + n
		lineV(px, x0, 6, 9, blue)
		if n == 2 {
			lineH(px, x0-2, x0, 6, blue)
			lineH(px, x0-2, x0, 9, blue)
		}
		if n == 3 {
			lineH(px, x0-2, x0, 6, blue)
			lineH(px, x0-2, x0, 8, blue)
			lineH(px, x0-2, x0, 9, blue)
		}
	case "recent":
		for i := 4; i <= 11; i++ {
			putPixel(px, i, 3, blue)
			putPixel(px, i, 12, blue)
		}
		for i := 4; i <= 11; i++ {
			putPixel(px, 3, i, blue)
			putPixel(px, 12, i, blue)
		}
		lineV(px, 8, 5, 8, black)
		lineH(px, 8, 11, 8, black)
	case "conn":
		fillRectIcon(px, 2, 4, 5, 7, purple)
		fillRectIcon(px, 10, 8, 13, 11, purple)
		lineH(px, 5, 10, 7, purple)
		lineV(px, 10, 7, 8, purple)
		lineH(px, 5, 10, 8, purple)
	case "zoomout":
		for x := 3; x <= 9; x++ {
			putPixel(px, x, 3, blue)
			putPixel(px, x, 10, blue)
		}
		for y := 3; y <= 10; y++ {
			putPixel(px, 3, y, blue)
			putPixel(px, 10, y, blue)
		}
		lineH(px, 5, 8, 7, black)
		lineH(px, 10, 14, 13, black)
		putPixel(px, 11, 11, black)
		putPixel(px, 12, 12, black)
	case "zoomin":
		for x := 3; x <= 9; x++ {
			putPixel(px, x, 3, blue)
			putPixel(px, x, 10, blue)
		}
		for y := 3; y <= 10; y++ {
			putPixel(px, 3, y, blue)
			putPixel(px, 10, y, blue)
		}
		lineH(px, 5, 8, 7, black)
		lineV(px, 7, 5, 9, black)
		lineH(px, 10, 14, 13, black)
		putPixel(px, 11, 11, black)
		putPixel(px, 12, 12, black)
	case "search":
		for x := 3; x <= 9; x++ {
			putPixel(px, x, 3, blue)
			putPixel(px, x, 9, blue)
		}
		for y := 3; y <= 9; y++ {
			putPixel(px, 3, y, blue)
			putPixel(px, 9, y, blue)
		}
		putPixel(px, 10, 10, black)
		putPixel(px, 11, 11, black)
		putPixel(px, 12, 12, black)
		putPixel(px, 13, 13, black)
	case "help":
		for x := 3; x <= 12; x++ {
			putPixel(px, x, 2, blue)
			putPixel(px, x, 13, blue)
		}
		for y := 2; y <= 13; y++ {
			putPixel(px, 2, y, blue)
			putPixel(px, 13, y, blue)
		}
		lineH(px, 6, 9, 5, bitmapColor(255, 255, 255))
		putPixel(px, 10, 6, bitmapColor(255, 255, 255))
		putPixel(px, 9, 7, bitmapColor(255, 255, 255))
		putPixel(px, 8, 8, bitmapColor(255, 255, 255))
		putPixel(px, 8, 11, bitmapColor(255, 255, 255))
	case "play":
		for y := 3; y <= 12; y++ {
			for x := 4; x <= 4+(y-3)/2; x++ {
				putPixel(px, x, y, green)
			}
		}
	case "stop":
		fillRectIcon(px, 4, 4, 11, 11, red)
	case "new":
		fillRectIcon(px, 3, 2, 11, 13, bitmapColor(255, 255, 255))
		lineV(px, 3, 2, 13, black)
		lineH(px, 3, 11, 2, black)
		lineV(px, 11, 2, 13, black)
		lineH(px, 3, 11, 13, black)
		lineH(px, 6, 9, 8, green)
		lineV(px, 7, 6, 10, green)
		lineV(px, 8, 6, 10, green)
	case "open":
		fillRectIcon(px, 2, 5, 13, 12, yellow)
		fillRectIcon(px, 3, 3, 8, 6, yellow)
		lineH(px, 2, 13, 5, black)
		lineV(px, 2, 5, 12, black)
		lineH(px, 2, 13, 12, black)
		lineV(px, 13, 6, 12, black)
		lineH(px, 4, 12, 7, bitmapColor(255, 218, 95))
	case "save":
		fillRectIcon(px, 3, 2, 12, 13, blue)
		fillRectIcon(px, 5, 3, 10, 6, bitmapColor(230, 240, 255))
		fillRectIcon(px, 5, 9, 10, 12, bitmapColor(245, 245, 245))
		lineH(px, 3, 12, 2, black)
		lineH(px, 3, 12, 13, black)
		lineV(px, 3, 2, 13, black)
		lineV(px, 12, 2, 13, black)
	case "folder":
		fillRectIcon(px, 2, 5, 13, 12, yellow)
		fillRectIcon(px, 3, 3, 8, 5, yellow)
		lineH(px, 2, 13, 5, black)
		lineV(px, 2, 5, 12, black)
		lineH(px, 2, 13, 12, black)
	case "map":
		fillRectIcon(px, 2, 3, 13, 12, bitmapColor(230, 245, 230))
		lineH(px, 2, 13, 3, black)
		lineH(px, 2, 13, 12, black)
		lineV(px, 2, 3, 12, black)
		lineV(px, 13, 3, 12, black)
		lineV(px, 6, 4, 11, green)
		lineV(px, 10, 4, 11, blue)
		lineH(px, 3, 12, 7, gray)
	}

	bmp, _, _ := pCreateBitmap.Call(16, 16, 1, 32, uintptr(unsafe.Pointer(&px[0])))
	h := syscall.Handle(bmp)
	if h != 0 {
		toolbarBitmaps = append(toolbarBitmaps, h)
	}
	return h
}

func toolbarBitmap(kind string) syscall.Handle {
	if h := toolbarBitmapFromAsset(kind); h != 0 {
		return h
	}
	return toolbarBitmapFallback(kind)
}

func setButtonBitmap(button syscall.Handle, kind string) {
	if button == 0 {
		return
	}
	bmp := toolbarBitmap(kind)
	if bmp != 0 {
		pSendMessageW.Call(uintptr(button), BM_SETIMAGE, IMAGE_BITMAP, uintptr(bmp))
	}
}

func releaseToolbarBitmaps() {
	for _, b := range toolbarBitmaps {
		if b != 0 {
			pDeleteObject.Call(uintptr(b))
		}
	}
	toolbarBitmaps = nil
}
