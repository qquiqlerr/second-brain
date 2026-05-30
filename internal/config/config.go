// Package config loads runtime configuration from environment variables.
package config

import (
	"fmt"
	"slices"
	"time"

	"github.com/ilyakaznacheev/cleanenv"
)

// Config is the fully-populated runtime configuration.
type Config struct {
	OpenRouterAPIKey  string `env:"OPENROUTER_API_KEY"          env-required:"true"   env-description:"OpenRouter API key (sk-or-...)"`
	OpenRouterBaseURL string `env:"OPENROUTER_BASE_URL"         env-default:"https://openrouter.ai/api/v1"`
	AtomizeModel      string `env:"OPENROUTER_ATOMIZE_MODEL"    env-default:"anthropic/claude-haiku-4.5"`
	TranscribeModel   string `env:"OPENROUTER_TRANSCRIBE_MODEL" env-default:"openai/whisper-1"`
	HTTPReferer       string `env:"OPENROUTER_HTTP_REFERER"     env-default:"https://github.com/aleksejmetlusko/second-brain"`
	XTitle            string `env:"OPENROUTER_X_TITLE"          env-default:"Second Brain"`

	TelegramBotToken string  `env:"TELEGRAM_BOT_TOKEN" env-required:"true"`
	AllowedUserIDs   []int64 `env:"ALLOWED_USER_IDS"   env-required:"true" env-separator:"," env-description:"CSV of allowlisted Telegram user IDs"`

	NotesDir  string `env:"NOTES_DIR"  env-default:"/data/notes"`
	ConfigDir string `env:"CONFIG_DIR" env-default:"/etc/second-brain"`

	HTTPTimeout       time.Duration `env:"HTTP_TIMEOUT"       env-default:"120s"`
	TranscribeTimeout time.Duration `env:"TRANSCRIBE_TIMEOUT" env-default:"180s"`
	AtomizeTimeout    time.Duration `env:"ATOMIZE_TIMEOUT"    env-default:"60s"`
	FSWriteTimeout    time.Duration `env:"FS_WRITE_TIMEOUT"   env-default:"5s"`

	TZ       string `env:"TZ"        env-default:"Europe/Moscow"`
	LogLevel string `env:"LOG_LEVEL" env-default:"info" env-description:"debug | info | warn | error"`

	// Embeddings go through the same OpenRouter base URL + API key as the
	// atomizer (OPENROUTER_*). Direct providers (OpenAI, Voyage, etc.) are
	// often geo-blocked from RU IPs; OpenRouter proxies them.
	EmbeddingModel string `env:"EMBEDDING_MODEL" env-default:"openai/text-embedding-3-small"`
	EmbeddingDim   int    `env:"EMBEDDING_DIM"   env-default:"1536"`

	IndexDBPath       string        `env:"INDEX_DB_PATH"       env-default:"/data/index/index.db"`
	RAGScanInterval   time.Duration `env:"RAG_SCAN_INTERVAL"   env-default:"5m"`
	IndexerBatchSize  int           `env:"INDEXER_BATCH_SIZE"  env-default:"1000"`
	LinkTopK          int           `env:"LINK_TOP_K"          env-default:"5"`
	LinkMinSimilarity float32       `env:"LINK_MIN_SIMILARITY" env-default:"0.30"`
	FindTopK          int           `env:"FIND_TOP_K"          env-default:"5"`
}

var allowedLogLevels = []string{"debug", "info", "warn", "error"}

// Load parses environment variables according to struct tags.
func Load() (Config, error) {
	var cfg Config
	if err := cleanenv.ReadEnv(&cfg); err != nil {
		return Config{}, fmt.Errorf("read env: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Validate enforces semantic rules that cleanenv tags cannot express.
func (c Config) Validate() error {
	if c.OpenRouterAPIKey == "" {
		return fmt.Errorf("OPENROUTER_API_KEY is required but empty")
	}
	if c.TelegramBotToken == "" {
		return fmt.Errorf("TELEGRAM_BOT_TOKEN is required but empty")
	}
	if !slices.Contains(allowedLogLevels, c.LogLevel) {
		return fmt.Errorf("invalid LOG_LEVEL %q (expected one of %v)", c.LogLevel, allowedLogLevels)
	}
	if len(c.AllowedUserIDs) == 0 {
		return fmt.Errorf("ALLOWED_USER_IDS must contain at least one user")
	}
	if c.EmbeddingDim <= 0 {
		return fmt.Errorf("EMBEDDING_DIM must be positive, got %d", c.EmbeddingDim)
	}
	if c.LinkTopK <= 0 {
		return fmt.Errorf("LINK_TOP_K must be positive, got %d", c.LinkTopK)
	}
	if c.LinkMinSimilarity < -1 || c.LinkMinSimilarity > 1 {
		return fmt.Errorf("LINK_MIN_SIMILARITY must be in [-1, 1], got %v", c.LinkMinSimilarity)
	}
	if c.FindTopK <= 0 {
		return fmt.Errorf("FIND_TOP_K must be positive, got %d", c.FindTopK)
	}
	return nil
}

// Description returns the auto-generated env description for tooling.
func Description() (string, error) {
	var cfg Config
	return cleanenv.GetDescription(&cfg, nil)
}
