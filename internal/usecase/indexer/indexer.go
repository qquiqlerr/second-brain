package indexer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/aleksejmetlusko/second-brain/internal/domain"
	portout "github.com/aleksejmetlusko/second-brain/internal/port/out"
)

// Indexer is the FS-scanner-driven orchestrator.
type Indexer struct {
	notesDir string
	embedder portout.Embedder
	vec      portout.VectorIndex
	linker   *Linker
	cfg      Config

	mu sync.Mutex // serializes RunOnce calls
}

// New constructs an Indexer.
func New(notesDir string, e portout.Embedder, v portout.VectorIndex, rewriter YAMLRewriter, cfg Config) *Indexer {
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 100
	}
	return &Indexer{
		notesDir: notesDir,
		embedder: e,
		vec:      v,
		linker:   NewLinker(v, rewriter, cfg),
		cfg:      cfg,
	}
}

// RunOnce performs a single scan→diff→embed→upsert→link cycle.
func (i *Indexer) RunOnce(ctx context.Context) error {
	i.mu.Lock()
	defer i.mu.Unlock()

	start := time.Now()
	fsList, paths, bodies, err := i.scan(ctx)
	if err != nil {
		return err
	}

	dbList, err := i.vec.ListAllMeta(ctx)
	if err != nil {
		return err
	}

	toEmbed, toDelete := Diff(fsList, dbList)
	var embeddedIDs []string

	for batchStart := 0; batchStart < len(toEmbed); batchStart += i.cfg.BatchSize {
		end := batchStart + i.cfg.BatchSize
		if end > len(toEmbed) {
			end = len(toEmbed)
		}
		batch := toEmbed[batchStart:end]
		texts := make([]string, len(batch))
		for k, e := range batch {
			texts[k] = domain.CanonicalBody(bodies[e.ID])
		}
		vectors, err := i.embedder.Embed(ctx, texts, portout.EmbedDocument)
		if err != nil {
			return fmt.Errorf("embed batch [%d:%d]: %w", batchStart, end, err)
		}

		items := make([]portout.IndexItem, len(batch))
		for k, e := range batch {
			note := mustParseNote(paths[e.ID])
			items[k] = portout.IndexItem{
				ID:        e.ID,
				FilePath:  paths[e.ID],
				BodyHash:  e.BodyHash,
				Category:  note.Category,
				Tags:      note.Tags,
				Date:      note.Date,
				Kind:      note.Kind,
				Embedding: vectors[k],
			}
			embeddedIDs = append(embeddedIDs, e.ID)
		}
		if err := i.vec.Upsert(ctx, items); err != nil {
			return fmt.Errorf("upsert batch: %w", err)
		}
		// Sync the linked_notes column with what's currently in the YAML
		// file. Upsert leaves the column at its default (`'[]'`), so without
		// this sync the linker would believe stale on-disk links are gone
		// and skip its no-op guard, then accidentally NOT rewrite the file
		// even when the new (temporal) set is empty.
		for _, e := range batch {
			note := mustParseNote(paths[e.ID])
			if err := i.vec.UpdateLinkedNotes(ctx, e.ID, note.LinkedNotes); err != nil {
				slog.Warn("indexer: sync linked_notes from yaml", "id", e.ID, "err", err)
			}
		}
	}

	if len(toDelete) > 0 {
		if err := i.vec.DeleteByIDs(ctx, toDelete); err != nil {
			return fmt.Errorf("delete: %w", err)
		}
	}

	if len(embeddedIDs) > 0 {
		if err := i.linker.Recompute(ctx, embeddedIDs); err != nil {
			// Soft-fail: index is intact, links can catch up next cycle.
			slog.Error("indexer: linker recompute", "err", err)
		}
	}

	slog.Info("indexer.run",
		"duration_ms", time.Since(start).Milliseconds(),
		"n_embedded", len(embeddedIDs),
		"n_deleted", len(toDelete),
	)
	return nil
}

// Loop blocks running RunOnce on a ticker until ctx is cancelled.
func (i *Indexer) Loop(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	// Initial run on startup
	if err := i.RunOnce(ctx); err != nil {
		slog.Error("indexer.startup", "err", err)
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := i.RunOnce(ctx); err != nil {
				slog.Error("indexer.tick", "err", err)
			}
		}
	}
}

// scan walks notesDir collecting FSEntry per *.md and stashes parsed
// note metadata + body for later use.
func (i *Indexer) scan(ctx context.Context) ([]FSEntry, map[string]string, map[string]string, error) {
	var entries []FSEntry
	paths := make(map[string]string)
	bodies := make(map[string]string)
	err := filepath.WalkDir(i.notesDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			slog.Warn("indexer.walk", "path", path, "err", walkErr)
			return nil
		}
		if d.IsDir() || filepath.Ext(d.Name()) != ".md" {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		data, err := os.ReadFile(path)
		if err != nil {
			slog.Warn("indexer.read", "path", path, "err", err)
			return nil
		}
		n, err := domain.UnmarshalNote(data)
		if err != nil {
			slog.Warn("indexer.parse", "path", path, "err", err)
			return nil
		}
		// Hash the canonical body (without the auto-generated linked_notes
		// section) so a linker-driven rewrite of that section doesn't make
		// the next scan think the note's content changed.
		canonical := domain.CanonicalBody(n.Body)
		h := sha256.Sum256([]byte(canonical))
		entries = append(entries, FSEntry{
			ID:       n.ID,
			FilePath: path,
			BodyHash: hex.EncodeToString(h[:]),
		})
		paths[n.ID] = path
		bodies[n.ID] = n.Body
		return nil
	})
	if err != nil {
		return nil, nil, nil, fmt.Errorf("walk: %w", err)
	}
	return entries, paths, bodies, nil
}

// mustParseNote re-reads a file from disk into domain.Note. Returns a zero
// Note on error — caller has already validated parsing during scan.
func mustParseNote(path string) domain.Note {
	data, err := os.ReadFile(path)
	if err != nil {
		return domain.Note{}
	}
	n, _ := domain.UnmarshalNote(data)
	return n
}
