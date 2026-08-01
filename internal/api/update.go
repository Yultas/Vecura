package api

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"vecura/internal/update"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// CurrentVersion is replaced by the release build with the git tag version.
var CurrentVersion = "dev"

const updateCheckInterval = 24 * time.Hour

type UpdateInfo struct {
	Available bool   `json:"available"`
	Current   string `json:"current"`
	Version   string `json:"version"`
	URL       string `json:"url"`
	SHA256    string `json:"sha256"`
	Signature string `json:"signature"`
	Notes     string `json:"notes"`
}

func (a *App) CheckForUpdates(force bool) (*UpdateInfo, error) {
	result := &UpdateInfo{Current: CurrentVersion}
	if CurrentVersion == "dev" && !force {
		return result, nil
	}
	if !force {
		cfg := a.loadConfig()
		if cfg.LastUpdateCheck > 0 && time.Since(time.Unix(cfg.LastUpdateCheck, 0)) < updateCheckInterval {
			result.Current = CurrentVersion
			return result, nil
		}
	}
	manifest, err := update.FetchManifest()
	if err != nil {
		return nil, err
	}
	if err := update.VerifyManifest(manifest); err != nil {
		return nil, err
	}
	a.cfgMu.Lock()
	cfg := a.loadConfig()
	cfg.LastUpdateCheck = time.Now().UTC().Unix()
	a.saveConfig(cfg)
	a.cfgMu.Unlock()
	result.Version = manifest.Version
	result.URL = manifest.URL
	result.SHA256 = manifest.SHA256
	result.Signature = manifest.Signature
	result.Notes = manifest.Notes
	result.Available = compareVersions(manifest.Version, CurrentVersion) > 0
	return result, nil
}

func (a *App) DownloadUpdate(info UpdateInfo) error {
	if !info.Available || info.URL == "" {
		return fmt.Errorf("no update is available")
	}
	manifest := update.Manifest{
		Version: info.Version, URL: info.URL, SHA256: info.SHA256,
		Signature: info.Signature, Notes: info.Notes,
	}
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find application: %w", err)
	}
	updater := filepath.Join(filepath.Dir(exe), "updater.exe")
	if _, err := os.Stat(updater); err != nil {
		return fmt.Errorf("updater.exe is not installed next to the application")
	}
	updateDir := filepath.Join(os.TempDir(), "Vecura-updates")
	source, err := update.DownloadAndVerify(manifest, updateDir)
	if err != nil {
		return err
	}
	cmd := exec.Command(updater, "--pid", strconv.Itoa(os.Getpid()), "--source", source, "--target", exe)
	cmd.Dir = filepath.Dir(updater)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start updater: %w", err)
	}
	runtime.Quit(a.ctx)
	return nil
}

func compareVersions(a, b string) int {
	parse := func(v string) []int {
		v = strings.TrimPrefix(strings.TrimSpace(v), "v")
		parts := strings.SplitN(v, "+", 2)[0]
		parts = strings.SplitN(parts, "-", 2)[0]
		out := make([]int, 3)
		for i, p := range strings.Split(parts, ".") {
			if i >= len(out) {
				break
			}
			out[i], _ = strconv.Atoi(p)
		}
		return out
	}
	av, bv := parse(a), parse(b)
	for i := range av {
		if av[i] > bv[i] {
			return 1
		}
		if av[i] < bv[i] {
			return -1
		}
	}
	return 0
}
