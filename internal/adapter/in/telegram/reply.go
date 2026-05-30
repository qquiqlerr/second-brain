package telegram

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/aleksejmetlusko/second-brain/internal/domain"
	"github.com/aleksejmetlusko/second-brain/internal/port/in"
	portout "github.com/aleksejmetlusko/second-brain/internal/port/out"
)

// FormatReply turns an IngestResult plus a top-level error into the Russian-
// language summary the user sees in Telegram.
func FormatReply(r in.IngestResult, topErr error) string {
	switch {
	case errors.Is(topErr, domain.ErrAtomizerNoNotes):
		return "🤔 не нашёл идей в дампе"
	case errors.Is(topErr, domain.ErrEmptyDump):
		return "❌ не распознал речь, повтори"
	case topErr != nil:
		return fmt.Sprintf("❌ %s", shortError(topErr))
	case len(r.Notes) == 0:
		return "🤔 ничего не сохранилось"
	}

	var atoms []domain.Note
	var summary *domain.Note
	for i, n := range r.Notes {
		if n.Kind == domain.KindSummary {
			summary = &r.Notes[i]
			continue
		}
		atoms = append(atoms, n)
	}

	var b strings.Builder
	if summary != nil {
		fmt.Fprintf(&b, "✅ Сохранено %s + саммари:\n", pluralNotes(len(atoms)))
	} else {
		fmt.Fprintf(&b, "✅ Сохранено %s:\n", pluralNotes(len(atoms)))
	}
	for _, n := range atoms {
		fmt.Fprintf(&b, "• [%s] (%s)", n.Slug, n.Category)
		for _, tag := range n.Tags {
			fmt.Fprintf(&b, " #%s", tag)
		}
		b.WriteByte('\n')
	}
	if summary != nil {
		fmt.Fprintf(&b, "📝 Саммари: [%s] (%s)\n", summary.Slug, summary.Category)
	}
	if r.Uncategorized > 0 {
		fmt.Fprintf(&b, "📦 %s в Uncategorized\n", pluralUncat(r.Uncategorized))
	}
	if len(r.Errors) > 0 {
		fmt.Fprintf(&b, "⚠️ %s не записалась\n", pluralNotErr(len(r.Errors)))
	}
	return strings.TrimRight(b.String(), "\n")
}

func pluralNotes(n int) string {
	switch n % 10 {
	case 1:
		if n%100 != 11 {
			return fmt.Sprintf("%d заметка", n)
		}
	case 2, 3, 4:
		if n%100 < 12 || n%100 > 14 {
			return fmt.Sprintf("%d заметки", n)
		}
	}
	return fmt.Sprintf("%d заметок", n)
}

func pluralUncat(n int) string {
	if n == 1 {
		return "1 уехала"
	}
	return fmt.Sprintf("%d уехали", n)
}

func pluralNotErr(n int) string {
	if n == 1 {
		return "1 заметка"
	}
	return fmt.Sprintf("%d заметок", n)
}

func shortError(err error) string {
	msg := err.Error()
	if len(msg) > 200 {
		msg = msg[:200] + "..."
	}
	return msg
}

// FormatFindReply renders a /find result for Telegram. Empty hits yield
// a friendly "ничего не нашёл" line.
func FormatFindReply(query string, hits []portout.SearchHit, notesDir string) string {
	if len(hits) == 0 {
		return "🔍 Ничего не нашёл по запросу: " + query
	}
	var b strings.Builder
	fmt.Fprintf(&b, "🔍 Топ-%d по запросу: %s\n\n", len(hits), query)
	for i, h := range hits {
		snippet := snippetFromFile(h.Meta.FilePath, 200)
		relPath, _ := filepath.Rel(notesDir, h.Meta.FilePath)
		fmt.Fprintf(&b, "%d. %s/%s (%.2f)\n%s\n`%s`\n\n",
			i+1, h.Meta.Category, idSlug(h.ID), h.Score, snippet, relPath)
	}
	return strings.TrimRight(b.String(), "\n")
}

func snippetFromFile(path string, maxRunes int) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	n, err := domain.UnmarshalNote(data)
	if err != nil {
		return ""
	}
	body := strings.TrimSpace(n.Body)
	// Byte-slice (body[:N]) cuts mid-rune in Cyrillic/multibyte text and
	// produces invalid UTF-8 — Telegram rejects the whole message.
	runes := []rune(body)
	if len(runes) > maxRunes {
		body = string(runes[:maxRunes]) + "..."
	}
	return body
}

// idSlug returns the part after the YYYYMMDD- prefix in a note id.
func idSlug(id string) string {
	if i := strings.IndexByte(id, '-'); i >= 0 {
		return id[i+1:]
	}
	return id
}
