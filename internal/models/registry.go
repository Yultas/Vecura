package models

import (
	"sync"

	"vecura/internal/embedder"
)

// LoadedModel tracks a registered embedder.
type LoadedModel struct {
	Key      string
	Provider string
	ModelID  string
	Embedder embedder.Embedder
	Local    bool
}

// Registry is a thread-safe collection of registered embedders. Vecura's AI
// functionality is implemented strictly through remote HTTP APIs, so the
// registry only needs to track and look up embedders by key — there is no
// local model runtime to load, budget RAM for, or evict.
type Registry struct {
	mu     sync.RWMutex
	models map[string]*LoadedModel
}

// NewRegistry creates an empty model registry.
func NewRegistry() *Registry {
	return &Registry{models: make(map[string]*LoadedModel)}
}

// RegisterRemote adds a remote (pure-Go, HTTP-based) embedder.
func (r *Registry) RegisterRemote(e embedder.Embedder) *LoadedModel {
	lm := &LoadedModel{
		Key:      e.Key(),
		Provider: "remote",
		ModelID:  e.Key(),
		Embedder: e,
		Local:    false,
	}
	r.mu.Lock()
	r.models[lm.Key] = lm
	r.mu.Unlock()
	return lm
}

// RegisterLocal adds a local (server-backed) embedder such as a local llama-server.
func (r *Registry) RegisterLocal(e embedder.Embedder) *LoadedModel {
	lm := &LoadedModel{
		Key:      e.Key(),
		Provider: "local",
		ModelID:  e.Key(),
		Embedder: e,
		Local:    true,
	}
	r.mu.Lock()
	r.models[lm.Key] = lm
	r.mu.Unlock()
	return lm
}

// Unload removes a model by key.
func (r *Registry) Unload(key string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.models, key)
}

// Get returns the embedder for a model key.
func (r *Registry) Get(key string) (embedder.Embedder, bool) {
	r.mu.RLock()
	m, ok := r.models[key]
	r.mu.RUnlock()
	if !ok {
		return nil, false
	}
	return m.Embedder, true
}

// List returns all registered models.
func (r *Registry) List() []*LoadedModel {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*LoadedModel, 0, len(r.models))
	for _, m := range r.models {
		out = append(out, m)
	}
	return out
}
