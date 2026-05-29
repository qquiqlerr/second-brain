package domain_test

import (
	"testing"
	"time"

	"github.com/aleksejmetlusko/second-brain/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNote_FieldsRoundTrip(t *testing.T) {
	date := time.Date(2026, 5, 27, 22, 40, 0, 0, time.UTC)
	n := domain.Note{
		ID:            "20260527-tms-auth-bug",
		SchemaVersion: domain.SchemaVersion,
		Date:          date,
		Source:        domain.SourceTelegramVoice,
		Category:      "work/projects/tms",
		Tags:          []string{"bug", "auth"},
		Slug:          "tms-auth-bug",
		Body:          "Body text",
		Ingest: domain.IngestMeta{
			DumpID:          "uuid-1",
			ModelAtomize:    "anthropic/claude-3.5-haiku",
			ModelTranscribe: "openai/whisper-1",
		},
	}
	require.Equal(t, "1.1", n.SchemaVersion)
	require.Equal(t, domain.DumpSource("telegram-voice"), n.Source)
}

func TestMarshalUnmarshalNote_PreservesLinkedNotes(t *testing.T) {
	n := domain.Note{
		ID:            "20260529-x",
		SchemaVersion: domain.SchemaVersion,
		Date:          time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC),
		Source:        domain.SourceTelegramText,
		Kind:          domain.KindAtom,
		Category:      "work/projects",
		Tags:          []string{"a"},
		LinkedNotes:   []string{"20260520-a", "20260521-b"},
		Ingest:        domain.IngestMeta{DumpID: "d", ModelAtomize: "m"},
		Body:          "body",
	}
	data, err := domain.MarshalNote(n)
	require.NoError(t, err)
	got, err := domain.UnmarshalNote(data)
	require.NoError(t, err)
	assert.Equal(t, n.LinkedNotes, got.LinkedNotes)
}

func TestMarshalNote_OmitsEmptyLinkedNotes(t *testing.T) {
	n := domain.Note{
		ID:            "x",
		SchemaVersion: domain.SchemaVersion,
		Date:          time.Now(),
		Source:        domain.SourceTelegramText,
		Kind:          domain.KindAtom,
		Tags:          []string{},
		Ingest:        domain.IngestMeta{DumpID: "d", ModelAtomize: "m"},
		Body:          "b",
	}
	data, err := domain.MarshalNote(n)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "linked_notes")
}
