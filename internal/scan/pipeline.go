package scan

import (
	"context"
	"database/sql"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	_ "image/png"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"

	"vecura/internal/db"
	"vecura/internal/embedder"
	"vecura/internal/models"
	"vecura/internal/vector"
)

var imageExts = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".webp": true, ".bmp": true, ".gif": true,
}

// Progress reports scan progress to subscribers.
type Progress struct {
	Total    int
	Done     int
	Current  string
	Finished bool
}

// Pipeline ties together db, embedder registry and the vector store.
type Pipeline struct {
	db       *sql.DB
	imgRepo  *db.ImageRepo
	embRepo  *db.EmbeddingRepo
	models   *models.Registry
	store    *vector.VectorStore
	cache    *embedder.Cache
	thumbDir string

	vlEmbedder embedder.Embedder // optional VL embedder for image embeddings
	Logf       func(string, ...interface{})

	progMu   sync.Mutex
	progress Progress

	dbMu sync.Mutex // serializes all DB writes (SQLite is single-writer)

	subMu sync.Mutex
	sub   map[chan Progress]struct{}
}

// NewPipeline builds a scan pipeline.
func NewPipeline(d *sql.DB, m *models.Registry, s *vector.VectorStore, thumbDir string) *Pipeline {
	return &Pipeline{
		db:       d,
		imgRepo:  db.NewImageRepo(d),
		embRepo:  db.NewEmbeddingRepo(d),
		models:   m,
		store:    s,
		cache:    embedder.NewCache(),
		thumbDir: thumbDir,
		sub:      make(map[chan Progress]struct{}),
		Logf:     func(f string, a ...interface{}) { log.Printf(f, a...) },
	}
}

// NewPipelineFromStore builds a pipeline sharing the same DB/models but a
// different vector store (used to rebuild an independent store from DB).
func NewPipelineFromStore(p *Pipeline, s *vector.VectorStore) *Pipeline {
	return &Pipeline{
		db:         p.db,
		imgRepo:    p.imgRepo,
		embRepo:    p.embRepo,
		models:     p.models,
		store:      s,
		cache:      p.cache,
		thumbDir:   p.thumbDir,
		vlEmbedder: p.vlEmbedder,
		sub:        make(map[chan Progress]struct{}),
	}
}

// Subscribe registers a channel for progress updates.
func (p *Pipeline) Subscribe(ch chan Progress) {
	p.subMu.Lock()
	p.sub[ch] = struct{}{}
	p.subMu.Unlock()
}

// Unsubscribe removes a progress channel.
func (p *Pipeline) Unsubscribe(ch chan Progress) {
	p.subMu.Lock()
	delete(p.sub, ch)
	p.subMu.Unlock()
}

// SetVLEmbedder configures an optional VL embedder. When set, the scan
// pipeline will also create visual embeddings for each image during scan.
func (p *Pipeline) SetVLEmbedder(e embedder.Embedder) {
	p.vlEmbedder = e
}

// ImageRepo exposes the underlying image repository for the API layer.
func (p *Pipeline) ImageRepo() *db.ImageRepo { return p.imgRepo }

func (p *Pipeline) emit() {
	p.progMu.Lock()
	cur := p.progress
	p.progMu.Unlock()
	p.subMu.Lock()
	for ch := range p.sub {
		select {
		case ch <- cur:
		default:
		}
	}
	p.subMu.Unlock()
}

// BuildFromDB loads persisted embeddings into the store at boot.
func (p *Pipeline) BuildFromDB() error {
	rows, err := p.embRepo.LoadAllEmbeddings()
	if err != nil {
		return err
	}
	vrows := make([]vector.EmbeddingRow, len(rows))
	for i, r := range rows {
		vrows[i] = vector.EmbeddingRow{
			ImageID:  r.ImageID,
			Provider: r.Provider,
			ModelID:  r.ModelID,
			Dim:      r.Dim,
			Vector:   r.Vector,
		}
	}
	p.store.BuildFromRows(vrows)
	return nil
}

// scanWorkers bounds how many files are decoded/thumbnailed in parallel. It
// also bounds how many concurrent embedding requests are in flight, since
// each worker embeds its own batches independently (see ScanFolder). It
// defaults to the number of logical CPUs (decode/resize/encode is CPU-bound)
// and can be overridden with VECURA_SCAN_WORKERS for tuning against a
// provider's rate limits.
var scanWorkers = envInt("VECURA_SCAN_WORKERS", runtime.NumCPU())

// scanBatch is how many embeddings are sent per embed request for models
// that don't specify their own batch size. Override with VECURA_SCAN_BATCH.
var scanBatch = envInt("VECURA_SCAN_BATCH", 32)

// envInt reads a positive integer from an environment variable, falling
// back to def when unset, empty, or invalid.
func envInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return def
	}
	return n
}

// ScanFolder walks a folder, generating thumbnails and embeddings for new
// images using the given model key. Work is parallelized across scanWorkers
// goroutines (image decode, thumbnail generation, embedding requests, DB
// writes). Each worker batches its embedding calls (scanBatch per call) and
// issues them independently and concurrently: OpenAI-compatible embedders
// are plain stateless HTTP calls, so there is no correctness reason to
// serialize them, and doing so previously capped scan throughput at one
// in-flight HTTP request no matter how many workers were running. Already-
// embedded images are skipped, making a re-scan of the same folder nearly
// instant.
func (p *Pipeline) ScanFolder(ctx context.Context, root, modelKey string) error {
	e, ok := p.models.Get(modelKey)
	if !ok {
		return fmt.Errorf("model not registered: %s", modelKey)
	}

	files, err := collectImages(root)
	if err != nil {
		return err
	}
	sort.Strings(files)

	p.progMu.Lock()
	p.progress = Progress{Total: len(files), Done: 0, Finished: false}
	p.progMu.Unlock()
	p.emit()

	known, err := p.imgRepo.GetAllImages()
	if err != nil {
		return fmt.Errorf("preload images: %w", err)
	}
	embedded, err := p.embRepo.GetEmbeddedImageIDs("remote", e.Key())
	if err != nil {
		return fmt.Errorf("preload embeddings: %w", err)
	}

	// Load VL-embedded set if VL embedder is configured.
	var vlEmb map[int32]bool
	if p.vlEmbedder != nil {
		vlEmb, err = p.embRepo.GetEmbeddedImageIDs("local", p.vlEmbedder.Key())
		if err != nil {
			return fmt.Errorf("preload VL embeddings: %w", err)
		}
	}

	fileCh := make(chan string)
	var wg sync.WaitGroup
	batch := effectiveBatch(e)
	for i := 0; i < scanWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p.workerScan(ctx, fileCh, e, batch, known, embedded, vlEmb)
		}()
	}

feed:
	for _, f := range files {
		select {
		case <-ctx.Done():
			break feed
		case fileCh <- f:
		}
	}
	close(fileCh)
	wg.Wait()

	p.progMu.Lock()
	p.progress.Finished = true
	p.progMu.Unlock()
	p.emit()
	return nil
}

// workerScan pulls file paths and processes them in batches. known and
// embedded are read-only snapshots taken once by ScanFolder before any
// worker starts, so per-file decisions (new / unchanged / prompt-changed /
// already embedded) are plain map lookups instead of a DB round trip.
func (p *Pipeline) workerScan(ctx context.Context, fileCh <-chan string, e embedder.Embedder, batch int, known map[string]db.ImageIDPrompt, embedded map[int32]bool, vlEmb map[int32]bool) {
	prompts := make([]string, 0, batch)
	ids := make([]int32, 0, batch)
	paths := make([]string, 0, batch)
	var vlPending sync.Map // imageID -> path for images needing VL embedding
	flush := func() {
		if len(prompts) == 0 {
			return
		}
		p.flushBatch(e, prompts, ids, paths, &vlPending)
		prompts = prompts[:0]
		ids = ids[:0]
		paths = paths[:0]
	}

	for f := range fileCh {
		// Count every file exactly once so progress always reaches 100%,
		// even if the file is skipped, errors, or panics below.
		p.incDone()
		if ctx.Err() != nil {
			return
		}
		func() {
			defer func() {
				if r := recover(); r != nil {
					p.Logf("[scan] recovered panic on %s: %v", f, r)
				}
			}()
			if !thumbnailUpToDate(f, p.thumbDir) {
				_ = generateThumbnail(f, p.thumbDir)
			}

			rawPrompt, embedText := ExtractPromptData(f)
			if rawPrompt == "" {
				rawPrompt = filepath.Base(f)
				embedText = rawPrompt
			}
			prompt := rawPrompt

			row, exists := known[f]
			var id int32
			has := false
			needsUpsert := !exists || row.Prompt != prompt
			needReembed := false
			if exists {
				id = row.ID
				has = embedded[id]
				if has {
					switch {
					case row.Prompt == "" || row.Prompt == filepath.Base(f):
						needReembed = prompt != filepath.Base(f)
					case row.Prompt != prompt:
						needReembed = true
					}
				}
			}

			if needsUpsert || needReembed {
				p.dbMu.Lock()
				if needsUpsert {
					newID, uerr := p.imgRepo.UpsertImage(f, prompt)
					if uerr != nil {
						p.dbMu.Unlock()
						p.Logf("[scan] upsert failed for %s: %v", f, uerr)
						return
					}
					id = newID
				}
				if needReembed {
					if derr := p.embRepo.DeleteEmbedding(id, "remote", e.Key()); derr != nil {
						p.Logf("[scan] delete stale embedding for %s: %v", f, derr)
					}
					// Also delete stale VL embedding.
					if p.vlEmbedder != nil {
						_ = p.embRepo.DeleteEmbedding(id, "local", p.vlEmbedder.Key())
					}
					has = false
				}
				p.dbMu.Unlock()
			}

			// Check VL embedding status for skip logic.
			needsVL := false
			if p.vlEmbedder != nil && id > 0 {
				if vlEmb == nil || !vlEmb[id] {
					needsVL = true
				}
			}

			// Incremental: skip images already embedded with this model,
			// unless we just decided to re-embed above.
			if has && !needsVL {
				return
			}
			if !has {
				if v, ok := p.cache.Get(f, embedText, e.Key()); ok {
					p.store.Add(id, "remote", e.Key(), v)
					// Still need VL even if text is cached.
					if needsVL {
						prompts = append(prompts, embedText)
						ids = append(ids, id)
						paths = append(paths, f)
					}
					return
				}
			}
			if !has {
				prompts = append(prompts, embedText)
				ids = append(ids, id)
				paths = append(paths, f)
				if len(prompts) >= batch {
					flush()
				}
			}
			// Collect images that need VL embedding.
			if needsVL {
				vlPending.Store(id, f)
			}
		}()
	}
	flush()

	// VL embedding pass: create visual embeddings for all collected images.
	if p.vlEmbedder != nil {
		vlPending.Range(func(key, value interface{}) bool {
			id := key.(int32)
			f := value.(string)
			data, err := os.ReadFile(f)
			if err != nil {
				p.Logf("[scan] read image for VL embedding failed %s: %v", f, err)
				return true
			}
			vec, err := p.vlEmbedder.(*embedder.LLamaVLEmbedder).EmbedImage(data)
			if err != nil {
				p.Logf("[scan] VL embed failed for %s: %v", f, err)
				return true
			}
			if len(vec) == 0 {
				return true
			}
			p.store.Add(id, "local", p.vlEmbedder.Key(), vec)
			p.dbMu.Lock()
			err = p.embRepo.BatchInsertEmbeddings([]db.EmbeddingRow{{
				ImageID: id, Provider: "local", ModelID: p.vlEmbedder.Key(), Dim: len(vec), Vector: vec,
			}})
			p.dbMu.Unlock()
			if err != nil {
				p.Logf("[scan] persist VL embedding failed for %s: %v", f, err)
			}
			return true
		})
	}
}

// effectiveBatch returns the embedding batch size for the model. For the
// OpenAI-compatible remote embedder this honors the per-model Batch setting
// (e.g. 128); other embedders fall back to scanBatch.
func effectiveBatch(e embedder.Embedder) int {
	if re, ok := e.(*embedder.OpenAICompatibleEmbedder); ok && re.Batch > 0 {
		return re.Batch
	}
	return scanBatch
}

// flushBatch embeds a batch of prompts and persists the resulting vectors.
// Progress for these files was already counted when they were pulled from the
// channel, so this function does not touch the progress counter. The
// thumbnail for each path was already generated by workerScan before the
// file was queued here, so it is not regenerated (that used to double the
// decode/resize/encode cost for every newly embedded image).
func (p *Pipeline) flushBatch(e embedder.Embedder, prompts []string, ids []int32, paths []string, _ *sync.Map) {
	vecs, err := e.Embed(prompts)
	if err != nil {
		p.Logf("[scan] embed failed for %d items: %v", len(prompts), err)
		return
	}
	rows := make([]db.EmbeddingRow, 0, len(prompts))
	for i, prompt := range prompts {
		if i >= len(vecs) {
			break
		}
		vec := vecs[i]
		if len(vec) == 0 {
			continue
		}
		id := ids[i]
		rows = append(rows, db.EmbeddingRow{
			ImageID: id, Provider: "remote", ModelID: e.Key(), Dim: e.Dim(), Vector: vec,
		})
		p.cache.Put(paths[i], prompt, e.Key(), vec)
		p.store.Add(id, "remote", e.Key(), vec)
	}
	if len(rows) > 0 {
		p.dbMu.Lock()
		err := p.embRepo.BatchInsertEmbeddings(rows)
		p.dbMu.Unlock()
		if err != nil {
			p.Logf("[scan] persist embeddings failed: %v", err)
		}
	}
}

// incDone advances progress by one processed file.
func (p *Pipeline) incDone() {
	p.progMu.Lock()
	p.progress.Done++
	p.progMu.Unlock()
	p.emit()
}

// collectImages walks root and returns every file with a recognized image
// extension. It uses filepath.WalkDir (not the older filepath.Walk): WalkDir
// consumes the os.DirEntry values that ReadDir already produced, whereas
// Walk re-stats every entry itself, so WalkDir avoids one Lstat syscall per
// file — a measurable difference when scanning folders with tens of
// thousands of images.
func collectImages(root string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if imageExts[ext] {
			out = append(out, path)
		}
		return nil
	})
	return out, err
}

// thumbnailSize is the longest edge, in pixels, of generated thumbnails.
const thumbnailSize = 256

// thumbnailPath returns where src's thumbnail lives inside thumbDir.
func thumbnailPath(src, thumbDir string) string {
	return filepath.Join(thumbDir, filepath.Base(src)+".png")
}

// thumbnailUpToDate reports whether src already has a generated thumbnail,
// so the (relatively expensive) decode/resize/encode in generateThumbnail
// can be skipped for files a previous scan already handled.
func thumbnailUpToDate(src, thumbDir string) bool {
	if thumbDir == "" {
		return true
	}
	_, err := os.Stat(thumbnailPath(src, thumbDir))
	return err == nil
}

// generateThumbnail creates a thumbnail in thumbDir (pure-Go, PNG). Best-
// effort: decode/encode failures are ignored so one corrupt or unsupported
// image never aborts the whole scan.
func generateThumbnail(src, thumbDir string) error {
	if thumbDir == "" {
		return nil
	}
	f, err := os.Open(src)
	if err != nil {
		return nil
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return nil
	}
	resized := resizeTo(img, thumbnailSize)
	if err := os.MkdirAll(thumbDir, 0o755); err != nil {
		return nil
	}
	dst, err := os.Create(thumbnailPath(src, thumbDir))
	if err != nil {
		return nil
	}
	defer dst.Close()
	_ = png.Encode(dst, resized)
	return nil
}

// resizeTo scales an image so its longest side equals n while preserving the
// original aspect ratio (nearest-neighbor). This keeps thumbnails
// rectangular instead of forcing them into a square and distorting them.
func resizeTo(src image.Image, n int) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w == 0 || h == 0 {
		return src
	}
	longest := w
	if h > longest {
		longest = h
	}
	scale := float64(n) / float64(longest)
	nw := int(float64(w) * scale)
	nh := int(float64(h) * scale)
	if nw < 1 {
		nw = 1
	}
	if nh < 1 {
		nh = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	for y := 0; y < nh; y++ {
		sy := b.Min.Y + int(float64(y)*float64(h)/float64(nh))
		for x := 0; x < nw; x++ {
			sx := b.Min.X + int(float64(x)*float64(w)/float64(nw))
			dst.Set(x, y, src.At(sx, sy))
		}
	}
	return dst
}
