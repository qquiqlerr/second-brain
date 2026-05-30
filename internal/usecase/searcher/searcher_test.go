package searcher

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

func TestSearcher_Find_ReturnsTopK(t *testing.T) {
	embedder := mocks.NewMockEmbedder(t)
	vec := mocks.NewMockVectorIndex(t)

	embedder.On("Embed", mock.Anything, []string{"auth bug"}, portout.EmbedQuery).
		Return([][]float32{{0.1, 0.2}}, nil).Once()
	vec.On("SearchByVector", mock.Anything, []float32{0.1, 0.2}, mock.MatchedBy(func(q portout.SearchQuery) bool {
		return q.TopK == 5 && len(q.KindFilter) == 1 && q.KindFilter[0] == "atom"
	})).Return([]portout.SearchHit{
		{ID: "a", Score: 0.9, Meta: portout.IndexMeta{ID: "a", Category: "work", FilePath: "/data/notes/work/a.md"}},
	}, nil).Once()

	s := New(embedder, vec, Config{TopK: 5})
	hits, err := s.Find(context.Background(), "auth bug", Options{})
	require.NoError(t, err)
	require.Len(t, hits, 1)
	assert.Equal(t, "a", hits[0].ID)
}

func TestSearcher_Find_EmptyQuery_ReturnsError(t *testing.T) {
	s := New(nil, nil, Config{TopK: 5})
	_, err := s.Find(context.Background(), "  ", Options{})
	assert.Error(t, err)
}

func TestSearcher_Find_AppliesCategoryFilter(t *testing.T) {
	embedder := mocks.NewMockEmbedder(t)
	vec := mocks.NewMockVectorIndex(t)

	embedder.On("Embed", mock.Anything, mock.Anything, portout.EmbedQuery).Return([][]float32{{1, 0}}, nil).Once()
	vec.On("SearchByVector", mock.Anything, mock.Anything, mock.MatchedBy(func(q portout.SearchQuery) bool {
		return q.CategoryPrefix == "work/" && !q.DateFrom.IsZero() && q.DateFrom.Equal(time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC))
	})).Return(nil, nil).Once()

	s := New(embedder, vec, Config{TopK: 5})
	_, err := s.Find(context.Background(), "q", Options{
		CategoryPrefix: "work/",
		DateFrom:       time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)
}
