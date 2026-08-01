package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Well-known environment variables that may hold a provider's API key, so
// the UI can auto-fill it instead of prompting the user. Kept as constants
// (instead of repeated string literals) so the provider preset, the initial
// registration, and the settings-save path can never drift out of sync.
const (
	envOpenRouterAPIKey = "OPENROUTER_API_KEY"
	envOpenAIAPIKey     = "OPENAI_API_KEY"
)

// apiKeyFromEnv resolves a provider's API key from its well-known
// environment variable, returning "" for providers without one.
func apiKeyFromEnv(provider string) string {
	switch provider {
	case "openrouter":
		return os.Getenv(envOpenRouterAPIKey)
	case "openai":
		return os.Getenv(envOpenAIAPIKey)
	default:
		return ""
	}
}

// ProviderPreset describes a known embedding provider with its base URL and
// commonly used models (id -> default dimensionality).
type ProviderPreset struct {
	ID       string        `json:"id"`
	Name     string        `json:"name"`
	BaseURL  string        `json:"baseUrl"`
	DocURL   string        `json:"docUrl"`
	Models   []PresetModel `json:"models"`
	NeedsKey bool          `json:"needsKey"`
	// EnvKey, when set, is the env var that may hold a preconfigured
	// API key for this provider (e.g. OPENROUTER_API_KEY). The UI uses
	// it to auto-fill the key field instead of prompting the user.
	EnvKey string `json:"envKey"`
	// KeyFromEnv is the API key resolved from EnvKey at startup. Empty
	// when the env var is not set in the system.
	KeyFromEnv string `json:"keyFromEnv"`
}

// PresetModel is a known model for a provider.
type PresetModel struct {
	ID  string `json:"id"`
	Dim int    `json:"dim"`
}

// ProviderPresets returns the built-in provider configurations so the UI can
// prefill Base URL and offer a starting model list.
func (a *App) ProviderPresets() ([]ProviderPreset, error) {
	presets := []ProviderPreset{
		{
			ID:       "openai",
			Name:     "OpenAI",
			BaseURL:  "https://api.openai.com/v1",
			DocURL:   "https://platform.openai.com/docs/guides/embeddings",
			NeedsKey: true,
			Models: []PresetModel{
				{ID: "text-embedding-3-small", Dim: 1536},
				{ID: "text-embedding-3-large", Dim: 3072},
				{ID: "text-embedding-ada-002", Dim: 1536},
			},
		},
		{
			ID:         "openrouter",
			Name:       "OpenRouter",
			BaseURL:    "https://openrouter.ai/api/v1",
			DocURL:     "https://openrouter.ai/models?q=embeddings",
			NeedsKey:   true,
			EnvKey:     envOpenRouterAPIKey,
			KeyFromEnv: os.Getenv(envOpenRouterAPIKey),
			Models: []PresetModel{
				{ID: "openai/text-embedding-3-small", Dim: 1536},
				{ID: "openai/text-embedding-3-large", Dim: 3072},
				{ID: "nvidia/llama-3.2-nv-embedqa-1b-v1", Dim: 2048},
			},
		},
		{
			ID:       "localai",
			Name:     "LocalAI (self-hosted)",
			BaseURL:  "http://localhost:8080/v1",
			DocURL:   "https://localai.io",
			NeedsKey: false,
			Models:   []PresetModel{},
		},
		{
			ID:       "ollama",
			Name:     "Ollama (OpenAI-compat)",
			BaseURL:  "http://localhost:11434/v1",
			DocURL:   "https://ollama.com",
			NeedsKey: false,
			Models:   []PresetModel{},
		},
	}
	return presets, nil
}

// RemoteModel is a model discovered from a provider's /models endpoint.
// IsEmbed marks models that look embedding-capable so the UI can surface
// them first and filter out plain chat models.
type RemoteModel struct {
	ID            string `json:"id"`
	ContextLength int    `json:"contextLength"`
	IsEmbed       bool   `json:"isEmbed"`
}

// ListRemoteModels queries a provider's /models endpoint and returns the
// available embedding model IDs so the user can pick one instead of typing
// it. It asks the provider to filter by the "embeddings" output modality
// (OpenRouter supports this), falling back to the unfiltered /models list
// plus a heuristic when the provider does not honour the query param.
func (a *App) ListRemoteModels(baseURL, apiKey string) ([]RemoteModel, error) {
	if baseURL == "" {
		return nil, fmt.Errorf("baseUrl is required")
	}

	// Preferred: server-side embedding filter.
	models, err := a.fetchModels(baseURL+"/models?output_modalities=embeddings", apiKey)
	if err == nil && len(models) > 0 {
		return models, nil
	}
	// Fallback: unfiltered list, filter client-side by heuristic.
	return a.fetchModels(baseURL+"/models", apiKey)
}

// fetchModels performs one GET to the given models URL and decodes the
// response into RemoteModel values, marking embedding-capable ones.
func (a *App) fetchModels(url, apiKey string) ([]RemoteModel, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list models: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("provider returned %d: %s", resp.StatusCode, string(body))
	}

	var parsed struct {
		Data []struct {
			ID            string `json:"id"`
			ContextLength int    `json:"context_length"`
			Architecture  struct {
				Modality         string   `json:"modality"`
				OutputModalities []string `json:"output_modalities"`
			} `json:"architecture"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("decode models: %w", err)
	}

	out := make([]RemoteModel, 0, len(parsed.Data))
	for _, m := range parsed.Data {
		if len(m.ID) == 0 {
			continue
		}
		isEmbed := isEmbeddingModel(m.ID, m.Architecture.Modality, m.Architecture.OutputModalities)
		out = append(out, RemoteModel{ID: m.ID, ContextLength: m.ContextLength, IsEmbed: isEmbed})
	}
	// When the list mixes chat and embedding models, keep only embeds.
	if onlyEmbeddings(out) {
		filtered := make([]RemoteModel, 0, len(out))
		for _, m := range out {
			if m.IsEmbed {
				filtered = append(filtered, m)
			}
		}
		return filtered, nil
	}
	return out, nil
}

// isEmbeddingModel reports whether a model can produce embeddings.
func isEmbeddingModel(id, modality string, outModalities []string) bool {
	low := strings.ToLower(id)
	if strings.Contains(low, "embed") {
		return true
	}
	if strings.Contains(strings.ToLower(modality), "embed") {
		return true
	}
	for _, om := range outModalities {
		if strings.Contains(strings.ToLower(om), "embed") {
			return true
		}
	}
	return false
}

// onlyEmbeddings reports whether any model in the list is an embedding model.
func onlyEmbeddings(models []RemoteModel) bool {
	for _, m := range models {
		if m.IsEmbed {
			return true
		}
	}
	return false
}

// dimForModel resolves a known dimensionality for a model id, falling back to
// the supplied default (used when registering from a fetched list).
func dimForModel(modelID string, presetDim int) int {
	if presetDim > 0 {
		return presetDim
	}
	switch {
	case strings.Contains(modelID, "3-small"):
		return 1536
	case strings.Contains(modelID, "3-large"):
		return 3072
	case strings.Contains(modelID, "ada-002"):
		return 1536
	case strings.Contains(modelID, "nemotron") && strings.Contains(modelID, "embed"):
		return 2048
	case strings.Contains(modelID, "nv-embedqa"):
		return 2048
	}
	return 0
}
