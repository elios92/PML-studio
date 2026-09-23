//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

// browseFolder opens the Windows Common Item Dialog in folder-selection mode.
// This is the Explorer-style picker used by Windows 10/11. The legacy
// SHBrowseForFolder API is intentionally not used anywhere in PLM Studio.
func browseFolder(title string) string {
	return browseFolderAt(title, "")
}

func browseFolderAt(title, initialFolder string) string {
	var dialog uintptr
	hr, _, _ := pCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsidFileOpenDialog)),
		0,
		CLSCTX_INPROC_SERVER,
		uintptr(unsafe.Pointer(&iidIFileOpenDialog)),
		uintptr(unsafe.Pointer(&dialog)),
	)
	if hresultFailed(hr) || dialog == 0 {
		msgbox("PLM Studio", "Windows non ha potuto aprire il selettore cartelle moderno.", MB_OK|MB_ICONERROR)
		return ""
	}
	defer comRelease(dialog)

	var options uint32
	if hr = comCall(dialog, 10, uintptr(unsafe.Pointer(&options))); hresultFailed(hr) {
		msgbox("PLM Studio", "Impossibile leggere le opzioni del selettore cartelle Windows.", MB_OK|MB_ICONERROR)
		return ""
	}

	options |= FOS_PICKFOLDERS | FOS_FORCEFILESYSTEM | FOS_PATHMUSTEXIST | FOS_NOCHANGEDIR
	if hr = comCall(dialog, 9, uintptr(options)); hresultFailed(hr) {
		msgbox("PLM Studio", "Impossibile attivare la selezione cartelle nel dialog Windows.", MB_OK|MB_ICONERROR)
		return ""
	}

	if title != "" {
		_ = comCall(dialog, 17, uintptr(unsafe.Pointer(wstr(title)))) // IFileDialog::SetTitle
	}
	_ = comCall(dialog, 18, uintptr(unsafe.Pointer(wstr("Seleziona cartella")))) // SetOkButtonLabel
	_ = comCall(dialog, 19, uintptr(unsafe.Pointer(wstr("Cartella:"))))          // SetFileNameLabel

	// IModalWindow::Show. ERROR_CANCELLED is expected when the user presses Cancel.
	setPickerFolder(dialog, initialFolder)
	hr = comCall(dialog, 3, uintptr(hwndMain))
	if hresultFailed(hr) {
		return ""
	}

	var item uintptr
	hr = comCall(dialog, 20, uintptr(unsafe.Pointer(&item))) // IFileDialog::GetResult
	if hresultFailed(hr) || item == 0 {
		return ""
	}
	defer comRelease(item)

	var pathPtr uintptr
	hr = comCall(item, 5, SIGDN_FILESYSPATH, uintptr(unsafe.Pointer(&pathPtr))) // IShellItem::GetDisplayName
	if hresultFailed(hr) || pathPtr == 0 {
		return ""
	}
	defer pCoTaskMemFree.Call(pathPtr)

	return utf16PtrToString(pathPtr)
}

func hresultFailed(hr uintptr) bool {
	return int32(uint32(hr)) < 0
}

func comCall(object uintptr, methodIndex uintptr, args ...uintptr) uintptr {
	if object == 0 {
		return uintptr(uint32(0x80004003)) // E_POINTER
	}
	vtable := *(*uintptr)(unsafe.Pointer(object))
	if vtable == 0 {
		return uintptr(uint32(0x80004003))
	}
	method := *(*uintptr)(unsafe.Pointer(vtable + methodIndex*unsafe.Sizeof(uintptr(0))))
	if method == 0 {
		return uintptr(uint32(0x80004001)) // E_NOTIMPL
	}
	argv := make([]uintptr, 0, len(args)+1)
	argv = append(argv, object)
	argv = append(argv, args...)
	r1, _, _ := syscall.SyscallN(method, argv...)
	return r1
}

func comRelease(object uintptr) {
	if object != 0 {
		_ = comCall(object, 2)
	}
}

func utf16PtrToString(ptr uintptr) string {
	if ptr == 0 {
		return ""
	}
	p := (*uint16)(unsafe.Pointer(ptr))
	buf := make([]uint16, 0, 260)
	for i := uintptr(0); ; i++ {
		ch := *(*uint16)(unsafe.Pointer(uintptr(unsafe.Pointer(p)) + i*2))
		if ch == 0 {
			break
		}
		buf = append(buf, ch)
	}
	return syscall.UTF16ToString(buf)
}

type comdlgFilterSpec struct {
	Name *uint16
	Spec *uint16
}

// browseProjectMain opens the modern Windows file picker and deliberately asks
// for the converted project's main.py. The selected file becomes the single
// anchor from which PLM Studio derives converted/, assets/Graphics/ and every
// other project resource path.
func browseProjectMain(title string) string {
	var dialog uintptr
	hr, _, _ := pCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsidFileOpenDialog)),
		0,
		CLSCTX_INPROC_SERVER,
		uintptr(unsafe.Pointer(&iidIFileOpenDialog)),
		uintptr(unsafe.Pointer(&dialog)),
	)
	if hresultFailed(hr) || dialog == 0 {
		msgbox("PLM Studio", "Windows non ha potuto aprire il selettore file moderno.", MB_OK|MB_ICONERROR)
		return ""
	}
	defer comRelease(dialog)

	filters := []comdlgFilterSpec{
		{Name: wstr("Progetto PLM convertito (main.py)"), Spec: wstr("main.py")},
		{Name: wstr("File Python (*.py)"), Spec: wstr("*.py")},
	}
	_ = comCall(dialog, 4, uintptr(len(filters)), uintptr(unsafe.Pointer(&filters[0]))) // SetFileTypes
	_ = comCall(dialog, 5, 1)                                                           // SetFileTypeIndex

	var options uint32
	if hr = comCall(dialog, 10, uintptr(unsafe.Pointer(&options))); hresultFailed(hr) {
		return ""
	}
	// File picker, not folder picker. This is important: main.py is the project
	// identity for already-converted PLM/Python projects.
	options &^= FOS_PICKFOLDERS
	options |= FOS_FORCEFILESYSTEM | FOS_PATHMUSTEXIST | FOS_NOCHANGEDIR
	if hr = comCall(dialog, 9, uintptr(options)); hresultFailed(hr) {
		return ""
	}

	if title != "" {
		_ = comCall(dialog, 17, uintptr(unsafe.Pointer(wstr(title))))
	}
	setProjectPickerDefaultFolder(dialog)
	_ = comCall(dialog, 15, uintptr(unsafe.Pointer(wstr("main.py"))))                   // SetFileName
	_ = comCall(dialog, 18, uintptr(unsafe.Pointer(wstr("Apri progetto"))))             // SetOkButtonLabel
	_ = comCall(dialog, 19, uintptr(unsafe.Pointer(wstr("File principale progetto:")))) // SetFileNameLabel

	hr = comCall(dialog, 3, uintptr(hwndMain))
	if hresultFailed(hr) {
		return ""
	}

	var item uintptr
	hr = comCall(dialog, 20, uintptr(unsafe.Pointer(&item)))
	if hresultFailed(hr) || item == 0 {
		return ""
	}
	defer comRelease(item)

	var pathPtr uintptr
	hr = comCall(item, 5, SIGDN_FILESYSPATH, uintptr(unsafe.Pointer(&pathPtr)))
	if hresultFailed(hr) || pathPtr == 0 {
		return ""
	}
	defer pCoTaskMemFree.Call(pathPtr)

	return utf16PtrToString(pathPtr)
}

// browsePalettePNG opens the Windows file picker for a single numbered PNG
// palette. Filename validation is performed by palette_manager.go after the
// selection so the dialog can still show the user why a file was rejected.
func browsePalettePNG(title string) string {
	var dialog uintptr
	hr, _, _ := pCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsidFileOpenDialog)),
		0,
		CLSCTX_INPROC_SERVER,
		uintptr(unsafe.Pointer(&iidIFileOpenDialog)),
		uintptr(unsafe.Pointer(&dialog)),
	)
	if hresultFailed(hr) || dialog == 0 {
		msgbox("PML Studio", "Windows non ha potuto aprire il selettore PNG.", MB_OK|MB_ICONERROR)
		return ""
	}
	defer comRelease(dialog)

	filters := []comdlgFilterSpec{
		{Name: wstr("Palette PNG (*.png)"), Spec: wstr("*.png")},
		{Name: wstr("Immagini PNG (*.png)"), Spec: wstr("*.png")},
	}
	_ = comCall(dialog, 4, uintptr(len(filters)), uintptr(unsafe.Pointer(&filters[0])))
	_ = comCall(dialog, 5, 1)

	var options uint32
	if hr = comCall(dialog, 10, uintptr(unsafe.Pointer(&options))); hresultFailed(hr) {
		return ""
	}
	options &^= FOS_PICKFOLDERS
	options |= FOS_FORCEFILESYSTEM | FOS_PATHMUSTEXIST | FOS_NOCHANGEDIR
	if hr = comCall(dialog, 9, uintptr(options)); hresultFailed(hr) {
		return ""
	}
	if title != "" {
		_ = comCall(dialog, 17, uintptr(unsafe.Pointer(wstr(title))))
	}
	_ = comCall(dialog, 18, uintptr(unsafe.Pointer(wstr("Inserisci palette"))))
	_ = comCall(dialog, 19, uintptr(unsafe.Pointer(wstr("Nome richiesto: pallette <numero>.png"))))

	hr = comCall(dialog, 3, uintptr(hwndMain))
	if hresultFailed(hr) {
		return ""
	}
	var item uintptr
	hr = comCall(dialog, 20, uintptr(unsafe.Pointer(&item)))
	if hresultFailed(hr) || item == 0 {
		return ""
	}
	defer comRelease(item)
	var pathPtr uintptr
	hr = comCall(item, 5, SIGDN_FILESYSPATH, uintptr(unsafe.Pointer(&pathPtr)))
	if hresultFailed(hr) || pathPtr == 0 {
		return ""
	}
	defer pCoTaskMemFree.Call(pathPtr)
	return utf16PtrToString(pathPtr)
}

// browseCharacterPNG opens the modern Windows file picker for a character PNG
// that will be imported into the project's Graphics/Characters directory.
func browseCharacterPNG(title string, owner syscall.Handle) string {
	var dialog uintptr
	hr, _, _ := pCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsidFileOpenDialog)),
		0,
		CLSCTX_INPROC_SERVER,
		uintptr(unsafe.Pointer(&iidIFileOpenDialog)),
		uintptr(unsafe.Pointer(&dialog)),
	)
	if hresultFailed(hr) || dialog == 0 {
		msgbox("PML Studio", "Windows non ha potuto aprire il selettore PNG.", MB_OK|MB_ICONERROR)
		return ""
	}
	defer comRelease(dialog)
	filters := []comdlgFilterSpec{
		{Name: wstr("Sprite Characters PNG (*.png)"), Spec: wstr("*.png")},
		{Name: wstr("Immagini PNG (*.png)"), Spec: wstr("*.png")},
	}
	_ = comCall(dialog, 4, uintptr(len(filters)), uintptr(unsafe.Pointer(&filters[0])))
	_ = comCall(dialog, 5, 1)
	var options uint32
	if hr = comCall(dialog, 10, uintptr(unsafe.Pointer(&options))); hresultFailed(hr) {
		return ""
	}
	options &^= FOS_PICKFOLDERS
	options |= FOS_FORCEFILESYSTEM | FOS_PATHMUSTEXIST | FOS_NOCHANGEDIR
	if hr = comCall(dialog, 9, uintptr(options)); hresultFailed(hr) {
		return ""
	}
	if title != "" {
		_ = comCall(dialog, 17, uintptr(unsafe.Pointer(wstr(title))))
	}
	_ = comCall(dialog, 18, uintptr(unsafe.Pointer(wstr("Importa sprite"))))
	_ = comCall(dialog, 19, uintptr(unsafe.Pointer(wstr("File PNG Characters:"))))
	showOwner := owner
	if showOwner == 0 {
		showOwner = hwndMain
	}
	hr = comCall(dialog, 3, uintptr(showOwner))
	if hresultFailed(hr) {
		return ""
	}
	var item uintptr
	hr = comCall(dialog, 20, uintptr(unsafe.Pointer(&item)))
	if hresultFailed(hr) || item == 0 {
		return ""
	}
	defer comRelease(item)
	var pathPtr uintptr
	hr = comCall(item, 5, SIGDN_FILESYSPATH, uintptr(unsafe.Pointer(&pathPtr)))
	if hresultFailed(hr) || pathPtr == 0 {
		return ""
	}
	defer pCoTaskMemFree.Call(pathPtr)
	return utf16PtrToString(pathPtr)
}

func setProjectPickerDefaultFolder(dialog uintptr) {
	setPickerFolder(dialog, settings.ProjectsPath)
}

func setPickerFolder(dialog uintptr, folder string) {
	if folder == "" {
		return
	}
	var item uintptr
	iid := GUID{0x43826d1e, 0xe718, 0x42ee, [8]byte{0xbc, 0x55, 0xa1, 0xe2, 0x61, 0xc3, 0x7b, 0xfe}}
	hr, _, _ := syscall.NewLazyDLL("shell32.dll").NewProc("SHCreateItemFromParsingName").Call(uintptr(unsafe.Pointer(wstr(folder))), 0, uintptr(unsafe.Pointer(&iid)), uintptr(unsafe.Pointer(&item)))
	if hresultFailed(hr) || item == 0 {
		return
	}
	defer comRelease(item)
	_ = comCall(dialog, 12, item) // IFileDialog.SetFolder
}
