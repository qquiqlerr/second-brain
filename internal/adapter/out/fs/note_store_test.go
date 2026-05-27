package fs_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aleksejmetlusko/second-brain/internal/adapter/out/fs"
	"github.com/aleksejmetlusko/second-brain/internal/domain"
	"github.com/stretchr/testify/require"
)

func sample(t *testing.T, category, slug string) domain.Note {
	t.Helper()
	return domain.Note{
		ID:            domain.BuildID(time.Date(2026, 5, 27, 22, 40, 0, 0, time.UTC), slug),
		SchemaVersion: domain.SchemaVersion,
		Date:          time.Date(2026, 5, 27, 22, 40, 0, 0, time.UTC),
		Source:        domain.SourceTelegramText,
		Category:      category,
		Tags:          []string{"bug"},
		Slug:          slug,
		Body:          "body\n",
		Ingest:        domain.IngestMeta{DumpID: "uuid-1", ModelAtomize: "m"},
	}
}

func TestNoteStore_WriteCreatesDirectoriesAndFile(t *testing.T) {
	root := t.TempDir()
	store := fs.NewNoteStore(root)

	path, finalID, err := store.Write(t.Context(), sample(t, "work/projects/tms", "x"))
	require.NoError(t, err)
	require.Equal(t, "20260527-x", finalID)
	require.Equal(t, filepath.Join(root, "work", "projects", "tms", "20260527-x.md"), path)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(data), "id: 20260527-x")
}

func TestNoteStore_CollisionAppendsSuffix(t *testing.T) {
	root := t.TempDir()
	store := fs.NewNoteStore(root)

	_, id1, err := store.Write(t.Context(), sample(t, "work/projects/tms", "dup"))
	require.NoError(t, err)
	require.Equal(t, "20260527-dup", id1)

	_, id2, err := store.Write(t.Context(), sample(t, "work/projects/tms", "dup"))
	require.NoError(t, err)
	require.Equal(t, "20260527-dup-2", id2)

	_, id3, err := store.Write(t.Context(), sample(t, "work/projects/tms", "dup"))
	require.NoError(t, err)
	require.Equal(t, "20260527-dup-3", id3)

	// File for -2 must contain the matching id in YAML
	data, _ := os.ReadFile(filepath.Join(root, "work", "projects", "tms", "20260527-dup-2.md"))
	require.Contains(t, string(data), "id: 20260527-dup-2")
}

func TestNoteStore_UncategorizedLayout(t *testing.T) {
	root := t.TempDir()
	store := fs.NewNoteStore(root)
	n := sample(t, domain.CategoryUncategorized, "alien")
	n.OriginalCategory = "crypto/defi"

	path, _, err := store.Write(t.Context(), n)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(path, filepath.Join(root, "uncategorized")))

	data, _ := os.ReadFile(path)
	require.Contains(t, string(data), "original_category: crypto/defi")
}
