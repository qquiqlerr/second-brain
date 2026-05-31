package domain

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	frontmatterDelim         = "---\n"
	linkedNotesSectionStart  = "<!-- linked_notes_start -->"
	linkedNotesSectionEnd    = "<!-- linked_notes_end -->"
	linkedNotesSectionHeader = "## Связано"
)

// MarshalNote serializes a Note as a Markdown file with YAML frontmatter
// followed by the Body. Output ends with a newline.
func MarshalNote(n Note) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString(frontmatterDelim)

	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(n); err != nil {
		return nil, fmt.Errorf("encode frontmatter: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("close encoder: %w", err)
	}

	buf.WriteString(frontmatterDelim)
	buf.WriteString(n.Body)
	if !bytes.HasSuffix(buf.Bytes(), []byte("\n")) {
		buf.WriteByte('\n')
	}
	return buf.Bytes(), nil
}

// UpdateFrontmatter rewrites the YAML frontmatter of a marshalled note so
// that:
//   - linked_notes is set to the given slice (or removed if nil/empty)
//   - schema_version is set to the current SchemaVersion
//   - body's auto-generated "Связано" wikilink section is added/refreshed/removed
//     to match links (so Obsidian and Quartz both see the graph edges)
//
// The function is pure: it does not touch the filesystem.
func UpdateFrontmatter(data []byte, links []string) ([]byte, error) {
	n, err := UnmarshalNote(data)
	if err != nil {
		return nil, fmt.Errorf("parse frontmatter: %w", err)
	}
	n.SchemaVersion = SchemaVersion
	n.LinkedNotes = links
	n.Body = ApplyLinkedNotesSection(n.Body, links)
	return MarshalNote(n)
}

// UnmarshalNote parses a Markdown file with YAML frontmatter back into a Note.
// Body is everything after the second --- delimiter.
func UnmarshalNote(data []byte) (Note, error) {
	if !bytes.HasPrefix(data, []byte(frontmatterDelim)) {
		return Note{}, errors.New("note: missing opening --- delimiter")
	}
	rest := data[len(frontmatterDelim):]
	end := bytes.Index(rest, []byte("\n"+frontmatterDelim))
	if end < 0 {
		return Note{}, errors.New("note: missing closing --- delimiter")
	}
	frontmatter := rest[:end+1] // include trailing newline
	body := rest[end+1+len(frontmatterDelim):]

	var n Note
	if err := yaml.Unmarshal(frontmatter, &n); err != nil {
		return Note{}, fmt.Errorf("decode frontmatter: %w", err)
	}
	n.Body = string(body)
	return n, nil
}

// CanonicalBody returns body with the auto-generated linked_notes section
// stripped. The indexer uses this for body_hash and embedding inputs so
// that linker-driven rewrites don't trigger re-embedding next cycle.
func CanonicalBody(body string) string {
	return stripLinkedNotesSection(body)
}

// ApplyLinkedNotesSection replaces (or appends, or removes) the auto-
// generated wikilink section in body to match links. The section is
// bracketed by HTML comments so it's safe to detect even if the user
// also has a "## Связано" heading in their own content.
func ApplyLinkedNotesSection(body string, links []string) string {
	stripped := stripLinkedNotesSection(body)
	if len(links) == 0 {
		return stripped
	}
	var b strings.Builder
	b.WriteString(strings.TrimRight(stripped, "\n"))
	b.WriteString("\n\n")
	b.WriteString(linkedNotesSectionStart + "\n")
	b.WriteString(linkedNotesSectionHeader + "\n")
	for _, id := range links {
		fmt.Fprintf(&b, "- [[%s]]\n", id)
	}
	b.WriteString(linkedNotesSectionEnd + "\n")
	return b.String()
}

func stripLinkedNotesSection(body string) string {
	startIdx := strings.Index(body, linkedNotesSectionStart)
	if startIdx == -1 {
		return body
	}
	endIdx := strings.Index(body[startIdx:], linkedNotesSectionEnd)
	if endIdx == -1 {
		return body // malformed; leave alone
	}
	endIdx += startIdx + len(linkedNotesSectionEnd)
	// Consume trailing newline that terminates the end marker line.
	if endIdx < len(body) && body[endIdx] == '\n' {
		endIdx++
	}
	// Drop any blank line that separates the body from our section.
	pre := strings.TrimRight(body[:startIdx], "\n")
	if pre != "" {
		pre += "\n"
	}
	return pre + body[endIdx:]
}
