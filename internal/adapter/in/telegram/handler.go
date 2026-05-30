package telegram

import (
	"context"
	"io"
	"log/slog"
	"strings"

	"github.com/aleksejmetlusko/second-brain/internal/domain"
	"github.com/aleksejmetlusko/second-brain/internal/port/in"
)

// MaxVoiceSeconds caps the duration of a single voice message we accept;
// audios above this need to be split by the user.
const MaxVoiceSeconds = 1500

// UpdateKind enumerates the message types the handler distinguishes.
type UpdateKind int

const (
	UpdateText UpdateKind = iota + 1
	UpdateVoice
	UpdateOther
)

// IncomingUpdate is the adapter-internal representation of a Telegram update,
// stripped of library-specific types so Route is pure-Go testable.
type IncomingUpdate struct {
	UpdateID         int64
	FromUserID       int64
	Kind             UpdateKind
	Text             string
	VoiceDurationSec int
	AudioOpener      func(context.Context) (io.ReadCloser, error)
	AudioMIME        string
}

// Action is the routing decision: what to do with the update.
type Action int

const (
	ActionDrop Action = iota + 1
	ActionProcessText
	ActionProcessVoice
	ActionReplyUnsupported
	ActionReplyTooLong
	ActionFind
)

// Decision returns Route's outcome with a pre-rendered user-visible reply
// for static-reply branches.
type Decision struct {
	Action Action
	Reply  string
}

// SearcherFn isolates the telegram package from the searcher use case.
// It receives a free-text query and returns the rendered reply text.
type SearcherFn func(ctx context.Context, query string) (string, error)

// Router decides what to do with each incoming update and runs the use case
// for the messages that pass the filters.
type Router struct {
	uc       in.IngestDumpUseCase
	searcher SearcherFn
	allowed  map[int64]struct{}
	dedup    *UpdateDedup
	logger   *slog.Logger
}

// NewRouter constructs a Router. Pass a nil searcher to disable /find.
func NewRouter(uc in.IngestDumpUseCase, searcher SearcherFn, allowed map[int64]struct{}, dedup *UpdateDedup, logger *slog.Logger) *Router {
	return &Router{uc: uc, searcher: searcher, allowed: allowed, dedup: dedup, logger: logger}
}

// Route classifies the update without running the use case.
func (r *Router) Route(u IncomingUpdate) Decision {
	if _, ok := r.allowed[u.FromUserID]; !ok {
		r.logger.Info("rejected non-allowlisted user", "user_id", u.FromUserID, "update_id", u.UpdateID)
		return Decision{Action: ActionDrop}
	}
	if r.dedup.Seen(u.UpdateID) {
		r.logger.Info("dropping duplicate update", "update_id", u.UpdateID)
		return Decision{Action: ActionDrop}
	}
	if u.Kind == UpdateText && strings.HasPrefix(strings.TrimSpace(u.Text), "/find") {
		return Decision{Action: ActionFind}
	}
	switch u.Kind {
	case UpdateText:
		return Decision{Action: ActionProcessText}
	case UpdateVoice:
		if u.VoiceDurationSec > MaxVoiceSeconds {
			return Decision{Action: ActionReplyTooLong, Reply: "❌ Голосовое > 25 мин, разбей"}
		}
		return Decision{Action: ActionProcessVoice}
	default:
		return Decision{Action: ActionReplyUnsupported, Reply: "Поддерживаются текст и голос"}
	}
}

// Handle runs the full incoming → use case → reply pipeline and returns the
// user-visible reply text.
func (r *Router) Handle(ctx context.Context, u IncomingUpdate) (string, error) {
	d := r.Route(u)
	switch d.Action {
	case ActionDrop:
		return "", nil
	case ActionReplyUnsupported, ActionReplyTooLong:
		return d.Reply, nil
	case ActionProcessText:
		res, err := r.uc.Execute(ctx, in.IngestRequest{
			Source: domain.SourceTelegramText,
			Text:   u.Text,
			UserID: u.FromUserID,
		})
		return FormatReply(res, err), nil
	case ActionProcessVoice:
		audio, err := u.AudioOpener(ctx)
		if err != nil {
			return "❌ не удалось скачать голосовое, повтори", nil
		}
		defer audio.Close()
		res, err := r.uc.Execute(ctx, in.IngestRequest{
			Source:    domain.SourceTelegramVoice,
			Audio:     audio,
			AudioMIME: u.AudioMIME,
			UserID:    u.FromUserID,
		})
		return FormatReply(res, err), nil
	case ActionFind:
		if r.searcher == nil {
			return "Поиск не настроен", nil
		}
		query := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(u.Text), "/find"))
		if query == "" {
			return "Использование: /find <запрос>", nil
		}
		return r.searcher(ctx, query)
	default:
		return "", nil
	}
}
