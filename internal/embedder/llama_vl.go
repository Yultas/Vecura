package embedder

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"
)

// LLamaVLEmbedder encodes images via a running llama-server with a VL model.
type LLamaVLEmbedder struct {
	BaseURL string // e.g. "http://127.0.0.1:8090"
	dim     int
	client  *http.Client
}

// NewLLamaVLEmbedder creates a VL embedder pointing at the given server.
func NewLLamaVLEmbedder(baseURL string, dim int) *LLamaVLEmbedder {
	return &LLamaVLEmbedder{
		BaseURL: baseURL,
		dim:     dim,
		client: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

func (e *LLamaVLEmbedder) Key() string { return "local/vl" }
func (e *LLamaVLEmbedder) Dim() int    { return e.dim }

// Embed implements the Embedder interface — it embeds the text descriptions
// by forwarding them to the server's /v1/embeddings endpoint.
// For VL models this is a secondary path; the primary use is EmbedImage.
func (e *LLamaVLEmbedder) Embed(texts []string) ([][]float32, error) {
	type reqBody struct {
		Input []string `json:"input"`
		Model string   `json:"model"`
	}
	body, _ := json.Marshal(reqBody{Input: texts, Model: "vl"})
	resp, err := e.client.Post(e.BaseURL+"/v1/embeddings", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("vl text embed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("vl text embed status %d", resp.StatusCode)
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

// vlEmbedRequest is the payload for /v1/embeddings with an image.
type vlEmbedRequest struct {
	Model string `json:"model"`
	Input string `json:"input"` // base64 data URI of the image
}

// EmbedImage encodes a single image and returns its embedding vector.
func (e *LLamaVLEmbedder) EmbedImage(imageData []byte) ([]float32, error) {
	ct := detectMIME(imageData)
	b64 := base64.StdEncoding.EncodeToString(imageData)
	dataURI := "data:" + ct + ";base64," + b64

	body, _ := json.Marshal(vlEmbedRequest{
		Model: "vl",
		Input: dataURI,
	})
	resp, err := e.client.Post(e.BaseURL+"/v1/embeddings", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("vl image embed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("vl image embed status %d", resp.StatusCode)
	}
	var parsed struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, err
	}
	if len(parsed.Data) == 0 {
		return nil, fmt.Errorf("vl image embed: empty response")
	}
	return parsed.Data[0].Embedding, nil
}

// EmbedImages encodes multiple images, returning one vector per image.
func (e *LLamaVLEmbedder) EmbedImages(images [][]byte) ([][]float32, error) {
	out := make([][]float32, len(images))
	for i, img := range images {
		vec, err := e.EmbedImage(img)
		if err != nil {
			return nil, fmt.Errorf("image %d: %w", i, err)
		}
		out[i] = vec
	}
	return out, nil
}

// detectMIME sniffs the first bytes of an image to return the MIME type.
func detectMIME(data []byte) string {
	if len(data) < 4 {
		return "application/octet-stream"
	}
	if data[0] == 0x89 && data[1] == 'P' && data[2] == 'N' && data[3] == 'G' {
		return "image/png"
	}
	if data[0] == 0xFF && data[1] == 0xD8 {
		return "image/jpeg"
	}
	if data[0] == 'G' && data[1] == 'I' && data[2] == 'F' {
		return "image/gif"
	}
	if data[0] == 'R' && data[1] == 'I' && data[2] == 'F' && data[3] == 'F' {
		return "image/webp"
	}
	return "application/octet-stream"
}

// EmbedImageFile reads an image from disk and embeds it.
func (e *LLamaVLEmbedder) EmbedImageFile(path string) ([]float32, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return e.EmbedImage(data)
}

// ProbeDim sends a test text embedding request to the running llama-server
// and returns the actual embedding dimension from the response vector length.
func ProbeDim(baseURL string) (int, error) {
	type reqBody struct {
		Input []string `json:"input"`
		Model string   `json:"model"`
	}
	body, _ := json.Marshal(reqBody{Input: []string{"probe"}, Model: "vl"})
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

// ImageToBase64 converts raw image bytes to a base64 data URI.
func ImageToBase64(data []byte) string {
	ct := detectMIME(data)
	b64 := base64.StdEncoding.EncodeToString(data)
	return "data:" + ct + ";base64," + b64
}
