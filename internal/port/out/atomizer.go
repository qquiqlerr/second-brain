package out

import (
	"context"

	"github.com/aleksejmetlusko/second-brain/internal/domain"
)

// Atomizer asks an LLM to split a dump into atomic notes constrained by
// the supplied taxonomy.
type Atomizer interface {
	Atomize(ctx context.Context, dump string, tax domain.Taxonomy) ([]domain.Note, error)
}
