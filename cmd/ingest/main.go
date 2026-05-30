// Command ingest is the entry point for the Ingestion MVP service.
// It wires adapters together and runs the Telegram long-poll loop.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/go-telegram/bot"

	"github.com/aleksejmetlusko/second-brain/internal/adapter/httpretry"
	"github.com/aleksejmetlusko/second-brain/internal/adapter/in/telegram"
	"github.com/aleksejmetlusko/second-brain/internal/adapter/out/fs"
	"github.com/aleksejmetlusko/second-brain/internal/adapter/out/openrouter"
	"github.com/aleksejmetlusko/second-brain/internal/adapter/out/sqlitevec"
	"github.com/aleksejmetlusko/second-brain/internal/adapter/out/voyage"
	"github.com/aleksejmetlusko/second-brain/internal/config"
	"github.com/aleksejmetlusko/second-brain/internal/domain"
	"github.com/aleksejmetlusko/second-brain/internal/usecase"
	"github.com/aleksejmetlusko/second-brain/internal/usecase/indexer"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	logger := newLogger(cfg.LogLevel)
	slog.SetDefault(logger)

	if _, err := time.LoadLocation(cfg.TZ); err != nil {
		return fmt.Errorf("invalid TZ %s: %w", cfg.TZ, err)
	}

	taxLoader := fs.NewTaxonomyLoader(filepath.Join(cfg.ConfigDir, "taxonomy.yml"), time.Minute)
	if _, err := taxLoader.Load(context.Background()); err != nil {
		return fmt.Errorf("initial taxonomy load: %w", err)
	}

	noteStore := fs.NewNoteStore(cfg.NotesDir)

	openrtrClient := openrouter.New(openrouter.ClientConfig{
		APIKey:      cfg.OpenRouterAPIKey,
		BaseURL:     cfg.OpenRouterBaseURL,
		HTTPReferer: cfg.HTTPReferer,
		XTitle:      cfg.XTitle,
		HTTPTimeout: cfg.HTTPTimeout,
		Retry:       httpretry.Default(),
	})
	atomizer := openrouter.NewAtomizer(openrtrClient, cfg.AtomizeModel)
	transcriber := openrouter.NewTranscriber(openrtrClient, cfg.TranscribeModel)

	uc := usecase.NewIngestUseCase(transcriber, atomizer, noteStore, taxLoader, cfg.AtomizeModel, cfg.TranscribeModel)
	uc.WithTimeouts(usecase.StageTimeouts{
		Transcribe: cfg.TranscribeTimeout,
		Atomize:    cfg.AtomizeTimeout,
		Write:      cfg.FSWriteTimeout,
	})

	voyageClient := voyage.NewClient(voyage.ClientConfig{
		APIKey:      cfg.VoyageAPIKey,
		BaseURL:     cfg.VoyageBaseURL,
		HTTPTimeout: cfg.HTTPTimeout,
		Retry:       httpretry.Default(),
	})
	embedder := voyage.NewEmbedder(voyageClient, cfg.EmbeddingModel, cfg.EmbeddingDim)

	vec, err := sqlitevec.Open(context.Background(), sqlitevec.Config{
		DBPath:         cfg.IndexDBPath,
		EmbeddingDim:   cfg.EmbeddingDim,
		EmbeddingModel: cfg.EmbeddingModel,
	})
	if err != nil {
		return fmt.Errorf("open vector index: %w", err)
	}
	defer func() { _ = vec.Close() }()

	rewriter := func(_ context.Context, path string, links []string) error {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out, err := domain.UpdateFrontmatter(data, links)
		if err != nil {
			return err
		}
		tmp := path + ".tmp"
		if err := os.WriteFile(tmp, out, 0o644); err != nil {
			return err
		}
		return os.Rename(tmp, path)
	}

	ix := indexer.New(cfg.NotesDir, embedder, vec, rewriter, indexer.Config{
		BatchSize:         cfg.IndexerBatchSize,
		LinkTopK:          cfg.LinkTopK,
		LinkMinSimilarity: cfg.LinkMinSimilarity,
	})

	allowed := make(map[int64]struct{}, len(cfg.AllowedUserIDs))
	for _, id := range cfg.AllowedUserIDs {
		allowed[id] = struct{}{}
	}
	dedup := telegram.NewUpdateDedup(1024)
	router := telegram.NewRouter(uc, allowed, dedup, logger)
	tgHandler := telegram.NewTelegramHandler(router, cfg.TelegramBotToken)

	b, err := bot.New(cfg.TelegramBotToken, bot.WithDefaultHandler(tgHandler.Handle))
	if err != nil {
		return fmt.Errorf("init telegram bot: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go ix.Loop(ctx, cfg.RAGScanInterval)

	logger.Info("starting bot",
		"atomize_model", cfg.AtomizeModel,
		"transcribe_model", cfg.TranscribeModel,
		"embedding_model", cfg.EmbeddingModel,
		"notes_dir", cfg.NotesDir,
		"index_db_path", cfg.IndexDBPath,
	)
	b.Start(ctx)

	if err := ctx.Err(); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	logger.Info("shutdown complete")
	return nil
}

func newLogger(level string) *slog.Logger {
	var lvl slog.Level
	_ = lvl.UnmarshalText([]byte(level))
	return slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: lvl}))
}
