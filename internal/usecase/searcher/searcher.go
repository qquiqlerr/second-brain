// Package searcher exposes semantic retrieval over the indexed notes.
package searcher

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aleksejmetlusko/second-brain/internal/domain"
	portout "github.com/aleksejmetlusko/second-brain/internal/port/out"
)

// Config carries searcher tunables.
type Config struct {
	TopK int
}

// Options scopes a single Find call.
type Options struct {
	CategoryPrefix string
	DateFrom       time.Time
}

// Searcher coordinates query-time embedding and vector search.
type Searcher struct {
	embedder portout.Embedder
	vec      portout.VectorIndex
	topK     int
}

// New constructs a Searcher.
func New(e portout.Embedder, v portout.VectorIndex, cfg Config) *Searcher {
	k := cfg.TopK
	if k <= 0 {
		k = 5
	}
	return &Searcher{embedder: e, vec: v, topK: k}
}

// Find performs semantic retrieval over atoms.
func (s *Searcher) Find(ctx context.Context, query string, opts Options) ([]portout.SearchHit, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("%w: empty query", domain.ErrSearchEmpty)
	}
	vecs, err := s.embedder.Embed(ctx, []string{query}, portout.EmbedQuery)
	if err != nil {
		return nil, err
	}
	if len(vecs) != 1 {
		return nil, fmt.Errorf("%w: embedder returned %d vectors for 1 query", domain.ErrEmbedder, len(vecs))
	}
	hits, err := s.vec.SearchByVector(ctx, vecs[0], portout.SearchQuery{
		TopK:           s.topK,
		KindFilter:     []string{domain.KindAtom},
		CategoryPrefix: opts.CategoryPrefix,
		DateFrom:       opts.DateFrom,
	})
	if err != nil {
		return nil, err
	}
	return hits, nil
}
