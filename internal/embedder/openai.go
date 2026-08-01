package embedder

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// OpenAICompatibleEmbedder is a pure-Go embedder for OpenAI/OpenRouter-style
// HTTP APIs. It is safe for concurrent use: it holds no mutable state beyond
// a shared *http.Client, which is itself safe for concurrent requests.
type OpenAICompatibleEmbedder struct {
	BaseURL string
	APIKey  string
	Model   string
	dim     int
	Batch   int

	client *http.Client
}

// NewOpenAICompatibleEmbedder builds a remote embedder. baseURL must include
// the API version path (e.g. https://api.openai.com/v1).
func NewOpenAICompatibleEmbedder(baseURL, apiKey, model string, dim, batch int) *OpenAICompatibleEmbedder {
	if batch <= 0 {
		batch = 128
	}
	return &OpenAICompatibleEmbedder{
		BaseURL: baseURL,
		APIKey:  apiKey,
		Model:   model,
		dim:     dim,
		Batch:   batch,
		client: &http.Client{
			Timeout: 60 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 100,
			},
		},
	}
}

func (e *OpenAICompatibleEmbedder) Key() string { return "remote/" + e.Model }
func (e *OpenAICompatibleEmbedder) Dim() int    { return e.dim }

type embedRequest struct {
	Input      []string `json:"input"`
	Model      string   `json:"model"`
	Dimensions int      `json:"dimensions,omitempty"`
}

type embedResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Embed sends texts in batches and returns one vector per input.
func (e *OpenAICompatibleEmbedder) Embed(texts []string) ([][]float32, error) {
	return e.EmbedCtx(context.Background(), texts)
}

// EmbedCtx is the context-aware variant of Embed.
func (e *OpenAICompatibleEmbedder) EmbedCtx(ctx context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, 0, len(texts))
	for start := 0; start < len(texts); start += e.Batch {
		end := start + e.Batch
		if end > len(texts) {
			end = len(texts)
		}
		vecs, err := e.embedBatch(ctx, texts[start:end])
		if err != nil {
			return nil, err
		}
		out = append(out, vecs...)
	}
	return out, nil
}

func (e *OpenAICompatibleEmbedder) embedBatch(ctx context.Context, batch []string) ([][]float32, error) {
	body, err := json.Marshal(embedRequest{Input: batch, Model: e.Model, Dimensions: e.dim})
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.BaseURL+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.APIKey)

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("do: %w", err)
	}
	defer resp.Body.Close()

	var parsed embedResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	if parsed.Error != nil {
		return nil, fmt.Errorf("api error: %s", parsed.Error.Message)
	}
	if len(parsed.Data) != len(batch) {
		return nil, fmt.Errorf("api returned %d vectors for %d inputs", len(parsed.Data), len(batch))
	}
	vecs := make([][]float32, len(parsed.Data))
	for i, d := range parsed.Data {
		vecs[i] = d.Embedding
	}
	return vecs, nil
}
