package embedder

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// LocalLLMEmbedder provides text embedding via a running llama-server.
type LocalLLMEmbedder struct {
	BaseURL string // e.g. "http://127.0.0.1:8090"
	dim     int
	client  *http.Client
}

// NewLocalLLMEmbedder creates a local text embedder pointing at the given server.
func NewLocalLLMEmbedder(baseURL string, dim int) *LocalLLMEmbedder {
	return &LocalLLMEmbedder{
		BaseURL: baseURL,
		dim:     dim,
		client: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

func (e *LocalLLMEmbedder) Key() string { return "local/llm" }
func (e *LocalLLMEmbedder) Dim() int    { return e.dim }

// Embed implements the Embedder interface by forwarding text to the server's
// /v1/embeddings endpoint.
func (e *LocalLLMEmbedder) Embed(texts []string) ([][]float32, error) {
	type reqBody struct {
		Input []string `json:"input"`
		Model string   `json:"model"`
	}
	body, _ := json.Marshal(reqBody{Input: texts, Model: "default"})
	resp, err := e.client.Post(e.BaseURL+"/v1/embeddings", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("local text embed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("local text embed status %d", resp.StatusCode)
	}
	var parsed struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, err
	}
	out := make([][]float32, len(parsed.Data))
	for i, d := range parsed.Data {
		out[i] = d.Embedding
	}
	return out, nil
}

// ProbeDim sends a test text embedding request to the running llama-server
// and returns the actual embedding dimension from the response vector length.
func ProbeDim(baseURL string) (int, error) {
	type reqBody struct {
		Input []string `json:"input"`
		Model string   `json:"model"`
	}
	body, _ := json.Marshal(reqBody{Input: []string{"probe"}, Model: "default"})
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Post(baseURL+"/v1/embeddings", "application/json", bytes.NewReader(body))
	if err != nil {
		return 0, fmt.Errorf("probe dim: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return 0, fmt.Errorf("probe dim status %d", resp.StatusCode)
	}
	var parsed struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return 0, fmt.Errorf("probe dim decode: %w", err)
	}
	if len(parsed.Data) == 0 || len(parsed.Data[0].Embedding) == 0 {
		return 0, fmt.Errorf("probe dim: empty embedding response")
	}
	return len(parsed.Data[0].Embedding), nil
}
