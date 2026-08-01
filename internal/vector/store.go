package vector

import (
	"runtime"
	"sync"
	"sync/atomic"
)

// ModelVectors holds all vectors for one (provider, model_id) pair as a flat
// array. Vectors are never appended past the preallocated Cap during reads.
type ModelVectors struct {
	Dim     int
	Cap     int
	Count   int
	IDs     []int32
	Vectors []float32
	InvNorm []float32
}

// storeSnapshot is the read-only view used by concurrent searches.
type storeSnapshot struct {
	models map[string]*ModelVectors
}

// VectorStore is a model-aware in-memory vector store. Writes grow under Lock
// and publish a new immutable snapshot via atomic pointer.
type VectorStore struct {
	mu   sync.Mutex
	m    map[string]*ModelVectors
	snap atomic.Pointer[storeSnapshot]
}

func modelKey(provider, modelID string) string {
	return provider + "/" + modelID
}

// NewVectorStore creates an empty store and publishes the initial snapshot.
func NewVectorStore() *VectorStore {
	s := &VectorStore{m: make(map[string]*ModelVectors)}
	s.snap.Store(&storeSnapshot{models: s.m})
	return s
}

func (s *VectorStore) getOrCreateLocked(provider, modelID string, dim int) *ModelVectors {
	k := modelKey(provider, modelID)
	mv, ok := s.m[k]
	if !ok {
		mv = &ModelVectors{Dim: dim, Cap: 0, Count: 0}
		s.m[k] = mv
	}
	return mv
}

// Add appends one vector. It grows Cap by 1.5x as needed under Lock, then
// publishes a fresh snapshot.
func (s *VectorStore) Add(imageID int32, provider, modelID string, vec []float32) {
	s.mu.Lock()
	defer s.mu.Unlock()

	mv := s.getOrCreateLocked(provider, modelID, len(vec))
	if mv.Dim != len(vec) {
		// Mismatched dim for this model key is a programming error.
		panic("vector dim mismatch for model key")
	}
	need := mv.Count + 1
	if need > mv.Cap {
		newCap := mv.Cap
		if newCap == 0 {
			newCap = 1024
		}
		for newCap < need {
			newCap = (newCap * 3) / 2
		}
		ids := make([]int32, newCap)
		copy(ids, mv.IDs[:mv.Count])
		vecs := make([]float32, newCap*mv.Dim)
		copy(vecs, mv.Vectors[:mv.Count*mv.Dim])
		inv := make([]float32, newCap)
		copy(inv, mv.InvNorm[:mv.Count])
		mv.IDs = ids
		mv.Vectors = vecs
		mv.InvNorm = inv
		mv.Cap = newCap
	}

	base := mv.Count * mv.Dim
	copy(mv.Vectors[base:], vec)
	mv.IDs[mv.Count] = imageID
	mv.InvNorm[mv.Count] = invNorm(vec)
	mv.Count++

	// Publish immutable snapshot (shallow copy of the map).
	snap := &storeSnapshot{models: make(map[string]*ModelVectors, len(s.m))}
	for k, v := range s.m {
		snap.models[k] = v
	}
	s.snap.Store(snap)
}

// Reset clears all vectors from the store and publishes an empty snapshot.
func (s *VectorStore) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m = make(map[string]*ModelVectors)
	s.snap.Store(&storeSnapshot{models: s.m})
}

// BuildFromRows bulk-loads vectors grouped by (provider, model_id), computing
// InvNorm once at write time. Safe to call before any concurrent reads.
func (s *VectorStore) BuildFromRows(rows []EmbeddingRow) {
	s.mu.Lock()
	defer s.mu.Unlock()

	grouped := make(map[string][]EmbeddingRow)
	for _, r := range rows {
		k := modelKey(r.Provider, r.ModelID)
		grouped[k] = append(grouped[k], r)
	}

	for k, gr := range grouped {
		dim := gr[0].Dim
		mv := &ModelVectors{
			Dim:     dim,
			Cap:     len(gr),
			Count:   len(gr),
			IDs:     make([]int32, len(gr)),
			Vectors: make([]float32, len(gr)*dim),
			InvNorm: make([]float32, len(gr)),
		}
		for i, r := range gr {
			mv.IDs[i] = r.ImageID
			base := i * dim
			copy(mv.Vectors[base:], r.Vector)
			mv.InvNorm[i] = invNorm(r.Vector)
		}
		s.m[k] = mv
	}

	snap := &storeSnapshot{models: make(map[string]*ModelVectors, len(s.m))}
	for k, v := range s.m {
		snap.models[k] = v
	}
	s.snap.Store(snap)
}

// Search returns the Top-K results for query Q in the given model space.
// It reads the immutable snapshot without locking for the duration of search.
func (s *VectorStore) Search(Q []float32, provider, modelID string, K int) []Result {
	snap := s.snap.Load()
	mv, ok := snap.models[modelKey(provider, modelID)]
	if !ok || mv.Count == 0 || K <= 0 {
		return nil
	}
	if len(Q) != mv.Dim {
		return nil
	}

	qNorm := invNorm(Q)

	// Partition vectors across CPUs; each segment computes a local min-heap.
	numCPU := maxInt(1, runtime.NumCPU())
	segSize := (mv.Count + numCPU - 1) / numCPU
	if segSize == 0 {
		segSize = 1
	}

	type segResult struct {
		heap *minHeap
	}
	results := make([]segResult, numCPU)

	var wg sync.WaitGroup
	for seg := 0; seg < numCPU; seg++ {
		start := seg * segSize
		if start >= mv.Count {
			break
		}
		end := start + segSize
		if end > mv.Count {
			end = mv.Count
		}
		h := &minHeap{items: make([]Result, 0, K)}
		results[seg] = segResult{heap: h}
		wg.Add(1)
		go func(start, end int, h *minHeap) {
			defer wg.Done()
			for i := start; i < end; i++ {
				base := i * mv.Dim
				dot := dotProd(Q, mv.Vectors[base:base+mv.Dim])
				score := dot * mv.InvNorm[i] * qNorm
				h.push(Result{ID: mv.IDs[i], Score: score})
			}
		}(start, end, h)
	}
	wg.Wait()

	// Merge segment heaps into the final Top-K.
	merged := &minHeap{items: make([]Result, 0, K)}
	for i := 0; i < numCPU; i++ {
		if results[i].heap == nil {
			continue
		}
		for _, r := range results[i].heap.items {
			merged.push(r)
		}
	}

	out := merged.items
	// Sort descending by score.
	sortSlice(out)
	return out
}
