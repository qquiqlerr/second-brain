package config_test

import (
	"testing"
	"time"

	"github.com/aleksejmetlusko/second-brain/internal/config"
	"github.com/stretchr/testify/require"
)

func setRequired(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "sk-or-test")
	t.Setenv("TELEGRAM_BOT_TOKEN", "tg-test")
	t.Setenv("ALLOWED_USER_IDS", "11,22")
	t.Setenv("VOYAGE_API_KEY", "pa-test")
}

func TestLoad_DefaultsApplied(t *testing.T) {
	setRequired(t)
	cfg, err := config.Load()
	require.NoError(t, err)
	require.Equal(t, "anthropic/claude-haiku-4.5", cfg.AtomizeModel)
	require.Equal(t, "openai/whisper-1", cfg.TranscribeModel)
	require.Equal(t, "/data/notes", cfg.NotesDir)
	require.Equal(t, 120*time.Second, cfg.HTTPTimeout)
	require.Equal(t, "Europe/Moscow", cfg.TZ)
	require.Equal(t, []int64{11, 22}, cfg.AllowedUserIDs)
}

func TestLoad_RequiredMissing(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "")
	t.Setenv("TELEGRAM_BOT_TOKEN", "tg-test")
	t.Setenv("ALLOWED_USER_IDS", "11")
	_, err := config.Load()
	require.Error(t, err)
}

func TestValidate_RejectsBadLogLevel(t *testing.T) {
	cfg := config.Config{LogLevel: "loud"}
	require.Error(t, cfg.Validate())
}

func TestValidate_AcceptsKnownLogLevels(t *testing.T) {
	for _, lvl := range []string{"debug", "info", "warn", "error"} {
		cfg := config.Config{
			LogLevel:         lvl,
			OpenRouterAPIKey: "k",
			TelegramBotToken: "t",
			AllowedUserIDs:   []int64{1},
			VoyageAPIKey:     "pa-test",
			EmbeddingDim:     1024,
		}
		require.NoError(t, cfg.Validate())
	}
}

func TestLoad_MissingVoyageKey_Fails(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "sk-or-test")
	t.Setenv("TELEGRAM_BOT_TOKEN", "tg-test")
	t.Setenv("ALLOWED_USER_IDS", "11,22")
	t.Setenv("VOYAGE_API_KEY", "")
	_, err := config.Load()
	require.Error(t, err)
}
