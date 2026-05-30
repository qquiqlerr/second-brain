package indexer

import (
	"context"
	"fmt"
	"log/slog"
	"slices"

	"github.com/aleksejmetlusko/second-brain/internal/domain"
	portout "github.com/aleksejmetlusko/second-brain/internal/port/out"
)

// Config bundles indexer-wide tunables.
type Config struct {
	BatchSize         int
	LinkTopK          int
	LinkMinSimilarity float32
}

// YAMLRewriter writes new linked_notes (and bumps schema_version) into the
// frontmatter at file_path atomically. Concrete implementation lives in
// the indexer orchestrator alongside the FS adapter.
type YAMLRewriter func(ctx context.Context, filePath string, linkedNotes []string) error

// Linker recomputes linked_notes for atoms after re-embedding.
//
// Temporal-only: an atom's linked_notes only references atoms with a
// strictly earlier `date`. Rationale:
//   - matches journaling intuition (a thought references past context)
//   - older files stay byte-identical after new notes are added — no git
//     churn, predictable mtime
//   - Obsidian's backlinks panel already shows "B is referenced by A" by
//     scanning all files, so the user still sees the reverse view without
//     us duplicating the edge in both files
//   - removes the need to expand the affected set to seeds + neighbors:
//     only the seed itself can gain links, never the older neighbors
type Linker struct {
	vec      portout.VectorIndex
	rewrite  YAMLRewriter
	topK     int
	minScore float32
}

// NewLinker constructs a Linker.
func NewLinker(vec portout.VectorIndex, rewrite YAMLRewriter, cfg Config) *Linker {
	return &Linker{vec: vec, rewrite: rewrite, topK: cfg.LinkTopK, minScore: cfg.LinkMinSimilarity}
}

// Recompute updates linked_notes for the seed IDs only (no neighbor
// expansion — temporal rule means older atoms never gain new links from
// younger ones). Caller passes only atoms; summaries are skipped silently
// if they sneak in.
func (l *Linker) Recompute(ctx context.Context, seedIDs []string) error {
	updated := 0
	for _, id := range seedIDs {
		didUpdate, err := l.recomputeOne(ctx, id)
		if err != nil {
			slog.Warn("linker: recompute failed", "id", id, "err", err)
			continue
		}
		if didUpdate {
			updated++
		}
	}
	slog.Info("linker.recompute", "n_seeds", len(seedIDs), "n_updated", updated)
	return nil
}

func (l *Linker) recomputeOne(ctx context.Context, id string) (bool, error) {
	meta, found, err := l.vec.GetMeta(ctx, id)
	if err != nil {
		return false, err
	}
	if !found || meta.Kind != domain.KindAtom {
		return false, nil
	}
	vec, found, err := l.vec.GetEmbedding(ctx, id)
	if err != nil || !found {
		return false, err
	}
	// DateBefore = meta.Date: strict "<" filter in sqlite-vec excludes
	// self (same timestamp) and any atom written at the same instant. If
	// two atoms share an identical timestamp (rare, only on backfill of
	// hand-crafted notes) neither links to the other — acceptable.
	hits, err := l.vec.SearchByVector(ctx, vec, portout.SearchQuery{
		TopK:       l.topK + 1,
		KindFilter: []string{domain.KindAtom},
		DateBefore: meta.Date,
	})
	if err != nil {
		return false, err
	}
	newLinks := make([]string, 0, l.topK)
	for _, h := range hits {
		if h.ID == id {
			continue // belt-and-suspenders; DateBefore already excludes
		}
		if h.Score < l.minScore {
			break // hits are sorted desc
		}
		newLinks = append(newLinks, h.ID)
		if len(newLinks) >= l.topK {
			break
		}
	}
	if slices.Equal(newLinks, meta.LinkedNotes) {
		return false, nil
	}
	if err := l.vec.UpdateLinkedNotes(ctx, id, newLinks); err != nil {
		return false, fmt.Errorf("update links in db: %w", err)
	}
	if l.rewrite != nil {
		if err := l.rewrite(ctx, meta.FilePath, newLinks); err != nil {
			// Log but don't fail. DB is now ahead of disk; next pass with the
			// same body_hash won't re-embed and won't re-attempt the rewrite.
			// Acceptable for v1 — manual rebuild or re-edit triggers retry.
			slog.Warn("linker: yaml rewrite failed", "path", meta.FilePath, "err", err)
		}
	}
	return true, nil
}
