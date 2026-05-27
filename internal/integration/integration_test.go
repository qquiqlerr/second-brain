//go:build integration

package integration_test

import (
	"context"
	"encoding/json"
	"flag"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aleksejmetlusko/second-brain/internal/adapter/in/telegram"
	"github.com/aleksejmetlusko/second-brain/internal/adapter/out/fs"
	"github.com/aleksejmetlusko/second-brain/internal/domain"
	"github.com/aleksejmetlusko/second-brain/internal/port/in"
	"github.com/aleksejmetlusko/second-brain/internal/port/out"
	"github.com/aleksejmetlusko/second-brain/internal/usecase"
	"github.com/stretchr/testify/require"
)

var update = flag.Bool("update", false, "rewrite golden files from current output")

// replayAtomizer returns a canned list of notes from a JSON file.
type replayAtomizer struct{ path string }

func (r *replayAtomizer) Atomize(_ context.Context, _ string, _ domain.Taxonomy) ([]domain.Note, error) {
	data, err := os.ReadFile(r.path)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Atoms []struct {
			TitleSlug string   `json:"title_slug"`
			Category  string   `json:"category"`
			Tags      []string `json:"tags"`
			Body      string   `json:"body"`
		} `json:"atoms"`
		Summary *struct {
			TitleSlug string   `json:"title_slug"`
			Tags      []string `json:"tags"`
			Body      string   `json:"body"`
		} `json:"summary"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}
	notes := make([]domain.Note, 0, len(resp.Atoms)+1)
	for _, it := range resp.Atoms {
		notes = append(notes, domain.Note{Kind: domain.KindAtom, Category: it.Category, Tags: it.Tags, Slug: it.TitleSlug, Body: it.Body})
	}
	if resp.Summary != nil && resp.Summary.TitleSlug != "" {
		notes = append(notes, domain.Note{Kind: domain.KindSummary, Tags: resp.Summary.Tags, Slug: resp.Summary.TitleSlug, Body: resp.Summary.Body})
	}
	return notes, nil
}

type stubTranscriber struct{}

func (stubTranscriber) Transcribe(_ context.Context, _ io.Reader, _ string) (string, error) {
	return "", nil
}

func runCase(t *testing.T, caseDir string) {
	t.Helper()
	notesDir := t.TempDir()

	moscow, _ := time.LoadLocation("Europe/Moscow")
	fixedTime := time.Date(2026, 5, 27, 22, 40, 0, 0, moscow)

	store := fs.NewNoteStore(notesDir)
	taxLoader := fs.NewTaxonomyLoader(filepath.Join(caseDir, "taxonomy.yml"), 0)

	atomizer := &replayAtomizer{path: filepath.Join(caseDir, "atomizer_response.json")}

	uc := usecase.NewIngestUseCase(stubTranscriber{}, atomizer, store, taxLoader, "test-atomize", "test-transcribe")
	uc.WithFixedNow(func() time.Time { return fixedTime })
	uc.WithFixedDumpID("00000000-0000-0000-0000-000000000001")

	input, err := os.ReadFile(filepath.Join(caseDir, "input.txt"))
	require.NoError(t, err)

	result, topErr := uc.Execute(context.Background(), in.IngestRequest{
		Source: domain.SourceTelegramText,
		Text:   string(input),
	})

	reply := telegram.FormatReply(result, topErr)

	expectedDir := filepath.Join(caseDir, "expected")
	if *update {
		writeGolden(t, expectedDir, notesDir, reply)
		t.Logf("updated golden for %s", caseDir)
		return
	}

	compareDirs(t, expectedDir, notesDir)
	expectedReply, err := os.ReadFile(filepath.Join(expectedDir, "reply.txt"))
	require.NoError(t, err)
	require.Equal(t, strings.TrimSpace(string(expectedReply)), strings.TrimSpace(reply))
}

func compareDirs(t *testing.T, want, got string) {
	t.Helper()
	err := filepath.Walk(want, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(want, p)
		if rel == "reply.txt" {
			return nil
		}
		expected, _ := os.ReadFile(p)
		actual, _ := os.ReadFile(filepath.Join(got, rel))
		require.Equal(t, string(expected), string(actual), "mismatch in %s", rel)
		return nil
	})
	require.NoError(t, err)
}

func writeGolden(t *testing.T, expectedDir, notesDir, reply string) {
	t.Helper()
	_ = os.RemoveAll(expectedDir)
	require.NoError(t, os.MkdirAll(expectedDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(expectedDir, "reply.txt"), []byte(reply+"\n"), 0o644))
	err := filepath.Walk(notesDir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(notesDir, p)
		dst := filepath.Join(expectedDir, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(dst), 0o755))
		data, _ := os.ReadFile(p)
		require.NoError(t, os.WriteFile(dst, data, 0o644))
		return nil
	})
	require.NoError(t, err)
}

func TestGoldenCases(t *testing.T) {
	root := "../../testdata/golden"
	entries, err := os.ReadDir(root)
	require.NoError(t, err)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		t.Run(e.Name(), func(t *testing.T) {
			runCase(t, filepath.Join(root, e.Name()))
		})
	}
}

var _ out.Atomizer = (*replayAtomizer)(nil)
var _ out.Transcriber = stubTranscriber{}
var _ = slog.Default
