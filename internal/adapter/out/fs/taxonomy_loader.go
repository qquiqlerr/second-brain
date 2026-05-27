package fs

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/aleksejmetlusko/second-brain/internal/domain"
)

// TaxonomyLoader reads taxonomy.yml from disk with a TTL + mtime cache so
// edits are picked up without restart.
type TaxonomyLoader struct {
	path string
	ttl  time.Duration

	mu        sync.Mutex
	cached    domain.Taxonomy
	cachedAt  time.Time
	cachedMod time.Time
	hasCache  bool
}

// NewTaxonomyLoader returns a loader for the given taxonomy.yml path.
// A non-positive TTL disables caching.
func NewTaxonomyLoader(path string, ttl time.Duration) *TaxonomyLoader {
	return &TaxonomyLoader{path: path, ttl: ttl}
}

// Load returns the current Taxonomy snapshot, reloading the file when the
// TTL has elapsed and the file's modification time has changed.
func (l *TaxonomyLoader) Load(_ context.Context) (domain.Taxonomy, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	info, err := os.Stat(l.path)
	if err != nil {
		return domain.Taxonomy{}, fmt.Errorf("stat taxonomy %s: %w", l.path, err)
	}

	if l.hasCache && l.ttl > 0 && time.Since(l.cachedAt) < l.ttl {
		return l.cached, nil
	}
	if l.hasCache && info.ModTime().Equal(l.cachedMod) {
		l.cachedAt = time.Now()
		return l.cached, nil
	}

	data, err := os.ReadFile(l.path)
	if err != nil {
		return domain.Taxonomy{}, fmt.Errorf("read taxonomy %s: %w", l.path, err)
	}
	tax, err := domain.LoadTaxonomy(data)
	if err != nil {
		return domain.Taxonomy{}, err
	}
	l.cached = tax
	l.cachedAt = time.Now()
	l.cachedMod = info.ModTime()
	l.hasCache = true
	return tax, nil
}
