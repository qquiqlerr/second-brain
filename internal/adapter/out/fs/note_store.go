// Package fs contains filesystem-backed driven adapters.
package fs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/aleksejmetlusko/second-brain/internal/domain"
)

// NoteStore writes notes to disk under a configurable root directory.
type NoteStore struct {
	root string
}

// NewNoteStore returns a store that creates files under root.
func NewNoteStore(root string) *NoteStore {
	return &NoteStore{root: root}
}

// Write serializes a Note to the filesystem under root/<category>/<id>.md.
// If a file with that ID already exists, the suffix "-2", "-3", ... is
// appended (both to the filename and to the YAML id field) until a free
// name is found. The final ID is returned alongside the path.
func (s *NoteStore) Write(_ context.Context, n domain.Note) (string, string, error) {
	dir := filepath.Join(s.root, filepath.FromSlash(n.Category))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", fmt.Errorf("mkdir %s: %w", dir, err)
	}

	finalID, path, err := s.resolveFreePath(dir, n.ID)
	if err != nil {
		return "", "", err
	}
	n.ID = finalID

	data, err := domain.MarshalNote(n)
	if err != nil {
		return "", "", fmt.Errorf("marshal note: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", "", fmt.Errorf("write %s: %w", path, err)
	}
	return path, finalID, nil
}

// resolveFreePath probes baseID, then baseID-2, baseID-3, ... until a name
// that does not exist is found.
func (s *NoteStore) resolveFreePath(dir, baseID string) (string, string, error) {
	for i := range 1000 {
		id := baseID
		if i > 0 {
			id = fmt.Sprintf("%s-%d", baseID, i+1)
		}
		path := filepath.Join(dir, id+".md")
		_, err := os.Stat(path)
		switch {
		case errors.Is(err, os.ErrNotExist):
			return id, path, nil
		case err == nil:
			continue
		default:
			return "", "", fmt.Errorf("stat %s: %w", path, err)
		}
	}
	return "", "", fmt.Errorf("could not find free name for %s after 1000 attempts", baseID)
}
