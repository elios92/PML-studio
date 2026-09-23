//go:build windows

package main

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	updateRepository = "elios92/PML-studio"
	updateAPIURL     = "https://api.github.com/repos/" + updateRepository + "/releases/latest"
	updateExeAsset   = "PML.Studio.exe"
	updateHashAsset  = "PML.Studio.exe.sha256"
	wmAppUpdateResult = 0x8004
)

//go:embed version.txt
var embeddedVersion string

type githubRelease struct {
	TagName string `json:"tag_name"`
	Name    string `json:"name"`
	Body    string `json:"body"`
	HTMLURL string `json:"html_url"`
	Assets  []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

type updateResult struct {
	phase   string
	manual  bool
	release githubRelease
	err     error
	staged  string
}

var (
	updateMu     sync.Mutex
	updateBusy   bool
	updateLatest updateResult
	updateClient = &http.Client{Timeout: 30 * time.Second}
)

func currentAppVersion() string {
	v := strings.TrimSpace(embeddedVersion)
	if v == "" {
		return "0.0.0"
	}
	return strings.TrimPrefix(strings.ToLower(v), "v")
}

func parseVersion(v string) ([]int, bool) {
	v = strings.TrimSpace(strings.TrimPrefix(strings.ToLower(v), "v"))
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	parts := strings.Split(v, ".")
	if len(parts) == 0 || len(parts) > 4 {
		return nil, false
	}
	out := make([]int, len(parts))
	for i, p := range parts {
		if p == "" {
			return nil, false
		}
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return nil, false
		}
		out[i] = n
	}
	return out, true
}

func versionNewer(remote, local string) bool {
	r, rok := parseVersion(remote)
	l, lok := parseVersion(local)
	if !rok || !lok {
		return false
	}
	n := len(r)
	if len(l) > n {
		n = len(l)
	}
	for i := 0; i < n; i++ {
		var rv, lv int
		if i < len(r) { rv = r[i] }
		if i < len(l) { lv = l[i] }
		if rv != lv {
			return rv > lv
		}
	}
	return false
}

func fetchLatestRelease() (githubRelease, error) {
	req, err := http.NewRequest(http.MethodGet, updateAPIURL, nil)
	if err != nil {
		return githubRelease{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "PML-Studio-Updater/"+currentAppVersion())
	resp, err := updateClient.Do(req)
	if err != nil {
		return githubRelease{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return githubRelease{}, errors.New("nessuna release pubblicata")
	}
	if resp.StatusCode != http.StatusOK {
		return githubRelease{}, fmt.Errorf("GitHub Releases: HTTP %d", resp.StatusCode)
	}
	var rel githubRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&rel); err != nil {
		return githubRelease{}, fmt.Errorf("risposta release non valida: %w", err)
	}
	if rel.TagName == "" {
		return githubRelease{}, errors.New("release senza versione")
	}
	return rel, nil
}

func beginUpdateCheck(manual bool) {
	updateMu.Lock()
	if updateBusy {
		updateMu.Unlock()
		if manual {
			msgbox("Aggiornamenti", "Un controllo aggiornamenti e gia in corso.", MB_OK|MB_ICONINFORMATION)
		}
		return
	}
	updateBusy = true
	updateMu.Unlock()

	if manual {
		setToolbarStatus("Controllo aggiornamenti in corso...")
	}
	go func() {
		rel, err := fetchLatestRelease()
		updateMu.Lock()
		updateLatest = updateResult{phase: "check", manual: manual, release: rel, err: err}
		updateBusy = false
		updateMu.Unlock()
		pPostMessageW.Call(uintptr(hwndMain), wmAppUpdateResult, 0, 0)
	}()
}

func beginAutomaticUpdateCheck() { beginUpdateCheck(false) }

func releaseAssetURL(rel githubRelease, name string) string {
	for _, a := range rel.Assets {
		if a.Name == name {
			return a.URL
		}
	}
	return ""
}

func downloadURL(url string, maxBytes int64) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil { return nil, err }
	req.Header.Set("User-Agent", "PML-Studio-Updater/"+currentAppVersion())
	resp, err := updateClient.Do(req)
	if err != nil { return nil, err }
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download HTTP %d", resp.StatusCode)
	}
	r := io.LimitReader(resp.Body, maxBytes+1)
	b, err := io.ReadAll(r)
	if err != nil { return nil, err }
	if int64(len(b)) > maxBytes {
		return nil, errors.New("file aggiornamento oltre il limite consentito")
	}
	return b, nil
}

func expectedSHA256(rel githubRelease) (string, error) {
	u := releaseAssetURL(rel, updateHashAsset)
	if u == "" {
		return "", fmt.Errorf("asset obbligatorio %q assente dalla release", updateHashAsset)
	}
	b, err := downloadURL(u, 64*1024)
	if err != nil { return "", err }
	fields := strings.Fields(string(b))
	if len(fields) == 0 || len(fields[0]) != 64 {
		return "", errors.New("file SHA-256 non valido")
	}
	if _, err := hex.DecodeString(fields[0]); err != nil {
		return "", errors.New("SHA-256 non valido")
	}
	return strings.ToLower(fields[0]), nil
}

func beginUpdateDownload(rel githubRelease) {
	updateMu.Lock()
	if updateBusy {
		updateMu.Unlock()
		return
	}
	updateBusy = true
	updateMu.Unlock()
	setToolbarStatus("Download aggiornamento " + rel.TagName + "...")

	go func() {
		var staged string
		var err error
		exeURL := releaseAssetURL(rel, updateExeAsset)
		if exeURL == "" {
			err = fmt.Errorf("asset obbligatorio %q assente dalla release", updateExeAsset)
		} else {
			var expected string
			expected, err = expectedSHA256(rel)
			if err == nil {
				var data []byte
				data, err = downloadURL(exeURL, 256*1024*1024)
				if err == nil {
					sum := sha256.Sum256(data)
					actual := hex.EncodeToString(sum[:])
					if actual != expected {
						err = fmt.Errorf("SHA-256 non corrisponde: download rifiutato")
					} else {
						var current string
						current, err = os.Executable()
						if err == nil {
							staged = current + ".update-new"
							err = os.WriteFile(staged, data, 0755)
						}
					}
				}
			}
		}
		updateMu.Lock()
		updateLatest = updateResult{phase: "download", manual: true, release: rel, err: err, staged: staged}
		updateBusy = false
		updateMu.Unlock()
		pPostMessageW.Call(uintptr(hwndMain), wmAppUpdateResult, 0, 0)
	}()
}

func handleUpdateResult() {
	updateMu.Lock()
	r := updateLatest
	updateLatest = updateResult{}
	updateMu.Unlock()

	if r.err != nil {
		if r.manual || r.phase == "download" {
			msgbox("Aggiornamenti", "Aggiornamento non completato:\r\n\r\n"+r.err.Error(), MB_OK|MB_ICONERROR)
			setToolbarStatus("Aggiornamenti: " + r.err.Error())
		}
		return
	}
	if r.phase == "check" {
		if !versionNewer(r.release.TagName, currentAppVersion()) {
			if r.manual {
				msgbox("Aggiornamenti", "PML Studio e aggiornato.\r\n\r\nVersione installata: "+currentAppVersion()+"\r\nUltima release: "+r.release.TagName, MB_OK|MB_ICONINFORMATION)
				setToolbarStatus("PML Studio e aggiornato.")
			}
			return
		}
		notes := strings.TrimSpace(r.release.Body)
		if len(notes) > 900 { notes = notes[:900] + "..." }
		text := "Nuovo aggiornamento disponibile.\r\n\r\nInstallata: " + currentAppVersion() + "\r\nDisponibile: " + r.release.TagName
		if notes != "" { text += "\r\n\r\nNovita:\r\n" + notes }
		text += "\r\n\r\nScaricare e installare adesso?"
		if msgboxResult("Aggiornamento PML Studio", text, MB_YESNO|MB_ICONINFORMATION) == IDYES {
			beginUpdateDownload(r.release)
		}
		return
	}
	if r.phase == "download" {
		if !confirmUnsavedProjectChanges("installare l'aggiornamento") {
			_ = os.Remove(r.staged)
			setToolbarStatus("Aggiornamento annullato.")
			return
		}
		if err := launchUpdateHelper(r.staged); err != nil {
			_ = os.Remove(r.staged)
			msgbox("Aggiornamenti", "Impossibile avviare l'installazione:\r\n\r\n"+err.Error(), MB_OK|MB_ICONERROR)
			return
		}
		pDestroyWindow.Call(uintptr(hwndMain))
	}
}

func launchUpdateHelper(staged string) error {
	target, err := os.Executable()
	if err != nil { return err }
	cmd := exec.Command(staged, "--pml-apply-update", target, strconv.Itoa(os.Getpid()))
	cmd.Dir = filepath.Dir(target)
	return cmd.Start()
}

func runUpdateHelperFromArgs() bool {
	if len(os.Args) != 4 || os.Args[1] != "--pml-apply-update" {
		return false
	}
	target := os.Args[2]
	pid64, err := strconv.ParseUint(os.Args[3], 10, 32)
	if err != nil { return true }
	waitForProcessExit(uint32(pid64), 30*time.Second)
	backup := target + ".update-backup"
	_ = os.Remove(backup)
	if err := os.Rename(target, backup); err != nil {
		return true
	}
	self, err := os.Executable()
	if err != nil {
		_ = os.Rename(backup, target)
		return true
	}
	if err = copyFileSync(self, target); err != nil {
		_ = os.Remove(target)
		_ = os.Rename(backup, target)
		return true
	}
	cmd := exec.Command(target)
	cmd.Dir = filepath.Dir(target)
	if err = cmd.Start(); err != nil {
		_ = os.Remove(target)
		_ = os.Rename(backup, target)
		return true
	}
	return true
}

func waitForProcessExit(pid uint32, timeout time.Duration) {
	const synchronize = 0x00100000
	k := syscall.NewLazyDLL("kernel32.dll")
	openProcess := k.NewProc("OpenProcess")
	wait := k.NewProc("WaitForSingleObject")
	closeHandle := k.NewProc("CloseHandle")
	h, _, _ := openProcess.Call(synchronize, 0, uintptr(pid))
	if h == 0 {
		time.Sleep(500 * time.Millisecond)
		return
	}
	defer closeHandle.Call(h)
	ms := uintptr(timeout / time.Millisecond)
	wait.Call(h, ms)
}

func copyFileSync(src, dst string) error {
	in, err := os.Open(src)
	if err != nil { return err }
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil { return err }
	_, copyErr := io.Copy(out, in)
	syncErr := out.Sync()
	closeErr := out.Close()
	if copyErr != nil { return copyErr }
	if syncErr != nil { return syncErr }
	return closeErr
}
