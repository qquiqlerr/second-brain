package domain

import (
	"bytes"
	"errors"
	"fmt"

	"gopkg.in/yaml.v3"
)

const frontmatterDelim = "---\n"

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
