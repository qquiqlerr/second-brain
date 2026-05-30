package openaiembed

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/aleksejmetlusko/second-brain/internal/adapter/httpretry"
	portout "github.com/aleksejmetlusko/second-brain/internal/port/out"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEmbedder_HappyPath_ReturnsVectors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/embeddings", r.URL.Path)
		assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))

		var req map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		assert.Equal(t, "openai/text-embedding-3-small", req["model"])
		assert.Equal(t, []any{"hello", "world"}, req["input"])
		// OpenAI shape: no input_type field.
		_, hasInputType := req["input_type"]
		assert.False(t, hasInputType, "input_type must not be sent in OpenAI shape")

		resp := map[string]any{
			"data": []map[string]any{
				{"embedding": []float32{0.1, 0.2, 0.3}, "index": 0},
				{"embedding": []float32{0.4, 0.5, 0.6}, "index": 1},
			},
			"model": "openai/text-embedding-3-small",
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := NewClient(ClientConfig{
		APIKey:      "test-key",
		BaseURL:     srv.URL,
		HTTPTimeout: 5 * time.Second,
		Retry:       httpretry.Default(),
	})
	e := NewEmbedder(c, "openai/text-embedding-3-small", 3)

	vecs, err := e.Embed(t.Context(), []string{"hello", "world"}, portout.EmbedDocument)
	require.NoError(t, err)
	require.Len(t, vecs, 2)
	assert.Equal(t, []float32{0.1, 0.2, 0.3}, vecs[0])
	assert.Equal(t, []float32{0.4, 0.5, 0.6}, vecs[1])
}

func TestEmbedder_Returns429AsRetryable(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls < 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(w, "slow down")
			return
		}
		resp := map[string]any{
			"data":  []map[string]any{{"embedding": []float32{1, 0}, "index": 0}},
			"model": "m",
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := NewClient(ClientConfig{APIKey: "k", BaseURL: srv.URL, HTTPTimeout: 5 * time.Second, Retry: httpretry.Policy{MaxAttempts: 3, BaseDelay: time.Millisecond, MaxDelay: time.Millisecond, Jitter: 0}})
	e := NewEmbedder(c, "m", 2)

	vecs, err := e.Embed(t.Context(), []string{"x"}, portout.EmbedQuery)
	require.NoError(t, err)
	assert.Len(t, vecs, 1)
	assert.Equal(t, 3, calls)
}

func TestEmbedder_400IsTerminal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, "bad model")
	}))
	defer srv.Close()

	c := NewClient(ClientConfig{APIKey: "k", BaseURL: srv.URL, HTTPTimeout: 5 * time.Second, Retry: httpretry.Default()})
	e := NewEmbedder(c, "m", 2)

	_, err := e.Embed(t.Context(), []string{"x"}, portout.EmbedQuery)
	require.Error(t, err)
}

func TestEmbedder_DimReturnsConfigured(t *testing.T) {
	e := NewEmbedder(nil, "m", 1536)
	assert.Equal(t, 1536, e.Dim())
}
