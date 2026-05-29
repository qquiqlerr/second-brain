package domain_test

import (
	"strings"
	"testing"
	"time"

	"github.com/aleksejmetlusko/second-brain/internal/domain"
	"github.com/stretchr/testify/require"
)

func sampleNote() domain.Note {
	moscow, _ := time.LoadLocation("Europe/Moscow")
	return domain.Note{
		ID:            "20260527-tms-auth-bug",
		SchemaVersion: domain.SchemaVersion,
		Date:          time.Date(2026, 5, 27, 22, 40, 0, 0, moscow),
		Source:        domain.SourceTelegramVoice,
		Kind:          domain.KindAtom,
		Category:      "work/projects/tms",
		Tags:          []string{"bug", "auth"},
		Slug:          "tms-auth-bug",
		Body:          "Текст мысли.\nВторая строка.\n",
		Ingest: domain.IngestMeta{
			DumpID:          "00000000-0000-0000-0000-000000000001",
			ModelAtomize:    "anthropic/claude-3.5-haiku",
			ModelTranscribe: "openai/whisper-1",
		},
	}
}

func TestMarshalNote_Layout(t *testing.T) {
	out, err := domain.MarshalNote(sampleNote())
	require.NoError(t, err)
	s := string(out)

	require.True(t, strings.HasPrefix(s, "---\n"), "must start with --- delimiter")
	require.Contains(t, s, "\n---\n")
	require.Contains(t, s, "id: 20260527-tms-auth-bug")
	require.Contains(t, s, "schema_version: \"1.1\"")
	require.Contains(t, s, "category: work/projects/tms")
	require.Contains(t, s, "Текст мысли.")
	require.True(t, strings.HasSuffix(s, "Вторая строка.\n"))
}

func TestMarshalNote_OmitsEmptyOriginalAndTranscribe(t *testing.T) {
	n := sampleNote()
	n.Source = domain.SourceTelegramText
	n.Ingest.ModelTranscribe = ""

	out, err := domain.MarshalNote(n)
	require.NoError(t, err)
	s := string(out)

	require.NotContains(t, s, "original_category")
	require.NotContains(t, s, "model_transcribe")
}

func TestUnmarshalNote_RoundTrip(t *testing.T) {
	in := sampleNote()
	data, err := domain.MarshalNote(in)
	require.NoError(t, err)

	out, err := domain.UnmarshalNote(data)
	require.NoError(t, err)

	require.Equal(t, in.ID, out.ID)
	require.Equal(t, in.Category, out.Category)
	require.Equal(t, in.Tags, out.Tags)
	require.Equal(t, in.Body, out.Body)
	require.Equal(t, in.Ingest.DumpID, out.Ingest.DumpID)
	require.True(t, in.Date.Equal(out.Date))
}

func TestUnmarshalNote_RejectsMissingDelimiter(t *testing.T) {
	_, err := domain.UnmarshalNote([]byte("no frontmatter here"))
	require.Error(t, err)
}

func TestMarshalNote_KindSummary(t *testing.T) {
	n := sampleNote()
	n.Kind = domain.KindSummary
	out, err := domain.MarshalNote(n)
	require.NoError(t, err)
	require.Contains(t, string(out), "kind: summary")
}
