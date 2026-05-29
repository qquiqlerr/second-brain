package openrouter_test

import (
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/aleksejmetlusko/second-brain/internal/adapter/httpretry"
	"github.com/aleksejmetlusko/second-brain/internal/adapter/out/openrouter"
	"github.com/stretchr/testify/require"
)

func newTranscriberFakeServer(t *testing.T, handler http.HandlerFunc) *openrouter.Transcriber {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	cli := openrouter.New(openrouter.ClientConfig{
		APIKey:      "test",
		BaseURL:     srv.URL,
		HTTPTimeout: 5 * time.Second,
		Retry:       httpretry.Policy{MaxAttempts: 2, BaseDelay: time.Millisecond},
	})
	return openrouter.NewTranscriber(cli, "openai/whisper-1")
}

func TestTranscriber_SendsMultipart(t *testing.T) {
	transcriber := newTranscriberFakeServer(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/audio/transcriptions", r.URL.Path)
		require.Equal(t, "Bearer test", r.Header.Get("Authorization"))

		mt, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		require.NoError(t, err)
		require.Equal(t, "multipart/form-data", mt)
		require.NotEmpty(t, params["boundary"])

		err = r.ParseMultipartForm(1 << 20)
		require.NoError(t, err)
		require.Equal(t, []string{"openai/whisper-1"}, r.MultipartForm.Value["model"])
		require.Len(t, r.MultipartForm.File["file"], 1)

		_ = json.NewEncoder(w).Encode(map[string]string{"text": "hello world"})
	})

	got, err := transcriber.Transcribe(t.Context(), strings.NewReader("AUDIO"), "audio/ogg")
	require.NoError(t, err)
	require.Equal(t, "hello world", got)
}

func TestTranscriber_ReturnsHTTPError(t *testing.T) {
	transcriber := newTranscriberFakeServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, "no")
	})

	_, err := transcriber.Transcribe(t.Context(), strings.NewReader("AUDIO"), "audio/ogg")
	require.Error(t, err)
}
