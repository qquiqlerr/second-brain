package httpretry_test

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/aleksejmetlusko/second-brain/internal/adapter/httpretry"
	"github.com/stretchr/testify/require"
)

func TestRetry_StopsOnNonRetryableHTTP(t *testing.T) {
	policy := httpretry.Policy{MaxAttempts: 3, BaseDelay: time.Millisecond}
	var calls int
	err := httpretry.With(t.Context(), policy, func(_ context.Context) error {
		calls++
		return httpretry.HTTPError{Status: 401, Msg: "unauthorized"}
	})
	require.Equal(t, 1, calls)
	require.Error(t, err)
}

func TestRetry_RetriesOn5xx(t *testing.T) {
	policy := httpretry.Policy{MaxAttempts: 3, BaseDelay: time.Millisecond}
	var calls int
	err := httpretry.With(t.Context(), policy, func(_ context.Context) error {
		calls++
		if calls < 3 {
			return httpretry.HTTPError{Status: 503, Msg: "transient"}
		}
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, 3, calls)
}

func TestRetry_GivesUpAfterMaxAttempts(t *testing.T) {
	policy := httpretry.Policy{MaxAttempts: 2, BaseDelay: time.Millisecond}
	var calls int
	err := httpretry.With(t.Context(), policy, func(_ context.Context) error {
		calls++
		return httpretry.HTTPError{Status: 502, Msg: "bad gw"}
	})
	require.Equal(t, 2, calls)
	require.Error(t, err)
}

func TestRetry_RetriesOnNetError(t *testing.T) {
	policy := httpretry.Policy{MaxAttempts: 2, BaseDelay: time.Millisecond}
	var calls int
	err := httpretry.With(t.Context(), policy, func(_ context.Context) error {
		calls++
		if calls == 1 {
			return &net.OpError{Op: "dial", Err: errors.New("conn refused")}
		}
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, 2, calls)
}

func TestRetry_NoRetryOnContextCanceled(t *testing.T) {
	policy := httpretry.Policy{MaxAttempts: 3, BaseDelay: time.Millisecond}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	var calls int
	err := httpretry.With(ctx, policy, func(_ context.Context) error {
		calls++
		return httpretry.HTTPError{Status: 503, Msg: "down"}
	})
	require.Equal(t, 1, calls)
	require.ErrorIs(t, err, context.Canceled)
}
