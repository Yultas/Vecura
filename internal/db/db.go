package db

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS images (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    file_path  TEXT UNIQUE,
    prompt     TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS tags (
    id   INTEGER PRIMARY KEY,
    name TEXT UNIQUE
);

CREATE TABLE IF NOT EXISTS image_tags (
    image_id INTEGER,
    tag_id   INTEGER,
    PRIMARY KEY (image_id, tag_id)
);
CREATE INDEX IF NOT EXISTS idx_image_tags_tag ON image_tags(tag_id);

CREATE TABLE IF NOT EXISTS embeddings (
    image_id   INTEGER,
    provider   TEXT,
    model_id   TEXT,
    dim        INTEGER,
    vector     BLOB,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (image_id, provider, model_id)
);
CREATE INDEX IF NOT EXISTS idx_embeddings_model ON embeddings(provider, model_id);

CREATE TABLE IF NOT EXISTS search_history (
    query  TEXT PRIMARY KEY,
    last_used DATETIME DEFAULT CURRENT_TIMESTAMP
);
`

// Open opens (creating if needed) the SQLite database at path and applies the
// schema. WAL mode is enabled for transactional batch inserts.
func Open(path string) (*sql.DB, error) {
	d, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if _, err := d.Exec("PRAGMA journal_mode=WAL;"); err != nil {
		d.Close()
		return nil, fmt.Errorf("enable wal: %w", err)
	}
	// WAL's default is synchronous=FULL, which fsyncs on every commit. NORMAL
	// still fsyncs at WAL checkpoints but not after each transaction, which
	// is safe against application crashes in WAL mode (SQLite guarantees the
	// WAL file itself stays consistent) and only risks losing the most recent
	// commits on an OS crash or power loss — an acceptable tradeoff here since
	// this database can always be rebuilt by rescanning. This is the single
	// biggest lever for scan throughput: without it, every per-file write
	// (previously one per file, now only for new/changed files) blocks on a
	// disk fsync.
	if _, err := d.Exec("PRAGMA synchronous=NORMAL;"); err != nil {
		d.Close()
		return nil, fmt.Errorf("set synchronous: %w", err)
	}
	// Concurrent writers (the parallel scan pool) must wait for the write
	// lock instead of failing immediately with SQLITE_BUSY.
	if _, err := d.Exec("PRAGMA busy_timeout=5000;"); err != nil {
		d.Close()
		return nil, fmt.Errorf("busy timeout: %w", err)
	}
	if _, err := d.Exec(schema); err != nil {
		d.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	return d, nil
}
