package telegram_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/aleksejmetlusko/second-brain/internal/adapter/in/telegram"
	"github.com/aleksejmetlusko/second-brain/internal/domain"
	"github.com/aleksejmetlusko/second-brain/internal/port/in"
	inmocks "github.com/aleksejmetlusko/second-brain/internal/port/in/mocks"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func nopLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestRoute_AllowlistRejectsUnknown(t *testing.T) {
	uc := inmocks.NewMockIngestDumpUseCase(t)
	r := telegram.NewRouter(uc, nil, map[int64]struct{}{42: {}}, telegram.NewUpdateDedup(8), nopLogger())

	decision := r.Route(telegram.IncomingUpdate{UpdateID: 1, FromUserID: 999, Kind: telegram.UpdateText, Text: "x"})
	require.Equal(t, telegram.ActionDrop, decision.Action)
}

func TestRoute_DedupSkipsDuplicate(t *testing.T) {
	uc := inmocks.NewMockIngestDumpUseCase(t)
	r := telegram.NewRouter(uc, nil, map[int64]struct{}{42: {}}, telegram.NewUpdateDedup(8), nopLogger())

	d1 := r.Route(telegram.IncomingUpdate{UpdateID: 7, FromUserID: 42, Kind: telegram.UpdateText, Text: "x"})
	require.Equal(t, telegram.ActionProcessText, d1.Action)

	d2 := r.Route(telegram.IncomingUpdate{UpdateID: 7, FromUserID: 42, Kind: telegram.UpdateText, Text: "x"})
	require.Equal(t, telegram.ActionDrop, d2.Action)
}

func TestRoute_VoiceTooLongRejected(t *testing.T) {
	uc := inmocks.NewMockIngestDumpUseCase(t)
	r := telegram.NewRouter(uc, nil, map[int64]struct{}{42: {}}, telegram.NewUpdateDedup(8), nopLogger())

	d := r.Route(telegram.IncomingUpdate{UpdateID: 1, FromUserID: 42, Kind: telegram.UpdateVoice, VoiceDurationSec: telegram.MaxVoiceSeconds + 1})
	require.Equal(t, telegram.ActionReplyTooLong, d.Action)
}

func TestRoute_VoiceWithinLimitProcessed(t *testing.T) {
	uc := inmocks.NewMockIngestDumpUseCase(t)
	r := telegram.NewRouter(uc, nil, map[int64]struct{}{42: {}}, telegram.NewUpdateDedup(8), nopLogger())

	d := r.Route(telegram.IncomingUpdate{UpdateID: 1, FromUserID: 42, Kind: telegram.UpdateVoice, VoiceDurationSec: 60})
	require.Equal(t, telegram.ActionProcessVoice, d.Action)
}

func TestRoute_UnsupportedKindReplies(t *testing.T) {
	uc := inmocks.NewMockIngestDumpUseCase(t)
	r := telegram.NewRouter(uc, nil, map[int64]struct{}{42: {}}, telegram.NewUpdateDedup(8), nopLogger())

	d := r.Route(telegram.IncomingUpdate{UpdateID: 1, FromUserID: 42, Kind: telegram.UpdateOther})
	require.Equal(t, telegram.ActionReplyUnsupported, d.Action)
}

func TestHandle_ProcessTextCallsUseCase(t *testing.T) {
	uc := inmocks.NewMockIngestDumpUseCase(t)
	uc.EXPECT().Execute(mock.Anything, mock.MatchedBy(func(req in.IngestRequest) bool {
		return req.Source == domain.SourceTelegramText && req.Text == "hello" && req.UserID == 42
	})).Return(in.IngestResult{Notes: []domain.Note{{Slug: "x", Category: "work"}}}, nil).Once()

	r := telegram.NewRouter(uc, nil, map[int64]struct{}{42: {}}, telegram.NewUpdateDedup(8), nopLogger())
	reply, err := r.Handle(t.Context(), telegram.IncomingUpdate{UpdateID: 1, FromUserID: 42, Kind: telegram.UpdateText, Text: "hello"})
	require.NoError(t, err)
	require.NotEmpty(t, reply)
}

func TestHandle_TopErrorIsFormatted(t *testing.T) {
	uc := inmocks.NewMockIngestDumpUseCase(t)
	uc.EXPECT().Execute(mock.Anything, mock.Anything).Return(in.IngestResult{}, errors.New("transcribe: down")).Once()

	r := telegram.NewRouter(uc, nil, map[int64]struct{}{42: {}}, telegram.NewUpdateDedup(8), nopLogger())
	reply, err := r.Handle(t.Context(), telegram.IncomingUpdate{UpdateID: 1, FromUserID: 42, Kind: telegram.UpdateText, Text: "hi"})
	require.NoError(t, err)
	require.Contains(t, reply, "❌")
}

func TestRoute_FindCommandTriggersActionFind(t *testing.T) {
	uc := inmocks.NewMockIngestDumpUseCase(t)
	r := telegram.NewRouter(uc, nil, map[int64]struct{}{42: {}}, telegram.NewUpdateDedup(8), nopLogger())
	d := r.Route(telegram.IncomingUpdate{UpdateID: 1, FromUserID: 42, Kind: telegram.UpdateText, Text: "/find auth bug"})
	require.Equal(t, telegram.ActionFind, d.Action)
}

func TestHandle_FindWithEmptyQueryGivesUsage(t *testing.T) {
	uc := inmocks.NewMockIngestDumpUseCase(t)
	searcher := func(_ context.Context, _ string) (string, error) { return "ok", nil }
	r := telegram.NewRouter(uc, searcher, map[int64]struct{}{42: {}}, telegram.NewUpdateDedup(8), nopLogger())
	reply, err := r.Handle(t.Context(), telegram.IncomingUpdate{UpdateID: 1, FromUserID: 42, Kind: telegram.UpdateText, Text: "/find  "})
	require.NoError(t, err)
	require.Contains(t, reply, "Использование")
}

func TestHandle_FindForwardsQueryToSearcher(t *testing.T) {
	uc := inmocks.NewMockIngestDumpUseCase(t)
	var captured string
	searcher := func(_ context.Context, q string) (string, error) {
		captured = q
		return "🔍 ok", nil
	}
	r := telegram.NewRouter(uc, searcher, map[int64]struct{}{42: {}}, telegram.NewUpdateDedup(8), nopLogger())
	reply, err := r.Handle(t.Context(), telegram.IncomingUpdate{UpdateID: 1, FromUserID: 42, Kind: telegram.UpdateText, Text: "/find auth bug"})
	require.NoError(t, err)
	require.Equal(t, "🔍 ok", reply)
	require.Equal(t, "auth bug", captured)
}
