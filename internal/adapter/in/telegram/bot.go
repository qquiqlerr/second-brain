package telegram

import (
	"context"
	"log/slog"

	"github.com/go-telegram/bot"
)

// Bot wraps a *bot.Bot with allowlist and dedup state shared by handlers.
type Bot struct {
	BotAPI         *bot.Bot
	AllowedUserIDs map[int64]struct{}
	Dedup          *UpdateDedup
	Logger         *slog.Logger
}

// NewBot constructs a Bot wrapper. The wrapper does not start polling on its
// own; the composition root attaches handlers and calls Start.
func NewBot(api *bot.Bot, allowed []int64, logger *slog.Logger) *Bot {
	set := make(map[int64]struct{}, len(allowed))
	for _, id := range allowed {
		set[id] = struct{}{}
	}
	return &Bot{
		BotAPI:         api,
		AllowedUserIDs: set,
		Dedup:          NewUpdateDedup(1024),
		Logger:         logger,
	}
}

// Start begins long-polling. The context controls shutdown.
func (b *Bot) Start(ctx context.Context) {
	b.BotAPI.Start(ctx)
}
