// Package in declares driving (inbound) ports that adapters can call to
// invoke domain use cases.
package in

import (
	"context"
	"io"

	"github.com/aleksejmetlusko/second-brain/internal/domain"
)

// IngestRequest describes one dump entering the pipeline.
type IngestRequest struct {
	Source    domain.DumpSource
	Text      string        // populated for text dumps
	Audio     io.ReadCloser // populated for voice dumps; caller transfers ownership
	AudioMIME string
	UserID    int64
}

// IngestResult summarizes the outcome of processing a single dump.
type IngestResult struct {
	Notes         []domain.Note
	Paths         []string
	Uncategorized int
	Errors        []error
}

// IngestDumpUseCase is the entry point for converting a dump into stored notes.
type IngestDumpUseCase interface {
	Execute(ctx context.Context, req IngestRequest) (IngestResult, error)
}
