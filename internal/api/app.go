package api

import (
	"context"
	"database/sql"
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
	// Date is the source file modification time in Unix nanoseconds. It is
	// used by the grid for stable newest-first sorting.
	Date int64 `json:"date"`
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

	// llama components for local embedding
	llamaRM        *llama.RuntimeManager
	llamaServer    *llama.Server
	localEmbedder  *embedder.LocalLLMEmbedder
	localModelPath string // path to text embedding .gguf model
	localModelDim  int    // embedding dimensionality

	progMu  sync.Mutex
	cfgMu   sync.Mutex
	localMu sync.Mutex // serialises SetLocalModel / StopLLamaServer

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
	Provider       string                 `json:"provider"`
	Providers      map[string]providerCfg `json:"providers"`
	ProviderChecks map[string]bool        `json:"providerChecks"`
	SelectedModel  string                 `json:"selectedModel"`
	FetchedModels  []RemoteModel          `json:"fetchedModels"`
	ActiveModel    string                 `json:"activeModel"`
	FolderPath     string                 `json:"folderPath"`
	Models         []modelCfg             `json:"models"`

	// Local model settings
	LLamaBackend    string `json:"llamaBackend"` // "cuda-12.4", "cuda-13.3", "vulkan", "cpu"
	AutoStartLocal  bool   `json:"autoStartLocal"`
	LocalModelPath  string `json:"localModelPath"`
	LocalModelDim   int    `json:"localModelDim"`
	LastUpdateCheck int64  `json:"lastUpdateCheck"`
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
	if cfg.ProviderChecks == nil {
		cfg.ProviderChecks = map[string]bool{}
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
	if cfg.ProviderChecks == nil {
		cfg.ProviderChecks = map[string]bool{}
	}
	// SaveSettings is also called during normal UI teardown. Therefore a
	// provider is marked as checked only when the frontend explicitly reports
	// a successful CheckProvider call; merely having a saved URL is not enough.
	if req.ProviderChecked {
		cfg.ProviderChecks[req.Provider] = true
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
	Provider        string        `json:"provider"`
	BaseURL         string        `json:"baseUrl"`
	APIKey          string        `json:"apiKey"`
	SelectedModel   string        `json:"selectedModel"`
	FetchedModels   []RemoteModel `json:"fetchedModels"`
	FolderPath      string        `json:"folderPath"`
	ProviderChecked bool          `json:"providerChecked"`
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

	// Restore local model configuration.
	if cfg.LocalModelPath != "" {
		a.localModelPath = cfg.LocalModelPath
		a.localModelDim = cfg.LocalModelDim
		a.registerLocalEmbedder()
		if cfg.AutoStartLocal && a.llamaServer != nil {
			// Do not block Wails startup while the model is loading.
			go func(modelPath string) {
				Blogf("[local] automatic runtime startup enabled")
				if err := a.llamaServer.Start(modelPath); err != nil {
					BlogErrf("[local] automatic runtime start failed: %v", err)
				} else if err := a.llamaServer.WaitForReady(30 * time.Second); err != nil {
					BlogErrf("[local] automatic runtime not ready: %v", err)
				} else {
					Blogf("[local] automatic runtime ready on port %d", a.llamaServer.Port())
				}
			}(cfg.LocalModelPath)
		}
	}
}

// Shutdown is called by Wails on app close. It stops the llama-server.
func (a *App) Shutdown(ctx context.Context) {
	a.localMu.Lock()
	defer a.localMu.Unlock()

	if a.llamaServer != nil {
		_ = a.llamaServer.Stop()
	}
}

// registerLocalEmbedder registers the local embedder in the model registry
// if the server is running and the model is configured.
func (a *App) registerLocalEmbedder() {
	if a.localModelPath == "" {
		return
	}
	port := 8090
	if a.llamaServer != nil {
		port = a.llamaServer.Port()
	}
	a.localEmbedder = embedder.NewLocalLLMEmbedder(
		fmt.Sprintf("http://127.0.0.1:%d", port),
		a.localModelDim,
	)
	a.registry.RegisterLocal(a.localEmbedder)
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
func (a *App) Search(query, provider, modelID string, K int, tag string) ([]SearchHit, error) {
	if K <= 0 {
		K = defaultSearchLimit
	}
	query = strings.TrimSpace(query)
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

	best := map[int32]float32{}

	// 1) Keyword / substring match (no model needed).
	var kw []int32
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

	// 2) Semantic text search when the model is registered.
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
	} else {
		// No text model registered: still record the query for suggestions.
		_ = a.history.AddQuery(query)
	}

	// 3) Local model semantic search when the local model is available.
	if a.localEmbedder != nil {
		localKey := a.localEmbedder.Key()
		if e, ok := a.registry.Get(localKey); ok {
			Q, qerr := e.Embed([]string{query})
			if qerr != nil {
				BlogWarnf("[search] local embed query failed: %v", qerr)
			} else if len(Q) > 0 {
				_ = a.history.AddQuery(query)
				// Extract provider/modelID from the local key ("local/llm").
				localProvider := "local"
				localModelID := "llm"
				if idx := strings.Index(localKey, "/"); idx >= 0 {
					localProvider = localKey[:idx]
					localModelID = localKey[idx+1:]
				}
				res := a.store.Search(Q[0], localProvider, localModelID, K)
				for _, r := range res {
					if tag != "" && !allowed[r.ID] {
						continue
					}
					// Local model results get a slight boost to distinguish from text-only matches.
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
		var fileDate int64
		if info, statErr := os.Stat(img.Path); statErr == nil {
			fileDate = info.ModTime().UnixNano()
		}
		hits = append(hits, SearchHit{
			ID:     s.id,
			Path:   img.Path,
			Prompt: img.Prompt,
			Score:  s.score,
			Date:   fileDate,
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
// Validate the model synchronously so callers do not show scan progress when
// the configured model is no longer registered (for example after restart).
func (a *App) ScanFolder(path, modelKey string) error {
	if _, ok := a.registry.Get(modelKey); !ok {
		err := fmt.Errorf("model not registered: %s", modelKey)
		BlogErrf("[scan] failed: %v", err)
		return err
	}
	if modelKey == "local/llm" && (a.llamaServer == nil || !a.llamaServer.IsRunning()) {
		err := fmt.Errorf("local embedding model is not running")
		BlogErrf("[scan] failed: %v", err)
		return err
	}
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
// Local model (llama.cpp) support
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

// SetLocalAutoStart persists whether the local runtime should start on launch.
func (a *App) SetLocalAutoStart(enabled bool) error {
	a.cfgMu.Lock()
	defer a.cfgMu.Unlock()
	cfg := a.loadConfig()
	cfg.AutoStartLocal = enabled
	a.saveConfig(cfg)
	return nil
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

// SelectGGUFFile opens a native file dialog to choose a text embedding model.
func (a *App) SelectGGUFFile() (string, error) {
	if a.ctx == nil {
		return "", fmt.Errorf("app not started")
	}
	filter := "*.gguf"
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Select embedding model (.gguf)",
		Filters: []runtime.FileFilter{
			{DisplayName: "GGUF Files", Pattern: filter},
		},
	})
	if err != nil {
		return "", err
	}
	return path, nil
}

// SetLocalModel configures the local text embedding model and dimensionality,
// then persists them. If the server is running it is restarted with the new model.
func (a *App) SetLocalModel(modelPath string, dim int) error {
	if modelPath == "" {
		return fmt.Errorf("model path is required")
	}
	if dim <= 0 {
		dim = 512 // default dimensionality
	}

	a.localMu.Lock()
	Blogf("[local] configuring model=%s dim=%d", modelPath, dim)

	a.cfgMu.Lock()
	a.localModelPath = modelPath
	a.localModelDim = dim

	saved := a.loadConfig()
	saved.LocalModelPath = modelPath
	saved.LocalModelDim = dim
	a.saveConfig(saved)
	a.cfgMu.Unlock()

	// Restart the server if it's running.
	if a.llamaServer != nil && a.llamaServer.IsRunning() {
		Blogf("[local] stopping existing server")
		_ = a.llamaServer.Stop()
	}
	if err := a.llamaServer.Start(modelPath); err != nil {
		a.localMu.Unlock()
		BlogErrf("[local] server start failed: %v", err)
		return fmt.Errorf("start server: %w", err)
	}
	a.localMu.Unlock()

	// Wait for the server to be ready (without holding localMu so StopLLamaServer can proceed).
	Blogf("[local] waiting for server to be ready...")
	if err := a.llamaServer.WaitForReady(30 * time.Second); err != nil {
		BlogErrf("[local] server not ready: %v", err)
		return fmt.Errorf("server not ready: %w", err)
	}
	Blogf("[local] server ready on port %d", a.llamaServer.Port())

	// Re-acquire lock for the remaining setup.
	a.localMu.Lock()
	defer a.localMu.Unlock()

	// Bail if the server was stopped while we were waiting (e.g. user clicked Stop).
	if !a.llamaServer.IsRunning() {
		Blogf("[local] server stopped during startup, aborting")
		return fmt.Errorf("server was stopped during startup")
	}

	// Auto-detect embedding dimension from the running server.
	serverURL := fmt.Sprintf("http://127.0.0.1:%d", a.llamaServer.Port())
	if detectedDim, err := embedder.ProbeDim(serverURL); err != nil {
		Blogf("[local] could not auto-detect dimension: %v, using %d", err, dim)
	} else {
		Blogf("[local] detected embedding dimension: %d", detectedDim)
		dim = detectedDim
		a.cfgMu.Lock()
		a.localModelDim = dim
		saved := a.loadConfig()
		saved.LocalModelDim = dim
		a.saveConfig(saved)
		a.cfgMu.Unlock()
	}

	// Register the local embedder and make the new key active. This also
	// migrates configurations that still refer to the removed local/vl key.
	a.registerLocalEmbedder()
	a.cfgMu.Lock()
	saved = a.loadConfig()
	saved.ActiveModel = a.localEmbedder.Key()
	a.activeModel = saved.ActiveModel
	a.saveConfig(saved)
	a.cfgMu.Unlock()
	Blogf("[local] local embedder registered")
	return nil
}

// StopLLamaServer stops the running llama.cpp server.
func (a *App) StopLLamaServer() error {
	a.localMu.Lock()
	defer a.localMu.Unlock()

	if a.llamaServer == nil || !a.llamaServer.IsRunning() {
		return nil
	}
	Blogf("[local] stopping server")
	if err := a.llamaServer.Stop(); err != nil {
		BlogErrf("[local] stop server failed: %v", err)
		return fmt.Errorf("stop server: %w", err)
	}
	Blogf("[local] server stopped")
	return nil
}
