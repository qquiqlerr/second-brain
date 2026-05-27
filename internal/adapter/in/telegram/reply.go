package telegram

import (
	"errors"
	"fmt"
	"strings"

	"github.com/aleksejmetlusko/second-brain/internal/domain"
	"github.com/aleksejmetlusko/second-brain/internal/port/in"
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

	var b strings.Builder
	fmt.Fprintf(&b, "✅ Сохранено %s:\n", pluralNotes(len(r.Notes)))
	for _, n := range r.Notes {
		fmt.Fprintf(&b, "• [%s] (%s)", n.Slug, n.Category)
		for _, tag := range n.Tags {
			fmt.Fprintf(&b, " #%s", tag)
		}
		b.WriteByte('\n')
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
