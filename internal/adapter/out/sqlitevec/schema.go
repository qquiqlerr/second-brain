package sqlitevec

import (
	"context"
	"database/sql"
	"fmt"
)

// applySchema creates tables on a fresh DB or no-ops if they exist.
// `dim` parameterizes the vec0 virtual table column type.
func applySchema(ctx context.Context, db *sql.DB, dim int) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS notes_meta (
            id            TEXT PRIMARY KEY,
            file_path     TEXT NOT NULL,
            body_hash     TEXT NOT NULL,
            category      TEXT NOT NULL,
            tags_json     TEXT NOT NULL,
            date          TEXT NOT NULL,
            kind          TEXT NOT NULL,
            indexed_at    TEXT NOT NULL,
            linked_notes  TEXT NOT NULL DEFAULT '[]'
        )`,
		`CREATE TABLE IF NOT EXISTS index_meta (
            key   TEXT PRIMARY KEY,
            value TEXT NOT NULL
        )`,
		fmt.Sprintf(`CREATE VIRTUAL TABLE IF NOT EXISTS notes_vec USING vec0(
            id        TEXT PRIMARY KEY,
            embedding FLOAT[%d]
        )`, dim),
	}
	for _, s := range stmts {
		if _, err := db.ExecContext(ctx, s); err != nil {
			return fmt.Errorf("exec schema stmt: %w", err)
		}
	}
	return nil
}

// readIndexMeta reads the embedding model/dim recorded in the index_meta table.
// On mismatch with the current config, Open will rebuild the vec table.
func readIndexMeta(ctx context.Context, db *sql.DB) (model string, dim int, _ error) {
	rows, err := db.QueryContext(ctx, `SELECT key, value FROM index_meta`)
	if err != nil {
		return "", 0, err
	}
	defer rows.Close()
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return "", 0, err
		}
		switch k {
		case "embedding_model":
			model = v
		case "embedding_dim":
			_, _ = fmt.Sscanf(v, "%d", &dim)
		}
	}
	return model, dim, rows.Err()
}

func writeIndexMeta(ctx context.Context, db *sql.DB, model string, dim int) error {
	stmts := []struct{ k, v string }{
		{"embedding_model", model},
		{"embedding_dim", fmt.Sprintf("%d", dim)},
	}
	for _, s := range stmts {
		if _, err := db.ExecContext(ctx, `INSERT OR REPLACE INTO index_meta(key, value) VALUES (?, ?)`, s.k, s.v); err != nil {
			return fmt.Errorf("write index_meta: %w", err)
		}
	}
	return nil
}
