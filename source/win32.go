//go:build windows

package main

import (
	"embed"
	"encoding/binary"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

//go:embed pokeball.ico
var assets embed.FS

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	ole32    = syscall.NewLazyDLL("ole32.dll")
	dwmapi   = syscall.NewLazyDLL("dwmapi.dll")
	uxtheme  = syscall.NewLazyDLL("uxtheme.dll")

	pCreateWindowExW      = user32.NewProc("CreateWindowExW")
	pDefWindowProcW       = user32.NewProc("DefWindowProcW")
	pDispatchMessageW     = user32.NewProc("DispatchMessageW")
	pGetMessageW          = user32.NewProc("GetMessageW")
	pPostQuitMessage      = user32.NewProc("PostQuitMessage")
	pPostMessageW         = user32.NewProc("PostMessageW")
	pRegisterClassExW     = user32.NewProc("RegisterClassExW")
	pShowWindow           = user32.NewProc("ShowWindow")
	pTranslateMessage     = user32.NewProc("TranslateMessage")
	pUpdateWindow         = user32.NewProc("UpdateWindow")
	pLoadCursorW          = user32.NewProc("LoadCursorW")
	pLoadImageW           = user32.NewProc("LoadImageW")
	pSendMessageW         = user32.NewProc("SendMessageW")
	pSetWindowTextW       = user32.NewProc("SetWindowTextW")
	pMoveWindow           = user32.NewProc("MoveWindow")
	pGetClientRect        = user32.NewProc("GetClientRect")
	pMessageBoxW          = user32.NewProc("MessageBoxW")
	pInvalidateRect       = user32.NewProc("InvalidateRect")
	pRedrawWindow         = user32.NewProc("RedrawWindow")
	pGetWindowTextW       = user32.NewProc("GetWindowTextW")
	pGetWindowTextLengthW = user32.NewProc("GetWindowTextLengthW")
	pGetClassNameW        = user32.NewProc("GetClassNameW")
	pGetDlgCtrlID         = user32.NewProc("GetDlgCtrlID")
	pBeginPaint           = user32.NewProc("BeginPaint")
	pEndPaint             = user32.NewProc("EndPaint")
	pFillRect             = user32.NewProc("FillRect")
	pCreateMenu           = user32.NewProc("CreateMenu")
	pCreatePopupMenu      = user32.NewProc("CreatePopupMenu")
	pAppendMenuW          = user32.NewProc("AppendMenuW")
	pTrackPopupMenu       = user32.NewProc("TrackPopupMenu")
	pDestroyMenu          = user32.NewProc("DestroyMenu")
	pSetForegroundWindow  = user32.NewProc("SetForegroundWindow")
	pSetMenu              = user32.NewProc("SetMenu")
	pDrawMenuBar          = user32.NewProc("DrawMenuBar")
	pDestroyWindow        = user32.NewProc("DestroyWindow")
	pEnableWindow         = user32.NewProc("EnableWindow")
	pDestroyIcon          = user32.NewProc("DestroyIcon")
	pSetCapture           = user32.NewProc("SetCapture")
	pReleaseCapture       = user32.NewProc("ReleaseCapture")
	pSetScrollInfo        = user32.NewProc("SetScrollInfo")
	pGetScrollInfo        = user32.NewProc("GetScrollInfo")
	pGetWindowRect        = user32.NewProc("GetWindowRect")
	pTrackMouseEvent      = user32.NewProc("TrackMouseEvent")
	pCallWindowProcW      = user32.NewProc("CallWindowProcW")
	pSetWindowLongPtrW    = user32.NewProc("SetWindowLongPtrW")
	pSetFocus             = user32.NewProc("SetFocus")
	pSetCursor            = user32.NewProc("SetCursor")
	pGetCursorPos         = user32.NewProc("GetCursorPos")
	pScreenToClient       = user32.NewProc("ScreenToClient")

	pGetModuleHandleW = kernel32.NewProc("GetModuleHandleW")
	pMoveFileExW      = kernel32.NewProc("MoveFileExW")
	pCreateActCtxW    = kernel32.NewProc("CreateActCtxW")
	pActivateActCtx   = kernel32.NewProc("ActivateActCtx")
	pDeactivateActCtx = kernel32.NewProc("DeactivateActCtx")
	pReleaseActCtx    = kernel32.NewProc("ReleaseActCtx")

	// These three procedures belong to GDI32. Do not move them to USER32.
	pSetTextColor     = gdi32.NewProc("SetTextColor")
	pSetBkColor       = gdi32.NewProc("SetBkColor")
	pCreateFontW      = gdi32.NewProc("CreateFontW")
	pSetBkMode        = gdi32.NewProc("SetBkMode")
	pTextOutW         = gdi32.NewProc("TextOutW")
	pCreateSolidBrush = gdi32.NewProc("CreateSolidBrush")
	pCreateHatchBrush = gdi32.NewProc("CreateHatchBrush")
	pCreatePen        = gdi32.NewProc("CreatePen")
	pSelectObject     = gdi32.NewProc("SelectObject")
	pDeleteObject     = gdi32.NewProc("DeleteObject")
	pGetStockObject   = gdi32.NewProc("GetStockObject")
	pRectangle        = gdi32.NewProc("Rectangle")
	pCreateBitmap     = gdi32.NewProc("CreateBitmap")
	pMoveToEx         = gdi32.NewProc("MoveToEx")
	pLineTo           = gdi32.NewProc("LineTo")
	pStretchDIBits    = gdi32.NewProc("StretchDIBits")

	pCoTaskMemFree    = ole32.NewProc("CoTaskMemFree")
	pCoInitializeEx   = ole32.NewProc("CoInitializeEx")
	pCoUninitialize   = ole32.NewProc("CoUninitialize")
	pCoCreateInstance = ole32.NewProc("CoCreateInstance")

	pDwmSetWindowAttribute = dwmapi.NewProc("DwmSetWindowAttribute")
	pSetWindowTheme        = uxtheme.NewProc("SetWindowTheme")
)

const (
	WS_OVERLAPPEDWINDOW       = 0x00CF0000
	WS_VISIBLE                = 0x10000000
	WS_CHILD                  = 0x40000000
	WS_POPUP                  = 0x80000000
	WS_BORDER                 = 0x00800000
	WS_VSCROLL                = 0x00200000
	WS_TABSTOP                = 0x00010000
	WS_GROUP                  = 0x00020000
	WS_CLIPCHILDREN           = 0x02000000
	BS_PUSHBUTTON             = 0
	BS_GROUPBOX               = 7
	BS_FLAT                   = 0x8000
	BS_BITMAP                 = 0x0080
	BS_PUSHLIKE               = 0x1000
	BS_AUTORADIOBUTTON        = 0x0009
	BS_AUTOCHECKBOX           = 0x0003
	BS_MULTILINE              = 0x00002000
	SW_HIDE                   = 0
	ES_MULTILINE              = 0x0004
	ES_AUTOVSCROLL            = 0x0040
	ES_WANTRETURN             = 0x1000
	CBS_DROPDOWN              = 0x0002
	CBS_DROPDOWNLIST          = 0x0003
	LBS_NOTIFY                = 0x0001
	LBS_NOINTEGRALHEIGHT      = 0x0100
	CS_DBLCLKS                = 0x0008
	WM_DESTROY                = 0x0002
	WM_SIZE                   = 0x0005
	WM_PAINT                  = 0x000F
	WM_ERASEBKGND             = 0x0014
	WM_COMMAND                = 0x0111
	WM_KEYDOWN                = 0x0100
	WM_CTLCOLOREDIT           = 0x0133
	WM_CTLCOLORLISTBOX        = 0x0134
	WM_CTLCOLORBTN            = 0x0135
	WM_CTLCOLORSTATIC         = 0x0138
	WM_SETFONT                = 0x0030
	WM_LBUTTONDOWN            = 0x0201
	WM_LBUTTONDBLCLK          = 0x0203
	WM_LBUTTONUP              = 0x0202
	WM_MOUSEMOVE              = 0x0200
	WM_MOUSELEAVE             = 0x02A3
	WM_RBUTTONDOWN            = 0x0204
	WM_RBUTTONUP              = 0x0205
	WM_MOUSEWHEEL             = 0x020A
	WM_CAPTURECHANGED         = 0x0215
	WM_HSCROLL                = 0x0114
	WM_VSCROLL                = 0x0115
	WM_CLOSE                  = 0x0010
	WM_SETICON                = 0x0080
	WM_SETCURSOR              = 0x0020
	BM_GETCHECK               = 0x00F0
	BM_SETCHECK               = 0x00F1
	BM_SETIMAGE               = 0x00F7
	BST_UNCHECKED             = 0
	BST_CHECKED               = 1
	SW_SHOW                   = 5
	IDC_ARROW                 = 32512
	IDC_HAND                  = 32649
	IDC_SIZEWE                = 32644
	IDC_SIZENS                = 32645
	IMAGE_BITMAP              = 0
	IMAGE_ICON                = 1
	LR_LOADFROMFILE           = 0x10
	LR_DEFAULTSIZE            = 0x40
	ICON_SMALL                = 0
	ICON_BIG                  = 1
	LB_ADDSTRING              = 0x0180
	LB_RESETCONTENT           = 0x0184
	LB_GETCURSEL              = 0x0188
	LB_SETCURSEL              = 0x0186
	CB_ADDSTRING              = 0x0143
	CB_GETCURSEL              = 0x0147
	CB_SETCURSEL              = 0x014E
	CB_RESETCONTENT           = 0x014B
	CB_SETITEMHEIGHT          = 0x0153
	TVM_SETBKCOLOR            = 0x111D
	TVM_SETTEXTCOLOR          = 0x111E
	TVM_SETLINECOLOR          = 0x1128
	LBN_SELCHANGE             = 1
	LBN_DBLCLK                = 2
	CBN_SELCHANGE             = 1
	EN_CHANGE                 = 0x0300
	BN_CLICKED                = 0
	BIF_RETURNONLYFSDIRS      = 1
	BIF_NEWDIALOGSTYLE        = 0x40
	BIF_EDITBOX               = 0x10
	MB_OK                     = 0
	MB_ICONERROR              = 0x10
	MB_ICONINFORMATION        = 0x40
	MB_YESNOCANCEL            = 0x00000003
	IDYES                     = 6
	IDNO                      = 7
	IDCANCEL                  = 2
	MF_STRING                 = 0x0000
	MF_GRAYED                 = 0x0001
	MF_SEPARATOR              = 0x0800
	MF_POPUP                  = 0x0010
	TPM_LEFTALIGN             = 0x0000
	TPM_TOPALIGN              = 0x0000
	TPM_RIGHTBUTTON           = 0x0002
	TPM_RETURNCMD             = 0x0100
	COINIT_APARTMENTTHREADED  = 2
	TRANSPARENT               = 1
	OPAQUE                    = 2
	PS_SOLID                  = 0
	HS_DIAGCROSS              = 5
	DEFAULT_GUI_FONT          = 17
	WS_HSCROLL                = 0x00100000
	DIB_RGB_COLORS            = 0
	SRCCOPY                   = 0x00CC0020
	BI_RGB                    = 0
	MOVEFILE_REPLACE_EXISTING = 0x00000001
	MOVEFILE_WRITE_THROUGH    = 0x00000008
	SIF_RANGE                 = 0x0001
	SIF_PAGE                  = 0x0002
	SIF_POS                   = 0x0004
	SIF_TRACKPOS              = 0x0010
	SIF_ALL                   = SIF_RANGE | SIF_PAGE | SIF_POS | SIF_TRACKPOS
	SB_LINEUP                 = 0
	SB_LINELEFT               = 0
	SB_LINEDOWN               = 1
	SB_LINERIGHT              = 1
	SB_PAGEUP                 = 2
	SB_PAGELEFT               = 2
	SB_PAGEDOWN               = 3
	SB_PAGERIGHT              = 3
	SB_THUMBPOSITION          = 4
	SB_THUMBTRACK             = 5
	TME_LEAVE                 = 0x00000002
	SB_TOP                    = 6
	SB_LEFT                   = 6
	SB_BOTTOM                 = 7
	SB_RIGHT                  = 7
	SB_ENDSCROLL              = 8
	SB_HORZ                   = 0
	SB_VERT                   = 1

	RDW_INVALIDATE  = 0x0001
	RDW_ALLCHILDREN = 0x0080
)

type WNDCLASSEX struct {
	cbSize, style               uint32
	lpfnWndProc                 uintptr
	cbClsExtra, cbWndExtra      int32
	hInstance, hIcon, hCursor   syscall.Handle
	hbrBackground               syscall.Handle
	lpszMenuName, lpszClassName *uint16
	hIconSm                     syscall.Handle
}

type POINT struct{ X, Y int32 }
type MSG struct {
	Hwnd           syscall.Handle
	Message        uint32
	WParam, LParam uintptr
	Time           uint32
	Pt             POINT
}
type TRACKMOUSEEVENT struct {
	CbSize      uint32
	DwFlags     uint32
	HwndTrack   syscall.Handle
	DwHoverTime uint32
}

type RECT struct{ Left, Top, Right, Bottom int32 }
type PAINTSTRUCT struct {
	hdc                  syscall.Handle
	fErase               int32
	rcPaint              RECT
	fRestore, fIncUpdate int32
	rgbReserved          [32]byte
}
type BITMAPINFOHEADER struct {
	BiSize          uint32
	BiWidth         int32
	BiHeight        int32
	BiPlanes        uint16
	BiBitCount      uint16
	BiCompression   uint32
	BiSizeImage     uint32
	BiXPelsPerMeter int32
	BiYPelsPerMeter int32
	BiClrUsed       uint32
	BiClrImportant  uint32
}

type BITMAPINFO struct {
	BmiHeader BITMAPINFOHEADER
	BmiColors [1]uint32
}

type SCROLLINFO struct {
	CbSize    uint32
	FMask     uint32
	NMin      int32
	NMax      int32
	NPage     uint32
	NPos      int32
	NTrackPos int32
}
type ACTCTXW struct {
	CbSize                 uint32
	DwFlags                uint32
	LpSource               *uint16
	WProcessorArchitecture uint16
	WLangID                uint16
	LpAssemblyDirectory    *uint16
	LpResourceName         *uint16
	LpApplicationName      *uint16
	HModule                syscall.Handle
}

type GUID struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

const (
	CLSCTX_INPROC_SERVER = 0x1
	FOS_PICKFOLDERS      = 0x00000020
	FOS_FORCEFILESYSTEM  = 0x00000040
	FOS_PATHMUSTEXIST    = 0x00000800
	FOS_NOCHANGEDIR      = 0x00000008
	SIGDN_FILESYSPATH    = 0x80058000
)

var (
	clsidFileOpenDialog = GUID{0xDC1C5A9C, 0xE88A, 0x4DDE, [8]byte{0xA5, 0xA1, 0x60, 0xF8, 0x2A, 0x20, 0xAE, 0xF7}}
	iidIFileOpenDialog  = GUID{0xD57C7288, 0xD4AD, 0x4768, [8]byte{0xBE, 0x02, 0x9D, 0x96, 0x95, 0x32, 0xD9, 0x60}}
)

var (
	appIconBig   syscall.Handle
	appIconSmall syscall.Handle
)

func wstr(s string) *uint16   { p, _ := syscall.UTF16PtrFromString(s); return p }
func loword(v uintptr) uint16 { return uint16(v & 0xffff) }
func hiword(v uintptr) uint16 { return uint16((v >> 16) & 0xffff) }

func createWindow(class, title string, style uint32, x, y, w, h int32, parent syscall.Handle, id uintptr, hInst syscall.Handle) syscall.Handle {
	r, _, _ := pCreateWindowExW.Call(0, uintptr(unsafe.Pointer(wstr(class))), uintptr(unsafe.Pointer(wstr(title))), uintptr(style), uintptr(x), uintptr(y), uintptr(w), uintptr(h), uintptr(parent), id, uintptr(hInst), 0)
	hwnd := syscall.Handle(r)
	if hwnd != 0 {
		// Tutti i controlli creati da PML Studio passano da qui: applicare il
		// font/visual style moderno in un solo punto impedisce regressioni verso
		// DEFAULT_GUI_FONT (aspetto Win9x/Windows 98).
		applyModernControlTheme(hwnd, class)
	}
	return hwnd
}

func setText(h syscall.Handle, s string) {
	if h == 0 {
		return
	}
	pSetWindowTextW.Call(uintptr(h), uintptr(unsafe.Pointer(wstr(s))))
}

func addList(h syscall.Handle, s string) {
	if h == 0 {
		return
	}
	pSendMessageW.Call(uintptr(h), LB_ADDSTRING, 0, uintptr(unsafe.Pointer(wstr(s))))
}

func clearList(h syscall.Handle) {
	if h != 0 {
		pSendMessageW.Call(uintptr(h), LB_RESETCONTENT, 0, 0)
	}
}

func appendMenu(menu syscall.Handle, flags uintptr, id uintptr, label string) {
	var p uintptr
	if label != "" {
		p = uintptr(unsafe.Pointer(wstr(label)))
	}
	pAppendMenuW.Call(uintptr(menu), flags, id, p)
}

func appendPopup(menu syscall.Handle, popup syscall.Handle, label string) {
	pAppendMenuW.Call(uintptr(menu), MF_POPUP|MF_STRING, uintptr(popup), uintptr(unsafe.Pointer(wstr(label))))
}

func getText(h syscall.Handle) string {
	if h == 0 {
		return ""
	}
	n, _, _ := pGetWindowTextLengthW.Call(uintptr(h))
	b := make([]uint16, n+1)
	pGetWindowTextW.Call(uintptr(h), uintptr(unsafe.Pointer(&b[0])), n+1)
	return syscall.UTF16ToString(b)
}

func msgbox(t, s string, f uintptr) {
	pMessageBoxW.Call(uintptr(hwndMain), uintptr(unsafe.Pointer(wstr(s))), uintptr(unsafe.Pointer(wstr(t))), f)
}

func msgboxResult(t, s string, f uintptr) int {
	r, _, _ := pMessageBoxW.Call(uintptr(hwndMain), uintptr(unsafe.Pointer(wstr(s))), uintptr(unsafe.Pointer(wstr(t))), f)
	return int(r)
}

func rgb(r, g, b byte) uintptr { return uintptr(r) | uintptr(g)<<8 | uintptr(b)<<16 }

func createSolidBrush(color uintptr) uintptr {
	br, _, _ := pCreateSolidBrush.Call(color)
	return br
}

func deleteGDIObject(obj uintptr) {
	if obj != 0 {
		pDeleteObject.Call(obj)
	}
}

func fillWithBrush(hdc syscall.Handle, r RECT, brush uintptr) {
	if hdc == 0 || brush == 0 {
		return
	}
	pFillRect.Call(uintptr(hdc), uintptr(unsafe.Pointer(&r)), brush)
}

func fill(hdc syscall.Handle, r RECT, color uintptr) {
	br := createSolidBrush(color)
	if br == 0 {
		return
	}
	fillWithBrush(hdc, r, br)
	deleteGDIObject(br)
}

func text(hdc syscall.Handle, x, y int32, s string, color uintptr) {
	if hdc == 0 || s == "" {
		return
	}
	pSetBkMode.Call(uintptr(hdc), TRANSPARENT)
	pSetTextColor.Call(uintptr(hdc), color)
	u := syscall.StringToUTF16(s)
	pTextOutW.Call(uintptr(hdc), uintptr(x), uintptr(y), uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)-1))
}

func invalidate(h syscall.Handle) {
	diagInvalidate()
	if h != 0 {
		pInvalidateRect.Call(uintptr(h), 0, 0)
	}
}

func embeddedIconValid(data []byte) bool {
	if len(data) < 6 {
		return false
	}
	return binary.LittleEndian.Uint16(data[0:2]) == 0 &&
		binary.LittleEndian.Uint16(data[2:4]) == 1 &&
		binary.LittleEndian.Uint16(data[4:6]) > 0
}

func writeEmbeddedIcon() string {
	d, err := assets.ReadFile("pokeball.ico")
	if err != nil || !embeddedIconValid(d) {
		return ""
	}

	p := filepath.Join(os.TempDir(), "PLM_Studio_Pokeball.ico")
	if err := os.WriteFile(p, d, 0600); err != nil {
		return ""
	}
	return p
}

func loadWindowIcons(hInst syscall.Handle) (syscall.Handle, syscall.Handle) {
	// Preferisci la risorsa PE incorporata da resource.syso: e la stessa icona
	// che Explorer mostra sul file EXE. Nessun file temporaneo e necessario.
	bigRes, _, _ := pLoadImageW.Call(uintptr(hInst), 1, IMAGE_ICON, 32, 32, 0)
	smallRes, _, _ := pLoadImageW.Call(uintptr(hInst), 1, IMAGE_ICON, 16, 16, 0)
	if bigRes != 0 || smallRes != 0 {
		return syscall.Handle(bigRes), syscall.Handle(smallRes)
	}

	// Fallback conservativo: usa il file .ico incorporato nel binario.
	p := writeEmbeddedIcon()
	if p == "" {
		return 0, 0
	}
	defer os.Remove(p)

	big, _, _ := pLoadImageW.Call(0, uintptr(unsafe.Pointer(wstr(p))), IMAGE_ICON, 32, 32, LR_LOADFROMFILE)
	small, _, _ := pLoadImageW.Call(0, uintptr(unsafe.Pointer(wstr(p))), IMAGE_ICON, 16, 16, LR_LOADFROMFILE)
	return syscall.Handle(big), syscall.Handle(small)
}

func setWindowIcon(h syscall.Handle) {
	if h == 0 {
		return
	}
	if appIconBig != 0 {
		pSendMessageW.Call(uintptr(h), WM_SETICON, ICON_BIG, uintptr(appIconBig))
	}
	if appIconSmall != 0 {
		pSendMessageW.Call(uintptr(h), WM_SETICON, ICON_SMALL, uintptr(appIconSmall))
	}
}

func releaseWindowIcons() {
	if appIconBig != 0 {
		pDestroyIcon.Call(uintptr(appIconBig))
		appIconBig = 0
	}
	if appIconSmall != 0 {
		pDestroyIcon.Call(uintptr(appIconSmall))
		appIconSmall = 0
	}
}
