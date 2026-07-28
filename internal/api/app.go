package api

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"vecura/internal/db"
	"vecura/internal/embedder"
	"vecura/internal/gpu"
	"vecura/internal/llama"
	"vecura/internal/models"
	"vecura/internal/scan"
	"vecura/internal/vector"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

const (
	// defaultSearchLimit is used when the frontend does not specify K.
	defaultSearchLimit = 24
	// recentSearchLimit bounds how many past queries feed the suggestion
	// dropdown.
	recentSearchLimit = 20
	// MinWindowWidth/MinWindowHeight are the app's declared minimum window
	// size. main.go feeds the same constants into windows.Options so the
	// persisted-size restore logic below can never contradict the window's
	// actual declared minimum.
	MinWindowWidth  = 880
	MinWindowHeight = 600
)

// SearchHit is one result returned to the frontend.
type SearchHit struct {
	ID     int32   `json:"id"`
	Path   string  `json:"path"`
	Prompt string  `json:"prompt"`
	Score  float32 `json:"score"`
}

// ModelInfo describes a registered model for the frontend.
type ModelInfo struct {
	Key      string `json:"key"`
	Provider string `json:"provider"`
	ModelID  string `json:"modelId"`
	Local    bool   `json:"local"`
	Dim      int    `json:"dim"`
}

// AddModelConfig registers a remote model via API.
type AddModelConfig struct {
	Provider string `json:"provider"`
	BaseURL  string `json:"baseUrl"`
	APIKey   string `json:"apiKey"`
	Model    string `json:"model"`
	Dim      int    `json:"dim"`
	Batch    int    `json:"batch"`
}

// App is the Wails-exposed application struct.
type App struct {
	ctx      context.Context
	db       *sql.DB
	pipeline *scan.Pipeline
	store    *vector.VectorStore
	registry *models.Registry
	history  *db.HistoryRepo

	// windowStatePath is where we persist the window size between runs.
	windowStatePath string

	// configPath is where we persist user settings between runs.
	configPath string

	// activeModel is the currently selected model key, restored on startup.
	activeModel string

	// llama components for local VL embedding
	llamaRM     *llama.RuntimeManager
	llamaServer *llama.Server
	vlEmbedder  *embedder.LLamaVLEmbedder
	vlModelPath string // path to VL .gguf model
	vlProjPath  string // path to mmproj .gguf
	vlModelDim  int    // embedding dimensionality

	progMu sync.Mutex
	cfgMu  sync.Mutex
	vlMu   sync.Mutex // serialises SetVLModel / StopLLamaServer
}

// NewApp constructs the Wails App.
func NewApp(d *sql.DB, p *scan.Pipeline, s *vector.VectorStore, reg *models.Registry, thumbDir string, llamaRM *llama.RuntimeManager, llamaServer *llama.Server) *App {
	return &App{
		db:              d,
		pipeline:        p,
		store:           s,
		registry:        reg,
		history:         db.NewHistoryRepo(d),
		windowStatePath: filepath.Join(filepath.Dir(thumbDir), "window.json"),
		configPath:      filepath.Join(filepath.Dir(thumbDir), "config.json"),
		llamaRM:         llamaRM,
		llamaServer:     llamaServer,
	}
}

// providerCfg stores per-provider connection settings persisted to disk.
type providerCfg struct {
	BaseURL string `json:"baseUrl"`
	APIKey  string `json:"apiKey"`
}

// modelCfg stores a registered remote model so it can be re-registered
// after a restart.
type modelCfg struct {
	Provider string `json:"provider"`
	BaseURL  string `json:"baseUrl"`
	APIKey   string `json:"apiKey"`
	Model    string `json:"model"`
	Dim      int    `json:"dim"`
}

// appConfig is the persisted user settings blob. All fields are exported so
// Wails can serialize it to the frontend.
type appConfig struct {
	Provider      string                 `json:"provider"`
	Providers     map[string]providerCfg `json:"providers"`
	SelectedModel string                 `json:"selectedModel"`
	FetchedModels []RemoteModel          `json:"fetchedModels"`
	ActiveModel   string                 `json:"activeModel"`
	FolderPath    string                 `json:"folderPath"`
	Models        []modelCfg             `json:"models"`

	// Local VL model settings
	LLamaBackend string `json:"llamaBackend"` // "cuda-12.4", "cuda-13.3", "vulkan", "cpu"
	VLModelPath  string `json:"vlModelPath"`
	VLProjPath   string `json:"vlProjPath"`
	VLModelDim   int    `json:"vlModelDim"`
}

// loadConfig reads the persisted settings, returning an empty config when no
// file exists yet.
func (a *App) loadConfig() appConfig {
	var cfg appConfig
	data, err := os.ReadFile(a.configPath)
	if err != nil {
		return cfg
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg
	}
	if cfg.Providers == nil {
		cfg.Providers = map[string]providerCfg{}
	}
	return cfg
}

// saveConfig writes the settings to disk. It ensures the parent directory
// exists and logs any failure instead of silently dropping the write.
func (a *App) saveConfig(cfg appConfig) {
	if dir := filepath.Dir(a.configPath); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			BlogWarnf("[config] mkdir %s failed: %v", dir, err)
		}
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		BlogErrf("[config] marshal failed: %v", err)
		return
	}
	if err := os.WriteFile(a.configPath, data, 0o644); err != nil {
		BlogErrf("[config] write %s failed: %v", a.configPath, err)
	}
}

// registerModelFromCfg re-registers a saved remote model after a restart.
func (a *App) registerModelFromCfg(m modelCfg) {
	apiKey := m.APIKey
	// When the saved API key is empty (e.g. it was never persisted because
	// the key came from an env var), try the provider's saved config and
	// well-known env vars so the embedder can still authenticate.
	if apiKey == "" {
		cfg := a.loadConfig()
		if pc, ok := cfg.Providers[m.Provider]; ok && pc.APIKey != "" {
			apiKey = pc.APIKey
		}
	}
	if apiKey == "" {
		apiKey = apiKeyFromEnv(m.Provider)
	}
	e := newRemoteEmbedder(AddModelConfig{
		Provider: m.Provider,
		BaseURL:  m.BaseURL,
		APIKey:   apiKey,
		Model:    m.Model,
		Dim:      m.Dim,
		Batch:    128,
	})
	a.registry.RegisterRemote(e)
}

// SaveSettings persists the connection/selection UI state so that navigating
// away and back (or restarting) keeps the user's setup.
func (a *App) SaveSettings(req SaveSettingsReq) error {
	a.cfgMu.Lock()
	defer a.cfgMu.Unlock()
	cfg := a.loadConfig()
	cfg.Provider = req.Provider
	if cfg.Providers == nil {
		cfg.Providers = map[string]providerCfg{}
	}
	// Inherit API key from env when the frontend sends empty (the key
	// may have been auto-filled from an env var during the session).
	apiKey := req.APIKey
	if apiKey == "" {
		apiKey = apiKeyFromEnv(req.Provider)
	}
	cfg.Providers[req.Provider] = providerCfg{BaseURL: req.BaseURL, APIKey: apiKey}
	cfg.SelectedModel = req.SelectedModel
	cfg.FetchedModels = req.FetchedModels
	cfg.FolderPath = req.FolderPath
	a.saveConfig(cfg)
	return nil
}

// SaveSettingsReq carries the UI state from the frontend.
type SaveSettingsReq struct {
	Provider      string        `json:"provider"`
	BaseURL       string        `json:"baseUrl"`
	APIKey        string        `json:"apiKey"`
	SelectedModel string        `json:"selectedModel"`
	FetchedModels []RemoteModel `json:"fetchedModels"`
	FolderPath    string        `json:"folderPath"`
}

// GetConfig returns the persisted settings to the frontend on mount.
func (a *App) GetConfig() (*appConfig, error) {
	a.cfgMu.Lock()
	defer a.cfgMu.Unlock()
	cfg := a.loadConfig()
	return &cfg, nil
}

// SetActiveModel records which registered model is currently selected.
func (a *App) SetActiveModel(key string) error {
	a.cfgMu.Lock()
	defer a.cfgMu.Unlock()
	cfg := a.loadConfig()
	cfg.ActiveModel = key
	a.activeModel = key
	a.saveConfig(cfg)
	return nil
}

// Startup stores the Wails runtime context, restores the previous window
// size, and bridges scan progress to events.
// GetCtx returns the Wails context, used by system-tray callbacks to
// show/hide/quit the window after startup.
func (a *App) GetCtx() context.Context {
	return a.ctx
}

func (a *App) Startup(ctx context.Context) {
	a.ctx = ctx
	InitLogger(ctx)
	Blogf("Vecura started")

	// Wire backend loggers to emit via Wails events.
	a.pipeline.Logf = func(f string, args ...interface{}) { Blogf(f, args...) }
	a.llamaRM.Logf = func(f string, args ...interface{}) { Blogf(f, args...) }
	a.llamaServer.SetLogFunc(func(f string, args ...interface{}) { Blogf(f, args...) })

	a.restoreWindowSize()
	// Persist window size on every resize so the next launch matches.
	runtime.EventsOn(ctx, "resize", func(_ ...interface{}) {
		a.saveWindowSize()
	})
	ch := make(chan scan.Progress, 16)
	a.pipeline.Subscribe(ch)
	go func() {
		for p := range ch {
			runtime.EventsEmit(ctx, "scan:progress", p)
		}
	}()
	// Restore saved settings: re-register models and the active selection.
	cfg := a.loadConfig()
	for _, m := range cfg.Models {
		a.registerModelFromCfg(m)
	}
	a.activeModel = cfg.ActiveModel

	// Restore VL model configuration.
	if cfg.VLModelPath != "" {
		a.vlModelPath = cfg.VLModelPath
		a.vlProjPath = cfg.VLProjPath
		a.vlModelDim = cfg.VLModelDim
		a.registerVLEmbedder()
	}
}

// Shutdown is called by Wails on app close. It stops the llama-server.
func (a *App) Shutdown(ctx context.Context) {
	a.vlMu.Lock()
	defer a.vlMu.Unlock()

	if a.llamaServer != nil {
		_ = a.llamaServer.Stop()
	}
}

// registerVLEmbedder registers the VL embedder in the model registry
// if the server is running and the model is configured.
func (a *App) registerVLEmbedder() {
	if a.vlModelPath == "" {
		return
	}
	port := 8090
	if a.llamaServer != nil {
		port = a.llamaServer.Port()
	}
	a.vlEmbedder = embedder.NewLLamaVLEmbedder(
		fmt.Sprintf("http://127.0.0.1:%d", port),
		a.vlModelDim,
	)
	a.registry.RegisterLocal(a.vlEmbedder)
	// Also set VL embedder on the pipeline so scan creates VL embeddings.
	a.pipeline.SetVLEmbedder(a.vlEmbedder)
}

// windowState is the persisted {width,height} blob.
type windowState struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

// restoreWindowSize re-applies the size saved on the previous run.
func (a *App) restoreWindowSize() {
	data, err := os.ReadFile(a.windowStatePath)
	if err != nil {
		return // no saved size yet
	}
	var s windowState
	if err := json.Unmarshal(data, &s); err != nil {
		return
	}
	if s.Width < MinWindowWidth || s.Height < MinWindowHeight {
		return // ignore implausibly small sizes
	}
	runtime.WindowSetSize(a.ctx, s.Width, s.Height)
}

// saveWindowSize writes the current window size to disk.
func (a *App) saveWindowSize() {
	w, h := runtime.WindowGetSize(a.ctx)
	if w <= 0 || h <= 0 {
		return
	}
	data, err := json.Marshal(windowState{Width: w, Height: h})
	if err != nil {
		return
	}
	_ = os.WriteFile(a.windowStatePath, data, 0o644)
}

// Search runs a hybrid search: a keyword/substring match against the stored
// prompt and file path (always available, no embedding model required) merged
// with a semantic vector search when a model is registered. Keyword matches
// rank at the top so queries like "photorealistic" reliably surface images
// whose prompt contains that word, even if the embedding model is weak or
// absent.
//
// searchMode controls which embedding spaces are searched:
//   - "" or "auto": keyword + text embeddings + VL embeddings (if available)
//   - "text":       keyword + text embeddings only
//   - "vl":         VL embeddings only (query is interpreted as visual content description)
func (a *App) Search(query, provider, modelID string, K int, tag string, searchMode string) ([]SearchHit, error) {
	if K <= 0 {
		K = defaultSearchLimit
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return []SearchHit{}, nil
	}

	// @p / @c routing prefixes let the user pick the embedding space for this
	// query only, overriding the UI searchMode:
	//   @p  -> search text prompts (text embeddings)
	//   @c  -> search image content (VL embeddings)
	// Anything else falls back to the UI searchMode (auto/text/vl).
	resolvedMode := searchMode
	switch {
	case strings.HasPrefix(query, "@p"):
		resolvedMode = "text"
		query = strings.TrimSpace(query[2:])
	case strings.HasPrefix(query, "@c"):
		resolvedMode = "vl"
		query = strings.TrimSpace(query[2:])
	}
	if query == "" {
		return []SearchHit{}, nil
	}

	// id -> best score so far.
	allowed := map[int32]bool{}
	if tag != "" {
		ids, err := a.pipeline.ImageRepo().GetByTag(tag)
		if err != nil {
			return nil, err
		}
		for _, id := range ids {
			allowed[id] = true
		}
	}

	// id -> best score so far.
	best := map[int32]float32{}

	// 1) Keyword / substring match (no model needed). Skip in VL-only mode.
	var kw []int32
	if resolvedMode != "vl" {
		var kerr error
		kw, kerr = a.pipeline.ImageRepo().SearchByText(query, K)
		if kerr != nil {
			BlogWarnf("[search] keyword search failed: %v", kerr)
		}
		for _, id := range kw {
			if tag != "" && !allowed[id] {
				continue
			}
			if _, ok := best[id]; !ok {
				best[id] = 1.0 // keyword matches float to the top
			}
		}
	}

	// 2) Semantic text search when the model is registered. Skip in VL-only mode.
	if resolvedMode != "vl" {
		if e, ok := a.registry.Get(provider + "/" + modelID); ok {
			Q, qerr := e.Embed([]string{query})
			if qerr != nil {
				BlogWarnf("[search] embed query failed: %v", qerr)
			} else if len(Q) > 0 {
				_ = a.history.AddQuery(query)
				res := a.store.Search(Q[0], provider, modelID, K+len(kw))
				for _, r := range res {
					if tag != "" && !allowed[r.ID] {
						continue
					}
					if r.Score > best[r.ID] {
						best[r.ID] = r.Score
					}
				}
			}
		} else if resolvedMode != "vl" {
			// No text model registered: still record the query for suggestions.
			_ = a.history.AddQuery(query)
		}
	}

	// 3) VL semantic search when the VL model is available.
	if resolvedMode != "text" && a.vlEmbedder != nil {
		vlKey := a.vlEmbedder.Key()
		if e, ok := a.registry.Get(vlKey); ok {
			Q, qerr := e.Embed([]string{query})
			if qerr != nil {
				BlogWarnf("[search] VL embed query failed: %v", qerr)
			} else if len(Q) > 0 {
				_ = a.history.AddQuery(query)
				// Extract provider/modelID from the VL key ("local/vl").
				vlProvider := "local"
				vlModelID := "vl"
				if idx := strings.Index(vlKey, "/"); idx >= 0 {
					vlProvider = vlKey[:idx]
					vlModelID = vlKey[idx+1:]
				}
				res := a.store.Search(Q[0], vlProvider, vlModelID, K)
				for _, r := range res {
					if tag != "" && !allowed[r.ID] {
						continue
					}
					// VL results get a slight boost to distinguish from text-only matches.
					score := r.Score * 1.01
					if score > best[r.ID] {
						best[r.ID] = score
					}
				}
			}
		}
	}

	if len(best) == 0 {
		return []SearchHit{}, nil
	}

	type scored struct {
		id    int32
		score float32
	}
	order := make([]scored, 0, len(best))
	for id, sc := range best {
		order = append(order, scored{id, sc})
	}
	sort.SliceStable(order, func(i, j int) bool {
		return order[i].score > order[j].score
	})
	if len(order) > K {
		order = order[:K]
	}

	ids := make([]int32, len(order))
	for i, s := range order {
		ids[i] = s.id
	}
	images, ierr := a.pipeline.ImageRepo().GetImagesByIDs(ids)
	if ierr != nil {
		return nil, ierr
	}

	hits := make([]SearchHit, 0, len(order))
	for _, s := range order {
		img, ok := images[s.id]
		if !ok {
			continue
		}
		hits = append(hits, SearchHit{
			ID:     s.id,
			Path:   img.Path,
			Prompt: img.Prompt,
			Score:  s.score,
		})
	}
	return hits, nil
}

// AddModel registers a remote model reachable through an OpenAI-compatible
// embeddings API.
func (a *App) AddModel(cfg AddModelConfig) (*ModelInfo, error) {
	if cfg.BaseURL == "" || cfg.Model == "" {
		return nil, fmt.Errorf("baseUrl and model are required")
	}
	// Inherit API key from the saved provider config when the frontend
	// sends an empty key (e.g. the key comes from an env var and was not
	// explicitly pasted by the user).
	if cfg.APIKey == "" {
		saved := a.loadConfig()
		if pc, ok := saved.Providers[cfg.Provider]; ok && pc.APIKey != "" {
			cfg.APIKey = pc.APIKey
		}
	}
	// Auto-resolve dim from known model id when not supplied.
	dim := cfg.Dim
	if dim <= 0 {
		dim = dimForModel(cfg.Model, 0)
	}
	if dim <= 0 {
		return nil, fmt.Errorf("cannot infer dimensionality for %q; please specify dim", cfg.Model)
	}
	cfg.Dim = dim
	e := newRemoteEmbedder(cfg)
	lm := a.registry.RegisterRemote(e)
	Blogf("[model] registered provider=%s model=%s dim=%d", cfg.Provider, cfg.Model, dim)
	// Persist the registered model and make it the active one.
	a.cfgMu.Lock()
	saved := a.loadConfig()
	mc := modelCfg{Provider: cfg.Provider, BaseURL: cfg.BaseURL, APIKey: cfg.APIKey, Model: cfg.Model, Dim: dim}
	found := false
	for i := range saved.Models {
		if saved.Models[i].Provider == cfg.Provider && saved.Models[i].Model == cfg.Model {
			saved.Models[i] = mc
			found = true
			break
		}
	}
	if !found {
		saved.Models = append(saved.Models, mc)
	}
	saved.ActiveModel = lm.Key
	a.activeModel = lm.Key
	a.saveConfig(saved)
	a.cfgMu.Unlock()
	return &ModelInfo{
		Key:      lm.Key,
		Provider: lm.Provider,
		ModelID:  lm.ModelID,
		Local:    lm.Local,
		Dim:      e.Dim(),
	}, nil
}

// ListModels returns registered models.
func (a *App) ListModels() []ModelInfo {
	loaded := a.registry.List()
	out := make([]ModelInfo, 0, len(loaded))
	for _, m := range loaded {
		dim := 0
		if m.Embedder != nil {
			dim = m.Embedder.Dim()
		}
		out = append(out, ModelInfo{
			Key:      m.Key,
			Provider: m.Provider,
			ModelID:  m.ModelID,
			Local:    m.Local,
			Dim:      dim,
		})
	}
	return out
}

// ScanFolder triggers a background scan of a folder for a given model key.
func (a *App) ScanFolder(path, modelKey string) error {
	Blogf("[scan] started folder=%s model=%s", path, modelKey)
	go func() {
		err := a.pipeline.ScanFolder(a.ctx, path, modelKey)
		if err != nil {
			BlogErrf("[scan] failed: %v", err)
		} else {
			Blogf("[scan] finished folder=%s", path)
		}
	}()
	return nil
}

// PickFolder opens a native directory dialog and returns the chosen path.
// Returns "" if cancelled.
func (a *App) PickFolder() (string, error) {
	if a.ctx == nil {
		return "", fmt.Errorf("app not started")
	}
	path, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title:                "Select image folder",
		CanCreateDirectories: true,
		ShowHiddenFiles:      false,
		ResolvesAliases:      true,
	})
	if err != nil {
		return "", err
	}
	return path, nil
}

// ImageDataURI returns a full-resolution data-URI for the given image path,
// used by the preview dialog.
func (a *App) ImageDataURI(imagePath string) (string, error) {
	data, err := os.ReadFile(imagePath)
	if err != nil {
		return "", fmt.Errorf("read image: %w", err)
	}
	ct := mimeType(imagePath)
	return "data:" + ct + ";base64," + base64.StdEncoding.EncodeToString(data), nil
}

func mimeType(p string) string {
	switch strings.ToLower(filepath.Ext(p)) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	case ".gif":
		return "image/gif"
	case ".bmp":
		return "image/bmp"
	default:
		return "application/octet-stream"
	}
}

// RecentSearches returns recent search queries for the suggestion dropdown.
func (a *App) RecentSearches() ([]string, error) {
	return a.history.Recent(recentSearchLimit)
}

// CheckProvider validates an API key by calling the provider's /models
// endpoint. Returns the available models on success.
func (a *App) CheckProvider(baseURL, apiKey string) ([]RemoteModel, error) {
	Blogf("[provider] checking baseUrl=%s", baseURL)
	models, err := a.ListRemoteModels(baseURL, apiKey)
	if err != nil {
		BlogErrf("[provider] check failed: %v", err)
		return nil, err
	}
	Blogf("[provider] check ok, %d models found", len(models))
	return models, nil
}

// RemoveModel unregisters a model by key.
func (a *App) RemoveModel(key string) error {
	a.registry.Unload(key)
	a.cfgMu.Lock()
	saved := a.loadConfig()
	kept := saved.Models[:0]
	for _, m := range saved.Models {
		if "remote/"+m.Model != key {
			kept = append(kept, m)
		}
	}
	saved.Models = kept
	if saved.ActiveModel == key {
		saved.ActiveModel = ""
		a.activeModel = ""
	}
	a.saveConfig(saved)
	a.cfgMu.Unlock()
	return nil
}

// ClearDB wipes all indexed data: images, embeddings, tags, and search
// history. The in-memory vector store is reset so search returns empty
// immediately after. Registered models and provider settings are preserved.
func (a *App) ClearDB() error {
	tables := []string{"image_tags", "tags", "embeddings", "search_history", "images"}
	for _, t := range tables {
		if _, err := a.db.Exec("DELETE FROM " + t); err != nil {
			return fmt.Errorf("clear %s: %w", t, err)
		}
	}
	a.store.Reset()
	return nil
}

// GetModelInfo returns stored info for a registered model key.
func (a *App) GetModelInfo(key string) (*ModelInfo, error) {
	for _, m := range a.registry.List() {
		if m.Key == key {
			dim := 0
			if m.Embedder != nil {
				dim = m.Embedder.Dim()
			}
			return &ModelInfo{
				Key:      m.Key,
				Provider: m.Provider,
				ModelID:  m.ModelID,
				Local:    m.Local,
				Dim:      dim,
			}, nil
		}
	}
	return nil, fmt.Errorf("model not found: %s", key)
}

// ---------------------------------------------------------------------------
// Local VL model (llama.cpp) support
// ---------------------------------------------------------------------------

// GPUInfoResult is the JSON-safe GPU detection result.
type GPUInfoResult struct {
	Vendor string `json:"vendor"`
	Name   string `json:"name"`
	VRAM   uint64 `json:"vram"`
}

// DetectGPU probes the system for a discrete GPU.
func (a *App) DetectGPU() (*GPUInfoResult, error) {
	info := gpu.DetectGPU()
	return &GPUInfoResult{
		Vendor: info.Vendor,
		Name:   info.Name,
		VRAM:   info.VRAM,
	}, nil
}

// LLamaStatus describes the llama.cpp runtime state.
type LLamaStatus struct {
	Installed bool   `json:"installed"`
	Version   string `json:"version"`
	Backend   string `json:"backend"`
	ServerOK  bool   `json:"serverOK"`
}

// GetLLamaStatus returns the current state of the llama.cpp runtime and server.
func (a *App) GetLLamaStatus() (*LLamaStatus, error) {
	st := a.llamaRM.Status()
	serverOK := false
	if a.llamaServer != nil {
		serverOK = a.llamaServer.IsRunning()
	}
	return &LLamaStatus{
		Installed: st.Installed,
		Version:   st.Version,
		Backend:   st.Backend,
		ServerOK:  serverOK,
	}, nil
}

// DownloadLLama downloads the llama.cpp runtime for the given backend
// ("cuda-12.4", "cuda-13.3", "vulkan", "cpu"). Progress events are emitted
// via Wails events.
func (a *App) DownloadLLama(backend string) error {
	Blogf("[llama] download started backend=%s", backend)
	progress := make(chan float64, 32)
	go func() {
		for p := range progress {
			runtime.EventsEmit(a.ctx, "llama:progress", p)
		}
	}()
	if err := a.llamaRM.Download(backend, progress); err != nil {
		BlogErrf("[llama] download failed: %v", err)
		return err
	}
	Blogf("[llama] download complete backend=%s", backend)
	// Persist the chosen backend in config.
	a.cfgMu.Lock()
	saved := a.loadConfig()
	saved.LLamaBackend = backend
	a.saveConfig(saved)
	a.cfgMu.Unlock()
	return nil
}

// SelectGGUFFile opens a native file dialog to choose a .gguf file.
// kind is "model" or "mmproj".
func (a *App) SelectGGUFFile(kind string) (string, error) {
	if a.ctx == nil {
		return "", fmt.Errorf("app not started")
	}
	filter := "*.gguf"
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Select " + kind + " file (.gguf)",
		Filters: []runtime.FileFilter{
			{DisplayName: "GGUF Files", Pattern: filter},
		},
	})
	if err != nil {
		return "", err
	}
	return path, nil
}

// SetVLModel configures the VL model paths and dimensionality, then
// persists them. If the server is running it is restarted with the new model.
// projPath may be empty when the model .gguf already contains the projector.
func (a *App) SetVLModel(modelPath, projPath string, dim int) error {
	if modelPath == "" {
		return fmt.Errorf("model path is required")
	}
	if dim <= 0 {
		dim = 512 // default dimensionality for SmolVLM-500M
	}

	a.vlMu.Lock()
	Blogf("[vl] configuring model=%s proj=%s dim=%d", modelPath, projPath, dim)

	a.cfgMu.Lock()
	a.vlModelPath = modelPath
	a.vlProjPath = projPath
	a.vlModelDim = dim

	saved := a.loadConfig()
	saved.VLModelPath = modelPath
	saved.VLProjPath = projPath
	saved.VLModelDim = dim
	a.saveConfig(saved)
	a.cfgMu.Unlock()

	// Restart the server if it's running.
	if a.llamaServer != nil && a.llamaServer.IsRunning() {
		Blogf("[vl] stopping existing server")
		_ = a.llamaServer.Stop()
	}
	if err := a.llamaServer.Start(modelPath, projPath); err != nil {
		a.vlMu.Unlock()
		BlogErrf("[vl] server start failed: %v", err)
		return fmt.Errorf("start server: %w", err)
	}
	a.vlMu.Unlock()

	// Wait for the server to be ready (without holding vlMu so StopLLamaServer can proceed).
	Blogf("[vl] waiting for server to be ready...")
	if err := a.llamaServer.WaitForReady(30 * time.Second); err != nil {
		BlogErrf("[vl] server not ready: %v", err)
		return fmt.Errorf("server not ready: %w", err)
	}
	Blogf("[vl] server ready on port %d", a.llamaServer.Port())

	// Re-acquire lock for the remaining setup.
	a.vlMu.Lock()
	defer a.vlMu.Unlock()

	// Bail if the server was stopped while we were waiting (e.g. user clicked Stop).
	if !a.llamaServer.IsRunning() {
		Blogf("[vl] server stopped during startup, aborting")
		return fmt.Errorf("server was stopped during startup")
	}

	// Auto-detect embedding dimension from the running server.
	serverURL := fmt.Sprintf("http://127.0.0.1:%d", a.llamaServer.Port())
	if detectedDim, err := embedder.ProbeDim(serverURL); err != nil {
		Blogf("[vl] could not auto-detect dimension: %v, using %d", err, dim)
	} else {
		Blogf("[vl] detected embedding dimension: %d", detectedDim)
		dim = detectedDim
		a.cfgMu.Lock()
		a.vlModelDim = dim
		saved := a.loadConfig()
		saved.VLModelDim = dim
		a.saveConfig(saved)
		a.cfgMu.Unlock()
	}

	// Register the VL embedder.
	a.registerVLEmbedder()
	Blogf("[vl] VL embedder registered")
	return nil
}

// StopLLamaServer stops the running llama.cpp server.
func (a *App) StopLLamaServer() error {
	a.vlMu.Lock()
	defer a.vlMu.Unlock()

	if a.llamaServer == nil || !a.llamaServer.IsRunning() {
		return nil
	}
	Blogf("[vl] stopping server")
	if err := a.llamaServer.Stop(); err != nil {
		BlogErrf("[vl] stop server failed: %v", err)
		return fmt.Errorf("stop server: %w", err)
	}
	Blogf("[vl] server stopped")
	return nil
}

// SearchByImage encodes an image and searches the vector store for visually
// similar images.
func (a *App) SearchByImage(imagePath string, K int) ([]SearchHit, error) {
	if K <= 0 {
		K = defaultSearchLimit
	}
	if a.vlEmbedder == nil {
		return nil, fmt.Errorf("VL model not configured")
	}
	if !a.llamaServer.IsRunning() {
		return nil, fmt.Errorf("llama-server not running")
	}

	data, err := os.ReadFile(imagePath)
	if err != nil {
		return nil, fmt.Errorf("read image: %w", err)
	}
	vec, err := a.vlEmbedder.EmbedImage(data)
	if err != nil {
		return nil, fmt.Errorf("embed image: %w", err)
	}

	res := a.store.Search(vec, "local", "vl", K)
	if len(res) == 0 {
		return []SearchHit{}, nil
	}

	ids := make([]int32, len(res))
	for i, r := range res {
		ids[i] = r.ID
	}
	images, ierr := a.pipeline.ImageRepo().GetImagesByIDs(ids)
	if ierr != nil {
		return nil, ierr
	}

	hits := make([]SearchHit, 0, len(res))
	for _, r := range res {
		img, ok := images[r.ID]
		if !ok {
			continue
		}
		hits = append(hits, SearchHit{
			ID:     r.ID,
			Path:   img.Path,
			Prompt: img.Prompt,
			Score:  r.Score,
		})
	}
	return hits, nil
}

// SearchByImageDataURI encodes raw image bytes (base64 string) and searches
// the vector store for visually similar images.
func (a *App) SearchByImageDataURI(dataURI string, K int) ([]SearchHit, error) {
	if K <= 0 {
		K = defaultSearchLimit
	}
	if a.vlEmbedder == nil {
		return nil, fmt.Errorf("VL model not configured")
	}
	if !a.llamaServer.IsRunning() {
		return nil, fmt.Errorf("llama-server not running")
	}

	// Decode base64 data URI.
	if idx := strings.Index(dataURI, ","); idx >= 0 {
		dataURI = dataURI[idx+1:]
	}
	raw, err := base64.StdEncoding.DecodeString(dataURI)
	if err != nil {
		return nil, fmt.Errorf("decode base64: %w", err)
	}

	vec, err := a.vlEmbedder.EmbedImage(raw)
	if err != nil {
		return nil, fmt.Errorf("embed image: %w", err)
	}

	res := a.store.Search(vec, "local", "vl", K)
	if len(res) == 0 {
		return []SearchHit{}, nil
	}

	ids := make([]int32, len(res))
	for i, r := range res {
		ids[i] = r.ID
	}
	images, ierr := a.pipeline.ImageRepo().GetImagesByIDs(ids)
	if ierr != nil {
		return nil, ierr
	}

	hits := make([]SearchHit, 0, len(res))
	for _, r := range res {
		img, ok := images[r.ID]
		if !ok {
			continue
		}
		hits = append(hits, SearchHit{
			ID:     r.ID,
			Path:   img.Path,
			Prompt: img.Prompt,
			Score:  r.Score,
		})
	}
	return hits, nil
}
