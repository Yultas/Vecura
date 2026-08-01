package db

import (
	"database/sql"
	"fmt"
)

// EmbeddingRow is a single persisted embedding for an (image, provider, model).
type EmbeddingRow struct {
	ImageID  int32
	Provider string
	ModelID  string
	Dim      int
	Vector   []float32
}

// EmbeddingRepo persists embeddings and loads them for boot.
type EmbeddingRepo struct {
	db *sql.DB
}

func NewEmbeddingRepo(d *sql.DB) *EmbeddingRepo { return &EmbeddingRepo{db: d} }

// BatchInsertEmbeddings performs a transactional bulk insert of embeddings.
func (r *EmbeddingRepo) BatchInsertEmbeddings(rows []EmbeddingRow) error {
	if len(rows) == 0 {
		return nil
	}
	tx, err := r.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	stmt, err := tx.Prepare(
		`INSERT INTO embeddings (image_id, provider, model_id, dim, vector)
         VALUES (?, ?, ?, ?, ?)
         ON CONFLICT(image_id, provider, model_id) DO UPDATE SET dim=excluded.dim, vector=excluded.vector`)
	if err != nil {
		tx.Rollback()
		return fmt.Errorf("prepare: %w", err)
	}
	defer stmt.Close()
	for _, row := range rows {
		if len(row.Vector) != row.Dim {
			tx.Rollback()
			return fmt.Errorf("dim mismatch: len=%d dim=%d", len(row.Vector), row.Dim)
		}
		if _, err := stmt.Exec(row.ImageID, row.Provider, row.ModelID, row.Dim, float32ToBlob(row.Vector)); err != nil {
			tx.Rollback()
			return fmt.Errorf("exec embed: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// LoadAllEmbeddings returns all stored embeddings. The Vector field shares
// memory with the underlying BLOB rows (no copy).
func (r *EmbeddingRepo) LoadAllEmbeddings() ([]EmbeddingRow, error) {
	return r.GetEmbeddingsByModel("", "")
}

// DeleteEmbedding removes a single (image, provider, model) embedding. Used by
// the scan pipeline to force a re-embed when an image's prompt text changes.
func (r *EmbeddingRepo) DeleteEmbedding(imageID int32, provider, modelID string) error {
	if _, err := r.db.Exec(
		`DELETE FROM embeddings WHERE image_id = ? AND provider = ? AND model_id = ?`,
		imageID, provider, modelID); err != nil {
		return fmt.Errorf("delete embedding: %w", err)
	}
	return nil
}

// HasEmbedding reports whether an embedding already exists for the given
// (image, provider, model) triple. Used to skip already-indexed images so
// re-scanning a folder is cheap.
func (r *EmbeddingRepo) HasEmbedding(imageID int32, provider, modelID string) (bool, error) {
	var n int
	err := r.db.QueryRow(
		`SELECT 1 FROM embeddings WHERE image_id = ? AND provider = ? AND model_id = ? LIMIT 1`,
		imageID, provider, modelID,
	).Scan(&n)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("has embedding: %w", err)
	}
	return true, nil
}

// GetEmbeddedImageIDs returns the set of image ids that already have an
// embedding for the given (provider, modelID). The scan pipeline loads this
// once per scan so checking whether a file still needs embedding is a
// memory lookup instead of one HasEmbedding query per file.
func (r *EmbeddingRepo) GetEmbeddedImageIDs(provider, modelID string) (map[int32]bool, error) {
	rows, err := r.db.Query(
		`SELECT image_id FROM embeddings WHERE provider = ? AND model_id = ?`,
		provider, modelID)
	if err != nil {
		return nil, fmt.Errorf("get embedded image ids: %w", err)
	}
	defer rows.Close()

	out := make(map[int32]bool)
	for rows.Next() {
		var id int32
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan embedded id: %w", err)
		}
		out[id] = true
	}
	return out, rows.Err()
}

// GetEmbeddingsByModel returns embeddings for a specific (provider, modelID).
// Empty provider/modelID returns all embeddings.
func (r *EmbeddingRepo) GetEmbeddingsByModel(provider, modelID string) ([]EmbeddingRow, error) {
	var (
		rows *sql.Rows
		err  error
	)
	if provider == "" {
		rows, err = r.db.Query(
			`SELECT image_id, provider, model_id, dim, vector FROM embeddings`)
	} else {
		rows, err = r.db.Query(
			`SELECT image_id, provider, model_id, dim, vector FROM embeddings
             WHERE provider = ? AND model_id = ?`, provider, modelID)
	}
	if err != nil {
		return nil, fmt.Errorf("query embeddings: %w", err)
	}
	defer rows.Close()

	var out []EmbeddingRow
	for rows.Next() {
		var (
			row  EmbeddingRow
			blob []byte
		)
		if err := rows.Scan(&row.ImageID, &row.Provider, &row.ModelID, &row.Dim, &blob); err != nil {
			return nil, fmt.Errorf("scan embedding: %w", err)
		}
		row.Vector = blobToFloat32(blob)
		out = append(out, row)
	}
	return out, rows.Err()
}
