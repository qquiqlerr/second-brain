package domain_test

import (
	"testing"
	"time"

	"github.com/aleksejmetlusko/second-brain/internal/domain"
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
	require.Equal(t, "1.0", n.SchemaVersion)
	require.Equal(t, domain.DumpSource("telegram-voice"), n.Source)
}
