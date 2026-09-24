//go:build windows && game_launcher

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

var (
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	user32Game = syscall.NewLazyDLL("user32.dll")
	procSetDllDirectoryW = kernel32.NewProc("SetDllDirectoryW")
	procMessageBoxGameW = user32Game.NewProc("MessageBoxW")
)

func utf16Ptr(s string) *uint16 {
	p, _ := syscall.UTF16PtrFromString(s)
	return p
}

func failGameLauncher(message string) {
	procMessageBoxGameW.Call(
		0,
		uintptr(unsafe.Pointer(utf16Ptr(message))),
		uintptr(unsafe.Pointer(utf16Ptr("PML Runtime"))),
		0x10,
	)
	os.Exit(1)
}

func main() {
	exe, err := os.Executable()
	if err != nil {
		failGameLauncher("Impossibile determinare il percorso del gioco.")
	}
	root := filepath.Dir(exe)
	if err := os.Chdir(root); err != nil {
		failGameLauncher("Impossibile aprire la cartella del gioco.\n\n" + err.Error())
	}

	mainPy := filepath.Join(root, "main.py")
	pythonDLL := filepath.Join(root, "python311.dll")
	if st, err := os.Stat(mainPy); err != nil || st.IsDir() || st.Size() == 0 {
		failGameLauncher("Runtime PML incompleto: main.py mancante.")
	}
	if st, err := os.Stat(pythonDLL); err != nil || st.IsDir() || st.Size() == 0 {
		failGameLauncher("Runtime PML incompleto: python311.dll mancante.")
	}

	// Host CPython inside the game EXE. There is deliberately no pythonw.exe
	// child process: the project EXE is the Windows process that owns the game.
	_ = os.Setenv("PYTHONHOME", root)
	_ = os.Setenv("PYTHONUTF8", "1")
	_ = os.Setenv("PYTHONPATH", strings.Join([]string{
		filepath.Join(root, "Lib"),
		filepath.Join(root, "Lib", "site-packages"),
		root,
	}, string(os.PathListSeparator)))
	if strings.Contains(strings.ToUpper(filepath.Base(exe)), "DEBUG") {
		_ = os.Setenv("PLM_DEBUG", "1")
	}

	procSetDllDirectoryW.Call(uintptr(unsafe.Pointer(utf16Ptr(root))))
	py := syscall.NewLazyDLL(pythonDLL)
	initialize := py.NewProc("Py_Initialize")
	run := py.NewProc("PyRun_SimpleString")
	finalize := py.NewProc("Py_FinalizeEx")
	if err := py.Load(); err != nil {
		failGameLauncher("Impossibile caricare python311.dll.\n\n" + err.Error())
	}

	initialize.Call()

	args := append([]string{mainPy}, os.Args[1:]...)
	if strings.Contains(strings.ToUpper(filepath.Base(exe)), "DEBUG") {
		found := false
		for _, a := range args[1:] {
			if a == "--debug" {
				found = true
				break
			}
		}
		if !found {
			args = append(args, "--debug")
		}
	}
	argsJSON, _ := json.Marshal(args)
	script := fmt.Sprintf(
		"import os, sys, runpy\n"+
			"sys.argv = %s\n"+
			"os.chdir(%q)\n"+
			"runpy.run_path(%q, run_name='__main__')\n",
		string(argsJSON), filepath.ToSlash(root), filepath.ToSlash(mainPy),
	)
	cscript := append([]byte(script), 0)
	r, _, _ := run.Call(uintptr(unsafe.Pointer(&cscript[0])))
	exitCode, _, _ := finalize.Call()
	if r != 0 || exitCode != 0 {
		failGameLauncher("Il runtime Python del progetto si e' chiuso con un errore.")
	}
}
