package out

import (
	"context"

	"github.com/aleksejmetlusko/second-brain/internal/domain"
)

// TaxonomyLoader returns the current Taxonomy snapshot. Implementations
// may cache and watch the underlying file.
type TaxonomyLoader interface {
	Load(ctx context.Context) (domain.Taxonomy, error)
}
