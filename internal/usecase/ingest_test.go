package usecase_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aleksejmetlusko/second-brain/internal/domain"
	"github.com/aleksejmetlusko/second-brain/internal/port/in"
	outmocks "github.com/aleksejmetlusko/second-brain/internal/port/out/mocks"
	"github.com/aleksejmetlusko/second-brain/internal/usecase"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func buildTaxonomy(t *testing.T) domain.Taxonomy {
	t.Helper()
	tax, err := domain.LoadTaxonomy([]byte(`
version: "1.0"
categories:
  work:
    projects:
      - tms
tags:
  - bug
  - auth
`))
	require.NoError(t, err)
	return tax
}

func noteAt(category, slug string) domain.Note {
	return domain.Note{
		SchemaVersion: domain.SchemaVersion,
		Date:          time.Date(2026, 5, 27, 22, 40, 0, 0, time.UTC),
		Source:        domain.SourceTelegramText,
		Category:      category,
		Tags:          []string{"bug"},
		Slug:          slug,
		Body:          "body",
	}
}

func TestExecute_TextDump_Happy(t *testing.T) {
	transcriber := outmocks.NewMockTranscriber(t)
	atomizer := outmocks.NewMockAtomizer(t)
	store := outmocks.NewMockNoteStore(t)
	taxLdr := outmocks.NewMockTaxonomyLoader(t)

	tax := buildTaxonomy(t)
	taxLdr.EXPECT().Load(mock.Anything).Return(tax, nil).Once()

	notes := []domain.Note{
		noteAt("work/projects/tms", "tms-bug-a"),
		noteAt("work/projects/tms", "tms-bug-b"),
	}
	atomizer.EXPECT().
		Atomize(mock.Anything, "dump", mock.Anything).
		Return(notes, nil).
		Once()

	store.EXPECT().
		Write(mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, n domain.Note) (string, string, error) {
			require.True(t, strings.HasPrefix(n.ID, "20260527-tms-bug"))
			return "/notes/work/projects/tms/" + n.ID + ".md", n.ID, nil
		}).
		Times(2)

	uc := usecase.NewIngestUseCase(transcriber, atomizer, store, taxLdr, "anthropic/claude-3.5-haiku", "openai/whisper-1")

	result, err := uc.Execute(t.Context(), in.IngestRequest{
		Source: domain.SourceTelegramText,
		Text:   "dump",
		UserID: 42,
	})
	require.NoError(t, err)
	require.Len(t, result.Notes, 2)
	require.Len(t, result.Paths, 2)
	require.Zero(t, result.Uncategorized)
	require.Empty(t, result.Errors)

	// shared DumpID across all notes
	require.Equal(t, result.Notes[0].Ingest.DumpID, result.Notes[1].Ingest.DumpID)
	require.NotEmpty(t, result.Notes[0].Ingest.DumpID)
	require.Equal(t, "anthropic/claude-3.5-haiku", result.Notes[0].Ingest.ModelAtomize)
	require.Empty(t, result.Notes[0].Ingest.ModelTranscribe, "transcribe model should be empty for text")
}

func TestExecute_VoiceDump_Happy(t *testing.T) {
	transcriber := outmocks.NewMockTranscriber(t)
	atomizer := outmocks.NewMockAtomizer(t)
	store := outmocks.NewMockNoteStore(t)
	taxLdr := outmocks.NewMockTaxonomyLoader(t)

	taxLdr.EXPECT().Load(mock.Anything).Return(buildTaxonomy(t), nil).Once()
	transcriber.EXPECT().
		Transcribe(mock.Anything, mock.Anything, "audio/ogg").
		Return("recognized dump", nil).Once()
	atomizer.EXPECT().
		Atomize(mock.Anything, "recognized dump", mock.Anything).
		Return([]domain.Note{noteAt("work/projects/tms", "x")}, nil).Once()
	store.EXPECT().
		Write(mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, n domain.Note) (string, string, error) { return "/p.md", n.ID, nil }).
		Once()

	uc := usecase.NewIngestUseCase(transcriber, atomizer, store, taxLdr, "atomize-model", "whisper")
	result, err := uc.Execute(t.Context(), in.IngestRequest{
		Source:    domain.SourceTelegramVoice,
		Audio:     nil,
		AudioMIME: "audio/ogg",
	})
	require.NoError(t, err)
	require.Len(t, result.Notes, 1)
	require.Equal(t, "whisper", result.Notes[0].Ingest.ModelTranscribe)
}

func TestExecute_VoiceDump_EmptyTranscript(t *testing.T) {
	transcriber := outmocks.NewMockTranscriber(t)
	taxLdr := outmocks.NewMockTaxonomyLoader(t)

	taxLdr.EXPECT().Load(mock.Anything).Return(buildTaxonomy(t), nil).Once()
	transcriber.EXPECT().Transcribe(mock.Anything, mock.Anything, "audio/ogg").
		Return("   \n  ", nil).Once()

	uc := usecase.NewIngestUseCase(transcriber, nil, nil, taxLdr, "a", "w")
	_, err := uc.Execute(t.Context(), in.IngestRequest{Source: domain.SourceTelegramVoice, AudioMIME: "audio/ogg"})
	require.ErrorIs(t, err, domain.ErrEmptyDump)
}

func TestExecute_TaxonomyLoadFails(t *testing.T) {
	taxLdr := outmocks.NewMockTaxonomyLoader(t)
	taxLdr.EXPECT().Load(mock.Anything).Return(domain.Taxonomy{}, domain.ErrTaxonomyMissing).Once()

	uc := usecase.NewIngestUseCase(nil, nil, nil, taxLdr, "a", "w")
	_, err := uc.Execute(t.Context(), in.IngestRequest{Source: domain.SourceTelegramText, Text: "x"})
	require.ErrorIs(t, err, domain.ErrTaxonomyMissing)
}

func TestExecute_AtomizerFails(t *testing.T) {
	atomizer := outmocks.NewMockAtomizer(t)
	taxLdr := outmocks.NewMockTaxonomyLoader(t)

	taxLdr.EXPECT().Load(mock.Anything).Return(buildTaxonomy(t), nil).Once()
	atomizer.EXPECT().Atomize(mock.Anything, mock.Anything, mock.Anything).
		Return(nil, domain.ErrAtomizerBadResponse).Once()

	uc := usecase.NewIngestUseCase(nil, atomizer, nil, taxLdr, "a", "w")
	_, err := uc.Execute(t.Context(), in.IngestRequest{Source: domain.SourceTelegramText, Text: "x"})
	require.ErrorIs(t, err, domain.ErrAtomizerBadResponse)
}

func TestExecute_AtomizerReturnsZeroNotes(t *testing.T) {
	atomizer := outmocks.NewMockAtomizer(t)
	taxLdr := outmocks.NewMockTaxonomyLoader(t)

	taxLdr.EXPECT().Load(mock.Anything).Return(buildTaxonomy(t), nil).Once()
	atomizer.EXPECT().Atomize(mock.Anything, mock.Anything, mock.Anything).
		Return([]domain.Note{}, nil).Once()

	uc := usecase.NewIngestUseCase(nil, atomizer, nil, taxLdr, "a", "w")
	_, err := uc.Execute(t.Context(), in.IngestRequest{Source: domain.SourceTelegramText, Text: "x"})
	require.ErrorIs(t, err, domain.ErrAtomizerNoNotes)
}

func TestExecute_UncategorizedFallback(t *testing.T) {
	atomizer := outmocks.NewMockAtomizer(t)
	store := outmocks.NewMockNoteStore(t)
	taxLdr := outmocks.NewMockTaxonomyLoader(t)

	taxLdr.EXPECT().Load(mock.Anything).Return(buildTaxonomy(t), nil).Once()
	notes := []domain.Note{
		noteAt("work/projects/tms", "valid"),
		noteAt("crypto/defi", "alien"),
	}
	atomizer.EXPECT().Atomize(mock.Anything, mock.Anything, mock.Anything).Return(notes, nil).Once()
	store.EXPECT().Write(mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, n domain.Note) (string, string, error) {
			return "/p/" + n.ID + ".md", n.ID, nil
		}).Times(2)

	uc := usecase.NewIngestUseCase(nil, atomizer, store, taxLdr, "a", "w")
	result, err := uc.Execute(t.Context(), in.IngestRequest{Source: domain.SourceTelegramText, Text: "x"})
	require.NoError(t, err)
	require.Equal(t, 1, result.Uncategorized)
	require.Len(t, result.Notes, 2)

	uncatNotes := 0
	for _, n := range result.Notes {
		if n.Category == domain.CategoryUncategorized {
			require.Equal(t, "crypto/defi", n.OriginalCategory)
			uncatNotes++
		}
	}
	require.Equal(t, 1, uncatNotes)
}

func TestExecute_PartialStoreFailure(t *testing.T) {
	atomizer := outmocks.NewMockAtomizer(t)
	store := outmocks.NewMockNoteStore(t)
	taxLdr := outmocks.NewMockTaxonomyLoader(t)

	taxLdr.EXPECT().Load(mock.Anything).Return(buildTaxonomy(t), nil).Once()
	atomizer.EXPECT().Atomize(mock.Anything, mock.Anything, mock.Anything).
		Return([]domain.Note{noteAt("work/projects/tms", "a"), noteAt("work/projects/tms", "b")}, nil).Once()

	var calls int
	store.EXPECT().Write(mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, n domain.Note) (string, string, error) {
			calls++
			if calls == 1 {
				return "", "", errors.New("disk full")
			}
			return "/p/" + n.ID + ".md", n.ID, nil
		}).Times(2)

	uc := usecase.NewIngestUseCase(nil, atomizer, store, taxLdr, "a", "w")
	result, err := uc.Execute(t.Context(), in.IngestRequest{Source: domain.SourceTelegramText, Text: "x"})
	require.NoError(t, err)
	require.Len(t, result.Notes, 1)
	require.Len(t, result.Errors, 1)
}

func TestExecute_AllStoresFail(t *testing.T) {
	atomizer := outmocks.NewMockAtomizer(t)
	store := outmocks.NewMockNoteStore(t)
	taxLdr := outmocks.NewMockTaxonomyLoader(t)

	taxLdr.EXPECT().Load(mock.Anything).Return(buildTaxonomy(t), nil).Once()
	atomizer.EXPECT().Atomize(mock.Anything, mock.Anything, mock.Anything).
		Return([]domain.Note{noteAt("work/projects/tms", "a")}, nil).Once()
	store.EXPECT().Write(mock.Anything, mock.Anything).
		Return("", "", errors.New("permission denied")).Once()

	uc := usecase.NewIngestUseCase(nil, atomizer, store, taxLdr, "a", "w")
	_, err := uc.Execute(t.Context(), in.IngestRequest{Source: domain.SourceTelegramText, Text: "x"})
	require.Error(t, err)
}
