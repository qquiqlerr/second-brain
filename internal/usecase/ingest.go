// Package usecase implements driving-port use cases by orchestrating
// driven-port adapters.
package usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/aleksejmetlusko/second-brain/internal/domain"
	"github.com/aleksejmetlusko/second-brain/internal/port/in"
	"github.com/aleksejmetlusko/second-brain/internal/port/out"
)

// IngestUseCase implements in.IngestDumpUseCase. Use NewIngestUseCase to construct.
type IngestUseCase struct {
	transcriber out.Transcriber
	atomizer    out.Atomizer
	store       out.NoteStore
	taxLoader   out.TaxonomyLoader

	atomizeModel    string
	transcribeModel string
	now             func() time.Time
}

// NewIngestUseCase wires the use case with its driven ports.
// atomizeModel and transcribeModel are recorded in every produced note's
// IngestMeta for traceability.
func NewIngestUseCase(
	tr out.Transcriber,
	at out.Atomizer,
	ns out.NoteStore,
	tl out.TaxonomyLoader,
	atomizeModel string,
	transcribeModel string,
) *IngestUseCase {
	return &IngestUseCase{
		transcriber:     tr,
		atomizer:        at,
		store:           ns,
		taxLoader:       tl,
		atomizeModel:    atomizeModel,
		transcribeModel: transcribeModel,
		now:             time.Now,
	}
}

// Execute runs the pipeline: load taxonomy → transcribe (if voice) → atomize
// → normalize → store. Partial store failures are accumulated in result.Errors.
// A non-nil error is returned only when a whole-pipeline stage fails
// (taxonomy/transcribe/atomize/empty dump).
func (u *IngestUseCase) Execute(ctx context.Context, req in.IngestRequest) (in.IngestResult, error) {
	tax, err := u.taxLoader.Load(ctx)
	if err != nil {
		return in.IngestResult{}, fmt.Errorf("load taxonomy: %w", err)
	}

	text := req.Text
	transcribeUsed := false
	if req.Source == domain.SourceTelegramVoice {
		text, err = u.transcriber.Transcribe(ctx, req.Audio, req.AudioMIME)
		if err != nil {
			return in.IngestResult{}, fmt.Errorf("transcribe: %w", err)
		}
		if strings.TrimSpace(text) == "" {
			return in.IngestResult{}, domain.ErrEmptyDump
		}
		transcribeUsed = true
	}
	if strings.TrimSpace(text) == "" {
		return in.IngestResult{}, domain.ErrEmptyDump
	}

	notes, err := u.atomizer.Atomize(ctx, text, tax)
	if err != nil {
		return in.IngestResult{}, fmt.Errorf("atomize: %w", err)
	}
	if len(notes) == 0 {
		return in.IngestResult{}, domain.ErrAtomizerNoNotes
	}

	dumpID := uuid.NewString()
	now := u.now()
	result := in.IngestResult{}

	for _, n := range notes {
		n = tax.Normalize(n)
		if n.Category == domain.CategoryUncategorized {
			result.Uncategorized++
		}

		n.SchemaVersion = domain.SchemaVersion
		n.Source = req.Source
		n.Date = now
		n.Ingest = domain.IngestMeta{
			DumpID:       dumpID,
			ModelAtomize: u.atomizeModel,
		}
		if transcribeUsed {
			n.Ingest.ModelTranscribe = u.transcribeModel
		}
		n.Slug = domain.Slugify(n.Slug)
		n.ID = domain.BuildID(n.Date, n.Slug)

		path, finalID, writeErr := u.store.Write(ctx, n)
		if writeErr != nil {
			result.Errors = append(result.Errors, fmt.Errorf("write %s: %w", n.ID, writeErr))
			continue
		}
		n.ID = finalID
		result.Notes = append(result.Notes, n)
		result.Paths = append(result.Paths, path)
	}

	if len(result.Notes) == 0 && len(result.Errors) > 0 {
		return result, fmt.Errorf("all writes failed: %w", errors.Join(result.Errors...))
	}
	return result, nil
}
