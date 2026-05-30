//go:build integration

package sqlitevec_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/aleksejmetlusko/second-brain/internal/adapter/out/sqlitevec"
	portout "github.com/aleksejmetlusko/second-brain/internal/port/out"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func openTestIndex(t *testing.T) *sqlitevec.Index {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	idx, err := sqlitevec.Open(context.Background(), sqlitevec.Config{
		DBPath:         path,
		EmbeddingDim:   3,
		EmbeddingModel: "test-model",
	})
	require.NoError(t, err)
	t.Cleanup(func() { idx.Close() })
	return idx
}

func TestIndex_UpsertAndSearchRoundtrip(t *testing.T) {
	idx := openTestIndex(t)
	ctx := t.Context()

	items := []portout.IndexItem{
		{ID: "a", FilePath: "a.md", BodyHash: "h1", Category: "work", Kind: "atom", Date: time.Now(), Embedding: []float32{1, 0, 0}},
		{ID: "b", FilePath: "b.md", BodyHash: "h2", Category: "work", Kind: "atom", Date: time.Now(), Embedding: []float32{0, 1, 0}},
		{ID: "c", FilePath: "c.md", BodyHash: "h3", Category: "personal", Kind: "atom", Date: time.Now(), Embedding: []float32{0, 0, 1}},
	}
	require.NoError(t, idx.Upsert(ctx, items))

	hits, err := idx.SearchByVector(ctx, []float32{1, 0, 0}, portout.SearchQuery{TopK: 2})
	require.NoError(t, err)
	require.Len(t, hits, 2)
	assert.Equal(t, "a", hits[0].ID)
}

func TestIndex_DeleteByIDs(t *testing.T) {
	idx := openTestIndex(t)
	ctx := t.Context()

	require.NoError(t, idx.Upsert(ctx, []portout.IndexItem{
		{ID: "a", FilePath: "a.md", BodyHash: "h", Category: "x", Kind: "atom", Date: time.Now(), Embedding: []float32{1, 0, 0}},
	}))
	require.NoError(t, idx.DeleteByIDs(ctx, []string{"a"}))

	_, found, err := idx.GetMeta(ctx, "a")
	require.NoError(t, err)
	assert.False(t, found)
}

func TestIndex_UpdateLinkedNotes(t *testing.T) {
	idx := openTestIndex(t)
	ctx := t.Context()

	require.NoError(t, idx.Upsert(ctx, []portout.IndexItem{
		{ID: "a", FilePath: "a.md", BodyHash: "h", Category: "x", Kind: "atom", Date: time.Now(), Embedding: []float32{1, 0, 0}},
	}))
	require.NoError(t, idx.UpdateLinkedNotes(ctx, "a", []string{"b", "c"}))

	meta, found, err := idx.GetMeta(ctx, "a")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, []string{"b", "c"}, meta.LinkedNotes)
}

func TestIndex_GetEmbedding(t *testing.T) {
	idx := openTestIndex(t)
	ctx := t.Context()

	vec := []float32{0.5, 0.5, 0}
	require.NoError(t, idx.Upsert(ctx, []portout.IndexItem{
		{ID: "a", FilePath: "a.md", BodyHash: "h", Category: "x", Kind: "atom", Date: time.Now(), Embedding: vec},
	}))
	got, found, err := idx.GetEmbedding(ctx, "a")
	require.NoError(t, err)
	require.True(t, found)
	assert.InDeltaSlice(t, vec, got, 1e-6)
}

func TestIndex_KindFilterAppliedInSearch(t *testing.T) {
	idx := openTestIndex(t)
	ctx := t.Context()

	require.NoError(t, idx.Upsert(ctx, []portout.IndexItem{
		{ID: "a", FilePath: "a.md", BodyHash: "h", Category: "x", Kind: "atom", Date: time.Now(), Embedding: []float32{1, 0, 0}},
		{ID: "s", FilePath: "s.md", BodyHash: "h", Category: "x", Kind: "summary", Date: time.Now(), Embedding: []float32{0.9, 0.1, 0}},
	}))
	hits, err := idx.SearchByVector(ctx, []float32{1, 0, 0}, portout.SearchQuery{TopK: 5, KindFilter: []string{"atom"}})
	require.NoError(t, err)
	require.Len(t, hits, 1)
	assert.Equal(t, "a", hits[0].ID)
}

func TestIndex_DimMismatchReturnsError(t *testing.T) {
	idx := openTestIndex(t)
	ctx := t.Context()
	err := idx.Upsert(ctx, []portout.IndexItem{
		{ID: "a", Embedding: []float32{1, 0}}, // 2-dim, expected 3
	})
	require.Error(t, err)
}
