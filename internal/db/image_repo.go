package db

import (
	"database/sql"
	"fmt"
	"strings"
)

// ImageRepo persists image metadata and tag associations.
type ImageRepo struct {
	db *sql.DB
}

func NewImageRepo(d *sql.DB) *ImageRepo { return &ImageRepo{db: d} }

// UpsertImage inserts or updates an image row, returning its id.
//
// This uses RETURNING instead of LastInsertId(): when the ON CONFLICT branch
// performs an UPDATE rather than an INSERT, SQLite does NOT advance
// last_insert_rowid(), so LastInsertId() would silently return a stale id
// from a previous, unrelated insert on the same connection. That mismatch
// would attach embeddings to the wrong image whenever a folder is re-scanned.
func (r *ImageRepo) UpsertImage(filePath, prompt string) (int32, error) {
	var id int32
	err := r.db.QueryRow(
		`INSERT INTO images (file_path, prompt) VALUES (?, ?)
         ON CONFLICT(file_path) DO UPDATE SET prompt=excluded.prompt
         RETURNING id`,
		filePath, prompt).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("upsert image: %w", err)
	}
	return id, nil
}

// GetImagePath returns the file path for an image id.
func (r *ImageRepo) GetImagePath(id int32) (string, error) {
	var p string
	err := r.db.QueryRow(`SELECT file_path FROM images WHERE id = ?`, id).Scan(&p)
	if err != nil {
		return "", fmt.Errorf("get image path: %w", err)
	}
	return p, nil
}

// GetImagePathPrompt returns the file path and stored prompt for an image id.
func (r *ImageRepo) GetImagePathPrompt(id int32) (string, string, error) {
	var p, pr string
	err := r.db.QueryRow(`SELECT file_path, prompt FROM images WHERE id = ?`, id).Scan(&p, &pr)
	if err != nil {
		return "", "", fmt.Errorf("get image: %w", err)
	}
	return p, pr, nil
}

// ImagePathPrompt is one row returned by GetImagesByIDs.
type ImagePathPrompt struct {
	Path   string
	Prompt string
}

// GetImagesByIDs batch-loads path/prompt for a set of image ids in a single
// query. Search used to call GetImagePathPrompt once per hit, which meant one
// round trip per result (up to K); for large result sets this dominated
// search latency and made the UI feel sluggish. Missing ids are simply
// absent from the returned map.
func (r *ImageRepo) GetImagesByIDs(ids []int32) (map[int32]ImagePathPrompt, error) {
	out := make(map[int32]ImagePathPrompt, len(ids))
	if len(ids) == 0 {
		return out, nil
	}

	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}
	query := "SELECT id, file_path, prompt FROM images WHERE id IN (" +
		strings.Join(placeholders, ",") + ")"

	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("get images by ids: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			id   int32
			path string
			pr   string
		)
		if err := rows.Scan(&id, &path, &pr); err != nil {
			return nil, fmt.Errorf("scan image: %w", err)
		}
		out[id] = ImagePathPrompt{Path: path, Prompt: pr}
	}
	return out, rows.Err()
}

// ImageIDPrompt is one row returned by GetAllImages.
type ImageIDPrompt struct {
	ID     int32
	Prompt string
}

// GetAllImages bulk-loads every indexed image's id and stored prompt, keyed
// by file path. The scan pipeline uses this to decide per-file work (new,
// unchanged, or prompt-changed) purely in memory instead of running
// GetPromptByPath + UpsertImage as separate round trips for every single
// file on every scan.
func (r *ImageRepo) GetAllImages() (map[string]ImageIDPrompt, error) {
	rows, err := r.db.Query(`SELECT id, file_path, prompt FROM images`)
	if err != nil {
		return nil, fmt.Errorf("get all images: %w", err)
	}
	defer rows.Close()

	out := make(map[string]ImageIDPrompt)
	for rows.Next() {
		var (
			id   int32
			path string
			pr   string
		)
		if err := rows.Scan(&id, &path, &pr); err != nil {
			return nil, fmt.Errorf("scan image: %w", err)
		}
		out[path] = ImageIDPrompt{ID: id, Prompt: pr}
	}
	return out, rows.Err()
}

// GetPromptByPath returns the currently stored prompt for a file path (before
// any upsert). The boolean reports whether a row already exists. Used by the
// scan pipeline to decide whether an already-embedded image must be
// re-embedded because its prompt text improved.
func (r *ImageRepo) GetPromptByPath(filePath string) (string, bool, error) {
	var p string
	err := r.db.QueryRow(`SELECT prompt FROM images WHERE file_path = ?`, filePath).Scan(&p)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("get prompt by path: %w", err)
	}
	return p, true, nil
}

// SearchByText returns image ids whose stored prompt or file path contains q
// (case-insensitive substring match). This powers keyword search, which works
// without any embedding model. Results are ordered by rowid (insertion order)
// and capped at K.
func (r *ImageRepo) SearchByText(q string, K int) ([]int32, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return nil, nil
	}
	// Escape SQLite LIKE wildcards so the user query is matched literally.
	esc := strings.ReplaceAll(strings.ReplaceAll(q, `\`, `\\`), "%", `\%`)
	like := "%" + esc + "%"
	rows, err := r.db.Query(
		`SELECT id FROM images
          WHERE prompt LIKE ? ESCAPE '\'
             OR file_path LIKE ? ESCAPE '\'
          ORDER BY id
          LIMIT ?`,
		like, like, K)
	if err != nil {
		return nil, fmt.Errorf("search by text: %w", err)
	}
	defer rows.Close()
	var ids []int32
	for rows.Next() {
		var id int32
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan text id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// AddTag associates a tag (created if absent) with an image.
func (r *ImageRepo) AddTag(imageID int32, tag string) error {
	var tagID int32
	err := r.db.QueryRow(`SELECT id FROM tags WHERE name = ?`, tag).Scan(&tagID)
	if err == sql.ErrNoRows {
		res, insErr := r.db.Exec(`INSERT INTO tags (name) VALUES (?)`, tag)
		if insErr != nil {
			return fmt.Errorf("insert tag: %w", insErr)
		}
		tid, insErr := res.LastInsertId()
		if insErr != nil {
			return fmt.Errorf("tag id: %w", insErr)
		}
		tagID = int32(tid)
	} else if err != nil {
		return fmt.Errorf("select tag: %w", err)
	}
	_, err = r.db.Exec(
		`INSERT OR IGNORE INTO image_tags (image_id, tag_id) VALUES (?, ?)`,
		imageID, tagID)
	if err != nil {
		return fmt.Errorf("link tag: %w", err)
	}
	return nil
}

// GetByTag returns image ids that have the given tag.
func (r *ImageRepo) GetByTag(tag string) ([]int32, error) {
	rows, err := r.db.Query(
		`SELECT it.image_id FROM image_tags it
         JOIN tags t ON t.id = it.tag_id
         WHERE t.name = ?`, tag)
	if err != nil {
		return nil, fmt.Errorf("get by tag: %w", err)
	}
	defer rows.Close()
	var ids []int32
	for rows.Next() {
		var id int32
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan tag id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
