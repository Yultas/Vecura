package db

import (
	"database/sql"
	"fmt"
)

// HistoryRepo stores recent search queries for suggestion dropdowns.
type HistoryRepo struct {
	db *sql.DB
}

func NewHistoryRepo(d *sql.DB) *HistoryRepo { return &HistoryRepo{db: d} }

// AddQuery records a search query (upserting last_used).
func (r *HistoryRepo) AddQuery(q string) error {
	if q == "" {
		return nil
	}
	if _, err := r.db.Exec(
		`INSERT INTO search_history (query) VALUES (?)
         ON CONFLICT(query) DO UPDATE SET last_used=CURRENT_TIMESTAMP`,
		q); err != nil {
		return fmt.Errorf("add query: %w", err)
	}
	return nil
}

// Recent returns up to limit most recently used queries.
func (r *HistoryRepo) Recent(limit int) ([]string, error) {
	rows, err := r.db.Query(
		`SELECT query FROM search_history ORDER BY last_used DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("recent: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var q string
		if err := rows.Scan(&q); err != nil {
			return nil, fmt.Errorf("scan query: %w", err)
		}
		out = append(out, q)
	}
	return out, rows.Err()
}
