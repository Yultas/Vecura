package embedder

// Embedder generates vector embeddings for text. Implementations may be local
// or remote HTTP API.
type Embedder interface {
	// Key returns the model key "provider/model_id".
	Key() string
	// Dim returns the vector dimensionality.
	Dim() int
	// Embed returns one embedding per input text.
	Embed(texts []string) ([][]float32, error)
}
