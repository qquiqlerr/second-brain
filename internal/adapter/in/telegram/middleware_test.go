package telegram_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/aleksejmetlusko/second-brain/internal/adapter/in/telegram"
	"github.com/stretchr/testify/require"
)

func TestRecoverMiddleware_ConvertsPanicToReply(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := func(_ context.Context, _ telegram.IncomingUpdate) (string, error) {
		panic("kaboom")
	}
	wrapped := telegram.RecoverMiddleware(logger, handler)
	reply, err := wrapped(context.Background(), telegram.IncomingUpdate{UpdateID: 1, FromUserID: 1})
	require.NoError(t, err)
	require.Contains(t, reply, "❌ внутренняя ошибка")
}

func TestRecoverMiddleware_PassThroughError(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := func(_ context.Context, _ telegram.IncomingUpdate) (string, error) {
		return "", errors.New("boom")
	}
	wrapped := telegram.RecoverMiddleware(logger, handler)
	_, err := wrapped(context.Background(), telegram.IncomingUpdate{UpdateID: 1, FromUserID: 1})
	require.Error(t, err)
}
