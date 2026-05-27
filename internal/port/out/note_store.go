package out

import (
	"context"

	"github.com/aleksejmetlusko/second-brain/internal/domain"
)

// NoteStore persists a note. Implementations may rename to resolve filename
// collisions; the final ID is reflected back into the returned Note via the
// in/out IngestResult by the caller.
type NoteStore interface {
	Write(ctx context.Context, n domain.Note) (path string, finalID string, err error)
}
