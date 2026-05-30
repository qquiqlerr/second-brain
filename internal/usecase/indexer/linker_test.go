package indexer

import (
	"context"
	"testing"

	portout "github.com/aleksejmetlusko/second-brain/internal/port/out"
	"github.com/aleksejmetlusko/second-brain/internal/port/out/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestLinker_AssignsTopKAtomNeighborsAboveThreshold(t *testing.T) {
	vec := mocks.NewMockVectorIndex(t)

	// Meta lookups — all atoms. The seed (a) and its neighbors (b, c) all
	// end up in the affected set, so each gets a GetMeta call in phase 2.
	vec.On("GetMeta", mock.Anything, "a").Return(portout.IndexMeta{ID: "a", FilePath: "a.md", Kind: "atom"}, true, nil)
	vec.On("GetMeta", mock.Anything, "b").Return(portout.IndexMeta{ID: "b", FilePath: "b.md", Kind: "atom"}, true, nil)
	vec.On("GetMeta", mock.Anything, "c").Return(portout.IndexMeta{ID: "c", FilePath: "c.md", Kind: "atom"}, true, nil)

	// Embeddings
	aVec := []float32{1, 0, 0}
	bVec := []float32{0.9, 0.1, 0}
	cVec := []float32{0.3, 0.3, 0.9}
	vec.On("GetEmbedding", mock.Anything, "a").Return(aVec, true, nil)
	vec.On("GetEmbedding", mock.Anything, "b").Return(bVec, true, nil)
	vec.On("GetEmbedding", mock.Anything, "c").Return(cVec, true, nil)

	// Searches keyed on query vector.
	vec.On("SearchByVector", mock.Anything, aVec, mock.Anything).Return([]portout.SearchHit{
		{ID: "a", Score: 1.0, Meta: portout.IndexMeta{ID: "a", Kind: "atom"}},
		{ID: "b", Score: 0.9, Meta: portout.IndexMeta{ID: "b", Kind: "atom"}},
		{ID: "c", Score: 0.5, Meta: portout.IndexMeta{ID: "c", Kind: "atom"}}, // below 0.7 threshold
	}, nil)
	vec.On("SearchByVector", mock.Anything, bVec, mock.Anything).Return([]portout.SearchHit{
		{ID: "b", Score: 1.0, Meta: portout.IndexMeta{ID: "b", Kind: "atom"}},
		{ID: "a", Score: 0.9, Meta: portout.IndexMeta{ID: "a", Kind: "atom"}},
	}, nil)
	// c has no neighbors above threshold — its linked_notes stay empty, which
	// equals its starting nil so no UpdateLinkedNotes call is expected.
	vec.On("SearchByVector", mock.Anything, cVec, mock.Anything).Return([]portout.SearchHit{
		{ID: "c", Score: 1.0, Meta: portout.IndexMeta{ID: "c", Kind: "atom"}},
		{ID: "a", Score: 0.5, Meta: portout.IndexMeta{ID: "a", Kind: "atom"}},
	}, nil)

	// Critical assertions: each affected atom gets its top-K (above threshold,
	// self excluded) written back.
	vec.On("UpdateLinkedNotes", mock.Anything, "a", []string{"b"}).Return(nil).Once()
	vec.On("UpdateLinkedNotes", mock.Anything, "b", []string{"a"}).Return(nil).Once()

	var rewriteCalls []string
	rewriter := func(_ context.Context, path string, _ []string) error {
		rewriteCalls = append(rewriteCalls, path)
		return nil
	}

	l := NewLinker(vec, rewriter, Config{LinkTopK: 5, LinkMinSimilarity: 0.7})
	err := l.Recompute(context.Background(), []string{"a"})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"a.md", "b.md"}, rewriteCalls)
}

func TestLinker_NoOpWhenLinksUnchanged(t *testing.T) {
	vec := mocks.NewMockVectorIndex(t)

	vec.On("GetMeta", mock.Anything, "a").Return(portout.IndexMeta{ID: "a", FilePath: "a.md", Kind: "atom", LinkedNotes: []string{"b"}}, true, nil)
	vec.On("GetMeta", mock.Anything, "b").Return(portout.IndexMeta{ID: "b", FilePath: "b.md", Kind: "atom", LinkedNotes: []string{"a"}}, true, nil)

	aVec := []float32{1, 0, 0}
	bVec := []float32{0.9, 0.1, 0}
	vec.On("GetEmbedding", mock.Anything, "a").Return(aVec, true, nil)
	vec.On("GetEmbedding", mock.Anything, "b").Return(bVec, true, nil)

	vec.On("SearchByVector", mock.Anything, aVec, mock.Anything).Return([]portout.SearchHit{
		{ID: "a", Score: 1.0, Meta: portout.IndexMeta{ID: "a", Kind: "atom"}},
		{ID: "b", Score: 0.9, Meta: portout.IndexMeta{ID: "b", Kind: "atom"}},
	}, nil)
	vec.On("SearchByVector", mock.Anything, bVec, mock.Anything).Return([]portout.SearchHit{
		{ID: "b", Score: 1.0, Meta: portout.IndexMeta{ID: "b", Kind: "atom"}},
		{ID: "a", Score: 0.9, Meta: portout.IndexMeta{ID: "a", Kind: "atom"}},
	}, nil)

	rewriter := func(_ context.Context, _ string, _ []string) error {
		t.Fatal("rewriter must not be called when links unchanged")
		return nil
	}
	l := NewLinker(vec, rewriter, Config{LinkTopK: 5, LinkMinSimilarity: 0.7})
	err := l.Recompute(context.Background(), []string{"a"})
	require.NoError(t, err)
	// UpdateLinkedNotes is intentionally absent from the mock — the mock will
	// fail at cleanup if it's called.
}

func TestLinker_SkipsSummaries(t *testing.T) {
	vec := mocks.NewMockVectorIndex(t)
	vec.On("GetMeta", mock.Anything, "s").Return(portout.IndexMeta{ID: "s", Kind: "summary"}, true, nil)

	l := NewLinker(vec, nil, Config{LinkTopK: 5, LinkMinSimilarity: 0.7})
	err := l.Recompute(context.Background(), []string{"s"})
	require.NoError(t, err)
}
