package indexer

import (
	"context"
	"testing"
	"time"

	portout "github.com/aleksejmetlusko/second-brain/internal/port/out"
	"github.com/aleksejmetlusko/second-brain/internal/port/out/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestLinker_AssignsTopKAtomNeighborsAboveThreshold(t *testing.T) {
	vec := mocks.NewMockVectorIndex(t)

	now := time.Now()
	// Seed `a` is the newest; b/c are older. Temporal rule means linker
	// only computes for `a`, looks at past atoms only.
	vec.On("GetMeta", mock.Anything, "a").Return(portout.IndexMeta{
		ID: "a", FilePath: "a.md", Kind: "atom", Date: now,
	}, true, nil).Once()

	aVec := []float32{1, 0, 0}
	vec.On("GetEmbedding", mock.Anything, "a").Return(aVec, true, nil).Once()

	// Crucial: the linker must pass DateBefore=a.Date so sqlite-vec excludes
	// future/same-time atoms. We assert that here.
	vec.On("SearchByVector", mock.Anything, aVec, mock.MatchedBy(func(q portout.SearchQuery) bool {
		return q.DateBefore.Equal(now) && len(q.KindFilter) == 1 && q.KindFilter[0] == "atom"
	})).Return([]portout.SearchHit{
		{ID: "b", Score: 0.9, Meta: portout.IndexMeta{ID: "b", Kind: "atom"}},
		{ID: "c", Score: 0.5, Meta: portout.IndexMeta{ID: "c", Kind: "atom"}}, // below 0.7 threshold
	}, nil).Once()

	vec.On("UpdateLinkedNotes", mock.Anything, "a", []string{"b"}).Return(nil).Once()

	var rewritePath string
	rewriter := func(_ context.Context, path string, _ []string) error {
		rewritePath = path
		return nil
	}

	l := NewLinker(vec, rewriter, Config{LinkTopK: 5, LinkMinSimilarity: 0.7})
	err := l.Recompute(context.Background(), []string{"a"})
	require.NoError(t, err)
	assert.Equal(t, "a.md", rewritePath)
}

func TestLinker_NoOpWhenLinksUnchanged(t *testing.T) {
	vec := mocks.NewMockVectorIndex(t)

	now := time.Now()
	vec.On("GetMeta", mock.Anything, "a").Return(portout.IndexMeta{
		ID: "a", FilePath: "a.md", Kind: "atom", Date: now, LinkedNotes: []string{"b"},
	}, true, nil).Once()

	aVec := []float32{1, 0, 0}
	vec.On("GetEmbedding", mock.Anything, "a").Return(aVec, true, nil).Once()
	vec.On("SearchByVector", mock.Anything, aVec, mock.Anything).Return([]portout.SearchHit{
		{ID: "b", Score: 0.9, Meta: portout.IndexMeta{ID: "b", Kind: "atom"}},
	}, nil).Once()

	rewriter := func(_ context.Context, _ string, _ []string) error {
		t.Fatal("rewriter must not be called when links unchanged")
		return nil
	}
	l := NewLinker(vec, rewriter, Config{LinkTopK: 5, LinkMinSimilarity: 0.7})
	err := l.Recompute(context.Background(), []string{"a"})
	require.NoError(t, err)
	// UpdateLinkedNotes is intentionally absent from the mock — the mock
	// will fail at cleanup if it's called.
}

func TestLinker_SkipsSummaries(t *testing.T) {
	vec := mocks.NewMockVectorIndex(t)
	vec.On("GetMeta", mock.Anything, "s").Return(portout.IndexMeta{ID: "s", Kind: "summary"}, true, nil).Once()

	l := NewLinker(vec, nil, Config{LinkTopK: 5, LinkMinSimilarity: 0.7})
	err := l.Recompute(context.Background(), []string{"s"})
	require.NoError(t, err)
}

func TestLinker_DoesNotProcessNeighbors(t *testing.T) {
	// Regression: under the old bidirectional design, calling Recompute([a])
	// would expand the affected set to a + a's neighbors and rewrite each
	// of them. Under temporal rule only `a` gets recomputed; older neighbors
	// stay unchanged. The mock has no GetMeta("b") expectation — if the
	// linker tries to load it, the mock will fail.
	vec := mocks.NewMockVectorIndex(t)

	now := time.Now()
	vec.On("GetMeta", mock.Anything, "a").Return(portout.IndexMeta{
		ID: "a", FilePath: "a.md", Kind: "atom", Date: now,
	}, true, nil).Once()
	vec.On("GetEmbedding", mock.Anything, "a").Return([]float32{1, 0, 0}, true, nil).Once()
	vec.On("SearchByVector", mock.Anything, mock.Anything, mock.Anything).Return([]portout.SearchHit{
		{ID: "b", Score: 0.9, Meta: portout.IndexMeta{ID: "b", Kind: "atom"}},
	}, nil).Once()
	vec.On("UpdateLinkedNotes", mock.Anything, "a", []string{"b"}).Return(nil).Once()

	l := NewLinker(vec, nil, Config{LinkTopK: 5, LinkMinSimilarity: 0.7})
	require.NoError(t, l.Recompute(context.Background(), []string{"a"}))
}
