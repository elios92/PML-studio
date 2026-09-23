//go:build windows && launcher

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"unsafe"
)

var (
	user32          = syscall.NewLazyDLL("user32.dll")
	procMessageBoxW = user32.NewProc("MessageBoxW")
)

const (
	MB_OK              = 0x00000000
	MB_ICONERROR       = 0x00000010
	MB_ICONINFORMATION = 0x00000040
	WM_CREATE          = 0x0001
	MF_STRING          = 0x00000000
	MF_POPUP           = 0x00000010
	MF_SEPARATOR       = 0x00000800
	CBS_HASSTRINGS     = 0x0200
	SS_LEFT            = 0x00000000
)

func wstr(s string) *uint16 {
	p, _ := syscall.UTF16PtrFromString(s)
	return p
}

func messageBox(title, message string, flags uintptr) {
	procMessageBoxW.Call(
		0,
		uintptr(unsafe.Pointer(wstr(message))),
		uintptr(unsafe.Pointer(wstr(title))),
		flags,
	)
}

func fatal(message string) {
	messageBox(
		"PLM Studio Launcher",
		message,
		MB_OK|MB_ICONERROR,
	)
	os.Exit(1)
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func sourceHash(root string) (string, error) {
	var files []string

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			if path == root {
				return nil
			}

			name := strings.ToLower(d.Name())

			switch name {
			case ".git", "bin", "dist", "build":
				return filepath.SkipDir
			}

			return nil
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}

		files = append(files, rel)
		return nil
	})

	if err != nil {
		return "", err
	}

	sort.Strings(files)

	hash := sha256.New()

	for _, rel := range files {
		hash.Write([]byte(filepath.ToSlash(rel)))
		hash.Write([]byte{0})

		data, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			return "", err
		}

		hash.Write(data)
		hash.Write([]byte{0})
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}

func checkSource(sourceDir string) error {
	required := []string{
		"main.go",
		"go.mod",
		"pokeball.ico",
	}

	for _, name := range required {
		path := filepath.Join(sourceDir, name)

		if !fileExists(path) {
			return fmt.Errorf(
				"file sorgente mancante:\n%s",
				path,
			)
		}
	}

	return nil
}

func checkGo() error {
	cmd := exec.Command("go", "version")

	if err := cmd.Run(); err != nil {
		return fmt.Errorf(
			"Go non risulta installato o non è disponibile nel PATH.\n\n" +
				"Installa Go e poi riavvia PLM Studio.",
		)
	}

	return nil
}

func buildRuntime(sourceDir, binDir, runtimePath string) error {
	if err := checkGo(); err != nil {
		return err
	}

	if err := os.MkdirAll(binDir, 0755); err != nil {
		return err
	}

	tempRuntime := runtimePath + ".new"
	oldRuntime := runtimePath + ".old"

	_ = os.Remove(tempRuntime)
	_ = os.Remove(oldRuntime)

	cmd := exec.Command(
		"go",
		"build",
		"-trimpath",
		"-ldflags",
		"-H=windowsgui",
		"-o",
		tempRuntime,
		".",
	)

	cmd.Dir = sourceDir

	cmd.Env = append(
		os.Environ(),
		"GOOS=windows",
		"GOARCH=amd64",
	)

	output, err := cmd.CombinedOutput()

	if err != nil {
		return fmt.Errorf(
			"errore durante la compilazione di PLM Studio:\n\n%s\n\n%s",
			err,
			string(output),
		)
	}

	if fileExists(runtimePath) {
		if err := os.Rename(runtimePath, oldRuntime); err != nil {
			_ = os.Remove(tempRuntime)

			return fmt.Errorf(
				"non riesco a sostituire il runtime precedente.\n\n"+
					"Chiudi PLM Studio e riprova.\n\n%s",
				err,
			)
		}
	}

	if err := os.Rename(tempRuntime, runtimePath); err != nil {
		if fileExists(oldRuntime) {
			_ = os.Rename(oldRuntime, runtimePath)
		}

		return fmt.Errorf(
			"impossibile installare il nuovo runtime:\n%s",
			err,
		)
	}

	_ = os.Remove(oldRuntime)

	return nil
}

func launchRuntime(runtimePath, workingDir string) error {
	args := []string{}

	if len(os.Args) > 1 {
		args = os.Args[1:]
	}

	logPath := filepath.Join(workingDir, "PLM_Studio_runtime.log")

	logFile, err := os.Create(logPath)
	if err != nil {
		return fmt.Errorf(
			"impossibile creare il log del runtime:\n%s",
			err,
		)
	}
	defer logFile.Close()

	cmd := exec.Command(runtimePath, args...)
	cmd.Dir = workingDir
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	err = cmd.Run()

	if err != nil {
		return fmt.Errorf(
			"PLM Studio si è chiuso per un errore.\n\n"+
				"Log creato in:\n%s\n\n"+
				"Errore:\n%s",
			logPath,
			err,
		)
	}

	return nil
}

func main() {
	exePath, err := os.Executable()
	if err != nil {
		fatal("Impossibile determinare la cartella di PLM Studio.")
	}

	rootDir := filepath.Dir(exePath)

	sourceDir := filepath.Join(rootDir, "source")
	binDir := filepath.Join(rootDir, "bin")

	runtimePath := filepath.Join(
		binDir,
		"PLM_Studio_runtime.exe",
	)

	hashPath := filepath.Join(
		binDir,
		".source_hash",
	)

	if err := checkSource(sourceDir); err != nil {
		fatal(err.Error())
	}

	currentHash, err := sourceHash(sourceDir)
	if err != nil {
		fatal(
			"Impossibile controllare i sorgenti:\n\n" +
				err.Error(),
		)
	}

	oldHash := ""

	if data, err := os.ReadFile(hashPath); err == nil {
		oldHash = strings.TrimSpace(string(data))
	}

	needsBuild :=
		!fileExists(runtimePath) ||
			currentHash != oldHash

	if needsBuild {
		if err := buildRuntime(
			sourceDir,
			binDir,
			runtimePath,
		); err != nil {
			fatal(err.Error())
		}

		if err := os.WriteFile(
			hashPath,
			[]byte(currentHash),
			0644,
		); err != nil {
			fatal(
				"PLM Studio è stato compilato, ma non riesco " +
					"a salvare lo stato dell'aggiornamento:\n\n" +
					err.Error(),
			)
		}
	}

	if err := launchRuntime(runtimePath, rootDir); err != nil {
		fatal(
			"Impossibile avviare PLM Studio:\n\n" +
				err.Error(),
		)
	}
}
