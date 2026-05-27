package telegram_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/aleksejmetlusko/second-brain/internal/adapter/in/telegram"
	"github.com/aleksejmetlusko/second-brain/internal/domain"
	"github.com/aleksejmetlusko/second-brain/internal/port/in"
	"github.com/stretchr/testify/require"
)

func makeResult() in.IngestResult {
	return in.IngestResult{
		Notes: []domain.Note{
			{ID: "20260527-tms-auth-bug", Kind: domain.KindAtom, Category: "work/projects/tms", Tags: []string{"appsheet", "bug"}, Slug: "tms-auth-bug"},
			{ID: "20260527-sleep", Kind: domain.KindAtom, Category: "health/sleep", Tags: []string{"idea"}, Slug: "sleep"},
		},
		Paths:         []string{"/p1.md", "/p2.md"},
		Uncategorized: 0,
	}
}

func TestFormatReply_HappySummary(t *testing.T) {
	out := telegram.FormatReply(makeResult(), nil)
	require.True(t, strings.HasPrefix(out, "✅"))
	require.Contains(t, out, "Сохранено 2 заметки")
	require.Contains(t, out, "[tms-auth-bug] (work/projects/tms) #appsheet #bug")
	require.Contains(t, out, "[sleep] (health/sleep) #idea")
}

func TestFormatReply_SummaryHighlighted(t *testing.T) {
	r := makeResult()
	r.Notes = append(r.Notes, domain.Note{
		ID:       "20260527-daily-summary",
		Kind:     domain.KindSummary,
		Category: "personal",
		Slug:     "daily-summary",
	})
	out := telegram.FormatReply(r, nil)
	require.Contains(t, out, "+ саммари")
	require.Contains(t, out, "📝 Саммари: [daily-summary] (personal)")
}

func TestFormatReply_WithUncategorized(t *testing.T) {
	r := makeResult()
	r.Uncategorized = 1
	out := telegram.FormatReply(r, nil)
	require.Contains(t, out, "📦 1 уехала в Uncategorized")
}

func TestFormatReply_PartialErrors(t *testing.T) {
	r := makeResult()
	r.Errors = []error{errors.New("disk full")}
	out := telegram.FormatReply(r, nil)
	require.Contains(t, out, "⚠️ 1 заметка не записалась")
}

func TestFormatReply_EmptyNotesNonErrorMessage(t *testing.T) {
	out := telegram.FormatReply(in.IngestResult{}, domain.ErrAtomizerNoNotes)
	require.Contains(t, out, "🤔")
}

func TestFormatReply_TranscribeError(t *testing.T) {
	out := telegram.FormatReply(in.IngestResult{}, errors.New("transcribe: blah"))
	require.True(t, strings.HasPrefix(out, "❌"))
}
