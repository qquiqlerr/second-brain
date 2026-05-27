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

// StageTimeouts caps each pipeline stage. Zero = no timeout for that stage.
type StageTimeouts struct {
	Transcribe time.Duration
	Atomize    time.Duration
	Write      time.Duration
}

// IngestUseCase implements in.IngestDumpUseCase. Use NewIngestUseCase to construct.
type IngestUseCase struct {
	transcriber out.Transcriber
	atomizer    out.Atomizer
	store       out.NoteStore
	taxLoader   out.TaxonomyLoader

	atomizeModel    string
	transcribeModel string
	now             func() time.Time
	fixedDumpID     string
	timeouts        StageTimeouts
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
		tctx, tcancel := withOptionalTimeout(ctx, u.timeouts.Transcribe)
		text, err = u.transcriber.Transcribe(tctx, req.Audio, req.AudioMIME)
		tcancel()
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

	actx, acancel := withOptionalTimeout(ctx, u.timeouts.Atomize)
	notes, err := u.atomizer.Atomize(actx, text, tax)
	acancel()
	if err != nil {
		return in.IngestResult{}, fmt.Errorf("atomize: %w", err)
	}
	if len(notes) == 0 {
		return in.IngestResult{}, domain.ErrAtomizerNoNotes
	}

	dumpID := u.fixedDumpID
	if dumpID == "" {
		dumpID = uuid.NewString()
	}
	now := u.now()
	result := in.IngestResult{}

	for _, n := range notes {
		n = tax.Normalize(n)
		if n.Category == domain.CategoryUncategorized {
			result.Uncategorized++
		}

		n.SchemaVersion = domain.SchemaVersion
		n.Source = req.Source
		n.Date = now.Truncate(time.Second)
		n.Ingest = domain.IngestMeta{
			DumpID:       dumpID,
			ModelAtomize: u.atomizeModel,
		}
		if transcribeUsed {
			n.Ingest.ModelTranscribe = u.transcribeModel
		}
		n.Slug = domain.Slugify(n.Slug)
		n.ID = domain.BuildID(n.Date, n.Slug)

		wctx, wcancel := withOptionalTimeout(ctx, u.timeouts.Write)
		path, finalID, writeErr := u.store.Write(wctx, n)
		wcancel()
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

// WithFixedNow overrides the now() function. Test-only.
func (u *IngestUseCase) WithFixedNow(now func() time.Time) {
	u.now = now
}

// WithFixedDumpID forces a deterministic dump ID for golden tests.
func (u *IngestUseCase) WithFixedDumpID(id string) {
	u.fixedDumpID = id
}

// WithTimeouts applies per-stage timeouts. Zero values disable the timeout
// for that stage.
func (u *IngestUseCase) WithTimeouts(t StageTimeouts) {
	u.timeouts = t
}

// withOptionalTimeout returns ctx unchanged with a no-op cancel when d is 0
// or negative, or a derived context with the given timeout otherwise.
func withOptionalTimeout(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	if d <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, d)
}
