package telegram

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// TelegramHandler bridges go-telegram/bot models to our adapter-internal
// IncomingUpdate type and dispatches through Router.
type TelegramHandler struct {
	router *Router
	httpC  *http.Client
	token  string
}

// NewTelegramHandler returns a handler compatible with bot.WithDefaultHandler.
func NewTelegramHandler(router *Router, token string) *TelegramHandler {
	return &TelegramHandler{router: router, httpC: http.DefaultClient, token: token}
}

// Handle is the entry point passed to the go-telegram/bot library.
func (h *TelegramHandler) Handle(ctx context.Context, b *bot.Bot, update *models.Update) {
	if update == nil || update.Message == nil {
		return
	}
	msg := update.Message
	in := h.buildIncoming(update, msg, b)

	logger := slog.Default().With("update_id", update.ID, "user_id", msg.From.ID)
	handler := RecoverMiddleware(logger, h.router.Handle)

	reply, err := handler(ctx, in)
	if err != nil {
		logger.Error("handler error", "err", err)
		_, _ = b.SendMessage(ctx, &bot.SendMessageParams{ChatID: msg.Chat.ID, Text: "❌ внутренняя ошибка"})
		return
	}
	if reply == "" {
		return
	}
	_, _ = b.SendMessage(ctx, &bot.SendMessageParams{ChatID: msg.Chat.ID, Text: reply})
}

func (h *TelegramHandler) buildIncoming(update *models.Update, msg *models.Message, b *bot.Bot) IncomingUpdate {
	in := IncomingUpdate{UpdateID: update.ID, FromUserID: msg.From.ID}
	switch {
	case msg.Voice != nil:
		in.Kind = UpdateVoice
		in.VoiceDurationSec = msg.Voice.Duration
		in.AudioMIME = msg.Voice.MimeType
		fileID := msg.Voice.FileID
		in.AudioOpener = func(ctx context.Context) (io.ReadCloser, error) {
			return h.downloadFile(ctx, b, fileID)
		}
	case msg.Text != "":
		in.Kind = UpdateText
		in.Text = msg.Text
	default:
		in.Kind = UpdateOther
	}
	return in
}

func (h *TelegramHandler) downloadFile(ctx context.Context, b *bot.Bot, fileID string) (io.ReadCloser, error) {
	file, err := b.GetFile(ctx, &bot.GetFileParams{FileID: fileID})
	if err != nil {
		return nil, fmt.Errorf("get file: %w", err)
	}
	url := fmt.Sprintf("https://api.telegram.org/file/bot%s/%s", h.token, file.FilePath)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := h.httpC.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("download status %d", resp.StatusCode)
	}
	return resp.Body, nil
}
