//go:build windows

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

var procSetDllDirectoryW = kernel32.NewProc("SetDllDirectoryW")

func utf16Ptr(s string) *uint16 {
	p, _ := syscall.UTF16PtrFromString(s)
	return p
}

func failGameLauncher(message string) {
	pMessageBoxW.Call(
		0,
		uintptr(unsafe.Pointer(utf16Ptr(message))),
		uintptr(unsafe.Pointer(utf16Ptr("PML Runtime"))),
		0x10,
	)
	os.Exit(1)
}

func runEmbeddedGameHost() bool {
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	root := filepath.Dir(exe)
	manifestData, manifestErr := os.ReadFile(filepath.Join(root, "converted", "runtime_install.json"))
	if manifestErr != nil {
		return false
	}
	var manifest runtimeInstallManifest
	if json.Unmarshal(manifestData, &manifest) != nil {
		return false
	}
	self := filepath.Base(exe)
	if !strings.EqualFold(self, manifest.ReleaseEXE) && !strings.EqualFold(self, manifest.DebugEXE) {
		return false
	}
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
	// argsJSON is JSON, which is also valid Python list syntax. The source fed
	// to PyRun_SimpleString must contain REAL newline bytes: "\\n" here would
	// reach Python as a backslash followed by 'n' and trigger
	// "unexpected character after line continuation character".
	script := fmt.Sprintf("import sys\nsys.argv = %s\n", string(argsJSON))
	// Always wrap the Python entry point so GUI builds preserve the complete
	// traceback in runtime_error.log. Without a console, PyRun_SimpleString would
	// otherwise print the useful exception to nowhere and only the generic host
	// dialog would remain.
	body := ""
	if os.Getenv("PLM_MAP_INDEX") != "" {
		body = playtestMapResolverBootstrap()
	} else {
		body = fmt.Sprintf(
			"import os, runpy\n"+
				"os.chdir(%q)\n"+
				"runpy.run_path(%q, run_name='__main__')\n",
			filepath.ToSlash(root), filepath.ToSlash(mainPy),
		)
	}
	script += "import traceback\ntry:\n"
	for _, line := range strings.Split(body, "\n") {
		if line != "" {
			script += "    " + line + "\n"
		}
	}
	script += "except BaseException:\n" +
		"    _plm_tb = traceback.format_exc()\n" +
		"    open('runtime_error.log', 'w', encoding='utf-8').write(_plm_tb)\n" +
		"    raise\n"
	cscript := append([]byte(script), 0)
	r, _, _ := run.Call(uintptr(unsafe.Pointer(&cscript[0])))
	// PyRun_SimpleString returns -1 when Python raised an exception. Do not
	// immediately finalize the interpreter: Py_FinalizeEx can itself report a
	// non-zero status unrelated to a successful game shutdown and previously
	// produced a misleading generic error after the window had already run.
	if int32(r) != 0 {
		detail := ""
		if b, readErr := os.ReadFile(filepath.Join(root, "runtime_error.log")); readErr == nil {
			lines := strings.Split(strings.TrimSpace(string(b)), "\n")
			if len(lines) > 8 {
				lines = lines[len(lines)-8:]
			}
			detail = "\n\n" + strings.Join(lines, "\n")
		}
		failGameLauncher("Il runtime Python del progetto ha generato un errore." + detail)
	}
	finalize.Call()
	return true
}
