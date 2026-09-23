//go:build windows

package main

import (
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
)

func init() {
	diagCurrentWorkerOperation.Store("idle")
	diagCurrentUIOperation.Store("idle")
}

func TestMapViewportPixels(t *testing.T) {
	surface := &PixelSurface{Width: 1152, Height: 1600, Pixels: make([]uint32, 1152*1600)}
	for y := 0; y < 1600; y++ {
		for x := 0; x < 1152; x++ {
			surface.Pixels[y*1152+x] = uint32(y%256)<<16 | uint32(x%256)<<8 | 0x33
		}
	}
	checkViewportPixels(t, surface)
}

func checkViewportPixels(t *testing.T, surface *PixelSurface) {
	t.Helper()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	// Draw into a memory DC: no window or desktop interaction is required.
	dc, _, _ := gdi32.NewProc("CreateCompatibleDC").Call(0)
	if dc == 0 {
		t.Fatal("CreateCompatibleDC failed")
	}
	defer gdi32.NewProc("DeleteDC").Call(dc)
	bmp, _, _ := gdi32.NewProc("CreateBitmap").Call(1200, 900, 1, 32, 0)
	if bmp == 0 {
		t.Fatal("CreateBitmap failed")
	}
	old, _, _ := pSelectObject.Call(dc, bmp)
	if old == 0 || int32(old) == -1 {
		t.Fatal("SelectObject failed")
	}
	defer func() { pSelectObject.Call(dc, old); pDeleteObject.Call(bmp) }()
	saved := currentMapSurface
	defer func() { currentMapSurface = saved; mapScrollX = 0; mapScrollY = 0; mapZoomIndex = 0 }()
	currentMapSurface = surface
	for zoom := 0; zoom < 3; zoom++ {
		mapZoomIndex = zoom
		mapScrollX, mapScrollY = 16, 24
		if !drawMapSurfaceViewport(syscall.Handle(dc), 1079, 661) {
			t.Errorf("draw failed at zoom %d", zoom)
		}
		for y := 0; y < 661; y += 33 {
			for x := 0; x < 1079; x += 33 {
				sx, sy := (mapScrollX+x)*32/mapDisplayTileSize(), (mapScrollY+y)*32/mapDisplayTileSize()
				if sx >= surface.Width || sy >= surface.Height {
					continue
				}
				got, _, _ := gdi32.NewProc("GetPixel").Call(dc, uintptr(x), uintptr(int(gridOriginY)+y))
				px := surface.Pixels[sy*surface.Width+sx]
				want := (px&255)<<16 | px&0xff00 | (px>>16)&255
				if uint32(got) != want {
					t.Fatalf("zoom %d at %d,%d: pixel=%06x want=%06x", zoom, x, y, got, want)
				}
			}
		}
	}
}

func TestProjectMapLoading(t *testing.T) {
	root := os.Getenv("PLM_TEST_PROJECT")
	if root == "" {
		t.Skip("set PLM_TEST_PROJECT to audit a converted project read-only")
	}
	files, err := filepath.Glob(filepath.Join(root, "converted", "maps", "Map*.json"))
	if err != nil || len(files) == 0 {
		t.Fatal("no maps", err)
	}
	for _, file := range files {
		doc, err := loadMapDocument(file)
		if err != nil {
			t.Errorf("%s: %v", filepath.Base(file), err)
			continue
		}
		ts, err := loadTilesetDescriptor(root, doc.TilesetID)
		if err != nil {
			t.Errorf("%s: %v", filepath.Base(file), err)
			continue
		}
		surface, err := buildMapSurface(doc, ts)
		if err != nil {
			t.Errorf("%s: %v", filepath.Base(file), err)
			continue
		}
		if filepath.Base(file) == "Map005.json" {
			t.Run("VentifoglieViewport", func(t *testing.T) { checkViewportPixels(t, surface) })
			colored := 0
			for _, px := range surface.Pixels {
				if px != 0xF4F4F4 {
					colored++
				}
			}
			if colored == 0 {
				t.Error("Ventifoglie renders blank")
			}
			t.Logf("Ventifoglie: %d non-background pixels", colored)
		}
	}
	t.Logf("Audited %d maps", len(files))
}
