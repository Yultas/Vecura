package api

import (
	"vecura/internal/embedder"
)

// newRemoteEmbedder builds an OpenAI-compatible embedder from API config.
func newRemoteEmbedder(cfg AddModelConfig) *embedder.OpenAICompatibleEmbedder {
	return embedder.NewOpenAICompatibleEmbedder(cfg.BaseURL, cfg.APIKey, cfg.Model, cfg.Dim, cfg.Batch)
}
