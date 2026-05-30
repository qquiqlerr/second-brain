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

// Recompute updates linked_notes for the seed IDs and their immediate
// neighbors. Caller passes only atoms; summaries are skipped silently if
// they sneak in.
func (l *Linker) Recompute(ctx context.Context, seedIDs []string) error {
	affected := make(map[string]struct{}, len(seedIDs)*2)
	for _, id := range seedIDs {
		meta, found, err := l.vec.GetMeta(ctx, id)
		if err != nil {
			return err
		}
		if !found || meta.Kind != domain.KindAtom {
			continue
		}
		affected[id] = struct{}{}

		vec, _, err := l.vec.GetEmbedding(ctx, id)
		if err != nil {
			return err
		}
		neighbors, err := l.vec.SearchByVector(ctx, vec, portout.SearchQuery{
			TopK:       l.topK + 1,
			KindFilter: []string{domain.KindAtom},
		})
		if err != nil {
			return err
		}
		for _, n := range neighbors {
			if n.ID != id {
				affected[n.ID] = struct{}{}
			}
		}
	}

	for id := range affected {
		if err := l.recomputeOne(ctx, id); err != nil {
			slog.Warn("linker: recompute failed", "id", id, "err", err)
			// continue, don't abort the whole pass
		}
	}
	return nil
}

func (l *Linker) recomputeOne(ctx context.Context, id string) error {
	meta, found, err := l.vec.GetMeta(ctx, id)
	if err != nil {
		return err
	}
	if !found || meta.Kind != domain.KindAtom {
		return nil
	}
	vec, found, err := l.vec.GetEmbedding(ctx, id)
	if err != nil || !found {
		return err
	}
	hits, err := l.vec.SearchByVector(ctx, vec, portout.SearchQuery{
		TopK:       l.topK + 1,
		KindFilter: []string{domain.KindAtom},
	})
	if err != nil {
		return err
	}
	newLinks := make([]string, 0, l.topK)
	for _, h := range hits {
		if h.ID == id {
			continue
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
		return nil
	}
	if err := l.vec.UpdateLinkedNotes(ctx, id, newLinks); err != nil {
		return fmt.Errorf("update links in db: %w", err)
	}
	if l.rewrite != nil {
		if err := l.rewrite(ctx, meta.FilePath, newLinks); err != nil {
			// Log but don't fail. DB is now ahead of disk; next pass with the
			// same body_hash won't re-embed and won't re-attempt the rewrite.
			// Acceptable for v1 — manual rebuild or re-edit triggers retry.
			slog.Warn("linker: yaml rewrite failed", "path", meta.FilePath, "err", err)
		}
	}
	return nil
}
