package llama

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
)

const (
	releasesURL = "https://api.github.com/repos/ggml-org/llama.cpp/releases/latest"
)

// RuntimeInfo describes the state of the local llama.cpp installation.
type RuntimeInfo struct {
	Installed bool   `json:"installed"`
	Version   string `json:"version"`
	Backend   string `json:"backend"` // "cuda-12.4", "cuda-13.3", "vulkan", "cpu"
	Path      string `json:"path"`
}

// RuntimeManager manages the llama.cpp runtime download and installation.
type RuntimeManager struct {
	baseDir string // ~/.vecura/llama
	Logf    func(string, ...interface{})
}

// NewRuntimeManager creates a manager rooted at baseDir.
func NewRuntimeManager(baseDir string) *RuntimeManager {
	return &RuntimeManager{baseDir: baseDir, Logf: func(f string, a ...interface{}) { log.Printf(f, a...) }}
}

// Status returns the current runtime installation state.
func (rm *RuntimeManager) Status() RuntimeInfo {
	meta := rm.loadMeta()
	serverPath := rm.ServerPath()
	if serverPath == "" {
		return RuntimeInfo{Installed: false}
	}
	return RuntimeInfo{
		Installed: true,
		Version:   meta.Version,
		Backend:   meta.Backend,
		Path:      serverPath,
	}
}

// ServerPath returns the absolute path to llama-server.exe, or "" if not found.
func (rm *RuntimeManager) ServerPath() string {
	meta := rm.loadMeta()
	if meta.Backend == "" {
		return ""
	}
	binDir := filepath.Join(rm.baseDir, "bin", meta.Backend)
	exe := filepath.Join(binDir, "llama-server.exe")
	if _, err := os.Stat(exe); err != nil {
		return ""
	}
	return exe
}

// BinDir returns the directory containing the llama-server binary for the
// current backend. Needed so llama-server can find adjacent DLLs.
func (rm *RuntimeManager) BinDir() string {
	meta := rm.loadMeta()
	if meta.Backend == "" {
		return ""
	}
	return filepath.Join(rm.baseDir, "bin", meta.Backend)
}

// Download fetches the specified llama.cpp build from GitHub releases.
// progress receives values in [0,1]. The backend string matches the zip
// naming: "cuda-12.4", "cuda-13.3", "vulkan", "cpu".
func (rm *RuntimeManager) Download(backend string, progress chan<- float64) error {
	defer close(progress)

	rm.Logf("[llama] resolving latest release tag...")
	tag, err := fetchLatestTag()
	if err != nil {
		rm.Logf("[llama] fetch latest tag failed: %v", err)
		return fmt.Errorf("fetch latest tag: %w", err)
	}
	rm.Logf("[llama] latest tag: %s", tag)

	zipName, err := rm.buildAssetName(backend, tag)
	if err != nil {
		return err
	}
	downloadURL := fmt.Sprintf(
		"https://github.com/ggml-org/llama.cpp/releases/download/%s/%s",
		tag, zipName,
	)
	rm.Logf("[llama] downloading %s...", zipName)

	// Download the zip to a temp file.
	if err := os.MkdirAll(rm.baseDir, 0o755); err != nil {
		return err
	}
	tmpZip := filepath.Join(rm.baseDir, zipName+".tmp")
	defer os.Remove(tmpZip)

	if err := downloadFile(downloadURL, tmpZip, progress); err != nil {
		return fmt.Errorf("download: %w", err)
	}

	// Extract the zip.
	destBin := filepath.Join(rm.baseDir, "bin", backend)
	rm.Logf("[llama] extracting to %s...", destBin)
	if err := extractZip(tmpZip, destBin); err != nil {
		rm.Logf("[llama] extract failed: %v", err)
		return fmt.Errorf("extract: %w", err)
	}

	// Save metadata.
	meta := runtimeMeta{
		Version: tag,
		Backend: backend,
	}
	rm.saveMeta(meta)
	rm.Logf("[llama] installation complete backend=%s version=%s", backend, tag)

	return nil
}

// runtimeMeta is persisted to disk to track which build is installed.
type runtimeMeta struct {
	Version string `json:"version"`
	Backend string `json:"backend"`
}

func (rm *RuntimeManager) metaPath() string {
	return filepath.Join(rm.baseDir, "runtime.json")
}

func (rm *RuntimeManager) loadMeta() runtimeMeta {
	var meta runtimeMeta
	data, err := os.ReadFile(rm.metaPath())
	if err != nil {
		return meta
	}
	_ = json.Unmarshal(data, &meta)
	return meta
}

func (rm *RuntimeManager) saveMeta(meta runtimeMeta) {
	data, _ := json.MarshalIndent(meta, "", "  ")
	os.WriteFile(rm.metaPath(), data, 0o644)
}

// buildAssetName maps backend to the correct asset zip filename.
// CUDA builds use "cudart-llama" prefix without the tag; others use "llama-{tag}".
func (rm *RuntimeManager) buildAssetName(backend, tag string) (string, error) {
	switch backend {
	case "cuda-12.4":
		return "cudart-llama-bin-win-cuda-12.4-x64.zip", nil
	case "cuda-13.3":
		return "cudart-llama-bin-win-cuda-13.3-x64.zip", nil
	case "vulkan":
		return "llama-" + tag + "-bin-win-vulkan-x64.zip", nil
	case "cpu":
		return "llama-" + tag + "-bin-win-cpu-x64.zip", nil
	}
	return "", fmt.Errorf("unsupported backend: %s", backend)
}

func fetchLatestTag() (string, error) {
	resp, err := http.Get(releasesURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var tag struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tag); err != nil {
		return "", err
	}
	return tag.TagName, nil
}

func downloadFile(url, dest string, progress chan<- float64) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	total := resp.ContentLength
	if total <= 0 {
		total = 1
	}

	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()

	buf := make([]byte, 128*1024)
	var read int64
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				return werr
			}
			read += int64(n)
			select {
			case progress <- float64(read) / float64(total):
			default:
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// ModelsDir returns the path to ~/.vecura/llama/models.
func (rm *RuntimeManager) ModelsDir() string {
	d := filepath.Join(rm.baseDir, "models")
	os.MkdirAll(d, 0o755)
	return d
}

// BaseDir returns the root of the llama runtime directory.
func (rm *RuntimeManager) BaseDir() string { return rm.baseDir }
