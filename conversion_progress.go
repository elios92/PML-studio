//go:build windows

package main

import (
	"fmt"
	"sync"
	"syscall"
	"unsafe"
)

const (
	wmAppImportProgress = 0x80A0
	wmAppImportDone     = 0x80A1
	pbmSetPos           = 0x0402
	pbmSetRange32       = 0x0406
)

type essentialsConversionDialogState struct {
	mu sync.Mutex

	progress EssentialsImportProgress
	report   *EssentialsImportReport
	err      error
	finished bool

	hwnd      syscall.Handle
	hPhase    syscall.Handle
	hDetail   syscall.Handle
	hProgress syscall.Handle
	hPercent  syscall.Handle
	hSource   syscall.Handle
	hDest     syscall.Handle
	hStatus   syscall.Handle
	owner     syscall.Handle
}

var (
	conversionDialogClassOnce sync.Once
	conversionDialogState     essentialsConversionDialogState
)

func ensureConversionDialogClass() {
	conversionDialogClassOnce.Do(func() {
		hi, _, _ := pGetModuleHandleW.Call(0)
		hInst := syscall.Handle(hi)
		cur, _, _ := pLoadCursorW.Call(0, IDC_ARROW)
		className := wstr("PLMStudioEssentialsConversion01")
		wc := WNDCLASSEX{
			cbSize:        uint32(unsafe.Sizeof(WNDCLASSEX{})),
			lpfnWndProc:   syscall.NewCallback(conversionProgressWndProc),
			hInstance:     hInst,
			hCursor:       syscall.Handle(cur),
			hbrBackground: themeWindowBackgroundBrush(),
			lpszClassName: className,
		}
		pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	})
}

func conversionProgressWndProc(hwnd syscall.Handle, msg uint32, w, l uintptr) uintptr {
	switch msg {
	case wmAppImportProgress:
		refreshConversionProgressControls()
		return 0
	case wmAppImportDone:
		refreshConversionProgressControls()
		conversionDialogState.mu.Lock()
		conversionDialogState.finished = true
		conversionDialogState.mu.Unlock()
		return 0
	case WM_CLOSE:
		conversionDialogState.mu.Lock()
		running := !conversionDialogState.finished
		conversionDialogState.mu.Unlock()
		if running {
			// A partial Essentials import must not be interrupted by simply closing
			// the window. The source project is read-only, but the destination may
			// be in the middle of an atomic copy/conversion operation.
			return 0
		}
	case WM_SIZE:
		layoutConversionProgressDialog(hwnd)
		return 0
	case WM_CTLCOLORSTATIC, WM_CTLCOLOREDIT, WM_CTLCOLORLISTBOX, WM_CTLCOLORBTN:
		if brush := modernCtlColor(msg, w, syscall.Handle(l)); brush != 0 {
			return brush
		}
	case WM_ERASEBKGND:
		if themeBrushWindow != 0 {
			var rc RECT
			pGetClientRect.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&rc)))
			pFillRect.Call(w, uintptr(unsafe.Pointer(&rc)), uintptr(themeBrushWindow))
			return 1
		}
	}
	r, _, _ := pDefWindowProcW.Call(uintptr(hwnd), uintptr(msg), w, l)
	return r
}

func layoutConversionProgressDialog(hwnd syscall.Handle) {
	conversionDialogState.mu.Lock()
	defer conversionDialogState.mu.Unlock()
	if conversionDialogState.hwnd == 0 || conversionDialogState.hwnd != hwnd {
		return
	}
	var rc RECT
	pGetClientRect.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&rc)))
	width := int32(rc.Right - rc.Left)
	if width < 520 {
		width = 520
	}
	margin := int32(22)
	inner := width - margin*2
	moveControl(conversionDialogState.hPhase, margin, 22, inner, 24)
	moveControl(conversionDialogState.hDetail, margin, 52, inner, 42)
	moveControl(conversionDialogState.hProgress, margin, 104, inner-70, 24)
	moveControl(conversionDialogState.hPercent, width-margin-62, 104, 62, 24)
	moveControl(conversionDialogState.hSource, margin, 142, inner, 22)
	moveControl(conversionDialogState.hDest, margin, 168, inner, 22)
	moveControl(conversionDialogState.hStatus, margin, 202, inner, 24)
}

func refreshConversionProgressControls() {
	conversionDialogState.mu.Lock()
	p := conversionDialogState.progress
	hPhase := conversionDialogState.hPhase
	hDetail := conversionDialogState.hDetail
	hProgress := conversionDialogState.hProgress
	hPercent := conversionDialogState.hPercent
	hStatus := conversionDialogState.hStatus
	finished := conversionDialogState.finished
	err := conversionDialogState.err
	conversionDialogState.mu.Unlock()

	if hPhase != 0 {
		setText(hPhase, p.Phase)
	}
	if hDetail != 0 {
		setText(hDetail, p.Detail)
	}
	if hProgress != 0 {
		pSendMessageW.Call(uintptr(hProgress), pbmSetPos, uintptr(p.Percent), 0)
	}
	if hPercent != 0 {
		setText(hPercent, fmt.Sprintf("%d%%", p.Percent))
	}
	if hStatus != 0 {
		switch {
		case finished && err != nil:
			setText(hStatus, "Conversione interrotta per errore.")
		case finished:
			setText(hStatus, "Conversione completata. Apertura progetto...")
		default:
			setText(hStatus, "PLM Studio sta lavorando. La finestra rimane attiva durante la conversione.")
		}
	}
}

func centerConversionWindow(owner syscall.Handle, width, height int32) (int32, int32) {
	if owner == 0 {
		return -2147483648, -2147483648
	}
	var rc RECT
	if r, _, _ := pGetWindowRect.Call(uintptr(owner), uintptr(unsafe.Pointer(&rc))); r == 0 {
		return -2147483648, -2147483648
	}
	x := rc.Left + ((rc.Right-rc.Left)-width)/2
	y := rc.Top + ((rc.Bottom-rc.Top)-height)/2
	return x, y
}

// showEssentialsConversionProgress runs the expensive conversion on a worker
// goroutine while this OS/UI thread continues pumping Win32 messages. This is
// deliberately modal from the user's point of view, but never freezes the main
// window or triggers Windows' "Not responding" state.
func showEssentialsConversionProgress(owner syscall.Handle, source, dest string) (*EssentialsImportReport, error) {
	ensureConversionDialogClass()

	conversionDialogState.mu.Lock()
	conversionDialogState.progress = EssentialsImportProgress{Percent: 0, Phase: "Preparazione conversione", Detail: "Avvio importatore Pokémon Essentials v20.1..."}
	conversionDialogState.report = nil
	conversionDialogState.err = nil
	conversionDialogState.finished = false
	conversionDialogState.owner = owner
	conversionDialogState.hwnd = 0
	conversionDialogState.hPhase = 0
	conversionDialogState.hDetail = 0
	conversionDialogState.hProgress = 0
	conversionDialogState.hPercent = 0
	conversionDialogState.hSource = 0
	conversionDialogState.hDest = 0
	conversionDialogState.hStatus = 0
	conversionDialogState.mu.Unlock()

	hi, _, _ := pGetModuleHandleW.Call(0)
	hInst := syscall.Handle(hi)
	width, height := int32(690), int32(300)
	x, y := centerConversionWindow(owner, width, height)
	hwnd := createWindow("PLMStudioEssentialsConversion01", "Conversione Pokémon Essentials → PLM/Python", WS_OVERLAPPEDWINDOW|WS_VISIBLE, x, y, width, height, owner, 0, hInst)
	if hwnd == 0 {
		return convertEssentialsProject(source, dest)
	}
	setWindowIcon(hwnd)
	applyModernWindowFrame(hwnd)

	conversionDialogState.mu.Lock()
	conversionDialogState.hwnd = hwnd
	conversionDialogState.hPhase = createWindow("STATIC", "Preparazione conversione", WS_CHILD|WS_VISIBLE, 0, 0, 0, 0, hwnd, 1, hInst)
	conversionDialogState.hDetail = createWindow("STATIC", "Avvio importatore...", WS_CHILD|WS_VISIBLE, 0, 0, 0, 0, hwnd, 2, hInst)
	conversionDialogState.hProgress = createWindow("msctls_progress32", "", WS_CHILD|WS_VISIBLE, 0, 0, 0, 0, hwnd, 3, hInst)
	conversionDialogState.hPercent = createWindow("STATIC", "0%", WS_CHILD|WS_VISIBLE, 0, 0, 0, 0, hwnd, 4, hInst)
	conversionDialogState.hSource = createWindow("STATIC", "Origine: "+source, WS_CHILD|WS_VISIBLE, 0, 0, 0, 0, hwnd, 5, hInst)
	conversionDialogState.hDest = createWindow("STATIC", "Destinazione: "+dest, WS_CHILD|WS_VISIBLE, 0, 0, 0, 0, hwnd, 6, hInst)
	conversionDialogState.hStatus = createWindow("STATIC", "PLM Studio sta lavorando...", WS_CHILD|WS_VISIBLE, 0, 0, 0, 0, hwnd, 7, hInst)
	hProgress := conversionDialogState.hProgress
	conversionDialogState.mu.Unlock()
	if hProgress != 0 {
		pSendMessageW.Call(uintptr(hProgress), pbmSetRange32, 0, 100)
		pSendMessageW.Call(uintptr(hProgress), pbmSetPos, 0, 0)
	}
	layoutConversionProgressDialog(hwnd)
	pShowWindow.Call(uintptr(hwnd), SW_SHOW)
	pUpdateWindow.Call(uintptr(hwnd))
	if owner != 0 {
		pEnableWindow.Call(uintptr(owner), 0)
	}

	go func(dialog syscall.Handle) {
		report, err := convertEssentialsProjectWithProgress(source, dest, func(p EssentialsImportProgress) {
			conversionDialogState.mu.Lock()
			// Enforce monotonic UI progress even if a future importer phase sends a
			// slightly older stage percentage.
			if p.Percent < conversionDialogState.progress.Percent {
				p.Percent = conversionDialogState.progress.Percent
			}
			conversionDialogState.progress = p
			conversionDialogState.mu.Unlock()
			pPostMessageW.Call(uintptr(dialog), wmAppImportProgress, 0, 0)
		})
		conversionDialogState.mu.Lock()
		conversionDialogState.report = report
		conversionDialogState.err = err
		if err == nil && conversionDialogState.progress.Percent < 100 {
			conversionDialogState.progress = EssentialsImportProgress{Percent: 100, Phase: "Conversione completata", Detail: "Finalizzazione completata."}
		}
		conversionDialogState.mu.Unlock()
		pPostMessageW.Call(uintptr(dialog), wmAppImportDone, 0, 0)
	}(hwnd)

	var m MSG
	for {
		conversionDialogState.mu.Lock()
		finished := conversionDialogState.finished
		conversionDialogState.mu.Unlock()
		if finished {
			break
		}
		r, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		pDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}

	if owner != 0 {
		pEnableWindow.Call(uintptr(owner), 1)
		pSetFocus.Call(uintptr(owner))
	}
	pDestroyWindow.Call(uintptr(hwnd))

	conversionDialogState.mu.Lock()
	report := conversionDialogState.report
	err := conversionDialogState.err
	conversionDialogState.hwnd = 0
	conversionDialogState.mu.Unlock()
	return report, err
}
