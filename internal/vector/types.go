package vector

// EmbeddingRow is a single embedding used to build the in-memory store.
type EmbeddingRow struct {
	ImageID  int32
	Provider string
	ModelID  string
	Dim      int
	Vector   []float32
}

// Result is a single search match.
type Result struct {
	ID    int32
	Score float32
}
