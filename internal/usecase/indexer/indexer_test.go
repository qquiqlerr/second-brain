package indexer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/aleksejmetlusko/second-brain/internal/domain"
	portout "github.com/aleksejmetlusko/second-brain/internal/port/out"
	"github.com/aleksejmetlusko/second-brain/internal/port/out/mocks"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestIndexer_EmptyDB_EmbedsAllFiles(t *testing.T) {
	notesDir := t.TempDir()
	noteID := "20260527-x"
	writeNote(t, notesDir, "work/projects/"+noteID+".md", domain.Note{
		ID:            noteID,
		SchemaVersion: domain.SchemaVersion,
		Date:          time.Now(),
		Source:        domain.SourceTelegramText,
		Kind:          domain.KindAtom,
		Category:      "work/projects",
		Tags:          []string{},
		Ingest:        domain.IngestMeta{DumpID: "d", ModelAtomize: "m"},
		Body:          "hello",
	})

	embedder := mocks.NewMockEmbedder(t)
	embedder.On("Embed", mock.Anything,
		mock.MatchedBy(func(s []string) bool {
			return len(s) == 1 && s[0] == "hello\n"
		}),
		portout.EmbedDocument,
	).Return([][]float32{{1, 0, 0}}, nil).Once()

	vec := mocks.NewMockVectorIndex(t)
	vec.On("ListAllMeta", mock.Anything).Return(nil, nil).Once()
	vec.On("Upsert", mock.Anything, mock.MatchedBy(func(items []portout.IndexItem) bool {
		return len(items) == 1 && items[0].ID == noteID
	})).Return(nil).Once()

	// Linker mocks. The freshly-embedded note is its own only neighbor, so
	// affected = {noteID} and new links are empty (matches existing nil → no-op).
	vec.On("GetMeta", mock.Anything, noteID).Return(portout.IndexMeta{
		ID:       noteID,
		FilePath: filepath.Join(notesDir, "work/projects/"+noteID+".md"),
		Kind:     "atom",
	}, true, nil)
	vec.On("GetEmbedding", mock.Anything, noteID).Return([]float32{1, 0, 0}, true, nil)
	vec.On("SearchByVector", mock.Anything, mock.Anything, mock.Anything).Return([]portout.SearchHit{
		{ID: noteID, Meta: portout.IndexMeta{ID: noteID, Kind: "atom"}},
	}, nil)

	rewriter := func(_ context.Context, _ string, _ []string) error { return nil }
	ix := New(notesDir, embedder, vec, rewriter, Config{BatchSize: 10, LinkTopK: 5, LinkMinSimilarity: 0.7})
	require.NoError(t, ix.RunOnce(context.Background()))
}

func TestIndexer_UnchangedFile_SkipsEmbed(t *testing.T) {
	notesDir := t.TempDir()
	noteID := "20260527-y"
	body := "stable body"
	writeNote(t, notesDir, noteID+".md", domain.Note{
		ID:            noteID,
		SchemaVersion: domain.SchemaVersion,
		Date:          time.Now(),
		Source:        domain.SourceTelegramText,
		Kind:          domain.KindAtom,
		Category:      "x",
		Tags:          []string{},
		Ingest:        domain.IngestMeta{DumpID: "d", ModelAtomize: "m"},
		Body:          body,
	})

	embedder := mocks.NewMockEmbedder(t) // no calls expected
	vec := mocks.NewMockVectorIndex(t)
	// DB has the same body_hash → diff yields empty toEmbed/toDelete.
	hash := bodyHashOf(body + "\n")
	vec.On("ListAllMeta", mock.Anything).Return([]portout.IndexMeta{
		{ID: noteID, BodyHash: hash},
	}, nil).Once()

	ix := New(notesDir, embedder, vec, nil, Config{BatchSize: 10, LinkTopK: 5})
	require.NoError(t, ix.RunOnce(context.Background()))
}

func TestIndexer_DeletedFile_PrunedFromIndex(t *testing.T) {
	notesDir := t.TempDir() // empty — no notes on disk
	embedder := mocks.NewMockEmbedder(t)

	vec := mocks.NewMockVectorIndex(t)
	vec.On("ListAllMeta", mock.Anything).Return([]portout.IndexMeta{
		{ID: "gone", BodyHash: "h"},
	}, nil).Once()
	vec.On("DeleteByIDs", mock.Anything, []string{"gone"}).Return(nil).Once()

	ix := New(notesDir, embedder, vec, nil, Config{BatchSize: 10, LinkTopK: 5})
	require.NoError(t, ix.RunOnce(context.Background()))
}

func writeNote(t *testing.T, root, rel string, n domain.Note) {
	t.Helper()
	p := filepath.Join(root, rel)
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
	data, err := domain.MarshalNote(n)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(p, data, 0o644))
}

func bodyHashOf(body string) string {
	// Mirror the indexer's scan hashing so the diff round-trips. If scan
	// changes its hashing scheme, this needs updating.
	h := sha256.Sum256([]byte(body))
	return hex.EncodeToString(h[:])
}
