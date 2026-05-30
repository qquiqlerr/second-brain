// Package sqlitevec implements port/out.VectorIndex backed by SQLite plus
// the sqlite-vec extension (vec0 virtual table).
package sqlitevec

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	sqlitevec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	_ "github.com/mattn/go-sqlite3"

	"github.com/aleksejmetlusko/second-brain/internal/domain"
	portout "github.com/aleksejmetlusko/second-brain/internal/port/out"
)

// Config is the constructor input.
type Config struct {
	DBPath         string
	EmbeddingDim   int
	EmbeddingModel string
}

// Index implements port/out.VectorIndex.
type Index struct {
	db  *sql.DB
	dim int
}

// Open creates or opens the DB, registers the vec0 extension, applies the
// schema, and rebuilds the vec table if EmbeddingDim or EmbeddingModel
// changed.
func Open(ctx context.Context, cfg Config) (*Index, error) {
	sqlitevec.Auto() // registers the vec extension on every future sqlite3 conn

	db, err := sql.Open("sqlite3", cfg.DBPath)
	if err != nil {
		return nil, fmt.Errorf("%w: open: %w", domain.ErrVectorIndex, err)
	}
	db.SetMaxOpenConns(1) // SQLite + sqlite-vec are happiest single-conn

	if err := applySchema(ctx, db, cfg.EmbeddingDim); err != nil {
		return nil, fmt.Errorf("%w: schema: %w", domain.ErrVectorIndex, err)
	}

	prevModel, prevDim, err := readIndexMeta(ctx, db)
	if err != nil {
		return nil, fmt.Errorf("%w: read meta: %w", domain.ErrVectorIndex, err)
	}
	if prevDim != 0 && prevDim != cfg.EmbeddingDim {
		slog.Warn("embedding dim changed, dropping notes_vec and notes_meta", "old_dim", prevDim, "new_dim", cfg.EmbeddingDim)
		if _, err := db.ExecContext(ctx, `DROP TABLE IF EXISTS notes_vec`); err != nil {
			return nil, fmt.Errorf("%w: drop vec: %w", domain.ErrVectorIndex, err)
		}
		if _, err := db.ExecContext(ctx, `DELETE FROM notes_meta`); err != nil {
			return nil, fmt.Errorf("%w: clear meta: %w", domain.ErrVectorIndex, err)
		}
		if err := applySchema(ctx, db, cfg.EmbeddingDim); err != nil {
			return nil, fmt.Errorf("%w: re-create schema: %w", domain.ErrVectorIndex, err)
		}
	} else if prevModel != "" && prevModel != cfg.EmbeddingModel {
		slog.Warn("embedding model changed (same dim), clearing index", "old", prevModel, "new", cfg.EmbeddingModel)
		if _, err := db.ExecContext(ctx, `DELETE FROM notes_vec`); err != nil {
			return nil, fmt.Errorf("%w: clear vec: %w", domain.ErrVectorIndex, err)
		}
		if _, err := db.ExecContext(ctx, `DELETE FROM notes_meta`); err != nil {
			return nil, fmt.Errorf("%w: clear meta: %w", domain.ErrVectorIndex, err)
		}
	}
	if err := writeIndexMeta(ctx, db, cfg.EmbeddingModel, cfg.EmbeddingDim); err != nil {
		return nil, err
	}
	return &Index{db: db, dim: cfg.EmbeddingDim}, nil
}

// Close releases the DB.
func (i *Index) Close() error { return i.db.Close() }

// Upsert inserts/updates rows. Does NOT touch linked_notes — use UpdateLinkedNotes.
func (i *Index) Upsert(ctx context.Context, items []portout.IndexItem) error {
	if len(items) == 0 {
		return nil
	}
	for _, it := range items {
		if len(it.Embedding) != i.dim {
			return fmt.Errorf("%w: item %s has dim %d, want %d", domain.ErrVectorIndex, it.ID, len(it.Embedding), i.dim)
		}
	}

	tx, err := i.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("%w: begin: %w", domain.ErrVectorIndex, err)
	}
	defer tx.Rollback() //nolint:errcheck // commit path returns ErrTxDone we don't care about

	metaStmt, err := tx.PrepareContext(ctx, `
		INSERT INTO notes_meta(id, file_path, body_hash, category, tags_json, date, kind, indexed_at, linked_notes)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, '[]')
		ON CONFLICT(id) DO UPDATE SET
		  file_path  = excluded.file_path,
		  body_hash  = excluded.body_hash,
		  category   = excluded.category,
		  tags_json  = excluded.tags_json,
		  date       = excluded.date,
		  kind       = excluded.kind,
		  indexed_at = excluded.indexed_at`)
	if err != nil {
		return fmt.Errorf("%w: prep meta: %w", domain.ErrVectorIndex, err)
	}
	defer metaStmt.Close()

	vecDelStmt, err := tx.PrepareContext(ctx, `DELETE FROM notes_vec WHERE id = ?`)
	if err != nil {
		return fmt.Errorf("%w: prep vec del: %w", domain.ErrVectorIndex, err)
	}
	defer vecDelStmt.Close()
	vecInsStmt, err := tx.PrepareContext(ctx, `INSERT INTO notes_vec(id, embedding) VALUES (?, ?)`)
	if err != nil {
		return fmt.Errorf("%w: prep vec ins: %w", domain.ErrVectorIndex, err)
	}
	defer vecInsStmt.Close()

	now := time.Now().UTC().Format(time.RFC3339)
	for _, it := range items {
		tagsJSON, err := json.Marshal(it.Tags)
		if err != nil {
			return fmt.Errorf("%w: marshal tags: %w", domain.ErrVectorIndex, err)
		}
		if _, err := metaStmt.ExecContext(ctx, it.ID, it.FilePath, it.BodyHash, it.Category, string(tagsJSON), it.Date.UTC().Format(time.RFC3339), it.Kind, now); err != nil {
			return fmt.Errorf("%w: upsert meta: %w", domain.ErrVectorIndex, err)
		}
		if _, err := vecDelStmt.ExecContext(ctx, it.ID); err != nil {
			return fmt.Errorf("%w: vec del: %w", domain.ErrVectorIndex, err)
		}
		blob, err := serializeVector(it.Embedding)
		if err != nil {
			return fmt.Errorf("%w: serialize vec for %s: %w", domain.ErrVectorIndex, it.ID, err)
		}
		if _, err := vecInsStmt.ExecContext(ctx, it.ID, blob); err != nil {
			return fmt.Errorf("%w: vec ins: %w", domain.ErrVectorIndex, err)
		}
	}
	return tx.Commit()
}

// SearchByVector returns top-K matches sorted by ascending L2 distance
// (rendered as descending Score in the result).
func (i *Index) SearchByVector(ctx context.Context, vec []float32, q portout.SearchQuery) ([]portout.SearchHit, error) {
	if q.TopK <= 0 {
		q.TopK = 10
	}
	if len(vec) != i.dim {
		return nil, fmt.Errorf("%w: query dim %d != index dim %d", domain.ErrVectorIndex, len(vec), i.dim)
	}

	blob, err := serializeVector(vec)
	if err != nil {
		return nil, fmt.Errorf("%w: serialize query: %w", domain.ErrVectorIndex, err)
	}

	var whereClauses []string
	args := []any{blob, q.TopK}
	if len(q.KindFilter) > 0 {
		placeholders := strings.TrimRight(strings.Repeat("?,", len(q.KindFilter)), ",")
		whereClauses = append(whereClauses, "m.kind IN ("+placeholders+")")
		for _, k := range q.KindFilter {
			args = append(args, k)
		}
	}
	if q.CategoryPrefix != "" {
		whereClauses = append(whereClauses, "m.category LIKE ?")
		args = append(args, q.CategoryPrefix+"%")
	}
	if !q.DateFrom.IsZero() {
		whereClauses = append(whereClauses, "m.date >= ?")
		args = append(args, q.DateFrom.UTC().Format(time.RFC3339))
	}
	whereSQL := ""
	if len(whereClauses) > 0 {
		whereSQL = " AND " + strings.Join(whereClauses, " AND ")
	}

	query := `
		SELECT v.id, v.distance, m.file_path, m.body_hash, m.category, m.tags_json, m.date, m.kind, m.indexed_at, m.linked_notes
		FROM notes_vec v
		JOIN notes_meta m ON m.id = v.id
		WHERE v.embedding MATCH ? AND k = ?` + whereSQL + `
		ORDER BY v.distance ASC`

	rows, err := i.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("%w: search: %w", domain.ErrVectorIndex, err)
	}
	defer rows.Close()

	var hits []portout.SearchHit
	for rows.Next() {
		var (
			h            portout.SearchHit
			distance     float64
			tagsJSON     string
			linksRaw     string
			dateStr      string
			indexedAtStr string
		)
		if err := rows.Scan(&h.ID, &distance, &h.Meta.FilePath, &h.Meta.BodyHash, &h.Meta.Category, &tagsJSON, &dateStr, &h.Meta.Kind, &indexedAtStr, &linksRaw); err != nil {
			return nil, fmt.Errorf("%w: scan: %w", domain.ErrVectorIndex, err)
		}
		h.Meta.ID = h.ID
		// sqlite-vec returns L2 distance. For unit-norm vectors (OpenAI
		// embeddings are unit-norm), cosine_similarity = 1 - distance²/2,
		// landing in [-1, 1]. We clamp negatives to 0 for display sanity.
		cosSim := 1 - distance*distance/2
		if cosSim < 0 {
			cosSim = 0
		}
		h.Score = float32(cosSim)
		h.Meta.Date, _ = time.Parse(time.RFC3339, dateStr)
		h.Meta.IndexedAt, _ = time.Parse(time.RFC3339, indexedAtStr)
		_ = json.Unmarshal([]byte(tagsJSON), &h.Meta.Tags)
		_ = json.Unmarshal([]byte(linksRaw), &h.Meta.LinkedNotes)
		hits = append(hits, h)
	}
	return hits, rows.Err()
}

// GetMeta loads a meta row by id.
func (i *Index) GetMeta(ctx context.Context, id string) (portout.IndexMeta, bool, error) {
	var (
		m            portout.IndexMeta
		tagsJSON     string
		linksRaw     string
		dateStr      string
		indexedAtStr string
	)
	err := i.db.QueryRowContext(ctx, `SELECT id, file_path, body_hash, category, tags_json, date, kind, indexed_at, linked_notes FROM notes_meta WHERE id = ?`, id).
		Scan(&m.ID, &m.FilePath, &m.BodyHash, &m.Category, &tagsJSON, &dateStr, &m.Kind, &indexedAtStr, &linksRaw)
	if errors.Is(err, sql.ErrNoRows) {
		return portout.IndexMeta{}, false, nil
	}
	if err != nil {
		return portout.IndexMeta{}, false, fmt.Errorf("%w: get meta: %w", domain.ErrVectorIndex, err)
	}
	m.Date, _ = time.Parse(time.RFC3339, dateStr)
	m.IndexedAt, _ = time.Parse(time.RFC3339, indexedAtStr)
	_ = json.Unmarshal([]byte(tagsJSON), &m.Tags)
	_ = json.Unmarshal([]byte(linksRaw), &m.LinkedNotes)
	return m, true, nil
}

// GetEmbedding loads the stored vector by id.
func (i *Index) GetEmbedding(ctx context.Context, id string) ([]float32, bool, error) {
	var blob []byte
	err := i.db.QueryRowContext(ctx, `SELECT embedding FROM notes_vec WHERE id = ?`, id).Scan(&blob)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("%w: get embedding: %w", domain.ErrVectorIndex, err)
	}
	return deserializeVector(blob), true, nil
}

// UpdateLinkedNotes overwrites the linked_notes JSON column for id.
func (i *Index) UpdateLinkedNotes(ctx context.Context, id string, links []string) error {
	if links == nil {
		links = []string{}
	}
	raw, err := json.Marshal(links)
	if err != nil {
		return fmt.Errorf("%w: marshal links: %w", domain.ErrVectorIndex, err)
	}
	res, err := i.db.ExecContext(ctx, `UPDATE notes_meta SET linked_notes = ? WHERE id = ?`, string(raw), id)
	if err != nil {
		return fmt.Errorf("%w: update links: %w", domain.ErrVectorIndex, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("%w: id %q not found", domain.ErrVectorIndex, id)
	}
	return nil
}

// DeleteByIDs removes rows from both tables in one tx.
func (i *Index) DeleteByIDs(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	tx, err := i.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("%w: begin: %w", domain.ErrVectorIndex, err)
	}
	defer tx.Rollback() //nolint:errcheck // commit path returns ErrTxDone we don't care about
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx, `DELETE FROM notes_meta WHERE id = ?`, id); err != nil {
			return fmt.Errorf("%w: del meta: %w", domain.ErrVectorIndex, err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM notes_vec WHERE id = ?`, id); err != nil {
			return fmt.Errorf("%w: del vec: %w", domain.ErrVectorIndex, err)
		}
	}
	return tx.Commit()
}

// ListAllMeta returns all meta rows.
func (i *Index) ListAllMeta(ctx context.Context) ([]portout.IndexMeta, error) {
	rows, err := i.db.QueryContext(ctx, `SELECT id, file_path, body_hash, category, tags_json, date, kind, indexed_at, linked_notes FROM notes_meta`)
	if err != nil {
		return nil, fmt.Errorf("%w: list meta: %w", domain.ErrVectorIndex, err)
	}
	defer rows.Close()
	var out []portout.IndexMeta
	for rows.Next() {
		var (
			m            portout.IndexMeta
			tagsJSON     string
			linksRaw     string
			dateStr      string
			indexedAtStr string
		)
		if err := rows.Scan(&m.ID, &m.FilePath, &m.BodyHash, &m.Category, &tagsJSON, &dateStr, &m.Kind, &indexedAtStr, &linksRaw); err != nil {
			return nil, fmt.Errorf("%w: scan: %w", domain.ErrVectorIndex, err)
		}
		m.Date, _ = time.Parse(time.RFC3339, dateStr)
		m.IndexedAt, _ = time.Parse(time.RFC3339, indexedAtStr)
		_ = json.Unmarshal([]byte(tagsJSON), &m.Tags)
		_ = json.Unmarshal([]byte(linksRaw), &m.LinkedNotes)
		out = append(out, m)
	}
	return out, rows.Err()
}

// serializeVector encodes []float32 as little-endian bytes for the vec0 BLOB column.
// The binding's SerializeFloat32 does the same; inlined here to avoid the dep
// at non-CGO compile time should the import set ever shrink.
func serializeVector(v []float32) ([]byte, error) {
	buf := new(bytes.Buffer)
	if err := binary.Write(buf, binary.LittleEndian, v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func deserializeVector(b []byte) []float32 {
	if len(b)%4 != 0 {
		return nil
	}
	out := make([]float32, len(b)/4)
	_ = binary.Read(bytes.NewReader(b), binary.LittleEndian, out)
	return out
}
