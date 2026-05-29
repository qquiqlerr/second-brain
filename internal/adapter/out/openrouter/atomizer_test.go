package openrouter_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/aleksejmetlusko/second-brain/internal/adapter/httpretry"
	"github.com/aleksejmetlusko/second-brain/internal/adapter/out/openrouter"
	"github.com/aleksejmetlusko/second-brain/internal/domain"
	"github.com/stretchr/testify/require"
)

func newAtomizerFakeServer(t *testing.T, handler http.HandlerFunc) (*openrouter.Atomizer, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	cli := openrouter.New(openrouter.ClientConfig{
		APIKey:      "test",
		BaseURL:     srv.URL,
		HTTPTimeout: 5 * time.Second,
		Retry:       httpretry.Policy{MaxAttempts: 2, BaseDelay: time.Millisecond},
	})
	return openrouter.NewAtomizer(cli, "test-model"), srv
}

func writeChatResponse(w http.ResponseWriter, content string) {
	body := map[string]any{
		"id":      "x",
		"object":  "chat.completion",
		"choices": []map[string]any{{"index": 0, "message": map[string]any{"role": "assistant", "content": content}}},
	}
	_ = json.NewEncoder(w).Encode(body)
}

func TestAtomizer_ParsesValidResponse(t *testing.T) {
	atomizer, _ := newAtomizerFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/chat/completions", r.URL.Path)
		require.Equal(t, "Bearer test", r.Header.Get("Authorization"))
		writeChatResponse(w, `{
			"atoms": [
				{"title_slug":"tms-auth-bug","category":"work/projects/tms","tags":["bug"],"body":"text"},
				{"title_slug":"sleep-idea","category":"health/sleep","tags":[],"body":"hm"}
			],
			"summary": {
				"title_slug": "daily-summary",
				"tags": [],
				"body": "## Задачи\n- t\n## Состояние\n- ok"
			}
		}`)
	})

	tax := buildTaxonomyForAtomizer(t)
	notes, err := atomizer.Atomize(t.Context(), "dump", tax)
	require.NoError(t, err)
	require.Len(t, notes, 3)
	require.Equal(t, domain.KindAtom, notes[0].Kind)
	require.Equal(t, domain.KindAtom, notes[1].Kind)
	require.Equal(t, domain.KindSummary, notes[2].Kind)
	require.Equal(t, "work/projects/tms", notes[0].Category)
	require.Equal(t, "tms-auth-bug", notes[0].Slug)
	require.Equal(t, []string{"bug"}, notes[0].Tags)
	require.Equal(t, "daily-summary", notes[2].Slug)
	require.Empty(t, notes[2].Category, "atomizer must leave summary Category empty for Normalize to set")
}

func TestAtomizer_RejectsMalformedJSON(t *testing.T) {
	atomizer, _ := newAtomizerFakeServer(t, func(w http.ResponseWriter, _ *http.Request) {
		writeChatResponse(w, "definitely not json")
	})

	_, err := atomizer.Atomize(t.Context(), "dump", buildTaxonomyForAtomizer(t))
	require.ErrorIs(t, err, domain.ErrAtomizerBadResponse)
}

func TestAtomizer_RetriesOn5xx(t *testing.T) {
	var calls int
	atomizer, _ := newAtomizerFakeServer(t, func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		writeChatResponse(w, `{"atoms":[{"title_slug":"x","category":"work/projects/tms","tags":[],"body":""}],"summary":null}`)
	})

	notes, err := atomizer.Atomize(t.Context(), "dump", buildTaxonomyForAtomizer(t))
	require.NoError(t, err)
	require.Len(t, notes, 1)
	require.Equal(t, 2, calls)
}

func TestAtomizer_NoRetryOn401(t *testing.T) {
	var calls int
	atomizer, _ := newAtomizerFakeServer(t, func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusUnauthorized)
	})

	_, err := atomizer.Atomize(t.Context(), "dump", buildTaxonomyForAtomizer(t))
	require.Error(t, err)
	require.Equal(t, 1, calls)
}

func buildTaxonomyForAtomizer(t *testing.T) domain.Taxonomy {
	t.Helper()
	tax, err := domain.LoadTaxonomy([]byte(`
version: "1.0"
categories:
  work:
    projects:
      - tms
  health:
    - sleep
tags:
  - bug
`))
	require.NoError(t, err)
	return tax
}

func TestAtomizer_StripsCodeFencesIfPresent(t *testing.T) {
	atomizer, _ := newAtomizerFakeServer(t, func(w http.ResponseWriter, _ *http.Request) {
		writeChatResponse(w, "```json\n{\"atoms\":[],\"summary\":null}\n```")
	})

	notes, err := atomizer.Atomize(t.Context(), "dump", buildTaxonomyForAtomizer(t))
	require.NoError(t, err)
	require.Empty(t, notes)
}

func TestAtomizer_StripsCodeFencesWithTrailingNewline(t *testing.T) {
	atomizer, _ := newAtomizerFakeServer(t, func(w http.ResponseWriter, _ *http.Request) {
		writeChatResponse(w, "```json\n{\"atoms\":[],\"summary\":null}\n```\n")
	})

	notes, err := atomizer.Atomize(t.Context(), "dump", buildTaxonomyForAtomizer(t))
	require.NoError(t, err)
	require.Empty(t, notes)
}

func TestAtomizer_NoSummaryWhenMissing(t *testing.T) {
	atomizer, _ := newAtomizerFakeServer(t, func(w http.ResponseWriter, _ *http.Request) {
		writeChatResponse(w, `{
			"atoms": [
				{"title_slug":"x","category":"work/projects/tms","tags":["bug"],"body":"body"}
			],
			"summary": null
		}`)
	})

	notes, err := atomizer.Atomize(t.Context(), "dump", buildTaxonomyForAtomizer(t))
	require.NoError(t, err)
	require.Len(t, notes, 1)
	require.Equal(t, domain.KindAtom, notes[0].Kind)
	for _, n := range notes {
		require.NotEqual(t, domain.KindSummary, n.Kind)
	}
}
