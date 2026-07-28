package main

import (
	"embed"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"vecura/internal/api"
	"vecura/internal/db"
	"vecura/internal/llama"
	"vecura/internal/models"
	"vecura/internal/scan"
	"vecura/internal/vector"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed internal/tray/icon.ico
var trayIcon []byte

// defaultWindowWidth/defaultWindowHeight are the initial window size used
// when no previous size was persisted to disk.
const (
	defaultWindowWidth  = 1100
	defaultWindowHeight = 760
)

func main() {
	home, err := os.UserHomeDir()
	if err != nil {
		log.Fatal(err)
	}
	appDir := filepath.Join(home, ".vecura")
	_ = os.MkdirAll(appDir, 0o755)
	dbPath := filepath.Join(appDir, "gallery.db")
	thumbDir := filepath.Join(appDir, "thumbnails")

	d, err := db.Open(dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer d.Close()

	store := vector.NewVectorStore()
	registry := models.NewRegistry()
	pipeline := scan.NewPipeline(d, registry, store, thumbDir)
	if err := pipeline.BuildFromDB(); err != nil {
		log.Fatal(err)
	}

	// llama.cpp runtime and server for local VL embeddings.
	llamaDir := filepath.Join(appDir, "llama")
	llamaRM := llama.NewRuntimeManager(llamaDir)
	llamaSrv := llama.NewServer(llamaRM)

	app := api.NewApp(d, pipeline, store, registry, thumbDir, llamaRM, llamaSrv)
	app.SetTrayIcon(trayIcon)

	// Load a local .env file (if present) so OPENROUTER_API_KEY and
	// friends are visible via os.Getenv. System env vars always win.
	// Try relative CWD first, then the config dir (~/.vecura/).
	loadEnvFile(".env")
	loadEnvFile(filepath.Join(appDir, ".env"))

	err = wails.Run(&options.App{
		Title:            "Vecura",
		Width:            defaultWindowWidth,
		Height:           defaultWindowHeight,
		MinWidth:         api.MinWindowWidth,
		MinHeight:        api.MinWindowHeight,
		Frameless:        true,
		BackgroundColour: &options.RGBA{R: 0, G: 0, B: 0, A: 0},
		// Closing the window hides it (and removes it from the taskbar)
		// instead of quitting. A custom system tray (see below) re-opens it.
		HideWindowOnClose: true,
		AssetServer: &assetserver.Options{
			Assets:  assets,
			Handler: localThumbnailHandler(thumbDir),
		},
		Windows: &windows.Options{
			// Acrylic backdrop gives the vibrant, blurred material.
			BackdropType:                      windows.Acrylic,
			WebviewIsTransparent:              true,
			WindowIsTranslucent:               true,
			DisableFramelessWindowDecorations: false,
		},
		OnStartup:  app.Startup,
		OnShutdown: app.Shutdown,
		Bind: []interface{}{
			app,
		},
	})
	if err != nil {
		log.Fatal(err)
	}
}

func localThumbnailHandler(thumbDir string) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/local-thumbs/", http.StripPrefix(
		"/local-thumbs/",
		http.FileServer(http.Dir(thumbDir)),
	))
	return mux
}

// loadEnvFile reads KEY=VALUE lines from path (ignoring comments and
// blank lines) and exports them into the process environment, but only when
// the key is not already set. This keeps system env vars authoritative.
func loadEnvFile(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return // no .env is fine
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.Trim(strings.TrimSpace(v), `"`)
		if k == "" || os.Getenv(k) != "" {
			continue
		}
		_ = os.Setenv(k, v)
	}
}
