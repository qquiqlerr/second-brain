package domain_test

import (
	"strings"
	"testing"
	"time"

	"github.com/aleksejmetlusko/second-brain/internal/domain"
	"github.com/stretchr/testify/assert"
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

func TestUpdateFrontmatter_RewritesLinkedNotesAndBumpsVersion(t *testing.T) {
	src := []byte(`---
id: 20260527-x
schema_version: "1.0"
date: 2026-05-27T22:40:00Z
source: telegram-text
kind: atom
category: work/projects
tags: [a]
ingest:
  dump_id: d
  model_atomize: m
---
hello body
`)
	got, err := domain.UpdateFrontmatter(src, []string{"20260520-a", "20260521-b"})
	require.NoError(t, err)
	n, err := domain.UnmarshalNote(got)
	require.NoError(t, err)
	assert.Equal(t, domain.SchemaVersion, n.SchemaVersion)
	assert.Equal(t, []string{"20260520-a", "20260521-b"}, n.LinkedNotes)
	assert.Equal(t, "hello body\n", n.Body)
}

func TestUpdateFrontmatter_EmptyLinksClearField(t *testing.T) {
	src := []byte(`---
id: x
schema_version: "1.1"
date: 2026-05-27T22:40:00Z
source: telegram-text
kind: atom
category: work
tags: []
linked_notes:
  - id1
ingest: {dump_id: d, model_atomize: m}
---
body
`)
	got, err := domain.UpdateFrontmatter(src, nil)
	require.NoError(t, err)
	assert.NotContains(t, string(got), "linked_notes")

	got, err = domain.UpdateFrontmatter(src, []string{})
	require.NoError(t, err)
	assert.NotContains(t, string(got), "linked_notes")
}
